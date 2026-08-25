package ui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"foldersnap/internal/app"
	"foldersnap/internal/cleanup"
	ignorepkg "foldersnap/internal/ignore"
	"foldersnap/internal/model"
	platform "foldersnap/internal/platform/windows"

	"github.com/pwiecz/go-fltk"
)

const WindowTitle = "FolderSnap"

const (
	colorWindow       fltk.Color = 0x17171700
	colorPanel        fltk.Color = 0x20202000
	colorRaised       fltk.Color = 0x2B2B2B00
	colorInput        fltk.Color = 0x29292900
	colorSelection    fltk.Color = 0x3A3A3A00
	colorDivider      fltk.Color = 0x38383800
	colorText         fltk.Color = 0xF1F1F100
	colorSecondary    fltk.Color = 0xB8B8B800
	colorAccent       fltk.Color = 0x34775E00
	colorAddedCard    fltk.Color = 0x21312800
	colorRemovedCard  fltk.Color = 0x38242400
	colorModifiedCard fltk.Color = 0x392F2000
)

type App struct {
	service *app.Service
	window  *fltk.Window
	status  *fltk.Box

	rootsBrowser    *fltk.HoldBrowser
	timelineBrowser *fltk.HoldBrowser
	changesBrowser  *fltk.HoldBrowser
	search          *fltk.Input
	summary         *fltk.Box
	addedSummary    *fltk.Box
	removedSummary  *fltk.Box
	modifiedSummary *fltk.Box
	deltaSummary    *fltk.Box
	filterButtons   map[model.ChangeType]*fltk.Button
	snapshotButton  *fltk.Button
	compareButton   *fltk.Button
	editButton      *fltk.Button
	warningsButton  *fltk.Button
	deleteButton    *fltk.Button

	roots              []model.WatchedRoot
	timeline           []model.SnapshotRecord
	selectedRootID     string
	selectedSnapshotID string
	beforeID           string
	afterID            string
	compareVersion     uint64
	compareState       comparisonState
	diff               model.DiffResult
	filter             model.ChangeType
	background         bool
	quitting           bool
	notify             func(string, string)
}

type comparisonState string

const (
	comparisonIdle    comparisonState = "idle"
	comparisonLoading comparisonState = "loading"
	comparisonDone    comparisonState = "done"
	comparisonError   comparisonState = "error"
)

func New(service *app.Service, background bool) *App {
	fltk.InitStyles()
	fltk.SetScheme("gtk+")
	applyTheme()
	u := &App{service: service, background: background}
	u.build()
	u.bindEvents()
	u.refreshRoots()
	return u
}

func (u *App) Run() int {
	if !u.background {
		u.Show()
	}
	for !u.quitting {
		fltk.Wait()
	}
	return 0
}

func (u *App) Show()              { u.window.Show(); u.window.TakeFocus() }
func (u *App) Hide()              { u.window.Hide() }
func (u *App) RawHandle() uintptr { return u.window.RawHandle() }
func (u *App) Quit() {
	u.quitting = true
	u.service.Close()
	u.window.Hide()
	fltk.AwakeNullMessage()
}
func (u *App) TriggerSnapshot()                        { u.snapshotNow() }
func (u *App) ShowSettings()                           { u.Show(); u.showSettings() }
func (u *App) SetNotifier(notify func(string, string)) { u.notify = notify }

