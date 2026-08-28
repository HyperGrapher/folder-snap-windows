package cleanup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"foldersnap/internal/atomicfile"
	"foldersnap/internal/model"
	"foldersnap/internal/pathutil"
)

type Status string

const (
	StatusReady             Status = "ready"
	StatusAlreadyMissing    Status = "already_missing"
	StatusChanged           Status = "changed_since_snapshot"
	StatusTypeChanged       Status = "type_changed"
	StatusInvalidPath       Status = "outside_root_or_invalid"
	StatusUnreadable        Status = "access_denied_or_unreadable"
	StatusContainsUntracked Status = "contains_untracked_content"
	StatusMoved             Status = "moved_to_recycle_bin"
	StatusFailed            Status = "failed"
)

type Candidate struct {
	Path  string
	Entry model.SnapshotEntry
}

type PreflightItem struct {
	Candidate Candidate
	Target    string
	Status    Status
	Reason    string
}

type MoveResult struct {
	Target string
	Error  error
}

type RecycleBin interface {
	Move(context.Context, []string) []MoveResult
}

type Service struct {
	RootPath string
	Recycler RecycleBin
}

func Plan(diff model.DiffResult) []Candidate {
	var result []Candidate
	for _, item := range diff.Entries {
		if item.Change == model.ChangeAdded && !item.Uncertain && !item.ScopeDifference && item.After != nil {
			result = append(result, Candidate{Path: item.Path, Entry: *item.After})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}

func (s Service) Preflight(ctx context.Context, selected []Candidate) []PreflightItem {
	known := make(map[string]Candidate, len(selected))
	for _, item := range selected {
		known[item.Path] = item
	}
	results := make([]PreflightItem, 0, len(selected))
	for _, candidate := range selected {
		if ctx.Err() != nil {
			break
		}
		if normalized, normalizeErr := pathutil.NormalizeRelative(candidate.Path); normalizeErr != nil || normalized == "" {
			results = append(results, PreflightItem{Candidate: candidate, Status: StatusInvalidPath, Reason: "empty or invalid relative path"})
			continue
		}
		target, err := pathutil.JoinWithinRoot(s.RootPath, candidate.Path)
		if err != nil {
			results = append(results, PreflightItem{Candidate: candidate, Status: StatusInvalidPath, Reason: err.Error()})
			continue
		}
		item := PreflightItem{Candidate: candidate, Target: target}
		if ancestor, ancestorErr := reparseAncestor(s.RootPath, target); ancestorErr != nil {
			item.Status, item.Reason = StatusUnreadable, ancestorErr.Error()
			results = append(results, item)
			continue
		} else if ancestor != "" {
			item.Status, item.Reason = StatusInvalidPath, "path crosses reparse point: "+ancestor
			results = append(results, item)
			continue
		}
		info, err := os.Lstat(target)
		if errors.Is(err, os.ErrNotExist) {
			item.Status = StatusAlreadyMissing
			results = append(results, item)
			continue
		}
		if err != nil {
			item.Status, item.Reason = StatusUnreadable, err.Error()
			results = append(results, item)
			continue
		}
		currentType := typeOf(info)
		if currentType != candidate.Entry.Type {
			item.Status, item.Reason = StatusTypeChanged, fmt.Sprintf("current type is %s", currentType)
			results = append(results, item)
			continue
		}
		if currentType == model.EntryFile && !matchesFile(info, candidate.Entry) {
			item.Status, item.Reason = StatusChanged, "size or timestamp changed"
			results = append(results, item)
			continue
		}
		if currentType == model.EntryReparse {
			currentTarget, readErr := os.Readlink(target)
			if readErr != nil || (candidate.Entry.LinkTarget != "" && currentTarget != candidate.Entry.LinkTarget) {
				item.Status, item.Reason = StatusChanged, "link target changed or cannot be read"
				results = append(results, item)
				continue
			}
		}
		if currentType == model.EntryDirectory {
			if extra, walkErr := containsUntracked(target, candidate.Path, known); walkErr != nil {
				item.Status, item.Reason = StatusUnreadable, walkErr.Error()
				results = append(results, item)
				continue
			} else if extra != "" {
				item.Status, item.Reason = StatusContainsUntracked, "current content is not selected: "+extra
				results = append(results, item)
				continue
			}
		}
		item.Status = StatusReady
		results = append(results, item)
	}
	for i := range results {
		if results[i].Status != StatusReady || results[i].Candidate.Entry.Type != model.EntryDirectory {
			continue
		}
		prefix := results[i].Candidate.Path + "/"
		for _, child := range results {
			if strings.HasPrefix(child.Candidate.Path, prefix) && child.Status != StatusReady && child.Status != StatusAlreadyMissing {
				results[i].Status = StatusContainsUntracked
				results[i].Reason = "a selected descendant did not pass preflight: " + child.Candidate.Path
				break
			}
		}
	}
	return results
}

func (s Service) Execute(ctx context.Context, preflight []PreflightItem) []PreflightItem {
	result := append([]PreflightItem(nil), preflight...)
	candidates := make([]Candidate, 0, len(preflight))
	for _, item := range preflight {
		if item.Status == StatusReady {
			candidates = append(candidates, item.Candidate)
		}
	}
	// Revalidate after the confirmation dialog and immediately before any
	// filesystem mutation. This closes the preflight/execute race window.
	revalidated := s.Preflight(ctx, candidates)
	ready := make([]PreflightItem, 0, len(revalidated))
	for _, current := range revalidated {
		for index := range result {
			if result[index].Candidate.Path == current.Candidate.Path {
				result[index] = current
				break
			}
		}
		if current.Status == StatusReady {
			ready = append(ready, current)
		}
	}
	sort.Slice(ready, func(i, j int) bool {
		leftDepth := strings.Count(ready[i].Candidate.Path, "/")
		rightDepth := strings.Count(ready[j].Candidate.Path, "/")
		if leftDepth != rightDepth {
			return leftDepth > rightDepth
		}
		return ready[i].Candidate.Entry.Type != model.EntryDirectory && ready[j].Candidate.Entry.Type == model.EntryDirectory
	})
	for _, item := range ready {
		if item.Candidate.Entry.Type == model.EntryDirectory {
			entries, err := os.ReadDir(item.Target)
			if err == nil && len(entries) > 0 {
				for i := range result {
					if result[i].Candidate.Path == item.Candidate.Path {
						result[i].Status, result[i].Reason = StatusContainsUntracked, "directory is not empty after child operations"
					}
				}
				continue
			}
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				for i := range result {
					if result[i].Candidate.Path == item.Candidate.Path {
						result[i].Status, result[i].Reason = StatusFailed, err.Error()
					}
				}
				continue
			}
		}
		moves := s.Recycler.Move(ctx, []string{item.Target})
		for i := range result {
			if result[i].Candidate.Path != item.Candidate.Path {
				continue
			}
			if len(moves) == 0 || moves[0].Error != nil {
				result[i].Status = StatusFailed
				if len(moves) == 0 {
					result[i].Reason = "Recycle Bin adapter returned no result"
				} else {
					result[i].Reason = moves[0].Error.Error()
				}
			} else {
				result[i].Status = StatusMoved
			}
		}
	}
	return result
}

func WriteAudit(dataDir, rootID, beforeID, afterID string, results []PreflightItem) error {
	if err := pathutil.ValidateStorageID(rootID); err != nil {
		return err
	}
	path := filepath.Join(dataDir, "roots", rootID, "cleanup-log.jsonl")
	var existing []byte
	existing, _ = os.ReadFile(path)
	record := struct {
		Timestamp time.Time       `json:"timestamp"`
		RootID    string          `json:"rootId"`
		BeforeID  string          `json:"beforeSnapshotId"`
		AfterID   string          `json:"afterSnapshotId"`
		Results   []PreflightItem `json:"results"`
	}{time.Now().UTC(), rootID, beforeID, afterID, results}
	line, err := json.Marshal(record)
	if err != nil {
		return err
	}
	existing = append(existing, line...)
	existing = append(existing, '\n')
	return atomicfile.Write(path, 0o600, func(file *os.File) error { _, err := file.Write(existing); return err })
}

func reparseAncestor(root, target string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return "", err
	}
	parts := strings.FieldsFunc(relative, func(r rune) bool { return r == '/' || r == '\\' })
	current := rootAbs
	for _, part := range parts[:max(0, len(parts)-1)] {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			return "", nil
		}
		if statErr != nil {
			return "", statErr
		}
		if typeOf(info) == model.EntryReparse {
			return current, nil
		}
	}
	return "", nil
}

