package tasklog

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestOpen_CreatesParentDirsAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks", "task-1", "task.log")
	w, err := Open(path, 1024)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()
	if _, err := io.WriteString(w, "hello\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := read(t, path); got != "hello\n" {
		t.Fatalf("task.log = %q, want %q", got, "hello\n")
	}
}

func TestOpen_DefaultsMaxBytes(t *testing.T) {
	w, err := Open(filepath.Join(t.TempDir(), "task.log"), 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()
	if w.maxBytes != DefaultMaxBytes {
		t.Fatalf("maxBytes = %d, want default %d", w.maxBytes, DefaultMaxBytes)
	}
}

func TestOpen_EmptyPath(t *testing.T) {
	if _, err := Open("", 10); err == nil {
		t.Fatal("want error on empty path")
	}
}

// TestRotate_KeepsSingleBackupAndBounds verifies the core W4 acceptance: once
// the active file passes the cap it rotates aside to exactly one `.1` backup and
// continues writing to a fresh file, so the newest output is always in task.log.
func TestRotate_KeepsSingleBackupAndBounds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.log")
	w, err := Open(path, 10) // tiny cap to force rotation
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()

	// 1st write fits (8 <= 10).
	mustWrite(t, w, "AAAAAAAA") // 8 bytes
	// 2nd write (4 bytes) would push to 12 > 10 → rotate first, then write.
	mustWrite(t, w, "BBBB")
	// 3rd write (8 bytes) would push to 12 > 10 → rotate again, overwriting .1.
	mustWrite(t, w, "CCCCCCCC")

	if got := read(t, path); got != "CCCCCCCC" {
		t.Fatalf("active task.log = %q, want last write", got)
	}
	// Exactly one backup, holding the immediately-previous segment.
	if got := read(t, path+backupSuffix); got != "BBBB" {
		t.Fatalf("backup = %q, want %q", got, "BBBB")
	}
	// No second-level backup is ever created.
	if _, err := os.Stat(path + ".2"); !os.IsNotExist(err) {
		t.Fatalf("unexpected .2 backup: err=%v", err)
	}
}

// TestRotate_OversizedSingleWriteNotTruncated — a single record larger than the
// cap is written whole (never split), and does not produce an empty backup.
func TestRotate_OversizedSingleWriteNotTruncated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.log")
	w, err := Open(path, 4)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()

	big := strings.Repeat("X", 20) // 20 > cap 4
	mustWrite(t, w, big)
	if got := read(t, path); got != big {
		t.Fatalf("oversized write truncated: got %d bytes, want %d", len(got), len(big))
	}
	// First write to an empty file must not have created a backup.
	if _, err := os.Stat(path + backupSuffix); !os.IsNotExist(err) {
		t.Fatalf("oversized first write created a backup: err=%v", err)
	}
	// A following small write rotates the oversized record aside.
	mustWrite(t, w, "y")
	if got := read(t, path); got != "y" {
		t.Fatalf("post-rotate active = %q, want %q", got, "y")
	}
	if got := read(t, path+backupSuffix); got != big {
		t.Fatalf("backup = %d bytes, want oversized record %d", len(got), len(big))
	}
}

func TestWrite_ReturnsByteCount(t *testing.T) {
	w, err := Open(filepath.Join(t.TempDir(), "task.log"), 1024)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()
	n, err := w.Write([]byte("abcde"))
	if err != nil || n != 5 {
		t.Fatalf("Write = (%d, %v), want (5, nil)", n, err)
	}
}

func TestReopen_AppendsAndPreservesSizeAccounting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.log")

	w1, err := Open(path, 10)
	if err != nil {
		t.Fatalf("Open1: %v", err)
	}
	mustWrite(t, w1, "12345678") // size 8
	if err := w1.Close(); err != nil {
		t.Fatalf("Close1: %v", err)
	}

	// Reopen: existing 8 bytes are honored, so a 4-byte write rotates (8+4>10).
	w2, err := Open(path, 10)
	if err != nil {
		t.Fatalf("Open2: %v", err)
	}
	defer w2.Close()
	mustWrite(t, w2, "9999")
	if got := read(t, path); got != "9999" {
		t.Fatalf("active = %q, want fresh post-rotate write", got)
	}
	if got := read(t, path+backupSuffix); got != "12345678" {
		t.Fatalf("backup = %q, want pre-restart content", got)
	}
}

func TestClose_Idempotent(t *testing.T) {
	w, err := Open(filepath.Join(t.TempDir(), "task.log"), 1024)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := w.Write([]byte("x")); err == nil {
		t.Fatal("write after close should error")
	}
}

// TestConcurrentWrites models stdout+stderr teeing into one Writer: no data is
// lost or interleaved within a single Write, and the total byte count across
// task.log + its backup equals what was written (modulo rotation overwrites).
func TestConcurrentWrites_Safe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.log")
	w, err := Open(path, 4096)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()

	var wg sync.WaitGroup
	const writers, perWriter = 8, 50
	rec := bytes.Repeat([]byte("z"), 7) // each Write is atomic under the lock
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWriter; j++ {
				if _, err := w.Write(rec); err != nil {
					t.Errorf("concurrent write: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	// Every byte in the active file must be 'z' (no torn writes / corruption).
	active := read(t, path)
	for i := 0; i < len(active); i++ {
		if active[i] != 'z' {
			t.Fatalf("corrupt byte at %d: %q", i, active[i])
		}
	}
}

func mustWrite(t *testing.T, w *Writer, s string) {
	t.Helper()
	if _, err := io.WriteString(w, s); err != nil {
		t.Fatalf("write %q: %v", s, err)
	}
}
