# FolderSnap Qt/C++ Reimplementation Plan

Status: implementation handoff  
Target: Windows 10/11 x64, C++20 and Qt 6  
UI direction: intentionally unspecified; preserve behavior and safety contracts, but design a fresh interface

## 1. Purpose

Build FolderSnap as a Windows desktop application in C++ with Qt. FolderSnap records metadata-only snapshots of configured folder trees, keeps local history, compares any two snapshots from the same folder, and can safely move selected items that were added between those snapshots to the Windows Recycle Bin.

This is not a file-content backup application. A snapshot stores paths and metadata, not file bytes. The implementation must remain local-only and must not require an account, cloud service, network service, telemetry, or a background Windows service.

This document is self-contained: its requirements, data contracts, and test matrix are the implementation specification. Cleanup and HTML/CSV export are required product features.

## 2. Required product behavior at a glance

The Qt implementation must provide all of these behaviors:

- Register multiple watched folders and maintain independent settings/history for each.
- Take manual metadata snapshots without blocking the GUI.
- Schedule snapshots at supported intervals or calendar times.
- Keep snapshot payloads and a lightweight global history index on disk.
- Repair recoverable history inconsistencies on startup.
- Select two explicit snapshots and compare them chronologically.
- Classify changes as Added, Removed, Modified, Unchanged, Uncertain, or Scope Difference.
- Search/filter changed results and present them as an expandable multi-level tree.
- Sort files largest-first within each comparison-tree folder.
- Edit snapshot descriptions, inspect scan warnings, delete one snapshot, or clear a folder's history.
- After a snapshot exists, export that snapshot as a standalone HTML report or CSV file.
- After a comparison completes, export that comparison as HTML or CSV, with Added/Removed/Modified file and folder rows visibly distinguished in HTML.
- Use the supplied HTML/JavaScript template for an offline searchable, sortable snapshot report.
- Review Added items and safely move selected live items to the Recycle Bin after two-stage validation.
- Run in the Windows notification area, optionally start with Windows, and optionally close to the tray.
- Enforce single-instance operation and activate the existing window when launched again.
- Log operational failures locally with simple rotation.

## 3. Recommended Qt solution structure

Keep domain and storage code independent from widgets so the implementation is testable without a GUI.

```text
FolderSnap/
├── CMakeLists.txt
├── assets/
│   └── snapshot-export-template.html  # offline report template, packaged with the app
├── src/
│   ├── main.cpp
│   ├── domain/          # value types and enums only
│   ├── paths/           # normalization and containment
│   ├── ignore/          # gitignore-like compiler/matcher
│   ├── scan/            # concurrent metadata scanner
│   ├── history/         # JSON/gzip payloads, index, repair, retention
│   ├── diff/            # snapshot comparison engine
│   ├── cleanup/         # plan, preflight, execute, audit
│   ├── schedule/        # due-time calculations
│   ├── application/     # orchestration, jobs, events/signals
│   ├── platform/windows/# Recycle Bin, startup, single instance, activation
│   └── ui/              # fresh Qt Widgets or Qt Quick UI
└── tests/
```

Recommended dependencies:

- Qt 6 Core, Concurrent, and either Widgets or Quick.
- Qt Test for unit/integration tests.
- CMake with CTest.
- zlib for actual gzip streams. Do not use `qCompress`; its format is not gzip-compatible.
- Windows Shell COM for Recycle Bin operations.
- Win32 APIs or Qt native interfaces for single-instance activation and startup registration.

Recommended core classes:

- `AppService`: owns configuration, scan jobs, scheduler, and domain-facing signals.
- `ConfigStore`: reads/writes `config.json` atomically.
- `HistoryStore`: owns the global index and immutable payload lifecycle.
- `SnapshotScanner`: creates a `Snapshot` from a watched root.
- `IgnoreMatcher`: compiles and evaluates ordered rules.
- `DiffEngine`: compares two snapshots without accessing the live filesystem.
- `CleanupService`: performs plan, preflight, revalidation, Recycle Bin moves, and audit.
- `ScheduleCalculator`: pure due-time calculations.
- `SingleInstance`, `StartupRegistration`, `RecycleBin`, and `ShellIntegration`: Windows adapters.

Use Qt queued signals to cross worker/UI thread boundaries. Never access UI objects from scan, compare, cleanup, or scheduler workers.

### 3.1 Dependencies: required, recommended, and optional

The core application can stay small and use Qt/C++/Win32 primitives. These are the dependency choices for the target implementation:

| Dependency | Need | Use |
|---|---|---|
| Qt 6 Core | Required | `QString`, `QDateTime`, JSON, paths, signals/slots, and core utilities |
| Qt 6 Concurrent or `QThreadPool` | Recommended | Cancellable scan/compare/cleanup jobs without blocking the GUI |
| Qt 6 Widgets or Qt Quick | Required, choose one | Fresh UI; choose the interface independently |
| Qt 6 Network | Optional/recommended with `QLocalServer` | Local single-instance activation channel; omit when using a Win32-only activation mechanism |
| Qt 6 Test | Recommended | Automated unit, integration, and UI-facing behavior tests |
| zlib | Required | Read/write gzip-compressed `.snapshot` payloads; `qCompress` is not gzip |
| Windows SDK / Shell COM | Required on Windows | `IFileOperation` Recycle Bin moves, file attributes, timestamps, and native shell integration |
| Win32 Registry APIs or `QSettings` NativeFormat | Required | HKCU startup registration |
| `QLocalServer`/`QLocalSocket` plus a named mutex | Recommended | Race-safe single-instance ownership and activation messaging |
| C++ standard library (`std::filesystem`, `std::jthread`, `std::chrono`) | Recommended | Filesystem traversal, cancellation, and platform-neutral domain logic |
| CSV library | Optional | Not necessary: RFC 4180-style quoting is small and easier to test locally |
| HTML templating library | Not recommended | Use the checked-in standalone template and safe JSON injection |

