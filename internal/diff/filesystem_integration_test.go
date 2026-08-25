package diff

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"foldersnap/internal/history"
	"foldersnap/internal/model"
	"foldersnap/internal/scan"
)

func TestAdjacentFilesystemSnapshotsDetectEveryChangeType(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "removed.txt"), "remove me")
	writeFile(t, filepath.Join(root, "modified.txt"), "before")

	scanner := scan.Scanner{}
	take := func() model.Snapshot {
		snapshot, err := scanner.Scan(context.Background(), scan.Request{
			RootID: "root", RootPath: root, Trigger: model.TriggerManual,
		})
		if err != nil {
			t.Fatal(err)
		}
		return snapshot
	}

	first := take()
	writeFile(t, filepath.Join(root, "added.txt"), "new")
	second := take()
	assertOnlyChange(t, first, second, model.ChangeAdded, "added.txt")

	if err := os.Remove(filepath.Join(root, "removed.txt")); err != nil {
		t.Fatal(err)
	}
	third := take()
	assertOnlyChange(t, second, third, model.ChangeRemoved, "removed.txt")

	modifiedPath := filepath.Join(root, "modified.txt")
	writeFile(t, modifiedPath, "after and a different size")
	stamp := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(modifiedPath, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	fourth := take()
	assertOnlyChange(t, third, fourth, model.ChangeModified, "modified.txt")
}

func TestFreshHistoryWorkflowComparesExplicitAdjacentPairs(t *testing.T) {
	root := t.TempDir()
	store := history.Store{DataDir: t.TempDir()}
	writeFile(t, filepath.Join(root, "removed.txt"), "remove me")
	writeFile(t, filepath.Join(root, "modified.txt"), "before")

	take := func() model.SnapshotRecord {
		snapshot, err := (scan.Scanner{}).Scan(context.Background(), scan.Request{
			RootID: "root", RootPath: root, DisplayTitle: "Test Root", Trigger: model.TriggerManual,
		})
		if err != nil {
			t.Fatal(err)
		}
		record, err := store.Save(snapshot, 50)
		if err != nil {
			t.Fatal(err)
		}
		return record
	}
	compare := func(beforeRecord, afterRecord model.SnapshotRecord, change model.ChangeType, path string) {
		before, err := store.Load(beforeRecord.SnapshotID)
		if err != nil {
			t.Fatal(err)
		}
		after, err := store.Load(afterRecord.SnapshotID)
		if err != nil {
			t.Fatal(err)
		}
		assertOnlyChange(t, before, after, change, path)
	}

	first := take()
	writeFile(t, filepath.Join(root, "added.txt"), "new")
	second := take()
	compare(first, second, model.ChangeAdded, "added.txt")

	if err := os.Remove(filepath.Join(root, "removed.txt")); err != nil {
		t.Fatal(err)
	}
	third := take()
	compare(second, third, model.ChangeRemoved, "removed.txt")

	modifiedPath := filepath.Join(root, "modified.txt")
	writeFile(t, modifiedPath, "after and a different size")
	stamp := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(modifiedPath, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	fourth := take()
	compare(third, fourth, model.ChangeModified, "modified.txt")

	records, err := store.List("root")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 4 || records[0].SnapshotID != fourth.SnapshotID || records[3].SnapshotID != first.SnapshotID {
		t.Fatalf("history is not newest-first: %+v", records)
	}
}

func assertOnlyChange(t *testing.T, before, after model.Snapshot, wantType model.ChangeType, wantPath string) {
	t.Helper()
	result, err := (Engine{}).Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	var changes []model.DiffEntry
	for _, entry := range result.Entries {
		if entry.Change != model.ChangeUnchanged && !entry.Uncertain && !entry.ScopeDifference {
			changes = append(changes, entry)
		}
	}
	if len(changes) != 1 || changes[0].Change != wantType || changes[0].Path != wantPath {
		t.Fatalf("changes = %+v, want one %s %s", changes, wantType, wantPath)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
