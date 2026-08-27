package ui

import "github.com/pwiecz/go-fltk"

const (
	colorCard         fltk.Color = 0x28282800
	colorCardSelected fltk.Color = 0x2A383300
	colorCardBorder   fltk.Color = 0x44444400
	colorBadgeA       fltk.Color = 0x3F8F7400
	colorBadgeB       fltk.Color = 0xA8733900
	colorFolderIcon   fltk.Color = 0xC6964F00
)

type cardListEntry struct {
	button *fltk.Button
	height int
}

// cardList avoids Fl_Browser's fragile multi-column rendering and gives each
// item a real web-style card surface inside a native scroll container.
type cardList struct {
	scroll  *fltk.Scroll
	entries []cardListEntry
	empty   *fltk.Box
	gap     int
	padding int
}

func newCardList(x, y, width, height int) *cardList {
	scroll := fltk.NewScroll(x, y, width, height)
	scroll.SetType(fltk.SCROLL_VERTICAL)
	scroll.SetBox(fltk.FLAT_BOX)
	scroll.SetColor(colorPanel)
	scroll.End()
	list := &cardList{scroll: scroll, gap: 7, padding: 5}
	scroll.SetResizeHandler(list.layout)
	return list
}

func (list *cardList) clear() {
	for _, entry := range list.entries {
		entry.button.Destroy()
	}
	list.entries = nil
	if list.empty != nil {
		list.empty.Destroy()
		list.empty = nil
	}
	list.scroll.ScrollTo(0, 0)
}

func (list *cardList) add(height int, tooltip string, draw func(*fltk.Button), callback func()) {
	list.scroll.Begin()
	button := fltk.NewButton(0, 0, 100, height, "")
	button.SetBox(fltk.NO_BOX)
	button.ClearVisibleFocus()
	button.SetTooltip(tooltip)
	button.SetDrawHandler(func(func()) { draw(button) })
	if callback != nil {
		button.SetCallback(callback)
	}
	list.scroll.End()
	list.entries = append(list.entries, cardListEntry{button: button, height: height})
	list.layout()
}

type changeCardStyle struct {
	Path, Detail, Status string
	Change               string
	Directory            bool
}

func drawChangeCard(button *fltk.Button, card changeCardStyle) {
	x, y, width, height := button.X(), button.Y(), button.W(), button.H()
	accent, background := colorSecondary, colorCard
	switch card.Change {
	case "added":
		accent, background = colorBadgeA, colorAddedCard
	case "removed":
		accent, background = 0xB85B5B00, colorRemovedCard
	case "modified":
		accent, background = 0xC18A4200, colorModifiedCard
	}
	fltk.DrawBox(fltk.RFLAT_BOX, x, y, width, height, background)
	fltk.DrawBox(fltk.ROUNDED_FRAME, x, y, width, height, colorCardBorder)
	fltk.DrawRectfWithColor(x+1, y+9, 3, height-18, accent)

	if card.Directory {
		fltk.DrawRectfWithColor(x+15, y+23, 19, 14, colorFolderIcon)
		fltk.DrawRectfWithColor(x+17, y+19, 9, 5, colorFolderIcon)
	} else {
		fltk.DrawRectfWithColor(x+17, y+16, 15, 21, colorSecondary)
		fltk.DrawRectfWithColor(x+20, y+20, 9, 2, background)
		fltk.DrawRectfWithColor(x+20, y+25, 9, 2, background)
		fltk.DrawRectfWithColor(x+20, y+30, 7, 2, background)
	}

	statusWidth := 92
	textWidth := width - 66 - statusWidth
	fltk.SetDrawColor(colorText)
	fltk.SetDrawFont(fltk.HELVETICA_BOLD, 13)
	fltk.Draw(card.Path, x+43, y+7, textWidth, 22, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE|fltk.ALIGN_CLIP)
	fltk.SetDrawColor(colorSecondary)
	fltk.SetDrawFont(fltk.HELVETICA, 11)
	fltk.Draw(card.Detail, x+43, y+29, textWidth, 18, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE|fltk.ALIGN_CLIP)
	fltk.SetDrawColor(accent)
	fltk.SetDrawFont(fltk.HELVETICA_BOLD, 11)
	fltk.Draw(card.Status, x+width-statusWidth-14, y, statusWidth, height, fltk.ALIGN_RIGHT|fltk.ALIGN_INSIDE|fltk.ALIGN_CLIP)
}

func (list *cardList) showEmpty(message string) {
	list.scroll.Begin()
	box := fltk.NewBox(fltk.NO_BOX, 0, 0, 100, 80, message)
	box.SetLabelColor(colorSecondary)
	box.SetLabelSize(12)
	box.SetAlign(fltk.ALIGN_CENTER | fltk.ALIGN_INSIDE | fltk.ALIGN_WRAP)
	list.scroll.End()
	list.empty = box
	list.layout()
}

