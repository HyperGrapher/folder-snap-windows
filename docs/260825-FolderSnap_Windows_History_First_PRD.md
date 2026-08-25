# FolderSnap — Windows History-First Product Requirements Document

**Product:** FolderSnap  
**Primary platform:** Windows 10 and Windows 11, x64  
**Implementation language:** Go  
**Desktop UI:** FLTK through `github.com/pwiecz/go-fltk`  
**Concurrency:** Go goroutines and channels for scanning, scheduling, persistence, diff computation, and cleanup work  
**Primary product feature:** Snapshot History + Compare  
**Secondary/future feature:** HTML export  
**Document status:** Implementation PRD for a coding agent  

---

## 0. Purpose of This Document

This PRD defines the Windows version of FolderSnap as a **history-first folder snapshot and comparison utility**.

The previous Windows concept had many of the correct individual features, but its product hierarchy treated watched-folder management as the main experience and History Compare as another destination. That is not the intended product.

For this Windows edition:

> **The reason FolderSnap exists is to let the user answer “what changed in this folder between these two points in time?” quickly and safely.**

Adding folders, scheduling snapshots, exclusions, retention, and the tray process all exist to make that comparison history useful. HTML export is explicitly not required for the initial implementation.

This PRD also incorporates the strongest ideas proven in the earlier macOS implementation: snapshots grouped by root, immutable local history, atomic persistence, relative-path diffing, added/removed/modified classifications, retention, search/filtering, and rename/move being represented as remove + add.

---

# 1. Product Summary

FolderSnap is a lightweight Windows tray application that records metadata snapshots of user-selected folder trees and lets the user compare any two snapshots from the same root folder.

A snapshot records the state of the folder tree at a point in time. FolderSnap does **not** copy or back up file contents in the first implementation. Its job is to maintain a reliable timeline of folder-tree state.

The primary workflow is:

1. Add a root folder.
2. Take snapshots manually and/or on a schedule.
3. Open FolderSnap and immediately see the latest comparison for that folder.
4. Choose any older and newer snapshots to compare.
5. Review files/folders that were added, removed, or modified.
6. Optionally select items that were added between those snapshots and remove them from the current filesystem, with strong safety checks.

FolderSnap must run quietly in the background, remain usable with very large trees, survive restarts, and never perform a destructive filesystem operation silently.

---

# 2. Product Positioning and Principles

## 2.1 Product Positioning

FolderSnap is **not** a backup application, source-control system, filesystem journal, or synchronization client.

It is best thought of as:

> **A lightweight “time machine for folder structure and file metadata.”**

Typical questions it should answer:

- What files did this installer/tool/project generate since yesterday?
- Which files appeared after I ran a build or script?
- Which files disappeared?
- Which existing files changed?
- What changed in this folder during the last few hours/days/weeks?
- Can I remove only the files that were introduced after a known-good snapshot?

## 2.2 Product Principles

### History first
The default window should lead the user directly to a selected folder's snapshot timeline and comparison, not to a dashboard of settings.

### Safe by default
Historical comparison must never be confused with the current live filesystem. Cleanup actions must verify current state before deleting anything.

### Local and private
Snapshots remain on the local computer. No network service, account, telemetry, or cloud storage is required for v1.

### Metadata, not file backup
Snapshots contain metadata required for history comparison, not file contents.

### Predictable over clever
Do not add fuzzy rename detection, content inference, or automatic restoration in v1. A rename/move is represented as Removed + Added.

### Fast enough to stay enabled
Scanning and diffing should be lightweight enough that users are comfortable leaving FolderSnap in the tray.

### Background work must never freeze the UI
Filesystem scans, compression, persistence, comparison, and deletion/preflight operations run outside the FLTK UI thread.

---

# 3. Goals, Non-Goals, and Scope

## 3.1 V1 Goals

The initial complete version must provide:

- Add/remove/archive watched root folders.
- Manual snapshots.
- Per-folder scheduled snapshots.
- Snapshot descriptions.
- Snapshot deletion and retention.
- Per-folder exclusions with gitignore-like syntax.
- Built-in exclusions for common generated content.
- A history-first main window.
- Compare any two snapshots from the same root.
- Added / Removed / Modified / Unchanged classification.
- Search and filtering of comparison results.
- Size delta and useful summary statistics.
- Incomplete-scan and exclusion-scope warnings.
- Tree-based selection of **Added** items for cleanup.
- Live validation before cleanup.
- Safe deletion using the Windows Recycle Bin by default.
- Background/tray operation.
- Windows startup option.
- Persistent state under the current user's local application-data directory.
- Atomic snapshot/config writes.
- Unit tests around scanning, ignore matching, history storage, diffing, scheduling logic, and cleanup safety.

## 3.2 Explicitly Out of Scope for Initial Implementations

Do **not** block the first usable product on any of the following:

- HTML export.
- CSV/text diff export.
- File-content backup or version restoration.
- Cloud sync.
- Accounts/login.
- Real-time event journaling via Windows filesystem watcher APIs.
- NTFS USN Journal integration.
- File-content hashing by default.
- Fuzzy rename or move detection.
- Comparing snapshots from different watched roots.
- Automatic rollback of modified files.
- File preview/content diff.
- Shell Explorer context-menu integration.
- Windows service mode.
- Multi-user machine-wide configuration.
- Network share correctness guarantees.
- Plugin system.

HTML export may be implemented later as a side feature once History Compare and cleanup are stable.

---

# 4. Target Users and Core Use Cases

## 4.1 Primary User

A technical or power user who wants a lightweight record of changes inside specific folders without putting those folders under source control or running a full backup solution.

Examples:

- Software developer tracking project-parent/Appdata/ProgramData folders, generated artifacts, downloaded assets, design files non-Git content or unwanted dowloaded/generated files by other apps or system that needs to be deleted to prevent SSD bloat.
- User testing software that creates many files.
- User who wants to understand what changed in a workspace over time.
- User who occasionally wants to clean up files introduced after a known point.

## 4.2 Core User Stories

### US-1 — Add a root
As a user, I can choose a folder and start recording its history.

### US-2 — Snapshot now
As a user, I can take a snapshot immediately and continue using the application while it scans.

### US-3 — Automatic history
As a user, I can choose a schedule per watched root so history accumulates automatically.

### US-4 — Compare latest changes quickly
As a user, when I open a folder with at least two snapshots, I immediately see the difference between its latest two snapshots without configuring a comparison every time.

### US-5 — Compare arbitrary points
As a user, I can choose any older snapshot as Before and any newer snapshot as After.

### US-6 — Understand changes
As a user, I can clearly distinguish added, removed, and modified entries and search/filter the results.

### US-7 — Add context
As a user, I can attach a short description to any snapshot.

### US-8 — Clean up newly introduced files
As a user, I can select only files/folders that were Added between two snapshots and ask FolderSnap to remove them from the current folder.

### US-9 — Avoid deleting later work
As a user, if an Added file has changed after the selected After snapshot, FolderSnap must detect that and prevent it from being silently deleted.

### US-10 — Ignore noise
As a user, I can exclude generated or irrelevant paths using familiar ignore patterns.

### US-11 — Background operation
As a user, I can close the main window and keep FolderSnap running in the tray to take scheduled snapshots.

---

# 5. Core Concepts and Terminology

## 5.1 Watched Root

A user-selected root directory registered in FolderSnap.

Each watched root has:

- Stable internal `rootID`.
- Display name.
- Original absolute path.
- Normalized absolute path used for duplicate detection.
- Active/archived state.
- Schedule configuration.
- Ignore configuration.
- Retention configuration.
- Last successful snapshot time.
- Last scan status.

“Watched” in v1 means **registered for snapshotting**. It does not imply continuous filesystem-event monitoring.

## 5.2 Snapshot

