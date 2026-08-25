package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	configpkg "foldersnap/internal/config"
	diffpkg "foldersnap/internal/diff"
	"foldersnap/internal/history"
	"foldersnap/internal/ids"
	"foldersnap/internal/model"
	"foldersnap/internal/pathutil"
	"foldersnap/internal/scan"
	"foldersnap/internal/scheduler"
)

type EventType string

const (
	EventScanStarted  EventType = "scan_started"
	EventScanProgress EventType = "scan_progress"
	EventScanComplete EventType = "scan_complete"
	EventScanFailed   EventType = "scan_failed"
	EventConfigChange EventType = "config_changed"
)

type Event struct {
	Type     EventType
	RootID   string
	Items    int64
	Path     string
	Snapshot *model.SnapshotRecord
	Trigger  model.SnapshotTrigger
	Err      error
}

type rootJob struct {
	running          bool
	pendingManual    bool
	pendingScheduled bool
	cancel           context.CancelFunc
}

type Service struct {
	mu          sync.RWMutex
	config      model.Config
	configStore configpkg.Store
	history     history.Store
	scanner     scan.Scanner
	logger      *log.Logger
	events      chan Event
	jobs        map[string]*rootJob
	scanSlots   chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
	wake        chan struct{}
}

func New(dataDir string, logger *log.Logger) (*Service, error) {
	configStore := configpkg.Store{DataDir: dataDir}
	cfg, err := configStore.Load()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{
		config: cfg, configStore: configStore, history: history.Store{DataDir: dataDir},
		scanner: scan.Scanner{}, logger: logger, events: make(chan Event, 256), jobs: make(map[string]*rootJob),
		scanSlots: make(chan struct{}, 2), ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1),
	}
	_, _ = service.history.Repair()
	return service, nil
}

func (s *Service) StartScheduler()      { go s.schedulerLoop() }
func (s *Service) Events() <-chan Event { return s.events }
func (s *Service) DataDir() string      { return s.configStore.DataDir }

func (s *Service) Close() {
	s.cancel()
	s.mu.Lock()
	for _, job := range s.jobs {
		if job.cancel != nil {
			job.cancel()
		}
	}
	s.mu.Unlock()
}

func (s *Service) Config() model.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copy := s.config
	copy.Roots = append([]model.WatchedRoot(nil), s.config.Roots...)
	return copy
}

