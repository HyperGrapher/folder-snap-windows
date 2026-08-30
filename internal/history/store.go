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
	"sync"
	"time"

	"foldersnap/internal/atomicfile"
	"foldersnap/internal/model"
	"foldersnap/internal/pathutil"
)

var ErrSnapshotPayloadMissing = errors.New("snapshot payload is missing")
var ErrHistoryIndexCorrupt = errors.New("history index is corrupt")

const maxDecodedSnapshotBytes = 1 << 30

type Store struct {
	DataDir string
	mu      sync.RWMutex
}

type quarantinedPayload struct{ original, quarantine string }

func (s *Store) historyDir() string { return filepath.Join(s.DataDir, "History") }
func (s *Store) indexPath() string  { return filepath.Join(s.historyDir(), "index.json") }
func (s *Store) snapshotPath(snapshotID string) string {
	return filepath.Join(s.historyDir(), snapshotID+".snapshot")
}

func (s *Store) LoadIndex() (model.HistoryIndex, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadIndex()
}

func (s *Store) loadIndex() (model.HistoryIndex, error) {
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
		return model.HistoryIndex{}, fmt.Errorf("%w: %v", ErrHistoryIndexCorrupt, err)
	}
	if index.SchemaVersion != model.SchemaVersion {
		return model.HistoryIndex{}, fmt.Errorf("unsupported history schema %d", index.SchemaVersion)
	}
	seen := make(map[string]bool, len(index.Records))
	for _, record := range index.Records {
		if err := pathutil.ValidateStorageID(record.SnapshotID); err != nil {
			return model.HistoryIndex{}, fmt.Errorf("unsafe snapshot ID: %w", errors.Join(ErrHistoryIndexCorrupt, err))
		}
		if err := pathutil.ValidateStorageID(record.RootID); err != nil {
			return model.HistoryIndex{}, fmt.Errorf("unsafe root ID: %w", errors.Join(ErrHistoryIndexCorrupt, err))
		}
		if seen[record.SnapshotID] {
			return model.HistoryIndex{}, fmt.Errorf("%w: duplicate snapshot ID %s", ErrHistoryIndexCorrupt, record.SnapshotID)
		}
		seen[record.SnapshotID] = true
	}
	sortRecords(index.Records)
	for i := range index.Records {
		_, statErr := os.Stat(s.snapshotPath(index.Records[i].SnapshotID))
		index.Records[i].PayloadAvailable = statErr == nil
	}
	return index, nil
}

