package taskexec

import (
	"os"
	"path/filepath"
	"strings"
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

// TestContextRecovery_SlowPath_IgnoresArchivedSegments asserts the design §10
// rule: slow-path replay reads ONLY the current segment; historical ".gz"
// archives are audit-only and must not be replayed (W3 alignment).
func TestContextRecovery_SlowPath_IgnoresArchivedSegments(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task-1")
	os.MkdirAll(taskDir, 0o700)
	w := NewEventStreamWriter()

	// Build an archived segment from old events, then a fresh current segment.
	old := strings.Repeat(`{"id":"ev-old","event_type":"assistant_text","payload":"{}","occurred_at":"2026-06-27T00:00:00Z"}`+"\n", 3)
	if err := os.WriteFile(filepath.Join(taskDir, "events.current.jsonl"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := w.UpdateOffset(taskDir, EventOffset{Segment: "current", ByteOffset: int64(len(old))}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.MaybeRollSegment(taskDir, int64(len(old))); err != nil {
		t.Fatalf("roll: %v", err)
	}
	// Now append one event to the fresh current segment.
	if err := w.Append(taskDir, RawEvent{ID: "ev-new", EventType: "assistant_text", Payload: `{}`, OccurredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	// Sanity: an archive really exists alongside current.
	if segs, _ := w.ListArchivedSegments(taskDir); len(segs) != 1 {
		t.Fatalf("expected 1 archived segment, got %v", segs)
	}

	events, err := r(t).RecoverTask(taskDir, false)
	if err != nil {
		t.Fatalf("slow path: %v", err)
	}
	if len(events) != 1 || events[0].ID != "ev-new" {
		t.Fatalf("slow path must replay only current segment, got %+v", events)
	}
}

// r is a tiny helper so the archived-segment test reads cleanly.
func r(t *testing.T) *ContextRecovery {
	t.Helper()
	return DefaultContextRecovery()
}

func TestDefaultContextRecovery_Defaults(t *testing.T) {
	r := DefaultContextRecovery()
	if r.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", r.MaxRetries)
	}
}
