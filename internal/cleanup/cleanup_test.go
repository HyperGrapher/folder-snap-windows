package cleanup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"foldersnap/internal/model"
)

type fakeRecycler struct {
	fail  bool
	moved []string
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
	service := Service{RootPath: t.TempDir(), Recycler: recycler}
	result := service.Execute(context.Background(), []PreflightItem{{Candidate: Candidate{Path: "a"}, Target: "a", Status: StatusReady}})
	if result[0].Status != StatusFailed || len(recycler.moved) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}
