# FolderSnap Windows — Go Implementation Plan

**Date:** 25 August 2026  
**Source:** `260825-FolderSnap_Windows_History_First_PRD.md`  
**Baseline:** FolderSnap macOS History implementation

**Status:** Implemented and verified on 27 August 2026

## Architecture decision

Keep Go and FLTK. Replace the development persistence model with schema version 2 and a macOS-style single history index plus compressed snapshot payloads. No schema-v1 migration will be implemented; local development data will be reset.

The scanner continues to persist flat, sorted relative-path entries because that is the native form consumed by the diff engine. This is behaviorally equivalent to the macOS engine flattening its saved tree before comparison while avoiding a redundant tree conversion.

## Delivery order

1. **Contract reset** — update the PRD, remove automatic comparison, define explicit A/B plus Compare behavior, and mark cleanup/export as deferred.
2. **Fresh persistence** — schema-v2 config; global `History/index.json`; compressed `<id>.snapshot` payloads; atomic save/load/delete/prune; no legacy migration.
3. **Selection state** — folder selection clears state; first click A; second click B; third rolls B to A; chronological ordering; selection changes invalidate prior results.
4. **History UI rebuild** — macOS-style folder sidebar, snapshot picker above comparison, explicit Compare button, empty/loading/error/done states, summary cards, filters, search, and detailed result rows.
5. **Regression verification** — fresh root data, sequential add/remove/modify snapshots, explicit arbitrary-pair comparisons, restart behavior, missing payload handling, static build, and DLL inspection.
6. **Data reset** — stop only the running FolderSnap executable, remove `%LOCALAPPDATA%\FolderSnap`, launch the rebuilt application against an empty schema-v2 store.

## Core contracts

- `Scanner.Scan(context.Context, ScanRequest) (Snapshot, error)`
- `HistoryStore.Save(snapshot, retention) (SnapshotRecord, error)`
- `HistoryStore.List(rootID) ([]SnapshotRecord, error)`
- `HistoryStore.Load(snapshotID) (Snapshot, error)`
- `HistoryStore.Delete(snapshotID) error`
- `HistoryStore.DeleteRoot(rootID) error`
- `DiffEngine.Compare(snapshotA, snapshotB) (DiffResult, error)`

## Quality gates

- A new folder has no implicit A/B selection.
- Diff computation cannot begin with fewer than two explicit selections.
- Every selection change invalidates any previous result and stale worker completion is ignored.
- Add, remove, modify, rename, and type replacement tests match macOS behavior.
- Global history index round-trip, per-root grouping, deletion, retention, and missing-payload behavior are tested.
- `go test -count=1 ./...` and `go vet ./...` pass.
- Release build uses the Windows GUI subsystem and depends only on Windows system DLLs.

## Verification result

- Folder and snapshot history render as custom cards with visible paths, dates, counts, sizes, descriptions, warnings, and right-aligned A/B badges.
- A real saved pair was selected and compared in the release build. The UI reported one modified file and rendered its relative path, before/after size, and classification correctly.
- `go test -count=1 ./...`, `go vet ./...`, and `git diff --check` pass.
- The release executable was rebuilt and inspected; its imports are limited to Windows system DLLs.
- A 188,610-file / 21.7 GB saved-history comparison was exercised: after completion the release process settled at about 31 MB working set and 0% CPU while hidden; the same process reopened successfully through the tray path.
- Large diff results are paginated at 200 cards per page, and hidden-window tray actions use a main-thread dispatch queue so Open, Settings, Snapshot, and Quit do not depend on FLTK dispatching an Awake callback with no visible window.

## Deferred until the core is stable

- HTML/CSV/text export
- Added-item cleanup UI and current-filesystem deletion
- hashing, content backup, restore, and rename detection
