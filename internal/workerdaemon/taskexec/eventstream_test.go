package taskexec

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEventStreamWriter_Append_And_ReadAll(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task-1")
	os.MkdirAll(taskDir, 0o700)
	w := NewEventStreamWriter()

	ev1 := RawEvent{
		ID:         "ev-1",
		EventType:  "assistant_text",
		Payload:    `{"text":"hello"}`,
		OccurredAt: time.Now().UTC(),
	}
	ev2 := RawEvent{
		ID:         "ev-2",
		EventType:  "tool_use",
		Payload:    `{"tool_name":"grep"}`,
		OccurredAt: time.Now().UTC(),
	}
	if err := w.Append(taskDir, ev1); err != nil {
		t.Fatalf("Append ev1: %v", err)
	}
	if err := w.Append(taskDir, ev2); err != nil {
		t.Fatalf("Append ev2: %v", err)
	}

	events, err := w.ReadAll(taskDir)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("ReadAll = %d events, want 2", len(events))
	}
	if events[0].ID != "ev-1" || events[1].ID != "ev-2" {
		t.Errorf("events = %+v", events)
	}
}

func TestEventStreamWriter_ReadAll_MissingFile(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task-1")
	os.MkdirAll(taskDir, 0o700)
	w := NewEventStreamWriter()
	events, err := w.ReadAll(taskDir)
	if err != nil {
		t.Fatalf("ReadAll on missing: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestEventOffset_ReadWrite(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task-1")
	os.MkdirAll(taskDir, 0o700)
	w := NewEventStreamWriter()

	off := EventOffset{Segment: "current", ByteOffset: 1024, LastEventID: "ev-5"}
	if err := w.UpdateOffset(taskDir, off); err != nil {
		t.Fatalf("UpdateOffset: %v", err)
	}
	got, err := w.ReadOffset(taskDir)
	if err != nil {
		t.Fatalf("ReadOffset: %v", err)
	}
	if got.Segment != "current" || got.ByteOffset != 1024 || got.LastEventID != "ev-5" {
		t.Errorf("offset = %+v", got)
	}
}

func TestEventOffset_ReadMissing(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task-1")
	os.MkdirAll(taskDir, 0o700)
	w := NewEventStreamWriter()
	off, err := w.ReadOffset(taskDir)
	if err != nil {
		t.Fatalf("ReadOffset on missing: %v", err)
	}
	if off.ByteOffset != 0 || off.LastEventID != "" {
		t.Errorf("expected zero offset, got %+v", off)
	}
}

func TestEventStreamWriter_Append_ToNonexistentDir(t *testing.T) {
	w := NewEventStreamWriter()
	ev := RawEvent{ID: "ev-x", EventType: "test", Payload: `{}`, OccurredAt: time.Now().UTC()}
	err := w.Append("/nonexistent/dir/that/does/not/exist", ev)
	if err == nil {
		t.Fatal("expected error appending to nonexistent dir, got nil")
	}
}

func TestEventStreamWriter_ReadAll_UnreadableDir(t *testing.T) {
	// Create a file where the events file path would be, making the directory
	// component a file (so open fails with a non-ErrNotExist error)
	base := t.TempDir()
	// Use a path whose parent directory is actually a file
	taskDir := filepath.Join(base, "task-blocker")
	// Write a file at taskDir so that filepath.Join(taskDir, eventsCurrentFile) fails to open
	if err := os.WriteFile(taskDir, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := NewEventStreamWriter()
	// taskDir is a file, so joining eventsCurrentFile to it will fail on open
	_, err := w.ReadAll(taskDir)
	if err == nil {
		t.Fatal("expected error opening events in file-as-dir, got nil")
	}
}

func TestEventOffset_ReadOffset_CorruptFile(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task-1")
	os.MkdirAll(taskDir, 0o700)
	// Write corrupt JSON to the offset file
	offsetPath := filepath.Join(taskDir, "events.offset")
	if err := os.WriteFile(offsetPath, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := NewEventStreamWriter()
	_, err := w.ReadOffset(taskDir)
	if err == nil {
		t.Fatal("expected error reading corrupt offset file, got nil")
	}
}
