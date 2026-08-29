package ui

import (
	"testing"

	"foldersnap/internal/cleanup"
	"foldersnap/internal/model"
)

func cleanupTestCandidates() []cleanup.Candidate {
	return []cleanup.Candidate{
		{Path: "NewFolder", Entry: model.SnapshotEntry{Type: model.EntryDirectory}},
		{Path: "NewFolder/one.txt", Entry: model.SnapshotEntry{Type: model.EntryFile, Size: 10}},
		{Path: "NewFolder/Sub", Entry: model.SnapshotEntry{Type: model.EntryDirectory}},
		{Path: "NewFolder/Sub/two.txt", Entry: model.SnapshotEntry{Type: model.EntryFile, Size: 20}},
		{Path: "other.txt", Entry: model.SnapshotEntry{Type: model.EntryFile, Size: 5}},
	}
}

func TestCleanupSelectionStartsEmptyAndDirectoryPropagates(t *testing.T) {
	selection := newCleanupSelection(cleanupTestCandidates())
	if count, size := selection.stats(); count != 0 || size != 0 {
		t.Fatalf("initial stats = %d, %d", count, size)
	}
	selection.toggle(0)
	if count, size := selection.stats(); count != 4 || size != 30 {
		t.Fatalf("selected directory stats = %d, %d", count, size)
	}
	if selection.state(0) != cleanupChecked {
		t.Fatalf("directory state = %v, want checked", selection.state(0))
	}
	if selection.selected[4] {
		t.Fatal("unrelated item was selected")
	}
}

func TestCleanupSelectionClearsAncestorsAndShowsIndeterminate(t *testing.T) {
	selection := newCleanupSelection(cleanupTestCandidates())
	selection.toggle(0)
	selection.toggle(3)
	if selection.selected[0] || selection.selected[2] {
		t.Fatal("a partially selected directory remained eligible for deletion")
	}
	if selection.state(0) != cleanupIndeterminate || selection.state(2) != cleanupUnchecked {
		t.Fatalf("states = root %v, sub %v", selection.state(0), selection.state(2))
	}
	if count, size := selection.stats(); count != 1 || size != 10 {
		t.Fatalf("partial stats = %d, %d", count, size)
	}
}

func TestCleanupSelectionHierarchyIsCaseInsensitive(t *testing.T) {
	selection := newCleanupSelection(cleanupTestCandidates())
	selection.toggle(0)
	if !selection.selected[1] || !selection.selected[3] {
		t.Fatal("mixed-case directory did not select its descendants")
	}
}

func TestCleanupCandidateMatchesNameAndPath(t *testing.T) {
	candidate := cleanup.Candidate{Path: "NewFolder/report.txt", Entry: model.SnapshotEntry{DisplayPath: "NewFolder/report.txt"}}
	for _, query := range []string{"report", "newfolder/", "TXT"} {
		if !cleanupCandidateMatches(candidate, query) {
			t.Fatalf("query %q did not match candidate", query)
		}
	}
	if cleanupCandidateMatches(candidate, "missing") {
		t.Fatal("non-matching query returned true")
	}
}
