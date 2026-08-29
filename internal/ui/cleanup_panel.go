package ui

import (
	"path"
	"strings"

	"foldersnap/internal/cleanup"
	"foldersnap/internal/model"

	"github.com/pwiecz/go-fltk"
)

type cleanupCheckState uint8

const (
	cleanupUnchecked cleanupCheckState = iota
	cleanupChecked
	cleanupIndeterminate
)

// cleanupSelection is deliberately independent of FLTK so selection and
// directory propagation can be tested without a display server.
type cleanupSelection struct {
	candidates []cleanup.Candidate
	selected   []bool
}

func newCleanupSelection(candidates []cleanup.Candidate) *cleanupSelection {
	return &cleanupSelection{
		candidates: append([]cleanup.Candidate(nil), candidates...),
		selected:   make([]bool, len(candidates)), // safety: always start empty
	}
}

func (s *cleanupSelection) toggle(index int) {
	if index < 0 || index >= len(s.candidates) {
		return
	}
	checked := !s.selected[index]
	s.selected[index] = checked
	if s.candidates[index].Entry.Type == model.EntryDirectory {
		prefix := cleanupPathKey(s.candidates[index].Path) + "/"
		for child := index + 1; child < len(s.candidates); child++ {
			candidatePath := cleanupPathKey(s.candidates[child].Path)
			if strings.HasPrefix(candidatePath, prefix) {
				s.selected[child] = checked
			}
		}
	}
	if !checked {
		s.clearSelectedAncestors(index)
	}
}

func (s *cleanupSelection) clearSelectedAncestors(index int) {
	childPath := cleanupPathKey(s.candidates[index].Path)
	for candidate := 0; candidate < len(s.candidates); candidate++ {
		if s.candidates[candidate].Entry.Type != model.EntryDirectory {
			continue
		}
		if strings.HasPrefix(childPath, cleanupPathKey(s.candidates[candidate].Path)+"/") {
			s.selected[candidate] = false
		}
	}
}

func (s *cleanupSelection) setAll(checked bool) {
	for index := range s.selected {
		s.selected[index] = checked
	}
}

func (s *cleanupSelection) state(index int) cleanupCheckState {
	if index < 0 || index >= len(s.candidates) {
		return cleanupUnchecked
	}
	if s.candidates[index].Entry.Type != model.EntryDirectory {
		if s.selected[index] {
			return cleanupChecked
		}
		return cleanupUnchecked
	}
	prefix := cleanupPathKey(s.candidates[index].Path) + "/"
	checked, unchecked := s.selected[index], !s.selected[index]
	for child := index + 1; child < len(s.candidates); child++ {
		if !strings.HasPrefix(cleanupPathKey(s.candidates[child].Path), prefix) {
			continue
		}
		if s.selected[child] {
			checked = true
		} else {
			unchecked = true
		}
	}
	if checked && unchecked {
		return cleanupIndeterminate
	}
	if checked {
		return cleanupChecked
	}
	return cleanupUnchecked
}

func (s *cleanupSelection) chosen() []cleanup.Candidate {
	result := make([]cleanup.Candidate, 0)
	for index, candidate := range s.candidates {
		if s.selected[index] {
			result = append(result, candidate)
		}
	}
	return result
}

func (s *cleanupSelection) stats() (count int, bytes int64) {
	for index, candidate := range s.candidates {
		if !s.selected[index] {
			continue
		}
		count++
		if candidate.Entry.Type == model.EntryFile {
			bytes += candidate.Entry.Size
		}
	}
	return count, bytes
}

