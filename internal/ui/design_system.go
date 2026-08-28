package ui

import "github.com/pwiecz/go-fltk"

const (
	space1 = 4
	space2 = 8
	space3 = 12
	space4 = 16
	space6 = 24
	space8 = 32

	textSmall   = 11
	textMeta    = 12
	textBody    = 13
	textHeading = 17
)

const (
	colorWindow       fltk.Color = 0x0E111700
	colorPanel        fltk.Color = 0x161B2400
	colorRaised       fltk.Color = 0x1E253300
	colorInput        fltk.Color = 0x252D3D00
	colorSelection    fltk.Color = 0x273B5400
	colorDivider      fltk.Color = 0x1E253300
	colorBorder       fltk.Color = 0x2D374800
	colorText         fltk.Color = 0xE2E8F000
	colorSecondary    fltk.Color = 0x71809600
	colorDisabled     fltk.Color = 0x3D4A5C00
	colorAccent       fltk.Color = 0x4A90E200
	colorAccentHover  fltk.Color = 0x5BA3F500
	colorAccentPress  fltk.Color = 0x3572B000
	colorAdded        fltk.Color = 0x38A16900
	colorAddedCard    fltk.Color = 0x1A2E2200
	colorRemoved      fltk.Color = 0xE53E3E00
	colorRemovedCard  fltk.Color = 0x2D151500
	colorModified     fltk.Color = 0xD69E2E00
	colorModifiedCard fltk.Color = 0x2D241000
	colorUnchanged    fltk.Color = 0x4A556800
	colorWarning      fltk.Color = 0xDD6B2000
	colorDestructive  fltk.Color = 0xFC444400
	colorRoleB        fltk.Color = 0x5DDBC800
)

type buttonVariant int

const (
	buttonSecondary buttonVariant = iota
	buttonPrimary
	buttonGhost
	buttonDestructive
	buttonTab
)

type buttonVisualState struct {
	hovered bool
	pressed bool
	active  func() bool
}

func styledButton(text string, variant buttonVariant) *fltk.Button {
	button := fltk.NewButton(0, 0, 100, 36, text)
	button.SetBox(fltk.NO_BOX)
	button.SetDownBox(fltk.NO_BOX)
	button.ClearVisibleFocus()
	button.SetLabelFont(fltk.HELVETICA_BOLD)
	button.SetLabelSize(textMeta)
	state := &buttonVisualState{}
	button.SetEventHandler(func(event fltk.Event) bool {
		switch event {
		case fltk.ENTER:
			state.hovered = true
			button.Redraw()
		case fltk.LEAVE:
			state.hovered, state.pressed = false, false
			button.Redraw()
		case fltk.PUSH:
			state.pressed = true
			button.Redraw()
		case fltk.RELEASE:
			state.pressed = false
			button.Redraw()
		}
		return false
	})
	button.SetDrawHandler(func(func()) {
		drawStyledButton(button, variant, state)
	})
	return button
}

func styledTab(text string, selected func() bool) *fltk.Button {
	button := styledButton(text, buttonTab)
	// The draw handler owns this closure for the lifetime of the navigation.
	state := &buttonVisualState{active: selected}
	button.SetEventHandler(func(event fltk.Event) bool {
		switch event {
		case fltk.ENTER:
			state.hovered = true
			button.Redraw()
		case fltk.LEAVE:
			state.hovered, state.pressed = false, false
			button.Redraw()
		case fltk.PUSH:
			state.pressed = true
			button.Redraw()
		case fltk.RELEASE:
			state.pressed = false
			button.Redraw()
		}
		return false
	})
	button.SetDrawHandler(func(func()) { drawStyledButton(button, buttonTab, state) })
	return button
}

func drawStyledButton(button *fltk.Button, variant buttonVariant, state *buttonVisualState) {
	x, y, width, height := button.X(), button.Y(), button.W(), button.H()
	active := state.active != nil && state.active()
	background, border, foreground := colorRaised, colorBorder, colorText
	switch variant {
	case buttonPrimary:
		background, border, foreground = colorAccent, colorAccent, colorText
		if state.hovered {
			background, border = colorAccentHover, colorAccentHover
		}
		if state.pressed {
			background, border = colorAccentPress, colorAccentPress
		}
	case buttonDestructive:
		background, border = colorDestructive, colorDestructive
		if state.hovered {
			background = 0xFF5C5C00
		}
		if state.pressed {
			background = 0xD9363600
		}
	case buttonGhost:
		background, border, foreground = colorPanel, colorPanel, colorSecondary
		if state.hovered || state.pressed {
			background, border, foreground = colorRaised, colorRaised, colorText
		}
	case buttonTab:
		background, border, foreground = colorPanel, colorPanel, colorSecondary
		if state.hovered || state.pressed {
			background, foreground = colorRaised, colorText
		}
		if active {
			background, foreground = colorPanel, colorText
		}
	default:
		background = colorPanel
		if state.hovered {
			background, border = colorRaised, colorAccent
		}
		if state.pressed {
			background = colorInput
		}
	}
	if !button.IsActive() {
		background, border, foreground = colorPanel, colorDivider, colorDisabled
	}
	fltk.DrawBox(fltk.RFLAT_BOX, x, y, width, height, background)
	if variant != buttonGhost && variant != buttonTab {
		fltk.DrawBox(fltk.ROUNDED_FRAME, x, y, width, height, border)
	}
	if variant == buttonTab && active {
		fltk.DrawRectfWithColor(x+space2, y+height-2, width-space4, 2, colorAccent)
	}
	fltk.SetDrawColor(foreground)
	fltk.SetDrawFont(fltk.HELVETICA_BOLD, textMeta)
	fltk.Draw(button.Label(), x+space2, y, width-space4, height, fltk.ALIGN_CENTER|fltk.ALIGN_INSIDE|fltk.ALIGN_CLIP)
}

func panelBox(color fltk.Color) *fltk.Box {
	box := fltk.NewBox(fltk.FLAT_BOX, 0, 0, 100, 30, "")
	box.SetColor(color)
	return box
}
