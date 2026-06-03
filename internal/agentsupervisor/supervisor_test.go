package agentsupervisor_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentsupervisor"
)

// TestSupervisor_DrainOffsetArtifacts exercises the drain → events.jsonl +
// offset, the held-open stdin (Inject), and the observability artifacts using a
// `sh` stand-in child (no real claude, no subprocess role dispatch).
func TestSupervisor_DrainOffsetArtifacts(t *testing.T) {
	home := t.TempDir()

	// Stand-in child: emit 3 valid stream-json lines then exit, while reading
	// (and discarding) stdin so it never blocks.
	script := `cat >/dev/null & ` +
		`for i in 1 2 3; do echo '{"type":"system","subtype":"tick"}'; sleep 0.05; done`
	sup, err := agentsupervisor.New(agentsupervisor.Config{
		AgentID:  "agent-unit",
		HomeDir:  home,
		ChildCmd: []string{"sh", "-c", script},
		Logger:   func(string) {},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := sup.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Observability artifacts exist immediately after Start.
	pidBytes, err := os.ReadFile(filepath.Join(home, agentsupervisor.PIDFileName))
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	if pid, _ := strconv.Atoi(strings.TrimSpace(string(pidBytes))); pid != sup.ChildPID() {
		t.Fatalf("claude.pid=%s != child pid %d", strings.TrimSpace(string(pidBytes)), sup.ChildPID())
	}

	instBytes, err := os.ReadFile(filepath.Join(home, agentsupervisor.InstanceFileName))
	if err != nil {
		t.Fatalf("read instance file: %v", err)
	}
	var rec struct {
		InstanceID    string `json:"instance_id"`
		AgentID       string `json:"agent_id"`
		SupervisorPID int    `json:"supervisor_pid"`
		ChildPID      int    `json:"child_pid"`
		StartedAt     string `json:"started_at"`
	}
	if err := json.Unmarshal(instBytes, &rec); err != nil {
		t.Fatalf("unmarshal instance: %v", err)
	}
	if rec.InstanceID != sup.InstanceID() {
		t.Fatalf("instance id mismatch: file=%s sup=%s", rec.InstanceID, sup.InstanceID())
	}
	if rec.AgentID != "agent-unit" || rec.ChildPID != sup.ChildPID() {
		t.Fatalf("instance record mismatch: %+v", rec)
	}
	if _, err := time.Parse(time.RFC3339Nano, rec.StartedAt); err != nil {
		t.Fatalf("started_at not RFC3339Nano: %q (%v)", rec.StartedAt, err)
	}

	// Inject a line into the held-open stdin (must not error before exit).
	_ = sup.Inject("hello")

	// Child exits after 3 lines; wait for the drain join.
	select {
	case <-sup.Done():
	case <-time.After(5 * time.Second):
		t.Fatalf("supervisor did not finish")
	}

	// Offset advanced and equals events.jsonl size; the file holds 3 lines.
	off := sup.Offset()
	if off <= 0 {
		t.Fatalf("offset did not advance: %d", off)
	}
	data, end, err := agentsupervisor.ReadEventsFrom(home, 0)
	if err != nil {
		t.Fatalf("ReadEventsFrom(0): %v", err)
	}
	if end != off {
		t.Fatalf("reader end %d != supervisor offset %d", end, off)
	}
	if n := strings.Count(string(data), "\n"); n != 3 {
		t.Fatalf("expected 3 drained lines, got %d: %q", n, string(data))
	}

	// Read-from-offset: from end → nothing new, offset unchanged.
	rest, end2, err := agentsupervisor.ReadEventsFrom(home, end)
	if err != nil {
		t.Fatalf("ReadEventsFrom(end): %v", err)
	}
	if len(rest) != 0 || end2 != end {
		t.Fatalf("expected no fresh bytes at EOF, got %d bytes end=%d", len(rest), end2)
	}

	// Inject after exit returns the closed sentinel.
	if err := sup.Inject("late"); err != agentsupervisor.ErrSupervisorClosed {
		t.Fatalf("expected ErrSupervisorClosed after exit, got %v", err)
	}
}

// TestSupervisor_Validation covers New's required-field guards.
func TestSupervisor_Validation(t *testing.T) {
	cases := []agentsupervisor.Config{
		{HomeDir: "x", ChildCmd: []string{"sh"}}, // no agent id
		{AgentID: "a", ChildCmd: []string{"sh"}}, // no home
		{AgentID: "a", HomeDir: "x"},             // no child cmd
	}
	for i, c := range cases {
		if _, err := agentsupervisor.New(c); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}
