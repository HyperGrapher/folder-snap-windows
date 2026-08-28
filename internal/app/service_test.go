package app

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"foldersnap/internal/model"
	"foldersnap/internal/scan"
)

func TestWatchedParentExcludesFolderSnapDataDirectory(t *testing.T) {
	parent := t.TempDir()
	dataDir := filepath.Join(parent, "FolderSnap")
	service, err := New(dataDir, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	root, err := service.AddRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "visible.txt"), []byte("visible"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "private.snapshot"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := service.effectiveIgnoreRules(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := (scan.Scanner{}).Scan(context.Background(), scan.Request{RootID: root.RootID, RootPath: root.Path, Trigger: model.TriggerManual, IgnoreRules: rules})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 1 || snapshot.Entries[0].RelativePath != "visible.txt" {
		t.Fatalf("protected data directory was scanned: %+v", snapshot.Entries)
	}
}

func TestFolderSnapDataDirectoryCannotBeWatchedDirectly(t *testing.T) {
	dataDir := t.TempDir()
	service, err := New(dataDir, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if _, err := service.AddRoot(dataDir); err == nil {
		t.Fatal("expected direct data-directory watch to be rejected")
	}
}

func TestConfigAndRootReturnDefensiveIgnoreRuleCopies(t *testing.T) {
	parent := t.TempDir()
	service, err := New(filepath.Join(t.TempDir(), "FolderSnap"), log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	root, err := service.AddRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	cfg := service.Config()
	cfg.DefaultIgnoreRules[0] = "mutated-default"
	cfg.Roots[0].IgnoreRules[0] = "mutated-root"
	returnedRoot, ok := service.Root(root.RootID)
	if !ok {
		t.Fatal("root disappeared")
	}
	returnedRoot.IgnoreRules[0] = "mutated-again"
	actual := service.Config()
	if actual.DefaultIgnoreRules[0] == "mutated-default" || actual.Roots[0].IgnoreRules[0] == "mutated-root" || actual.Roots[0].IgnoreRules[0] == "mutated-again" {
		t.Fatalf("service state was mutated through a returned slice: %+v", actual)
	}
}
