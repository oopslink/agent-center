package agentsupervisor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ReadEventsFrom reads the persistent events cursor (events.jsonl under
// homeDir) starting at byte offset `from`, returning the raw bytes appended at
// or after that offset and the new end offset. This is the READ side of the
// future daemon re-attach (s2): a reader that was last at offset N re-opens the
// file and resumes from N with no consumer-side state in the supervisor.
//
// Returns (nil, from, nil) when there is nothing new (from >= file size). A
// `from` past EOF is clamped to EOF rather than erroring (a reattach may have a
// stale offset after an s2 ack-truncation).
func ReadEventsFrom(homeDir string, from int64) (data []byte, end int64, err error) {
	path := filepath.Join(homeDir, EventsFileName)
	f, err := os.Open(path)
	if err != nil {
		return nil, from, fmt.Errorf("agentsupervisor: open events file: %w", err)
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, from, fmt.Errorf("agentsupervisor: stat events file: %w", err)
	}
	size := st.Size()
	if from < 0 {
		from = 0
	}
	if from >= size {
		return nil, size, nil
	}
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return nil, from, fmt.Errorf("agentsupervisor: seek events file: %w", err)
	}
	buf := make([]byte, size-from)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, from, fmt.Errorf("agentsupervisor: read events file: %w", err)
	}
	return buf[:n], from + int64(n), nil
}