func (u *App) build() {
	u.window = fltk.NewWindow(1200, 760, WindowTitle)
	u.window.SetColor(colorWindow)
	u.window.SetSizeRange(980, 640, 0, 0, 0, 0, false)

	outer := fltk.NewFlex(0, 0, 1200, 760)
	outer.SetType(fltk.COLUMN)
	outer.SetGap(1)
	outer.SetColor(colorDivider)
	top := fltk.NewFlex(0, 0, 1200, 58)
	top.SetType(fltk.ROW)
	top.SetMargin(12, 9)
	top.SetColor(colorPanel)
	title := label("FolderSnap", 20, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE)
	u.status = label("Ready", 12, fltk.ALIGN_RIGHT|fltk.ALIGN_INSIDE)
	add := button("Add Folder")
	u.snapshotButton = button("Snapshot Now")
	u.snapshotButton.Deactivate()
	settings := button("Settings")
	top.Fixed(title, 180)
	top.Fixed(u.status, 360)
	top.Fixed(add, 110)
	top.Fixed(u.snapshotButton, 130)
	top.Fixed(settings, 90)
	top.End()
	outer.Fixed(top, 58)

	content := fltk.NewFlex(0, 59, 1200, 701)
	content.SetType(fltk.ROW)
	content.SetGap(1)
	content.SetColor(colorDivider)
	left := fltk.NewFlex(0, 59, 265, 701)
	left.SetType(fltk.COLUMN)
	left.SetMargin(14, 12)
	left.SetColor(colorPanel)
	leftTitle := label("FOLDERS", 14, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE)
	leftSubtitle := label("Folders available for snapshot history", 11, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE)
	leftSubtitle.SetLabelColor(colorSecondary)
	u.rootsBrowser = fltk.NewHoldBrowser(0, 0, 265, 590)
	u.rootsBrowser.SetColumnChar('\t')
	u.rootsBrowser.SetColumnWidths(142, 66, 0)
	styleWidget(u.rootsBrowser)
	rootSettings := button("Folder Settings")
	left.Fixed(leftTitle, 25)
	left.Fixed(leftSubtitle, 30)
	left.Fixed(rootSettings, 38)
	left.End()
	content.Fixed(left, 270)

	right := fltk.NewFlex(271, 59, 929, 701)
	right.SetType(fltk.COLUMN)
	right.SetMargin(16, 12)
	right.SetGap(8)
	right.SetColor(colorPanel)

	snapshotHeader := fltk.NewFlex(0, 0, 929, 48)
	snapshotHeader.SetType(fltk.ROW)
	snapshotTitle := label("SNAPSHOTS", 14, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE)
	snapshotHint := label("Select A and B, then compare", 12, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE)
	snapshotHint.SetLabelColor(colorSecondary)
	u.compareButton = button("Compare A → B")
	u.compareButton.SetColor(colorAccent)
	u.compareButton.Deactivate()
	snapshotHeader.Fixed(snapshotTitle, 105)
	snapshotHeader.Fixed(u.compareButton, 145)
	snapshotHeader.End()

	u.timelineBrowser = fltk.NewHoldBrowser(0, 0, 929, 176)
	u.timelineBrowser.SetColumnChar('\t')
	u.timelineBrowser.SetColumnWidths(48, 140, 92, 96, 96, 0)
	styleWidget(u.timelineBrowser)
	snapshotActions := fltk.NewFlex(0, 0, 929, 34)
	snapshotActions.SetType(fltk.ROW)
	snapshotActions.SetGap(8)
	u.editButton = button("Edit Description")
	u.warningsButton = button("Warnings")
	u.deleteButton = button("Delete Snapshot")
	u.editButton.Deactivate()
	u.warningsButton.Deactivate()
	u.deleteButton.Deactivate()
	snapshotActions.Fixed(u.editButton, 140)
	snapshotActions.Fixed(u.warningsButton, 100)
	snapshotActions.Fixed(u.deleteButton, 130)
	snapshotActions.End()

	u.summary = label("Pick two snapshots", 15, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE)
	u.summary.SetLabelColor(colorSecondary)
	metrics := fltk.NewFlex(0, 0, 929, 62)
	metrics.SetType(fltk.ROW)
	metrics.SetGap(8)
	u.addedSummary = metricCard("ADDED\n—", colorAddedCard)
	u.removedSummary = metricCard("REMOVED\n—", colorRemovedCard)
	u.modifiedSummary = metricCard("MODIFIED\n—", colorModifiedCard)
	u.deltaSummary = metricCard("SIZE DELTA\n—", colorRaised)
	metrics.End()
	controls := fltk.NewFlex(0, 0, 929, 38)
	controls.SetType(fltk.ROW)
	controls.SetGap(6)
	all, added := button("All"), button("Added")
	removed, modified := button("Removed"), button("Modified")
	u.filterButtons = map[model.ChangeType]*fltk.Button{
		"":                   all,
		model.ChangeAdded:    added,
		model.ChangeRemoved:  removed,
		model.ChangeModified: modified,
	}
	controls.Fixed(all, 62)
	controls.Fixed(added, 72)
	controls.Fixed(removed, 82)
	controls.Fixed(modified, 82)
	_ = fltk.NewBox(fltk.NO_BOX, 0, 0, 20, 34, "")
	searchLabel := label("Search", 12, fltk.ALIGN_RIGHT|fltk.ALIGN_INSIDE)
	searchLabel.SetLabelColor(colorSecondary)
	u.search = fltk.NewInput(0, 0, 220, 34, "")
	u.search.SetTooltip("Search files by name or relative path")
	styleWidget(u.search)
	u.search.SetColor(colorInput)
	controls.Fixed(searchLabel, 56)
	controls.Fixed(u.search, 240)
	controls.End()
	u.changesBrowser = fltk.NewHoldBrowser(0, 0, 929, 310)
	u.changesBrowser.SetColumnChar('\t')
	u.changesBrowser.SetColumnWidths(88, 470, 0)
	styleWidget(u.changesBrowser)
	right.Fixed(snapshotHeader, 48)
	right.Fixed(u.timelineBrowser, 176)
	right.Fixed(snapshotActions, 34)
	right.Fixed(u.summary, 40)
	right.Fixed(metrics, 62)
	right.Fixed(controls, 38)
	right.End()
	content.End()
	outer.End()
	u.window.Resizable(outer)
	u.window.End()

	add.SetCallback(u.addFolder)
	rootSettings.SetCallback(u.showRootSettings)
	u.snapshotButton.SetCallback(u.snapshotNow)
	u.rootsBrowser.SetCallback(u.selectRoot)
	u.timelineBrowser.SetCallback(u.selectTimelineSnapshot)
	u.compareButton.SetCallback(u.compareSelected)
	u.editButton.SetCallback(u.editSnapshotDescription)
	u.warningsButton.SetCallback(u.viewSnapshotWarnings)
	u.deleteButton.SetCallback(u.deleteSelectedSnapshot)
	all.SetCallback(func() { u.setFilter("") })
	added.SetCallback(func() { u.setFilter(model.ChangeAdded) })
	removed.SetCallback(func() { u.setFilter(model.ChangeRemoved) })
	modified.SetCallback(func() { u.setFilter(model.ChangeModified) })
	u.search.SetCallbackCondition(fltk.WhenChanged)
	u.search.SetCallback(func() {
		if u.compareState == comparisonDone {
			u.renderDiff()
		}
	})
	settings.SetCallback(u.showSettings)
	u.window.SetCallback(func() {
		if u.service.Config().CloseToTray {
			u.Hide()
			return
		}
		u.Quit()
	})
	u.renderFilterState()
	u.showIdleComparison("Select a folder and pick two snapshots.")
}

func (u *App) bindEvents() {
	go func() {
		for event := range u.service.Events() {
			event := event
			fltk.Awake(func() { u.handleEvent(event) })
		}
	}()
}

func (u *App) handleEvent(event app.Event) {
	switch event.Type {
	case app.EventScanStarted:
		u.status.SetLabel("Scanning...")
	case app.EventScanProgress:
		u.status.SetLabel(fmt.Sprintf("Scanning... %d items", event.Items))
	case app.EventScanFailed:
		u.status.SetLabel("Snapshot failed")
		if event.Trigger == model.TriggerScheduled && u.notify != nil {
			u.notify("FolderSnap snapshot failed", event.Err.Error())
		} else {
			fltk.MessageBox("Snapshot failed", event.Err.Error())
		}
		u.snapshotButton.Activate()
	case app.EventScanComplete:
		u.status.SetLabel("Snapshot saved")
		if event.Trigger == model.TriggerScheduled && u.service.Config().NotifyScheduledSuccess && u.notify != nil {
			u.notify("FolderSnap", "Scheduled snapshot saved successfully.")
		}
		u.snapshotButton.Activate()
		u.refreshRoots()
		if event.RootID == u.selectedRootID {
			u.loadTimeline()
		}
	case app.EventConfigChange:
		u.refreshRoots()
	}
}

