package workercontroller

import (
	"context"
	"errors"
	"os"
	"sort"
	"sync"
	"testing"

	"github.com/oopslink/agent-center/internal/workerdaemon/agentcontrol"
	"github.com/oopslink/agent-center/internal/workerdaemon/agentlauncher"
)

// fakeLauncher records Ensure/Stop/Adopt and reports a running set.
type fakeLauncher struct {
	mu        sync.Mutex
	running   map[string]bool
	ensureN   map[string]int
	adoptN    map[string]int
	ensERR    map[string]error
	adoptPIDs map[string]int // returned by AdoptablePIDs
	adoptERR  error
	lastSpec  map[string]agentlauncher.AgentSpec
}

func newFakeLauncher() *fakeLauncher {
	return &fakeLauncher{
		running:   map[string]bool{},
		ensureN:   map[string]int{},
		adoptN:    map[string]int{},
		ensERR:    map[string]error{},
		adoptPIDs: map[string]int{},
		lastSpec:  map[string]agentlauncher.AgentSpec{},
	}
}

func (l *fakeLauncher) Ensure(spec agentlauncher.AgentSpec) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ensureN[spec.AgentID]++
	l.lastSpec[spec.AgentID] = spec
	if err := l.ensERR[spec.AgentID]; err != nil {
		return err
	}
	l.running[spec.AgentID] = true
	return nil
}

func TestReconcileSpecs_PreservesPerAgentEnv(t *testing.T) {
	l := newFakeLauncher()
	c, err := New(Config{Launcher: l, SockDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	c.ReconcileSpecs([]agentlauncher.AgentSpec{
		{AgentID: "off"},
		{AgentID: "on", Env: []string{"AC_EXECUTOR_GIT_WORKTREE=1"}},
	})
	if len(l.lastSpec["off"].Env) != 0 {
		t.Fatalf("off env = %v, want empty", l.lastSpec["off"].Env)
	}
	if got := l.lastSpec["on"].Env; len(got) != 1 || got[0] != "AC_EXECUTOR_GIT_WORKTREE=1" {
		t.Fatalf("on env = %v", got)
	}
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
func (l *fakeLauncher) Adopt(spec agentlauncher.AgentSpec, _ int) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.adoptN[spec.AgentID]++
	if l.adoptERR != nil {
		return l.adoptERR
	}
	l.running[spec.AgentID] = true
	return nil
}
func (l *fakeLauncher) AdoptablePIDs() map[string]int {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]int{}
	for k, v := range l.adoptPIDs {
		out[k] = v
	}
	return out
}

// fakeClient records delivered commands and can be set to fail. probeID/probeErr drive
// the T860 gap5 adoption identity probe.
type fakeClient struct {
	mu       sync.Mutex
	sock     string
	got      []agentcontrol.Command
	err      error
	probeID  string
	probeErr error
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
func (c *fakeClient) Probe(context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.probeID, c.probeErr
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

// newAdoptController builds a controller whose control clients all Probe with the given
// id/err — enough for the single-agent T860 gap5 adoption tests.
func newAdoptController(t *testing.T, l agentlauncher.AgentLauncher, probeID string, probeErr error) *Controller {
	t.Helper()
	c, err := New(Config{
		Launcher:  l,
		SockDir:   "/tmp/acs",
		NewClient: func(string) controlClient { return &fakeClient{probeID: probeID, probeErr: probeErr} },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// TestReconcileWithAdoption_AdoptsLiveSurvivor: a recorded pid that is alive (this test
// process's own pid) AND whose socket probe echoes the agent id is ADOPTED, not respawned.
func TestReconcileWithAdoption_AdoptsLiveSurvivor(t *testing.T) {
	l := newFakeLauncher()
	l.adoptPIDs["a"] = os.Getpid() // a definitely-alive pid
	c := newAdoptController(t, l, "a", nil)

	c.ReconcileWithAdoption(context.Background(), []string{"a"})

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.adoptN["a"] != 1 {
		t.Errorf("Adopt(a) called %d times, want 1", l.adoptN["a"])
	}
	if l.ensureN["a"] != 0 {
		t.Errorf("Ensure(a) called %d times, want 0 (survivor must not be respawned)", l.ensureN["a"])
	}
}

// TestReconcileWithAdoption_RespawnsOnProbeMismatch: pid alive but the socket serves a
// DIFFERENT agent (recycled pid / stale socket) → respawn, not adopt.
func TestReconcileWithAdoption_RespawnsOnProbeMismatch(t *testing.T) {
	l := newFakeLauncher()
	l.adoptPIDs["a"] = os.Getpid()
	c := newAdoptController(t, l, "someone-else", nil) // probe returns wrong id

	c.ReconcileWithAdoption(context.Background(), []string{"a"})

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.adoptN["a"] != 0 {
		t.Errorf("Adopt(a) called %d times, want 0 (identity mismatch)", l.adoptN["a"])
	}
	if l.ensureN["a"] != 1 {
		t.Errorf("Ensure(a) called %d times, want 1 (respawn on mismatch)", l.ensureN["a"])
	}
}

// TestReconcileWithAdoption_RespawnsOnDeadPid: a recorded pid that no longer exists fails
// the fast pre-filter → respawn (no probe needed).
func TestReconcileWithAdoption_RespawnsOnDeadPid(t *testing.T) {
	l := newFakeLauncher()
	l.adoptPIDs["a"] = 0x7fffffff // not a live pid
	c := newAdoptController(t, l, "a", nil)

	c.ReconcileWithAdoption(context.Background(), []string{"a"})

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.adoptN["a"] != 0 {
		t.Errorf("Adopt(a) called %d times, want 0 (dead pid)", l.adoptN["a"])
	}
	if l.ensureN["a"] != 1 {
		t.Errorf("Ensure(a) called %d times, want 1 (respawn on dead pid)", l.ensureN["a"])
	}
}

// TestReconcileWithAdoption_NoRecordSpawns: an agent with no recorded pid is a plain
// spawn (the common first-boot case).
func TestReconcileWithAdoption_NoRecordSpawns(t *testing.T) {
	l := newFakeLauncher()
	c := newAdoptController(t, l, "", errors.New("no server"))

	c.ReconcileWithAdoption(context.Background(), []string{"a"})

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.ensureN["a"] != 1 || l.adoptN["a"] != 0 {
		t.Errorf("no-record agent: ensure=%d adopt=%d, want ensure=1 adopt=0", l.ensureN["a"], l.adoptN["a"])
	}
}

// TestReconcileWithAdoption_StopsUndesired: an agent no longer desired is stopped (same
// as the plain Reconcile).
func TestReconcileWithAdoption_StopsUndesired(t *testing.T) {
	l := newFakeLauncher()
	l.running["old"] = true
	c := newAdoptController(t, l, "", errors.New("no server"))

	c.ReconcileWithAdoption(context.Background(), []string{"a"})

	if got := l.Running(); len(got) != 1 || got[0] != "a" {
		t.Errorf("running=%v, want [a] (old stopped, a spawned)", got)
	}
}