An immutable point-in-time record of the entries observed below one watched root.

A snapshot has:

- Stable `snapshotID`.
- `rootID`.
- Scan start time.
- Scan completion time.
- Trigger source: manual, scheduled, or future system-triggered source.
- Optional user description.
- Entry counts and total file bytes.
- Sorted list of snapshot entries.
- Ignore rules/configuration used during the scan.
- Scan warnings and unreadable prefixes.
- Schema version.

Editing a description changes snapshot **metadata**, not the recorded tree contents.

## 5.3 Snapshot Entry

One path observed in a snapshot.

Required fields:

- Relative path using `/` as the persisted separator.
- Display/original-cased relative path.
- Normalized comparison key.
- Entry type.
- File size for regular files.
- Last-write timestamp in UTC.
- Creation timestamp when cheaply available on Windows (stored for identity/preflight help, not used as a normal “modified” signal).
- Relevant Windows file attributes when available.

Entry types for v1:

- regular file
- directory
- symbolic link/reparse-point-like entry
- other/unknown

## 5.4 Diff

A deterministic result from comparing two snapshots of the **same rootID**.

The older snapshot is always `Before`; the newer is always `After`.

Top-level classifications:

- Added
- Removed
- Modified
- Unchanged

Additional state may decorate an entry without becoming a normal change type:

- uncertain because of scan error
- excluded because scope/ignore rules changed
- cleanup eligibility/current-state status

## 5.5 Cleanup

A user-authorized operation that attempts to remove selected items classified as Added between Before and After.

Cleanup is **not historical rollback**. It operates on the current filesystem, therefore it requires live validation.

---

# 6. Windows-Specific Filesystem Rules

Windows filesystem behavior must be explicitly handled rather than treated as POSIX-with-backslashes.

## 6.1 Path Comparison

- Persist relative paths with `/` separators for a stable cross-code representation.
- Preserve original casing for display.
- Use a case-insensitive normalized key for comparison on normal Windows roots.
- Treat `foo\\Bar.txt` and `FOO/bar.txt` as the same path key.
- Normalize `.` and redundant separators.
- Never allow a persisted relative path to escape the watched root using `..`.

## 6.2 Duplicate Watched Roots

Duplicate root detection is case-insensitive and separator-insensitive.

Examples that must resolve to the same logical root:

- `C:\\Projects\\Foo`
- `c:\\projects\\foo\\`

A parent directory and its child may both be watched intentionally.

## 6.3 Reparse Points, Junctions, and Symlinks

To avoid cycles and accidentally scanning outside the selected root:

- **Do not recursively follow directory symlinks, junctions, or other reparse points by default.**
- Record the link/reparse entry itself in the snapshot where feasible.
- If a target path can be queried safely, it may be stored as optional metadata for display only.
- Cleanup of a link removes the link itself; it must never recurse into its external target.

This behavior is fixed for v1 and should not be hidden behind an implicit implementation detail.

## 6.4 Hard Links

Treat each hard-link path as an independent path entry. No deduplication is required.

## 6.5 Hidden/System Files

Hidden and system entries are included unless excluded by a rule or inaccessible.

## 6.6 Alternate Data Streams

NTFS alternate data streams are out of scope for v1 and are not represented independently.

## 6.7 Long Paths and Unicode

- The application must use Unicode-safe APIs and Go strings throughout.
- The Windows build should be long-path aware where supported by the runtime/toolchain.
- Do not truncate paths internally to fit UI limits.
- UI may visually elide long paths while retaining the full value in tooltips/detail areas.

---

# 7. Application Lifecycle

## 7.1 First Launch

On first launch:

1. Open the main FolderSnap window.
2. Show a focused empty state explaining that two snapshots are required to compare history.
3. Provide one prominent `Add Folder` action.
4. After the user adds a folder, offer `Take First Snapshot` immediately.

Do not launch invisibly into the tray on a user's first manual run.

## 7.2 Normal Manual Launch

When the user launches `FolderSnap.exe` manually:

- If FolderSnap is not running, start it and show the main window.
- If it is already running, bring the existing main window to the foreground and exit the second process.

## 7.3 Windows Startup Launch

When FolderSnap is launched automatically with Windows startup:

- Start directly in background/tray mode.
- Do not open the main window unless an error requires user attention.

The startup invocation may include an internal command-line flag such as `--background` to differentiate this path.

## 7.4 Closing the Window

Closing the main window hides it to the tray if background operation is enabled.

The first time this happens, optionally show a one-time tray message: `FolderSnap is still running in the system tray.`

## 7.5 Exiting

Explicit `Quit FolderSnap` from the tray or Settings:

- Stops scheduling.
- Cancels or gracefully waits for background tasks according to shutdown policy.
- Flushes config/index metadata.
- Removes the tray icon.
- Exits the process.

Never treat the window close button as a destructive quit while background snapshots are expected.

## 7.6 Single Instance

Use a Windows per-user named mutex or equivalent to guarantee a single instance.

A second invocation must signal the first instance to show/activate its window. Do not merely fail silently.

---

# 8. Information Architecture — History Is the Main Feature

The main window should **not** use a traditional tab structure where `Folders`, `History`, and `Settings` compete as peers.

The main workspace should be a persistent three-region history layout:

```text
┌──────────────────────────────────────────────────────────────────────────────┐
│ FolderSnap       [Snapshot Now]                         [activity] [settings] │
├─────────────────┬─────────────────────────┬───────────────────────────────────┤
│ Watched Roots   │ Snapshot Timeline       │ Compare Workspace                 │
│                 │                         │                                   │
│ ● MyProject     │ Before: Aug 24 10:00    │ + 18 Added   - 4 Removed          │
│   D:\Projects  │ After : Aug 25 10:00    │ ~ 7 Modified  Δ +32.4 MB          │
│                 │                         │                                   │
│ ○ Assets        │ Today                   │ [All] [Added] [Removed] [Modified]│
│   E:\Assets    │  10:00 Scheduled        │ [ Search changes...              ]│
│                 │  09:00 Scheduled        │                                   │
│ + Add Folder    │ Yesterday               │ change rows / tree / details      │
│                 │  18:42 Manual           │                                   │
└─────────────────┴─────────────────────────┴───────────────────────────────────┘
```

Recommended logical sizing at 100% scale:

- Default window: approximately 1180 × 720.
- Minimum: approximately 960 × 620.
- Left roots pane: 220–280 px.
- Snapshot pane: 300–360 px.
- Compare workspace consumes remaining width.

The exact pixel values may be adjusted around FLTK layout behavior and DPI scaling, but the hierarchy should remain.

---

# 9. Main Window UX

## 9.1 Top Command Bar

The top bar contains only high-value global/contextual actions:

- Application title/logo area.
- `Snapshot Now` for the currently selected watched root.
- Activity indicator if background scan/diff/cleanup is active.
- Settings button.

Do not fill the top bar with export or rarely used management actions.

## 9.2 Watched Roots Pane

Each root row shows:

- Display folder name.
- Shortened full path.
- Reachability indicator.
- Latest snapshot time or `No snapshots`.
- Optional small running-scan indicator.

Selection of a root drives the timeline and compare workspace.

Context menu/actions:

- Take Snapshot Now.
- Folder Settings.
- Open in Explorer.
- Archive/Stop Watching.
- Delete History… (separate and clearly destructive).

### Archived roots

Archived roots are not included in normal active scanning/scheduling but their history remains available.

Archived roots may be shown in a collapsible `Archived` section below active roots.

## 9.3 Snapshot Timeline Pane

The timeline is the primary selector for historical points.

Snapshots are sorted newest first and grouped visually by date when practical.

Each snapshot row shows:

- Local date/time.
- Manual/Scheduled badge.
- Description first line if present.
- Compact totals, such as `12,482 files • 3.4 GB`.
- Warning icon if scan was incomplete.

