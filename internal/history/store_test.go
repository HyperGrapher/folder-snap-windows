package history

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"foldersnap/internal/model"
)

func TestGlobalIndexSaveLoadDescriptionAndPerRootRetention(t *testing.T) {
	store := Store{DataDir: t.TempDir()}
	for i := 0; i < 3; i++ {
		snapshot := sampleSnapshot("root-a", string(rune('a'+i)), time.Unix(int64(i+1), 0).UTC())
		if _, err := store.Save(snapshot, 2); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Save(sampleSnapshot("root-b", "other", time.Unix(4, 0).UTC()), 2); err != nil {
		t.Fatal(err)
	}

	records, err := store.List("root-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].SnapshotID != "c" || records[1].SnapshotID != "b" {
		t.Fatalf("unexpected retained records: %+v", records)
	}
	if err := store.UpdateDescription("root-a", "c", "after build"); err != nil {
		t.Fatal(err)
	}
	records, err = store.List("root-a")
	if err != nil || records[0].Description != "after build" {
		t.Fatalf("description was not indexed: records=%+v err=%v", records, err)
	}
	loaded, err := store.Load("c")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Entries) != 1 || loaded.Header.RootID != "root-a" {
		t.Fatalf("unexpected loaded snapshot: %+v", loaded)
	}

	index, err := store.LoadIndex()
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Records) != 3 || index.Records[0].SnapshotID != "other" {
		t.Fatalf("unexpected global index: %+v", index.Records)
	}
	if _, err := os.Stat(filepath.Join(store.DataDir, "History", "index.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.DataDir, "History", "c.snapshot")); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteRootKeepsOtherFolderHistory(t *testing.T) {
	store := Store{DataDir: t.TempDir()}
	for _, snapshot := range []model.Snapshot{
		sampleSnapshot("root-a", "a", time.Unix(1, 0).UTC()),
		sampleSnapshot("root-b", "b", time.Unix(2, 0).UTC()),
	} {
		if _, err := store.Save(snapshot, 50); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.DeleteRoot("root-a"); err != nil {
		t.Fatal(err)
	}
	if records, _ := store.List("root-a"); len(records) != 0 {
		t.Fatalf("root-a records remain: %+v", records)
	}
	if records, _ := store.List("root-b"); len(records) != 1 || records[0].SnapshotID != "b" {
		t.Fatalf("root-b history changed: %+v", records)
	}
}

func TestLoadReportsMissingSnapshotPayload(t *testing.T) {
	store := Store{DataDir: t.TempDir()}
	if _, err := store.Load("missing"); !errors.Is(err, ErrSnapshotPayloadMissing) {
		t.Fatalf("error = %v", err)
	}
}

func sampleSnapshot(rootID, id string, completed time.Time) model.Snapshot {
	return model.Snapshot{
		SchemaVersion: model.SchemaVersion,
		Header: model.SnapshotHeader{
			SchemaVersion: model.SchemaVersion, SnapshotID: id, RootID: rootID,
			RootPathAtCapture: `C:\Test\` + rootID, DisplayTitle: rootID,
			CompletedAtUTC: completed, Trigger: model.TriggerManual, FileCount: 1,
		},
		Entries: []model.SnapshotEntry{{RelativePath: "a.txt", DisplayPath: "a.txt", Type: model.EntryFile, Size: 1}},
	}
}