OpenSSL, a database, web server, JavaScript runtime, Chromium embedding, and cloud SDK are not needed. Exported HTML must run in a normal browser from a local file and must not require QtWebEngine or a network connection.

## 4. Domain model and persistent schema

Use schema version `2` as the fixed on-disk contract for this product. Preserve the JSON field names and semantics below exactly. Write UTC ISO-8601/RFC 3339 timestamps with sufficient sub-second precision, and parse the same format without converting them to local time.

### 4.1 Entry and trigger enums

```cpp
enum class EntryType { File, Directory, Reparse, Other };
// JSON: "file", "directory", "reparse", "other"

enum class SnapshotTrigger { Manual, Scheduled };
// JSON: "manual", "scheduled"
```

### 4.2 Snapshot entry

Each non-root filesystem entry has:

- `path`: normalized relative identity path; lowercase, `/` separators, never empty/absolute/outside the root.
- `displayPath`: relative path with filesystem display casing and `/` separators.
- `type`: one of the entry types above.
- `size`: bytes for regular files; zero for non-files.
- `modifiedNs`: last-write timestamp as Unix nanoseconds.
- `createdNs`: Windows creation timestamp as Unix nanoseconds; optional/zero when unavailable.
- `attributes`: Windows file attributes.
- `linkTarget`: reparse/symlink target when it can be read.

The root directory itself is not an entry.

### 4.3 Scan warning and ignore context

```json
{
  "path": "relative/subtree",
  "operation": "enumerate | stat",
  "category": "access_denied | not_found | io",
  "message": "native error text"
}
```

Every snapshot stores the exact ordered ignore-rule list plus its SHA-256 hash. The hash input is the raw rule strings joined with `\n`.

### 4.4 Snapshot header and payload

A snapshot payload contains:

- Top-level `schemaVersion`.
- `header` with its own `schemaVersion`.
- `snapshotId`: random RFC 4122 version-4 UUID text.
- `rootId`: stable UUID for the watched root.
- `rootPathAtCapture` and `displayTitle`.
- `startedAtUtc` and `completedAtUtc`.
- `trigger` and optional `description`.
- `fileCount`, `directoryCount`, `otherCount`, and `totalFileBytes`.
- `ignoreConfig` and sorted `scanWarnings`.
- `entries`, sorted ascending by normalized `path`.

### 4.5 Lightweight history index record

The global index stores one record per snapshot:

- Snapshot/root IDs, root path, display title, completion time, and trigger.
- Optional description.
- File/directory/other counts and total file bytes.
- Warning count and compressed payload byte count.

`payloadAvailable` is runtime-only. Compute it by checking whether the referenced payload exists; do not serialize it.

The enclosing index document is `{ "schemaVersion": 2, "records": [ ... ] }`. A record uses the fields `snapshotId`, `rootId`, `rootPath`, `displayTitle`, `completedAtUtc`, `trigger`, optional `description`, `fileCount`, `directoryCount`, `otherCount`, `totalFileBytes`, `warningCount`, and `compressedBytes`.

### 4.6 Watched-root configuration

Each root stores:

- `rootId`, editable `displayName`, immutable registered `path`, and `normalizedPath`.
- `archived`: stops future/manual snapshots without deleting history.
- A schedule, ordered `ignoreRules`, and `retention`.
- `lastSnapshotUtc` and `lastScanError` operational state.

Its `schedule` object uses `kind` (`manual`, `interval`, `daily`, `weekly`, or `monthly`), `nextDueAtUtc`, and only the applicable fields: `intervalHours`, `hour`, `minute`, `weekday`, and `dayOfMonth`. Validate hours as 0–23, minutes as 0–59, weekday as 0–6 (Sunday–Saturday), and day of month as 1–31. Do not silently coerce an invalid persisted schedule.

Registered paths cannot be changed in place. Adding the same path again must be rejected case-insensitively after normalization.

### 4.7 Global configuration

Store:

- `schemaVersion`.
- All roots.
- `defaultRetention`.
- `defaultIgnoreRules` used only when adding future roots.
- `launchAtStartup`.
- `notifyScheduledSuccess`.
- `closeToTray`.

Defaults:

- Retention: 50 snapshots per root.
- Exclusions: `node_modules/`, `build/`, `.git/`.
- Close to tray: true.
- Startup and successful-schedule notifications: false.

Valid retention choices are 10, 25, 50, 100, or 0 for unlimited.

## 5. Filesystem layout and durability

Use `%LOCALAPPDATA%\FolderSnap` as the data directory:

```text
FolderSnap/
├── config.json
├── History/
│   ├── index.json
│   ├── <snapshot-id>.snapshot
│   └── <snapshot-id>.snapshot.deleting   # transient tombstone only
├── roots/<root-id>/cleanup-log.jsonl
└── logs/
    ├── foldersnap.log
    └── foldersnap.log.1
```

Persistence requirements:

- Pretty-print `config.json` and `History/index.json` using two-space indentation.
- Store each `.snapshot` as a gzip-compressed JSON document. Prefer gzip speed level over maximum compression.
- Limit config decoding to 16 MiB and index decoding to 32 MiB.
- Reject a decoded snapshot over 1 GiB.
- Serialize all history mutations so concurrent snapshot saves cannot lose index records.
- Atomically write files in the destination directory: create a temporary file, write, flush, close, and replace with write-through semantics. `QSaveFile` may be used if its Windows durability/replacement behavior is verified; otherwise use `MoveFileExW(MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH)`.
- File permissions are private-user intent. Apply reasonable Windows ACL/privacy defaults.