### Before/After selection

Use explicit `Before` and `After` roles instead of abstract A/B labels.

Requirements:

- Before must be chronologically older than After.
- If the user selects them in reverse order, the app swaps them automatically.
- The same snapshot cannot occupy both roles.
- Selection state must remain visually obvious while scrolling.

### Default comparison

When a selected root has two or more snapshots and there is no persisted pair selection:

- Automatically select the previous snapshot as Before.
- Automatically select the latest snapshot as After.
- Automatically compute and show the comparison.

This is important: opening FolderSnap should usually answer “what changed most recently?” without an extra Compare button.

### Changing the pair

The UI may implement pair assignment through either:

- two sticky Before/After selectors above the timeline, or
- click-to-select with clear role markers and commands such as `Set as Before` / `Set as After`.

The coding agent should choose the approach that is most robust with go-fltk, but it must remain explicit and discoverable.

Avoid hidden modifier-key-only interactions.

## 9.4 Snapshot Actions

For a selected snapshot:

- Edit description.
- Delete snapshot.
- View scan warnings/skipped paths.
- Future: export.

Description editing should be inline or in a small non-destructive editor, not a full modal workflow.

## 9.5 Compare Workspace

The right pane is the focal point.

It contains, from top to bottom:

1. Comparison header with Before → After timestamps and descriptions.
2. Summary strip.
3. Filter/search controls.
4. Main change list/tree.
5. Contextual details/action area.

---

# 10. Comparison UX in Detail

## 10.1 Comparison Header

Example:

```text
Aug 24, 2026 18:00  →  Aug 25, 2026 10:00
“Before dependency update”   “After dependency update”
```

If one snapshot is incomplete or scope changed, show the warning in this header area rather than burying it in a tooltip.

## 10.2 Summary Strip

Show:

- Added count.
- Removed count.
- Modified count.
- Unchanged count, visually subdued.
- Net file-size delta.

Optional secondary totals:

- Added bytes.
- Removed bytes.
- Modified net bytes.

Size calculations apply to regular files, not directory pseudo-sizes.

## 10.3 Filters

Required filters:

- All Changes.
- Added.
- Removed.
- Modified.

`All Changes` excludes Unchanged entries by default.

Optional toggle:

- `Show unchanged` for diagnostic use, not a primary tab.

## 10.4 Search

Search is case-insensitive and matches:

- filename
- relative path

Search applies after the current change-type filter.

Do not block the UI on search. For typical result sizes, in-memory filtering is adequate.

## 10.5 Change Rows

Use a compact table/list optimized for scanning paths.

Suggested columns:

- change marker
- relative path
- type
- Before size
- After size
- modified time/detail

For Added and Removed entries, irrelevant cells may be blank or show em dash.

### Visual semantics

- Added: green accent.
- Removed: red accent.
- Modified: amber/yellow accent.
- Unchanged: neutral/dim.

Do not use full-row saturated colors. Prefer a narrow state marker, icon, and/or subtle tint to avoid visual fatigue.

## 10.6 Directory Presentation

The default comparison result should be a **flat, sortable list** because it is easier to scan, filter, and search.

A tree view is required specifically for cleanup selection of Added items, where hierarchy matters for parent/child selection.

Do not force the entire comparison UI into a tree just because snapshots represent directories.

## 10.7 Modified Files

For regular files, mark Modified when any relevant metadata used by v1 changes, including:

- size changes, or
- last-write time changes, or
- entry type changes.

A file-to-directory or directory-to-file replacement is Modified with subtype `type_changed` for top-level reporting, and must display the old/new types.

## 10.8 Directory Modification Noise

Do **not** classify a directory as Modified merely because its directory timestamp changed due to children being added/removed/changed.

Directories should normally produce:

- Added.
- Removed.
- Type changed.
- Attribute change if explicitly supported later.

Child changes already describe meaningful directory activity.

This avoids flooding the result with misleading Modified directories.

## 10.9 Rename and Move Semantics

No fuzzy rename/move inference in v1.

If a file moves or is renamed:

- old path = Removed.
- new path = Added.

This behavior must be documented and tested.

---

# 11. Snapshot Engine

## 11.1 Scan Trigger Sources

Snapshots may be triggered by:

- Manual user action.
- Scheduled timer.

Future trigger sources should fit the model, but are not required now.

## 11.2 Scan Concurrency

- A watched root may have at most one active scan.
- Manual and scheduled requests for the same root must be serialized.
- If a scheduled scan is already running and the user requests a manual snapshot, queue one manual scan after the active scan finishes.
- Multiple different roots may scan concurrently.
- Use an internal global concurrency limit to prevent disk thrashing. A default of 2 simultaneous scans is reasonable for v1.

The limit is an internal implementation setting initially; it does not need a user preference.

## 11.3 Cancellation

Every scan goroutine should use a cancellable context.

Cancel when:

- the app is explicitly quitting, or
- the watched root is being permanently removed while scanning.

Hiding the window must not cancel scans.

## 11.4 Snapshot Entry Storage Shape

Persist snapshots as a **flat list sorted by normalized relative path**, not as a recursive serialized tree.

Why:

- simpler deterministic diffing
- lower structural overhead
- easier validation/testing
- easier future streaming
- easier search/filter
- hierarchy can be reconstructed when needed

A conceptual snapshot entry:

```go
type SnapshotEntry struct {
    RelativePath   string    `json:"path"`          // persisted with /
    DisplayPath    string    `json:"displayPath"`   // original casing
    Type           EntryType `json:"type"`
    Size           int64     `json:"size"`
    ModifiedUnixNs int64     `json:"modifiedNs"`
    CreatedUnixNs  int64     `json:"createdNs,omitempty"`
    Attributes     uint32    `json:"attributes,omitempty"`
    LinkTarget     string    `json:"linkTarget,omitempty"`
}
```

Exact Go field names may differ, but the semantics must not.

## 11.5 Snapshot Metadata

Conceptual shape:

```go
type SnapshotHeader struct {
    SchemaVersion      int
    SnapshotID         string
    RootID             string
    RootPathAtCapture  string
    StartedAtUTC       time.Time
    CompletedAtUTC     time.Time
    Trigger            SnapshotTrigger
    Description        string
    FileCount          int64
    DirectoryCount     int64
    OtherCount         int64
    TotalFileBytes     int64
    IgnoreConfig       IgnoreConfigSnapshot
    IgnoreConfigHash   string
    ScanWarnings       []ScanWarning
}
```

## 11.6 Scan Algorithm

The scanner must:

1. Validate root exists and is a directory.
2. Capture scan-start metadata.
3. Recursively enumerate descendants.
4. Normalize relative paths.
5. Apply ignore rules before descending where safe.
6. Detect reparse/link entries and avoid recursive traversal through them.
7. Record entries.
8. Record unreadable paths without aborting the entire scan.
9. Sort entries by normalized relative path.
10. Compute totals.
11. Write snapshot atomically.
12. Update root history index atomically.
13. Emit a completion event back to the UI/scheduler.

## 11.7 Scan Progress

Because total entry count is unknown, do not show a fake percentage.

Show:

- indeterminate spinner/activity state.
- live count such as `Scanning… 18,420 items`.
- current relative path only if useful and inexpensive; it is not required.

## 11.8 Metadata-Only Change Detection Limitation

V1 intentionally does not hash every file's contents.

Therefore a file whose contents change while both size and last-write timestamp remain identical may not be detected.

This limitation should be documented in About/Help or developer documentation, but it should not clutter the main UI.

Future optional mode: content hashing for selected roots or size thresholds.

---

# 12. Incomplete Scans and Comparison Integrity

This area is critical. A missing path due to a permission error must not be presented as a definite historical deletion.

## 12.1 Scan Warnings

