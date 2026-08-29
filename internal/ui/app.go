package ui

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"
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

const resultPageSize = 200

type App struct {
	service *app.Service
	window  *fltk.Window
	status  *fltk.Box

	rootsCards         *folderList
	timelineCards      *cardList
	changesCards       *cardList
	foldersPage        *fltk.Group
	historyPage        *fltk.Group
	foldersTab         *fltk.Button
	historyTab         *fltk.Button
	settingsTab        *fltk.Button
	foldersSummary     *fltk.Box
	historyFolderTitle *fltk.Box
	historyFolderMeta  *fltk.Box
	sizeDeltaSummary   *fltk.Box
	search             *fltk.Input
	summary            *fltk.Box
	addedSummary       *fltk.Box
	removedSummary     *fltk.Box
	modifiedSummary    *fltk.Box
	deltaSummary       *fltk.Box
	filterButtons      map[model.ChangeType]*fltk.Button
	snapshotButton     *fltk.Button
	compareButton      *fltk.Button
	editButton         *fltk.Button
	warningsButton     *fltk.Button
	deleteButton       *fltk.Button
	resultsStatus      *fltk.Box
	previousPage       *fltk.Button
	nextPage           *fltk.Button

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
	diffPage           int
	background         bool
	quitting           bool
	notify             func(string, string)
	actions            *actionQueue
	activeScreen       screenID
	timelineCardStates map[string]timelineCardState
}

type timelineCardState struct {
	button *fltk.Button
	style  *snapshotCardStyle
}

type screenID string

const (
	screenFolders screenID = "folders"
	screenHistory screenID = "history"
)

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
	u := &App{service: service, background: background, actions: newActionQueue(16)}
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
		u.drainActions()
		if u.quitting {
			break
		}
		if u.window.IsShown() {
			fltk.Wait()
		} else {
			// Fl::wait() returns immediately when no FLTK window is shown.
			// A timed wait remains wakeable by tray callbacks without turning
			// the hidden application loop into a one-core busy spin.
			fltk.Wait(0.25)
		}
		u.drainActions()
	}
	return 0
}

// Dispatch queues work for the FLTK-owning thread. Unlike a bare Awake
// callback, this remains reliable while every FLTK window is hidden.
func (u *App) Dispatch(action func()) {
	u.actions.enqueue(action, fltk.AwakeNullMessage)
}

func (u *App) drainActions() {
	u.actions.drain()
}

