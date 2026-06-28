package taskexec

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// readGzip returns the decompressed contents of a .gz file.
func readGzip(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open gz: %v", err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()
	b, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("read gz: %v", err)
	}
	return string(b)
}

// writeRawCurrent writes raw bytes directly into events.current.jsonl, used to
// size the segment precisely in roll tests.
func writeRawCurrent(t *testing.T, taskDir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(taskDir, "events.current.jsonl"), []byte(content), 0o600); err != nil {
		t.Fatalf("write current: %v", err)
	}
}

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

func TestMaybeRollSegment_RollsWhenThresholdMetAndAcked(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task-1")
	os.MkdirAll(taskDir, 0o700)
	w := NewEventStreamWriter()

	content := strings.Repeat(`{"id":"ev","event_type":"assistant_text","payload":"{}","occurred_at":"2026-06-27T00:00:00Z"}`+"\n", 5)
	writeRawCurrent(t, taskDir, content)
	size := int64(len(content))

	// Center has fully acked the current segment (byte_offset == size).
	if err := w.UpdateOffset(taskDir, EventOffset{Segment: "current", ByteOffset: size, LastEventID: "ev-5"}); err != nil {
		t.Fatal(err)
	}

	gzName, err := w.MaybeRollSegment(taskDir, size) // threshold == size → eligible
	if err != nil {
		t.Fatalf("MaybeRollSegment: %v", err)
	}
	if gzName != "events.000001.jsonl.gz" {
		t.Fatalf("gzName = %q, want events.000001.jsonl.gz", gzName)
	}

	// Archive exists and decompresses to the exact original jsonl.
	gzPath := filepath.Join(taskDir, gzName)
	if got := readGzip(t, gzPath); got != content {
		t.Errorf("decompressed archive mismatch:\n got %q\nwant %q", got, content)
	}
	// No leftover plain snapshot or .tmp.
	if _, err := os.Stat(filepath.Join(taskDir, "events.000001.jsonl")); !os.IsNotExist(err) {
		t.Error("plain snapshot should have been removed")
	}
	if _, err := os.Stat(gzPath + ".tmp"); !os.IsNotExist(err) {
		t.Error(".tmp should have been renamed away")
	}

	// New current is empty.
	cur, err := os.ReadFile(filepath.Join(taskDir, "events.current.jsonl"))
	if err != nil {
		t.Fatalf("read new current: %v", err)
	}
	if len(cur) != 0 {
		t.Errorf("new current should be empty, got %d bytes", len(cur))
	}

	// Offset reset to fresh current, last event id preserved.
	off, err := w.ReadOffset(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	if off.Segment != "current" || off.ByteOffset != 0 || off.LastEventID != "ev-5" {
		t.Errorf("offset after roll = %+v, want {current 0 ev-5}", off)
	}
}

func TestMaybeRollSegment_NoRollWhenTailNotAcked(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task-1")
	os.MkdirAll(taskDir, 0o700)
	w := NewEventStreamWriter()

	content := strings.Repeat("x", 100)
	writeRawCurrent(t, taskDir, content)

	// Only the first half is acked → un-acked tail must NOT be archived.
	if err := w.UpdateOffset(taskDir, EventOffset{Segment: "current", ByteOffset: 50, LastEventID: "ev-1"}); err != nil {
		t.Fatal(err)
	}

	gzName, err := w.MaybeRollSegment(taskDir, 100)
	if err != nil {
		t.Fatalf("MaybeRollSegment: %v", err)
	}
	if gzName != "" {
		t.Errorf("expected no roll (tail not acked), got %q", gzName)
	}
	// Current segment untouched, no archive created.
	if got, _ := os.ReadFile(filepath.Join(taskDir, "events.current.jsonl")); string(got) != content {
		t.Error("current segment should be untouched when tail not acked")
	}
	segs, _ := w.ListArchivedSegments(taskDir)
	if len(segs) != 0 {
		t.Errorf("no archives expected, got %v", segs)
	}
}

func TestMaybeRollSegment_NoRollBelowThreshold(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task-1")
	os.MkdirAll(taskDir, 0o700)
	w := NewEventStreamWriter()

	writeRawCurrent(t, taskDir, strings.Repeat("x", 100))
	if err := w.UpdateOffset(taskDir, EventOffset{Segment: "current", ByteOffset: 100}); err != nil {
		t.Fatal(err)
	}

	gzName, err := w.MaybeRollSegment(taskDir, 1<<20) // threshold far above size
	if err != nil {
		t.Fatalf("MaybeRollSegment: %v", err)
	}
	if gzName != "" {
		t.Errorf("expected no roll below threshold, got %q", gzName)
	}
}

func TestMaybeRollSegment_MissingCurrentIsNoOp(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task-1")
	os.MkdirAll(taskDir, 0o700)
	w := NewEventStreamWriter()
	gzName, err := w.MaybeRollSegment(taskDir, 1)
	if err != nil {
		t.Fatalf("MaybeRollSegment on missing current: %v", err)
	}
	if gzName != "" {
		t.Errorf("expected no roll for missing current, got %q", gzName)
	}
}

func TestMaybeRollSegment_SequenceIncrements(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task-1")
	os.MkdirAll(taskDir, 0o700)
	w := NewEventStreamWriter()

	roll := func(content string) string {
		writeRawCurrent(t, taskDir, content)
		if err := w.UpdateOffset(taskDir, EventOffset{Segment: "current", ByteOffset: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		name, err := w.MaybeRollSegment(taskDir, int64(len(content)))
		if err != nil {
			t.Fatalf("roll: %v", err)
		}
		return name
	}

	if got := roll(strings.Repeat("a", 10)); got != "events.000001.jsonl.gz" {
		t.Fatalf("first roll = %q, want events.000001.jsonl.gz", got)
	}
	if got := roll(strings.Repeat("b", 10)); got != "events.000002.jsonl.gz" {
		t.Fatalf("second roll = %q, want events.000002.jsonl.gz", got)
	}

	segs, err := w.ListArchivedSegments(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 2 || segs[0] != "events.000001.jsonl.gz" || segs[1] != "events.000002.jsonl.gz" {
		t.Errorf("ListArchivedSegments = %v, want [000001 000002] oldest-first", segs)
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