When a directory cannot be enumerated, record a structured warning:

- relative path/prefix
- operation
- error category
- human-readable error
- timestamp if useful

When a single file cannot be stat'ed/read for metadata, record that specific path.

## 12.2 Unreadable Prefixes

For unreadable directories, treat the path as an **unknown subtree prefix**.

During diffing:

- If an entry exists in one snapshot but is absent from the other, and its path falls under an unreadable prefix in the other snapshot, do not classify it as definite Added/Removed.
- Mark it as uncertain/unknown or exclude it from definitive counts with a visible warning.

The compare header must show something like:

`Comparison incomplete: 2 subtrees could not be read in the Before snapshot.`

## 12.3 Root Scan Failure

If the root itself is unreachable, do not create a misleading empty snapshot.

The scan fails and no snapshot is saved.

---

# 13. Ignore / Exclusion System

## 13.1 Built-In Defaults

New watched roots should start with default excludes for common generated directories:

```text
node_modules/
build/
.git/
```

`dist/` should **not** be silently hard-coded unless added by product decision later; projects may intentionally care about it.

Built-ins are represented using the same ignore engine as custom patterns where possible.

## 13.2 Per-Root Rules

Each root has a multiline ignore-rules editor.

Rules use forward-slash relative-path semantics regardless of Windows path separators.

Required syntax:

- blank lines ignored
- `#` comment lines
- plain names
- `/` root anchoring
- trailing `/` for directories
- `*` within a segment
- `?` single-character wildcard if implementation library supports it reliably
- `**` across directories
- leading `!` negation/re-inclusion

## 13.3 Required Matching Semantics

Examples:

```gitignore
# anywhere
*.log

# any directory named node_modules
node_modules/

# root-only cache directory
/cache/

# nested cache content
**/.cache/**

# re-include a specific path
!important.log
```

Rules are evaluated in order, last matching rule wins.

## 13.4 Important Negation Rule

Gitignore-style re-inclusion is subtle when a parent directory is already pruned.

The implementation must not claim full gitignore compatibility unless it actually supports it.

For v1, choose one of these two implementation paths and document it in code/tests:

### Preferred
Use an ignore matcher capable of deciding whether a currently ignored directory may contain a later negated rule. Continue traversal when necessary to support re-inclusion correctly.

### Acceptable fallback
Explicitly define a constrained syntax where a child under an excluded directory cannot be re-included unless its parent path is also re-included. Surface this in the ignore editor help text.

Do not silently implement incorrect negation semantics.

## 13.5 Ignore Test Field

Per-root settings should include:

- `Test path` input.
- Result: Included / Excluded.
- Matching rule shown when possible.

This is highly useful for a pattern-driven feature and should be implemented before adding more exotic syntax.

## 13.6 Ignore Rules Are Snapshot Context

Every snapshot stores the ignore configuration or a normalized serialized form plus hash.

If ignore rules differ between Before and After, diffing must account for scope differences rather than blindly treating newly included paths as Added or newly excluded paths as Removed.

See the next section.

---

# 14. Diff Engine

## 14.1 Preconditions

- Both snapshots must exist.
- Both must belong to the same `rootID`.
- Before and After must be different snapshots.
- Before is older by completion timestamp; reverse selections are normalized.

## 14.2 Algorithm

Because entries are persisted sorted by normalized relative path, use a two-pointer merge over the two entry lists.

This yields O(n + m) comparison time and avoids requiring a giant hash map for normal operation.

For each path key:

- only in After → potential Added.
- only in Before → potential Removed.
- in both → compare type/metadata.

Before emitting Added/Removed, evaluate uncertainty and ignore-scope rules.

## 14.3 Ignore Scope Changes

If a path exists only in After but would have matched an exclusion in Before, it is not a definite Added filesystem change; it may simply have become visible because the ignore rules changed.

Likewise, if a path exists only in Before and would be excluded by After's rules, it may be a scope removal rather than a filesystem deletion.

The diff engine should therefore distinguish:

- real Added/Removed
- scope-only change caused by ignore configuration
- uncertain due to scan errors

For v1 UI, scope-only entries do not need a full fifth primary filter. They may be excluded from normal change counts and represented by a header warning/action such as:

`Ignore rules changed between these snapshots. 1,204 paths are outside one snapshot's comparison scope.`

An optional `Show scope differences` diagnostic view may be added if easy.

## 14.4 Modified Equality Rules

### Regular files
Unchanged when:

- type is file in both
- size equal
- last-write timestamp equal
- any included comparison-relevant attributes equal

Otherwise Modified.

### Directories
Do not use directory mtime alone to produce Modified.

### Reparse/link entries
Compare entry type and stored target metadata if available. Do not follow target contents.

## 14.5 Type Replacement

If the same relative path changes type:

- classify as Modified for top-level summary
- subtype `type_changed`
- display old type → new type

## 14.6 Summary Computation

Compute:

- added count
- removed count
- modified count
- unchanged count
- uncertain count
- scope-difference count
- added bytes
- removed bytes
- modified before bytes
- modified after bytes
- net total-byte delta

Normal headline counters remain Added / Removed / Modified / Unchanged.

## 14.7 Caching

Do not prematurely persist every diff result.

An in-memory cache keyed by `(beforeSnapshotID, afterSnapshotID)` is acceptable if useful. It should be bounded or simply hold the current/recent comparison.

Snapshots are immutable, so recomputation is deterministic.

---

# 15. Snapshot Descriptions

The user may attach a short description to any snapshot.

Examples:

- `Before upgrading dependencies`
- `Clean project state`
- `After running installer`

Requirements:

- Editable after snapshot creation.
- Stored in the root's snapshot index/metadata, not by mutating the compressed entry payload where avoidable.
- Displayed in timeline and compare header.
- Plain text only.
- Reasonable UI length limit such as 500 characters is acceptable to prevent accidental huge entries, even if display is usually 1–2 lines.

---

# 16. Scheduled Snapshots

Each active watched root has an independent schedule.

## 16.1 Required Schedule Options

- Manual only.
- Every 1 hour.
- Every 3 hours.
- Every 6 hours.
- Every 12 hours.
- Every day.
- Every week.
- Every month.

## 16.2 Schedule Configuration

### Hour-based intervals
No wall-clock picker is required. Persist `nextDueAt`.

### Daily
Allow a time-of-day. Default to the time at which the schedule was enabled.

### Weekly
Allow weekday + time. Default to current weekday/time.

### Monthly
Allow day-of-month + time. Default to current day/time.

If the configured day does not exist in a month, run on that month's final day.

## 16.3 Persisted Scheduling

Do not rely on a process-local ticker as the source of truth.

Persist enough schedule state to compute the next due time after restart/sleep.

## 16.4 Missed Schedules

On app startup/resume:

- If a root is overdue, take **at most one** catch-up snapshot by default.
- Do not create one snapshot for every missed interval.
- Then compute the next future due time.

This prevents long offline periods from creating a burst of meaningless scans.

## 16.5 Failure Behavior

If the root is unavailable at a due time:

- record a failed run in lightweight runtime status/logging
- do not create an empty snapshot
- preserve normal schedule progression
- retry at the next due time

## 16.6 Interaction with Manual Scans

A manual scan does not permanently shift a calendar daily/weekly/monthly schedule.

For interval schedules, the implementation may choose either anchored intervals or next-after-success semantics, but it must be deterministic and tested. Preferred behavior:

- scheduled due times remain schedule-driven
- a manual snapshot does not cancel the next scheduled snapshot

---

# 17. Snapshot Retention and Deletion

## 17.1 Default Retention

Default: 50 snapshots per watched root.

Supported choices initially:

- 10
- 25
- 50
- 100
- Unlimited

A custom numeric input is optional and not required for v1.

## 17.2 Retention Timing

After successfully writing a new snapshot and updating the index:

