package diff

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

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

	left := sortedEntries(before)
	right := sortedEntries(after)
	beforeWarnings := normalizedWarningPaths(before.Header.ScanWarnings)
	afterWarnings := normalizedWarningPaths(after.Header.ScanWarnings)
	result := model.DiffResult{
		BeforeID: before.Header.SnapshotID, AfterID: after.Header.SnapshotID,
		BeforeWarnings: len(before.Header.ScanWarnings), AfterWarnings: len(after.Header.ScanWarnings),
		IgnoreRulesChanged: strings.Join(before.Header.IgnoreConfig.Rules, "\n") != strings.Join(after.Header.IgnoreConfig.Rules, "\n"),
	}
	var copies entryArena
	var changes diffEntryBuilder
	for i, j := 0, 0; i < len(left) || j < len(right); {
		var entry model.DiffEntry
		switch {
		case i >= len(left):
			entry = onlyAfter(right[j], beforeWarnings, beforeMatcher, &copies)
			j++
		case j >= len(right):
			entry = onlyBefore(left[i], afterWarnings, afterMatcher, &copies)
			i++
		case left[i].RelativePath < right[j].RelativePath:
			entry = onlyBefore(left[i], afterWarnings, afterMatcher, &copies)
			i++
		case left[i].RelativePath > right[j].RelativePath:
			entry = onlyAfter(right[j], beforeWarnings, beforeMatcher, &copies)
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
			entry = model.DiffEntry{Path: a.RelativePath, DisplayPath: a.DisplayPath, Change: change, Subtype: subtype, Before: copies.copy(b), After: copies.copy(a)}
		}
		accumulate(&result.Summary, entry)
		changes.append(entry)
	}
	result.Entries = changes.finish()
	orderDiffEntries(result.Entries)
	result.Summary.NetBytes = after.Header.TotalFileBytes - before.Header.TotalFileBytes
	return result, nil
}

// Scanner output is already path-sorted. Reusing it avoids duplicating two
// complete snapshots during comparison; imported or test snapshots that are
// not sorted still get a defensive shallow copy before sorting.
func sortedEntries(snapshot model.Snapshot) []model.SnapshotEntry {
	entries := snapshot.Entries
	if snapshot.EntriesSorted {
		return entries
	}
	if sort.SliceIsSorted(entries, func(i, j int) bool { return entries[i].RelativePath < entries[j].RelativePath }) {
		return entries
	}
	result := append([]model.SnapshotEntry(nil), entries...)
	sort.Slice(result, func(i, j int) bool { return result[i].RelativePath < result[j].RelativePath })
	return result
}

func onlyAfter(entry model.SnapshotEntry, warnings []string, matcher *ignorepkg.Matcher, copies *entryArena) model.DiffEntry {
	result := model.DiffEntry{Path: entry.RelativePath, DisplayPath: entry.DisplayPath, Change: model.ChangeAdded, After: copies.copy(entry)}
	result.Uncertain = underWarning(entry.RelativePath, warnings)
	result.ScopeDifference = matcher.Match(entry.RelativePath, entry.Type == model.EntryDirectory).Excluded
	return result
}

