package agentsupervisor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/workerdaemon"
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
// delegates to the validated claude 2.1.156 parser in workerdaemon so the
// stream schema has ONE home (reused, not reinvented).
func parseStreamLine(line []byte) ([]workerdaemon.StreamEvent, error) {
	return workerdaemon.ParseClaudeStreamLine(line)
}

// encodeUserMessage encodes a plain user message as one newline-terminated
// stream-json user line for claude's --input-format stream-json, mirroring the
// shape used by the long-lived ClaudeSession path (workerdaemon). Isolated here
// so D2-g can correct the schema in one place.
func encodeUserMessage(msg string) ([]byte, error) {
	type textBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type innerMessage struct {
		Role    string      `json:"role"`
		Content []textBlock `json:"content"`
	}
	type userEnvelope struct {
		Type    string       `json:"type"`
		Message innerMessage `json:"message"`
	}
	env := userEnvelope{
		Type: "user",
		Message: innerMessage{
			Role:    "user",
			Content: []textBlock{{Type: "text", Text: msg}},
		},
	}
	b, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("agentsupervisor: encode user message: %w", err)
	}
	return append(b, '\n'), nil
}