- prune snapshots beyond the retention count, oldest first
- never prune the newly created snapshot
- pruning errors must not invalidate the new snapshot

## 17.3 Manual Snapshot Deletion

The user can delete individual snapshots from the timeline.

Confirmation should show:

- snapshot date/time
- description if present
- stored compressed size if known

Deleting a snapshot deletes only FolderSnap history data; it never changes files in the watched root.

## 17.4 Deleting Watched Roots

Use separate concepts:

### Stop Watching / Archive
- stops future scheduled snapshots
- retains all history
- root remains browsable under Archived

### Delete History
- permanently deletes FolderSnap snapshot data for the root
- does not delete the user's actual folder
- requires explicit destructive confirmation

Avoid an ambiguous single `Remove Folder` action that makes history retention unclear.

---

# 18. Safe Cleanup of Added Items

This feature is high-value and high-risk. It requires stricter rules than the earlier simplistic “Added = deletable” concept.

## 18.1 Entry Point

When a comparison contains definitive Added items, show a contextual action:

`Review Added Items for Cleanup…`

Do not label the initial action simply `Delete New Items`, because the first step is review and validation.

The action is disabled if:

- there are no Added items, or
- the comparison is invalid, or
- all Added items are only uncertain/scope-difference entries.

## 18.2 Cleanup Scope

Only entries classified as **definite Added** are candidates.

Never make the following directly cleanup-eligible:

- Modified.
- Removed.
- Unchanged.
- Uncertain due to scan errors.
- Scope-only changes due to ignore configuration changes.

## 18.3 Cleanup Tree

Cleanup view reconstructs a hierarchy from Added relative paths.

Important rule:

- unchanged ancestor folders may be shown as **synthetic structural nodes** so the hierarchy makes sense
- synthetic ancestors are not themselves cleanup candidates and should not have destructive checkbox semantics

Every cleanup-eligible Added entry receives a checkbox.

## 18.4 Checkbox Behavior

- Checking an eligible Added directory selects all eligible Added descendants.
- Unchecking it clears descendants.
- Partial child selection shows indeterminate state where FLTK implementation allows.
- If native tri-state behavior is cumbersome, implement an explicit custom state model even if drawing is simple.
- `Select All Eligible` and `Clear Selection` actions are required.

Default state: **nothing selected**.

Do not preselect all destructive targets.

## 18.5 Live Preflight — Mandatory

Before the confirmation dialog, FolderSnap must validate every selected target against the **current filesystem**.

The comparison's After snapshot is historical. The file may have changed after that snapshot.

### Current status classes

Each selected candidate resolves to one of:

- `Ready` — still matches the After snapshot sufficiently for safe removal.
- `Already missing` — no action required.
- `Changed since snapshot` — blocked by default.
- `Type changed` — blocked.
- `Outside root / invalid path` — blocked and treated as internal safety error.
- `Access denied / unreadable` — blocked or failed preflight.
- `Contains untracked/current content` — directory cannot be deleted wholesale.

## 18.6 File Preflight

For an Added regular file, compare current state with its After snapshot entry:

- path still resolves under the root
- current type is still regular file
- current size matches After
- current last-write timestamp matches After
- current creation timestamp matches After when both values are available

If any available identity-relevant metadata differs, mark `Changed since snapshot` and leave unselected/blocked.

This does not provide cryptographic identity, but it prevents the common and dangerous case of deleting a file that was edited after the historical comparison.

## 18.7 Directory Preflight

Directory timestamps are not sufficient.

If an Added directory is selected:

1. Enumerate its current contents without following reparse points.
2. Determine whether every current descendant targeted for removal is a known Added entry from the comparison and passes its own preflight.
3. If additional current files exist that were not in the After snapshot or are not selected, preserve them.
4. Delete eligible known children individually.
5. Remove the directory only if it is empty after child operations.

Never recursively delete a directory blindly merely because that directory path was Added historically.

## 18.8 Reparse/Link Cleanup

For a cleanup-eligible link/reparse entry:

- remove the link/reparse entry itself if safe
- never recurse through its target

## 18.9 Root Containment Validation

Every destructive path must be derived from the watched root plus validated relative path.

Before deletion:

- normalize the target
- reject absolute relative-path payloads
- reject `..` escape
- verify target remains within root according to Windows path semantics
- do not resolve a reparse target and then delete outside the root

This check must be in the cleanup service, not just the UI.

## 18.10 Deletion Method — Recycle Bin by Default

On Windows, the default cleanup operation must send files/folders to the **Recycle Bin** through an appropriate Windows Shell API.

Do not use `os.Remove` / `os.RemoveAll` as the normal cleanup path.

Reasons:

- user can recover mistakes
- behavior matches Windows expectations
- safer than permanent deletion

If the selected location does not support Recycle Bin behavior or the shell operation fails:

- report the failure
- do **not** silently fall back to permanent deletion

Permanent deletion may be added later as an advanced explicit option with additional confirmation, but is not needed for v1.

## 18.11 Final Confirmation

After preflight, show a confirmation summary:

```text
Ready to move to Recycle Bin:
  23 files
  4 folders
  182 MB

Blocked because they changed since the selected snapshot:
  3 files

Already missing:
  2 files
```

The destructive confirmation button should say:

`Move 27 items to Recycle Bin`

not merely `OK` or `Delete`.

## 18.12 Execution Ordering

When individually processing paths:

- deepest file/link entries first
- then deepest directories
- parent directories last

Continue after per-item failures where safe.

## 18.13 Cleanup Result

Show:

- successfully moved to Recycle Bin
- already missing
- blocked/changed
- failed with reason

Then return to the historical diff without pretending it has changed: Before and After remain immutable snapshots. Modified items remain listed exactly as historical Modified entries and are never included in cleanup. Added rows may show a non-historical status badge such as `Moved to Recycle Bin now` for the current session, but this must not rewrite the diff itself.

Offer a prominent:

`Take Post-Cleanup Snapshot`

Do not automatically rewrite the historical After snapshot.

## 18.14 Cleanup Audit Log

Maintain a local lightweight operation log containing:

- timestamp
- rootID
- Before/After snapshot IDs
- requested paths
- final per-path result

This is for diagnostics and safety traceability. It is not a backup or undo system.

---

# 19. Root Folder Settings

Per-root settings should be accessible without leaving the history workspace, preferably through a settings button/context action for the selected root.

Required sections:

## General

- Display name override (optional; otherwise folder basename).
- Path (read-only in normal state).
- `Open in Explorer`.
- Reachability status.

## Snapshot Schedule

- Manual only / interval / daily / weekly / monthly controls.
- Next scheduled snapshot display.

## Exclusions

- Built-in/default pattern area.
- Custom multiline patterns.
- Test-path input/result.

## History

- Retention count override or use global default.
- Snapshot count.
- Approximate history disk usage.

## Lifecycle

- Archive / Stop Watching.
- Delete FolderSnap History…

If a root is unavailable, a future `Relink Folder…` action may allow history to follow the same logical root to a new path. This is useful but may be deferred until after core v1.

---

# 20. Global Settings

Keep Settings compact. FolderSnap should not become a settings-heavy product.

## General

- Launch FolderSnap when Windows starts.
- Show notification on successful scheduled snapshot: on/off.
- Always notify on scheduled snapshot failure.
- Close window to tray: on by default while background schedules exist.

## History

- Global default retention: 50.
- Total FolderSnap history storage used.
- `Open Data Folder` may be provided for technical users, but it is not a primary action.

## Default Exclusions

- Default patterns applied when a new watched root is created.
- Editing defaults does not silently rewrite existing root rules unless the user chooses to apply them.

## Diagnostics

- Open log file/folder.
- App version/build information.

## Data Management

