package diff

import (
	"fmt"
	"testing"
	"time"

	"foldersnap/internal/model"
)

func TestCompareCoreCases(t *testing.T) {
	before := snapshot("before", 1, []model.SnapshotEntry{
		entry("dir", model.EntryDirectory, 0, 10),
		entry("modified.txt", model.EntryFile, 1, 10),
		entry("old.txt", model.EntryFile, 2, 10),
		entry("rename-old.txt", model.EntryFile, 3, 10),
		entry("same.txt", model.EntryFile, 4, 10),
		entry("type", model.EntryFile, 1, 10),
	})
	after := snapshot("after", 2, []model.SnapshotEntry{
		entry("dir", model.EntryDirectory, 0, 99),
		entry("modified.txt", model.EntryFile, 8, 20),
		entry("new.txt", model.EntryFile, 5, 10),
		entry("rename-new.txt", model.EntryFile, 3, 10),
		entry("same.txt", model.EntryFile, 4, 10),
		entry("type", model.EntryDirectory, 0, 10),
	})
	result, err := (Engine{}).Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Added != 2 || result.Summary.Removed != 2 || result.Summary.Modified != 2 || result.Summary.Unchanged != 2 {
		t.Fatalf("unexpected summary: %+v", result.Summary)
	}
	for _, item := range result.Entries {
		if item.Path == "dir" && item.Change != model.ChangeUnchanged {
			t.Fatal("directory mtime produced noise")
		}
		if item.Path == "type" && item.Subtype != "type_changed" {
			t.Fatal("missing type change subtype")
		}
	}
}

func TestLargeMostlyUnchangedComparisonMaterializesOnlyChanges(t *testing.T) {
	const count = 50_000
	beforeEntries := make([]model.SnapshotEntry, count)
	afterEntries := make([]model.SnapshotEntry, count)
	for index := range count {
		path := fmt.Sprintf("folder/file-%06d.dat", index)
		beforeEntries[index] = entry(path, model.EntryFile, 64, 10)
		afterEntries[index] = entry(path, model.EntryFile, 64, 10)
	}
	afterEntries[count-1].Size++
	result, err := (Engine{}).Compare(snapshot("before", 1, beforeEntries), snapshot("after", 2, afterEntries))
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Unchanged != count-1 || result.Summary.Modified != 1 {
		t.Fatalf("unexpected summary: %+v", result.Summary)
	}
	if len(result.Entries) != 1 || result.Entries[0].Path != afterEntries[count-1].RelativePath {
		t.Fatalf("materialized %d entries, want only the changed entry", len(result.Entries))
	}
}

func TestUncertaintyAndScope(t *testing.T) {
	before := snapshot("before", 1, nil)
	before.Header.ScanWarnings = []model.ScanWarning{{Path: "blocked", Operation: "enumerate"}}
	before.Header.IgnoreConfig.Rules = []string{"generated/"}
	after := snapshot("after", 2, []model.SnapshotEntry{entry("blocked/a", model.EntryFile, 1, 1), entry("generated/a", model.EntryFile, 1, 1)})
	result, err := (Engine{}).Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Uncertain != 1 || result.Summary.ScopeDifference != 1 || result.Summary.Added != 0 {
		t.Fatalf("unexpected summary: %+v", result.Summary)
	}
}

func TestFileAttributesAloneDoNotProduceModifiedNoise(t *testing.T) {
	before := snapshot("before", 1, []model.SnapshotEntry{entry("same.txt", model.EntryFile, 4, 10)})
	after := snapshot("after", 2, []model.SnapshotEntry{entry("same.txt", model.EntryFile, 4, 10)})
	before.Entries[0].Attributes = 1
	after.Entries[0].Attributes = 2
	result, err := (Engine{}).Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Modified != 0 || result.Summary.Unchanged != 1 {
		t.Fatalf("attribute-only change produced noise: %+v", result.Summary)
	}
}

func TestChangesUseMacOSGroupAndNaturalPathOrder(t *testing.T) {
	before := snapshot("before", 1, []model.SnapshotEntry{entry("removed.txt", model.EntryFile, 1, 1)})
	after := snapshot("after", 2, []model.SnapshotEntry{
		entry("file10.txt", model.EntryFile, 1, 1),
		entry("file2.txt", model.EntryFile, 1, 1),
	})
	result, err := (Engine{}).Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{result.Entries[0].Path, result.Entries[1].Path, result.Entries[2].Path}
	want := []string{"file2.txt", "file10.txt", "removed.txt"}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("paths = %v, want %v", paths, want)
		}
	}
}

func snapshot(id string, second int64, entries []model.SnapshotEntry) model.Snapshot {
	var total int64
	for _, item := range entries {
		if item.Type == model.EntryFile {
			total += item.Size
		}
	}
	return model.Snapshot{SchemaVersion: 1, Header: model.SnapshotHeader{SchemaVersion: 1, SnapshotID: id, RootID: "root", CompletedAtUTC: time.Unix(second, 0), TotalFileBytes: total}, Entries: entries}
}

func entry(path string, typ model.EntryType, size, modified int64) model.SnapshotEntry {
	return model.SnapshotEntry{RelativePath: path, DisplayPath: path, Type: typ, Size: size, ModifiedUnixNs: modified}
}
