package workerdaemon

// concurrent_exec_test.go — DAEMON-side executor wiring tests kept after Phase 0c
// moved the engine + fork/drain/recover/watchdog into agentruntime (those tests now
// live in package agentruntime). What stays here drives the daemon's opt-in gate,
// engine attach/reattach onto the per-agent LocalRuntime, and the recovery-once guard.

import (
	"context"
	"os/exec"
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/workerdaemon/agentruntime"
)

// testExecs is a one-entry allowed_executors list that opts an agent into concurrency.
var testExecs = []agent.ExecutorProfile{{CLI: "claude-code", Model: "m"}}

func lookTrue(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("true")
	if err != nil {
		t.Skipf("`true` not available: %v", err)
	}
	return p
}

// reserveRuntime installs a fresh managedAgent (runtime + shared state) for agentID,
// mirroring bringUpSession's reservation so the executor engine has a runtime to
// attach to. Returns the runtime so a test can assert HasExecutor().
func reserveRuntime(t *testing.T, c *AgentController, agentID string) *agentruntime.LocalRuntime {
	t.Helper()
	rt, st := c.newRuntimeFor(agentID)
	c.mu.Lock()
	c.agents[agentID] = &managedAgent{agentID: agentID, runtime: rt, state: st}
	c.mu.Unlock()
	return rt
}