- Export config/history archive: future/optional.
- Delete all FolderSnap data: advanced, explicit typed confirmation if implemented.
- Quit FolderSnap.

---

# 21. System Tray

## 21.1 Tray Behavior

Left-click:

- Show/activate main window.

Right-click menu:

- Open FolderSnap.
- Take Snapshot Now for currently/recently selected root, or submenu for roots if manageable.
- Settings.
- Quit.

If there are many watched roots, do not build an enormous tray menu. Prefer a capped/recent-root submenu plus `Open FolderSnap` for full management.

## 21.2 Tray States

At minimum:

- idle
- scanning/activity
- error requiring attention

Visual differences must still work at 16×16.

## 21.3 Notifications

Because the app is intended to work as a portable/simple executable, use the simplest reliable Windows notification mechanism supported by the tray integration.

Notifications are appropriate for:

- scheduled snapshot failed
- optional scheduled snapshot success
- cleanup completed with failures

Do not notify for every routine manual action while the main UI is already open.

---

# 22. Persistence and Storage

## 22.1 Data Location

Use:

```text
%LOCALAPPDATA%\FolderSnap\
```

not roaming `%APPDATA%`.

History can be large and is specific to the local machine/filesystem, so Local AppData is the appropriate default.

Suggested structure:

```text
%LOCALAPPDATA%\FolderSnap\
    config.json
    state.json                       # optional runtime UI/scheduler state
    logs\
        foldersnap.log
    roots\
        <rootID>\
            index.json
            snapshots\
                <snapshotID>.json.gz
            cleanup-log.jsonl        # optional per-root audit log
```

## 22.2 Root IDs

Use generated stable IDs rather than sanitized folder paths as directory names.

Benefits:

- avoids illegal characters
- survives path casing changes
- supports future relinking
- avoids path-length explosions

## 22.3 Snapshot Payload Format

Use versioned JSON compressed with standard-library gzip for v1.

Example conceptual envelope:

```json
{
  "schemaVersion": 1,
  "header": { ... },
  "entries": [ ... ]
}
```

Compression and exact field naming are implementation details, but schema versioning is mandatory.

## 22.4 Root Index

`index.json` contains lightweight metadata only:

- snapshotID
- completion timestamp
- trigger
- description
- counts/totals
- warning count
- compressed file size if known
- schema version/reference

Do not load full snapshot trees merely to render the timeline.

## 22.5 Atomic Writes

For config, indexes, and snapshot files:

1. write to temp file in same target directory
2. flush/close
3. replace/rename atomically where possible

If snapshot payload write succeeds but index update fails, startup repair should be able to detect orphaned snapshot files.

## 22.6 Startup Consistency Check

On startup, perform a lightweight consistency pass:

- index entry whose snapshot file is missing → mark corrupted/missing in UI
- orphan snapshot file not referenced by index → log and optionally recover metadata or quarantine; do not silently delete immediately
- malformed config → preserve bad file as backup and fail gracefully

A full expensive validation of every compressed snapshot is not required on every startup.

---

# 23. Build and Distribution Requirements

## 23.1 Windows Target

Initial supported build target:

- Windows 10 and Windows 11
- x86-64 / amd64
- normal per-user desktop process
- no administrator privileges required for ordinary use

ARM64 and older Windows versions are future concerns unless they work incidentally.

## 23.2 Executable Experience

The release should behave like a normal GUI application:

- no console window in release builds
- branded executable icon
- version metadata embedded where practical
- one primary executable from the user's point of view

The intended distribution is a portable/single-executable experience with no mandatory installer. Because go-fltk uses cgo/FLTK native libraries, the coding agent must verify the actual Windows toolchain/linking output rather than assume static packaging. If a runtime DLL is technically unavoidable with the chosen build configuration, document it immediately instead of silently violating the packaging goal.

## 23.3 Toolchain

Use the Windows toolchain supported by the selected go-fltk version (typically Go plus the required MinGW/C++ build environment). Pin dependency versions in `go.mod` and record the known-good build command in the repository README.

## 23.4 No Installer Dependency for Startup

`Launch with Windows` must work for the portable build using a current-user startup mechanism such as the `HKCU` Run key. If the executable is moved, the app should repair/update the startup entry the next time the setting is enabled or the app detects its path changed.

---

# 24. Architecture and Package Boundaries

Core logic must be UI-independent and testable.

Suggested project structure:

```text
cmd/
  foldersnap/
    main.go

internal/
  model/
    root.go
    snapshot.go
    diff.go
    cleanup.go

  config/
    store.go

  scan/
    scanner.go
    windows_metadata.go

  ignore/
    matcher.go

  history/
    store.go
    retention.go
    repair.go

  diff/
    engine.go

  scheduler/
    scheduler.go

  cleanup/
    planner.go
    preflight.go
    executor.go

  platform/windows/
    tray.go
    recyclebin.go
    startup.go
    single_instance.go
    explorer.go
    paths.go

  ui/
    app.go
    theme.go
    events.go
    root_pane.go
    timeline_pane.go
    compare_pane.go
    cleanup_view.go
    settings_view.go
```

Exact names may change, but the separation is important.

## 23.1 Dependency Direction

- UI depends on application/core services.
- Core diff/history/ignore logic must not import FLTK.
- Windows Shell integration is isolated behind small interfaces.
- Cleanup service validates safety independently of UI state.

## 23.2 Service Interfaces

Prefer small interfaces where platform abstraction genuinely helps testing, especially:

- filesystem access/stat abstraction for scanner tests
- recycle-bin executor
- clock for scheduler tests

Avoid unnecessary interface-everything architecture.

---

# 25. Goroutines, Channels, and FLTK UI Thread

`go-fltk` is a Go wrapper around FLTK and should be treated as a UI framework whose event processing/drawing belongs on the GUI thread.

## 24.1 Rule

**Background goroutines must not directly mutate arbitrary FLTK widgets.**

Use a UI event-dispatch mechanism:

```text
scanner/scheduler/diff/cleanup goroutine
            │
            ▼
       app event channel
            │
            ▼
 FLTK main-thread wake/timer/event pump
            │
            ▼
        update widgets
```

Use whichever main-thread wake/event mechanism is actually exposed reliably by the current go-fltk binding. If a specific FLTK primitive is not wrapped, use a short periodic main-thread timer to drain the Go event channel rather than updating widgets from workers.

## 24.2 Background Jobs

Represent long operations explicitly:

- ScanJob
- DiffJob
- CleanupPreflightJob
- CleanupExecutionJob

Each should expose:

- job ID
- root ID
- status
- cancellation
- progress counters where meaningful
- final result/error

## 24.3 Error Boundaries

A panic in a background job must not take down the UI process. Use sensible recovery at goroutine/service boundaries where appropriate and log diagnostic context.

Do not hide programmer errors everywhere with indiscriminate `recover`; recover only at top-level worker boundaries.

---

# 26. FLTK UI/UX Design System

The previous Windows concept specified several web-like visual effects that are not essential and may be awkward in FLTK. This version prioritizes a polished **desktop utility** over imitating a modern web app.

## 25.1 Theme

V1 may be dark-only.

Suggested palette:

| Role | Color |
|---|---|
| Window background | `#101318` |
| Panel background | `#171B22` |
| Raised/header | `#1E242D` |
| Input/list selected | `#252C36` |
| Divider | `#303845` |
| Primary text | `#E7EAF0` |
| Secondary text | `#9AA4B2` |
| Disabled text | `#626C7A` |
| Accent | `#4E8FEA` |
| Added | `#42B883` |
| Removed | `#E25C5C` |
| Modified | `#D8A441` |
| Warning | `#E28A45` |

Exact colors may be adjusted after real Windows rendering tests.

## 25.2 Typography

Prefer Windows-native readable fonts available through FLTK:

- Segoe UI where available for normal UI.
- Consolas for paths and ignore patterns.