func (u *App) refreshRoots() {
	u.roots = u.service.Config().Roots
	app.SortRoots(u.roots)
	u.rootsBrowser.Clear()
	selectedLine := 0
	for i, root := range u.roots {
		state := ""
		if root.Archived {
			state = " [Archived]"
		} else if root.LastScanError != "" {
			state = " [Unavailable]"
		}
		records, _ := u.service.ListSnapshots(root.RootID)
		count := fmt.Sprintf("%d saved", len(records))
		last := "—"
		if len(records) > 0 {
			last = records[0].CompletedAtUTC.Local().Format("02 Jan 15:04")
		}
		u.rootsBrowser.AddWithData(root.DisplayName+state+"\t"+count+"\t"+last, root.RootID)
		if root.RootID == u.selectedRootID {
			selectedLine = i + 1
		}
	}
	if selectedLine > 0 {
		u.rootsBrowser.SetValue(selectedLine)
	}
	if u.selectedRootID == "" && len(u.roots) > 0 {
		u.rootsBrowser.SetValue(1)
		u.selectRoot()
	}
}

func (u *App) addFolder() {
	chooser := fltk.NewNativeFileChooser()
	defer chooser.Destroy()
	chooser.SetTitle("Add watched folder")
	chooser.SetType(fltk.NativeFileChooser_BROWSE_DIRECTORY)
	if chooser.Show() != 0 || len(chooser.Filenames()) == 0 {
		return
	}
	root, err := u.service.AddRoot(chooser.Filenames()[0])
	if err != nil {
		fltk.MessageBox("Cannot add folder", err.Error())
		return
	}
	u.selectedRootID = root.RootID
	u.beforeID, u.afterID = "", ""
	u.selectedSnapshotID = ""
	u.refreshRoots()
	u.loadTimeline()
	if fltk.ChoiceDialog("Folder added. Take the first snapshot now?", "Not now", "Take Snapshot") == 1 {
		_ = u.service.RequestSnapshot(root.RootID, model.TriggerManual)
	}
}

func (u *App) selectRoot() {
	line := u.rootsBrowser.Value()
	if line < 1 || line > len(u.roots) {
		return
	}
	u.selectedRootID = u.roots[line-1].RootID
	u.beforeID, u.afterID = "", ""
	u.selectedSnapshotID = ""
	u.filter = ""
	u.search.SetValue("")
	u.snapshotButton.Activate()
	if u.roots[line-1].Archived {
		u.snapshotButton.Deactivate()
	}
	u.showIdleComparison("Pick two snapshots from this folder.")
	u.loadTimeline()
}

func (u *App) loadTimeline() {
	records, err := u.service.ListSnapshots(u.selectedRootID)
	if err != nil {
		u.showComparisonError("History unavailable: " + err.Error())
		return
	}
	u.timeline = append([]model.SnapshotRecord(nil), records...)
	u.applyPair(u.currentPair().reconcile(u.timeline))
	if _, ok := u.snapshotRecord(u.selectedSnapshotID); !ok {
		u.selectedSnapshotID = ""
	}
	u.renderTimeline()
	u.updateCompareButton()
	if u.compareState == comparisonDone && u.diff.BeforeID == u.beforeID && u.diff.AfterID == u.afterID {
		u.renderDiff()
		return
	}
	if u.compareState == comparisonLoading {
		return
	}
	switch {
	case len(u.timeline) == 0:
		u.showIdleComparison("No snapshots yet. Create the first snapshot for this folder.")
	case len(u.timeline) == 1:
		u.showIdleComparison("One snapshot saved. Create another before comparing.")
	case u.beforeID == "" && u.afterID == "":
		u.showIdleComparison("Pick two snapshots. The first is A and the second is B.")
	case u.beforeID == "" || u.afterID == "":
		u.showIdleComparison("Pick one more snapshot to complete the A/B pair.")
	default:
		u.showIdleComparison("A and B are ready. Press Compare to calculate changes.")
	}
}

func (u *App) renderTimeline() {
	u.timelineBrowser.Clear()
	selectedLine := 0
	for i, snapshot := range u.timeline {
		role := ""
		if snapshot.SnapshotID == u.beforeID {
			role = "A"
		} else if snapshot.SnapshotID == u.afterID {
			role = "B"
		}
		note := snapshot.Description
		if !snapshot.PayloadAvailable {
			if note != "" {
				note += " · "
			}
			note += "payload missing"
		}
		if snapshot.WarningCount > 0 {
			if note != "" {
				note += " · "
			}
			note += fmt.Sprintf("%d warning(s)", snapshot.WarningCount)
		}
		u.timelineBrowser.AddWithData(
			role+"\t"+snapshot.CompletedAtUTC.Local().Format("02 Jan 2006 15:04")+
				"\t"+fmt.Sprintf("%d files", snapshot.FileCount)+
				"\t"+fmt.Sprintf("%d folders", snapshot.DirectoryCount)+
				"\t"+formatBytes(snapshot.TotalFileBytes)+"\t"+note,
			snapshot.SnapshotID,
		)
		if snapshot.SnapshotID == u.selectedSnapshotID {
			selectedLine = i + 1
		}
	}
	if selectedLine > 0 {
		u.timelineBrowser.SetValue(selectedLine)
	}
	u.updateSnapshotActions()
}

// selectTimelineSnapshot mirrors the macOS history interaction: the first
// click selects A, the second selects B, and a third rolls the pair
// forward. Clicking an assigned snapshot removes it from the pair.
func (u *App) selectTimelineSnapshot() {
	line := u.timelineBrowser.Value()
	if line < 1 || line > len(u.timeline) {
		return
	}
	id := u.timeline[line-1].SnapshotID
	u.selectedSnapshotID = id
	u.applyPair(u.currentPair().selectSnapshot(id, u.timeline))
	u.renderTimeline()
	u.updateCompareButton()
	if u.beforeID != "" && u.afterID != "" {
		u.showIdleComparison("A and B are ready. Press Compare to calculate changes.")
	} else if u.beforeID != "" || u.afterID != "" {
		u.showIdleComparison("Pick one more snapshot to complete the A/B pair.")
	} else {
		u.showIdleComparison("Pick two snapshots. The first is A and the second is B.")
	}
}

