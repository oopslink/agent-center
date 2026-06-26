package taskexec

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	taskMetaFile    = "task.json"
	execContextFile = "execution.json"
)

// TaskDirEntry holds a task directory's parsed contents.
type TaskDirEntry struct {
	Meta    TaskExecutionMeta
	ExecCtx ExecutionContext
}

// validatePathComponent rejects any value that could escape the parent directory.
// Mirrors cognition/memory.validatePathComponent.
func validatePathComponent(name, v string) error {
	if v == "" {
		return fmt.Errorf("taskexec: %s required", name)
	}
	if strings.ContainsAny(v, "/\\:") {
		return fmt.Errorf("taskexec: %s %q contains path separator", name, v)
	}
	if strings.Contains(v, "\x00") {
		return fmt.Errorf("taskexec: %s %q contains null byte", name, v)
	}
	if v == "." || v == ".." || strings.Contains(v, "..") {
		return fmt.Errorf("taskexec: %s %q contains path traversal", name, v)
	}
	return nil
}

// DirManager handles per-task execution directory lifecycle.
type DirManager struct{}

// NewDirManager returns a new DirManager.
func NewDirManager() *DirManager { return &DirManager{} }

// Create initializes tasks/{task_id}/ with task.json + execution.json.
func (d *DirManager) Create(tasksDir string, meta TaskExecutionMeta, execCtx ExecutionContext) error {
	if err := meta.Validate(); err != nil {
		return err
	}
	if err := validatePathComponent("task_id", meta.TaskID); err != nil {
		return err
	}
	dir := filepath.Join(tasksDir, meta.TaskID)
	if err := mkdirAll(dir); err != nil {
		return fmt.Errorf("taskexec: mkdir %q: %w", dir, err)
	}
	if err := writeJSONAtomic(filepath.Join(dir, taskMetaFile), meta); err != nil {
		return fmt.Errorf("taskexec: write task.json: %w", err)
	}
	if err := writeJSONAtomic(filepath.Join(dir, execContextFile), execCtx); err != nil {
		return fmt.Errorf("taskexec: write execution.json: %w", err)
	}
	return nil
}

// Read loads the task.json + execution.json from tasks/{task_id}/.
func (d *DirManager) Read(tasksDir, taskID string) (TaskDirEntry, error) {
	if err := validatePathComponent("task_id", taskID); err != nil {
		return TaskDirEntry{}, err
	}
	dir := filepath.Join(tasksDir, taskID)
	var entry TaskDirEntry
	if err := readJSON(filepath.Join(dir, taskMetaFile), &entry.Meta); err != nil {
		return TaskDirEntry{}, fmt.Errorf("taskexec: read task.json for %s: %w", taskID, err)
	}
	// execution.json is optional (may not exist for very old dirs).
	// Only ignore ErrNotExist; surface corruption errors.
	if execErr := readJSON(filepath.Join(dir, execContextFile), &entry.ExecCtx); execErr != nil {
		if !errors.Is(execErr, os.ErrNotExist) {
			return TaskDirEntry{}, fmt.Errorf("taskexec: read execution.json for %s: %w", taskID, execErr)
		}
	}
	return entry, nil
}

// UpdateStatus updates the status field in task.json.
func (d *DirManager) UpdateStatus(tasksDir, taskID string, status TaskExecutionStatus) error {
	if err := validatePathComponent("task_id", taskID); err != nil {
		return err
	}
	dir := filepath.Join(tasksDir, taskID)
	var meta TaskExecutionMeta
	if err := readJSON(filepath.Join(dir, taskMetaFile), &meta); err != nil {
		return fmt.Errorf("taskexec: read for update %s: %w", taskID, err)
	}
	meta.Status = status
	meta.UpdatedAt = time.Now().UTC()
	return writeJSONAtomic(filepath.Join(dir, taskMetaFile), meta)
}

// ScanActive scans tasksDir for standard task directories (design §3.2).
// Returns metadata for each, skipping aborted/gc_deleting dirs.
func (d *DirManager) ScanActive(tasksDir string) []TaskExecutionMeta {
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return nil
	}
	var result []TaskExecutionMeta
	for _, e := range entries {
		if !e.IsDir() || !IsStandardTaskDir(e.Name()) {
			continue
		}
		var meta TaskExecutionMeta
		if err := readJSON(filepath.Join(tasksDir, e.Name(), taskMetaFile), &meta); err != nil {
			continue // skip unreadable dirs
		}
		result = append(result, meta)
	}
	return result
}

// IsStandardTaskDir identifies a standard execution directory name (design §3.2).
// Standard: pure task_id (no suffix). Non-standard: __aborted_ or __gc_deleting.
func IsStandardTaskDir(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") {
		return false
	}
	if strings.Contains(name, "__aborted_") {
		return false
	}
	if strings.Contains(name, "__gc_deleting") {
		return false
	}
	return true
}

func writeJSONAtomic(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func mkdirAll(path string) error {
	return os.MkdirAll(path, 0o700)
}