Malformed config behavior:

- Copy it to `config.json.corrupt-YYYYMMDDTHHMMSSZ`.
- Fail application initialization with an actionable error; do not silently reset user settings.
- Reject unsupported schema versions.

Identifier/path validation:

- Storage IDs must be a single safe filename component: not empty, `.`, `..`, absolute, or containing `/`, `\`, `:`, or NUL.
- Snapshot entry identity paths must normalize to the exact persisted lowercase relative value and must not escape the root.
- All joins of persisted relative paths to a watched root must repeat containment validation.

## 6. Watched-folder management

Implement these operations:

1. Add a folder selected by the user.
2. Verify it currently exists and is a directory.
3. Convert it to an absolute clean path and a lowercase `/`-normalized identity path.
4. Reject duplicate normalized paths.
5. Generate a root UUID and derive the initial display name from the last path component.
6. Copy global default exclusions and retention into the new root.
7. Use Manual-only scheduling initially.
8. Offer an immediate first snapshot after successful registration.
9. Allow changing display name, schedule, retention, exclusions, and archived state.
10. Allow opening the root in Explorer.
11. Allow clearing only that root's history after explicit destructive confirmation.

There is no permanent “remove watched root” operation. Archiving is the stop-watching behavior and preserves history/settings. Archived roots remain visible, sort after active roots, do not run schedules, and reject manual snapshot requests.

Data-directory self-protection is mandatory:

- The FolderSnap data directory itself cannot be registered as a watched root.
- If a watched root is an ancestor of the data directory, automatically add a root-anchored directory exclusion such as `/relative/path/to/foldersnap/`.
- Recompute/enforce this effective protection for every scan, not only when the root is first added.

## 7. Ignore-rule engine

Implement ordered, case-insensitive, gitignore-like rules with these exact supported concepts:

- Blank lines and lines beginning with `#` are ignored.
- Convert `\` to `/` and trim surrounding whitespace.
- `!pattern` re-includes matching paths.
- Last matching rule wins.
- Leading `/` anchors a rule at the watched-root boundary.
- Trailing `/` means a directory and its descendants.
- Patterns without `/` match a component at any depth.
- `*` matches within one path component.
- `?` matches one non-separator character.
- `**` crosses directory boundaries; `**/` may match zero or more directory prefixes.
- Empty patterns/negations are validation errors and should identify the 1-based rule line.

Expose a “test path” capability returning Included or Excluded plus the last matching original rule.

Traversal optimization must preserve negations. An excluded directory may be pruned only when the complete rule set has no negation. If any negation exists, continue traversing excluded directories because a descendant might be re-included.

## 8. Snapshot scanning

### 8.1 Scanner semantics

- Validate the root before scanning. If it is missing, unreadable at the root level, or no longer a directory, fail and save no snapshot.
- Walk directories concurrently; use 4 directory workers by default, support up to 32, and read directory entries in batches of 256.
- Do not follow reparse points or symlinked directories.
- Record a reparse entry itself when not excluded; attempt to read its target without failing the scan if target reading fails.
- Record files, directories, reparse points, and other filesystem objects.
- Capture Windows attributes, creation time, modification time, and regular-file size.
- Set non-file sizes to zero.
- Preserve display casing separately from normalized identity casing.
- Report progress after the first entry and approximately every 256 entries.
- Sort final entries by normalized path and warnings by path then operation.
- Count entries by type and sum only regular-file sizes.
- Honor cancellation throughout enumeration, result collection, sorting checkpoints, payload loading, and comparison.

### 8.2 Partial-read behavior

Failure to enumerate or stat a descendant must produce a warning and allow the snapshot to complete. A failure to enumerate the root itself must fail the entire scan. Warning categories are access denied, not found, or general I/O.

### 8.3 Scan orchestration

- Permit at most two roots to scan concurrently.
- Permit only one active scan job per root.
- If another request arrives for an already-running root, coalesce it into at most one pending Manual and one pending Scheduled request.
- After the active scan, run pending Manual first, then pending Scheduled.
- Emit scan-started, progress, completed, failed, and configuration-changed events/signals.
- A successful scan saves history, updates `lastSnapshotUtc`, clears `lastScanError`, and refreshes history.
- A failed scan records `lastScanError` without changing `lastSnapshotUtc`.
- Closing the service must cancel active/waiting work cleanly.

## 9. Scheduling

Supported schedule kinds:

- Manual only.
- Every 1, 3, 6, or 12 hours.
- Daily at local `HH:mm`.
- Weekly at local `HH:mm`, weekday 0=Sunday through 6=Saturday.
- Monthly at local `HH:mm`, day 1 through 31.

Rules:

- Store `nextDueAtUtc`, but calculate calendar schedules in the machine's local time zone.
- For a monthly day unavailable in a month, clamp to that month's last day (for example, day 31 becomes February 28/29).
- On first scheduler observation of a schedule with no next due time, calculate and persist the next future occurrence; do not immediately take a snapshot.
- Check schedules shortly after startup and then about every 15 seconds; wake promptly when root settings change.
- If one or many runs were missed while the app was closed, request only one catch-up snapshot.
- Advance the next due time to the first future occurrence while preserving the interval/calendar anchor.
- Archived and Manual-only roots are skipped.
- Scheduling a snapshot still obeys per-root coalescing and the global two-scan limit.

Use timezone-aware Qt date/time operations and test daylight-saving transitions.

## 10. History store, retention, deletion, and repair

### 10.1 Save and load

- Validate schema, IDs, and every entry path before saving.
- Reject duplicate snapshot IDs.
- Atomically write the payload first, then atomically update the global index.
- Sort index records newest-first by completion time, breaking equal timestamps by snapshot ID ascending.
- List history per root from that global index.
- On load, verify top-level/header schema, payload filename ID, root ID, and entry paths.
- Mark whether loaded entries were already sorted so the diff engine can avoid unnecessary copies/sorts.
- The service must verify that a loaded snapshot actually belongs to the requested root.

### 10.2 Retention

- Apply retention per root after a successful save.
- Keep the newest N records; 0 means unlimited.
- Never let pruning one root alter another root's history.
- Changing a retention setting does not immediately prune; prune after the next successful snapshot.

### 10.3 Descriptions and visibility

- Descriptions are editable plain text up to 500 Unicode code points.
- Description edits update the lightweight index only; immutable snapshot payload contents remain unchanged.
- If an indexed payload is missing, keep the record visible and mark it unavailable. Comparing/loading it should return a distinct missing-payload error.

### 10.4 Transactional deletion

For one snapshot, one root, and retention pruning:

1. Rename target payloads to `.snapshot.deleting` tombstones in deterministic ID order.
2. Write the new index atomically.
3. If index writing fails, restore tombstones in reverse order.
4. If it succeeds, remove tombstones.

Clearing a root's history must be rejected while that root is scanning. After success, clear its `lastSnapshotUtc` and `lastScanError` and persist configuration. Deleting history never touches the watched folder.

### 10.5 Startup repair

Run repair during service initialization. A repair error should be logged but should not necessarily prevent startup when configuration is usable.

Repair must:

- If `index.json` is malformed/corrupt, preserve a timestamped `.corrupt-*` copy, restore tombstones to payload names, and rebuild the index from every valid orphan payload.
- If the index is valid, restore a `.deleting` payload when its record is still referenced and its original payload is missing.
- Delete unreferenced tombstones.
- Discover complete `.snapshot` payloads absent from the index, validate/load them, and add records.
- Leave indexed-but-missing payload records intact so the application can report missing history.
- Ignore invalid orphan payloads instead of inventing records for them.

## 11. Snapshot pair selection

The user must choose comparison snapshots explicitly. Do not preselect the latest pair and do not silently advance the pair after new snapshots.

Selection rules:

- First unassigned snapshot click becomes A.
- Second becomes B.
- Always reorder assigned IDs chronologically so A is older and B is newer regardless of click order.
- Selecting a third unassigned snapshot rolls current B into A and makes the new snapshot B, then chronological ordering is applied.
- Clicking an already assigned A or B removes that assignment.
- Timeline refresh preserves assignments whose records still exist and drops missing ones.
- Changing watched root clears A, B, selected snapshot, search, filter, comparison, and expansion state.
- Any pair change cancels or invalidates a comparison in flight. Stale worker results must never overwrite current UI state.
- The same snapshot cannot occupy both roles and snapshots from different roots cannot be compared.

## 12. Diff engine

The diff engine is pure: it reads two snapshot values and does not inspect the current filesystem.

### 12.1 Preconditions and ordering

- Require different snapshot IDs and the same non-empty root ID.
- Swap inputs if necessary so `Before` is chronologically older.
- Use normalized relative paths as identity keys.
- Exploit already path-sorted entry arrays with a linear merge; defensively sort imported/unsorted input.
- Materialize only changed entries. Count unchanged entries in the summary without retaining them in the changed-entry vector.

### 12.2 Classification

- Present only in After: Added.
- Present only in Before: Removed.
- Same path but different entry type: Modified with subtype `type_changed`.
- Same regular-file type: Modified when size or modification nanoseconds differ. Attribute-only or creation-time-only changes do not count.
- Same directory type: Unchanged regardless of directory timestamp/attributes.
- Same reparse type: Modified when link target or attributes differ.
- Same Other type: Modified when modification time or attributes differ.
- Rename/move detection is not attempted. It appears as Removed at the old path plus Added at the new path.
- File contents are not hashed. A content change that preserves both size and last-write timestamp is not detected.

### 12.3 Incomplete and scope-sensitive results

For an item present on only one side:

- If its path lies at or beneath a scan-warning path in the missing side, mark it `Uncertain`; do not count it as a definite Added/Removed.
- If the missing side's stored ignore rules would exclude the path, mark it `ScopeDifference`; do not count it as a definite Added/Removed.
- Matching warning paths is case-insensitive and subtree-aware.
- Record whether the two raw ordered ignore-rule lists differ and expose Before/After warning counts.
- Uncertain classification takes summary precedence over Scope Difference if both flags are possible.

### 12.4 Summary

Return:

- Counts for Added, Removed, Modified, Unchanged, Uncertain, and Scope Difference.
- Added and removed regular-file bytes.
- Modified regular-file bytes before and after.
- Net bytes as `After.header.totalFileBytes - Before.header.totalFileBytes`.

The storage-level changed-entry ordering is Added, Removed, Modified, with case-insensitive natural numeric path order inside each group (`file2` before `file10`). The comparison tree described below supplies its own presentation ordering.

For large comparisons, check cancellation periodically (about every 4096 merge iterations), avoid duplicating unchanged entries, and release decoded full snapshots after producing the compact result.

## 13. Comparison result behavior (UI-independent requirements)

The new visual design is open, but these interactions/data presentations are required:

- Show summary counts and net size delta.
- Surface scan warnings, uncertain/scope-difference counts, and changed exclusions prominently enough that users know the result may be incomplete.
- Filter changed items by All, Added, Removed, or Modified.
- Search changed items case-insensitively by display path.
- Exclude Unchanged, Uncertain, and Scope Difference entries from the ordinary changed-item tree; still expose their counts/context.
- Build an expandable tree from relative paths and synthesize parent folders when only a descendant changed.
- Support expansion through arbitrary nesting levels.
- Sort folders before files within each folder.
- Sort files within each folder by effective size descending; use After size when it is a regular file, otherwise Before size for removed files. Break ties deterministically by case-insensitive name.
- Display recursive Before → After folder sizes for real and synthesized folders, calculated from all snapshot files rather than only changed descendants.
- A real changed folder carries its change classification; a synthesized parent is only structural.
- Show file Before → After size, folder labeling, and type-change detail.

Comparison work must run off the GUI thread. It may load Before and After payloads concurrently, but allow only one active comparison calculation for the application. Cancellation/version checks must suppress stale results.

### 13.1 Comparison export (Qt target only)

After a comparison has completed successfully, expose HTML and CSV export actions for that comparison. This is separate from the per-snapshot export in Section 14.1.

Comparison export requirements:

- Export the selected Before/After pair, including pair timestamps, root identity/display name, warning counts, exclusion-change state, and all materialized changed entries.
- Use the same standalone `assets/snapshot-export-template.html`; the exporter injects a `reportType: "comparison"` DTO and the template switches to comparison rendering.
- Render a collapsible multi-level tree. Include synthesized parent folders needed to reach changed descendants, even when a parent itself has no change classification.
- Color the complete HTML row for every definite Added, Removed (display label: Deleted), and Modified file or folder. Use distinct accessible colors plus a text status badge; never rely on color alone. Structural parent rows are neutral, and Uncertain/Scope Difference rows (if included by the exporter) use an explicit warning style.
- Show each file/folder's change status, Before → After type/size, and creation date where available. For folders, sizes must be recursive totals from the complete Before and After snapshots, including unchanged descendants.
- Provide case-insensitive search/filter by name/path and filters for All, Added, Removed/Deleted, and Modified. Matching descendants must keep their ancestors visible/expanded.
- Provide sibling sorting by Name, Size, or Creation date in Ascending or Descending order, with deterministic normalized-path tie-breaking. For Size, compare the effective After value when present and otherwise Before; folder values use recursive totals.
- Include both sides' metadata in CSV. Recommended columns are `path`, `displayPath`, `change`, `subtype`, `beforeType`, `afterType`, `beforeSizeBytes`, `afterSizeBytes`, `beforeCreatedAtUtc`, `afterCreatedAtUtc`, `beforeModifiedAtUtc`, `afterModifiedAtUtc`, `uncertain`, and `scopeDifference`.
- A comparison export is a historical report: it must not re-scan the live root, change A/B selection, mutate history, or perform cleanup.

The export DTO should preserve exact 64-bit sizes as decimal strings (`beforeSizeBytes`/`afterSizeBytes`) and provide ISO date strings because browser JSON numbers cannot safely represent Windows nanoseconds. A suggested shape is:

```json
{
  "reportType": "comparison",
  "header": { "rootTitle": "Projects", "beforeId": "…", "afterId": "…" },
  "entries": [
    { "path": "src/new.cpp", "displayPath": "src/new.cpp", "change": "added", "after": { "type": "file", "sizeBytes": "42", "createdAtUtc": "…" } }
  ],
  "folderSizes": { "src": { "beforeBytes": "100", "afterBytes": "142", "beforePresent": true, "afterPresent": true } }
}
```

Snapshot and comparison reports may share the same template file, but keep their DTO schemas explicit and versioned so a future template change cannot silently misrender one report type.

## 14. Snapshot history operations

For each snapshot, make these data available: local completion date/time, trigger, optional description, file/folder counts, total file bytes, warning count, and payload availability.

Required actions:

- Edit description with the 500-code-point limit.
- Inspect scan warnings in a complete, scrollable viewer.
- Delete one snapshot after confirmation, clearly stating that watched files are unchanged.
- Clear a root's entire history after confirmation.
- Missing payloads remain listed but cannot be compared.

History is newest-first. A newly completed snapshot refreshes the current root without automatically changing A/B.

## 14.1 Snapshot export (Qt target only)

Export is a per-snapshot operation, available once that snapshot has been successfully saved. It is not a comparison export and does not require an A/B pair.

Each snapshot record should expose a dropdown/actions menu with:

- **Export to HTML**
- **Export to CSV**

The action must be disabled or explain the problem when the indexed payload is missing. On activation, load and validate the immutable gzip payload on a worker thread, then ask for an output filename (or use a clearly documented default such as `<display-title>-<local-completion-time>.html` / `.csv`). Do not block the GUI while decoding or writing. Export uses the selected snapshot only and never changes history.

### 14.1.1 HTML report contract

Create and package the standalone template `assets/snapshot-export-template.html`. Produce an offline HTML document by injecting a snapshot DTO into it. It must work when opened directly from disk in a current browser:

- No CDN, remote font, network request, server, QtWebEngine, or JavaScript runtime dependency.
- Embed a JSON representation of the snapshot metadata and entries into the template. Escape `<`, `>`, `&`, and U+2028/U+2029 in the JSON representation so a filename or display path cannot terminate the script block.
- For the export DTO, include `createdAtUtc`/`modifiedAtUtc` ISO strings and preferably a decimal-string `sizeBytes` field. This avoids losing Windows nanosecond timestamps or large 64-bit sizes when a browser parses JSON; the template accepts legacy numeric `size` as a fallback.
- Render all snapshot files and folders in a collapsible multi-level tree. Include a root summary and each entry's display name/path, type, size, and creation date. The implementation may also show modified date, attributes, and link target because those are already captured metadata.
- Use text nodes/DOM APIs for user data; never concatenate untrusted paths into HTML markup.
- Provide case-insensitive filtering/search by name, display path, or normalized path. Matching descendants must remain reachable and their ancestor folders must be shown/expanded as needed.
- Provide a sort field of Name, Size, or Creation date and an Ascending/Descending direction. Sorting must be deterministic with a stable tie-breaker (normalized path). Preserve folder hierarchy while sorting siblings. Recommended default: folders first, then files by size descending to match the desktop comparison convention.
- Show zero-byte/unknown creation values clearly rather than inventing dates. Format timestamps in the browser's local timezone while retaining the original ISO timestamp in a data attribute or details view.
- Handle large snapshots without rebuilding the full DOM on every keystroke; debounce search and use a document fragment/virtualized strategy if needed.

The template must have a small, documented replacement marker (for example, `/* FOLDERSNAP_REPORT_DATA */`) and remain useful as a fixture with an empty/sample data set. Keep all JavaScript and CSS inline so the exporter only needs to inject safely serialized JSON and write bytes. Its report dispatcher must support both `reportType: "snapshot"` and `reportType: "comparison"`, with the comparison behavior specified in Section 13.1.

### 14.1.2 CSV report contract

Write UTF-8 CSV with RFC 4180-compatible quoting (quote fields containing commas, quotes, CR, or LF; double embedded quotes). A UTF-8 BOM is recommended for smooth opening in Windows Excel, but document the choice and test it.

Emit one header row and one row per snapshot entry. Recommended columns are:

`path`, `displayPath`, `type`, `sizeBytes`, `createdAtUtc`, `modifiedAtUtc`, `attributes`, `linkTarget`

Keep paths as relative snapshot paths, not live absolute paths. Use an empty field for metadata that is not applicable or unavailable (for example, link target for a regular file). CSV export must not read file contents or the live filesystem.

### 14.1.3 Export test cases

- Export a snapshot containing nested folders, Unicode/quoted/comma/newline names, zero-byte files, reparse entries, and unavailable creation times.
- Verify HTML can be opened from a disconnected machine and remains functional after search, ancestor expansion, and every sort field/direction.
- Verify HTML escaping prevents a path such as `</script><script>...` from executing.
- Verify CSV quoting round-trips all fields and the optional BOM is accepted by Excel-oriented tests.
- Verify export does not alter the immutable payload, index, descriptions, retention, A/B selection, or live filesystem.
- Verify missing payloads produce a clear error and no partial output file.
- Verify large payload export is cancellable/does not freeze the GUI.

## 15. Safe cleanup of Added items

Cleanup is a safety-critical shipped feature. Do not implement it as direct deletion.

### 15.1 Candidate plan and selection

- Candidates are only definite Added entries with an After entry.
- Exclude Uncertain and Scope Difference entries.
- Never offer Removed or Modified entries.
- Start with nothing selected.
- Support selection/search by display name and normalized path.
- Selecting a directory selects its candidate descendants.
- Deselecting a child clears selected ancestors; represent partially selected directories as indeterminate.
- Show selected item count and total bytes of selected regular files.

### 15.2 Preflight statuses

Every candidate must receive one of:

- `ready`
- `already_missing`
- `changed_since_snapshot`
- `type_changed`
- `outside_root_or_invalid`
- `access_denied_or_unreadable`
- `contains_untracked_content`
- `moved_to_recycle_bin`
- `failed`

Preflight checks:

1. Normalize candidate path and reject empty/unsafe paths. This prevents targeting the watched root itself.
2. Join and verify containment beneath the root.
3. Reject any path that crosses a reparse-point ancestor.
4. `lstat` the live target; missing is non-fatal `already_missing`.
5. Require live type to equal the After-snapshot type.
6. For files, require live size and modification nanoseconds to match. If stored creation time is nonzero and Windows metadata is available, require creation time too.
7. For reparse entries, require a readable unchanged target when the snapshot stored one.
8. For a selected directory, recursively ensure every live descendant is also a selected candidate; never follow reparse directories. Any extra content blocks the directory.
9. A ready selected directory becomes blocked if one of its selected descendants is blocked (except already-missing descendants).

All containment and selected-path keys are case-insensitive on Windows.

### 15.3 Confirmation, revalidation, and execution

- Show preflight Ready, Blocked, and Already Missing counts before confirmation.
- Immediately before mutation, rerun preflight on every previously ready item to close the time-of-check/time-of-use window.
- Process deepest paths first and files before their parent directories.
- Before moving a directory, verify it is empty after child operations; otherwise block it.
- Move each ready item through Windows Shell `IFileOperation` with undo/recycle semantics. Use flags equivalent to `FOF_ALLOWUNDO | FOF_NOCONFIRMATION | FOF_NOERRORUI | FOF_SILENT | FOFX_RECYCLEONDELETE | FOFX_EARLYFAILURE`.
- Detect operations the Shell reports as aborted.
- Never fall back to permanent deletion if the Recycle Bin move fails.
- Keep per-item success/failure/status results.

### 15.4 Audit and aftermath

Append one JSON object line to `%LOCALAPPDATA%\FolderSnap\roots\<root-id>\cleanup-log.jsonl` containing UTC timestamp, root ID, Before ID, After ID, and all results. Preserve prior lines through an atomic rewrite.

Report moved, blocked/changed, already-missing, and failed totals. Cleanup does not mutate historical snapshots. Prompt users to take a new snapshot to capture the live post-cleanup state.

## 16. Windows desktop integration

### 16.1 Process lifecycle

- Support `--background` to start hidden in the notification area.
- Run as a GUI-subsystem executable without a console window.
- Enable per-monitor-v2 DPI awareness, with a fallback for older systems. Qt usually handles this, but verify the manifest/runtime behavior.

### 16.2 Single instance

- Acquire a named local mutex equivalent to `Local\FolderSnap.SingleInstance`.
- If it already exists, find/activate/restore the existing main window and terminate the new process.
- For a robust Qt implementation, prefer `QLocalServer`/`QLocalSocket` for activation messaging plus a mutex for race-free ownership.

### 16.3 Notification area

Keep a tray icon for the process lifetime with actions equivalent to:

- Open FolderSnap.
- Take Snapshot Now for the currently selected root; no selected root means no operation.
- Open Settings.
- Quit FolderSnap.

Left-click opens/restores the application. Scheduled failures produce tray notifications. Scheduled successes notify only when enabled. Manual failures are surfaced interactively. If `closeToTray` is true, closing the window hides it; otherwise it exits. Settings must always expose an explicit Quit action.

### 16.4 Startup registration

Use `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, value name `FolderSnap`, and command:

