package ui

import (
	"github.com/pwiecz/go-fltk"
)

type folderRowStyle struct {
	RootID                             string
	Name, Path, LastSnapshot, Schedule string
	SnapshotCount                      int
	Selected, Unavailable, Archived    bool
}

type folderListEntry struct {
	group      *fltk.Group
	background *fltk.Box
	surface    *fltk.Button
	history    *fltk.Button
	settings   *fltk.Button
	card       *folderRowStyle
	height     int
}

type folderList struct {
	scroll  *fltk.Scroll
	entries []folderListEntry
	empty   *fltk.Box
	gap     int
	padding int
}

func newFolderList(x, y, width, height int) *folderList {
	scroll := fltk.NewScroll(x, y, width, height)
	scroll.SetType(fltk.SCROLL_VERTICAL)
	scroll.SetBox(fltk.NO_BOX)
	scroll.SetColor(colorWindow)
	scroll.End()
	list := &folderList{scroll: scroll, gap: space3, padding: space6}
	scroll.SetResizeHandler(list.layout)
	return list
}

func (list *folderList) clear() {
	for _, entry := range list.entries {
		entry.group.Destroy()
	}
	list.entries = nil
	if list.empty != nil {
		list.empty.Destroy()
		list.empty = nil
	}
	list.scroll.ScrollTo(0, 0)
}

func (list *folderList) add(card folderRowStyle, openHistory, openSettings func()) {
	cardState := card
	list.scroll.Begin()
	group := fltk.NewGroup(0, 0, 100, 86)
	background := fltk.NewBox(fltk.NO_BOX, 0, 0, 100, 86, "")
	surface := fltk.NewButton(0, 0, 100, 86, "")
	surface.SetBox(fltk.NO_BOX)
	surface.ClearVisibleFocus()
	surface.SetTooltip(card.Path)
	hovered := false
	pressed := false
	surface.SetEventHandler(func(event fltk.Event) bool {
		switch event {
		case fltk.ENTER:
			hovered = true
			surface.Redraw()
		case fltk.LEAVE:
			hovered, pressed = false, false
			surface.Redraw()
		case fltk.PUSH:
			pressed = true
			surface.Redraw()
		case fltk.RELEASE:
			pressed = false
			surface.Redraw()
		}
		return false
	})
	background.SetDrawHandler(func(func()) {
		drawFolderRow(background.X(), background.Y(), background.W(), background.H(), cardState, hovered, pressed)
	})
	surface.SetDrawHandler(func(func()) {})
	if openHistory != nil {
		surface.SetCallback(openHistory)
	}
	history := styledButton("History", buttonSecondary)
	history.SetTooltip("Open snapshot history")
	if openHistory != nil {
		history.SetCallback(openHistory)
	}
	settings := styledButton("•••", buttonGhost)
	settings.SetTooltip("Folder settings")
	if openSettings != nil {
		settings.SetCallback(openSettings)
	}
	group.End()
	list.scroll.End()
	list.entries = append(list.entries, folderListEntry{group: group, background: background, surface: surface, history: history, settings: settings, card: &cardState, height: 86})
}

// update refreshes rows without replacing their native widgets. Destroying the
// button that delivered the current click leaves FLTK's process-global pushed
// widget pointing at a dead control, which suppresses later release callbacks.
func (list *folderList) update(cards []folderRowStyle) bool {
	if len(cards) != len(list.entries) {
		return false
	}
	for index, card := range cards {
		if list.entries[index].card == nil || list.entries[index].card.RootID != card.RootID {
			return false
		}
	}
	for index, card := range cards {
		*list.entries[index].card = card
		list.entries[index].background.Redraw()
	}
	return true
}

func (list *folderList) selectRoot(rootID string) {
	for index := range list.entries {
		entry := &list.entries[index]
		if entry.card == nil {
			continue
		}
		selected := entry.card.RootID == rootID
		if entry.card.Selected != selected {
			entry.card.Selected = selected
			entry.background.Redraw()
		}
	}
}

func (list *folderList) finishBatch() { list.layout() }

func (list *folderList) showEmpty() {
	list.scroll.Begin()
	box := fltk.NewBox(fltk.NO_BOX, 0, 0, 100, 220, "No watched folders\n\nAdd a folder to begin building snapshot history.")
	box.SetLabelFont(fltk.HELVETICA)
	box.SetLabelSize(textBody)
	box.SetLabelColor(colorSecondary)
	box.SetAlign(fltk.ALIGN_CENTER | fltk.ALIGN_INSIDE | fltk.ALIGN_WRAP)
	list.scroll.End()
	list.empty = box
	list.layout()
}

