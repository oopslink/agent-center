// Package shim implements the per-execution shim (ADR-0018) responsible
// for fork+exec of the agent CLI and serving as the daemon's RPC proxy.
package shim

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FileNames within a per-execution directory (ADR-0018 § 3 +
// 02-task-execution § 9.1).
const (
	FileEnvelope = "envelope.json"
	FileStatus   = "status.json"
	FileEvents   = "events.jsonl"
	FileAgentLog = "agent.log"
	FileStderr   = "stderr.log"
	FileSock     = "shim.sock"
	FilePID      = "shim.pid"
)

// Phase is the shim status.json.phase enum.
type Phase string

const (
	PhaseStarting Phase = "starting"
	PhaseRunning  Phase = "running"
	PhaseDone     Phase = "done"
)

// Status is the JSON written to status.json (ADR-0018 § 3).
type Status struct {
	ExecutionID    string    `json:"execution_id"`
	Phase          Phase     `json:"phase"`
	ShimPID        int       `json:"shim_pid"`
	ShimStartTime  time.Time `json:"shim_start_time"`
	AgentPID       int       `json:"agent_pid"`
	AgentStartTime time.Time `json:"agent_start_time"`
	ExitCode       int       `json:"exit_code,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// PIDFile is the JSON written to shim.pid (subset of Status).
type PIDFile struct {
	PID       int       `json:"pid"`
	StartTime time.Time `json:"start_time"`
}

// Dir is a per-execution directory manager (atomic writes via temp+rename).
type Dir struct {
	Root        string // base dir (e.g. ~/.agent-center-worker/exec)
	ExecutionID string
}

// NewDir returns a Dir handle and ensures the directory exists.
func NewDir(root, executionID string) (*Dir, error) {
	if root == "" || executionID == "" {
		return nil, errors.New("shim/dir: root and execution_id required")
	}
	d := &Dir{Root: root, ExecutionID: executionID}
	if err := os.MkdirAll(d.Path(), 0o755); err != nil {
		return nil, fmt.Errorf("shim/dir: mkdir: %w", err)
	}
	return d, nil
}

// Path returns the per-execution directory path.
func (d *Dir) Path() string {
	return filepath.Join(d.Root, d.ExecutionID)
}

// File returns the absolute path of name within the per-execution dir.
func (d *Dir) File(name string) string {
	return filepath.Join(d.Path(), name)
}

// WriteEnvelope writes envelope.json atomically (temp+rename).
func (d *Dir) WriteEnvelope(envelopeJSON []byte) error {
	return writeAtomic(d.File(FileEnvelope), envelopeJSON, 0o644)
}

// ReadEnvelope returns the envelope.json bytes.
func (d *Dir) ReadEnvelope() ([]byte, error) {
	return os.ReadFile(d.File(FileEnvelope))
}

// WriteStatus writes status.json atomically.
func (d *Dir) WriteStatus(s Status) error {
	s.UpdatedAt = time.Now().UTC()
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return writeAtomic(d.File(FileStatus), b, 0o644)
}

// ReadStatus returns the current status.json.
func (d *Dir) ReadStatus() (Status, error) {
	b, err := os.ReadFile(d.File(FileStatus))
	if err != nil {
		return Status{}, err
	}
	var s Status
	if err := json.Unmarshal(b, &s); err != nil {
		return Status{}, err
	}
	return s, nil
}

// WritePID writes shim.pid (used for fencing across daemon restarts).
func (d *Dir) WritePID(p PIDFile) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return writeAtomic(d.File(FilePID), b, 0o644)
}

// ReadPID returns the shim PID file contents.
func (d *Dir) ReadPID() (PIDFile, error) {
	b, err := os.ReadFile(d.File(FilePID))
	if err != nil {
		return PIDFile{}, err
	}
	var p PIDFile
	if err := json.Unmarshal(b, &p); err != nil {
		return PIDFile{}, err
	}
	return p, nil
}

// AppendEvent appends a JSONL line to events.jsonl with O_APPEND. seq is
// caller-managed (monotonic).
func (d *Dir) AppendEvent(line []byte) error {
	f, err := os.OpenFile(d.File(FileEvents), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if len(line) == 0 || line[len(line)-1] != '\n' {
		line = append(line, '\n')
	}
	_, err = f.Write(line)
	return err
}

// CountEvents returns the line count of events.jsonl (use sparingly; full
// scan).
func (d *Dir) CountEvents() (int, error) {
	b, err := os.ReadFile(d.File(FileEvents))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	for _, c := range b {
		if c == '\n' {
			count++
		}
	}
	return count, nil
}

// Exists reports whether the per-execution directory exists on disk.
func (d *Dir) Exists() bool {
	info, err := os.Stat(d.Path())
	if err != nil {
		return false
	}
	return info.IsDir()
}

// Remove recursively deletes the per-execution directory (GC).
func (d *Dir) Remove() error {
	return os.RemoveAll(d.Path())
}

func writeAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
