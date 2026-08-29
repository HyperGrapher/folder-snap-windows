# FolderSnap for Windows — Product Requirements Document

**Date:** 25 August 2026  
**Platform:** Windows 10/11 x64  
**Implementation:** Go with FLTK  
**Status:** Fresh-schema implementation specification  
**Product baseline:** FolderSnap macOS history implementation

## 1. Product purpose

FolderSnap records metadata snapshots of folders and lets the user explicitly select two snapshots to see what changed between them.

The initial Windows release is a focused port of the proven macOS History feature. HTML export is intentionally excluded. Scheduling and Windows tray behavior may remain, but they must not change the snapshot/history interaction.

The primary question is:

> What was added, removed, or modified between snapshot A and snapshot B?

## 2. Source of truth

The following macOS files define the baseline behavior:

- `Sources/IndexFolderCore/SnapshotRecord.swift`
- `Sources/IndexFolderCore/HistoryStore.swift`
- `Sources/IndexFolderCore/DiffEngine.swift`
- `Sources/IndexFolderCore/FolderScanner.swift`
- `Sources/IndexFolderApp/HistoryViewModel.swift`
- `Sources/IndexFolderApp/HistoryView.swift`
- their corresponding core tests

Windows-specific filesystem and lifecycle handling may differ, but the history selection and comparison workflow must match this baseline.

## 3. Core workflow

1. The user adds or selects a folder.
2. The user creates snapshots manually or through an enabled schedule.
3. FolderSnap saves each successful snapshot to local compressed history.
4. Selecting a folder opens its newest-first snapshot list with no snapshots preselected.
5. Clicking one snapshot marks it as A.
6. Clicking another snapshot marks it as B.
7. FolderSnap orders A and B chronologically so A is older and B is newer.
8. The user presses Compare.
9. FolderSnap loads both stored snapshots and shows Added, Removed, and Modified entries.

FolderSnap must never silently choose or advance the comparison pair.

## 4. Snapshot-selection behavior

The Windows implementation must reproduce `HistoryViewModel.selectSnapshot` behavior:

- Selecting a folder clears A, B, previous diff results, filter, and search.
- The first unassigned snapshot clicked becomes A.
- The second unassigned snapshot clicked becomes B.
- If both are already assigned, clicking a third snapshot moves B to A and assigns the clicked snapshot to B.
- Clicking the current A removes A.
- Clicking the current B removes B.
- When both are assigned, the older timestamp is always A and the newer timestamp is always B.
- The same snapshot cannot be both A and B.
- Any selection change clears the previous diff and returns the comparison workspace to its idle state.
- Compare is enabled only when two different snapshots from the same folder are selected.
- A diff is computed only when the user presses Compare.
- Taking a new snapshot refreshes the list but does not select it or change A/B.

No pair selection is persisted between launches or folder changes.

## 5. Snapshot contents

A snapshot contains:

- stable snapshot ID
- root ID and canonical root path
- display title
- capture timestamp in UTC
- trigger (`manual` or `scheduled`)
- file, directory, and other-entry totals
- total regular-file bytes
- scan warnings
- the ignore rules active during capture
- entries below the root, addressed by normalized relative path

Each entry records:

- display relative path
- normalized case-insensitive comparison path
- type: file, directory, reparse point, or other
- file size where applicable
- last-write timestamp
- Windows creation timestamp and attributes where available
- reparse target when safely available

The root itself is excluded from diff results.

## 6. Scan rules

- Scanning runs outside the UI thread and supports cancellation.
- Hidden and system entries are included unless excluded by configuration.
- Directory symbolic links, junctions, and reparse points are recorded but not traversed.
- Unreadable entries are skipped and reported as warnings; they do not fail an otherwise usable scan.
- A root that cannot be opened does not produce an empty snapshot.
- Relative paths are persisted using `/` separators and compared case-insensitively on Windows.
- Entries are sorted by normalized relative path before persistence.
- A successful scan is saved even when it contains non-fatal warnings.

## 7. Fresh local persistence schema

There is no migration requirement from previous Windows development builds. Schema version 2 starts with empty local data.

All data is stored under `%LOCALAPPDATA%\FolderSnap`:

```text
FolderSnap/
  config.json
  History/
    index.json
    <snapshot-id>.snapshot
  Logs/
    foldersnap.log
```

`History/index.json` is the single history index, equivalent to the macOS `HistoryIndex`. It contains newest-first snapshot records for all roots. Records include root identity/path so they can be grouped by folder without opening payload files.

Each `.snapshot` file is gzip-compressed JSON containing the immutable snapshot payload. The `.snapshot` extension intentionally hides the compression implementation from product semantics.

Persistence requirements:

- index and snapshot writes use a same-directory temporary file and atomic replacement
- the payload is written before its index record
- deleting a snapshot removes its record and payload
- retention is applied independently per root
- missing payloads produce a visible comparison error
- malformed or unsupported schema data fails clearly; no best-effort legacy migration is attempted

## 8. Diff behavior

Diffing follows the macOS engine semantics:

- flatten both snapshots by normalized relative path
- union the paths
- only in B: Added
- only in A: Removed
- in both with changed type, file size, or modified timestamp: Modified
- otherwise: Unchanged
- directory modified timestamps do not create Modified noise
- rename and move appear as Removed plus Added
- results sort naturally by display relative path within each classification