func (u *App) Show() {
	u.window.Show()
	platform.ApplyDarkTitleBar(u.window.RawHandle())
	u.window.TakeFocus()
}
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
	u.window = fltk.NewWindow(1180, 720, WindowTitle)
	u.window.SetColor(colorWindow)
	u.window.SetSizeRange(900, 600, 0, 0, 0, 0, false)
	platform.ApplyDarkTitleBar(u.window.RawHandle())
	if icon, err := newFolderSnapIcon(); err == nil {
		u.window.SetIcons([]*fltk.RgbImage{icon})
	}

	outer := fltk.NewFlex(0, 0, 1180, 720)
	outer.SetType(fltk.COLUMN)
	outer.SetGap(0)
	outer.SetColor(colorWindow)

	navigation := fltk.NewFlex(0, 0, 1180, 46)
	navigation.SetType(fltk.ROW)
	navigation.SetMargin(0)
	navigation.SetGap(space1)
	navigation.SetColor(colorPanel)
	leftPadding := fltk.NewBox(fltk.NO_BOX, 0, 0, space4, 46, "")
	u.activeScreen = screenFolders
	u.foldersTab = styledTab("Folders", func() bool { return u.activeScreen == screenFolders })
	u.historyTab = styledTab("History & Compare", func() bool { return u.activeScreen == screenHistory })
	u.settingsTab = styledTab("Settings", func() bool { return false })
	u.status = label("Ready", textSmall, fltk.ALIGN_RIGHT|fltk.ALIGN_INSIDE)
	u.status.SetLabelColor(colorSecondary)
	navigation.Fixed(leftPadding, space4)
	navigation.Fixed(u.foldersTab, 96)
	navigation.Fixed(u.historyTab, 148)
	navigation.Fixed(u.settingsTab, 92)
	navigation.Fixed(u.status, 220)
	navigation.End()
	outer.Fixed(navigation, 46)

	pageHost := fltk.NewGroup(0, 46, 1180, 674)
	pageHost.SetBox(fltk.FLAT_BOX)
	pageHost.SetColor(colorWindow)
	u.foldersPage = fltk.NewGroup(0, 46, 1180, 674)
	u.foldersPage.SetBox(fltk.FLAT_BOX)
	u.foldersPage.SetColor(colorWindow)
	foldersLayout := fltk.NewFlex(0, 46, 1180, 674)
	foldersLayout.SetType(fltk.COLUMN)
	foldersLayout.SetColor(colorWindow)
	foldersHeader := fltk.NewFlex(0, 46, 1180, 86)
	foldersHeader.SetType(fltk.ROW)
	foldersHeader.SetMargin(space6, space4)
	foldersHeader.SetColor(colorWindow)
	headingBlock := fltk.NewFlex(0, 0, 800, 56)
	headingBlock.SetType(fltk.COLUMN)
	headingBlock.SetGap(space1)
	headingBlock.SetColor(colorWindow)
	heading := label("Watched Folders", textHeading, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE)
	heading.SetLabelFont(fltk.HELVETICA_BOLD)
	u.foldersSummary = label("", textMeta, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE)
	u.foldersSummary.SetLabelColor(colorSecondary)
	headingBlock.Fixed(heading, 26)
	headingBlock.End()
	add := styledButton("＋  Add Folder", buttonPrimary)
	foldersHeader.Fixed(add, 126)
	foldersHeader.End()
	u.rootsCards = newFolderList(0, 132, 1180, 588)
	foldersLayout.Fixed(foldersHeader, 86)
	foldersLayout.End()
	u.foldersPage.End()

	u.historyPage = fltk.NewGroup(0, 46, 1180, 674)
	u.historyPage.SetBox(fltk.FLAT_BOX)
	u.historyPage.SetColor(colorWindow)
	historyLayout := fltk.NewFlex(0, 46, 1180, 674)
	historyLayout.SetType(fltk.ROW)
	historyLayout.SetGap(1)
	historyLayout.SetColor(colorDivider)

	sidebar := fltk.NewFlex(0, 46, 306, 674)
	sidebar.SetType(fltk.COLUMN)
	sidebar.SetMargin(space3, space2)
	sidebar.SetGap(space2)
	sidebar.SetColor(colorPanel)
	sidebarHeader := fltk.NewFlex(0, 0, 282, 56)
	sidebarHeader.SetType(fltk.COLUMN)
	sidebarHeader.SetGap(space1)
	sidebarHeader.SetColor(colorPanel)
	u.historyFolderTitle = label("Select a folder", 15, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE)
	u.historyFolderTitle.SetLabelFont(fltk.HELVETICA_BOLD)
	u.historyFolderMeta = label("No snapshot history", textSmall, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE)
	u.historyFolderMeta.SetLabelColor(colorSecondary)
	sidebarHeader.Fixed(u.historyFolderTitle, 26)
	sidebarHeader.End()
	u.timelineCards = newCardList(0, 0, 282, 470)
	snapshotActions := fltk.NewFlex(0, 0, 282, 32)
	snapshotActions.SetType(fltk.ROW)
	snapshotActions.SetGap(space1)
	u.editButton = styledButton("Edit", buttonGhost)
	u.warningsButton = styledButton("Warnings", buttonGhost)
	u.deleteButton = styledButton("Delete", buttonGhost)
	u.editButton.Deactivate()
	u.warningsButton.Deactivate()
	u.deleteButton.Deactivate()
	snapshotActions.Fixed(u.editButton, 68)
	snapshotActions.Fixed(u.warningsButton, 82)
	snapshotActions.Fixed(u.deleteButton, 72)
	snapshotActions.End()
	u.compareButton = styledButton("Compare A → B", buttonPrimary)
	u.compareButton.Deactivate()
	sidebar.Fixed(sidebarHeader, 56)
	sidebar.Fixed(snapshotActions, 32)
	sidebar.Fixed(u.compareButton, 40)
	sidebar.End()
	historyLayout.Fixed(sidebar, 306)

	right := fltk.NewFlex(307, 46, 873, 674)
	right.SetType(fltk.COLUMN)
	right.SetMargin(space6, space4)
	right.SetGap(space3)
	right.SetColor(colorWindow)

	snapshotHeader := fltk.NewFlex(0, 0, 825, 52)
	snapshotHeader.SetType(fltk.ROW)
	u.summary = label("Pick two snapshots", textHeading, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE)
	u.summary.SetLabelFont(fltk.HELVETICA_BOLD)
	u.summary.SetLabelColor(colorText)
	u.snapshotButton = styledButton("Snapshot Now", buttonSecondary)
	u.snapshotButton.Deactivate()
	snapshotHeader.Fixed(u.snapshotButton, 126)
	snapshotHeader.End()
	metrics := fltk.NewFlex(0, 0, 825, 94)
	metrics.SetType(fltk.ROW)
	metrics.SetGap(space3)
	u.addedSummary = metricCard("ADDED\n—", colorAdded)
	u.removedSummary = metricCard("REMOVED\n—", colorRemoved)
	u.modifiedSummary = metricCard("MODIFIED\n—", colorModified)
	u.deltaSummary = metricCard("UNCHANGED\n—", colorUnchanged)
	metrics.End()
	u.sizeDeltaSummary = label("Net size change  —", textMeta, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE)
	u.sizeDeltaSummary.SetLabelFont(fltk.COURIER)
	u.sizeDeltaSummary.SetLabelColor(colorSecondary)
	controls := fltk.NewFlex(0, 0, 825, 38)
	controls.SetType(fltk.ROW)
	controls.SetGap(space2)
	all, added := styledButton("All Changes", buttonSecondary), styledButton("Added", buttonSecondary)
	removed, modified := styledButton("Removed", buttonSecondary), styledButton("Modified", buttonSecondary)
	u.filterButtons = map[model.ChangeType]*fltk.Button{
		"":                   all,
		model.ChangeAdded:    added,
		model.ChangeRemoved:  removed,
		model.ChangeModified: modified,
	}
	controls.Fixed(all, 84)
	controls.Fixed(added, 64)
	controls.Fixed(removed, 72)
	controls.Fixed(modified, 76)
	_ = fltk.NewBox(fltk.NO_BOX, 0, 0, 20, 34, "")
	u.search = fltk.NewInput(0, 0, 220, 34, "")
	u.search.SetTooltip("Search changed files by name or relative path")
	styleWidget(u.search)
	u.search.SetColor(colorInput)
	u.search.SetDrawHandler(func(baseDraw func()) {
		baseDraw()
		if u.search.Value() == "" && !u.search.HasFocus() {
			fltk.SetDrawColor(colorDisabled)
			fltk.SetDrawFont(fltk.HELVETICA, textMeta)
			fltk.Draw("Search changed files…", u.search.X()+space3, u.search.Y(), u.search.W()-space6, u.search.H(), fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE|fltk.ALIGN_CLIP)
		}
	})
	controls.Fixed(u.search, 180)
	controls.End()
	resultNavigation := fltk.NewFlex(0, 0, 825, 30)
	resultNavigation.SetType(fltk.ROW)
	resultNavigation.SetGap(6)
	u.resultsStatus = label("", textSmall, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE)
	u.resultsStatus.SetLabelColor(colorSecondary)
	u.previousPage = styledButton("Previous", buttonSecondary)
	u.nextPage = styledButton("Next", buttonSecondary)
	resultNavigation.Fixed(u.previousPage, 90)
	resultNavigation.Fixed(u.nextPage, 72)
	resultNavigation.End()
	u.changesCards = newCardList(0, 0, 825, 340)
	right.Fixed(snapshotHeader, 52)
	right.Fixed(metrics, 94)
	right.Fixed(u.sizeDeltaSummary, 24)
	right.Fixed(controls, 38)
	right.Fixed(resultNavigation, 30)
	right.End()
	historyLayout.End()
	u.historyPage.End()
	u.historyPage.Hide()
	pageHost.End()
	outer.End()
	u.window.Resizable(outer)
	u.window.End()

	add.SetCallback(func() { u.Dispatch(u.addFolder) })
	u.foldersTab.SetCallback(func() { u.showScreen(screenFolders) })
	u.historyTab.SetCallback(func() { u.showScreen(screenHistory) })
	u.settingsTab.SetCallback(func() { u.Dispatch(u.showSettings) })
	u.snapshotButton.SetCallback(u.snapshotNow)
	u.compareButton.SetCallback(u.compareSelected)
	u.editButton.SetCallback(func() { u.Dispatch(u.editSnapshotDescription) })
	u.warningsButton.SetCallback(func() { u.Dispatch(u.viewSnapshotWarnings) })
	u.deleteButton.SetCallback(func() { u.Dispatch(u.deleteSelectedSnapshot) })
	all.SetCallback(func() { u.setFilter("") })
	added.SetCallback(func() { u.setFilter(model.ChangeAdded) })
	removed.SetCallback(func() { u.setFilter(model.ChangeRemoved) })
	modified.SetCallback(func() { u.setFilter(model.ChangeModified) })
	u.search.SetCallbackCondition(fltk.WhenChanged)
	u.search.SetCallback(func() {
		if u.compareState == comparisonDone {
			u.diffPage = 0
			u.renderDiff()
		}
	})
	u.previousPage.SetCallback(func() {
		if u.diffPage > 0 {
			u.diffPage--
			u.renderDiff()
		}
	})
	u.nextPage.SetCallback(func() {
		u.diffPage++
		u.renderDiff()
	})
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