func onlyBefore(entry model.SnapshotEntry, warnings []string, matcher *ignorepkg.Matcher, copies *entryArena) model.DiffEntry {
	result := model.DiffEntry{Path: entry.RelativePath, DisplayPath: entry.DisplayPath, Change: model.ChangeRemoved, Before: copies.copy(entry)}
	result.Uncertain = underWarning(entry.RelativePath, warnings)
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

func orderDiffEntries(entries []model.DiffEntry) {
	addedEnd := partitionChange(entries, 0, model.ChangeAdded)
	removedEnd := partitionChange(entries, addedEnd, model.ChangeRemoved)
	ranges := [][2]int{{0, addedEnd}, {addedEnd, removedEnd}, {removedEnd, len(entries)}}
	sortRange := func(bounds [2]int) {
		items := entries[bounds[0]:bounds[1]]
		sort.Slice(items, func(i, j int) bool {
			left, right := items[i].DisplayPath, items[j].DisplayPath
			if comparison := naturalPathCompare(left, right); comparison != 0 {
				return comparison < 0
			}
			return left < right
		})
	}
	parallel := len(entries) >= 10_000
	if parallel {
		active := 0
		for _, bounds := range ranges {
			if bounds[1]-bounds[0] > 1 {
				active++
			}
		}
		parallel = active > 1
	}
	if !parallel {
		for _, bounds := range ranges {
			sortRange(bounds)
		}
		return
	}
	var wait sync.WaitGroup
	for _, bounds := range ranges {
		if bounds[1]-bounds[0] <= 1 {
			continue
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			sortRange(bounds)
		}()
	}
	wait.Wait()
}

func partitionChange(entries []model.DiffEntry, start int, change model.ChangeType) int {
	next := start
	for index := start; index < len(entries); index++ {
		if entries[index].Change == change {
			entries[next], entries[index] = entries[index], entries[next]
			next++
		}
	}
	return next
}

// naturalPathLess gives numeric filename components their expected order
// (file2 before file10), matching localizedStandardCompare closely enough for
// persisted Windows relative paths without affecting diff identity matching.
func naturalPathLess(left, right string) bool {
	return naturalPathCompare(left, right) < 0
}

func naturalPathCompare(left, right string) int {
	i, j := 0, 0
	for i < len(left) && j < len(right) {
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
				if len(leftNumber) < len(rightNumber) {
					return -1
				}
				return 1
			}
			if leftNumber != rightNumber {
				if leftNumber < rightNumber {
					return -1
				}
				return 1
			}
			i, j = leftEnd, rightEnd
			continue
		}
		leftRune, leftSize := utf8.DecodeRuneInString(left[i:])
		rightRune, rightSize := utf8.DecodeRuneInString(right[j:])
		leftRune = unicode.ToLower(leftRune)
		rightRune = unicode.ToLower(rightRune)
		if leftRune != rightRune {
			if leftRune < rightRune {
				return -1
			}
			return 1
		}
		i += leftSize
		j += rightSize
		if i == len(left) || j == len(right) {
			switch {
			case i == len(left) && j == len(right):
				return 0
			case i == len(left):
				return -1
			case j == len(right):
				return 1
			}
		}
	}
	switch {
	case i == len(left) && j == len(right):
		return 0
	case i == len(left):
		return -1
	case j == len(right):
		return 1
	default:
		return 0
	}
}

func isDigit(value byte) bool { return value >= '0' && value <= '9' }

func normalizedWarningPaths(warnings []model.ScanWarning) []string {
	paths := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		prefix := strings.Trim(strings.ToLower(strings.ReplaceAll(warning.Path, `\`, "/")), "/")
		paths = append(paths, prefix)
	}
	return paths
}

func underWarning(path string, warnings []string) bool {
	if len(warnings) == 0 {
		return false
	}
	path = strings.ToLower(strings.Trim(path, "/"))
	for _, prefix := range warnings {
		if prefix == "" || path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

type entryArena struct {
	current  []model.SnapshotEntry
	nextSize int
}

type diffEntryBuilder struct {
	chunks   [][]model.DiffEntry
	current  []model.DiffEntry
	nextSize int
	total    int
}

func (b *diffEntryBuilder) append(entry model.DiffEntry) {
	if len(b.current) == cap(b.current) {
		if len(b.current) > 0 {
			b.chunks = append(b.chunks, b.current)
		}
		if b.nextSize == 0 {
			b.nextSize = 1
		} else if b.nextSize < 1024 {
			b.nextSize *= 2
		}
		b.current = make([]model.DiffEntry, 0, b.nextSize)
	}
	b.current = append(b.current, entry)
	b.total++
}

func (b *diffEntryBuilder) finish() []model.DiffEntry {
	if b.total == 0 {
		return nil
	}
	if len(b.chunks) == 0 {
		return b.current
	}
	entries := make([]model.DiffEntry, b.total)
	offset := 0
	for _, chunk := range b.chunks {
		offset += copy(entries[offset:], chunk)
	}
	copy(entries[offset:], b.current)
	return entries
}

func (a *entryArena) copy(entry model.SnapshotEntry) *model.SnapshotEntry {
	if len(a.current) == cap(a.current) {
		if a.nextSize == 0 {
			a.nextSize = 1
		} else if a.nextSize < 1024 {
			a.nextSize *= 2
		}
		a.current = make([]model.SnapshotEntry, 0, a.nextSize)
	}
	a.current = append(a.current, entry)
	return &a.current[len(a.current)-1]
}

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
