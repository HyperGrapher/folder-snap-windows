# FolderSnap Windows — Data Safety Review

**Date:** 28 August 2026  
**Scope:** configuration, snapshot persistence, retention, deletion, cleanup, filesystem boundaries, and recovery

## Outcome

FolderSnap remains metadata-only: snapshots do not copy file contents, and ordinary snapshot creation/comparison never modifies watched files. The optional cleanup path moves explicitly selected added items through the Windows Recycle Bin after two validation passes.

The review found and corrected the following high-priority risks:

- concurrent root scans could race while updating the global history index;
- cleanup paths could cross a newly introduced junction or symbolic-link ancestor;
- a file could change between cleanup preflight and execution;
- watching a parent of `%LOCALAPPDATA%\FolderSnap` could snapshot FolderSnap's own growing history;
- interrupted history deletion/pruning could leave payloads that repair might later resurrect;
- malformed or path-like persisted identifiers could become storage paths;
- a malformed history index could prevent otherwise valid payload recovery;
- nested ignore-rule slices were exposed by reference from configuration getters.

## Implemented controls

- History read-modify-write operations are serialized across simultaneous scans.
- Snapshot/root identifiers and snapshot entry paths are validated before storage or loading.
- Decoded snapshot payloads are bounded to 1 GiB.
- Delete, delete-root, and retention pruning quarantine payloads before committing the index and recover safely after interruption.
- Corrupt history indexes are preserved as timestamped backups and rebuilt from valid payloads.
- Missing payloads remain visible in history instead of being silently removed.
- Atomic-write failures preserve the previous configuration/index and remove temporary files.
- FolderSnap's data directory is an enforced exclusion whenever it is inside a watched root; the data directory itself cannot be watched.
- Cleanup rejects lexical traversal and any reparse-point ancestor below the selected root.
- Cleanup revalidates size, timestamps, type, link target, directory contents, and containment immediately before Recycle Bin execution.
- Cleanup has no default selections and never falls back to permanent deletion when the Recycle Bin operation fails.
- The deletion panel is limited to eligible Added entries; partially selected folder trees clear ancestor-directory selection so the parent itself cannot be removed with unselected descendants.
- Windows cleanup containment and directory-content matching use case-insensitive canonical path keys.
- Configuration/root getters return defensive copies of ignore-rule slices.

## Residual product limitations

- Snapshot comparison uses metadata, not content hashing. A file whose content changes while size and last-write timestamp are preserved may appear unchanged.
- Snapshot files and configuration are local files protected by user-profile permissions; they are not encrypted at rest.
- The 1 GiB decoded-payload ceiling is a corruption/abuse guard, not a promise that comparisons near that size will fit every machine.
- Recycle Bin availability depends on Windows and the target volume. Unsupported targets fail closed and remain untouched.
- FolderSnap is not a content backup or restore product. Deleting snapshot history removes metadata only; it cannot restore watched files.

## Automated verification

- concurrent history saves preserve every index record and payload;
- add/remove/modify snapshot lifecycle and explicit-pair comparison;
- per-root retention and payload removal;
- missing-payload visibility and corrupt-index recovery;
- interrupted deletion quarantine recovery;
- unsafe identifier and traversal rejection;
- reparse-ancestor rejection and cleanup revalidation;
- data-directory self-exclusion;
- atomic-write rollback and malformed-config backup;
- result pagination, filtering, search, and tray action ordering;
- 50,000-entry regression and 200,000-entry comparison benchmark.