Suggested logical sizes at 100%:

- normal UI: 13–14 px equivalent
- secondary metadata: 11–12
- section title: 15–16 bold
- summary count: 18–22 bold

## 25.3 Visual Style

Use:

- flat panels
- 1px dividers
- consistent 8/12/16 px spacing rhythm
- restrained corner rounding only if reliable
- clear selected/focus states
- small color accents for diff state

Avoid depending on:

- blur/glow effects
- translucency
- slide animations
- overlay scrollbars
- complex shadows
- excessive pill controls

A static, crisp interface that behaves well is preferable to decorative effects.

## 25.4 Row Density

Suggested:

- watched-root row: 44–52 px
- snapshot timeline row: 54–64 px
- diff table row: 28–34 px

The timeline needs more vertical room because description/context matters.

## 25.5 Long Paths

- Use monospace in diff path column.
- Prefer middle or left elision that preserves filename/end of path.
- Full path appears in tooltip and/or selected-row detail line.

## 25.6 Empty States

### No watched roots
`Add a folder to start building a change history.`

Primary action: `Add Folder`

### One snapshot only
`One snapshot saved. Take another later to compare what changed.`

Primary action: `Snapshot Now`

### No changes
`No changes between these snapshots.`

Still show Unchanged total and comparison timestamps.

### Incomplete comparison
Do not call it “No changes” if scan uncertainty prevents certainty.

## 25.7 DPI Scaling

Test the Windows build at minimum:

- 100%
- 125%
- 150%
- 200%

Requirements:

- no clipped primary buttons
- readable text
- resizable panes
- no fixed pixel assumptions that make controls inaccessible

## 25.8 Keyboard

At minimum:

- Tab / Shift+Tab traverses controls logically.
- Enter activates focused primary action.
- Escape closes transient dialog/panel, not the whole app.
- Delete on a selected snapshot may initiate snapshot deletion only after confirmation.
- Ctrl+F focuses change search when compare workspace is active.
- F5 or Ctrl+R may refresh current filesystem/root status; optional.

Do not use single-key destructive shortcuts without confirmation.

---

# 27. Folder Picker and Native Windows Integration

Use a native or Windows-appropriate folder selection dialog where practical.

The result must:

- select directories only
- return Unicode paths
- handle drive roots
- support removable drives

If go-fltk's available chooser is insufficient, isolate a small Windows-native chooser implementation behind `platform/windows` rather than compromising the entire UI architecture.

Likewise, keep tray/startup/recycle-bin/single-instance functionality in small Windows-specific adapters.

---

# 28. Error Handling and User-Facing States

## 27.1 Root Unavailable

Display root status as unavailable.

Manual snapshot attempt:

- show inline error in the main window
- do not save empty snapshot

Scheduled attempt:

- log failure
- optional/required tray notification according to settings

## 27.2 Partial Scan

Save snapshot if the root itself was successfully scanned but some descendants failed.

Mark timeline row with warning.

Comparison must surface uncertainty rules described earlier.

## 27.3 Snapshot Corruption

Timeline entry remains visible with status:

`Snapshot data missing or unreadable`

Allowed actions:

- Retry load.
- Delete snapshot metadata/data.

Do not crash the timeline or entire root history.

## 27.4 Disk Full / Write Failure

- Delete temporary partial payload.
- Do not append a valid-looking index entry.
- Surface error.
- Preserve older history.

## 27.5 Cleanup Failure

Continue other independent targets where safe.

Result must distinguish:

- blocked before execution
- failed during execution
- succeeded
- already missing

## 27.6 Config Corruption

Keep a backup of the malformed config and show a recoverable startup error rather than silently resetting all watched roots.

---

# 29. Logging and Privacy

## 28.1 Logging

Local rotating log file only.

Log:

- startup/shutdown
- scan start/end/failure
- counts/duration
- snapshot write failure
- diff failure
- cleanup planning and outcomes
- scheduler decisions at debug level

Avoid logging every scanned filename by default because it creates massive logs and unnecessary privacy exposure.

## 28.2 Privacy

No telemetry or analytics in v1.

Snapshots necessarily contain relative file/folder names. This should be understood as local application data.

Do not copy file contents into logs or snapshot payloads.

---

# 30. Performance Requirements

These are engineering targets rather than guarantees for every disk/device.

## 29.1 UI Responsiveness

- No filesystem scan or diff computation on UI thread.
- Main window should remain interactive while scans run.
- Loading snapshot metadata/timeline should not require decompressing every snapshot.

## 29.2 Scanner

- Stream enumeration; do not first build an unnecessary recursive object graph.
- Keep only required entry metadata.
- Apply exclusions before descending into excluded subtrees.

## 29.3 Diff

- Sorted merge O(n + m).
- Avoid quadratic path searches.
- Comparison of ~100k entries per snapshot should be a normal supported case, not an exceptional design case.

## 29.4 Memory

It is acceptable for v1 to load two decompressed snapshots into memory for comparison, but the flat format should preserve a future path to streaming comparison.

## 29.5 Storage

Use gzip compression.

Do not make unverified claims in UI such as “10,000 files always use only X KB”; actual size depends heavily on path lengths and metadata.

---

# 31. Testing Strategy

Core correctness matters more than screenshot-perfect UI tests.

## 30.1 Scanner Tests

Cover:

- empty root
- nested files/directories
- Unicode names
- case variants
- hidden files
- excluded directory pruning
- unreadable path simulation through filesystem abstraction
- symlink/reparse no-follow behavior where feasible
- normalized relative paths

## 30.2 Ignore Matcher Tests

Cover:

- plain directory name
- root anchored pattern
- `*`
- `**`
- comments/blank lines
- trailing slash directory-only rule
- negation semantics
- Windows backslash input normalized to slash
- last-match-wins

## 30.3 History Store Tests

Cover:

- save/load round trip
- atomic index behavior
- missing payload
- malformed payload
- pruning
- delete one snapshot
- delete root history
- startup orphan/missing-file detection

## 30.4 Diff Tests

Mandatory cases:

- Added file
- Added directory with children
- Removed file
- Modified size
- Modified timestamp
- Unchanged file
- Directory child change does not create noisy directory Modified
- Rename = Removed + Added
- Move = Removed + Added
- file → directory type change
- directory → file type change
- case-normalized same path
- unreadable-prefix uncertainty
- ignore-scope change avoids false Added/Removed
- net size delta

## 30.5 Scheduler Tests

Use an injected clock.

Cover:

- hourly intervals
- daily time
- weekly day/time
- monthly day clamp
- restart with future nextDue
- missed interval creates at most one catch-up
- manual snapshot does not corrupt schedule
- unavailable root failure progression

## 30.6 Cleanup Planner/Preflight Tests

This is mandatory before enabling the destructive UI.

Cover:

- Added unchanged file → Ready
- Added file edited after After snapshot → blocked
- Added file already deleted → Already missing
- path changed from file to directory → blocked
- path escape attempt → blocked
- current extra child inside Added directory → preserve child, don't remove directory wholesale
- nested eligible files delete deepest first
- reparse entry never traversed
- scope/uncertain diff entries never eligible
- Recycle Bin adapter failure never falls back to permanent delete

## 30.7 UI Smoke Tests

Manual or lightweight automated checks:

- first launch
- add root
- snapshot progress
- two snapshot default comparison
- filter/search
- edit description
- incomplete scan warning
- cleanup review + preflight + result
- close-to-tray
- second instance activates first
- DPI 125/150%

---

# 32. Acceptance Criteria — Core V1

FolderSnap v1 is ready when all of the following are true.

## App lifecycle

- Manual launch opens main window.
- Windows-startup launch can remain background/tray only.
- Closing main window hides to tray without terminating schedules.
- Only one process instance runs per user.

## Watched roots

