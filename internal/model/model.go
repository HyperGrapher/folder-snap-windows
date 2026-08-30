package model

import "time"

const SchemaVersion = 2

type EntryType string

const (
	EntryFile      EntryType = "file"
	EntryDirectory EntryType = "directory"
	EntryReparse   EntryType = "reparse"
	EntryOther     EntryType = "other"
)

type SnapshotTrigger string

const (
	TriggerManual    SnapshotTrigger = "manual"
	TriggerScheduled SnapshotTrigger = "scheduled"
)

type ScanWarning struct {
	Path      string `json:"path"`
	Operation string `json:"operation"`
	Category  string `json:"category"`
	Message   string `json:"message"`
}

type IgnoreConfigSnapshot struct {
	Rules []string `json:"rules"`
	Hash  string   `json:"hash"`
}

type SnapshotHeader struct {
	SchemaVersion     int                  `json:"schemaVersion"`
	SnapshotID        string               `json:"snapshotId"`
	RootID            string               `json:"rootId"`
	RootPathAtCapture string               `json:"rootPathAtCapture"`
	DisplayTitle      string               `json:"displayTitle"`
	StartedAtUTC      time.Time            `json:"startedAtUtc"`
	CompletedAtUTC    time.Time            `json:"completedAtUtc"`
	Trigger           SnapshotTrigger      `json:"trigger"`
	Description       string               `json:"description,omitempty"`
	FileCount         int64                `json:"fileCount"`
	DirectoryCount    int64                `json:"directoryCount"`
	OtherCount        int64                `json:"otherCount"`
	TotalFileBytes    int64                `json:"totalFileBytes"`
	IgnoreConfig      IgnoreConfigSnapshot `json:"ignoreConfig"`
	ScanWarnings      []ScanWarning        `json:"scanWarnings,omitempty"`
}

type SnapshotEntry struct {
	RelativePath   string    `json:"path"`
	DisplayPath    string    `json:"displayPath"`
	Type           EntryType `json:"type"`
	Size           int64     `json:"size"`
	ModifiedUnixNs int64     `json:"modifiedNs"`
	CreatedUnixNs  int64     `json:"createdNs,omitempty"`
	Attributes     uint32    `json:"attributes,omitempty"`
	LinkTarget     string    `json:"linkTarget,omitempty"`
}

type Snapshot struct {
	SchemaVersion int             `json:"schemaVersion"`
	Header        SnapshotHeader  `json:"header"`
	Entries       []SnapshotEntry `json:"entries"`
	EntriesSorted bool            `json:"-"`
}

// SnapshotRecord is the lightweight, globally indexed history metadata. It
// mirrors the role of SnapshotRecord in the macOS implementation; the full
// entry payload remains in a separate compressed .snapshot file.
type SnapshotRecord struct {
	SnapshotID       string          `json:"snapshotId"`
	RootID           string          `json:"rootId"`
	RootPath         string          `json:"rootPath"`
	DisplayTitle     string          `json:"displayTitle"`
	CompletedAtUTC   time.Time       `json:"completedAtUtc"`
	Trigger          SnapshotTrigger `json:"trigger"`
	Description      string          `json:"description,omitempty"`
	FileCount        int64           `json:"fileCount"`
	DirectoryCount   int64           `json:"directoryCount"`
	OtherCount       int64           `json:"otherCount"`
	TotalFileBytes   int64           `json:"totalFileBytes"`
	WarningCount     int             `json:"warningCount"`
	CompressedBytes  int64           `json:"compressedBytes"`
	PayloadAvailable bool            `json:"-"`
}

type HistoryIndex struct {
	SchemaVersion int              `json:"schemaVersion"`
	Records       []SnapshotRecord `json:"records"`
}

type ScheduleKind string

const (
	ScheduleManual   ScheduleKind = "manual"
	ScheduleInterval ScheduleKind = "interval"
	ScheduleDaily    ScheduleKind = "daily"
	ScheduleWeekly   ScheduleKind = "weekly"
	ScheduleMonthly  ScheduleKind = "monthly"
)

type Schedule struct {
	Kind          ScheduleKind `json:"kind"`
	IntervalHours int          `json:"intervalHours,omitempty"`
	Hour          int          `json:"hour,omitempty"`
	Minute        int          `json:"minute,omitempty"`
	Weekday       time.Weekday `json:"weekday,omitempty"`
	DayOfMonth    int          `json:"dayOfMonth,omitempty"`
	NextDueAtUTC  time.Time    `json:"nextDueAtUtc,omitempty"`
}

type WatchedRoot struct {
	RootID          string    `json:"rootId"`
	DisplayName     string    `json:"displayName"`
	Path            string    `json:"path"`
	NormalizedPath  string    `json:"normalizedPath"`
	Archived        bool      `json:"archived"`
	Schedule        Schedule  `json:"schedule"`
	IgnoreRules     []string  `json:"ignoreRules"`
	Retention       int       `json:"retention"`
	LastSnapshotUTC time.Time `json:"lastSnapshotUtc,omitempty"`
	LastScanError   string    `json:"lastScanError,omitempty"`
}

type Config struct {
	SchemaVersion          int           `json:"schemaVersion"`
	Roots                  []WatchedRoot `json:"roots"`
	DefaultRetention       int           `json:"defaultRetention"`
	DefaultIgnoreRules     []string      `json:"defaultIgnoreRules"`
	LaunchAtStartup        bool          `json:"launchAtStartup"`
	NotifyScheduledSuccess bool          `json:"notifyScheduledSuccess"`
	CloseToTray            bool          `json:"closeToTray"`
}

func DefaultConfig() Config {
	return Config{
		SchemaVersion:      SchemaVersion,
		DefaultRetention:   50,
		DefaultIgnoreRules: []string{"node_modules/", "build/", ".git/"},
		CloseToTray:        true,
	}
}

type ChangeType string

const (
	ChangeAdded     ChangeType = "added"
	ChangeRemoved   ChangeType = "removed"
	ChangeModified  ChangeType = "modified"
	ChangeUnchanged ChangeType = "unchanged"
)

type DiffEntry struct {
	Path            string         `json:"path"`
	DisplayPath     string         `json:"displayPath"`
	Change          ChangeType     `json:"change"`
	Subtype         string         `json:"subtype,omitempty"`
	Before          *SnapshotEntry `json:"before,omitempty"`
	After           *SnapshotEntry `json:"after,omitempty"`
	Uncertain       bool           `json:"uncertain,omitempty"`
	ScopeDifference bool           `json:"scopeDifference,omitempty"`
}

type DiffSummary struct {
	Added, Removed, Modified, Unchanged int
	Uncertain, ScopeDifference          int
	AddedBytes, RemovedBytes            int64
	ModifiedBeforeBytes                 int64
	ModifiedAfterBytes                  int64
	NetBytes                            int64
}

type DiffResult struct {
	BeforeID           string
	AfterID            string
	BeforeWarnings     int
	AfterWarnings      int
	IgnoreRulesChanged bool
	Entries            []DiffEntry
	Summary            DiffSummary
}
