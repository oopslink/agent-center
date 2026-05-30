package agentsupervisor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/oopslink/agent-center/internal/claudestream"
	"github.com/oopslink/agent-center/internal/idgen"
)

// mintInstanceID mints a fresh ULID identifying this supervisor incarnation.
// Reuses the repo's idgen (no new dep). A ULID is monotonic + time-sortable,
// which is handy for ordering incarnations; uniqueness is what matters here.
func mintInstanceID() string { return idgen.MustNewULID() }

// instanceRecord is the supervisor.instance document. Together with claude.pid
// it lets a future daemon prove "same process, never restarted" in a
// PID-REUSE-SAFE way: the daemon remembers (InstanceID, started_at) and on
// reattach re-reads this file; if the recorded ChildPID was reused by an
// unrelated process the InstanceID will differ, so the daemon knows it is NOT
// the same supervisor incarnation.
type instanceRecord struct {
	InstanceID    string `json:"instance_id"`
	AgentID       string `json:"agent_id"`
	SupervisorPID int    `json:"supervisor_pid"`
	ChildPID      int    `json:"child_pid"`
	StartedAt     string `json:"started_at"` // RFC3339Nano
}

// writeArtifacts writes claude.pid (the child pid) and supervisor.instance
// (instance-id + RFC3339Nano start-ts + supervisor/child pids) atomically-ish
// under HomeDir. Best-effort; the caller logs failures (the survival core does
// not depend on these files existing).
func (s *Supervisor) writeArtifacts() error {
	childPID := s.ChildPID()

	pidPath := filepath.Join(s.cfg.HomeDir, PIDFileName)
	if err := writeFileAtomic(pidPath, []byte(fmt.Sprintf("%d\n", childPID)), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", PIDFileName, err)
	}

	rec := instanceRecord{
		InstanceID:    s.instanceID,
		AgentID:       s.cfg.AgentID,
		SupervisorPID: os.Getpid(),
		ChildPID:      childPID,
		StartedAt:     s.startedAt.Format(time.RFC3339Nano),
	}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal instance record: %w", err)
	}
	b = append(b, '\n')
	instPath := filepath.Join(s.cfg.HomeDir, InstanceFileName)
	if err := writeFileAtomic(instPath, b, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", InstanceFileName, err)
	}
	return nil
}

// writeFileAtomic writes via a temp file + rename so a reader never sees a
// half-written artifact.
func writeFileAtomic(path string, b []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// parseStreamLine is the tolerant validation hook for drained stdout lines. It
// delegates to the validated claude 2.1.156 parser in claudestream so the stream
// schema has ONE home (reused, not reinvented). claudestream is the leaf package
// these primitives were extracted into (v2.7 D2-f s3b-1) so agentsupervisor no
// longer imports workerdaemon (breaking the import cycle).
func parseStreamLine(line []byte) ([]claudestream.StreamEvent, error) {
	return claudestream.ParseStreamLine(line)
}

// encodeUserMessage encodes a plain user message as one newline-terminated
// stream-json user line for claude's --input-format stream-json. It delegates to
// claudestream.EncodeUserMessage so the INPUT schema (a documented best guess per
// D2-g) is shared with the long-lived ClaudeSession path in one home.
func encodeUserMessage(msg string) ([]byte, error) {
	return claudestream.EncodeUserMessage(msg)
}