func (list *folderList) layout() {
	x := list.scroll.X() + list.padding
	y := list.scroll.Y() + space1
	totalHeight := list.padding + space1
	for _, entry := range list.entries {
		totalHeight += entry.height + list.gap
	}
	reserveScrollbar := 0
	if totalHeight > list.scroll.H() {
		reserveScrollbar = 18
	}
	width := list.scroll.W() - list.padding*2 - reserveScrollbar
	if width < 620 {
		width = 620
	}
	for _, entry := range list.entries {
		entry.group.Resize(x, y, width, entry.height)
		entry.background.Resize(x, y, width, entry.height)
		entry.surface.Resize(x, y, width-148, entry.height)
		entry.history.Resize(x+width-132, y+27, 76, 32)
		entry.settings.Resize(x+width-48, y+27, 32, 32)
		y += entry.height + list.gap
	}
	if list.empty != nil {
		list.empty.Resize(x, list.scroll.Y()+space8, width, maxInt(220, list.scroll.H()-space8*2))
	}
	list.scroll.Redraw()
}

func drawFolderRow(x, y, width, height int, card folderRowStyle, hovered, pressed bool) {
	background, border := colorPanel, colorDivider
	if hovered {
		background, border = colorRaised, colorBorder
	}
	if pressed {
		background = colorInput
	}
	if card.Selected {
		border = colorAccent
	}
	drawRoundedFill(x, y, width, height, radiusLarge, background)
	drawRoundedFrame(x, y, width, height, radiusLarge, border)

	statusColor := colorAdded
	if card.Unavailable || card.Archived {
		statusColor = colorWarning
	}
	fltk.SetDrawColor(statusColor)
	fltk.DrawPie(x+space4, y+height/2-4, 8, 8, 0, 360)

	iconX, iconY := x+space8, y+21
	drawRoundedFill(iconX, iconY, 44, 44, 10, colorRaised)
	fltk.DrawRectfWithColor(iconX+11, iconY+17, 23, 16, 0xD6A65700)
	fltk.DrawRectfWithColor(iconX+13, iconY+13, 11, 6, 0xD6A65700)

	contentX := iconX + 58
	// Keep the three metadata columns and the History action on one rhythm.
	// In particular, the schedule pill has the same 16 px gap on both sides.
	actionsWidth := 148
	statsWidth := 310
	textWidth := width - (contentX - x) - statsWidth - actionsWidth
	if textWidth < 180 {
		textWidth = 180
	}
	fltk.SetDrawColor(colorText)
	fltk.SetDrawFont(fltk.HELVETICA_BOLD, 15)
	fltk.Draw(card.Name, contentX, y+14, textWidth, 24, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE|fltk.ALIGN_CLIP)
	fltk.SetDrawColor(colorSecondary)
	fltk.SetDrawFont(fltk.COURIER, textSmall)
	fltk.Draw(card.Path, contentX, y+38, textWidth, 22, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE|fltk.ALIGN_CLIP)

	statsX := x + width - actionsWidth - statsWidth
	drawFolderStat(statsX, y+17, 84, intString(card.SnapshotCount), "SNAPSHOTS")
	drawFolderStat(statsX+100, y+17, 100, card.LastSnapshot, "LAST SNAP")
	drawSchedulePill(statsX+224, y+29, 86, card.Schedule, card.Archived)
}

func drawFolderStat(x, y, width int, value, caption string) {
	fltk.SetDrawColor(colorText)
	fltk.SetDrawFont(fltk.HELVETICA_BOLD, 14)
	fltk.Draw(value, x, y, width, 22, fltk.ALIGN_RIGHT|fltk.ALIGN_INSIDE|fltk.ALIGN_CLIP)
	fltk.SetDrawColor(colorSecondary)
	fltk.SetDrawFont(fltk.HELVETICA_BOLD, 10)
	fltk.Draw(caption, x, y+23, width, 17, fltk.ALIGN_RIGHT|fltk.ALIGN_INSIDE|fltk.ALIGN_CLIP)
}

func drawSchedulePill(x, y, width int, value string, muted bool) {
	background, border, foreground := colorSelection, colorAccent, colorAccentHover
	if muted {
		background, border, foreground = colorInput, colorBorder, colorSecondary
	}
	drawRoundedFill(x, y, width, 26, 13, background)
	drawRoundedFrame(x, y, width, 26, 13, border)
	fltk.SetDrawColor(foreground)
	fltk.SetDrawFont(fltk.HELVETICA_BOLD, 10)
	fltk.Draw(value, x+space1, y, width-space2, 26, fltk.ALIGN_CENTER|fltk.ALIGN_INSIDE|fltk.ALIGN_CLIP)
}
