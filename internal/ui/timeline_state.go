package ui

import (
	"time"

	"foldersnap/internal/model"
)

// snapshotPair mirrors the macOS A/B selection model independently from FLTK.
type snapshotPair struct {
	BeforeID string
	AfterID  string
}

func (pair snapshotPair) reconcile(timeline []model.SnapshotRecord) snapshotPair {
	available := make(map[string]bool, len(timeline))
	for _, snapshot := range timeline {
		available[snapshot.SnapshotID] = true
	}
	if !available[pair.BeforeID] {
		pair.BeforeID = ""
	}
	if !available[pair.AfterID] {
		pair.AfterID = ""
	}
	return pair.ordered(timeline)
}

func (pair snapshotPair) selectSnapshot(id string, timeline []model.SnapshotRecord) snapshotPair {
	switch id {
	case "":
		return pair
	case pair.BeforeID:
		pair.BeforeID = ""
	case pair.AfterID:
		pair.AfterID = ""
	default:
		if pair.BeforeID == "" {
			pair.BeforeID = id
		} else if pair.AfterID == "" {
			pair.AfterID = id
		} else {
			pair.BeforeID, pair.AfterID = pair.AfterID, id
		}
	}
	return pair.ordered(timeline)
}

func (pair snapshotPair) ordered(timeline []model.SnapshotRecord) snapshotPair {
	if pair.BeforeID == "" || pair.AfterID == "" {
		return pair
	}
	times := make(map[string]time.Time, len(timeline))
	for _, snapshot := range timeline {
		times[snapshot.SnapshotID] = snapshot.CompletedAtUTC
	}
	beforeTime, beforeFound := times[pair.BeforeID]
	afterTime, afterFound := times[pair.AfterID]
	if beforeFound && afterFound && beforeTime.After(afterTime) {
		pair.BeforeID, pair.AfterID = pair.AfterID, pair.BeforeID
	}
	return pair
}
