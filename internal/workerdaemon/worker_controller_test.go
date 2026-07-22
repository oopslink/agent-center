package workerdaemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/runtimefs"
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
func (l *stubLauncher) Shutdown(context.Context) error           { return nil }
func (l *stubLauncher) Adopt(agentlauncher.AgentSpec, int) error { return nil }
func (l *stubLauncher) AdoptablePIDs() map[string]int            { return map[string]int{} }

func newTestHandler(t *testing.T) (controllerHandler, *stubLauncher) {
	h, l, _ := newTestHandlerWithReporter(t, nil)
	return h, l
}

func TestAgentRuntimeSpec_IsPerAgent(t *testing.T) {
	off := agentRuntimeSpec("off", false)
	on := agentRuntimeSpec("on", true)
	if len(off.Env) != 0 {
		t.Fatalf("off env = %v, want empty", off.Env)
	}
	if len(on.Env) != 1 || on.Env[0] != "AC_EXECUTOR_GIT_WORKTREE=1" {
		t.Fatalf("on env = %v", on.Env)
	}
	base := withoutEnv([]string{"PATH=/bin", "AC_EXECUTOR_GIT_WORKTREE=1"}, "AC_EXECUTOR_GIT_WORKTREE")
	if len(base) != 1 || base[0] != "PATH=/bin" {
		t.Fatalf("filtered base env = %v", base)
	}
}

func newTestHandlerWithReporter(t *testing.T, r lifecycleReporter) (controllerHandler, *stubLauncher, lifecycleReporter) {
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
	return controllerHandler{ctrl: ctrl, reporter: r, log: func(string) {}}, l, r
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

func TestControllerHandler_ReconcileStoppingTearsDownAndReportsStopped(t *testing.T) {
	r := &fakeLifecycleReporter{}
	h, l, _ := newTestHandlerWithReporter(t, r)
	_ = l.Ensure(agentlauncher.AgentSpec{AgentID: "a"})
	err := h.Handle(context.Background(), ControlCommand{
		CommandType: cmdTypeAgentReconcile,
		Payload:     `{"agent_id":"a","desired_lifecycle":"stopping"}`,
	})
	if err != nil {
		t.Fatalf("reconcile-stopping: %v", err)
	}
	if len(l.stopped) != 1 || l.stopped[0] != "a" {
		t.Errorf("a desired-stopping reconcile must stop the agent process, stopped=%v", l.stopped)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.got) != 1 || r.got[0].agentID != "a" || r.got[0].state != "stopped" {
		t.Fatalf("feedback = %+v, want one stopped feedback for a", r.got)
	}
}

func TestControllerHandler_ReconcileResettingTearsDownAndReportsStopped(t *testing.T) {
	r := &fakeLifecycleReporter{}
	h, l, _ := newTestHandlerWithReporter(t, r)
	_ = l.Ensure(agentlauncher.AgentSpec{AgentID: "a"})
	err := h.Handle(context.Background(), ControlCommand{
		CommandType: cmdTypeAgentReconcile,
		Payload:     `{"agent_id":"a","desired_lifecycle":"resetting"}`,
	})
	if err != nil {
		t.Fatalf("reconcile-resetting: %v", err)
	}
	if len(l.stopped) != 1 || l.stopped[0] != "a" {
		t.Errorf("a desired-resetting reconcile must stop the agent process, stopped=%v", l.stopped)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.got) != 1 || r.got[0].agentID != "a" || r.got[0].state != "stopped" {
		t.Fatalf("feedback = %+v, want one stopped feedback for a", r.got)
	}
}

func TestControllerHandler_ReconcileStopIgnoresAlreadySettledFeedback(t *testing.T) {
	r := &fakeLifecycleReporter{fail: &AdminError{
		Status: http.StatusConflict,
		Body:   `{"error":"illegal_transition","message":"agent: illegal lifecycle transition"}`,
	}}
	h, l, _ := newTestHandlerWithReporter(t, r)
	_ = l.Ensure(agentlauncher.AgentSpec{AgentID: "a"})
	err := h.Handle(context.Background(), ControlCommand{
		CommandType: cmdTypeAgentReconcile,
		Payload:     `{"agent_id":"a","desired_lifecycle":"stopping"}`,
	})
	if err != nil {
		t.Fatalf("already-settled stopped feedback should be acked, got %v", err)
	}
	if len(l.stopped) != 1 || l.stopped[0] != "a" {
		t.Errorf("stop should still be attempted, stopped=%v", l.stopped)
	}
}

func TestControllerHandler_ReconcileStopReturnsUnexpectedFeedbackError(t *testing.T) {
	r := &fakeLifecycleReporter{fail: errors.New("network down")}
	h, _, _ := newTestHandlerWithReporter(t, r)
	err := h.Handle(context.Background(), ControlCommand{
		CommandType: cmdTypeAgentReconcile,
		Payload:     `{"agent_id":"a","desired_lifecycle":"stopping"}`,
	})
	if err == nil {
		t.Fatal("unexpected lifecycle feedback error must retry")
	}
}

// fakeLifecycleReporter records lifecycle feedback (and can be set to fail).
type fakeLifecycleReporter struct {
	mu   sync.Mutex
	got  []lifecycleFeedback
	fail error
}

type lifecycleFeedback struct {
	agentID string
	state   string
	errMsg  string
	at      time.Time
}

func (r *fakeLifecycleReporter) ReportAgentLifecycle(_ context.Context, agentID, state, errMsg string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return r.fail
	}
	r.got = append(r.got, lifecycleFeedback{agentID: agentID, state: state, errMsg: errMsg, at: at})
	return nil
}

