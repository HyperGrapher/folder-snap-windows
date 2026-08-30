package scan

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

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
	if !snapshot.EntriesSorted {
		t.Fatal("scanner did not mark sorted output")
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

func TestConcurrentScanMatchesSingleWorker(t *testing.T) {
	root := t.TempDir()
	for directory := range 12 {
		dir := filepath.Join(root, "folder-"+strconv.Itoa(directory))
		mustMkdir(t, dir)
		for file := range 20 {
			mustWrite(t, filepath.Join(dir, "file-"+strconv.Itoa(file)+".txt"), "contents")
		}
	}
	fixed := time.Unix(100, 0)
	request := Request{RootID: "root", RootPath: root, IgnoreRules: []string{"folder-3/"}}
	single, err := (Scanner{Now: func() time.Time { return fixed }, Workers: 1}).Scan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	parallel, err := (Scanner{Now: func() time.Time { return fixed }, Workers: 4}).Scan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(single.Entries) != len(parallel.Entries) {
		t.Fatalf("parallel scanner output length differs: single=%d parallel=%d", len(single.Entries), len(parallel.Entries))
	}
	for index := range single.Entries {
		left, right := single.Entries[index], parallel.Entries[index]
		// Directory modification timestamps can settle asynchronously on Windows
		// immediately after constructing the fixture, so compare stable identity
		// and metadata fields here.
		left.ModifiedUnixNs, left.CreatedUnixNs = 0, 0
		right.ModifiedUnixNs, right.CreatedUnixNs = 0, 0
		if !reflect.DeepEqual(left, right) {
			t.Fatalf("parallel entry %d differs: single=%+v parallel=%+v", index, single.Entries[index], parallel.Entries[index])
		}
	}
	if single.Header.FileCount != parallel.Header.FileCount || single.Header.DirectoryCount != parallel.Header.DirectoryCount || single.Header.TotalFileBytes != parallel.Header.TotalFileBytes {
		t.Fatalf("parallel totals differ: single=%+v parallel=%+v", single.Header, parallel.Header)
	}
}

func TestScanHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (Scanner{Workers: 4}).Scan(ctx, Request{RootID: "root", RootPath: t.TempDir()})
	if err == nil {
		t.Fatal("cancelled scan unexpectedly succeeded")
	}
}

func BenchmarkScanTree(b *testing.B) {
	root := b.TempDir()
	for directory := range 32 {
		dir := filepath.Join(root, "folder-"+strconv.Itoa(directory))
		mustMkdir(b, dir)
		for file := range 64 {
			mustWrite(b, filepath.Join(dir, "file-"+strconv.Itoa(file)+".txt"), "contents")
		}
	}
	request := Request{RootID: "root", RootPath: root}
	for _, workers := range []int{1, 4} {
		b.Run("workers-"+strconv.Itoa(workers), func(b *testing.B) {
			scanner := Scanner{Workers: workers}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := scanner.Scan(context.Background(), request); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestRootFailureDoesNotProduceSnapshot(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), Request{RootID: "root", RootPath: filepath.Join(t.TempDir(), "missing")})
	if err == nil {
		t.Fatal("expected error")
	}
}

func mustMkdir(t testing.TB, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t testing.TB, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
