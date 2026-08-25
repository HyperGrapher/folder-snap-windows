package history

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"foldersnap/internal/atomicfile"
	"foldersnap/internal/model"
)

var ErrSnapshotPayloadMissing = errors.New("snapshot payload is missing")

type Store struct{ DataDir string }

func (s Store) historyDir() string { return filepath.Join(s.DataDir, "History") }
func (s Store) indexPath() string  { return filepath.Join(s.historyDir(), "index.json") }
func (s Store) snapshotPath(snapshotID string) string {
	return filepath.Join(s.historyDir(), snapshotID+".snapshot")
}

func (s Store) LoadIndex() (model.HistoryIndex, error) {
	file, err := os.Open(s.indexPath())
	if errors.Is(err, os.ErrNotExist) {
		return model.HistoryIndex{SchemaVersion: model.SchemaVersion}, nil
	}
	if err != nil {
		return model.HistoryIndex{}, err
	}
	defer file.Close()

	var index model.HistoryIndex
	if err := json.NewDecoder(io.LimitReader(file, 32<<20)).Decode(&index); err != nil {
		return model.HistoryIndex{}, fmt.Errorf("read history index: %w", err)
	}
	if index.SchemaVersion != model.SchemaVersion {
		return model.HistoryIndex{}, fmt.Errorf("unsupported history schema %d", index.SchemaVersion)
	}
	sortRecords(index.Records)
	for i := range index.Records {
		_, statErr := os.Stat(s.snapshotPath(index.Records[i].SnapshotID))
		index.Records[i].PayloadAvailable = statErr == nil
	}
	return index, nil
}

