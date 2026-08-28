package ui

import (
	"fmt"
	"testing"

	"foldersnap/internal/model"
)

func TestDiffViewPagePaginatesAndClamps(t *testing.T) {
	entries := make([]model.DiffEntry, 0, 253)
	for index := range 251 {
		entries = append(entries, model.DiffEntry{DisplayPath: fmt.Sprintf("file-%03d.txt", index), Change: model.ChangeAdded})
	}
	entries = append(entries,
		model.DiffEntry{DisplayPath: "unchanged", Change: model.ChangeUnchanged},
		model.DiffEntry{DisplayPath: "uncertain", Change: model.ChangeAdded, Uncertain: true},
	)
	first := makeDiffViewPage(entries, "", "", 0, 200)
	if first.Total != 251 || first.Start != 0 || first.End != 200 || len(first.Entries) != 200 {
		t.Fatalf("first page = %+v", first)
	}
	last := makeDiffViewPage(entries, "", "", 99, 200)
	if last.Page != 1 || last.Start != 200 || last.End != 251 || len(last.Entries) != 51 {
		t.Fatalf("clamped last page = %+v", last)
	}
}

func TestDiffViewPageFiltersAndSearchesCaseInsensitively(t *testing.T) {
	entries := []model.DiffEntry{
		{DisplayPath: "Docs/Report.TXT", Change: model.ChangeAdded},
		{DisplayPath: "docs/old.txt", Change: model.ChangeRemoved},
		{DisplayPath: "image.png", Change: model.ChangeAdded},
	}
	page := makeDiffViewPage(entries, model.ChangeAdded, " report ", 0, 200)
	if page.Total != 1 || len(page.Entries) != 1 || page.Entries[0].DisplayPath != "Docs/Report.TXT" {
		t.Fatalf("filtered page = %+v", page)
	}
}
