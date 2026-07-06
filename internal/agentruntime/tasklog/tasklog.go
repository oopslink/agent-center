// Package tasklog provides a size-bounded, rotating log sink for a Task CLI
// process's combined stdout/stderr (v2.16 W4 / design §3).
//
// Per the Agent Runtime design, each Task execution directory holds a
// `task.log` capturing the Task CLI process output:
//
//	tasks/{task_id}/task.log    # Task CLI 进程 stdout/stderr 日志
//
// A long-running / chatty Task could grow this file without bound, so the
// Writer enforces a rotation cap: once the active file would exceed MaxBytes it
// rotates the current file aside to a single `.1` backup and starts fresh,
// bounding on-disk usage to ~2*MaxBytes. The AgentController tees a spawned Task
// CLI process's stdout/stderr through one Writer (typically via io.MultiWriter
// so the bytes still reach the event reader), so Write is safe for concurrent
// callers.
package tasklog

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// DefaultMaxBytes is the default rotation threshold for a task.log when the
// caller does not specify one (10 MiB). With a single retained backup this caps
// a task's on-disk log footprint at ~20 MiB.
const DefaultMaxBytes int64 = 10 << 20

// backupSuffix is appended to the rotated-aside file. Exactly one backup is
// retained (each rotation overwrites it), so the footprint stays bounded.
const backupSuffix = ".1"

// Writer is a size-bounded, rotating io.WriteCloser for a task.log. The zero
// value is not usable — construct it with Open. All methods are safe for
// concurrent use.
type Writer struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	f        *os.File
	size     int64 // bytes written to the active file since the last rotation/open
	closed   bool
}

// Open opens (creating the parent directory and file as needed) the task.log at
// path for appending, with the given rotation threshold. A non-positive
// maxBytes falls back to DefaultMaxBytes. An existing file is opened in append
// mode and its current size is honored, so a process restart continues the same
// log and rotation accounting picks up where it left off.
func Open(path string, maxBytes int64) (*Writer, error) {
	if path == "" {
		return nil, fmt.Errorf("tasklog: empty path")
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("tasklog: mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("tasklog: open: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("tasklog: stat: %w", err)
	}
	return &Writer{path: path, maxBytes: maxBytes, f: f, size: info.Size()}, nil
}

// Write appends p to the active log, rotating first if appending p would push a
// non-empty file past MaxBytes. A single write larger than MaxBytes is still
// written in full (output is never truncated mid-record); it simply triggers a
// rotation on the next write. Write satisfies io.Writer.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, fmt.Errorf("tasklog: write after close")
	}
	// Rotate only when the file already holds data — never rotate an empty file
	// just because a single oversized write is about to land (that would create
	// an empty backup and still write the oversized record).
	if w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

// rotate closes the active file, renames it aside to the single `.1` backup
// (overwriting any previous backup), and reopens a fresh, truncated active
// file. The caller must hold w.mu.
func (w *Writer) rotate() error {
	if err := w.f.Close(); err != nil {
		return fmt.Errorf("tasklog: close on rotate: %w", err)
	}
	if err := os.Rename(w.path, w.path+backupSuffix); err != nil {
		// Best-effort reopen so the writer is not left wedged with a nil file.
		if f, reopenErr := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); reopenErr == nil {
			w.f = f
		}
		return fmt.Errorf("tasklog: rename on rotate: %w", err)
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("tasklog: reopen on rotate: %w", err)
	}
	w.f = f
	w.size = 0
	return nil
}

// Close flushes and closes the active file. It is idempotent. Close satisfies
// io.Closer.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	return w.f.Close()
}
