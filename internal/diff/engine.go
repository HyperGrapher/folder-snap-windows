package diff

import (
	"errors"
	"sort"
	"strings"

	ignorepkg "foldersnap/internal/ignore"
	"foldersnap/internal/model"
)

type Engine struct{}

func (Engine) Compare(before, after model.Snapshot) (model.DiffResult, error) {
	if before.Header.RootID == "" || before.Header.RootID != after.Header.RootID {
		return model.DiffResult{}, errors.New("snapshots must belong to the same root")
	}
	if before.Header.SnapshotID == after.Header.SnapshotID {
		return model.DiffResult{}, errors.New("snapshots must be different")
	}
	if after.Header.CompletedAtUTC.Before(before.Header.CompletedAtUTC) {
		before, after = after, before
	}
	beforeMatcher, err := ignorepkg.Compile(before.Header.IgnoreConfig.Rules)
	if err != nil {
		return model.DiffResult{}, err
	}
	afterMatcher, err := ignorepkg.Compile(after.Header.IgnoreConfig.Rules)
	if err != nil {
		return model.DiffResult{}, err
	}

	left := sortedEntries(before.Entries)
	right := sortedEntries(after.Entries)
	result := model.DiffResult{
		BeforeID: before.Header.SnapshotID, AfterID: after.Header.SnapshotID,
		BeforeWarnings: len(before.Header.ScanWarnings), AfterWarnings: len(after.Header.ScanWarnings),
		IgnoreRulesChanged: strings.Join(before.Header.IgnoreConfig.Rules, "\n") != strings.Join(after.Header.IgnoreConfig.Rules, "\n"),
	}
	for i, j := 0, 0; i < len(left) || j < len(right); {
		var entry model.DiffEntry
		switch {
		case i >= len(left):
			entry = onlyAfter(right[j], before, beforeMatcher)
			j++
		case j >= len(right):
			entry = onlyBefore(left[i], after, afterMatcher)
			i++
		case left[i].RelativePath < right[j].RelativePath:
			entry = onlyBefore(left[i], after, afterMatcher)
			i++
		case left[i].RelativePath > right[j].RelativePath:
			entry = onlyAfter(right[j], before, beforeMatcher)
			j++
		default:
			b, a := left[i], right[j]
			change, subtype := compareEntries(b, a)
			i++
			j++
			if change == model.ChangeUnchanged {
				result.Summary.Unchanged++
				continue
			}
			entry = model.DiffEntry{Path: a.RelativePath, DisplayPath: a.DisplayPath, Change: change, Subtype: subtype, Before: copyEntry(b), After: copyEntry(a)}
		}
		accumulate(&result.Summary, entry)
		result.Entries = append(result.Entries, entry)
	}
	sort.SliceStable(result.Entries, func(i, j int) bool {
		leftRank, rightRank := changeRank(result.Entries[i].Change), changeRank(result.Entries[j].Change)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return naturalPathLess(result.Entries[i].DisplayPath, result.Entries[j].DisplayPath)
	})
	result.Summary.NetBytes = after.Header.TotalFileBytes - before.Header.TotalFileBytes
	return result, nil
}

// Scanner output is already path-sorted. Reusing it avoids duplicating two
// complete snapshots during comparison; imported or test snapshots that are
// not sorted still get a defensive shallow copy before sorting.
func sortedEntries(entries []model.SnapshotEntry) []model.SnapshotEntry {
	if sort.SliceIsSorted(entries, func(i, j int) bool { return entries[i].RelativePath < entries[j].RelativePath }) {
		return entries
	}
	result := append([]model.SnapshotEntry(nil), entries...)
	sort.Slice(result, func(i, j int) bool { return result[i].RelativePath < result[j].RelativePath })
	return result
}

func onlyAfter(entry model.SnapshotEntry, before model.Snapshot, matcher *ignorepkg.Matcher) model.DiffEntry {
	result := model.DiffEntry{Path: entry.RelativePath, DisplayPath: entry.DisplayPath, Change: model.ChangeAdded, After: copyEntry(entry)}
	result.Uncertain = underWarning(entry.RelativePath, before.Header.ScanWarnings)
	result.ScopeDifference = matcher.Match(entry.RelativePath, entry.Type == model.EntryDirectory).Excluded
	return result
}

