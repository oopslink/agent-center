package taskexec

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const abortedSuffix = "__aborted_"
const gcDeletingSuffix = "__gc_deleting"

// AbortedDirName builds the abort-archived directory name (design §11.2).
func AbortedDirName(taskID string, ts time.Time) string {
	return taskID + abortedSuffix + ts.UTC().Format("20060102T150405Z")
}

// GCDeletingDirName builds the GC temporary directory name (design §11.3).
func GCDeletingDirName(abortedName string) string {
	return abortedName + gcDeletingSuffix
}

// ParseAbortedDir extracts the task ID, timestamp, and gc_deleting flag
// from an aborted directory name. Returns ok=false if not an aborted dir.
func ParseAbortedDir(name string) (taskID string, ts time.Time, gcDeleting bool, ok bool) {
	if name == "" {
		return "", time.Time{}, false, false
	}
	gcDeleting = strings.HasSuffix(name, gcDeletingSuffix)
	clean := strings.TrimSuffix(name, gcDeletingSuffix)

	idx := strings.Index(clean, abortedSuffix)
	if idx < 0 {
		return "", time.Time{}, false, false
	}
	taskID = clean[:idx]
	tsStr := clean[idx+len(abortedSuffix):]
	ts, err := time.Parse("20060102T150405Z", tsStr)
	if err != nil {
		// valid structure, unparsable timestamp — still return ok=true
		return taskID, time.Time{}, gcDeleting, true
	}
	return taskID, ts, gcDeleting, true
}

// AbortTask stops a task execution and atomically renames its directory
// to the aborted archive format (design §11.2).
//
// 1. Write abort lifecycle event to events.current.jsonl
// 2. Atomic rename: tasks/{task_id}/ → tasks/{task_id}__aborted_{ts}/
//
// Returns the aborted directory name.
func AbortTask(tasksDir, taskID string, ts time.Time) (string, error) {
	srcDir := filepath.Join(tasksDir, taskID)
	if _, err := os.Stat(srcDir); err != nil {
		return "", fmt.Errorf("taskexec: abort %s: source dir: %w", taskID, err)
	}

	// 1. Write abort lifecycle event
	abortEvent := RawEvent{
		ID:         fmt.Sprintf("abort-%s-%d", taskID, ts.UnixNano()),
		EventType:  "lifecycle",
		Payload:    mustMarshal(map[string]string{"event": "aborted"}),
		OccurredAt: ts.UTC(),
	}
	w := NewEventStreamWriter()
	if err := w.Append(srcDir, abortEvent); err != nil {
		return "", fmt.Errorf("taskexec: abort %s: write event: %w", taskID, err)
	}

	// 2. Atomic rename
	abortedName := AbortedDirName(taskID, ts)
	dstDir := filepath.Join(tasksDir, abortedName)
	if err := os.Rename(srcDir, dstDir); err != nil {
		return "", fmt.Errorf("taskexec: abort %s: rename: %w", taskID, err)
	}
	return abortedName, nil
}

func mustMarshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
