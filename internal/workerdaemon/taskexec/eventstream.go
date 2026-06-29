package taskexec

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"
)

const (
	eventsCurrentFile = "events.current.jsonl"
	eventsOffsetFile  = "events.offset"

	// DefaultSegmentMaxBytes is the size threshold at which the current event
	// segment becomes eligible for rolling/archival (design §8.1). Adjustable
	// in review.
	DefaultSegmentMaxBytes int64 = 8 << 20 // 8 MiB

	// currentSegmentName is the offset.Segment value while Center is consuming
	// the live events.current.jsonl segment.
	currentSegmentName = "current"
)

// segmentSeqRe matches archived/rolled segment filenames: events.000001.jsonl
// and events.000001.jsonl.gz. Group 1 is the zero-padded sequence number;
// group 2 is the optional ".gz" suffix.
var segmentSeqRe = regexp.MustCompile(`^events\.(\d{6})\.jsonl(\.gz)?$`)

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

// CurrentSegmentSize returns the byte size of events.current.jsonl in taskDir.
// A missing file is not an error — it returns 0 (nothing written yet).
func (w *EventStreamWriter) CurrentSegmentSize(taskDir string) (int64, error) {
	info, err := os.Stat(filepath.Join(taskDir, eventsCurrentFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("taskexec: stat current segment: %w", err)
	}
	return info.Size(), nil
}

// AckCurrent advances events.offset to the full extent of events.current.jsonl,
// recording lastEventID (when non-empty). This is the local "Center has consumed
// up to here" cursor (design §8.1): after the agent delivers an event to the
// Center activity stream, the runtime acks it locally by advancing the offset to
// the current segment's EOF. A fully-acked segment (ByteOffset >= size) is what
// gates archival in MaybeRollSegment. Returns the acked byte size.
func (w *EventStreamWriter) AckCurrent(taskDir, lastEventID string) (int64, error) {
	size, err := w.CurrentSegmentSize(taskDir)
	if err != nil {
		return 0, err
	}
	off, err := w.ReadOffset(taskDir)
	if err != nil {
		return 0, fmt.Errorf("taskexec: read offset for ack: %w", err)
	}
	off.Segment = currentSegmentName
	off.ByteOffset = size
	if lastEventID != "" {
		off.LastEventID = lastEventID
	}
	if err := w.UpdateOffset(taskDir, off); err != nil {
		return 0, fmt.Errorf("taskexec: ack offset: %w", err)
	}
	return size, nil
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

// MaybeRollSegment rolls events.current.jsonl into a compressed archive segment
// when both conditions hold (design §8.1):
//
//  1. the current segment has reached threshold bytes (default
//     DefaultSegmentMaxBytes when threshold <= 0), and
//  2. Center has acked the entire current segment — i.e. offset.Segment is
//     "current" and offset.ByteOffset >= the current segment size.
//
// Un-acked data is never archived: if the tail isn't fully acked, this is a
// no-op and returns "". On a successful roll it returns the archived ".gz"
// filename (e.g. "events.000001.jsonl.gz").
//
// Steps:
//  1. atomic rename events.current.jsonl → events.{seq}.jsonl (snapshot; frees
//     the current name so the next Append O_CREATEs a fresh current segment)
//  2. gzip events.{seq}.jsonl → events.{seq}.jsonl.gz (atomic via .tmp rename),
//     then remove the plain snapshot
//  3. recreate an empty events.current.jsonl
//  4. reset offset to {Segment: "current", ByteOffset: 0, LastEventID: kept}
//
// History ".gz" segments are audit-only and never replayed (see ReadAll /
// ContextRecovery, design §10).
func (w *EventStreamWriter) MaybeRollSegment(taskDir string, threshold int64) (string, error) {
	if threshold <= 0 {
		threshold = DefaultSegmentMaxBytes
	}
	curPath := filepath.Join(taskDir, eventsCurrentFile)
	info, err := os.Stat(curPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil // nothing to roll yet
		}
		return "", fmt.Errorf("taskexec: stat current segment: %w", err)
	}
	size := info.Size()
	if size < threshold {
		return "", nil // below threshold
	}

	off, err := w.ReadOffset(taskDir)
	if err != nil {
		return "", fmt.Errorf("taskexec: read offset for roll: %w", err)
	}
	// Only archive a segment whose tail Center has fully acked, so un-acked
	// data is never compressed away.
	if off.Segment != currentSegmentName || off.ByteOffset < size {
		return "", nil
	}

	seq, err := nextSegmentSeq(taskDir)
	if err != nil {
		return "", err
	}
	plainName := segmentFileName(seq)
	plainPath := filepath.Join(taskDir, plainName)

	// 1. Atomic snapshot.
	if err := os.Rename(curPath, plainPath); err != nil {
		return "", fmt.Errorf("taskexec: rename current segment: %w", err)
	}

	// 2. Compress, then drop the plain snapshot.
	gzName := plainName + ".gz"
	if err := gzipFile(plainPath, filepath.Join(taskDir, gzName)); err != nil {
		return "", fmt.Errorf("taskexec: gzip segment: %w", err)
	}
	if err := os.Remove(plainPath); err != nil {
		return "", fmt.Errorf("taskexec: remove plain segment: %w", err)
	}

	// 3. Recreate an empty current segment. O_CREATE without O_TRUNC is safe if
	//    a concurrent Append already recreated and wrote to it.
	f, err := os.OpenFile(curPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("taskexec: recreate current segment: %w", err)
	}
	_ = f.Close()

	// 4. Reset offset to the fresh (empty) current; keep the last acked event id.
	off.Segment = currentSegmentName
	off.ByteOffset = 0
	if err := w.UpdateOffset(taskDir, off); err != nil {
		return "", fmt.Errorf("taskexec: reset offset after roll: %w", err)
	}
	return gzName, nil
}

// ListArchivedSegments returns the archived ".gz" segment filenames in taskDir,
// sorted oldest-first by sequence. These are audit-only and never replayed
// (design §8.1, §10). Missing taskDir returns a nil slice.
func (w *EventStreamWriter) ListArchivedSegments(taskDir string) ([]string, error) {
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("taskexec: list archived segments: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if m := segmentSeqRe.FindStringSubmatch(e.Name()); m != nil && m[2] == ".gz" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // zero-padded seq → lexical order == numeric order
	return names, nil
}

// segmentFileName returns the plain rolled-segment name for a sequence number,
// zero-padded to 6 digits (e.g. "events.000001.jsonl").
func segmentFileName(seq int) string {
	return fmt.Sprintf("events.%06d.jsonl", seq)
}

// nextSegmentSeq returns max(existing segment seq) + 1 in taskDir (1 when none
// exist), considering both plain and ".gz" segments so a crash mid-gzip cannot
// cause a seq collision.
func nextSegmentSeq(taskDir string) (int, error) {
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return 0, fmt.Errorf("taskexec: scan segments: %w", err)
	}
	max := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := segmentSeqRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return max + 1, nil
}

// gzipFile gzip-compresses src into dst atomically (write dst.tmp, then rename).
func gzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(out)
	if _, err := io.Copy(gz, in); err != nil {
		_ = gz.Close()
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := gz.Close(); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
