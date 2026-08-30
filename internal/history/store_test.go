package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"foldersnap/internal/model"
	"foldersnap/internal/pathutil"
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
	if !loaded.EntriesSorted {
		t.Fatal("loaded sorted snapshot was not marked as sorted")
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
	if _, err := os.Stat(filepath.Join(store.DataDir, "History", "a.snapshot")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pruned payload still exists: %v", err)
	}
}

func TestConcurrentSavesDoNotLoseIndexRecords(t *testing.T) {
	store := &Store{DataDir: t.TempDir()}
	const count = 16
	start := make(chan struct{})
	errorsFound := make(chan error, count)
	var wait sync.WaitGroup
	for index := range count {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			id := fmt.Sprintf("snapshot-%02d", index)
			_, err := store.Save(sampleSnapshot(fmt.Sprintf("root-%02d", index), id, time.Unix(int64(index+1), 0).UTC()), 50)
			errorsFound <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	index, err := store.LoadIndex()
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Records) != count {
		t.Fatalf("records = %d, want %d", len(index.Records), count)
	}
	for number := range count {
		id := fmt.Sprintf("snapshot-%02d", number)
		if _, err := os.Stat(store.snapshotPath(id)); err != nil {
			t.Fatalf("payload %s: %v", id, err)
		}
	}
}

func TestDeleteRemovesIndexAndPayload(t *testing.T) {
	store := Store{DataDir: t.TempDir()}
	if _, err := store.Save(sampleSnapshot("root", "snapshot", time.Now().UTC()), 50); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("root", "snapshot"); err != nil {
		t.Fatal(err)
	}
	if records, err := store.List("root"); err != nil || len(records) != 0 {
		t.Fatalf("records = %+v, err = %v", records, err)
	}
	if _, err := os.Stat(store.snapshotPath("snapshot")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted payload remains: %v", err)
	}
}

func TestRepairRestoresReferencedQuarantineAndRemovesUnreferencedOne(t *testing.T) {
	store := Store{DataDir: t.TempDir()}
	for _, id := range []string{"referenced", "unreferenced"} {
		if _, err := store.Save(sampleSnapshot("root", id, time.Now().UTC()), 50); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Rename(store.snapshotPath("referenced"), store.snapshotPath("referenced")+".deleting"); err != nil {
		t.Fatal(err)
	}
	index, err := store.LoadIndex()
	if err != nil {
		t.Fatal(err)
	}
	kept := index.Records[:0]
	for _, record := range index.Records {
		if record.SnapshotID != "unreferenced" {
			kept = append(kept, record)
		}
	}
	index.Records = kept
	if err := store.writeIndex(index); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(store.snapshotPath("unreferenced"), store.snapshotPath("unreferenced")+".deleting"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Repair(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.snapshotPath("referenced")); err != nil {
		t.Fatalf("referenced quarantine was not restored: %v", err)
	}
	if _, err := os.Stat(store.snapshotPath("unreferenced") + ".deleting"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unreferenced quarantine remains: %v", err)
	}
}

func TestHistoryRejectsUnsafeIdentifiersAndEntryPaths(t *testing.T) {
	store := Store{DataDir: t.TempDir()}
	snapshot := sampleSnapshot("root", `..\escape`, time.Now().UTC())
	if _, err := store.Save(snapshot, 50); !errors.Is(err, pathutil.ErrUnsafeStorageID) {
		t.Fatalf("unsafe snapshot ID error = %v", err)
	}
	snapshot = sampleSnapshot("root", "safe", time.Now().UTC())
	snapshot.Entries[0].RelativePath = "../escape.txt"
	if _, err := store.Save(snapshot, 50); err == nil {
		t.Fatal("unsafe entry path was accepted")
	}
	if _, err := store.Load(`..\escape`); !errors.Is(err, pathutil.ErrUnsafeStorageID) {
		t.Fatalf("unsafe load ID error = %v", err)
	}
}

func TestLoadIndexRejectsUnsafePersistedIdentifier(t *testing.T) {
	store := Store{DataDir: t.TempDir()}
	if err := os.MkdirAll(store.historyDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	index := model.HistoryIndex{SchemaVersion: model.SchemaVersion, Records: []model.SnapshotRecord{{SnapshotID: `..\outside`, RootID: "root"}}}
	data, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.indexPath(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadIndex(); !errors.Is(err, pathutil.ErrUnsafeStorageID) {
		t.Fatalf("error = %v, want ErrUnsafeStorageID", err)
	}
}

func TestIndexedMissingPayloadRemainsVisible(t *testing.T) {
	store := Store{DataDir: t.TempDir()}
	if _, err := store.Save(sampleSnapshot("root", "missing", time.Now().UTC()), 50); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.snapshotPath("missing")); err != nil {
		t.Fatal(err)
	}
	records, err := store.List("root")
	if err != nil || len(records) != 1 || records[0].PayloadAvailable {
		t.Fatalf("records = %+v, err = %v", records, err)
	}
}

func TestRepairBacksUpCorruptIndexAndRebuildsFromPayloads(t *testing.T) {
	store := Store{DataDir: t.TempDir()}
	if _, err := store.Save(sampleSnapshot("root", "recoverable", time.Now().UTC()), 50); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.indexPath(), []byte(`{"broken":`), 0o600); err != nil {
		t.Fatal(err)
	}
	index, err := store.Repair()
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Records) != 1 || index.Records[0].SnapshotID != "recoverable" || !index.Records[0].PayloadAvailable {
		t.Fatalf("rebuilt index = %+v", index.Records)
	}
	backups, err := filepath.Glob(store.indexPath() + ".corrupt-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %v, err = %v", backups, err)
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
