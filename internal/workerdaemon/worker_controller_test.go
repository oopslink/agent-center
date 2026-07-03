package workerdaemon

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/oopslink/agent-center/internal/workerdaemon/agentcontrol"
	"github.com/oopslink/agent-center/internal/workerdaemon/agentlauncher"
	"github.com/oopslink/agent-center/internal/workerdaemon/workercontroller"
)

// stubLauncher for the controller: Ensure/Stop succeed, no real processes.
type stubLauncher struct {
	mu      sync.Mutex
	running map[string]bool
	stopped []string
}

func newStubLauncher() *stubLauncher { return &stubLauncher{running: map[string]bool{}} }
func (l *stubLauncher) Ensure(spec agentlauncher.AgentSpec) error {
	l.mu.Lock()
	l.running[spec.AgentID] = true
	l.mu.Unlock()
	return nil
}
func (l *stubLauncher) Stop(agentID string) error {
	l.mu.Lock()
	delete(l.running, agentID)
	l.stopped = append(l.stopped, agentID)
	l.mu.Unlock()
	return nil
}
func (l *stubLauncher) Running() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []string{}
	for id := range l.running {
		out = append(out, id)
	}
	return out
}
func (l *stubLauncher) Shutdown(context.Context) error { return nil }

func newTestHandler(t *testing.T) (controllerHandler, *stubLauncher) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "acwh")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	l := newStubLauncher()
	ctrl, err := workercontroller.New(workercontroller.Config{Launcher: l, SockDir: dir})
	if err != nil {
		t.Fatalf("New controller: %v", err)
	}
	return controllerHandler{ctrl: ctrl, log: func(string) {}}, l
}

func TestControllerHandler_UndeliveredCommandErrors(t *testing.T) {
	h, l := newTestHandler(t)
	// A work command for an agent whose process is not actually listening (fake
	// launcher spawns nothing) → the real agentcontrol client dials a dead socket →
	// Deliver errors → Handle returns error → the control loop leaves it un-acked
	// (no cursor advance) and retries. This is the no-lost-command guarantee.
	err := h.Handle(context.Background(), ControlCommand{
		CommandType: cmdTypeAgentWork,
		Offset:      5,
		Payload:     `{"agent_id":"a","task_id":"t1"}`,
	})
	if err == nil {
		t.Error("an undelivered command MUST return an error (cursor not advanced)")
	}
	// It still ensured the agent was launched (so the retry lands once it is up).
	if !l.running["a"] {
		t.Error("Deliver must ensure the target agent is launched")
	}
}

func TestControllerHandler_NoAgentIDIsSkipped(t *testing.T) {
	h, _ := newTestHandler(t)
	// No agent_id → unroutable → ack (nil) so a malformed command can't wedge the cursor.
	if err := h.Handle(context.Background(), ControlCommand{CommandType: cmdTypeAgentWork, Payload: `{}`}); err != nil {
		t.Errorf("a command with no agent_id should be skipped (nil), got %v", err)
	}
}

func TestControllerHandler_ReconcileStoppedTearsDown(t *testing.T) {
	h, l := newTestHandler(t)
	_ = l.Ensure(agentlauncher.AgentSpec{AgentID: "a"})
	err := h.Handle(context.Background(), ControlCommand{
		CommandType: cmdTypeAgentReconcile,
		Payload:     `{"agent_id":"a","desired_lifecycle":"stopped"}`,
	})
	if err != nil {
		t.Fatalf("reconcile-stopped: %v", err)
	}
	if len(l.stopped) != 1 || l.stopped[0] != "a" {
		t.Errorf("a desired-stopped reconcile must stop the agent process, stopped=%v", l.stopped)
	}
}

func TestWorkerSockDir_ShortAndCreated(t *testing.T) {
	dir, err := workerSockDir("worker-abc")
	if err != nil {
		t.Fatalf("workerSockDir: %v", err)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Errorf("sock dir not created: %v", err)
	}
	// The real constraint is the FULL socket path (dir + "/" + hashed name) fitting the
	// OS sun_path limit (~104 darwin / 108 linux).
	full := len(dir) + 1 + len(agentcontrol.SocketName("some-agent-id"))
	if full >= 104 {
		t.Errorf("full socket path too long (%d ≥ 104): dir=%q", full, dir)
	}
}