func matchesFile(info fs.FileInfo, expected model.SnapshotEntry) bool {
	if info.Size() != expected.Size || info.ModTime().UTC().UnixNano() != expected.ModifiedUnixNs {
		return false
	}
	if data, ok := info.Sys().(*syscall.Win32FileAttributeData); ok && expected.CreatedUnixNs != 0 {
		return data.CreationTime.Nanoseconds() == expected.CreatedUnixNs
	}
	return true
}

func typeOf(info fs.FileInfo) model.EntryType {
	attributes := uint32(0)
	if data, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		attributes = data.FileAttributes
	}
	if attributes&0x400 != 0 || info.Mode()&os.ModeSymlink != 0 {
		return model.EntryReparse
	}
	if info.IsDir() {
		return model.EntryDirectory
	}
	if info.Mode().IsRegular() {
		return model.EntryFile
	}
	return model.EntryOther
}

func containsUntracked(directory, relative string, selected map[string]Candidate) (string, error) {
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == directory {
			return nil
		}
		rel, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		candidatePath := strings.Trim(relative+"/"+strings.ToLower(filepath.ToSlash(rel)), "/")
		if _, ok := selected[candidatePath]; !ok {
			return fmt.Errorf("%w:%s", errExtraContent, candidatePath)
		}
		if info, err := entry.Info(); err == nil && typeOf(info) == model.EntryReparse && entry.IsDir() {
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errExtraContent) {
			parts := strings.SplitN(err.Error(), ":", 2)
			if len(parts) == 2 {
				return parts[1], nil
			}
		}
		return "", err
	}
	return "", nil
}

var errExtraContent = errors.New("extra content")