func (u *App) currentPair() snapshotPair {
	return snapshotPair{BeforeID: u.beforeID, AfterID: u.afterID}
}

func (u *App) applyPair(pair snapshotPair) {
	u.beforeID, u.afterID = pair.BeforeID, pair.AfterID
}

func (u *App) snapshotRecord(snapshotID string) (model.SnapshotRecord, bool) {
	for _, record := range u.timeline {
		if record.SnapshotID == snapshotID {
			return record, true
		}
	}
	return model.SnapshotRecord{}, false
}

func (u *App) selectedSnapshot() (model.SnapshotRecord, bool) {
	return u.snapshotRecord(u.selectedSnapshotID)
}

func (u *App) updateSnapshotActions() {
	if _, ok := u.selectedSnapshot(); ok {
		u.editButton.Activate()
		u.warningsButton.Activate()
		u.deleteButton.Activate()
		return
	}
	u.editButton.Deactivate()
	u.warningsButton.Deactivate()
	u.deleteButton.Deactivate()
}

func (u *App) compareSelected() {
	if u.beforeID == "" || u.afterID == "" || u.beforeID == u.afterID {
		return
	}
	rootID, beforeID, afterID := u.selectedRootID, u.beforeID, u.afterID
	u.compareVersion++
	version := u.compareVersion
	u.compareState = comparisonLoading
	u.diff = model.DiffResult{}
	u.summary.SetLabel("Loading snapshots and comparing files...")
	u.setMetricPlaceholders()
	u.changesBrowser.Clear()
	u.changesBrowser.Add("\tWorking...")
	u.compareButton.Deactivate()
	go func() {
		before, err := u.service.LoadSnapshot(rootID, beforeID)
		if err == nil {
			after, loadErr := u.service.LoadSnapshot(rootID, afterID)
			err = loadErr
			if err == nil {
				result, compareErr := u.service.Compare(before, after)
				err = compareErr
				if err == nil {
					fltk.Awake(func() {
						if u.selectedRootID == rootID && u.beforeID == beforeID && u.afterID == afterID && u.compareVersion == version {
							u.beforeID, u.afterID, u.diff = result.BeforeID, result.AfterID, result
							u.compareState = comparisonDone
							u.renderDiff()
							u.updateCompareButton()
						}
					})
					return
				}
			}
		}
		fltk.Awake(func() {
			if u.selectedRootID == rootID && u.beforeID == beforeID && u.afterID == afterID && u.compareVersion == version {
				u.showComparisonError("Comparison failed: " + err.Error())
				u.updateCompareButton()
			}
		})
	}()
}

func (u *App) renderDiff() {
	s := u.diff.Summary
	contextParts := make([]string, 0, 3)
	if s.Uncertain > 0 || s.ScopeDifference > 0 {
		contextParts = append(contextParts, fmt.Sprintf("%d incomplete result(s)", s.Uncertain+s.ScopeDifference))
	}
	if u.diff.BeforeWarnings+u.diff.AfterWarnings > 0 {
		contextParts = append(contextParts, fmt.Sprintf("%d scan warning(s)", u.diff.BeforeWarnings+u.diff.AfterWarnings))
	}
	if u.diff.IgnoreRulesChanged {
		contextParts = append(contextParts, "exclusions changed")
	}
	title := u.comparisonTitle()
	if len(contextParts) > 0 {
		title += "  ·  " + strings.Join(contextParts, "  ·  ")
	}
	u.summary.SetLabel(title)
	u.addedSummary.SetLabel(fmt.Sprintf("ADDED\n%d", s.Added))
	u.removedSummary.SetLabel(fmt.Sprintf("REMOVED\n%d", s.Removed))
	u.modifiedSummary.SetLabel(fmt.Sprintf("MODIFIED\n%d", s.Modified))
	u.deltaSummary.SetLabel("SIZE DELTA\n" + formatBytes(s.NetBytes))
	u.renderFilterState()
	query := strings.ToLower(strings.TrimSpace(u.search.Value()))
	u.changesBrowser.Clear()
	visible := 0
	for _, item := range u.diff.Entries {
		if item.Uncertain || item.ScopeDifference || item.Change == model.ChangeUnchanged {
			continue
		}
		if u.filter != "" && item.Change != u.filter {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(item.DisplayPath), query) {
			continue
		}
		marker := map[model.ChangeType]string{model.ChangeAdded: "Added", model.ChangeRemoved: "Removed", model.ChangeModified: "Modified"}[item.Change]
		u.changesBrowser.Add(marker + "\t" + item.DisplayPath + "\t" + diffEntryDetail(item))
		visible++
	}
	if visible == 0 {
		message := "No changed items in this comparison."
		if query != "" || u.filter != "" {
			message = "No changes match the current filter."
		}
		u.changesBrowser.Add("\t" + message)
	}
}

func (u *App) comparisonTitle() string {
	var before, after time.Time
	for _, snapshot := range u.timeline {
		switch snapshot.SnapshotID {
		case u.diff.BeforeID:
			before = snapshot.CompletedAtUTC
		case u.diff.AfterID:
			after = snapshot.CompletedAtUTC
		}
	}
	if before.IsZero() || after.IsZero() {
		return "Snapshot comparison"
	}
	return fmt.Sprintf("Comparing %s  →  %s", before.Local().Format("02 Jan 15:04"), after.Local().Format("02 Jan 15:04"))
}

func (u *App) showIdleComparison(message string) {
	u.compareVersion++
	u.compareState = comparisonIdle
	u.diff = model.DiffResult{}
	u.summary.SetLabel(message)
	u.setMetricPlaceholders()
	u.changesBrowser.Clear()
	u.renderFilterState()
	u.updateCompareButton()
}

func (u *App) showComparisonError(message string) {
	u.compareVersion++
	u.compareState = comparisonError
	u.diff = model.DiffResult{}
	u.summary.SetLabel(message)
	u.setMetricPlaceholders()
	u.changesBrowser.Clear()
	u.changesBrowser.Add("\t" + message)
	u.renderFilterState()
}

