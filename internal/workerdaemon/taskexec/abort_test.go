package taskexec

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAbortedDirName(t *testing.T) {
	ts := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	got := AbortedDirName("task-abc", ts)
	want := "task-abc__aborted_20260626T100000Z"
	if got != want {
		t.Errorf("AbortedDirName = %q, want %q", got, want)
	}
}

func TestParseAbortedDir(t *testing.T) {
	tests := []struct {
		name   string
		wantID string
		wantGC bool
		wantOK bool
	}{
		{"task-1__aborted_20260626T100000Z", "task-1", false, true},
		{"task-1__aborted_20260626T100000Z__gc_deleting", "task-1", true, true},
		{"task-1", "", false, false},
		{"", "", false, false},
	}
	for _, tt := range tests {
		id, _, gc, ok := ParseAbortedDir(tt.name)
		if ok != tt.wantOK {
			t.Errorf("ParseAbortedDir(%q) ok = %v, want %v", tt.name, ok, tt.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if id != tt.wantID {
			t.Errorf("ParseAbortedDir(%q) id = %q, want %q", tt.name, id, tt.wantID)
		}
		if gc != tt.wantGC {
			t.Errorf("ParseAbortedDir(%q) gc = %v, want %v", tt.name, gc, tt.wantGC)
		}
	}
}

func TestParseAbortedDir_Timestamp(t *testing.T) {
	wantTS := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	_, ts, _, ok := ParseAbortedDir("task-1__aborted_20260626T100000Z")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !ts.Equal(wantTS) {
		t.Errorf("ParseAbortedDir ts = %v, want %v", ts, wantTS)
	}
}

func TestGCDeletingDirName(t *testing.T) {
	abortedName := "task-abc__aborted_20260626T100000Z"
	got := GCDeletingDirName(abortedName)
	want := "task-abc__aborted_20260626T100000Z__gc_deleting"
	if got != want {
		t.Errorf("GCDeletingDirName = %q, want %q", got, want)
	}
}

func TestAbortTask(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	now := time.Now().UTC()
	meta := TaskExecutionMeta{TaskID: "task-1", Status: StatusRunning, CreatedAt: now, UpdatedAt: now}
	dm.Create(tasksDir, meta, ExecutionContext{})

	// Write an event first
	w := NewEventStreamWriter()
	w.Append(filepath.Join(tasksDir, "task-1"), RawEvent{ID: "ev-1", EventType: "assistant_text", Payload: "{}", OccurredAt: now})

	abortedName, err := AbortTask(tasksDir, "task-1", now)
	if err != nil {
		t.Fatalf("AbortTask: %v", err)
	}
	// Original dir should be gone
	if _, err := os.Stat(filepath.Join(tasksDir, "task-1")); !os.IsNotExist(err) {
		t.Error("original task dir should not exist after abort")
	}
	// Aborted dir should exist
	if _, err := os.Stat(filepath.Join(tasksDir, abortedName)); err != nil {
		t.Errorf("aborted dir %q should exist: %v", abortedName, err)
	}
	// Should have an abort event in the events file
	events, _ := w.ReadAll(filepath.Join(tasksDir, abortedName))
	if len(events) < 2 {
		t.Fatalf("expected ≥2 events (original + abort), got %d", len(events))
	}
	last := events[len(events)-1]
	if last.EventType != "lifecycle" {
		t.Errorf("last event type = %q, want lifecycle", last.EventType)
	}
}

func TestAbortTask_MissingDir(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	os.MkdirAll(tasksDir, 0o700)
	_, err := AbortTask(tasksDir, "nonexistent", time.Now())
	if err == nil {
		t.Error("expected error for missing task dir")
	}
}

func TestAbortTask_AbortedDirNameMatches(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	now := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	meta := TaskExecutionMeta{TaskID: "task-xyz", Status: StatusRunning, CreatedAt: now, UpdatedAt: now}
	dm.Create(tasksDir, meta, ExecutionContext{})

	abortedName, err := AbortTask(tasksDir, "task-xyz", now)
	if err != nil {
		t.Fatalf("AbortTask: %v", err)
	}
	want := "task-xyz__aborted_20260626T100000Z"
	if abortedName != want {
		t.Errorf("aborted dir name = %q, want %q", abortedName, want)
	}

	// ParseAbortedDir should parse it back correctly
	id, ts, gc, ok := ParseAbortedDir(abortedName)
	if !ok {
		t.Fatalf("ParseAbortedDir(%q) ok=false", abortedName)
	}
	if id != "task-xyz" {
		t.Errorf("parsed taskID = %q, want task-xyz", id)
	}
	if !ts.Equal(now) {
		t.Errorf("parsed ts = %v, want %v", ts, now)
	}
	if gc {
		t.Error("gc should be false for non-gc_deleting dir")
	}
}