func cleanupPathKey(value string) string {
	return strings.ToLower(strings.Trim(strings.ReplaceAll(value, `\`, "/"), "/"))
}

type cleanupPanelList struct {
	table          *fltk.TableRow
	selection      *cleanupSelection
	visibleIndices []int
	onChange       func()
}

func newCleanupPanelList(x, y, width, height int, onChange func()) *cleanupPanelList {
	list := &cleanupPanelList{onChange: onChange, selection: newCleanupSelection(nil)}
	list.table = fltk.NewTableRow(x, y, width, height)
	list.table.SetBox(fltk.NO_BOX)
	list.table.SetColor(colorPanel)
	list.table.DisableColumnHeaders()
	list.table.DisableRowHeaders()
	list.table.DisallowColumnResizing()
	list.table.DisallowRowResizing()
	list.table.SetColumnCount(1)
	list.table.SetColumnWidth(0, width-20)
	list.table.SetRowHeightAll(54)
	list.table.SetScrollbarSize(12)
	list.table.SetType(fltk.SelectSingle)
	list.table.SetDrawCellCallback(list.drawCell)
	// Fl_Table_Row can invoke its widget callback both when its native row
	// selection changes and again on release. The checkbox state is separate
	// from that native selection, so using the callback toggled it twice for a
	// single click. Handle exactly one RELEASE event instead and consume it
	// after updating our model.
	list.table.SetEventHandler(func(event fltk.Event) bool {
		if event != fltk.RELEASE || fltk.EventButton() != fltk.LeftMouse {
			return false
		}
		row, column := list.table.RowAndColumnFromCursor()
		if row < 0 || row >= len(list.visibleIndices) || column != 0 {
			return false
		}
		list.selection.toggle(list.visibleIndices[row])
		list.table.Redraw()
		if list.onChange != nil {
			list.onChange()
		}
		return true
	})
	list.table.End()
	return list
}

func (l *cleanupPanelList) setCandidates(candidates []cleanup.Candidate) {
	l.selection = newCleanupSelection(candidates)
	l.visibleIndices = make([]int, len(candidates))
	for index := range candidates {
		l.visibleIndices[index] = index
	}
	l.table.SetRowCount(len(l.visibleIndices))
	// FLTK only applies row_height_all to rows that already exist. Applying it
	// before SetRowCount leaves newly populated rows at the cramped theme
	// default and vertically clips their icons and metadata.
	l.table.SetRowHeightAll(54)
	l.table.SetTopRow(0)
	l.table.Redraw()
}

func (l *cleanupPanelList) setFilter(query string) {
	query = strings.ToLower(strings.TrimSpace(query))
	previous := l.selection
	l.visibleIndices = l.visibleIndices[:0]
	for index, candidate := range previous.candidates {
		if query == "" || cleanupCandidateMatches(candidate, query) {
			l.visibleIndices = append(l.visibleIndices, index)
		}
	}
	l.table.SetRowCount(len(l.visibleIndices))
	l.table.SetTopRow(0)
	l.table.Redraw()
}

func (l *cleanupPanelList) setAll(checked bool) {
	l.selection.setAll(checked)
	l.table.Redraw()
	if l.onChange != nil {
		l.onChange()
	}
}

func (l *cleanupPanelList) drawCell(context fltk.TableContext, row, _ int, x, y, width, height int) {
	if context == fltk.ContextTable && len(l.visibleIndices) == 0 {
		fltk.DrawRectfWithColor(x, y, width, height, colorPanel)
		fltk.SetDrawColor(colorSecondary)
		fltk.SetDrawFont(fltk.HELVETICA, textBody)
		fltk.Draw("No Added items match this filter.", x, y, width, height, fltk.ALIGN_CENTER|fltk.ALIGN_INSIDE)
		return
	}
	if context != fltk.ContextCell || row < 0 || row >= len(l.visibleIndices) {
		return
	}
	index := l.visibleIndices[row]
	candidate := l.selection.candidates[index]
	background := colorPanel
	if row%2 == 1 {
		background = colorWindow
	}
	fltk.DrawRectfWithColor(x, y, width, height, background)
	fltk.DrawRectfWithColor(x, y+height-1, width, 1, colorDivider)

	depth := strings.Count(strings.Trim(candidate.Path, "/"), "/")
	if depth > 8 {
		depth = 8
	}
	checkX := x + space4 + depth*space4
	checkY := y + (height-16)/2
	drawCleanupCheckbox(checkX, checkY, l.selection.state(index))

	iconX := checkX + 28
	iconY := y + (height-19)/2
	if candidate.Entry.Type == model.EntryDirectory {
		drawCleanupFolderIcon(iconX, iconY)
	} else {
		drawCleanupFileIcon(iconX, iconY)
	}

	textX := iconX + 28
	display := candidate.Entry.DisplayPath
	if display == "" {
		display = candidate.Path
	}
	name := path.Base(strings.ReplaceAll(display, `\`, "/"))
	meta := candidate.Path
	if candidate.Entry.Type == model.EntryFile {
		meta += "  ·  " + formatBytes(candidate.Entry.Size)
	} else {
		meta += "  ·  Folder"
	}
	textTop := y + (height-40)/2
	fltk.PushClip(textX, y, width-(textX-x)-space4, height)
	fltk.SetDrawColor(colorText)
	fltk.SetDrawFont(fltk.HELVETICA_BOLD, textBody)
	fltk.Draw(name, textX, textTop, width-(textX-x)-space4, 22, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE|fltk.ALIGN_CLIP)
	fltk.SetDrawColor(colorSecondary)
	fltk.SetDrawFont(fltk.HELVETICA, textSmall)
	fltk.Draw(meta, textX, textTop+22, width-(textX-x)-space4, 18, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE|fltk.ALIGN_CLIP)
	fltk.PopClip()
}

func cleanupCandidateMatches(candidate cleanup.Candidate, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	name := candidate.Entry.DisplayPath
	if name == "" {
		name = candidate.Path
	}
	return strings.Contains(strings.ToLower(name), query) || strings.Contains(strings.ToLower(candidate.Path), query)
}

func drawCleanupCheckbox(x, y int, state cleanupCheckState) {
	background, border := colorInput, colorBorder
	if state != cleanupUnchecked {
		background, border = colorAccent, colorAccent
	}
	drawRoundedFill(x, y, 16, 16, 4, background)
	drawRoundedFrame(x, y, 16, 16, 4, border)
	if state == cleanupChecked {
		fltk.DrawCheck(x+2, y+2, 12, 12, colorText)
	} else if state == cleanupIndeterminate {
		fltk.DrawRectfWithColor(x+4, y+7, 8, 2, colorText)
	}
}

func drawCleanupFolderIcon(x, y int) {
	fltk.DrawRectfWithColor(x+1, y+4, 20, 14, 0xD4A64A00)
	fltk.DrawRectfWithColor(x+3, y+1, 9, 5, 0xD4A64A00)
}

func drawCleanupFileIcon(x, y int) {
	drawRoundedFill(x+3, y, 15, 19, 2, colorSecondary)
	fltk.DrawRectfWithColor(x+6, y+5, 9, 1, colorPanel)
	fltk.DrawRectfWithColor(x+6, y+9, 9, 1, colorPanel)
}