func (u *App) setMetricPlaceholders() {
	u.addedSummary.SetLabel("ADDED\n—")
	u.removedSummary.SetLabel("REMOVED\n—")
	u.modifiedSummary.SetLabel("MODIFIED\n—")
	u.deltaSummary.SetLabel("SIZE DELTA\n—")
}

func (u *App) updateCompareButton() {
	if u.beforeID != "" && u.afterID != "" && u.beforeID != u.afterID && u.compareState != comparisonLoading {
		u.compareButton.Activate()
	} else {
		u.compareButton.Deactivate()
	}
}

func (u *App) setFilter(filter model.ChangeType) {
	u.filter = filter
	if u.compareState == comparisonDone {
		u.renderDiff()
	} else {
		u.renderFilterState()
	}
}

func (u *App) renderFilterState() {
	for filter, value := range u.filterButtons {
		if filter == u.filter {
			value.SetColor(colorAccent)
			value.SetSelectionColor(colorAccent)
		} else {
			value.SetColor(colorRaised)
			value.SetSelectionColor(colorSelection)
		}
		value.Redraw()
	}
}

func (u *App) snapshotNow() {
	if u.selectedRootID == "" {
		return
	}
	u.snapshotButton.Deactivate()
	if err := u.service.RequestSnapshot(u.selectedRootID, model.TriggerManual); err != nil {
		u.snapshotButton.Activate()
		fltk.MessageBox("Cannot take snapshot", err.Error())
	}
}

func (u *App) reviewCleanup() {
	candidates := cleanup.Plan(u.diff)
	if len(candidates) == 0 {
		return
	}
	win := newModalWindow(760, 560, "Review Added Items for Cleanup")
	list := fltk.NewCheckBrowser(16, 48, 728, 440)
	styleWidget(list)
	states := make([]bool, len(candidates))
	for _, item := range candidates {
		list.Add(strings.Repeat("  ", strings.Count(item.Path, "/"))+item.Entry.DisplayPath, false)
	}
	list.SetCallback(func() {
		for i := range candidates {
			checked := list.IsChecked(i + 1)
			if checked == states[i] {
				continue
			}
			states[i] = checked
			if candidates[i].Entry.Type == model.EntryDirectory {
				prefix := candidates[i].Path + "/"
				for j := range candidates {
					if strings.HasPrefix(candidates[j].Path, prefix) {
						states[j] = checked
						list.SetChecked(j+1, checked)
					}
				}
			}
			break
		}
	})
	message := fltk.NewBox(fltk.NO_BOX, 16, 12, 728, 28, "Nothing is selected by default. Current files are revalidated before removal.")
	message.SetAlign(fltk.ALIGN_LEFT | fltk.ALIGN_INSIDE)
	cancel := fltk.NewButton(492, 504, 110, 38, "Cancel")
	preflight := fltk.NewButton(612, 504, 132, 38, "Run Preflight")
	accepted := false
	cancel.SetCallback(func() { win.Hide() })
	preflight.SetCallback(func() { accepted = true; win.Hide() })
	win.SetModal()
	win.End()
	win.Show()
	for win.IsShown() {
		fltk.Wait()
	}
	if !accepted {
		return
	}
	selected := make([]cleanup.Candidate, 0)
	for i, candidate := range candidates {
		if states[i] {
			selected = append(selected, candidate)
		}
	}
	if len(selected) == 0 {
		fltk.MessageBox("Nothing selected", "Select at least one Added item.")
		return
	}
	root, ok := u.service.Root(u.selectedRootID)
	if !ok {
		return
	}
	u.status.SetLabel("Validating cleanup selection...")
	go u.runCleanup(root, selected)
}

func (u *App) runCleanup(root model.WatchedRoot, selected []cleanup.Candidate) {
	service := cleanup.Service{RootPath: root.Path, Recycler: platform.RecycleBin{}}
	preflight := service.Preflight(context.Background(), selected)
	ready, blocked, missing := 0, 0, 0
	for _, item := range preflight {
		switch item.Status {
		case cleanup.StatusReady:
			ready++
		case cleanup.StatusAlreadyMissing:
			missing++
		default:
			blocked++
		}
	}
	fltk.Awake(func() {
		u.status.SetLabel("Cleanup preflight complete")
		if ready == 0 {
			fltk.MessageBox("Nothing can be removed", fmt.Sprintf("Ready: 0\nBlocked: %d\nAlready missing: %d", blocked, missing))
			return
		}
		if fltk.ChoiceDialog(fmt.Sprintf("Ready to move to Recycle Bin: %d\nBlocked: %d\nAlready missing: %d", ready, blocked, missing), "Cancel", fmt.Sprintf("Move %d items", ready)) != 1 {
			return
		}
		u.status.SetLabel("Moving items to Recycle Bin...")
		go func() {
			results := service.Execute(context.Background(), preflight)
			_ = cleanup.WriteAudit(u.service.DataDir(), root.RootID, u.diff.BeforeID, u.diff.AfterID, results)
			moved, failed := 0, 0
			for _, item := range results {
				if item.Status == cleanup.StatusMoved {
					moved++
				} else if item.Status == cleanup.StatusFailed {
					failed++
				}
			}
			fltk.Awake(func() {
				u.status.SetLabel("Cleanup complete")
				fltk.MessageBox("Cleanup result", fmt.Sprintf("Moved to Recycle Bin: %d\nFailed: %d\n\nHistorical snapshots were not changed. Take a new snapshot to record current state.", moved, failed))
			})
		}()
	})
}

func (u *App) editSnapshotDescription() {
	snapshot, ok := u.selectedSnapshot()
	if !ok {
		return
	}
	value, ok := promptText("Snapshot description", "Plain-text description (maximum 500 characters)", snapshot.Description, 500)
	if !ok {
		return
	}
	if err := u.service.UpdateDescription(u.selectedRootID, snapshot.SnapshotID, value); err != nil {
		fltk.MessageBox("Cannot update description", err.Error())
		return
	}
	u.loadTimeline()
}

