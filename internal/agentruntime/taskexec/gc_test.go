package taskexec

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunGC_CleansExpiredAborted(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	os.MkdirAll(tasksDir, 0o700)

	// Create an aborted dir older than retention (8 days ago)
	oldTs := time.Now().Add(-8 * 24 * time.Hour)
	oldName := AbortedDirName("task-old", oldTs)
	os.MkdirAll(filepath.Join(tasksDir, oldName), 0o700)

	// Create a recent aborted dir (1 day ago) — should NOT be cleaned
	recentTs := time.Now().Add(-1 * 24 * time.Hour)
	recentName := AbortedDirName("task-recent", recentTs)
	os.MkdirAll(filepath.Join(tasksDir, recentName), 0o700)

	cfg := GCConfig{AbortedRetention: 7 * 24 * time.Hour, DoneRetention: 3 * 24 * time.Hour}
	result := RunGC(tasksDir, cfg, time.Now())

	if result.AbortedCleaned != 1 {
		t.Errorf("AbortedCleaned = %d, want 1", result.AbortedCleaned)
	}
	// Old dir gone
	if _, err := os.Stat(filepath.Join(tasksDir, oldName)); !os.IsNotExist(err) {
		t.Error("old aborted dir should be gone")
	}
	// Recent dir still there
	if _, err := os.Stat(filepath.Join(tasksDir, recentName)); err != nil {
		t.Error("recent aborted dir should still exist")
	}
}

func TestRunGC_CleansExpiredDone(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	old := time.Now().Add(-4 * 24 * time.Hour)
	// Create a done task dir
	meta := TaskExecutionMeta{TaskID: "task-done", Status: StatusDone, CreatedAt: old, UpdatedAt: old}
	dm.Create(tasksDir, meta, ExecutionContext{})

	cfg := GCConfig{AbortedRetention: 7 * 24 * time.Hour, DoneRetention: 3 * 24 * time.Hour}
	result := RunGC(tasksDir, cfg, time.Now())

	if result.DoneCleaned != 1 {
		t.Errorf("DoneCleaned = %d, want 1", result.DoneCleaned)
	}
}

func TestRunGC_SkipsRunningTasks(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	old := time.Now().Add(-10 * 24 * time.Hour)
	meta := TaskExecutionMeta{TaskID: "task-active", Status: StatusRunning, CreatedAt: old, UpdatedAt: old}
	dm.Create(tasksDir, meta, ExecutionContext{})

	cfg := GCConfig{AbortedRetention: 7 * 24 * time.Hour, DoneRetention: 3 * 24 * time.Hour}
	result := RunGC(tasksDir, cfg, time.Now())

	if result.DoneCleaned != 0 {
		t.Errorf("DoneCleaned = %d, want 0 (running task not cleaned)", result.DoneCleaned)
	}
}

func TestRunGC_HandlesGCDeletingLeftover(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	os.MkdirAll(tasksDir, 0o700)
	// Create a gc_deleting leftover (previous GC crashed)
	leftover := "task-x__aborted_20260601T000000Z__gc_deleting"
	os.MkdirAll(filepath.Join(tasksDir, leftover), 0o700)

	cfg := GCConfig{AbortedRetention: 7 * 24 * time.Hour}
	result := RunGC(tasksDir, cfg, time.Now())

	if result.LeftoverCleaned != 1 {
		t.Errorf("LeftoverCleaned = %d, want 1", result.LeftoverCleaned)
	}
}

func TestRunGC_NonexistentDir(t *testing.T) {
	result := RunGC("/nonexistent/tasks/dir", GCConfig{}, time.Now())
	// Should not error on nonexistent dir (os.IsNotExist swallowed)
	if len(result.Errors) != 0 {
		t.Errorf("expected no errors for nonexistent dir, got %v", result.Errors)
	}
}

func TestRunGC_SkipsRecentDone(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	// Only 1 day old — within DoneRetention (3d)
	recent := time.Now().Add(-1 * 24 * time.Hour)
	meta := TaskExecutionMeta{TaskID: "task-new-done", Status: StatusDone, CreatedAt: recent, UpdatedAt: recent}
	dm.Create(tasksDir, meta, ExecutionContext{})

	cfg := GCConfig{AbortedRetention: 7 * 24 * time.Hour, DoneRetention: 3 * 24 * time.Hour}
	result := RunGC(tasksDir, cfg, time.Now())

	if result.DoneCleaned != 0 {
		t.Errorf("DoneCleaned = %d, want 0 (recent done task not cleaned)", result.DoneCleaned)
	}
}