func (s *Service) AddRoot(path string) (model.WatchedRoot, error) {
	info, err := os.Stat(path)
	if err != nil {
		return model.WatchedRoot{}, err
	}
	if !info.IsDir() {
		return model.WatchedRoot{}, errors.New("selected path is not a directory")
	}
	normalized, err := pathutil.NormalizeRoot(path)
	if err != nil {
		return model.WatchedRoot{}, err
	}
	abs, _ := filepath.Abs(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, root := range s.config.Roots {
		if root.NormalizedPath == normalized {
			return model.WatchedRoot{}, errors.New("this folder is already watched")
		}
	}
	root := model.WatchedRoot{
		RootID: ids.New(), DisplayName: filepath.Base(filepath.Clean(abs)), Path: abs, NormalizedPath: normalized,
		Schedule: model.Schedule{Kind: model.ScheduleManual}, IgnoreRules: append([]string(nil), s.config.DefaultIgnoreRules...),
		Retention: s.config.DefaultRetention,
	}
	if root.DisplayName == "." || root.DisplayName == string(filepath.Separator) {
		root.DisplayName = abs
	}
	s.config.Roots = append(s.config.Roots, root)
	if err := s.configStore.Save(s.config); err != nil {
		s.config.Roots = s.config.Roots[:len(s.config.Roots)-1]
		return model.WatchedRoot{}, err
	}
	s.emit(Event{Type: EventConfigChange, RootID: root.RootID})
	return root, nil
}

func (s *Service) UpdateRoot(updated model.WatchedRoot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.config.Roots {
		if s.config.Roots[i].RootID == updated.RootID {
			s.config.Roots[i] = updated
			if err := s.configStore.Save(s.config); err != nil {
				return err
			}
			s.signalScheduler()
			s.emit(Event{Type: EventConfigChange, RootID: updated.RootID})
			return nil
		}
	}
	return os.ErrNotExist
}

func (s *Service) UpdateConfig(updated model.Config) error {
	if updated.DefaultRetention != 10 && updated.DefaultRetention != 25 && updated.DefaultRetention != 50 && updated.DefaultRetention != 100 && updated.DefaultRetention != 0 {
		return errors.New("unsupported default retention")
	}
	updated.SchemaVersion = model.SchemaVersion
	s.mu.Lock()
	defer s.mu.Unlock()
	updated.Roots = append([]model.WatchedRoot(nil), s.config.Roots...)
	if err := s.configStore.Save(updated); err != nil {
		return err
	}
	s.config = updated
	s.emit(Event{Type: EventConfigChange})
	return nil
}

func (s *Service) ClearHistory(rootID string) error {
	s.mu.RLock()
	job := s.jobs[rootID]
	running := job != nil && job.running
	s.mu.RUnlock()
	if running {
		return errors.New("cannot delete history while a scan is running")
	}
	if err := s.history.DeleteRoot(rootID); err != nil {
		return err
	}
	s.mu.Lock()
	for i := range s.config.Roots {
		if s.config.Roots[i].RootID == rootID {
			s.config.Roots[i].LastSnapshotUTC = time.Time{}
			s.config.Roots[i].LastScanError = ""
			break
		}
	}
	err := s.configStore.Save(s.config)
	s.mu.Unlock()
	s.emit(Event{Type: EventConfigChange, RootID: rootID})
	return err
}

func (s *Service) Root(rootID string) (model.WatchedRoot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, root := range s.config.Roots {
		if root.RootID == rootID {
			return root, true
		}
	}
	return model.WatchedRoot{}, false
}

func (s *Service) ListSnapshots(rootID string) ([]model.SnapshotRecord, error) {
	return s.history.List(rootID)
}
func (s *Service) LoadSnapshot(rootID, snapshotID string) (model.Snapshot, error) {
	snapshot, err := s.history.Load(snapshotID)
	if err != nil {
		return model.Snapshot{}, err
	}
	if snapshot.Header.RootID != rootID {
		return model.Snapshot{}, errors.New("snapshot belongs to a different folder")
	}
	return snapshot, nil
}
func (s *Service) UpdateDescription(rootID, snapshotID, value string) error {
	return s.history.UpdateDescription(rootID, snapshotID, value)
}
func (s *Service) DeleteSnapshot(rootID, snapshotID string) error {
	return s.history.Delete(rootID, snapshotID)
}
func (s *Service) Compare(before, after model.Snapshot) (model.DiffResult, error) {
	return (diffpkg.Engine{}).Compare(before, after)
}

func (s *Service) RequestSnapshot(rootID string, trigger model.SnapshotTrigger) error {
	root, ok := s.Root(rootID)
	if !ok || root.Archived {
		return errors.New("watched root is unavailable or archived")
	}
	s.mu.Lock()
	job := s.jobs[rootID]
	if job == nil {
		job = &rootJob{}
		s.jobs[rootID] = job
	}
	if job.running {
		if trigger == model.TriggerManual {
			job.pendingManual = true
		} else {
			job.pendingScheduled = true
		}
		s.mu.Unlock()
		return nil
	}
	job.running = true
	s.mu.Unlock()
	go s.runScans(rootID, trigger)
	return nil
}

func (s *Service) runScans(rootID string, trigger model.SnapshotTrigger) {
	slotHeld := false
	defer func() {
		if recovered := recover(); recovered != nil {
			if slotHeld {
				<-s.scanSlots
			}
			err := fmt.Errorf("scan worker panic: %v", recovered)
			s.logger.Printf("root=%s %v", rootID, err)
			s.mu.Lock()
			if job := s.jobs[rootID]; job != nil {
				job.running, job.cancel = false, nil
			}
			s.mu.Unlock()
			s.emit(Event{Type: EventScanFailed, RootID: rootID, Trigger: trigger, Err: err})
		}
	}()
	for {
		root, ok := s.Root(rootID)
		if !ok {
			break
		}
		ctx, cancel := context.WithCancel(s.ctx)
		s.mu.Lock()
		if job := s.jobs[rootID]; job != nil {
			job.cancel = cancel
		}
		s.mu.Unlock()
		acquired := false
		select {
		case s.scanSlots <- struct{}{}:
			acquired = true
		case <-ctx.Done():
		}
		if !acquired {
			cancel()
			break
		}
		slotHeld = true
		s.emit(Event{Type: EventScanStarted, RootID: rootID, Trigger: trigger})
		snapshot, err := s.scanner.Scan(ctx, scan.Request{RootID: rootID, RootPath: root.Path, DisplayTitle: root.DisplayName, Trigger: trigger, IgnoreRules: root.IgnoreRules, Progress: func(items int64, path string) {
			s.emit(Event{Type: EventScanProgress, RootID: rootID, Trigger: trigger, Items: items, Path: path})
		}})
		<-s.scanSlots
		slotHeld = false
		cancel()
		if err == nil {
			_, err = s.history.Save(snapshot, root.Retention)
		}
		if err != nil {
			s.logger.Printf("scan root=%s trigger=%s failed: %v", rootID, trigger, err)
			s.setRootScanResult(rootID, time.Time{}, err)
			s.emit(Event{Type: EventScanFailed, RootID: rootID, Trigger: trigger, Err: err})
		} else {
			s.logger.Printf("scan root=%s trigger=%s entries=%d warnings=%d", rootID, trigger, len(snapshot.Entries), len(snapshot.Header.ScanWarnings))
			s.setRootScanResult(rootID, snapshot.Header.CompletedAtUTC, nil)
			records, _ := s.history.List(rootID)
			var summary *model.SnapshotRecord
			if len(records) > 0 {
				value := records[0]
				summary = &value
			}
			s.emit(Event{Type: EventScanComplete, RootID: rootID, Trigger: trigger, Snapshot: summary})
		}

		s.mu.Lock()
		job := s.jobs[rootID]
		next := model.SnapshotTrigger("")
		if job != nil && job.pendingManual {
			job.pendingManual = false
			next = model.TriggerManual
		} else if job != nil && job.pendingScheduled {
			job.pendingScheduled = false
			next = model.TriggerScheduled
		}
		if next == "" {
			if job != nil {
				job.running = false
				job.cancel = nil
			}
			s.mu.Unlock()
			break
		}
		s.mu.Unlock()
		trigger = next
	}
}

func (s *Service) setRootScanResult(rootID string, completed time.Time, scanErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.config.Roots {
		if s.config.Roots[i].RootID != rootID {
			continue
		}
		if scanErr != nil {
			s.config.Roots[i].LastScanError = scanErr.Error()
		} else {
			s.config.Roots[i].LastScanError = ""
			s.config.Roots[i].LastSnapshotUTC = completed
		}
		_ = s.configStore.Save(s.config)
		break
	}
}

func (s *Service) schedulerLoop() {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
		case <-timer.C:
		}
		now := time.Now()
		cfg := s.Config()
		for _, root := range cfg.Roots {
			if root.Archived || root.Schedule.Kind == model.ScheduleManual {
				continue
			}
			if root.Schedule.NextDueAtUTC.IsZero() {
				next, err := scheduler.NextDue(root.Schedule, now)
				if err == nil {
					root.Schedule.NextDueAtUTC = next.UTC()
					_ = s.UpdateRoot(root)
				}
				continue
			}
			if scheduler.IsCatchUpDue(root.Schedule, now) {
				_ = s.RequestSnapshot(root.RootID, model.TriggerScheduled)
				due := root.Schedule.NextDueAtUTC.In(now.Location())
				next, err := scheduler.AdvanceToFuture(root.Schedule, due, now)
				if err == nil {
					root.Schedule.NextDueAtUTC = next.UTC()
					_ = s.UpdateRoot(root)
				}
			}
		}
		timer.Reset(15 * time.Second)
	}
}

func (s *Service) signalScheduler() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Service) emit(event Event) {
	select {
	case s.events <- event:
	default:
		s.logger.Printf("event queue full; dropped %s", event.Type)
	}
}

func SortRoots(roots []model.WatchedRoot) {
	sort.SliceStable(roots, func(i, j int) bool {
		if roots[i].Archived != roots[j].Archived {
			return !roots[i].Archived
		}
		return roots[i].DisplayName < roots[j].DisplayName
	})
}

func FormatError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%v", err)
}