func (u *App) viewSnapshotWarnings() {
	record, ok := u.selectedSnapshot()
	if !ok {
		return
	}
	snapshot, err := u.service.LoadSnapshot(u.selectedRootID, record.SnapshotID)
	if err != nil {
		fltk.MessageBox("Cannot load snapshot", err.Error())
		return
	}
	if len(snapshot.Header.ScanWarnings) == 0 {
		fltk.MessageBox("Scan warnings", "This snapshot completed without scan warnings.")
		return
	}
	var lines []string
	for i, warning := range snapshot.Header.ScanWarnings {
		if i == 30 {
			lines = append(lines, fmt.Sprintf("...and %d more", len(snapshot.Header.ScanWarnings)-i))
			break
		}
		lines = append(lines, fmt.Sprintf("%s — %s: %s", warning.Path, warning.Operation, warning.Message))
	}
	fltk.MessageBox("Scan warnings", strings.Join(lines, "\n"))
}

func (u *App) deleteSelectedSnapshot() {
	snapshot, ok := u.selectedSnapshot()
	if !ok {
		return
	}
	message := fmt.Sprintf("Delete FolderSnap history for %s?\n\nThis does not change files in the watched folder.", snapshot.CompletedAtUTC.Local().Format("02 Jan 2006 15:04"))
	if snapshot.Description != "" {
		message += "\n\n" + snapshot.Description
	}
	if fltk.ChoiceDialog(message, "Cancel", "Delete Snapshot") != 1 {
		return
	}
	if err := u.service.DeleteSnapshot(u.selectedRootID, snapshot.SnapshotID); err != nil {
		fltk.MessageBox("Cannot delete snapshot", err.Error())
		return
	}
	if u.beforeID == snapshot.SnapshotID {
		u.beforeID = ""
	}
	if u.afterID == snapshot.SnapshotID {
		u.afterID = ""
	}
	u.selectedSnapshotID = ""
	u.loadTimeline()
}

func (u *App) showRootSettings() {
	root, ok := u.service.Root(u.selectedRootID)
	if !ok {
		fltk.MessageBox("No folder selected", "Select a watched folder first.")
		return
	}
	win := newModalWindow(720, 690, "Folder Settings")
	modalLabel(20, 18, 150, 28, "Display name")
	name := fltk.NewInput(180, 18, 510, 28)
	name.SetValue(root.DisplayName)
	modalLabel(20, 56, 150, 28, "Folder")
	pathOutput := fltk.NewOutput(180, 56, 390, 28)
	pathOutput.SetValue(root.Path)
	openExplorer := fltk.NewButton(580, 56, 110, 28, "Open Explorer")

	modalLabel(20, 104, 150, 28, "Schedule")
	schedule := fltk.NewChoice(180, 104, 220, 28)
	for _, item := range []string{"Manual only", "Every 1 hour", "Every 3 hours", "Every 6 hours", "Every 12 hours", "Daily", "Weekly", "Monthly"} {
		schedule.Add(item, func() {})
	}
	schedule.SetValue(scheduleIndex(root.Schedule))
	modalLabel(420, 104, 90, 28, "Time")
	timeInput := fltk.NewInput(510, 104, 75, 28)
	hour, minute := root.Schedule.Hour, root.Schedule.Minute
	if root.Schedule.Kind == model.ScheduleManual || root.Schedule.Kind == model.ScheduleInterval {
		now := time.Now()
		hour, minute = now.Hour(), now.Minute()
	}
	timeInput.SetValue(fmt.Sprintf("%02d:%02d", hour, minute))
	modalLabel(590, 104, 45, 28, "Day")
	dayInput := fltk.NewIntInput(635, 104, 55, 28)
	day := root.Schedule.DayOfMonth
	if root.Schedule.Kind == model.ScheduleWeekly {
		day = int(root.Schedule.Weekday)
	}
	if day == 0 {
		day = time.Now().Day()
	}
	dayInput.SetValue(strconv.Itoa(day))
	dayInput.SetTooltip("Weekly: 0=Sunday through 6=Saturday. Monthly: 1 through 31.")

	modalLabel(20, 142, 150, 28, "Retention")
	retention := fltk.NewChoice(180, 142, 220, 28)
	for _, item := range []string{"10 snapshots", "25 snapshots", "50 snapshots", "100 snapshots", "Unlimited"} {
		retention.Add(item, func() {})
	}
	retention.SetValue(retentionIndex(root.Retention))

	modalLabel(20, 190, 670, 28, "Exclusions — ordered gitignore-like rules; last matching rule wins")
	buffer := fltk.NewTextBuffer()
	buffer.SetText(strings.Join(root.IgnoreRules, "\n"))
	editor := fltk.NewTextEditor(20, 220, 670, 280)
	editor.SetBuffer(buffer)
	editor.SetTextFont(fltk.COURIER)
	editor.SetTextSize(13)
	editor.SetTextColor(colorText)
	editor.SetColor(colorInput)
	editor.SetSelectionColor(colorAccent)

	modalLabel(20, 510, 100, 28, "Test path")
	testPath := fltk.NewInput(120, 510, 400, 28)
	testResult := modalLabel(530, 510, 160, 28, "")
	testButton := fltk.NewButton(20, 548, 110, 32, "Test Rule")
	archive := fltk.NewButton(140, 548, 150, 32, map[bool]string{true: "Resume Watching", false: "Archive / Stop"}[root.Archived])
	deleteHistory := fltk.NewButton(300, 548, 150, 32, "Delete History...")
	cancel := fltk.NewButton(468, 638, 100, 34, "Cancel")
	save := fltk.NewButton(578, 638, 112, 34, "Save")

	accepted := false
	archiveValue := root.Archived
	openExplorer.SetCallback(func() {
		if err := platform.OpenInExplorer(root.Path); err != nil {
			fltk.MessageBox("Cannot open Explorer", err.Error())
		}
	})
	testButton.SetCallback(func() {
		matcher, err := ignorepkg.Compile(splitRules(buffer.Text()))
		if err != nil {
			testResult.SetLabel("Invalid rules")
			return
		}
		result := matcher.Match(testPath.Value(), strings.HasSuffix(testPath.Value(), "/"))
		if result.Excluded {
			testResult.SetLabel("Excluded: " + result.MatchingRule)
		} else {
			testResult.SetLabel("Included")
		}
	})
	archive.SetCallback(func() {
		archiveValue = !archiveValue
		archive.SetLabel(map[bool]string{true: "Resume Watching", false: "Archive / Stop"}[archiveValue])
	})
	deleteHistory.SetCallback(func() {
		if fltk.ChoiceDialog("Permanently delete all FolderSnap snapshot history for this folder?\n\nThe actual folder and its files are not changed.", "Cancel", "Delete History") != 1 {
			return
		}
		if err := u.service.ClearHistory(root.RootID); err != nil {
			fltk.MessageBox("Cannot delete history", err.Error())
			return
		}
		u.beforeID, u.afterID = "", ""
		u.selectedSnapshotID = ""
		u.showIdleComparison("No snapshots yet. Create the first snapshot for this folder.")
		win.Hide()
		u.loadTimeline()
	})
	cancel.SetCallback(func() { win.Hide() })
	save.SetCallback(func() { accepted = true; win.Hide() })
	win.SetModal()
	win.End()
	win.Show()
	for win.IsShown() {
		fltk.Wait()
	}
	if !accepted {
		buffer.Destroy()
		return
	}
	rules := splitRules(buffer.Text())
	buffer.Destroy()
	if _, err := ignorepkg.Compile(rules); err != nil {
		fltk.MessageBox("Invalid exclusion rules", err.Error())
		return
	}
	parsedSchedule, err := scheduleFromInputs(schedule.Value(), timeInput.Value(), dayInput.Value())
	if err != nil {
		fltk.MessageBox("Invalid schedule", err.Error())
		return
	}
	root.DisplayName = strings.TrimSpace(name.Value())
	if root.DisplayName == "" {
		root.DisplayName = titleCase(filepathBase(root.Path))
	}
	root.Schedule = parsedSchedule
	root.Retention = retentionValue(retention.Value())
	root.IgnoreRules = rules
	root.Archived = archiveValue
	if err := u.service.UpdateRoot(root); err != nil {
		fltk.MessageBox("Cannot save folder settings", err.Error())
		return
	}
	u.refreshRoots()
	u.loadTimeline()
}

