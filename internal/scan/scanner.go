package scan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"foldersnap/internal/ids"
	ignorepkg "foldersnap/internal/ignore"
	"foldersnap/internal/model"
	"foldersnap/internal/pathutil"
)

const fileAttributeReparsePoint = 0x400

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
	Now func() time.Time
}

func (s Scanner) Scan(ctx context.Context, req Request) (model.Snapshot, error) {
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

	var seen int64
	err = filepath.WalkDir(req.RootPath, func(current string, entry fs.DirEntry, walkErr error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if current == req.RootPath {
			if walkErr != nil {
				return walkErr
			}
			return nil
		}
		relative, relErr := filepath.Rel(req.RootPath, current)
		if relErr != nil {
			return relErr
		}
		displayPath := filepath.ToSlash(relative)
		normalized, normalizeErr := pathutil.NormalizeRelative(displayPath)
		if normalizeErr != nil {
			return normalizeErr
		}

		if walkErr != nil {
			snapshot.Header.ScanWarnings = append(snapshot.Header.ScanWarnings, warning(normalized, "enumerate", walkErr))
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			snapshot.Header.ScanWarnings = append(snapshot.Header.ScanWarnings, warning(normalized, "stat", infoErr))
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		attributes, created := windowsMetadata(info)
		isReparse := attributes&fileAttributeReparsePoint != 0 || info.Mode()&os.ModeSymlink != 0
		isDir := info.IsDir()
		match := matcher.Match(normalized, isDir)
		if match.Excluded {
			if isDir && !isReparse && matcher.CanPrune(normalized) {
				return fs.SkipDir
			}
			if !isDir || isReparse {
				return nil
			}
		} else {
			item := model.SnapshotEntry{
				RelativePath:   normalized,
				DisplayPath:    displayPath,
				Type:           entryType(info, isReparse),
				ModifiedUnixNs: info.ModTime().UTC().UnixNano(),
				CreatedUnixNs:  created,
				Attributes:     attributes,
			}
			if item.Type == model.EntryFile {
				item.Size = info.Size()
				snapshot.Header.FileCount++
				snapshot.Header.TotalFileBytes += item.Size
			} else if item.Type == model.EntryDirectory {
				snapshot.Header.DirectoryCount++
			} else {
				snapshot.Header.OtherCount++
			}
			if isReparse {
				if target, readErr := os.Readlink(current); readErr == nil {
					item.LinkTarget = target
				}
			}
			snapshot.Entries = append(snapshot.Entries, item)
			seen++
			if req.Progress != nil && (seen == 1 || seen%256 == 0) {
				req.Progress(seen, displayPath)
			}
		}
		if isReparse && isDir {
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		return model.Snapshot{}, err
	}
	sort.Slice(snapshot.Entries, func(i, j int) bool {
		return snapshot.Entries[i].RelativePath < snapshot.Entries[j].RelativePath
	})
	snapshot.Header.CompletedAtUTC = s.Now().UTC()
	return snapshot, nil
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
