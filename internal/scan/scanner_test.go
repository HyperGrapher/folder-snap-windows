package scan

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"foldersnap/internal/model"
)

func TestScanSortedAndIgnored(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "src"))
	mustMkdir(t, filepath.Join(root, "node_modules", "pkg"))
	mustWrite(t, filepath.Join(root, "src", "B.txt"), "hello")
	mustWrite(t, filepath.Join(root, "src", "a.txt"), "world!")
	mustWrite(t, filepath.Join(root, "node_modules", "pkg", "x.js"), "ignored")

	snapshot, err := (Scanner{}).Scan(context.Background(), Request{
		RootID: "root", RootPath: root, Trigger: model.TriggerManual,
		IgnoreRules: []string{"node_modules/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Header.FileCount != 2 || snapshot.Header.TotalFileBytes != 11 {
		t.Fatalf("unexpected totals: %+v", snapshot.Header)
	}
	for i := 1; i < len(snapshot.Entries); i++ {
		if snapshot.Entries[i-1].RelativePath > snapshot.Entries[i].RelativePath {
			t.Fatal("entries are not sorted")
		}
	}
	for _, entry := range snapshot.Entries {
		if entry.RelativePath == "node_modules" || entry.RelativePath == "node_modules/pkg/x.js" {
			t.Fatalf("ignored entry included: %s", entry.RelativePath)
		}
	}
}

func TestRootFailureDoesNotProduceSnapshot(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), Request{RootID: "root", RootPath: filepath.Join(t.TempDir(), "missing")})
	if err == nil {
		t.Fatal("expected error")
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
