package ui

import (
	"strings"

	"foldersnap/internal/model"
)

type diffViewPage struct {
	Entries     []model.DiffEntry
	Page, Total int
	Start, End  int
}

func makeDiffViewPage(entries []model.DiffEntry, filter model.ChangeType, query string, requestedPage, pageSize int) diffViewPage {
	if pageSize < 1 {
		pageSize = 1
	}
	query = strings.ToLower(strings.TrimSpace(query))
	matches := func(item model.DiffEntry) bool {
		if item.Uncertain || item.ScopeDifference || item.Change == model.ChangeUnchanged {
			return false
		}
		if filter != "" && item.Change != filter {
			return false
		}
		return query == "" || strings.Contains(strings.ToLower(item.DisplayPath), query)
	}
	total := 0
	for _, item := range entries {
		if matches(item) {
			total++
		}
	}
	pageCount := (total + pageSize - 1) / pageSize
	page := max(0, requestedPage)
	if pageCount == 0 {
		page = 0
	} else if page >= pageCount {
		page = pageCount - 1
	}
	start := page * pageSize
	end := min(start+pageSize, total)
	result := diffViewPage{Page: page, Total: total, Start: start, End: end, Entries: make([]model.DiffEntry, 0, end-start)}
	matched := 0
	for _, item := range entries {
		if !matches(item) {
			continue
		}
		if matched >= start && matched < end {
			result.Entries = append(result.Entries, item)
		}
		matched++
		if matched >= end {
			break
		}
	}
	return result
}