func TestConcurrencyEnabled(t *testing.T) {
	cases := []struct {
		name string
		pl   reconcilePayload
		want bool
	}{
		{"both set", reconcilePayload{MaxConcurrentTasks: 2, AllowedExecutors: testExecs}, true},
		{"no max", reconcilePayload{AllowedExecutors: testExecs}, false},
		{"no executors", reconcilePayload{MaxConcurrentTasks: 2}, false},
		{"neither", reconcilePayload{}, false},
	}
	for _, tc := range cases {
		if got := concurrencyEnabled(tc.pl); got != tc.want {
			t.Errorf("%s: concurrencyEnabled = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestMaybeAttachExecutorEngine(t *testing.T) {
	trueBin := lookTrue(t)
	base := t.TempDir()
	c, _, _ := newTestController(t, base)
	c.cfg.BinaryPath = trueBin

	// Reserve managedAgents WITH runtimes (the engine attaches onto the runtime now).
	rtOn := reserveRuntime(t, c, "a-on")
	rtCodex := reserveRuntime(t, c, "a-codex")
	rtOff := reserveRuntime(t, c, "a-off")

	enabled := reconcilePayload{AgentID: "a-on", MaxConcurrentTasks: 2, AllowedExecutors: testExecs, AllowedModels: []string{"m"}}
	c.maybeAttachExecutorEngine(context.Background(), enabled)
	if !rtOn.HasExecutor() {
		t.Error("opt-in agent should get an executor engine attached")
	}

	// Codex agent: excluded even when concurrency fields are set.
	codexPl := reconcilePayload{AgentID: "a-codex", MaxConcurrentTasks: 2, AllowedExecutors: testExecs, AllowedModels: []string{"m"}, CLI: cliCodex}
	c.maybeAttachExecutorEngine(context.Background(), codexPl)
	if rtCodex.HasExecutor() {
		t.Error("codex agent must NOT get an executor engine")
	}

	// Concurrency not enabled: no engine (legacy inject path).
	c.maybeAttachExecutorEngine(context.Background(), reconcilePayload{AgentID: "a-off", MaxConcurrentTasks: 0})
	if rtOff.HasExecutor() {
		t.Error("non-opt-in agent must keep the legacy inject path (no engine)")
	}

	// Path-resolution failure (empty home base) → logs + falls back, no attach/panic.
	rtBad := reserveRuntime(t, c, "a-badpath")
	c.mu.Lock()
	savedBase := c.cfg.AgentHomeBase
	c.cfg.AgentHomeBase = ""
	c.mu.Unlock()
	c.maybeAttachExecutorEngine(context.Background(), reconcilePayload{AgentID: "a-badpath", MaxConcurrentTasks: 2, AllowedExecutors: testExecs, AllowedModels: []string{"m"}})
	c.mu.Lock()
	c.cfg.AgentHomeBase = savedBase
	c.mu.Unlock()
	if rtBad.HasExecutor() {
		t.Error("path-resolution failure must fall back (no engine attached)")
	}
}

// TestReattachExecutorEngineFromCache covers the concurrency-degradation fix: after a
// relaunch the agent's managedAgent is fresh with a nil executor engine, so the engine
// must be RE-ATTACHED from the cached reconcile config onto the fresh runtime.
func TestReattachExecutorEngineFromCache(t *testing.T) {
	trueBin := lookTrue(t)
	base := t.TempDir()
	c, _, _ := newTestController(t, base)
	c.cfg.BinaryPath = trueBin

	// 1) First reconcile attaches the engine AND caches the config.
	reserveRuntime(t, c, "a-cc")
	pl := reconcilePayload{AgentID: "a-cc", MaxConcurrentTasks: 2, AllowedExecutors: testExecs, AllowedModels: []string{"m"}}
	c.maybeAttachExecutorEngine(context.Background(), pl)
	if _, ok := c.cachedExecConfig("a-cc"); !ok {
		t.Fatal("maybeAttachExecutorEngine must cache the concurrency config for later re-attach")
	}

	// 2) Simulate a relaunch: a FRESH managedAgent+runtime with exec==nil.
	rt2 := reserveRuntime(t, c, "a-cc")

	// 3) The relaunch path re-attaches from the cache → engine back, concurrency kept.
	c.reattachExecutorEngineFromCache(context.Background(), "a-cc")
	if !rt2.HasExecutor() {
		t.Fatal("reattachExecutorEngineFromCache must restore the executor engine after a relaunch (concurrency must survive restart)")
	}
}

// TestBootReattach_RestoresExecutorEngine reproduces the dev3 bug: after a worker
// daemon restart where the claude session SURVIVED, boot takes the bootReattach path.
// Before the fix that path re-attached the session but NOT the executor engine, so a
// concurrency-enabled agent stayed exec==nil — which (a) dropped it out of
// SnapshotConcurrency (HasExecutor()==false → "0/N slots in use") and (b) left any
// executor forked before the restart an UNMANAGED ORPHAN (never re-adopted into the
// watchdog → no progress/stop activity events). The fix calls
// reattachExecutorEngineFromCache from bootReattach, mirroring bootReapRelaunch. This
// test drives that exact seam: config seeded from the center resume-state on boot
// (seedExecConfig) + a fresh runtime (exec==nil), then the reattach restores the engine
// AND the agent's visibility in the concurrency snapshot.
func TestBootReattach_RestoresExecutorEngine(t *testing.T) {
	trueBin := lookTrue(t)
	base := t.TempDir()
	c, _, _ := newTestController(t, base)
	c.cfg.BinaryPath = trueBin

	// Boot seeds the concurrency config from the center resume-state (the in-memory
	// cache from a prior reconcile is gone after a process restart).
	pl := reconcilePayload{AgentID: "a-reattach", MaxConcurrentTasks: 2, AllowedExecutors: testExecs, AllowedModels: []string{"m"}}
	c.seedExecConfig(pl)

	// bootReattach reserves a FRESH managedAgent+runtime with exec==nil (mirrors
	// newRuntimeFor + c.agents[id]=ma before the live-session reattach).
	rt := reserveRuntime(t, c, "a-reattach")

	// BUG STATE: a live-survivor re-attach that forgot the engine → exec==nil → the
	// agent is ABSENT from the concurrency snapshot (this is the "0/N slots" symptom).
	if rt.HasExecutor() {
		t.Fatal("precondition: a fresh reattach runtime must start with no engine")
	}
	if _, ok := c.SnapshotConcurrency()["a-reattach"]; ok {
		t.Fatal("precondition (bug state): an agent with no engine must be absent from the snapshot — this is the 0/N-slots symptom")
	}

	// THE FIX: bootReattach now calls this after rt.Attach(sess).
	c.reattachExecutorEngineFromCache(context.Background(), "a-reattach")

	if !rt.HasExecutor() {
		t.Fatal("bootReattach must re-attach the executor engine from the seeded config (else: no executor lifecycle events + concurrency degrades to single-active)")
	}
	// Symptom 2 directly: the agent reappears in the concurrency snapshot → slots visible.
	if _, ok := c.SnapshotConcurrency()["a-reattach"]; !ok {
		t.Fatal("after reattach the agent must appear in SnapshotConcurrency (else the panel shows '0/N slots in use' while an executor is running)")
	}
}

// TestReattachExecutorEngineFromCache_NoOpForDefaultAgent: an agent with no cached
// concurrency config must NOT get an engine — reattach is a safe no-op.
func TestReattachExecutorEngineFromCache_NoOpForDefaultAgent(t *testing.T) {
	base := t.TempDir()
	c, _, _ := newTestController(t, base)
	rt := reserveRuntime(t, c, "a-default")

	c.reattachExecutorEngineFromCache(context.Background(), "a-default")
	if rt.HasExecutor() {
		t.Fatal("a default agent (no cached concurrency config) must not get an executor engine")
	}

	// seedExecConfig with a NON-concurrency config is also ignored (not cached).
	c.seedExecConfig(reconcilePayload{AgentID: "a-default", MaxConcurrentTasks: 0})
	if _, ok := c.cachedExecConfig("a-default"); ok {
		t.Fatal("seedExecConfig must ignore a non-concurrency config")
	}
}