```text
"<absolute-executable-path>" --background
```

Enabling/disabling the setting updates this registry value. On startup, if configuration says enabled, repair/rewrite the registration so moving/updating the executable corrects the path.

### 16.5 Explorer integration

Support opening both a watched root and the FolderSnap data directory in Windows Explorer.

## 17. Logging and diagnostics

- Log to `%LOCALAPPDATA%\FolderSnap\logs\foldersnap.log` using UTC timestamps.
- At startup, if the active log exceeds 5 MiB, delete `.1` and rename the active log to `.1`.
- Log startup/background mode, shutdown/exit code, scan summaries, scan failures, repair failures, startup-registration repair failures, and dropped events.
- Do not log file contents because the application never reads them.

## 18. Concurrency and responsiveness requirements

- The GUI thread performs no folder traversal, gzip decode, diff merge, cleanup walk, or Shell file operation.
- Use cancellable worker tasks (`QThreadPool`, `QtConcurrent`, `std::jthread`, or a controlled combination).
- Global maximum: two simultaneous root scans.
- Per-root maximum: one scan, with coalesced pending request flags.
- Comparison maximum: one active calculation; pair/root changes cancel and invalidate it.
- History index operations require reader/writer synchronization, with all mutations serialized.
- Event delivery must be bounded or naturally backpressured. If progress is coalesced/dropped, completion and failure events must remain reliable.
- Recover worker exceptions at task boundaries and convert them to failures/signals rather than terminating the process.