The summary shows:

- Added count
- Removed count
- Modified count
- unchanged count where useful
- total byte delta from A to B

Windows scan-warning and changed-ignore-scope safeguards may decorate results as incomplete, but they must not turn known Removed entries into Added entries or hide definitive changes.

## 9. Main-window UX

The window follows the macOS History layout adapted to FLTK:

```text
┌────────────────────────────────────────────────────────────────────┐
│ FolderSnap                 Add Folder   Snapshot Now   Settings     │
├──────────────────┬─────────────────────────────────────────────────┤
│ Folders          │ Snapshots                    [ Compare ]         │
│                  │ [A] 25 Aug 10:00   files / folders / size       │
│ Downloads        │ [B] 25 Aug 11:00   files / folders / size       │
│ Projects         ├─────────────────────────────────────────────────┤
│                  │ Changes       A timestamp → B timestamp         │
│                  │ Added  Removed  Modified  Delta                  │
│                  │ filters                 search                  │
│                  │ change list                                     │
└──────────────────┴─────────────────────────────────────────────────┘
```

### Folder pane

- Shows display name, path/reachability state, snapshot count, and latest snapshot time.
- Selecting a folder resets comparison state.
- Folders with zero snapshots remain visible and show an actionable empty state.

### Snapshot picker

- Newest first.
- Each row shows local timestamp, file count, folder count, total size, and warning indicator.
- A and B markers remain visually obvious.
- Compare is a prominent explicit button.
- Snapshot deletion remains available.

### Changes workspace

- Idle state: `Pick two snapshots`.
- One-selection state explains that another snapshot is required.
- Loading, error, no-change, and completed states are distinct.
- Completed state shows four summary cards, filter buttons, search, and a flat change list.
- Each row shows type, relative path, old size → new size, and classification.

## 10. Visual design

- Neutral charcoal surfaces only; no dark navy or purple tones.
- Light foreground text must be set for labels, browsers, text fields, choices, and editors.
- Teal is the primary action/selection accent.
- Green, red, and amber are reserved for Added, Removed, and Modified semantics.
- Selection must not rely on color alone; A/B and text labels are required.
- Use spacing, borders, and hierarchy instead of gradients or decorative effects.

## 11. Windows lifecycle retained from the Windows product

- Single instance per user.
- Manual launch shows the window.
- `--background` launch may start in the tray.
- Scans and diffs never mutate FLTK widgets from worker goroutines.
- Snapshot schedules remain optional per folder.
- Startup registration remains optional.

These features are secondary to correct history behavior.

## 12. Added-item cleanup

After an explicit A/B comparison, the user may open a dedicated deletion panel for entries classified as Added. The panel must:

- show only Added entries that are neither uncertain nor caused by an exclusion-scope difference;
- start with no items selected;
- support Select All, Deselect All, and hierarchical directory selection with checked/indeterminate states;
- clearly exclude Removed and Modified entries from the operation;
- move selected current-filesystem items to the Windows Recycle Bin, never permanently delete them;
- validate containment, type, metadata, reparse ancestors, and directory contents before confirmation and again immediately before mutation;
- block changed, missing, unsafe, or untracked content without affecting it;
- never fall back to permanent deletion when the Recycle Bin is unavailable;
- preserve historical snapshots and write a per-root JSONL cleanup audit.

## 13. Deferred features

The following are not part of the stabilization release:

- HTML, CSV, or text export
- file-content backup or restoration
- content hashing
- rename detection
- cross-root comparison
- cloud sync, telemetry, or accounts
- NTFS journal integration

## 14. Acceptance criteria

The release is accepted when all of the following pass:

1. Add a clean test root and take snapshot A.
2. Add a file and take snapshot B; selecting A and B then pressing Compare shows that file only as Added.
3. Delete an existing file and take snapshot C; selecting B and C shows that file only as Removed.
4. Modify an existing file and take snapshot D; selecting C and D shows that file only as Modified.
5. Selecting a folder never preselects snapshots or computes a diff.
6. Selecting one row displays A; selecting a second displays A/B in chronological order.
7. Changing either selection clears the old diff until Compare is pressed again.
8. Restarting the app loads the history index but starts with no A/B selection.
9. Missing payload and corrupt-index errors are visible and do not fabricate changes.
10. All unit/integration tests and `go vet ./...` pass, and the release executable has no non-system runtime DLL dependency.
11. The deletion panel is unavailable until a completed comparison contains eligible Added entries.
12. The deletion panel starts empty, directory selection propagates to descendants, and partial selection never leaves an ancestor directory eligible for deletion.
13. Confirmed items are revalidated and moved only through the Windows Recycle Bin; changed or unsafe items remain untouched.

## 15. Implementation guardrails

- Core scanner, store, and diff logic remain independent of FLTK.
- Do not reintroduce automatic latest-two comparison.
- Do not retain or migrate schema-v1 development JSON.
- Do not compare a snapshot with the live filesystem.
- Do not infer removed files from a failed root scan.
- Do not traverse reparse-point directories.
- Do not mutate historical snapshot payloads.
- Do not let stale asynchronous comparison results overwrite a newer selection.
- Do not allow cleanup of Removed, Modified, uncertain, or exclusion-scope entries.
- Do not permanently delete current filesystem content or mutate historical snapshots.