func (s Store) List(rootID string) ([]model.SnapshotRecord, error) {
	index, err := s.LoadIndex()
	if err != nil {
		return nil, err
	}
	records := make([]model.SnapshotRecord, 0)
	for _, record := range index.Records {
		if record.RootID == rootID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (s Store) Save(snapshot model.Snapshot, retention int) (model.SnapshotRecord, error) {
	if snapshot.SchemaVersion != model.SchemaVersion || snapshot.Header.SchemaVersion != model.SchemaVersion {
		return model.SnapshotRecord{}, errors.New("snapshot schema version is invalid")
	}
	if snapshot.Header.SnapshotID == "" || snapshot.Header.RootID == "" {
		return model.SnapshotRecord{}, errors.New("snapshot identity is missing")
	}

	payloadPath := s.snapshotPath(snapshot.Header.SnapshotID)
	if err := atomicfile.Write(payloadPath, 0o600, func(file *os.File) error {
		compressed := gzip.NewWriter(file)
		if err := json.NewEncoder(compressed).Encode(snapshot); err != nil {
			_ = compressed.Close()
			return err
		}
		return compressed.Close()
	}); err != nil {
		return model.SnapshotRecord{}, err
	}

	record := recordFromSnapshot(snapshot)
	if stat, err := os.Stat(payloadPath); err == nil {
		record.CompressedBytes = stat.Size()
	}
	record.PayloadAvailable = true

	index, err := s.LoadIndex()
	if err != nil {
		return model.SnapshotRecord{}, err
	}
	kept := index.Records[:0]
	for _, existing := range index.Records {
		if existing.SnapshotID != record.SnapshotID {
			kept = append(kept, existing)
		}
	}
	index.Records = append(kept, record)
	sortRecords(index.Records)
	if err := s.writeIndex(index); err != nil {
		return model.SnapshotRecord{}, err
	}
	if retention > 0 {
		if err := s.Prune(snapshot.Header.RootID, retention); err != nil {
			return model.SnapshotRecord{}, err
		}
	}
	return record, nil
}

func (s Store) Load(snapshotID string) (model.Snapshot, error) {
	file, err := os.Open(s.snapshotPath(snapshotID))
	if errors.Is(err, os.ErrNotExist) {
		return model.Snapshot{}, fmt.Errorf("%w: %s", ErrSnapshotPayloadMissing, snapshotID)
	}
	if err != nil {
		return model.Snapshot{}, err
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return model.Snapshot{}, fmt.Errorf("open snapshot payload: %w", err)
	}
	defer compressed.Close()

	var snapshot model.Snapshot
	if err := json.NewDecoder(compressed).Decode(&snapshot); err != nil {
		return model.Snapshot{}, fmt.Errorf("read snapshot payload: %w", err)
	}
	if snapshot.SchemaVersion != model.SchemaVersion || snapshot.Header.SchemaVersion != model.SchemaVersion || snapshot.Header.SnapshotID != snapshotID {
		return model.Snapshot{}, errors.New("snapshot identity or schema mismatch")
	}
	return snapshot, nil
}

func (s Store) UpdateDescription(rootID, snapshotID, description string) error {
	if len([]rune(description)) > 500 {
		return errors.New("description exceeds 500 characters")
	}
	index, err := s.LoadIndex()
	if err != nil {
		return err
	}
	found := false
	for i := range index.Records {
		if index.Records[i].RootID == rootID && index.Records[i].SnapshotID == snapshotID {
			index.Records[i].Description = description
			found = true
			break
		}
	}
	if !found {
		return os.ErrNotExist
	}
	return s.writeIndex(index)
}

func (s Store) Delete(rootID, snapshotID string) error {
	index, err := s.LoadIndex()
	if err != nil {
		return err
	}
	kept := index.Records[:0]
	found := false
	for _, record := range index.Records {
		if record.RootID == rootID && record.SnapshotID == snapshotID {
			found = true
			continue
		}
		kept = append(kept, record)
	}
	if !found {
		return os.ErrNotExist
	}
	index.Records = kept
	if err := s.writeIndex(index); err != nil {
		return err
	}
	if err := os.Remove(s.snapshotPath(snapshotID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s Store) DeleteRoot(rootID string) error {
	index, err := s.LoadIndex()
	if err != nil {
		return err
	}
	kept := index.Records[:0]
	removed := make([]model.SnapshotRecord, 0)
	for _, record := range index.Records {
		if record.RootID == rootID {
			removed = append(removed, record)
			continue
		}
		kept = append(kept, record)
	}
	index.Records = kept
	if err := s.writeIndex(index); err != nil {
		return err
	}
	var first error
	for _, record := range removed {
		if err := os.Remove(s.snapshotPath(record.SnapshotID)); err != nil && !errors.Is(err, os.ErrNotExist) && first == nil {
			first = err
		}
	}
	return first
}

func (s Store) Prune(rootID string, retention int) error {
	if retention <= 0 {
		return nil
	}
	index, err := s.LoadIndex()
	if err != nil {
		return err
	}
	rootRecords := make([]model.SnapshotRecord, 0)
	for _, record := range index.Records {
		if record.RootID == rootID {
			rootRecords = append(rootRecords, record)
		}
	}
	if len(rootRecords) <= retention {
		return nil
	}
	removeIDs := make(map[string]bool)
	for _, record := range rootRecords[retention:] {
		removeIDs[record.SnapshotID] = true
	}
	kept := index.Records[:0]
	for _, record := range index.Records {
		if !removeIDs[record.SnapshotID] {
			kept = append(kept, record)
		}
	}
	index.Records = kept
	if err := s.writeIndex(index); err != nil {
		return err
	}
	var first error
	for snapshotID := range removeIDs {
		if err := os.Remove(s.snapshotPath(snapshotID)); err != nil && !errors.Is(err, os.ErrNotExist) && first == nil {
			first = err
		}
	}
	return first
}

// Repair recovers complete payloads that were written before an interrupted
// index update. Missing payloads stay indexed so the UI can report them.
func (s Store) Repair() (model.HistoryIndex, error) {
	index, err := s.LoadIndex()
	if err != nil {
		return model.HistoryIndex{}, err
	}
	referenced := make(map[string]bool, len(index.Records))
	for _, record := range index.Records {
		referenced[record.SnapshotID] = true
	}
	files, err := filepath.Glob(filepath.Join(s.historyDir(), "*.snapshot"))
	if err != nil {
		return model.HistoryIndex{}, err
	}
	changed := false
	for _, path := range files {
		id := strings.TrimSuffix(filepath.Base(path), ".snapshot")
		if referenced[id] {
			continue
		}
		snapshot, loadErr := s.Load(id)
		if loadErr != nil {
			continue
		}
		record := recordFromSnapshot(snapshot)
		if stat, statErr := os.Stat(path); statErr == nil {
			record.CompressedBytes = stat.Size()
		}
		record.PayloadAvailable = true
		index.Records = append(index.Records, record)
		changed = true
	}
	if changed {
		sortRecords(index.Records)
		if err := s.writeIndex(index); err != nil {
			return model.HistoryIndex{}, err
		}
	}
	return s.LoadIndex()
}

func (s Store) writeIndex(index model.HistoryIndex) error {
	index.SchemaVersion = model.SchemaVersion
	sortRecords(index.Records)
	return atomicfile.Write(s.indexPath(), 0o600, func(file *os.File) error {
		encoder := json.NewEncoder(file)
		encoder.SetIndent("", "  ")
		return encoder.Encode(index)
	})
}

func recordFromSnapshot(snapshot model.Snapshot) model.SnapshotRecord {
	title := snapshot.Header.DisplayTitle
	if title == "" {
		title = filepath.Base(filepath.Clean(snapshot.Header.RootPathAtCapture))
	}
	return model.SnapshotRecord{
		SnapshotID: snapshot.Header.SnapshotID, RootID: snapshot.Header.RootID,
		RootPath: snapshot.Header.RootPathAtCapture, DisplayTitle: title,
		CompletedAtUTC: snapshot.Header.CompletedAtUTC, Trigger: snapshot.Header.Trigger,
		Description: snapshot.Header.Description, FileCount: snapshot.Header.FileCount,
		DirectoryCount: snapshot.Header.DirectoryCount, OtherCount: snapshot.Header.OtherCount,
		TotalFileBytes: snapshot.Header.TotalFileBytes, WarningCount: len(snapshot.Header.ScanWarnings),
		PayloadAvailable: true,
	}
}

func sortRecords(records []model.SnapshotRecord) {
	sort.SliceStable(records, func(i, j int) bool {
		if !records[i].CompletedAtUTC.Equal(records[j].CompletedAtUTC) {
			return records[i].CompletedAtUTC.After(records[j].CompletedAtUTC)
		}
		return records[i].SnapshotID < records[j].SnapshotID
	})
}