## 19. Suggested implementation phases

### Phase 1 — Project foundation

- Configure CMake, Qt 6, C++20, zlib, tests, Windows GUI target, resources, and CI/build presets.
- Implement domain types, JSON serializers, UUID generation, timestamp conversion, and typed errors.
- Add canonical JSON and gzip fixtures that exercise every persisted field, including Unicode paths, zero values, timestamps with fractional seconds, warnings, and all schedule kinds.

Exit criteria: C++ reads and round-trips the documented `config.json`, `index.json`, and `.snapshot` contracts without semantic loss.

### Phase 2 — Safe paths, ignore rules, and atomic files

- Implement normalized root/relative paths, storage-ID validation, safe root joining, and data-directory protection.
- Implement the complete ordered ignore matcher and path-test API.
- Implement atomic replace and corruption backup helpers.

Exit criteria: traversal/absolute-path attacks are rejected and ignore tests cover anchoring, negation, directory rules, wildcard depth, casing, and last-match-wins.

### Phase 3 — Configuration and history store

- Implement defaults, config load/save, size/schema checks, malformed-config backup.
- Implement gzip payload save/load, global index, descriptions, per-root retention, tombstone deletion, and startup repair.
- Verify concurrent saves do not lose records.

Exit criteria: interruption simulations recover orphan payloads/tombstones and preserve indexed missing payloads.

