package workerdaemon

// concurrency_snapshot_test.go — DAEMON-side aggregate test kept after Phase 0c (the
// per-executor ExecutorEngine.SnapshotConcurrency tests moved into package
// agentruntime). This drives AgentController.SnapshotConcurrency's cross-agent
// aggregation: an agent with an engine attached to its runtime appears; one without is
// absent.

import (
	"testing"
)

func TestAgentController_SnapshotConcurrency_PerAgent(t *testing.T) {
	trueBin := lookTrue(t)
	base := t.TempDir()
	c, _, _ := newTestController(t, base)
	c.cfg.BinaryPath = trueBin

	// agent-agg: engine attached + one (adopted-orphan) executor present.
	rt := reserveRuntime(t, c, "agent-agg")
	home, _, _, err := c.agentPaths("agent-agg")
	if err != nil {
		t.Fatalf("agentPaths: %v", err)
	}
	ee, err := rt.BuildExecutorEngine(home, execConfigOf(reconcilePayload{
		AgentID: "agent-agg", MaxConcurrentTasks: 2, AllowedExecutors: testExecs, DefaultExecutorModel: "d",
	}))
	if err != nil {
		t.Fatalf("BuildExecutorEngine: %v", err)
	}
	rt.AttachExecutor(ee)
	rt.SeedOrphanForTest("executor-1", 4242) // deterministic live entry

	// agent-plain: a runtime with NO engine → must be absent from the aggregate.
	reserveRuntime(t, c, "agent-plain")

	all := c.SnapshotConcurrency()
	snap, ok := all["agent-agg"]
	if !ok {
		t.Fatalf("agent-agg missing from controller snapshot: %+v", all)
	}
	if snap.Active != 1 || len(snap.Executors) != 1 {
		t.Errorf("agent snapshot active=%d execs=%d, want 1/1", snap.Active, len(snap.Executors))
	}
	if _, ok := all["agent-plain"]; ok {
		t.Errorf("agent with no executor engine must be absent from the aggregate: %+v", all)
	}
}
