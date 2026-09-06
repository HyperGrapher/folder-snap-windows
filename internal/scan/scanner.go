package scan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"foldersnap/internal/ids"
	ignorepkg "foldersnap/internal/ignore"
	"foldersnap/internal/model"
	"foldersnap/internal/pathutil"
)

const fileAttributeReparsePoint = 0x400
const directoryBatchSize = 256

type Request struct {
	RootID       string
	RootPath     string
	DisplayTitle string
	Trigger      model.SnapshotTrigger
	Description  string
	IgnoreRules  []string
	Progress     func(items int64, relativePath string)
}

type Scanner struct {
	Now     func() time.Time
	Workers int
}

type directoryTask struct {
	absolute    string
	displayPath string
	root        bool
}

type directoryResult struct {
	entries     []model.SnapshotEntry
	directories []directoryTask
	warnings    []model.ScanWarning
	err         error
	complete    bool
}

func (s Scanner) Scan(ctx context.Context, req Request) (model.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return model.Snapshot{}, err
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	started := s.Now().UTC()
	rootInfo, err := os.Stat(req.RootPath)
	if err != nil {
		return model.Snapshot{}, fmt.Errorf("validate root: %w", err)
	}
	if !rootInfo.IsDir() {
		return model.Snapshot{}, errors.New("watched root is not a directory")
	}
	matcher, err := ignorepkg.Compile(req.IgnoreRules)
	if err != nil {
		return model.Snapshot{}, err
	}

	snapshot := model.Snapshot{SchemaVersion: model.SchemaVersion}
	snapshot.Header = model.SnapshotHeader{
		SchemaVersion:     model.SchemaVersion,
		SnapshotID:        ids.New(),
		RootID:            req.RootID,
		RootPathAtCapture: req.RootPath,
		DisplayTitle:      req.DisplayTitle,
		StartedAtUTC:      started,
		Trigger:           req.Trigger,
		Description:       req.Description,
		IgnoreConfig: model.IgnoreConfigSnapshot{
			Rules: append([]string(nil), req.IgnoreRules...),
			Hash:  matcher.Hash(),
		},
	}

	entries, warnings, err := s.walkConcurrent(ctx, req.RootPath, matcher, req.Progress)
	if err != nil {
		return model.Snapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return model.Snapshot{}, err
	}
	snapshot.Entries = entries
	snapshot.Header.ScanWarnings = warnings
	sort.Slice(snapshot.Header.ScanWarnings, func(i, j int) bool {
		if snapshot.Header.ScanWarnings[i].Path != snapshot.Header.ScanWarnings[j].Path {
			return snapshot.Header.ScanWarnings[i].Path < snapshot.Header.ScanWarnings[j].Path
		}
		return snapshot.Header.ScanWarnings[i].Operation < snapshot.Header.ScanWarnings[j].Operation
	})
	for _, item := range snapshot.Entries {
		switch item.Type {
		case model.EntryFile:
			snapshot.Header.FileCount++
			snapshot.Header.TotalFileBytes += item.Size
		case model.EntryDirectory:
			snapshot.Header.DirectoryCount++
		default:
			snapshot.Header.OtherCount++
		}
	}
	sort.Slice(snapshot.Entries, func(i, j int) bool {
		return snapshot.Entries[i].RelativePath < snapshot.Entries[j].RelativePath
	})
	if err := ctx.Err(); err != nil {
		return model.Snapshot{}, err
	}
	snapshot.EntriesSorted = true
	snapshot.Header.CompletedAtUTC = s.Now().UTC()
	return snapshot, nil
}

func (s Scanner) walkConcurrent(ctx context.Context, root string, matcher *ignorepkg.Matcher, progress func(int64, string)) ([]model.SnapshotEntry, []model.ScanWarning, error) {
	workers := s.Workers
	if workers <= 0 {
		workers = 4
	}
	if workers > 32 {
		workers = 32
	}
	walkCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	tasks := make(chan directoryTask)
	results := make(chan directoryResult, workers)
	var workersDone sync.WaitGroup
	workersDone.Add(workers)
	for range workers {
		go func() {
			defer workersDone.Done()
			for task := range tasks {
				scanDirectoryTask(walkCtx, task, matcher, results)
			}
		}()
	}

	queue := []directoryTask{{absolute: root, root: true}}
	inFlight := 0
	var entries []model.SnapshotEntry
	var warnings []model.ScanWarning
	var firstErr error
	var seen int64
	cancellation := walkCtx.Done()
	for len(queue) > 0 || inFlight > 0 {
		var taskOutput chan directoryTask
		var next directoryTask
		if firstErr == nil && len(queue) > 0 {
			taskOutput = tasks
			next = queue[0]
		}
		select {
		case <-cancellation:
			if firstErr == nil {
				firstErr = walkCtx.Err()
				queue = queue[:0]
			}
			cancellation = nil
		case taskOutput <- next:
			queue = queue[1:]
			inFlight++
		case result := <-results:
			if result.complete {
				inFlight--
			}
			if result.err != nil && firstErr == nil {
				firstErr = result.err
				queue = queue[:0]
				cancel()
			}
			warnings = append(warnings, result.warnings...)
			if firstErr == nil {
				queue = append(queue, result.directories...)
			}
			for _, item := range result.entries {
				if err := walkCtx.Err(); err != nil {
					if firstErr == nil {
						firstErr = err
						queue = queue[:0]
					}
					break
				}
				entries = append(entries, item)
				seen++
				if progress != nil && (seen == 1 || seen%256 == 0) {
					if err := callProgress(progress, seen, item.DisplayPath); err != nil && firstErr == nil {
						firstErr = err
						queue = queue[:0]
						cancel()
					}
				}
			}
		}
	}
	close(tasks)
	workersDone.Wait()
	if firstErr != nil {
		return nil, nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return entries, warnings, nil
}

func scanDirectoryTask(ctx context.Context, task directoryTask, matcher *ignorepkg.Matcher, results chan<- directoryResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			results <- directoryResult{err: fmt.Errorf("scan directory %q panic: %v", task.displayPath, recovered), complete: true}
		}
	}()
	err := scanDirectory(ctx, task, matcher, func(result directoryResult) bool {
		select {
		case results <- result:
			return true
		case <-ctx.Done():
			return false
		}
	})
	results <- directoryResult{err: err, complete: true}
}