### Phase 4 — Scanner

- Implement cancellable concurrent traversal and Windows metadata extraction.
- Apply ignore and reparse rules, warning handling, sorted output, counts, and progress.

Exit criteria: single-worker and multi-worker scans yield the same deterministic snapshot, root failures save nothing, and descendant failures yield warnings.

### Phase 5 — Diff engine and tree projection

- Implement chronological validation, linear merge, classifications, uncertainty/scope logic, summary, cancellation, and compact changed-entry ownership.
- Implement filter/search and hierarchical tree projection with inferred parents and descending file-size order.

Exit criteria: comparison fixtures produce the specified classifications and ordering, including natural ordering, type changes, scope changes, and 100k+ entry responsiveness.

### Phase 6 — Application service and scheduler

- Implement watched-root commands, scan slots, per-root queue/coalescing, signals, state persistence, and shutdown cancellation.
- Implement all schedule types, next-due persistence, one-run catch-up, and archived behavior.

Exit criteria: overlapping manual/scheduled requests obey ordering and catch-up never creates a burst of missed snapshots.

### Phase 7 — Fresh Qt UI

- Build a new interface around root management, history, explicit A/B comparison, tree results, warnings, settings, and progress.
- Add per-snapshot dropdown actions for Export to HTML and Export to CSV, plus completed-comparison HTML/CSV export actions, with missing-payload handling and worker-thread progress/errors.
- Preserve the interaction contracts in Sections 11, 13, and 14 while designing the UI freely.
- Handle missing payloads, archived roots, empty states, in-flight cancellation, and errors.

