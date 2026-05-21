package shim

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteAtomic_RenameFails exercises the os.Rename error branch by
// pointing the destination at an existing directory (which Rename cannot
// overwrite atomically on macOS / Linux). Covers the .tmp cleanup path
// in writeAtomic.
func TestWriteAtomic_RenameFails(t *testing.T) {
	tmp := t.TempDir()
	dest := filepath.Join(tmp, "dest")
	// Make `dest` a directory; os.Rename of a file onto a non-empty
	// directory must fail (or even an empty dir on most platforms).
	if err := os.MkdirAll(filepath.Join(dest, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := writeAtomic(dest, []byte("hi"), 0o600)
	if err == nil {
		t.Fatal("expected rename error")
	}
	// The temp file should be cleaned up after the failed rename.
	if _, statErr := os.Stat(dest + ".tmp"); !os.IsNotExist(statErr) {
		t.Fatalf("expected tmp file removed, got %v", statErr)
	}
}

// TestWriteAtomic_WriteFails exercises the os.WriteFile error branch by
// writing into a directory whose parent lacks write permission. On macOS
// /Linux a 0o555 (read+exec only) parent rejects file creation.
func TestWriteAtomic_WriteFails(t *testing.T) {
	parent := t.TempDir()
	// Restrict parent so writing inside fails. Restore mode in cleanup so
	// t.TempDir can remove it.
	if err := os.Chmod(parent, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })
	dest := filepath.Join(parent, "x")
	err := writeAtomic(dest, []byte("hi"), 0o600)
	if err == nil {
		// Running as root or on a CI volume that ignores chmod — skip.
		t.Skip("file creation succeeded despite read-only parent; environment doesn't enforce mode")
	}
	if !strings.Contains(err.Error(), "permission") && !strings.Contains(err.Error(), "denied") && !strings.Contains(err.Error(), "read-only") {
		t.Logf("non-permission error is fine: %v", err)
	}
}