func scanDirectory(ctx context.Context, task directoryTask, matcher *ignorepkg.Matcher, emit func(directoryResult) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	directory, err := os.Open(task.absolute)
	if err != nil {
		if task.root {
			return fmt.Errorf("enumerate root: %w", err)
		}
		normalized, _ := pathutil.NormalizeRelative(task.displayPath)
		emit(directoryResult{warnings: []model.ScanWarning{warning(normalized, "enumerate", err)}})
		return nil
	}
	defer directory.Close()
	for {
		children, readErr := directory.ReadDir(directoryBatchSize)
		result := directoryResult{
			entries:     make([]model.SnapshotEntry, 0, len(children)),
			directories: make([]directoryTask, 0),
		}
		for _, entry := range children {
			if err := ctx.Err(); err != nil {
				return err
			}
			displayPath := entry.Name()
			if task.displayPath != "" {
				displayPath = task.displayPath + "/" + entry.Name()
			}
			normalized, err := pathutil.NormalizeRelative(displayPath)
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				result.warnings = append(result.warnings, warning(normalized, "stat", err))
				continue
			}
			attributes, created := windowsMetadata(info)
			isReparse := attributes&fileAttributeReparsePoint != 0 || info.Mode()&os.ModeSymlink != 0
			isDirectory := info.IsDir()
			match := matcher.Match(normalized, isDirectory)
			current := filepath.Join(task.absolute, entry.Name())
			if !match.Excluded {
				item := model.SnapshotEntry{
					RelativePath: normalized, DisplayPath: displayPath,
					Type: entryType(info, isReparse), Size: info.Size(),
					ModifiedUnixNs: info.ModTime().UTC().UnixNano(), CreatedUnixNs: created, Attributes: attributes,
				}
				if item.Type != model.EntryFile {
					item.Size = 0
				}
				if isReparse {
					if target, linkErr := os.Readlink(current); linkErr == nil {
						item.LinkTarget = target
					}
				}
				result.entries = append(result.entries, item)
			}
			if isDirectory && !isReparse && (!match.Excluded || !matcher.CanPrune(normalized)) {
				result.directories = append(result.directories, directoryTask{absolute: current, displayPath: displayPath})
			}
		}
		if len(result.entries) > 0 || len(result.directories) > 0 || len(result.warnings) > 0 {
			if !emit(result) {
				return ctx.Err()
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			if task.root {
				return fmt.Errorf("enumerate root: %w", readErr)
			}
			normalized, _ := pathutil.NormalizeRelative(task.displayPath)
			emit(directoryResult{warnings: []model.ScanWarning{warning(normalized, "enumerate", readErr)}})
			return nil
		}
	}
}

func callProgress(progress func(int64, string), items int64, path string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("scan progress callback panic: %v", recovered)
		}
	}()
	progress(items, path)
	return nil
}

func entryType(info fs.FileInfo, reparse bool) model.EntryType {
	if reparse {
		return model.EntryReparse
	}
	if info.Mode().IsRegular() {
		return model.EntryFile
	}
	if info.IsDir() {
		return model.EntryDirectory
	}
	return model.EntryOther
}

func windowsMetadata(info fs.FileInfo) (uint32, int64) {
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok || data == nil {
		return 0, 0
	}
	return data.FileAttributes, data.CreationTime.Nanoseconds()
}

func warning(path, operation string, err error) model.ScanWarning {
	category := "io"
	if errors.Is(err, fs.ErrPermission) {
		category = "access_denied"
	} else if errors.Is(err, fs.ErrNotExist) {
		category = "not_found"
	}
	return model.ScanWarning{Path: strings.ToLower(filepath.ToSlash(path)), Operation: operation, Category: category, Message: err.Error()}
}
