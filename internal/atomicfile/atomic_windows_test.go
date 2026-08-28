package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFailedWritePreservesOriginalAndRemovesTemporaryFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "state.json")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("injected failure")
	err := Write(path, 0o600, func(file *os.File) error {
		if _, err := file.WriteString("partial"); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "original" {
		t.Fatalf("original = %q, err = %v", data, err)
	}
	temps, err := filepath.Glob(filepath.Join(directory, ".foldersnap-*.tmp"))
	if err != nil || len(temps) != 0 {
		t.Fatalf("temporary files = %v, err = %v", temps, err)
	}
}

func TestSuccessfulWriteAtomicallyReplacesOriginal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, 0o600, func(file *os.File) error {
		_, err := file.WriteString("new")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "new" {
		t.Fatalf("replacement = %q, err = %v", data, err)
	}
}
