package cleanup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"foldersnap/internal/model"
	"foldersnap/internal/pathutil"
)

type fakeRecycler struct {
	fail  bool
	moved []string
}

func TestPreflightRejectsReparseAncestorOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "redirect")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	info, err := os.Stat(outsideFile)
	if err != nil {
		t.Fatal(err)
	}
	candidate := Candidate{Path: "redirect/outside.txt", Entry: model.SnapshotEntry{RelativePath: "redirect/outside.txt", Type: model.EntryFile, Size: info.Size(), ModifiedUnixNs: info.ModTime().UnixNano()}}
	result := (Service{RootPath: root}).Preflight(context.Background(), []Candidate{candidate})
	if len(result) != 1 || result[0].Status != StatusInvalidPath {
		t.Fatalf("reparse ancestor was not blocked: %+v", result)
	}
}

func TestWriteAuditRejectsUnsafeRootID(t *testing.T) {
	err := WriteAudit(t.TempDir(), `..\outside`, "before", "after", nil)
	if !errors.Is(err, pathutil.ErrUnsafeStorageID) {
		t.Fatalf("error = %v, want ErrUnsafeStorageID", err)
	}
}

func TestPreflightNeverTargetsWatchedRootItself(t *testing.T) {
	root := t.TempDir()
	candidate := Candidate{Path: "", Entry: model.SnapshotEntry{Type: model.EntryDirectory}}
	result := (Service{RootPath: root}).Preflight(context.Background(), []Candidate{candidate})
	if len(result) != 1 || result[0].Status != StatusInvalidPath || result[0].Target != "" {
		t.Fatalf("empty path was not blocked: %+v", result)
	}
}

func (f *fakeRecycler) Move(_ context.Context, targets []string) []MoveResult {
	f.moved = append(f.moved, targets...)
	result := make([]MoveResult, len(targets))
	for i, target := range targets {
		result[i].Target = target
		if f.fail {
			result[i].Error = os.ErrPermission
		}
	}
	return result
}

func TestPlanAndPreflightChangedFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "added.txt")
	if err := os.WriteFile(path, []byte("now"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	diff := model.DiffResult{Entries: []model.DiffEntry{{Path: "added.txt", Change: model.ChangeAdded, After: &model.SnapshotEntry{RelativePath: "added.txt", Type: model.EntryFile, Size: 3, ModifiedUnixNs: info.ModTime().UnixNano() - 1}}}}
	candidates := Plan(diff)
	result := (Service{RootPath: root}).Preflight(context.Background(), candidates)
	if len(result) != 1 || result[0].Status != StatusChanged {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestDirectoryWithCurrentExtraContentIsBlocked(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "newdir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	candidates := []Candidate{{Path: "newdir", Entry: model.SnapshotEntry{RelativePath: "newdir", Type: model.EntryDirectory}}}
	result := (Service{RootPath: root}).Preflight(context.Background(), candidates)
	if result[0].Status != StatusContainsUntracked {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestDirectoryIsBlockedWhenSelectedChildChanged(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "newdir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	childPath := filepath.Join(dir, "child.txt")
	if err := os.WriteFile(childPath, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(childPath)
	candidates := []Candidate{
		{Path: "newdir", Entry: model.SnapshotEntry{RelativePath: "newdir", Type: model.EntryDirectory}},
		{Path: "newdir/child.txt", Entry: model.SnapshotEntry{RelativePath: "newdir/child.txt", Type: model.EntryFile, Size: info.Size() - 1, ModifiedUnixNs: info.ModTime().UnixNano()}},
	}
	result := (Service{RootPath: root}).Preflight(context.Background(), candidates)
	if result[0].Status != StatusContainsUntracked || result[1].Status != StatusChanged {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestExecuteNeverFallsBack(t *testing.T) {
	recycler := &fakeRecycler{fail: true}
	root := t.TempDir()
	path := filepath.Join(root, "a")
	if err := os.WriteFile(path, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	candidate := Candidate{Path: "a", Entry: model.SnapshotEntry{RelativePath: "a", Type: model.EntryFile, Size: info.Size(), ModifiedUnixNs: info.ModTime().UnixNano()}}
	service := Service{RootPath: root, Recycler: recycler}
	result := service.Execute(context.Background(), service.Preflight(context.Background(), []Candidate{candidate}))
	if result[0].Status != StatusFailed || len(recycler.moved) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestExecuteRevalidatesChangedFileBeforeRecycleBin(t *testing.T) {
	recycler := &fakeRecycler{}
	root := t.TempDir()
	path := filepath.Join(root, "added.txt")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	candidate := Candidate{Path: "added.txt", Entry: model.SnapshotEntry{RelativePath: "added.txt", Type: model.EntryFile, Size: info.Size(), ModifiedUnixNs: info.ModTime().UnixNano()}}
	service := Service{RootPath: root, Recycler: recycler}
	preflight := service.Preflight(context.Background(), []Candidate{candidate})
	if preflight[0].Status != StatusReady {
		t.Fatalf("initial preflight = %+v", preflight)
	}
	if err := os.WriteFile(path, []byte("changed after confirmation"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := service.Execute(context.Background(), preflight)
	if result[0].Status != StatusChanged || len(recycler.moved) != 0 {
		t.Fatalf("changed file was not blocked: result=%+v moved=%v", result, recycler.moved)
	}
}
