package taskexec

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestContextRecovery_FastPath(t *testing.T) {
	r := DefaultContextRecovery()
	events, err := r.RecoverTask("/nonexistent", true)
	if err != nil {
		t.Fatalf("fast path returned error: %v", err)
	}
	if events != nil {
		t.Fatalf("fast path returned events: %v", events)
	}
}

func TestContextRecovery_SlowPath_ReadsEvents(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task-1")
	os.MkdirAll(taskDir, 0o700)

	w := NewEventStreamWriter()
	ev := RawEvent{
		ID:         "ev-1",
		EventType:  "assistant_text",
		Payload:    `{"text":"hello"}`,
		OccurredAt: time.Now().UTC(),
	}
	if err := w.Append(taskDir, ev); err != nil {
		t.Fatalf("Append: %v", err)
	}

	r := DefaultContextRecovery()
	events, err := r.RecoverTask(taskDir, false)
	if err != nil {
		t.Fatalf("slow path returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].ID != "ev-1" {
		t.Errorf("expected event id ev-1, got %s", events[0].ID)
	}
}

func TestContextRecovery_SlowPath_MissingFile(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task-empty")
	os.MkdirAll(taskDir, 0o700)

	r := DefaultContextRecovery()
	events, err := r.RecoverTask(taskDir, false)
	if err != nil {
		t.Fatalf("slow path with missing file returned error: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events from missing file, got %d", len(events))
	}
}

func TestDefaultContextRecovery_Defaults(t *testing.T) {
	r := DefaultContextRecovery()
	if r.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", r.MaxRetries)
	}
}
