package workerdaemon

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
	"github.com/oopslink/agent-center/internal/workerdaemon/modelrouter"
	"github.com/oopslink/agent-center/internal/workerdaemon/orchestrator"
)

func lookTrue(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("true")
	if err != nil {
		t.Skipf("`true` not available: %v", err)
	}
	return p
}

func TestConcurrencyEnabled(t *testing.T) {
	cases := []struct {
		name string
		pl   reconcilePayload
		want bool
	}{
		{"both set", reconcilePayload{MaxConcurrentTasks: 2, AllowedModels: []string{"m"}}, true},
		{"no max", reconcilePayload{AllowedModels: []string{"m"}}, false},
		{"no models", reconcilePayload{MaxConcurrentTasks: 2}, false},
		{"neither", reconcilePayload{}, false},
	}
	for _, tc := range cases {
		if got := concurrencyEnabled(tc.pl); got != tc.want {
			t.Errorf("%s: concurrencyEnabled = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFirstNonEmptyLine(t *testing.T) {
	if got := firstNonEmptyLine("\n  \n first line\nsecond"); got != "first line" {
		t.Errorf("got %q, want 'first line'", got)
	}
	if got := firstNonEmptyLine("   \n  "); got != "" {
		t.Errorf("blank got %q, want ''", got)
	}
	long := strings.Repeat("x", 200)
	if got := firstNonEmptyLine(long); len(got) != 120 {
		t.Errorf("long line len = %d, want capped 120", len(got))
	}
}

func TestFuncClock_Now(t *testing.T) {
	fixed := time.Unix(1700000000, 0)
	if got := (funcClock{now: func() time.Time { return fixed }}).Now(); !got.Equal(fixed) {
		t.Errorf("funcClock with fn = %v, want %v", got, fixed)
	}
	// nil now → falls back to time.Now (non-zero).
	if (funcClock{}).Now().IsZero() {
		t.Error("funcClock with nil now should fall back to time.Now")
	}
}

func TestBuildExecutorEngine_ErrorOnBadRoot(t *testing.T) {
	c, _, _ := newTestController(t, t.TempDir())
	if _, err := c.buildExecutorEngine("", reconcilePayload{MaxConcurrentTasks: 1, AllowedModels: []string{"m"}}); err == nil {
		t.Error("empty agent root must surface an error")
	}
}

func TestDrainExecutor_NilGuards(t *testing.T) {
	c, ee, _ := engineForAgent(t, "agent-nilguard")
	c.drainExecutor(nil, nil) // must not panic
	c.drainExecutor(ee, nil)  // nil handle → no-op
}

func TestMaybeAttachExecutorEngine(t *testing.T) {
	trueBin := lookTrue(t)
	base := t.TempDir()
	c, _, _ := newTestController(t, base)
	c.cfg.BinaryPath = trueBin

	// Reserve managedAgents the way startSession does (attach targets an existing entry).
	for _, id := range []string{"a-on", "a-codex", "a-off"} {
		c.mu.Lock()
		c.agents[id] = &managedAgent{agentID: id}
		c.mu.Unlock()
	}

	enabled := reconcilePayload{AgentID: "a-on", MaxConcurrentTasks: 2, AllowedModels: []string{"m"}}
	c.maybeAttachExecutorEngine(enabled)
	c.mu.Lock()
	onExec := c.agents["a-on"].exec
	c.mu.Unlock()
	if onExec == nil {
		t.Error("opt-in agent should get an executor engine attached")
	}

	// Codex agent: excluded even when concurrency fields are set.
	codexPl := reconcilePayload{AgentID: "a-codex", MaxConcurrentTasks: 2, AllowedModels: []string{"m"}, CLI: cliCodex}
	c.maybeAttachExecutorEngine(codexPl)
	c.mu.Lock()
	codexExec := c.agents["a-codex"].exec
	c.mu.Unlock()
	if codexExec != nil {
		t.Error("codex agent must NOT get an executor engine")
	}

	// Concurrency not enabled: no engine (legacy inject path).
	c.maybeAttachExecutorEngine(reconcilePayload{AgentID: "a-off", MaxConcurrentTasks: 0})
	c.mu.Lock()
	offExec := c.agents["a-off"].exec
	c.mu.Unlock()
	if offExec != nil {
		t.Error("non-opt-in agent must keep the legacy inject path (no engine)")
	}

	// Path-resolution failure (empty home base) → logs + falls back, no attach/panic.
	c.mu.Lock()
	c.agents["a-badpath"] = &managedAgent{agentID: "a-badpath"}
	savedBase := c.cfg.AgentHomeBase
	c.cfg.AgentHomeBase = ""
	c.mu.Unlock()
	c.maybeAttachExecutorEngine(reconcilePayload{AgentID: "a-badpath", MaxConcurrentTasks: 2, AllowedModels: []string{"m"}})
	c.mu.Lock()
	badExec := c.agents["a-badpath"].exec
	c.cfg.AgentHomeBase = savedBase
	c.mu.Unlock()
	if badExec != nil {
		t.Error("path-resolution failure must fall back (no engine attached)")
	}
}

// engineForAgent builds an executor engine for a test agent whose forked executors
// are the harmless `true` binary.
func engineForAgent(t *testing.T, agentID string) (*AgentController, *executorEngine, string) {
	t.Helper()
	trueBin := lookTrue(t)
	base := t.TempDir()
	c, _, _ := newTestController(t, base)
	c.cfg.BinaryPath = trueBin // forked executors run `true` (exit 0, ignore args)
	home, _, _, err := c.agentPaths(agentID)
	if err != nil {
		t.Fatalf("agentPaths: %v", err)
	}
	pl := reconcilePayload{
		AgentID:              agentID,
		MaxConcurrentTasks:   2,
		AllowedModels:        []string{"claude-haiku"},
		DefaultExecutorModel: "claude-default",
	}
	ee, err := c.buildExecutorEngine(home, pl)
	if err != nil {
		t.Fatalf("buildExecutorEngine: %v", err)
	}
	return c, ee, home
}

func TestBuildExecutorEngine_ForksAndDrainFreesSlot(t *testing.T) {
	c, ee, _ := engineForAgent(t, "agent-x")
	if ee.engine.Pool().Max() != 2 {
		t.Fatalf("pool max = %d, want 2", ee.engine.Pool().Max())
	}
	// Fork a real (harmless) executor through the full chain.
	launched, err := ee.engine.HandleWork(context.Background(), orchestrator.WorkItem{
		TaskRef: "task-1", Goal: executor.Goal{Title: "do it"},
	})
	if err != nil {
		t.Fatalf("HandleWork: %v", err)
	}
	if launched.Model != "claude-default" {
		t.Errorf("model = %q, want claude-default (chain fallback)", launched.Model)
	}
	if ee.engine.Pool().Active() != 1 {
		t.Fatalf("active = %d, want 1 after fork", ee.engine.Pool().Active())
	}
	// Drain synchronously: reaps the `true` process and frees the slot.
	c.drainExecutor(ee, launched.Handle)
	if ee.engine.Pool().Active() != 0 {
		t.Errorf("active = %d, want 0 after drain", ee.engine.Pool().Active())
	}
}

func TestWorkViaExecutor_ForksAndRegistersRouting(t *testing.T) {
	c, ee, home := engineForAgent(t, "agent-y")
	c.mu.Lock()
	c.agents["agent-y"] = &managedAgent{agentID: "agent-y", exec: ee}
	c.mu.Unlock()

	err := c.workViaExecutor(context.Background(), workPayload{
		AgentID: "agent-y", TaskID: "t-1", TaskRef: "task-1", Brief: "fix the thing\nmore detail",
	}, ee)
	if err != nil {
		t.Fatalf("workViaExecutor: %v", err)
	}
	// Routing.json persists past the (async) drain — assert a problem was registered
	// with the task ref. (The drain goroutine only frees the pool slot + dir.)
	store, err := executor.NewRoutingStore(home, nil)
	if err != nil {
		t.Fatalf("NewRoutingStore: %v", err)
	}
	tbl, err := store.Load()
	if err != nil {
		t.Fatalf("routing Load: %v", err)
	}
	if len(tbl.Problems) != 1 || len(tbl.Problems[0].TaskRefs) == 0 || tbl.Problems[0].TaskRefs[0] != "task-1" {
		t.Errorf("expected one problem bound to task-1, got %+v", tbl.Problems)
	}
	// currentTaskID bookkeeping was set (mirrors the inject path).
	c.mu.Lock()
	got := c.agents["agent-y"].currentTaskID
	c.mu.Unlock()
	if got != "t-1" {
		t.Errorf("currentTaskID = %q, want t-1", got)
	}
}

// errRunnerWD makes the engine's runner build fail, exercising workViaExecutor's
// non-capacity error branch.
type errRunnerWD struct{}

func (errRunnerWD) Build(string, string) ([]string, error) { return nil, errors.New("runner boom") }

func TestWorkViaExecutor_NonCapacityErrorWraps(t *testing.T) {
	trueBin := lookTrue(t)
	home := t.TempDir()
	c, _, _ := newTestController(t, t.TempDir())
	c.cfg.BinaryPath = trueBin
	layout, _ := executor.NewLayout(home)
	fx, _ := executor.NewFileExchange(layout, nil)
	pool, _ := executor.NewPool(executor.PoolConfig{Exchange: fx, Spawner: executor.NewSpawner(), AgentRoot: home, BinaryPath: trueBin, Max: 2})
	routing, _ := executor.NewRoutingStore(home, nil)
	eng, _ := orchestrator.NewEngine(orchestrator.EngineConfig{
		Pool: pool, Routing: routing, Router: modelrouter.NewRouter(nil),
		RouterConfig: modelrouter.Config{DefaultExecutorModel: "m"},
		Runner:       errRunnerWD{}, IDs: orchestrator.NewULIDMinter(nil),
	})
	mon, _ := executor.NewMonitor(executor.MonitorConfig{Exchange: fx, Pool: pool})
	ee := &executorEngine{engine: eng, monitor: mon}

	err := c.workViaExecutor(context.Background(), workPayload{AgentID: "a", TaskID: "t", Brief: "do"}, ee)
	if err == nil || errors.Is(err, executor.ErrAtCapacity) {
		t.Fatalf("expected a non-capacity fork error, got %v", err)
	}
	if !strings.Contains(err.Error(), "fork executor") {
		t.Errorf("expected wrapped fork-executor error, got %v", err)
	}
}

func TestWorkViaExecutor_AtCapacityRetryable(t *testing.T) {
	c, ee, _ := engineForAgent(t, "agent-z") // max 2
	// Saturate the pool by launching 2 executors WITHOUT draining (hold the slots).
	for i := 0; i < 2; i++ {
		if _, err := ee.engine.HandleWork(context.Background(), orchestrator.WorkItem{
			TaskRef: "task-" + string(rune('a'+i)), Goal: executor.Goal{Title: "g"},
		}); err != nil {
			t.Fatalf("saturating launch %d: %v", i, err)
		}
	}
	err := c.workViaExecutor(context.Background(), workPayload{AgentID: "agent-z", TaskID: "t3", TaskRef: "task-c", Brief: "x"}, ee)
	if err == nil {
		t.Fatal("expected a retryable at-capacity error")
	}
	if !errors.Is(err, executor.ErrAtCapacity) {
		t.Errorf("expected at-capacity error, got %v", err)
	}
}