func (u *App) showSettings() {
	cfg := u.service.Config()
	win := newModalWindow(600, 620, "FolderSnap Settings")
	startup := fltk.NewCheckButton(24, 24, 550, 30, "Launch FolderSnap when Windows starts")
	startup.SetValue(cfg.LaunchAtStartup)
	notifySuccess := fltk.NewCheckButton(24, 62, 550, 30, "Notify after successful scheduled snapshots")
	notifySuccess.SetValue(cfg.NotifyScheduledSuccess)
	closeTray := fltk.NewCheckButton(24, 100, 550, 30, "Close the main window to the system tray")
	closeTray.SetValue(cfg.CloseToTray)
	modalLabel(24, 148, 180, 28, "Default retention")
	retention := fltk.NewChoice(210, 148, 180, 28)
	for _, item := range []string{"10 snapshots", "25 snapshots", "50 snapshots", "100 snapshots", "Unlimited"} {
		retention.Add(item, func() {})
	}
	retention.SetValue(retentionIndex(cfg.DefaultRetention))
	modalLabel(24, 190, 550, 42, "Data folder:\n"+u.service.DataDir())
	modalLabel(24, 238, 550, 28, "Default exclusions for newly added folders")
	defaultBuffer := fltk.NewTextBuffer()
	defaultBuffer.SetText(strings.Join(cfg.DefaultIgnoreRules, "\n"))
	defaultEditor := fltk.NewTextEditor(24, 268, 552, 210)
	defaultEditor.SetBuffer(defaultBuffer)
	defaultEditor.SetTextFont(fltk.COURIER)
	defaultEditor.SetTextSize(13)
	defaultEditor.SetTextColor(colorText)
	defaultEditor.SetColor(colorInput)
	defaultEditor.SetSelectionColor(colorAccent)
	openData := fltk.NewButton(24, 494, 140, 34, "Open Data Folder")
	quit := fltk.NewButton(174, 494, 140, 34, "Quit FolderSnap")
	cancel := fltk.NewButton(360, 558, 100, 34, "Cancel")
	save := fltk.NewButton(470, 558, 106, 34, "Save")
	accepted := false
	openData.SetCallback(func() { _ = platform.OpenInExplorer(u.service.DataDir()) })
	quit.SetCallback(func() { win.Hide(); u.Quit() })
	cancel.SetCallback(func() { win.Hide() })
	save.SetCallback(func() { accepted = true; win.Hide() })
	win.SetModal()
	win.End()
	win.Show()
	for win.IsShown() {
		fltk.Wait()
	}
	if !accepted {
		defaultBuffer.Destroy()
		return
	}
	defaultRules := splitRules(defaultBuffer.Text())
	defaultBuffer.Destroy()
	if _, err := ignorepkg.Compile(defaultRules); err != nil {
		fltk.MessageBox("Invalid default exclusions", err.Error())
		return
	}
	updated := cfg
	updated.LaunchAtStartup = startup.Value()
	updated.NotifyScheduledSuccess = notifySuccess.Value()
	updated.CloseToTray = closeTray.Value()
	updated.DefaultRetention = retentionValue(retention.Value())
	updated.DefaultIgnoreRules = defaultRules
	if err := platform.SetLaunchAtStartup(updated.LaunchAtStartup); err != nil {
		fltk.MessageBox("Cannot update Windows startup", err.Error())
		return
	}
	if err := u.service.UpdateConfig(updated); err != nil {
		fltk.MessageBox("Cannot save settings", err.Error())
	}
}

func promptText(title, prompt, initial string, maxRunes int) (string, bool) {
	win := newModalWindow(560, 180, title)
	modalLabel(18, 16, 524, 28, prompt)
	input := fltk.NewInput(18, 54, 524, 34)
	input.SetValue(initial)
	cancel := fltk.NewButton(326, 122, 100, 34, "Cancel")
	save := fltk.NewButton(436, 122, 106, 34, "Save")
	accepted := false
	cancel.SetCallback(func() { win.Hide() })
	save.SetCallback(func() {
		if len([]rune(input.Value())) > maxRunes {
			fltk.MessageBox("Value is too long", fmt.Sprintf("Maximum length is %d characters.", maxRunes))
			return
		}
		accepted = true
		win.Hide()
	})
	win.SetModal()
	win.End()
	win.Show()
	for win.IsShown() {
		fltk.Wait()
	}
	return input.Value(), accepted
}