func (u *App) showScreen(screen screenID) {
	u.activeScreen = screen
	if screen == screenHistory {
		u.foldersPage.Hide()
		u.historyPage.Show()
	} else {
		u.historyPage.Hide()
		u.foldersPage.Show()
	}
	u.foldersTab.Redraw()
	u.historyTab.Redraw()
	u.window.Redraw()
}

func (u *App) openRootHistory(rootID string) {
	u.selectRoot(rootID)
	u.showScreen(screenHistory)
}

func (u *App) bindEvents() {
	go func() {
		for event := range u.service.Events() {
			event := event
			u.Dispatch(func() { u.handleEvent(event) })
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
	totalSnapshots := 0
	cards := make([]folderRowStyle, 0, len(u.roots))
	for _, root := range u.roots {
		records, _ := u.service.ListSnapshots(root.RootID)
		totalSnapshots += len(records)
		lastSnapshot := "Never"
		if len(records) > 0 {
			lastSnapshot = relativeTime(records[0].CompletedAtUTC.Local(), time.Now())
		}
		card := folderRowStyle{
			RootID: root.RootID,
			Name:   root.DisplayName, Path: root.Path, SnapshotCount: len(records), LastSnapshot: lastSnapshot,
			Schedule: scheduleDisplay(root.Schedule), Selected: root.RootID == u.selectedRootID,
			Unavailable: root.LastScanError != "", Archived: root.Archived,
		}
		cards = append(cards, card)
	}
	if !u.rootsCards.update(cards) {
		u.rootsCards.clear()
		for _, card := range cards {
			rootID := card.RootID
			u.rootsCards.add(card,
				func() { u.Dispatch(func() { u.openRootHistory(rootID) }) },
				func() {
					u.Dispatch(func() {
						u.selectRoot(rootID)
						u.showRootSettings()
					})
				},
			)
		}
		u.rootsCards.finishBatch()
	}
	u.foldersSummary.SetLabel(fmt.Sprintf("%d folder%s  ·  %d snapshot%s total", len(u.roots), pluralSuffix(len(u.roots)), totalSnapshots, pluralSuffix(totalSnapshots)))
	if len(u.roots) == 0 {
		u.rootsCards.showEmpty()
	}
	if u.selectedRootID == "" && len(u.roots) > 0 {
		u.selectRoot(u.roots[0].RootID)
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
	if confirmDialog("Folder added", "Take the first snapshot now?", "Take Snapshot", false) {
		_ = u.service.RequestSnapshot(root.RootID, model.TriggerManual)
	}
}

func (u *App) selectRoot(rootID string) {
	if rootID == u.selectedRootID {
		return
	}
	var selected model.WatchedRoot
	found := false
	for _, root := range u.roots {
		if root.RootID == rootID {
			selected, found = root, true
			break
		}
	}
	if !found {
		return
	}
	u.selectedRootID = rootID
	u.historyFolderTitle.SetLabel(selected.DisplayName)
	u.beforeID, u.afterID = "", ""
	u.selectedSnapshotID = ""
	u.filter = ""
	u.search.SetValue("")
	u.snapshotButton.Activate()
	if selected.Archived {
		u.snapshotButton.Deactivate()
	}
	u.rootsCards.selectRoot(rootID)
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
	u.historyFolderMeta.SetLabel(fmt.Sprintf("%d snapshot%s  ·  newest first", len(records), pluralSuffix(len(records))))
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
	u.timelineCards.clear()
	u.timelineCardStates = make(map[string]timelineCardState, len(u.timeline))
	for _, snapshot := range u.timeline {
		role := ""
		if snapshot.SnapshotID == u.beforeID {
			role = "A"
		} else if snapshot.SnapshotID == u.afterID {
			role = "B"
		}
		note := titleCase(string(snapshot.Trigger))
		if snapshot.Description != "" {
			note += "  ·  " + snapshot.Description
		}
		if !snapshot.PayloadAvailable {
			note += "  ·  Payload missing"
		}
		if snapshot.WarningCount > 0 {
			note += fmt.Sprintf("  ·  %d warning%s", snapshot.WarningCount, pluralSuffix(snapshot.WarningCount))
		}
		details := fmt.Sprintf("%d file%s  •  %d folder%s  •  %s",
			snapshot.FileCount, pluralSuffix64(snapshot.FileCount),
			snapshot.DirectoryCount, pluralSuffix64(snapshot.DirectoryCount),
			formatBytes(snapshot.TotalFileBytes),
		)
		snapshotID := snapshot.SnapshotID
		card := &snapshotCardStyle{
			Date: snapshot.CompletedAtUTC.Local().Format("02 Jan 2006  ·  15:04"), Details: details,
			Note: note, Role: role, Selected: snapshot.SnapshotID == u.selectedSnapshotID, Missing: !snapshot.PayloadAvailable,
		}
		button := u.timelineCards.add(68, card.Date+" — "+details, func(button *fltk.Button) { drawSnapshotCard(button, *card) }, func() {
			u.Dispatch(func() { u.selectTimelineSnapshot(snapshotID) })
		})
		u.timelineCardStates[snapshotID] = timelineCardState{button: button, style: card}
	}
	if len(u.timeline) == 0 {
		u.timelineCards.showEmpty("No snapshots yet.\nCreate the first snapshot for this folder.")
	}
	u.updateSnapshotActions()
}

func (u *App) updateTimelineSelection() {
	for _, snapshot := range u.timeline {
		state, ok := u.timelineCardStates[snapshot.SnapshotID]
		if !ok || state.style == nil || state.button == nil {
			continue
		}
		role := ""
		if snapshot.SnapshotID == u.beforeID {
			role = "A"
		} else if snapshot.SnapshotID == u.afterID {
			role = "B"
		}
		state.style.Role = role
		state.style.Selected = snapshot.SnapshotID == u.selectedSnapshotID
		state.button.Redraw()
	}
	u.updateSnapshotActions()
}

// selectTimelineSnapshot mirrors the macOS history interaction: the first
// click selects A, the second selects B, and a third rolls the pair
// forward. Clicking an assigned snapshot removes it from the pair.
func (u *App) selectTimelineSnapshot(snapshotID string) {
	if _, ok := u.snapshotRecord(snapshotID); !ok {
		return
	}
	u.selectedSnapshotID = snapshotID
	u.applyPair(u.currentPair().selectSnapshot(snapshotID, u.timeline))
	u.updateTimelineSelection()
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
	u.diffPage = 0
	u.diff = model.DiffResult{}
	u.summary.SetLabel("Loading snapshots and comparing files...")
	u.setMetricPlaceholders()
	u.changesCards.clear()
	u.changesCards.showEmpty("Working…")
	u.compareButton.Deactivate()
	go func() {
		before, err := u.service.LoadSnapshot(rootID, beforeID)
		if err == nil {
			after, loadErr := u.service.LoadSnapshot(rootID, afterID)
			err = loadErr
			if err == nil {
				largeComparison := len(before.Entries)+len(after.Entries) >= 100_000
				result, compareErr := u.service.Compare(before, after)
				err = compareErr
				if err == nil {
					u.Dispatch(func() {
						if u.selectedRootID == rootID && u.beforeID == beforeID && u.afterID == afterID && u.compareVersion == version {
							u.beforeID, u.afterID, u.diff = result.BeforeID, result.AfterID, result
							u.compareState = comparisonDone
							u.renderDiff()
							u.updateCompareButton()
						}
					})
					if largeComparison {
						// The result owns compact copies of changed entries, so the
						// decoded full snapshots can be returned to Windows now.
						before = model.Snapshot{}
						after = model.Snapshot{}
						runtime.GC()
						debug.FreeOSMemory()
					}
					return
				}
			}
		}
		u.Dispatch(func() {
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
	u.deltaSummary.SetLabel(fmt.Sprintf("UNCHANGED\n%d", s.Unchanged))
	u.sizeDeltaSummary.SetLabel("Net size change  " + signedBytes(s.NetBytes))
	u.renderFilterState()
	query := strings.TrimSpace(u.search.Value())
	u.changesCards.clear()
	page := makeDiffViewPage(u.diff.Entries, u.filter, query, u.diffPage, resultPageSize)
	u.diffPage = page.Page
	for _, item := range page.Entries {
		marker := map[model.ChangeType]string{model.ChangeAdded: "ADDED", model.ChangeRemoved: "REMOVED", model.ChangeModified: "MODIFIED"}[item.Change]
		entry := item.After
		if entry == nil {
			entry = item.Before
		}
		u.changesCards.addDeferred(46, item.DisplayPath, func(button *fltk.Button) {
			drawChangeCard(button, changeCardStyle{
				Path: item.DisplayPath, Detail: diffEntryDetail(item), Status: marker,
				Change: string(item.Change), Directory: entry != nil && entry.Type == model.EntryDirectory,
			})
		}, nil)
	}
	u.changesCards.finishBatch()
	u.renderResultNavigation(page.Total, page.Start, page.End)
	if page.Total == 0 {
		message := "No changed items in this comparison."
		if query != "" || u.filter != "" {
			message = "No changes match the current filter."
		}
		u.changesCards.showEmpty(message)
	}
}

func (u *App) renderResultNavigation(total, start, end int) {
	if total == 0 {
		u.resultsStatus.SetLabel("0 changes")
		u.previousPage.Deactivate()
		u.nextPage.Deactivate()
		return
	}
	u.resultsStatus.SetLabel(fmt.Sprintf("Showing %d–%d of %d changes", start+1, end, total))
	if start > 0 {
		u.previousPage.Activate()
	} else {
		u.previousPage.Deactivate()
	}
	if end < total {
		u.nextPage.Activate()
	} else {
		u.nextPage.Deactivate()
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
	u.diffPage = 0
	u.summary.SetLabel(message)
	u.setMetricPlaceholders()
	u.changesCards.clear()
	u.changesCards.showEmpty(message)
	u.renderResultNavigation(0, 0, 0)
	u.renderFilterState()
	u.updateCompareButton()
}

func (u *App) showComparisonError(message string) {
	u.compareVersion++
	u.compareState = comparisonError
	u.diff = model.DiffResult{}
	u.diffPage = 0
	u.summary.SetLabel(message)
	u.setMetricPlaceholders()
	u.changesCards.clear()
	u.changesCards.showEmpty(message)
	u.renderResultNavigation(0, 0, 0)
	u.renderFilterState()
}

func (u *App) setMetricPlaceholders() {
	u.addedSummary.SetLabel("ADDED\n—")
	u.removedSummary.SetLabel("REMOVED\n—")
	u.modifiedSummary.SetLabel("MODIFIED\n—")
	u.deltaSummary.SetLabel("UNCHANGED\n—")
	u.sizeDeltaSummary.SetLabel("Net size change  —")
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
	u.diffPage = 0
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
	cancel := placedButton(492, 504, 110, 38, "Cancel", buttonSecondary)
	preflight := placedButton(612, 504, 132, 38, "Run Preflight", buttonPrimary)
	clearButtonFocus(cancel, preflight)
	accepted := false
	cancel.SetCallback(func() { closeModalWindow(win) })
	preflight.SetCallback(func() { accepted = true; closeModalWindow(win) })
	win.End()
	showModalWindow(win)
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
	u.Dispatch(func() {
		u.status.SetLabel("Cleanup preflight complete")
		if ready == 0 {
			fltk.MessageBox("Nothing can be removed", fmt.Sprintf("Ready: 0\nBlocked: %d\nAlready missing: %d", blocked, missing))
			return
		}
		if !confirmDialog("Cleanup preflight complete", fmt.Sprintf("Ready to move to Recycle Bin: %d\nBlocked: %d\nAlready missing: %d", ready, blocked, missing), fmt.Sprintf("Move %d items", ready), true) {
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
			u.Dispatch(func() {
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
	if !confirmDialog("Delete snapshot", message, "Delete Snapshot", true) {
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
	styleInput(name)
	modalLabel(20, 56, 150, 28, "Folder")
	pathOutput := fltk.NewOutput(180, 56, 390, 28)
	pathOutput.SetValue(root.Path)
	styleInput(&pathOutput.Input)
	openExplorer := placedButton(580, 56, 110, 28, "Open Explorer", buttonSecondary)
	clearButtonFocus(openExplorer)

	modalLabel(20, 104, 150, 28, "Schedule")
	schedule := fltk.NewChoice(180, 104, 220, 28)
	schedule.ClearVisibleFocus()
	styleWidget(schedule)
	schedule.SetColor(colorInput)
	for _, item := range []string{"Manual only", "Every 1 hour", "Every 3 hours", "Every 6 hours", "Every 12 hours", "Daily", "Weekly", "Monthly"} {
		schedule.Add(item, func() {})
	}
	schedule.SetValue(scheduleIndex(root.Schedule))
	modalLabel(420, 104, 90, 28, "Time")
	timeInput := fltk.NewInput(510, 104, 75, 28)
	styleInput(timeInput)
	hour, minute := root.Schedule.Hour, root.Schedule.Minute
	if root.Schedule.Kind == model.ScheduleManual || root.Schedule.Kind == model.ScheduleInterval {
		now := time.Now()
		hour, minute = now.Hour(), now.Minute()
	}
	timeInput.SetValue(fmt.Sprintf("%02d:%02d", hour, minute))
	modalLabel(590, 104, 45, 28, "Day")
	dayInput := fltk.NewIntInput(635, 104, 55, 28)
	styleInput(&dayInput.Input)
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
	retention.ClearVisibleFocus()
	styleWidget(retention)
	retention.SetColor(colorInput)
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
	editor.SetBox(fltk.RFLAT_BOX)

	modalLabel(20, 510, 100, 28, "Test path")
	testPath := fltk.NewInput(120, 510, 400, 28)
	styleInput(testPath)
	testResult := modalLabel(530, 510, 160, 28, "")
	testButton := placedButton(20, 548, 110, 32, "Test Rule", buttonSecondary)
	archive := placedButton(140, 548, 150, 32, map[bool]string{true: "Resume Watching", false: "Archive / Stop"}[root.Archived], buttonSecondary)
	deleteHistory := placedButton(300, 548, 150, 32, "Delete History...", buttonDestructive)
	cancel := placedButton(468, 638, 100, 34, "Cancel", buttonSecondary)
	save := placedButton(578, 638, 112, 34, "Save", buttonPrimary)
	clearButtonFocus(testButton, archive, deleteHistory, cancel, save)

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
		if !confirmDialog("Delete folder history", "Permanently delete all FolderSnap snapshot history for this folder?\n\nThe actual folder and its files are not changed.", "Delete History", true) {
			return
		}
		if err := u.service.ClearHistory(root.RootID); err != nil {
			fltk.MessageBox("Cannot delete history", err.Error())
			return
		}
		u.beforeID, u.afterID = "", ""
		u.selectedSnapshotID = ""
		u.showIdleComparison("No snapshots yet. Create the first snapshot for this folder.")
		closeModalWindow(win)
		u.loadTimeline()
	})
	cancel.SetCallback(func() { closeModalWindow(win) })
	save.SetCallback(func() { accepted = true; closeModalWindow(win) })
	win.End()
	showModalWindow(win)
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
	styleCheckButton(startup)
	notifySuccess := fltk.NewCheckButton(24, 62, 550, 30, "Notify after successful scheduled snapshots")
	notifySuccess.SetValue(cfg.NotifyScheduledSuccess)
	styleCheckButton(notifySuccess)
	closeTray := fltk.NewCheckButton(24, 100, 550, 30, "Close the main window to the system tray")
	closeTray.SetValue(cfg.CloseToTray)
	styleCheckButton(closeTray)
	modalLabel(24, 148, 180, 28, "Default retention")
	retention := fltk.NewChoice(210, 148, 180, 28)
	retention.ClearVisibleFocus()
	styleWidget(retention)
	retention.SetColor(colorInput)
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
	defaultEditor.SetBox(fltk.RFLAT_BOX)
	openData := placedButton(24, 494, 140, 34, "Open Data Folder", buttonSecondary)
	quit := placedButton(174, 494, 140, 34, "Quit FolderSnap", buttonDestructive)
	cancel := placedButton(360, 558, 100, 34, "Cancel", buttonSecondary)
	save := placedButton(470, 558, 106, 34, "Save", buttonPrimary)
	clearButtonFocus(openData, quit, cancel, save)
	accepted := false
	openData.SetCallback(func() { _ = platform.OpenInExplorer(u.service.DataDir()) })
	quit.SetCallback(func() { closeModalWindow(win); u.Quit() })
	cancel.SetCallback(func() { closeModalWindow(win) })
	save.SetCallback(func() { accepted = true; closeModalWindow(win) })
	win.End()
	showModalWindow(win)
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
	styleInput(input)
	cancel := placedButton(326, 122, 100, 34, "Cancel", buttonSecondary)
	save := placedButton(436, 122, 106, 34, "Save", buttonPrimary)
	clearButtonFocus(cancel, save)
	accepted := false
	cancel.SetCallback(func() { closeModalWindow(win) })
	save.SetCallback(func() {
		if len([]rune(input.Value())) > maxRunes {
			fltk.MessageBox("Value is too long", fmt.Sprintf("Maximum length is %d characters.", maxRunes))
			return
		}
		accepted = true
		closeModalWindow(win)
	})
	win.End()
	showModalWindow(win)
	for win.IsShown() {
		fltk.Wait()
	}
	return input.Value(), accepted
}

func confirmDialog(title, message, action string, destructive bool) bool {
	win := newModalWindow(540, 210, title)
	messageBox := fltk.NewBox(fltk.NO_BOX, 24, 22, 492, 112, message)
	messageBox.SetLabelFont(fltk.HELVETICA)
	messageBox.SetLabelSize(textBody)
	messageBox.SetLabelColor(colorText)
	messageBox.SetAlign(fltk.ALIGN_LEFT | fltk.ALIGN_INSIDE | fltk.ALIGN_WRAP)
	cancel := placedButton(270, 154, 104, 36, "Cancel", buttonSecondary)
	variant := buttonPrimary
	if destructive {
		variant = buttonDestructive
	}
	confirm := placedButton(384, 154, 132, 36, action, variant)
	accepted := false
	cancel.SetCallback(func() { closeModalWindow(win) })
	confirm.SetCallback(func() {
		accepted = true
		closeModalWindow(win)
	})
	win.End()
	showModalWindow(win)
	for win.IsShown() {
		fltk.Wait()
	}
	return accepted
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
	box.SetLabelFont(fltk.HELVETICA)
	box.SetLabelSize(textBody)
	return box
}

func newModalWindow(width, height int, title string) *fltk.Window {
	window := fltk.NewWindow(width, height, title)
	window.SetColor(colorWindow)
	platform.ApplyDarkTitleBar(window.RawHandle())
	return window
}

func showModalWindow(window *fltk.Window) {
	// FLTK's process-global modal pointer can remain attached to a hidden window
	// and suppress release callbacks in the main UI. The nested Wait loop below
	// already gives these dialogs synchronous behavior, so keep them non-modal.
	window.SetNonModal()
	window.SetCallback(func() { closeModalWindow(window) })
	window.Show()
	platform.ApplyDarkTitleBar(window.RawHandle())
}

func closeModalWindow(window *fltk.Window) {
	window.SetNonModal()
	window.Hide()
}

func filepathBase(path string) string {
	path = strings.TrimRight(strings.ReplaceAll(path, `\`, "/"), "/")
	if index := strings.LastIndex(path, "/"); index >= 0 {
		return path[index+1:]
	}
	return path
}

func relativeTime(value, now time.Time) string {
	delta := now.Sub(value)
	if delta < time.Minute {
		return "Just now"
	}
	if delta < time.Hour {
		return fmt.Sprintf("%dm ago", int(delta.Minutes()))
	}
	if delta < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(delta.Hours()))
	}
	days := int(delta.Hours() / 24)
	if days < 14 {
		return fmt.Sprintf("%dd ago", days)
	}
	return value.Format("02 Jan")
}

func scheduleDisplay(schedule model.Schedule) string {
	switch schedule.Kind {
	case model.ScheduleInterval:
		if schedule.IntervalHours == 1 {
			return "Every hour"
		}
		return fmt.Sprintf("Every %dh", schedule.IntervalHours)
	case model.ScheduleDaily:
		return "Daily"
	case model.ScheduleWeekly:
		return "Weekly"
	case model.ScheduleMonthly:
		return "Monthly"
	default:
		return "Manual"
	}
}

func label(text string, size int, align fltk.Align) *fltk.Box {
	box := fltk.NewBox(fltk.NO_BOX, 0, 0, 100, 30, text)
	box.SetLabelFont(fltk.HELVETICA)
	box.SetLabelSize(size)
	box.SetLabelColor(colorText)
	box.SetAlign(align)
	box.SetColor(colorPanel)
	return box
}

func metricCard(text string, color fltk.Color) *fltk.Box {
	box := fltk.NewBox(fltk.NO_BOX, 0, 0, 120, 94, text)
	box.SetDrawHandler(func(func()) {
		x, y, width, height := box.X(), box.Y(), box.W(), box.H()
		drawRoundedFill(x, y, width, height, radiusLarge, colorPanel)
		drawRoundedFrame(x, y, width, height, radiusLarge, colorDivider)
		fltk.DrawRectfWithColor(x+1, y+1, width-2, 3, color)
		parts := strings.SplitN(box.Label(), "\n", 2)
		caption, value := parts[0], "—"
		if len(parts) == 2 {
			value = parts[1]
		}
		fltk.SetDrawColor(color)
		fltk.SetDrawFont(fltk.HELVETICA_BOLD, 26)
		fltk.Draw(value, x+space4, y+12, width-space8, 34, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE|fltk.ALIGN_CLIP)
		fltk.SetDrawColor(colorSecondary)
		fltk.SetDrawFont(fltk.HELVETICA_BOLD, textSmall)
		fltk.Draw(caption, x+space4, y+52, width-space8, 22, fltk.ALIGN_LEFT|fltk.ALIGN_INSIDE|fltk.ALIGN_CLIP)
	})
	return box
}

func button(text string) *fltk.Button {
	return styledButton(text, buttonSecondary)
}

func clearButtonFocus(buttons ...*fltk.Button) {
	for _, value := range buttons {
		value.ClearVisibleFocus()
	}
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
	fltk.SetFont(fltk.HELVETICA, "Segoe UI")
	fltk.SetFont(fltk.HELVETICA_BOLD, "Segoe UI Bold")
	fltk.SetFont(fltk.HELVETICA_ITALIC, "Segoe UI Italic")
	fltk.SetFont(fltk.COURIER, "Consolas")
	fltk.SetBackgroundColor(14, 17, 23)
	fltk.SetBackground2Color(37, 45, 61)
	fltk.SetForegroundColor(226, 232, 240)
	fltk.SetColor(fltk.SELECTION_COLOR, 74, 144, 226)
	fltk.SetColor(fltk.INACTIVE_COLOR, 61, 74, 92)
	fltk.SetScrollbarSize(12)
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

func signedBytes(value int64) string {
	if value > 0 {
		return "+" + formatBytes(value)
	}
	if value < 0 {
		return "−" + formatBytes(-value)
	}
	return "0 B"
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

func pluralSuffix(value int) string {
	if value == 1 {
		return ""
	}
	return "s"
}

func pluralSuffix64(value int64) string {
	if value == 1 {
		return ""
	}
	return "s"
}