func onlyBefore(entry model.SnapshotEntry, after model.Snapshot, matcher *ignorepkg.Matcher) model.DiffEntry {
	result := model.DiffEntry{Path: entry.RelativePath, DisplayPath: entry.DisplayPath, Change: model.ChangeRemoved, Before: copyEntry(entry)}
	result.Uncertain = underWarning(entry.RelativePath, after.Header.ScanWarnings)
	result.ScopeDifference = matcher.Match(entry.RelativePath, entry.Type == model.EntryDirectory).Excluded
	return result
}

func compareEntries(before, after model.SnapshotEntry) (model.ChangeType, string) {
	if before.Type != after.Type {
		return model.ChangeModified, "type_changed"
	}
	switch before.Type {
	case model.EntryDirectory:
		return model.ChangeUnchanged, ""
	case model.EntryFile:
		if before.Size != after.Size || before.ModifiedUnixNs != after.ModifiedUnixNs {
			return model.ChangeModified, ""
		}
	case model.EntryReparse:
		if before.LinkTarget != after.LinkTarget || before.Attributes != after.Attributes {
			return model.ChangeModified, ""
		}
	default:
		if before.ModifiedUnixNs != after.ModifiedUnixNs || before.Attributes != after.Attributes {
			return model.ChangeModified, ""
		}
	}
	return model.ChangeUnchanged, ""
}

func changeRank(change model.ChangeType) int {
	switch change {
	case model.ChangeAdded:
		return 0
	case model.ChangeRemoved:
		return 1
	case model.ChangeModified:
		return 2
	default:
		return 3
	}
}

// naturalPathLess gives numeric filename components their expected order
// (file2 before file10), matching localizedStandardCompare closely enough for
// persisted Windows relative paths without affecting diff identity matching.
func naturalPathLess(left, right string) bool {
	left, right = strings.ToLower(left), strings.ToLower(right)
	for i, j := 0, 0; i < len(left) && j < len(right); {
		if isDigit(left[i]) && isDigit(right[j]) {
			leftEnd, rightEnd := i, j
			for leftEnd < len(left) && isDigit(left[leftEnd]) {
				leftEnd++
			}
			for rightEnd < len(right) && isDigit(right[rightEnd]) {
				rightEnd++
			}
			leftNumber := strings.TrimLeft(left[i:leftEnd], "0")
			rightNumber := strings.TrimLeft(right[j:rightEnd], "0")
			if leftNumber == "" {
				leftNumber = "0"
			}
			if rightNumber == "" {
				rightNumber = "0"
			}
			if len(leftNumber) != len(rightNumber) {
				return len(leftNumber) < len(rightNumber)
			}
			if leftNumber != rightNumber {
				return leftNumber < rightNumber
			}
			i, j = leftEnd, rightEnd
			continue
		}
		if left[i] != right[j] {
			return left[i] < right[j]
		}
		i++
		j++
		if i == len(left) || j == len(right) {
			return len(left) < len(right)
		}
	}
	return len(left) < len(right)
}

func isDigit(value byte) bool { return value >= '0' && value <= '9' }

func underWarning(path string, warnings []model.ScanWarning) bool {
	path = strings.Trim(path, "/")
	for _, warning := range warnings {
		prefix := strings.Trim(strings.ToLower(strings.ReplaceAll(warning.Path, `\`, "/")), "/")
		if prefix == "" || path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func copyEntry(entry model.SnapshotEntry) *model.SnapshotEntry { value := entry; return &value }

func accumulate(summary *model.DiffSummary, entry model.DiffEntry) {
	if entry.Uncertain {
		summary.Uncertain++
		return
	}
	if entry.ScopeDifference {
		summary.ScopeDifference++
		return
	}
	switch entry.Change {
	case model.ChangeAdded:
		summary.Added++
		if entry.After != nil && entry.After.Type == model.EntryFile {
			summary.AddedBytes += entry.After.Size
		}
	case model.ChangeRemoved:
		summary.Removed++
		if entry.Before != nil && entry.Before.Type == model.EntryFile {
			summary.RemovedBytes += entry.Before.Size
		}
	case model.ChangeModified:
		summary.Modified++
		if entry.Before != nil && entry.Before.Type == model.EntryFile {
			summary.ModifiedBeforeBytes += entry.Before.Size
		}
		if entry.After != nil && entry.After.Type == model.EntryFile {
			summary.ModifiedAfterBytes += entry.After.Size
		}
	case model.ChangeUnchanged:
		summary.Unchanged++
	}
}