// fakePoster records posted runtime_fs responses (and can be set to fail).
type fakePoster struct {
	mu   sync.Mutex
	got  []runtimefs.Response
	fail error
}

func (p *fakePoster) ReportRuntimeFsResponse(_ context.Context, resp runtimefs.Response) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fail != nil {
		return p.fail
	}
	p.got = append(p.got, resp)
	return nil
}

// TestControllerHandler_RuntimeFsServedLocally pins T860 gap2: an agent.runtime_fs
// command is handled by the worker-controller directly (reads the agent home + posts the
// correlated response), NOT proxied to the agent process.
func TestControllerHandler_RuntimeFsServedLocally(t *testing.T) {
	homeBase := t.TempDir()
	// Lay out an agent home: <homeBase>/agents/<id>/ with a file to list.
	home := filepath.Join(homeBase, "agents", "agent-1")
	if err := os.MkdirAll(filepath.Join(home, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "memory", "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &fakePoster{}
	h := controllerHandler{homeBase: homeBase, poster: p, log: func(string) {}}

	err := h.Handle(context.Background(), ControlCommand{
		CommandType: cmdTypeRuntimeFs,
		Payload:     `{"req_id":"r1","agent_id":"agent-1","op":"list","path":"memory"}`,
	})
	if err != nil {
		t.Fatalf("Handle runtime_fs = %v, want nil (served + posted)", err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.got) != 1 {
		t.Fatalf("posted %d responses, want 1", len(p.got))
	}
	resp := p.got[0]
	if resp.ReqID != "r1" || resp.AgentID != "agent-1" {
		t.Errorf("response correlation wrong: %+v", resp)
	}
	if resp.Code != "" {
		t.Fatalf("unexpected error code %q (%s)", resp.Code, resp.Message)
	}
	var list runtimefs.ListResult
	if err := json.Unmarshal(resp.Result, &list); err != nil {
		t.Fatalf("result not a ListResult: %v", err)
	}
	if len(list.Entries) != 1 || list.Entries[0].Name != "notes.txt" {
		t.Errorf("listing = %+v, want the single notes.txt entry", list.Entries)
	}
}

// TestControllerHandler_RuntimeFsUnknownOpReportsError pins that a bad op is reported as
// an error Response (still acked) rather than proxied or wedging the cursor.
func TestControllerHandler_RuntimeFsUnknownOpReportsError(t *testing.T) {
	p := &fakePoster{}
	h := controllerHandler{homeBase: t.TempDir(), poster: p, log: func(string) {}}
	if err := h.Handle(context.Background(), ControlCommand{
		CommandType: cmdTypeRuntimeFs,
		Payload:     `{"req_id":"r2","agent_id":"a","op":"bogus"}`,
	}); err != nil {
		t.Fatalf("Handle = %v, want nil", err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.got) != 1 || p.got[0].Code == "" {
		t.Errorf("unknown op must post an error response, got %+v", p.got)
	}
}

// TestControllerHandler_RuntimeFsPostFailureRetries pins that a failed response POST
// surfaces as an error so the control loop keeps the command un-acked and retries.
func TestControllerHandler_RuntimeFsPostFailureRetries(t *testing.T) {
	p := &fakePoster{fail: context.DeadlineExceeded}
	h := controllerHandler{homeBase: t.TempDir(), poster: p, log: func(string) {}}
	if err := h.Handle(context.Background(), ControlCommand{
		CommandType: cmdTypeRuntimeFs,
		Payload:     `{"req_id":"r3","agent_id":"a","op":"list","path":""}`,
	}); err == nil {
		t.Error("a failed response POST must return an error (retry, don't drop)")
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
