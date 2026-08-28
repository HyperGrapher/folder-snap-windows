package config

import (
	"os"
	"path/filepath"
	"testing"

	"foldersnap/internal/model"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	store := Store{DataDir: t.TempDir()}
	cfg := model.DefaultConfig()
	cfg.DefaultRetention = 25
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil || loaded.SchemaVersion != model.SchemaVersion || loaded.DefaultRetention != 25 {
		t.Fatalf("loaded = %+v, err = %v", loaded, err)
	}
}

func TestMalformedConfigIsPreservedAsCorruptBackup(t *testing.T) {
	store := Store{DataDir: t.TempDir()}
	if err := os.WriteFile(store.path(), []byte(`{"schemaVersion":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("malformed config unexpectedly loaded")
	}
	backups, err := filepath.Glob(store.path() + ".corrupt-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %v, err = %v", backups, err)
	}
	data, err := os.ReadFile(backups[0])
	if err != nil || string(data) != `{"schemaVersion":` {
		t.Fatalf("backup = %q, err = %v", data, err)
	}
}