Exit criteria: all workflows can be completed with no GUI-thread stalls and no implicit A/B selection.

### Phase 8 — Cleanup

- Implement hierarchical empty-by-default selection, preflight, confirmation, revalidation, deepest-first execution, Shell Recycle Bin adapter, and audit.
- Add race-condition, reparse, untracked-content, casing, and Recycle Bin failure tests.

Exit criteria: no path outside the root can be targeted and no code path permanently deletes a candidate.

### Phase 9 — Windows integration and release hardening

- Add single-instance activation, tray lifecycle/actions/notifications, close-to-tray, startup registry, Explorer actions, DPI behavior, icon/version resources, logging, and `--background`.
- Package and verify the standalone HTML template as a versioned application asset; validate default export filenames and Windows file-dialog behavior.
- Exercise sleep/resume, app restart while schedules are overdue, inaccessible/network roots, long paths, Unicode paths, and large histories.

Exit criteria: packaged app runs without a console or external runtime surprises on clean Windows 10/11 x64 machines.

## 20. Minimum automated test matrix

Implement automated coverage for every behavior below:

- Config round-trip and malformed-file preservation.
- Atomic-write failure preserving the original and removing temporary files.
- Safe path joining, normalized casing/separators, and unsafe storage IDs.
- Data directory cannot be watched and is excluded when nested beneath a watched ancestor.
- Ignore matching and last-rule-wins behavior.
- Deterministic scan output across worker counts, exclusions, cancellation, callback failure, descendant warnings, and root failure.
- Global-index save/load, per-root retention, description update, 16 concurrent saves, delete/clear isolation, missing payload visibility, unsafe persisted IDs/paths, cancellation, corrupt-index rebuild, orphan recovery, and tombstone repair.
- Core diff classifications, chronological input swap, cancellation, very large mostly-unchanged comparisons, natural ordering, uncertainty, scope differences, and attribute-only file noise suppression.
- Real filesystem adjacent-snapshot integration covering Added, Removed, Modified, rename-as-add/remove, and type changes.
- No automatic snapshot-pair selection; click ordering, third-click rollover, deselection, refresh preservation, and missing-record removal.
- Comparison tree expansion at multiple depths, inferred parents, filters/search, folder-first order, descending effective file size, and stable tie-breaking.
- Snapshot HTML/CSV exports, template JSON escaping, offline HTML behavior, tree rendering, search, Name/Size/Creation-date sorting in both directions, CSV quoting/BOM, missing-payload handling, cancellation, and immutability.
- Comparison HTML/CSV exports, Added/Removed/Modified colored rows for files and folders, status text accessible without color, recursive Before/After folder totals, comparison search/filter/sort, structural parents, and stale-pair protection.
- All schedule kinds, monthly clamping, missed-run collapse, anchor preservation, and local/UTC conversions.
- Cleanup plan eligibility, root-target rejection, traversal rejection, reparse ancestors, changed files, type changes, extra directory content, selected-child failure propagation, Windows case-insensitivity, second preflight race closure, deepest-first ordering, Recycle Bin failure with no fallback, and audit ID safety.
- Cleanup selection begins empty, directory propagation, ancestor clearing, indeterminate state, and filtering without losing selections.
- Application job tests for the two-scan limit, per-root coalescing, Manual-before-Scheduled pending order, archived rejection, and shutdown cancellation.
- Single-instance activation, startup registry command quoting, tray commands, scheduled notification policy, and close-to-tray lifecycle.

