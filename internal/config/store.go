package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"foldersnap/internal/atomicfile"
	"foldersnap/internal/model"
)

type Store struct{ DataDir string }

func LocalDataDir() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return "", errors.New("LOCALAPPDATA is not set")
	}
	return filepath.Join(base, "FolderSnap"), nil
}

func (s Store) path() string { return filepath.Join(s.DataDir, "config.json") }

func (s Store) Load() (model.Config, error) {
	file, err := os.Open(s.path())
	if errors.Is(err, os.ErrNotExist) {
		return model.DefaultConfig(), nil
	}
	if err != nil {
		return model.Config{}, err
	}
	defer file.Close()
	var cfg model.Config
	decoder := json.NewDecoder(io.LimitReader(file, 16<<20))
	if err := decoder.Decode(&cfg); err != nil {
		backup := fmt.Sprintf("%s.corrupt-%s", s.path(), time.Now().UTC().Format("20060102T150405Z"))
		_ = copyFile(s.path(), backup)
		return model.Config{}, fmt.Errorf("config is malformed; preserved as %s: %w", backup, err)
	}
	if cfg.SchemaVersion != model.SchemaVersion {
		return model.Config{}, fmt.Errorf("unsupported config schema %d", cfg.SchemaVersion)
	}
	return cfg, nil
}

func (s Store) Save(cfg model.Config) error {
	cfg.SchemaVersion = model.SchemaVersion
	return atomicfile.Write(s.path(), 0o600, func(file *os.File) error {
		encoder := json.NewEncoder(file)
		encoder.SetIndent("", "  ")
		return encoder.Encode(cfg)
	})
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
