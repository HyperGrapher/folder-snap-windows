# FolderSnap for Windows

FolderSnap records local metadata snapshots of selected folders and compares any two points in their history. It does not copy file contents and is not a backup product.

Snapshot comparison follows the macOS FolderSnap workflow. Select a folder, click one snapshot for A and another for B, then press **Compare A → B**. FolderSnap never preselects or silently advances the comparison pair. A selection change clears the previous result.

## Requirements

- Windows 10 or Windows 11 x64
- Go 1.26.1
- MinGW-w64 with `gcc` and `g++` on `PATH`

The pinned go-fltk dependency contains prebuilt Windows/amd64 FLTK libraries. Release builds link the GUI subsystem and do not open a console window.

## Build and test

```powershell
go test ./...
go vet ./...
.\scripts\build.ps1 -Configuration Release
```

The executable is written to `bin\FolderSnap.exe`. Use `FolderSnap.exe --background` for Windows-startup/tray-only launch.

## Local data

FolderSnap stores configuration under `%LOCALAPPDATA%\FolderSnap\config.json`. Snapshot records use a single `%LOCALAPPDATA%\FolderSnap\History\index.json`; immutable gzip-compressed payloads are stored beside it as `<snapshot-id>.snapshot`.

## Current product boundaries

Snapshots contain relative paths and filesystem metadata, not file contents. Rename and move operations appear as Removed plus Added. Content changes that preserve both size and last-write timestamp may not be detected. Cleanup, HTML/CSV export, content restoration, hashing, cloud services, and filesystem-journal integration are outside the stabilization release.