func (s *Store) List(rootID string) ([]model.SnapshotRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	index, err := s.loadIndex()
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

func (s *Store) Save(snapshot model.Snapshot, retention int) (model.SnapshotRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if snapshot.SchemaVersion != model.SchemaVersion || snapshot.Header.SchemaVersion != model.SchemaVersion {
		return model.SnapshotRecord{}, errors.New("snapshot schema version is invalid")
	}
	if snapshot.Header.SnapshotID == "" || snapshot.Header.RootID == "" {
		return model.SnapshotRecord{}, errors.New("snapshot identity is missing")
	}
	if err := pathutil.ValidateStorageID(snapshot.Header.SnapshotID); err != nil {
		return model.SnapshotRecord{}, err
	}
	if err := pathutil.ValidateStorageID(snapshot.Header.RootID); err != nil {
		return model.SnapshotRecord{}, err
	}
	if err := validateEntryPaths(snapshot.Entries); err != nil {
		return model.SnapshotRecord{}, err
	}
	index, err := s.loadIndex()
	if err != nil {
		return model.SnapshotRecord{}, err
	}
	for _, existing := range index.Records {
		if existing.SnapshotID == snapshot.Header.SnapshotID {
			return model.SnapshotRecord{}, fmt.Errorf("snapshot ID already exists: %s", snapshot.Header.SnapshotID)
		}
	}

	payloadPath := s.snapshotPath(snapshot.Header.SnapshotID)
	if err := atomicfile.Write(payloadPath, 0o600, func(file *os.File) error {
		compressed, err := gzip.NewWriterLevel(file, gzip.BestSpeed)
		if err != nil {
			return err
		}
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

	index.Records = append(index.Records, record)
	sortRecords(index.Records)
	if err := s.writeIndex(index); err != nil {
		return model.SnapshotRecord{}, err
	}
	if retention > 0 {
		if err := s.prune(snapshot.Header.RootID, retention); err != nil {
			return model.SnapshotRecord{}, err
		}
	}
	return record, nil
}

func (s *Store) Load(snapshotID string) (model.Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.load(snapshotID)
}

func (s *Store) load(snapshotID string) (model.Snapshot, error) {
	if err := pathutil.ValidateStorageID(snapshotID); err != nil {
		return model.Snapshot{}, err
	}
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
	if err := json.NewDecoder(io.LimitReader(compressed, maxDecodedSnapshotBytes+1)).Decode(&snapshot); err != nil {
		return model.Snapshot{}, fmt.Errorf("read snapshot payload: %w", err)
	}
	if snapshot.SchemaVersion != model.SchemaVersion || snapshot.Header.SchemaVersion != model.SchemaVersion || snapshot.Header.SnapshotID != snapshotID {
		return model.Snapshot{}, errors.New("snapshot identity or schema mismatch")
	}
	if err := pathutil.ValidateStorageID(snapshot.Header.RootID); err != nil {
		return model.Snapshot{}, err
	}
	entriesSorted, err := validateAndCheckEntryPaths(snapshot.Entries)
	if err != nil {
		return model.Snapshot{}, err
	}
	snapshot.EntriesSorted = entriesSorted
	return snapshot, nil
}

func (s *Store) UpdateDescription(rootID, snapshotID, description string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateIDs(rootID, snapshotID); err != nil {
		return err
	}
	if len([]rune(description)) > 500 {
		return errors.New("description exceeds 500 characters")
	}
	index, err := s.loadIndex()
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

func (s *Store) Delete(rootID, snapshotID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateIDs(rootID, snapshotID); err != nil {
		return err
	}
	index, err := s.loadIndex()
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
	quarantined, err := s.quarantinePayloads([]string{snapshotID})
	if err != nil {
		return err
	}
	if err := s.writeIndex(index); err != nil {
		return errors.Join(err, s.restoreQuarantined(quarantined))
	}
	return removeQuarantined(quarantined)
}

func (s *Store) DeleteRoot(rootID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := pathutil.ValidateStorageID(rootID); err != nil {
		return err
	}
	index, err := s.loadIndex()
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
	removedIDs := make([]string, 0, len(removed))
	for _, record := range removed {
		removedIDs = append(removedIDs, record.SnapshotID)
	}
	quarantined, err := s.quarantinePayloads(removedIDs)
	if err != nil {
		return err
	}
	if err := s.writeIndex(index); err != nil {
		return errors.Join(err, s.restoreQuarantined(quarantined))
	}
	return removeQuarantined(quarantined)
}

func (s *Store) Prune(rootID string, retention int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.prune(rootID, retention)
}

func (s *Store) prune(rootID string, retention int) error {
	if err := pathutil.ValidateStorageID(rootID); err != nil {
		return err
	}
	if retention <= 0 {
		return nil
	}
	index, err := s.loadIndex()
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
	idsToRemove := make([]string, 0, len(removeIDs))
	for snapshotID := range removeIDs {
		idsToRemove = append(idsToRemove, snapshotID)
	}
	quarantined, err := s.quarantinePayloads(idsToRemove)
	if err != nil {
		return err
	}
	if err := s.writeIndex(index); err != nil {
		return errors.Join(err, s.restoreQuarantined(quarantined))
	}
	return removeQuarantined(quarantined)
}

// Repair recovers complete payloads that were written before an interrupted
// index update. Missing payloads stay indexed so the UI can report them.
func (s *Store) Repair() (model.HistoryIndex, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.loadIndex()
	rebuilding := errors.Is(err, ErrHistoryIndexCorrupt)
	if rebuilding {
		backup := fmt.Sprintf("%s.corrupt-%s", s.indexPath(), time.Now().UTC().Format("20060102T150405.000000000Z"))
		if copyErr := copyFile(s.indexPath(), backup); copyErr != nil {
			return model.HistoryIndex{}, errors.Join(err, copyErr)
		}
		index = model.HistoryIndex{SchemaVersion: model.SchemaVersion}
		if restoreErr := s.restoreAllQuarantined(); restoreErr != nil {
			return model.HistoryIndex{}, restoreErr
		}
	} else if err != nil {
		return model.HistoryIndex{}, err
	}
	if err := s.repairQuarantined(index); err != nil {
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
	changed := rebuilding
	for _, path := range files {
		id := strings.TrimSuffix(filepath.Base(path), ".snapshot")
		if referenced[id] {
			continue
		}
		snapshot, loadErr := s.load(id)
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
	return s.loadIndex()
}

func (s *Store) restoreAllQuarantined() error {
	paths, err := filepath.Glob(filepath.Join(s.historyDir(), "*.snapshot.deleting"))
	if err != nil {
		return err
	}
	for _, quarantine := range paths {
		original := strings.TrimSuffix(quarantine, ".deleting")
		if _, statErr := os.Stat(original); statErr == nil {
			if err := os.Remove(quarantine); err != nil {
				return err
			}
			continue
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		if err := os.Rename(quarantine, original); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func (s *Store) quarantinePayloads(snapshotIDs []string) ([]quarantinedPayload, error) {
	ids := append([]string(nil), snapshotIDs...)
	sort.Strings(ids)
	result := make([]quarantinedPayload, 0, len(ids))
	for _, snapshotID := range ids {
		if err := pathutil.ValidateStorageID(snapshotID); err != nil {
			s.restoreQuarantined(result)
			return nil, err
		}
		original := s.snapshotPath(snapshotID)
		quarantine := original + ".deleting"
		if _, err := os.Stat(original); errors.Is(err, os.ErrNotExist) {
			if _, tombErr := os.Stat(quarantine); tombErr == nil {
				result = append(result, quarantinedPayload{original, quarantine})
			}
			continue
		} else if err != nil {
			s.restoreQuarantined(result)
			return nil, err
		}
		if err := os.Remove(quarantine); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.restoreQuarantined(result)
			return nil, err
		}
		if err := os.Rename(original, quarantine); err != nil {
			s.restoreQuarantined(result)
			return nil, err
		}
		result = append(result, quarantinedPayload{original, quarantine})
	}
	return result, nil
}

func (s *Store) restoreQuarantined(payloads []quarantinedPayload) error {
	var first error
	for index := len(payloads) - 1; index >= 0; index-- {
		payload := payloads[index]
		if _, err := os.Stat(payload.quarantine); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if err := os.Rename(payload.quarantine, payload.original); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func removeQuarantined(payloads []quarantinedPayload) error {
	var first error
	for _, payload := range payloads {
		if err := os.Remove(payload.quarantine); err != nil && !errors.Is(err, os.ErrNotExist) && first == nil {
			first = err
		}
	}
	return first
}

func (s *Store) repairQuarantined(index model.HistoryIndex) error {
	referenced := make(map[string]bool, len(index.Records))
	for _, record := range index.Records {
		referenced[record.SnapshotID] = true
	}
	paths, err := filepath.Glob(filepath.Join(s.historyDir(), "*.snapshot.deleting"))
	if err != nil {
		return err
	}
	for _, quarantine := range paths {
		name := filepath.Base(quarantine)
		id := strings.TrimSuffix(name, ".snapshot.deleting")
		if err := pathutil.ValidateStorageID(id); err != nil {
			continue
		}
		original := s.snapshotPath(id)
		if referenced[id] {
			if _, statErr := os.Stat(original); errors.Is(statErr, os.ErrNotExist) {
				if err := os.Rename(quarantine, original); err != nil {
					return err
				}
				continue
			}
		}
		if err := os.Remove(quarantine); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *Store) writeIndex(index model.HistoryIndex) error {
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

func validateIDs(rootID, snapshotID string) error {
	if err := pathutil.ValidateStorageID(rootID); err != nil {
		return err
	}
	return pathutil.ValidateStorageID(snapshotID)
}

func validateEntryPaths(entries []model.SnapshotEntry) error {
	_, err := validateAndCheckEntryPaths(entries)
	return err
}

func validateAndCheckEntryPaths(entries []model.SnapshotEntry) (bool, error) {
	sorted := true
	for index, entry := range entries {
		normalized, err := pathutil.NormalizeRelative(entry.RelativePath)
		if err != nil || normalized == "" || normalized != entry.RelativePath {
			return false, fmt.Errorf("snapshot contains unsafe entry path %q", entry.RelativePath)
		}
		if index > 0 && entries[index-1].RelativePath > entry.RelativePath {
			sorted = false
		}
	}
	return sorted, nil
}
