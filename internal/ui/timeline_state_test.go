package ui

import (
	"testing"
	"time"

	"foldersnap/internal/model"
)

func TestSnapshotPairNeverSelectsSnapshotsAutomatically(t *testing.T) {
	pair := (snapshotPair{}).reconcile(timelineForTest("third", "second", "first"))
	if pair.BeforeID != "" || pair.AfterID != "" {
		t.Fatalf("unexpected automatic pair: %#v", pair)
	}
}

func TestSnapshotPairFirstAndSecondClicksBecomeChronologicalAB(t *testing.T) {
	timeline := timelineForTest("third", "second", "first")
	pair := snapshotPair{}
	pair = pair.selectSnapshot("third", timeline)
	if pair.BeforeID != "third" || pair.AfterID != "" {
		t.Fatalf("first selection = %#v", pair)
	}
	pair = pair.selectSnapshot("first", timeline)
	if pair.BeforeID != "first" || pair.AfterID != "third" {
		t.Fatalf("ordered selections = %#v", pair)
	}
}

func TestSnapshotPairThirdSelectionRollsBToA(t *testing.T) {
	timeline := timelineForTest("third", "second", "first")
	pair := snapshotPair{}
	pair = pair.selectSnapshot("first", timeline)
	pair = pair.selectSnapshot("second", timeline)
	pair = pair.selectSnapshot("third", timeline)
	if pair.BeforeID != "second" || pair.AfterID != "third" {
		t.Fatalf("rolled pair = %#v", pair)
	}
}

func TestSnapshotPairClickingAssignedSnapshotClearsIt(t *testing.T) {
	timeline := timelineForTest("second", "first")
	pair := snapshotPair{BeforeID: "first", AfterID: "second"}
	pair = pair.selectSnapshot("first", timeline)
	if pair.BeforeID != "" || pair.AfterID != "second" {
		t.Fatalf("cleared A = %#v", pair)
	}
	pair = pair.selectSnapshot("second", timeline)
	if pair.BeforeID != "" || pair.AfterID != "" {
		t.Fatalf("cleared B = %#v", pair)
	}
}

func TestSnapshotPairRefreshPreservesAvailableSelections(t *testing.T) {
	pair := snapshotPair{BeforeID: "first", AfterID: "second"}
	pair = pair.reconcile(timelineForTest("third", "second", "first"))
	if pair.BeforeID != "first" || pair.AfterID != "second" {
		t.Fatalf("pair changed during refresh = %#v", pair)
	}
}

func TestSnapshotPairDropsMissingSelection(t *testing.T) {
	pair := snapshotPair{BeforeID: "first", AfterID: "second"}
	pair = pair.reconcile(timelineForTest("third", "second"))
	if pair.BeforeID != "" || pair.AfterID != "second" {
		t.Fatalf("reconciled pair = %#v", pair)
	}
}

func timelineForTest(ids ...string) []model.SnapshotRecord {
	base := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	timeline := make([]model.SnapshotRecord, len(ids))
	for i, id := range ids {
		timeline[i] = model.SnapshotRecord{SnapshotID: id, CompletedAtUTC: base.Add(-time.Duration(i) * time.Minute)}
	}
	return timeline
}
