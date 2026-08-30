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

func BenchmarkCompareLargeMostlyUnchanged(b *testing.B) {
	const count = 200_000
	beforeEntries := make([]model.SnapshotEntry, count)
	afterEntries := make([]model.SnapshotEntry, count)
	for index := range count {
		path := fmt.Sprintf("folder/file-%06d.dat", index)
		beforeEntries[index] = entry(path, model.EntryFile, 64, 10)
		afterEntries[index] = entry(path, model.EntryFile, 64, 10)
	}
	afterEntries[count-1].Size++
	before := snapshot("before", 1, beforeEntries)
	after := snapshot("after", 2, afterEntries)
	before.EntriesSorted = true
	after.EntriesSorted = true
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := (Engine{}).Compare(before, after)
		if err != nil || len(result.Entries) != 1 {
			b.Fatalf("entries=%d err=%v", len(result.Entries), err)
		}
	}
}

func BenchmarkCompareLargeChangeSet(b *testing.B) {
	const count = 50_000
	beforeEntries := make([]model.SnapshotEntry, count)
	afterEntries := make([]model.SnapshotEntry, count)
	for index := range count {
		beforeEntries[index] = entry(fmt.Sprintf("removed/item-%06d.dat", index), model.EntryFile, 64, 10)
		afterEntries[index] = entry(fmt.Sprintf("added/item-%06d.dat", index), model.EntryFile, 64, 10)
	}
	before := snapshot("before", 1, beforeEntries)
	after := snapshot("after", 2, afterEntries)
	before.EntriesSorted = true
	after.EntriesSorted = true
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := (Engine{}).Compare(before, after)
		if err != nil || len(result.Entries) != count*2 {
			b.Fatalf("entries=%d err=%v", len(result.Entries), err)
		}
	}
}

func TestUncertaintyAndScope(t *testing.T) {
	before := snapshot("before", 1, nil)
	before.Header.ScanWarnings = []model.ScanWarning{{Path: "blocked", Operation: "enumerate"}}
	before.Header.IgnoreConfig.Rules = []string{"generated/"}
	after := snapshot("after", 2, []model.SnapshotEntry{entry("Blocked/a", model.EntryFile, 1, 1), entry("generated/a", model.EntryFile, 1, 1)})
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

func TestLargeDiffOrderingGroupsAndSortsConcurrently(t *testing.T) {
	const count = 12_000
	changes := []model.ChangeType{model.ChangeModified, model.ChangeAdded, model.ChangeRemoved}
	entries := make([]model.DiffEntry, count)
	for index := range count {
		path := fmt.Sprintf("folder/file%d.txt", count-index)
		entries[index] = model.DiffEntry{Path: path, DisplayPath: path, Change: changes[index%len(changes)]}
	}
	orderDiffEntries(entries)
	rank := func(change model.ChangeType) int {
		switch change {
		case model.ChangeAdded:
			return 0
		case model.ChangeRemoved:
			return 1
		default:
			return 2
		}
	}
	for index := 1; index < len(entries); index++ {
		previousRank, currentRank := rank(entries[index-1].Change), rank(entries[index].Change)
		if previousRank > currentRank {
			t.Fatalf("change groups out of order at %d: %s before %s", index, entries[index-1].Change, entries[index].Change)
		}
		if previousRank == currentRank && naturalPathLess(entries[index].DisplayPath, entries[index-1].DisplayPath) {
			t.Fatalf("paths out of natural order at %d: %q before %q", index, entries[index-1].DisplayPath, entries[index].DisplayPath)
		}
	}
}

func TestEntryArenaPointersRemainStableAcrossChunks(t *testing.T) {
	var arena entryArena
	const count = 3_000
	entries := make([]*model.SnapshotEntry, count)
	for index := range count {
		path := fmt.Sprintf("file-%d", index)
		entries[index] = arena.copy(entry(path, model.EntryFile, int64(index), 10))
	}
	for index, item := range entries {
		want := fmt.Sprintf("file-%d", index)
		if item.RelativePath != want || item.Size != int64(index) {
			t.Fatalf("copy %d changed: %+v", index, item)
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