## 21. Data-versioning and development isolation

Schema version 2 is the initial released data contract defined in Section 4. Treat it as immutable once released. Any incompatible future change must introduce a new schema version, a documented migration, a backup before migration, and fixtures for both source and destination formats.

Support a command-line or environment override for an isolated development/test data directory. The released default remains `%LOCALAPPDATA%\FolderSnap`; development builds must not use a user's production data directory by accident.

## 22. Explicit non-features and boundaries

Keep these boundaries during implementation:

- No file-content backup or restoration.
- No content hashing.
- No rename/move inference.
- No continuous filesystem watcher or NTFS journal integration.
- No cloud sync, account, service, telemetry, or network API.
- No permanent deletion fallback during cleanup.
- No cross-root comparison.
- No automatic selection of the newest two snapshots.
- The UI should be designed fresh; only the interaction and safety requirements in this plan are mandatory.

## 23. Definition of done

The reimplementation is feature-complete when:

- Schema-v2 FolderSnap data is written and loaded safely according to this document.
- Every feature and safety invariant in this plan has automated coverage where practical.
- Manual and scheduled snapshots remain responsive and deterministic on large trees.
- Explicit A/B comparisons produce equivalent classifications and summaries.
- Results support the multi-level expandable tree and largest-first file ordering.
- Each saved snapshot can export a standalone searchable/sortable HTML tree and a complete CSV metadata report without changing history or the filesystem.
- Each completed comparison can export a standalone searchable/sortable HTML tree and a complete CSV change report with accessible Added/Removed/Modified row styling.
- Cleanup can only move validated Added items to the Recycle Bin and produces an audit record.
- Crash/interruption recovery preserves history consistency.
- Tray, startup, single-instance, notifications, Explorer integration, and background launch work on Windows 10/11 x64.
- The fresh UI exposes all required workflows.
