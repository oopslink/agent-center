package taskexec

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	eventsCurrentFile = "events.current.jsonl"
	eventsOffsetFile  = "events.offset"
)

// RawEvent is a single event line in events.current.jsonl (design §8.2).
type RawEvent struct {
	ID             string    `json:"id"`
	EventType      string    `json:"event_type"`
	TaskRef        string    `json:"task_ref,omitempty"`
	InteractionRef string    `json:"interaction_ref,omitempty"`
	Payload        string    `json:"payload"`
	OccurredAt     time.Time `json:"occurred_at"`
}

// EventOffset tracks the Center consumption position (design §8.1).
type EventOffset struct {
	Segment     string `json:"segment"`
	ByteOffset  int64  `json:"byte_offset"`
	LastEventID string `json:"last_event_id"`
}

// EventStreamWriter manages the per-task JSONL event stream.
type EventStreamWriter struct{}

// NewEventStreamWriter returns a new EventStreamWriter.
func NewEventStreamWriter() *EventStreamWriter { return &EventStreamWriter{} }

// Append writes one event line to events.current.jsonl. Appends atomically
// by opening in O_APPEND mode.
func (w *EventStreamWriter) Append(taskDir string, ev RawEvent) error {
	path := filepath.Join(taskDir, eventsCurrentFile)
	line, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("taskexec: marshal event: %w", err)
	}
	line = append(line, '\n')

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("taskexec: open events file: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("taskexec: write event: %w", err)
	}
	return nil
}

// ReadAll reads all events from events.current.jsonl (oldest-first).
// Returns nil slice if the file doesn't exist.
func (w *EventStreamWriter) ReadAll(taskDir string) ([]RawEvent, error) {
	path := filepath.Join(taskDir, eventsCurrentFile)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("taskexec: open events: %w", err)
	}
	defer f.Close()

	var events []RawEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20) // 1MB max line
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev RawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // skip malformed lines (design §10.2: truncate dirty data)
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		return events, fmt.Errorf("taskexec: scan events: %w", err)
	}
	return events, nil
}

// ReadOffset reads events.offset. Missing file returns zero offset.
func (w *EventStreamWriter) ReadOffset(taskDir string) (EventOffset, error) {
	path := filepath.Join(taskDir, eventsOffsetFile)
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return EventOffset{}, nil
		}
		return EventOffset{}, fmt.Errorf("taskexec: read offset: %w", err)
	}
	var off EventOffset
	if err := json.Unmarshal(b, &off); err != nil {
		return EventOffset{}, fmt.Errorf("taskexec: unmarshal offset: %w", err)
	}
	return off, nil
}

// UpdateOffset writes the consumption position atomically.
func (w *EventStreamWriter) UpdateOffset(taskDir string, off EventOffset) error {
	return writeJSONAtomic(filepath.Join(taskDir, eventsOffsetFile), off)
}