func scheduleIndex(schedule model.Schedule) int {
	switch schedule.Kind {
	case model.ScheduleInterval:
		return map[int]int{1: 1, 3: 2, 6: 3, 12: 4}[schedule.IntervalHours]
	case model.ScheduleDaily:
		return 5
	case model.ScheduleWeekly:
		return 6
	case model.ScheduleMonthly:
		return 7
	default:
		return 0
	}
}

func scheduleFromInputs(index int, clock, dayText string) (model.Schedule, error) {
	if index == 0 {
		return model.Schedule{Kind: model.ScheduleManual}, nil
	}
	if index >= 1 && index <= 4 {
		return model.Schedule{Kind: model.ScheduleInterval, IntervalHours: []int{0, 1, 3, 6, 12}[index]}, nil
	}
	parsed, err := time.Parse("15:04", strings.TrimSpace(clock))
	if err != nil {
		return model.Schedule{}, errors.New("time must use HH:MM")
	}
	schedule := model.Schedule{Hour: parsed.Hour(), Minute: parsed.Minute()}
	day, err := strconv.Atoi(strings.TrimSpace(dayText))
	if err != nil {
		return model.Schedule{}, errors.New("day must be a number")
	}
	switch index {
	case 5:
		schedule.Kind = model.ScheduleDaily
	case 6:
		if day < 0 || day > 6 {
			return model.Schedule{}, errors.New("weekly day must be 0 through 6")
		}
		schedule.Kind, schedule.Weekday = model.ScheduleWeekly, time.Weekday(day)
	case 7:
		if day < 1 || day > 31 {
			return model.Schedule{}, errors.New("monthly day must be 1 through 31")
		}
		schedule.Kind, schedule.DayOfMonth = model.ScheduleMonthly, day
	default:
		return model.Schedule{}, errors.New("unknown schedule")
	}
	return schedule, nil
}

func retentionIndex(value int) int {
	return map[int]int{10: 0, 25: 1, 50: 2, 100: 3, 0: 4}[value]
}

func retentionValue(index int) int {
	values := []int{10, 25, 50, 100, 0}
	if index < 0 || index >= len(values) {
		return 50
	}
	return values[index]
}

func splitRules(value string) []string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.Split(value, "\n")
}

func modalLabel(x, y, width, height int, text string) *fltk.Box {
	box := fltk.NewBox(fltk.NO_BOX, x, y, width, height, text)
	box.SetAlign(fltk.ALIGN_LEFT | fltk.ALIGN_INSIDE)
	box.SetLabelColor(colorText)
	return box
}

func newModalWindow(width, height int, title string) *fltk.Window {
	window := fltk.NewWindow(width, height, title)
	window.SetColor(colorWindow)
	return window
}

func filepathBase(path string) string {
	path = strings.TrimRight(strings.ReplaceAll(path, `\`, "/"), "/")
	if index := strings.LastIndex(path, "/"); index >= 0 {
		return path[index+1:]
	}
	return path
}

func label(text string, size int, align fltk.Align) *fltk.Box {
	box := fltk.NewBox(fltk.NO_BOX, 0, 0, 100, 30, text)
	box.SetLabelSize(size)
	box.SetLabelColor(colorText)
	box.SetAlign(align)
	box.SetColor(colorPanel)
	return box
}

func metricCard(text string, color fltk.Color) *fltk.Box {
	box := fltk.NewBox(fltk.FLAT_BOX, 0, 0, 120, 62, text)
	box.SetColor(color)
	box.SetLabelColor(colorText)
	box.SetLabelSize(14)
	box.SetAlign(fltk.ALIGN_CENTER | fltk.ALIGN_INSIDE)
	return box
}

func button(text string) *fltk.Button {
	value := fltk.NewButton(0, 0, 100, 34, text)
	styleWidget(value)
	value.SetColor(colorRaised)
	value.SetSelectionColor(colorAccent)
	return value
}

type stylable interface {
	SetColor(fltk.Color)
	SetSelectionColor(fltk.Color)
	SetLabelColor(fltk.Color)
}

func styleWidget(widget stylable) {
	widget.SetColor(colorPanel)
	widget.SetSelectionColor(colorSelection)
	widget.SetLabelColor(colorText)
}

func applyTheme() {
	// FLTK list and input text use palette colors instead of widget labels.
	// Set these globally so no control inherits black text on the dark theme.
	fltk.SetBackgroundColor(23, 23, 23)
	fltk.SetBackground2Color(41, 41, 41)
	fltk.SetForegroundColor(241, 241, 241)
	fltk.SetColor(fltk.SELECTION_COLOR, 52, 119, 94)
	fltk.SetColor(fltk.INACTIVE_COLOR, 168, 168, 168)
}

func formatBytes(value int64) string {
	sign := ""
	if value < 0 {
		sign, value = "-", -value
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	amount, index := float64(value), 0
	for amount >= 1024 && index < len(units)-1 {
		amount /= 1024
		index++
	}
	if index == 0 {
		return fmt.Sprintf("%s%d %s", sign, value, units[index])
	}
	return fmt.Sprintf("%s%.1f %s", sign, amount, units[index])
}

func diffEntryDetail(entry model.DiffEntry) string {
	if entry.Subtype == "type_changed" && entry.Before != nil && entry.After != nil {
		return fmt.Sprintf("%s → %s", titleCase(string(entry.Before.Type)), titleCase(string(entry.After.Type)))
	}
	return entrySizeLabel(entry.Before) + " → " + entrySizeLabel(entry.After)
}

func entrySizeLabel(entry *model.SnapshotEntry) string {
	if entry == nil {
		return "none"
	}
	if entry.Type == model.EntryDirectory {
		return "folder"
	}
	if entry.Type != model.EntryFile {
		return titleCase(string(entry.Type))
	}
	return formatBytes(entry.Size)
}

func titleCase(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
