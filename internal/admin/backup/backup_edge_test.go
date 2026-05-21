package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestPruneOldDirs_NonDirectoryEntriesSkipped covers the !entry.IsDir
// branch in pruneOldDirs.
func TestPruneOldDirs_NonDirectoryEntriesSkipped(t *testing.T) {
	dir := t.TempDir()
	// Create a file with a timestamp-shaped name → should NOT be
	// pruned (it's a file, not a dir).
	f := filepath.Join(dir, "20200101-000000")
	if err := os.WriteFile(f, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	r := &Runner{
		destRoot:   dir,
		retention:  1 * time.Hour,
		removeAll:  os.RemoveAll,
		readDirAll: func(p string) ([]os.DirEntry, error) { return os.ReadDir(p) },
	}
	pruned, err := r.pruneOldDirs(time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(pruned) != 0 {
		t.Errorf("file should be skipped, got pruned=%v", pruned)
	}
	if _, err := os.Stat(f); err != nil {
		t.Errorf("file should remain: %v", err)
	}
}

// TestPruneOldDirs_RetentionInfinite ensures setting retention to a
// very large value retains everything.
func TestPruneOldDirs_RetentionInfinite(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "20200101-000000")
	if err := os.MkdirAll(old, 0o700); err != nil {
		t.Fatal(err)
	}
	r := &Runner{
		destRoot:   dir,
		retention:  100 * 365 * 24 * time.Hour, // 100 years
		removeAll:  os.RemoveAll,
		readDirAll: func(p string) ([]os.DirEntry, error) { return os.ReadDir(p) },
	}
	pruned, err := r.pruneOldDirs(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(pruned) != 0 {
		t.Errorf("with 100y retention nothing should prune: %v", pruned)
	}
}
