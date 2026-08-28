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
	decorateButton(button, variant, nil)
	return button
}

func placedButton(x, y, width, height int, text string, variant buttonVariant) *fltk.Button {
	button := fltk.NewButton(x, y, width, height, text)
	decorateButton(button, variant, nil)
	return button
}

func decorateButton(button *fltk.Button, variant buttonVariant, active func() bool) {
	button.SetBox(fltk.NO_BOX)
	button.SetDownBox(fltk.NO_BOX)
	button.ClearVisibleFocus()
	button.SetLabelFont(fltk.HELVETICA_BOLD)
	button.SetLabelSize(textMeta)
	state := &buttonVisualState{active: active}
	button.SetDrawHandler(func(func()) {
		state.pressed = button.Value()
		drawStyledButton(button, variant, state)
	})
}

func styledTab(text string, selected func() bool) *fltk.Button {
	button := fltk.NewButton(0, 0, 100, 36, text)
	decorateButton(button, buttonTab, selected)
	return button
}

func styleCheckButton(button *fltk.CheckButton) {
	label := button.Label()
	button.SetBox(fltk.NO_BOX)
	button.ClearVisibleFocus()
	button.SetLabel("")
	button.SetDrawHandler(func(func()) {
		x, y, height := button.X(), button.Y(), button.H()
		size := 16
		checkY := y + (height-size)/2
		background, border := colorInput, colorBorder
		if button.Value() {
			background, border = colorAccent, colorAccent
		}
		fltk.DrawBox(fltk.RFLAT_BOX, x, checkY, size, size, background)
		fltk.DrawBox(fltk.ROUNDED_FRAME, x, checkY, size, size, border)
		if button.Value() {
			fltk.DrawCheck(x+2, checkY+2, size-4, size-4, colorText)
		}
		fltk.SetDrawColor(colorText)
		fltk.SetDrawFont(fltk.HELVETICA, textBody)
		fltk.Draw(label, x+space6, y, button.W()-space6, height, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE|fltk.ALIGN_CLIP)
	})
}

func styleInput(input *fltk.Input) {
	input.SetBox(fltk.RFLAT_BOX)
	input.SetColor(colorInput)
	input.SetSelectionColor(colorAccent)
	input.SetLabelColor(colorText)
	input.SetDrawHandler(func(baseDraw func()) {
		baseDraw()
		border := colorBorder
		if input.HasFocus() {
			border = colorAccent
		}
		fltk.DrawBox(fltk.ROUNDED_FRAME, input.X(), input.Y(), input.W(), input.H(), border)
	})
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
	if variant == buttonTab {
		fltk.DrawRectfWithColor(x, y, width, height-8, background)
	} else {
		fltk.DrawBox(fltk.RFLAT_BOX, x, y, width, height, background)
	}
	if variant != buttonGhost && variant != buttonTab {
		fltk.DrawBox(fltk.ROUNDED_FRAME, x, y, width, height, border)
	}
	if variant == buttonTab && active {
		fltk.DrawRectfWithColor(x+space2, y+height-2, width-space4, 2, colorAccent)
	}
	fltk.SetDrawColor(foreground)
	fltk.SetDrawFont(fltk.HELVETICA_BOLD, textMeta)
	textHeight := height
	if variant == buttonTab {
		textHeight = height - 8
	}
	fltk.Draw(button.Label(), x+space2, y, width-space4, textHeight, fltk.ALIGN_CENTER|fltk.ALIGN_INSIDE|fltk.ALIGN_CLIP)
}

func panelBox(color fltk.Color) *fltk.Box {
	box := fltk.NewBox(fltk.FLAT_BOX, 0, 0, 100, 30, "")
	box.SetColor(color)
	return box
}
