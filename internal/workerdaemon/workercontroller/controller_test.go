package workercontroller

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"

	"github.com/oopslink/agent-center/internal/workerdaemon/agentcontrol"
	"github.com/oopslink/agent-center/internal/workerdaemon/agentlauncher"
)

// fakeLauncher records Ensure/Stop and reports a running set.
type fakeLauncher struct {
	mu      sync.Mutex
	running map[string]bool
	ensureN map[string]int
	ensERR  map[string]error
}

func newFakeLauncher() *fakeLauncher {
	return &fakeLauncher{running: map[string]bool{}, ensureN: map[string]int{}, ensERR: map[string]error{}}
}

func (l *fakeLauncher) Ensure(spec agentlauncher.AgentSpec) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ensureN[spec.AgentID]++
	if err := l.ensERR[spec.AgentID]; err != nil {
		return err
	}
	l.running[spec.AgentID] = true
	return nil
}
func (l *fakeLauncher) Stop(agentID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.running, agentID)
	return nil
}
func (l *fakeLauncher) Running() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []string{}
	for id := range l.running {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
func (l *fakeLauncher) Shutdown(context.Context) error { return nil }

// fakeClient records delivered commands and can be set to fail.
type fakeClient struct {
	mu   sync.Mutex
	sock string
	got  []agentcontrol.Command
	err  error
}

func (c *fakeClient) Deliver(_ context.Context, cmd agentcontrol.Command) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	c.got = append(c.got, cmd)
	return nil
}

func newTestController(t *testing.T, l agentlauncher.AgentLauncher) *Controller {
	t.Helper()
	c, err := New(Config{
		Launcher:  l,
		SockDir:   "/tmp/acs",
		NewClient: func(string) controlClient { return &fakeClient{} },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestReconcile_EnsuresDesiredStopsRemoved(t *testing.T) {
	l := newFakeLauncher()
	c := newTestController(t, l)

	c.Reconcile([]string{"a", "b"})
	if got := c.Running(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("Running = %v, want [a b]", got)
	}
	// Drop "a", add "c".
	c.Reconcile([]string{"b", "c"})
	got := c.Running()
	if len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Errorf("Running = %v, want [b c] (a stopped, c ensured)", got)
	}
	// Ensure is idempotent per reconcile — "b" ensured on both rounds is fine.
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.ensureN["a"] != 1 || l.ensureN["c"] != 1 {
		t.Errorf("ensure counts a=%d c=%d, want 1 each", l.ensureN["a"], l.ensureN["c"])
	}
}

func TestDeliver_SuccessAndEnsuresAgent(t *testing.T) {
	l := newFakeLauncher()
	fc := &fakeClient{}
	c, err := New(Config{
		Launcher:  l,
		SockDir:   "/tmp/acs",
		NewClient: func(string) controlClient { return fc },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Deliver to an agent not yet reconciled → it is ensured (launched) then delivered.
	if err := c.Deliver(context.Background(), agentcontrol.Command{Type: "work_available", AgentID: "a", Seq: 3}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if l.ensureN["a"] == 0 {
		t.Error("Deliver must ensure the target agent is launched")
	}
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if len(fc.got) != 1 || fc.got[0].Seq != 3 {
		t.Errorf("delivered = %+v, want the command", fc.got)
	}
}

func TestDeliver_UndeliveredReturnsErrorForRetry(t *testing.T) {
	l := newFakeLauncher()
	fc := &fakeClient{err: errors.New("agent down")}
	c, _ := New(Config{Launcher: l, SockDir: "/tmp/acs", NewClient: func(string) controlClient { return fc }})

	// Agent process is down → Deliver errors → the control loop keeps the command
	// un-acked (no cursor advance) and retries. This is the no-lost-command guarantee.
	if err := c.Deliver(context.Background(), agentcontrol.Command{Type: "work_available", AgentID: "a"}); err == nil {
		t.Error("an undelivered command MUST return an error so the cursor is not advanced")
	}
}

func TestDeliver_LaunchFailureIsError(t *testing.T) {
	l := newFakeLauncher()
	l.ensERR["a"] = errors.New("spawn failed")
	c, _ := New(Config{Launcher: l, SockDir: "/tmp/acs", NewClient: func(string) controlClient { return &fakeClient{} }})
	if err := c.Deliver(context.Background(), agentcontrol.Command{Type: "work", AgentID: "a"}); err == nil {
		t.Error("a launch failure must surface as a delivery error (retry, don't drop)")
	}
}

func TestDeliver_MissingAgentID(t *testing.T) {
	c, _ := New(Config{Launcher: newFakeLauncher(), SockDir: "/tmp/acs", NewClient: func(string) controlClient { return &fakeClient{} }})
	if err := c.Deliver(context.Background(), agentcontrol.Command{Type: "work"}); err == nil {
		t.Error("a command with no agent_id must error")
	}
}

func TestNew_Validation(t *testing.T) {
	if _, err := New(Config{SockDir: "/tmp/acs"}); err == nil {
		t.Error("nil launcher must error")
	}
	if _, err := New(Config{Launcher: newFakeLauncher()}); err == nil {
		t.Error("empty sock_dir must error")
	}
}
