package ui

import (
	"testing"

	"foldersnap/internal/model"
)

func TestDiffTreeUsesExpandableParentsAndSortsFilesBySize(t *testing.T) {
	entries := []model.DiffEntry{
		{DisplayPath: "docs/nested/small.txt", Change: model.ChangeAdded, After: &model.SnapshotEntry{Type: model.EntryFile, Size: 1}},
		{DisplayPath: "docs/nested/large.txt", Change: model.ChangeAdded, After: &model.SnapshotEntry{Type: model.EntryFile, Size: 100}},
		{DisplayPath: "readme.txt", Change: model.ChangeAdded, After: &model.SnapshotEntry{Type: model.EntryFile, Size: 10}},
	}
	collapsed := makeDiffTreeView(entries, nil, "", "", map[string]bool{})
	if collapsed.Changes != 3 || len(collapsed.Rows) != 2 || collapsed.Rows[0].Name != "docs" || !collapsed.Rows[0].CanExpand {
		t.Fatalf("collapsed tree = %+v", collapsed)
	}
	expanded := makeDiffTreeView(entries, nil, "", "", map[string]bool{"docs": true, "docs/nested": true})
	if len(expanded.Rows) != 5 {
		t.Fatalf("expanded rows = %+v", expanded.Rows)
	}
	if expanded.Rows[2].Name != "large.txt" || expanded.Rows[3].Name != "small.txt" {
		t.Fatalf("files were not sorted largest first: %+v", expanded.Rows)
	}
}

func TestDiffTreeCarriesRecursiveFolderSizes(t *testing.T) {
	entries := []model.DiffEntry{{
		Path: "docs/file.txt", DisplayPath: "Docs/file.txt", Change: model.ChangeModified,
		Before: &model.SnapshotEntry{Type: model.EntryFile, Size: 1},
		After:  &model.SnapshotEntry{Type: model.EntryFile, Size: 2},
	}}
	want := model.FolderSize{BeforeBytes: 100, AfterBytes: 250, BeforePresent: true, AfterPresent: true}
	tree := makeDiffTreeView(entries, map[string]model.FolderSize{"docs": want}, "", "", map[string]bool{})
	if len(tree.Rows) != 1 || tree.Rows[0].FolderSize != want {
		t.Fatalf("folder row = %+v", tree.Rows)
	}
}