func (list *cardList) layout() {
	x := list.scroll.X() + list.padding
	y := list.scroll.Y() + list.padding
	totalHeight := list.padding * 2
	for _, entry := range list.entries {
		totalHeight += entry.height + list.gap
	}
	reserveScrollbar := 0
	if totalHeight > list.scroll.H() {
		reserveScrollbar = 18
	}
	width := list.scroll.W() - list.padding*2 - reserveScrollbar
	if width < 80 {
		width = 80
	}
	for _, entry := range list.entries {
		entry.button.Resize(x, y, width, entry.height)
		y += entry.height + list.gap
	}
	if list.empty != nil {
		list.empty.Resize(x+8, list.scroll.Y()+12, width-16, maxInt(70, list.scroll.H()-24))
	}
	list.scroll.Redraw()
}

type folderCardStyle struct {
	Name, Path, Meta string
	Count            int
	Selected         bool
	Unavailable      bool
}

func drawFolderCard(button *fltk.Button, card folderCardStyle) {
	x, y, width, height := button.X(), button.Y(), button.W(), button.H()
	background, border := colorCard, colorCardBorder
	if card.Selected {
		background, border = colorCardSelected, colorAccent
	}
	fltk.DrawBox(fltk.RFLAT_BOX, x, y, width, height, background)
	fltk.DrawBox(fltk.ROUNDED_FRAME, x, y, width, height, border)

	iconColor := colorFolderIcon
	if card.Unavailable {
		iconColor = colorRemovedCard
	}
	fltk.DrawRectfWithColor(x+13, y+21, 22, 16, iconColor)
	fltk.DrawRectfWithColor(x+15, y+17, 10, 5, iconColor)

	badgeWidth := 30
	textWidth := width - 66 - badgeWidth
	fltk.SetDrawColor(colorText)
	fltk.SetDrawFont(fltk.HELVETICA_BOLD, 14)
	fltk.Draw(card.Name, x+44, y+8, textWidth, 20, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE|fltk.ALIGN_CLIP)
	fltk.SetDrawColor(colorSecondary)
	fltk.SetDrawFont(fltk.HELVETICA, 11)
	fltk.Draw(card.Path, x+44, y+28, textWidth, 18, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE|fltk.ALIGN_CLIP)
	fltk.Draw(card.Meta, x+14, y+49, width-28, 17, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE|fltk.ALIGN_CLIP)

	fltk.DrawBox(fltk.RFLAT_BOX, x+width-badgeWidth-12, y+12, badgeWidth, 24, colorRaised)
	fltk.SetDrawColor(colorText)
	fltk.SetDrawFont(fltk.HELVETICA_BOLD, 11)
	fltk.Draw(intString(card.Count), x+width-badgeWidth-12, y+12, badgeWidth, 24, fltk.ALIGN_CENTER|fltk.ALIGN_INSIDE)
}

type snapshotCardStyle struct {
	Date, Details, Note string
	Role                string
	Selected            bool
	Missing             bool
}

func drawSnapshotCard(button *fltk.Button, card snapshotCardStyle) {
	x, y, width, height := button.X(), button.Y(), button.W(), button.H()
	background, border := colorCard, colorCardBorder
	if card.Role != "" {
		background = colorCardSelected
	}
	if card.Selected {
		border = colorAccent
	}
	if card.Missing {
		border = colorRemovedCard
	}
	fltk.DrawBox(fltk.RFLAT_BOX, x, y, width, height, background)
	fltk.DrawBox(fltk.ROUNDED_FRAME, x, y, width, height, border)

	textWidth := width - 76
	fltk.SetDrawColor(colorText)
	fltk.SetDrawFont(fltk.HELVETICA_BOLD, 14)
	fltk.Draw(card.Date, x+14, y+8, textWidth, 21, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE|fltk.ALIGN_CLIP)
	fltk.SetDrawColor(colorSecondary)
	fltk.SetDrawFont(fltk.HELVETICA, 12)
	fltk.Draw(card.Details, x+14, y+30, textWidth, 18, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE|fltk.ALIGN_CLIP)
	if card.Note != "" {
		fltk.SetDrawFont(fltk.HELVETICA, 11)
		fltk.Draw(card.Note, x+14, y+48, textWidth, 16, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE|fltk.ALIGN_CLIP)
	}

	if card.Role != "" {
		badgeColor := colorBadgeA
		if card.Role == "B" {
			badgeColor = colorBadgeB
		}
		badgeSize := 30
		fltk.DrawBox(fltk.OFLAT_BOX, x+width-badgeSize-16, y+(height-badgeSize)/2, badgeSize, badgeSize, badgeColor)
		fltk.SetDrawColor(colorText)
		fltk.SetDrawFont(fltk.HELVETICA_BOLD, 13)
		fltk.Draw(card.Role, x+width-badgeSize-16, y+(height-badgeSize)/2, badgeSize, badgeSize, fltk.ALIGN_CENTER|fltk.ALIGN_INSIDE)
	}
}

func intString(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	position := len(digits)
	for value > 0 {
		position--
		digits[position] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[position:])
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