- User can add a root with native/usable folder chooser.
- Duplicate path variants are rejected case-insensitively.
- Root availability is visible.
- Root can be archived without deleting history.

## Snapshots

- Manual snapshot works in a background goroutine.
- Required schedule modes work.
- Snapshot includes full non-excluded descendant metadata.
- Reparse/junction traversal does not escape/cycle.
- Snapshot persists across restart.
- User can edit description.
- User can delete snapshot history without affecting real files.

## History-first UX

- Selecting a root with at least two snapshots shows latest-two comparison automatically.
- User can choose any older/newer pair from the same root.
- Timeline remains usable without loading every snapshot payload.

## Diff

- Added/Removed/Modified/Unchanged classifications are correct for tested cases.
- Rename/move appears as Removed + Added.
- Directory timestamp noise does not flood Modified results.
- Search and filters work.
- Size delta is correct.
- Scan uncertainty is not mislabeled as definite change.
- Ignore-scope change is not mislabeled as definite change.

## Cleanup

- Only definite Added entries are eligible.
- Nothing is selected by default.
- Live preflight runs before destructive confirmation.
- Files changed since After are blocked by default.
- Directories with untracked/current content are not recursively destroyed.
- Root-containment checks exist in cleanup service.
- Normal cleanup uses Recycle Bin.
- Failure never silently falls back to permanent deletion.
- Historical snapshots remain immutable after cleanup.

## Exclusions

- `node_modules/`, `build/`, `.git/` are default excludes for new roots.
- Custom ignore syntax behaves according to documented semantics.
- User can test a path against current rules.

## Reliability

- Atomic writes are used.
- Corrupt/missing snapshots do not crash the app.
- Unavailable roots do not generate false empty snapshots.
- App remains responsive during scan/diff/cleanup.

---

# 33. Implementation Plan for Codex

The coding agent should implement this in vertical, testable phases. Do not begin with visual polish.

## Phase 0 — Repository/Foundation

Create:

- Go module.
- `cmd/foldersnap` entry point.
- package boundaries.
- config paths under LocalAppData.
- logger.
- core models and schema version constants.

Add a tiny FLTK window only to prove the selected toolchain builds on Windows x64.

**Exit criteria:** `go test ./...` and a Windows GUI executable builds/runs.

## Phase 1 — Snapshot Core Without Full UI

Implement:

- root registration model.
- path normalization.
- ignore matcher.
- scanner.
- reparse no-follow behavior.
- flat sorted snapshot entries.
- gzip snapshot persistence.
- root index.
- atomic writes.
- descriptions metadata update.
- retention.

Add thorough unit tests.

A small CLI/debug harness is acceptable in this phase.

**Exit criteria:** Can add a path in test/debug code, create/load multiple snapshots, and inspect metadata reliably.

## Phase 2 — Diff Engine

Implement:

- sorted merge diff.
- Added/Removed/Modified/Unchanged.
- type changes.
- size summaries.
- incomplete-scan uncertainty.
- ignore-scope handling.
- tests for all required cases.

**Exit criteria:** Diff engine is UI-independent and heavily tested.

## Phase 3 — History-First FLTK Main Window

Implement the three-region layout:

- roots pane.
- timeline pane.
- compare workspace.
- latest-two auto comparison.
- pair selection.
- summary.
- filters.
- search.
- description editing.
- snapshot deletion.
- empty/error/warning states.

Implement worker → UI event dispatch rather than mutating widgets from goroutines.

**Exit criteria:** Manual snapshots and arbitrary comparison are pleasant enough to use as the core product.

## Phase 4 — Scheduling + Tray + Single Instance

Implement:

- scheduler with injectable clock.
- persisted next due times.
- missed-run catch-up policy.
- tray icon/menu.
- close-to-tray.
- startup registry option under current user.
- manual vs background startup behavior.
- single-instance activation.

**Exit criteria:** App can live in tray for days and accumulate history without main window open.

## Phase 5 — Safe Added-Item Cleanup

Implement in this order:

1. cleanup plan generation from diff
2. hierarchy reconstruction
3. preflight service
4. root containment protection
5. current-state classifications
6. Recycle Bin Windows adapter
7. execution ordering/result model
8. UI tree + checkboxes
9. confirmation/result UX
10. cleanup audit log

Do not implement UI deletion first and “add safety later.”

**Exit criteria:** Tests demonstrate that later-modified or unknown files cannot be casually removed from an old comparison.

## Phase 6 — Exclusion UX + Error/Recovery Polish

Implement:

- per-root ignore editor.
- test-path helper.
- scan warning viewer.
- corrupted snapshot states.
- startup consistency checks.
- unavailable-root UX.

## Phase 7 — Settings + DPI + Usability Polish

Implement:

- retention settings.
- startup setting.
- notification setting.
- storage usage.
- keyboard shortcuts.
- DPI scaling fixes.
- context menus/tooltips.
- final theme polish.

## Future Phase — HTML Export

Only after v1 core is stable:

- export a selected snapshot to self-contained HTML
- optionally export filtered diff to CSV/text

HTML export is not part of the initial critical path and must not distort core data models or UI architecture.

---

# 34. Coding-Agent Guardrails

Codex should follow these rules while implementing the PRD.

1. **History Compare is the product center.** Do not redesign the app into a folder-management dashboard with History as a secondary tab.
2. **Keep core logic independent from FLTK.** Scanner, ignore matcher, history store, diff, scheduler calculations, cleanup planner, and preflight must be unit-testable without UI.
3. **Do not update FLTK widgets from worker goroutines directly.** Marshal events back to the GUI thread.
4. **Do not use recursive serialized UI trees as the storage model.** Persist flat sorted entries and reconstruct hierarchy only when needed.
5. **Do not treat directory mtime changes as useful Modified-directory events.**
6. **Do not call rename/move detection “smart.”** Rename and move are Removed + Added in v1.
7. **Do not save an empty snapshot when a root scan fails.**
8. **Do not convert permission-skipped paths into false Added/Removed results.** Preserve uncertainty.
9. **Do not ignore exclusion-rule differences between snapshots.** Prevent scope changes from becoming false filesystem changes.
10. **Do not make historical Added entries automatically deletable.** Always perform live preflight.
11. **Do not recursively delete an Added directory without inspecting current contents.**
12. **Do not silently fall back from Recycle Bin to permanent deletion.**
13. **Do not put `os.RemoveAll` behind the cleanup button.**
14. **Do not modify historical snapshots after filesystem cleanup.**
15. **Do not spend early implementation time on HTML export.**
16. **Do not overengineer visuals with web-style effects that are awkward in FLTK.** Prioritize layout, typography, state clarity, and responsiveness.
17. **Do not assume every FLTK widget/API is wrapped by go-fltk.** Keep custom/platform adapters narrow and verify actual binding support before building architecture around a widget.
18. **Write tests before enabling destructive cleanup in the GUI.**
19. **Use `%LOCALAPPDATA%`, not roaming `%APPDATA%`, for snapshot history.**
20. **Keep schema versions explicit from day one.**

---

# 35. Definition of Done

The first truly useful Windows FolderSnap build is not “done” when it can scan a folder and render a file tree.

It is done when a user can:

1. Add a real working folder.
2. Leave FolderSnap running in the tray.
3. Accumulate snapshots automatically.
4. Open the app and immediately understand the latest changes.
5. Compare any two meaningful points in history.
6. Trust that permission gaps and ignore-rule changes are not being lied about as filesystem changes.
7. Review exactly which files appeared between two points.
8. Safely move selected still-unchanged Added items to the Recycle Bin.
9. Keep the historical record intact after cleanup.
10. Continue using the app without freezes, accidental permanent deletion, or UI complexity getting in the way of the comparison workflow.

That experience is the core product. Everything else, including HTML export, is secondary.
