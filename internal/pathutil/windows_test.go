package pathutil

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestJoinWithinRootRejectsTraversalAndAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	for _, value := range []string{`..\outside.txt`, `../outside.txt`, `C:\outside.txt`, `\server\share\outside.txt`, `/outside.txt`} {
		if _, err := JoinWithinRoot(root, value); !errors.Is(err, ErrOutsideRoot) {
			t.Errorf("JoinWithinRoot(%q) error = %v, want ErrOutsideRoot", value, err)
		}
	}
	target, err := JoinWithinRoot(root, `nested\safe.txt`)
	if err != nil || target != filepath.Join(root, "nested", "safe.txt") {
		t.Fatalf("safe path = %q, %v", target, err)
	}
}

func TestValidateStorageID(t *testing.T) {
	for _, value := range []string{"snapshot-123", "a", "root_1"} {
		if err := ValidateStorageID(value); err != nil {
			t.Errorf("ValidateStorageID(%q) = %v", value, err)
		}
	}
	for _, value := range []string{"", ".", "..", `..\escape`, "../escape", `C:escape`, "a/b"} {
		if !errors.Is(ValidateStorageID(value), ErrUnsafeStorageID) {
			t.Errorf("ValidateStorageID(%q) did not reject unsafe value", value)
		}
	}
}