func TestRunGC_ReadDirError(t *testing.T) {
	// Create a file (not a dir) where tasksDir is expected — causes a non-IsNotExist error
	tmp := t.TempDir()
	notADir := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := RunGC(notADir, GCConfig{}, time.Now())
	// ReadDir on a file returns an error
	if len(result.Errors) == 0 {
		t.Error("expected errors when tasksDir is a file, got none")
	}
}

func TestDefaultGCConfig(t *testing.T) {
	cfg := DefaultGCConfig()
	if cfg.AbortedRetention != 7*24*time.Hour {
		t.Errorf("AbortedRetention = %v, want 7d", cfg.AbortedRetention)
	}
	if cfg.DoneRetention != 3*24*time.Hour {
		t.Errorf("DoneRetention = %v, want 3d", cfg.DoneRetention)
	}
}

// TestRunGC_RemovesArchivedSegmentsWithDoneDir asserts W3 alignment with GC:
// archived ".gz" segments live inside the task dir, so an expired done dir is
// removed wholesale (archives included) under the existing DoneRetention policy
// — no separate archive GC path, no conflict.
func TestRunGC_RemovesArchivedSegmentsWithDoneDir(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	old := time.Now().Add(-5 * 24 * time.Hour)
	meta := TaskExecutionMeta{TaskID: "task-archived", Status: StatusDone, CreatedAt: old, UpdatedAt: old}
	if err := dm.Create(tasksDir, meta, ExecutionContext{}); err != nil {
		t.Fatal(err)
	}
	// Drop an archived segment inside the task dir.
	gzPath := filepath.Join(tasksDir, "task-archived", "events.000001.jsonl.gz")
	if err := os.WriteFile(gzPath, []byte("gzip-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := GCConfig{AbortedRetention: 7 * 24 * time.Hour, DoneRetention: 3 * 24 * time.Hour}
	result := RunGC(tasksDir, cfg, time.Now())

	if result.DoneCleaned != 1 {
		t.Errorf("DoneCleaned = %d, want 1", result.DoneCleaned)
	}
	if _, err := os.Stat(gzPath); !os.IsNotExist(err) {
		t.Error("archived .gz segment should be removed with its expired done dir")
	}
}

// TestRunGC_RetainsArchivedSegmentsInRecentAborted asserts an aborted dir
// (with archived segments) within AbortedRetention is kept — archive retention
// follows the dir's retention, not the other way around.
func TestRunGC_RetainsArchivedSegmentsInRecentAborted(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	os.MkdirAll(tasksDir, 0o700)
	recentTs := time.Now().Add(-1 * 24 * time.Hour)
	name := AbortedDirName("task-recent", recentTs)
	dir := filepath.Join(tasksDir, name)
	os.MkdirAll(dir, 0o700)
	gzPath := filepath.Join(dir, "events.000001.jsonl.gz")
	if err := os.WriteFile(gzPath, []byte("gzip-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := GCConfig{AbortedRetention: 7 * 24 * time.Hour, DoneRetention: 3 * 24 * time.Hour}
	result := RunGC(tasksDir, cfg, time.Now())

	if result.AbortedCleaned != 0 {
		t.Errorf("AbortedCleaned = %d, want 0 (within retention)", result.AbortedCleaned)
	}
	if _, err := os.Stat(gzPath); err != nil {
		t.Error("archived .gz in recent aborted dir should be retained")
	}
}

func TestRunGC_MultipleCategories(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	os.MkdirAll(tasksDir, 0o700)

	// Leftover gc_deleting
	leftover := "task-z__aborted_20260101T000000Z__gc_deleting"
	os.MkdirAll(filepath.Join(tasksDir, leftover), 0o700)

	// Expired aborted
	oldAbortTs := time.Now().Add(-8 * 24 * time.Hour)
	oldAbortName := AbortedDirName("task-abort", oldAbortTs)
	os.MkdirAll(filepath.Join(tasksDir, oldAbortName), 0o700)

	// Expired done
	dm := NewDirManager()
	old := time.Now().Add(-5 * 24 * time.Hour)
	meta := TaskExecutionMeta{TaskID: "task-finished", Status: StatusDone, CreatedAt: old, UpdatedAt: old}
	dm.Create(tasksDir, meta, ExecutionContext{})

	cfg := GCConfig{AbortedRetention: 7 * 24 * time.Hour, DoneRetention: 3 * 24 * time.Hour}
	result := RunGC(tasksDir, cfg, time.Now())

	if result.LeftoverCleaned != 1 {
		t.Errorf("LeftoverCleaned = %d, want 1", result.LeftoverCleaned)
	}
	if result.AbortedCleaned != 1 {
		t.Errorf("AbortedCleaned = %d, want 1", result.AbortedCleaned)
	}
	if result.DoneCleaned != 1 {
		t.Errorf("DoneCleaned = %d, want 1", result.DoneCleaned)
	}
}
