package workerdaemon

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/concurrency"
	"github.com/oopslink/agent-center/internal/workerdaemon/agentruntime"
	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
	"github.com/oopslink/agent-center/internal/workerdaemon/orchestrator"
)

// SnapshotConcurrency must enumerate the live this-process executors AND merge the
// adopted orphans (deduped), so active never under-reports after a restart.
func TestSnapshotConcurrency_LiveAndOrphanMerged(t *testing.T) {
	_, ee, _ := engineForAgent(t, "agent-snap")

	// Fork a real (harmless `true`) executor through the full chain → a live handle
	// with input.json (task/cli/model) written.
	launched, err := ee.engine.HandleWork(context.Background(), orchestrator.WorkItem{
		TaskID: "task-1", TaskRef: "task-1", Goal: executor.Goal{Title: "do it"},
	})
	if err != nil {
		t.Fatalf("HandleWork: %v", err)
	}
	defer func() { _ = launched.Handle.Wait() }()

	// Register an adopted orphan (no live handle) — must still appear.
	ee.addOrphan("executor-orphan", 4242)

	snaps := ee.SnapshotConcurrency()
	if len(snaps) != 2 {
		t.Fatalf("got %d snapshots, want 2 (1 live + 1 orphan): %+v", len(snaps), snaps)
	}
	byID := map[string]concurrency.ExecutorSnapshot{}
	for _, s := range snaps {
		byID[s.ExecutorID] = s
	}
	live, ok := byID[launched.ExecutorID]
	if !ok {
		t.Fatalf("live executor %s missing from snapshot", launched.ExecutorID)
	}
	// Live executor: task/cli/model resolved from input.json, started_at stamped.
	if live.TaskID != "task-1" {
		t.Errorf("live.TaskID = %q, want task-1", live.TaskID)
	}
	if live.CLI != "claude-code" || live.Model == "" {
		t.Errorf("live cli/model = %q/%q, want claude-code/<non-empty>", live.CLI, live.Model)
	}
	if live.StartedAt.IsZero() {
		t.Error("live.StartedAt should be stamped at spawn")
	}
	if live.State == concurrency.StateOrphan {
		t.Error("live executor must not be state=orphan")
	}
	orphan, ok := byID["executor-orphan"]
	if !ok {
		t.Fatal("orphan executor missing from snapshot")
	}
	if orphan.State != concurrency.StateOrphan || orphan.PID != 4242 {
		t.Errorf("orphan = %+v, want state=orphan pid=4242", orphan)
	}
}

// An orphan that is ALSO a live handle must not be double-counted (dedup by id).
func TestSnapshotConcurrency_DedupsOrphanAlsoLive(t *testing.T) {
	_, ee, _ := engineForAgent(t, "agent-dedup")
	launched, err := ee.engine.HandleWork(context.Background(), orchestrator.WorkItem{
		TaskID: "t", TaskRef: "t", Goal: executor.Goal{Title: "g"},
	})
	if err != nil {
		t.Fatalf("HandleWork: %v", err)
	}
	defer func() { _ = launched.Handle.Wait() }()

	ee.addOrphan(launched.ExecutorID, launched.Handle.PID) // same id as the live one
	snaps := ee.SnapshotConcurrency()
	if len(snaps) != 1 {
		t.Fatalf("got %d snapshots, want 1 (orphan deduped against the live handle)", len(snaps))
	}
	if snaps[0].State == concurrency.StateOrphan {
		t.Error("the live handle should win over the orphan entry (not state=orphan)")
	}
}

// AgentController.SnapshotConcurrency aggregates per-agent; an agent with no exec
// engine is absent.
func TestAgentController_SnapshotConcurrency_PerAgent(t *testing.T) {
	c, ee, _ := engineForAgent(t, "agent-agg")
	launched, err := ee.engine.HandleWork(context.Background(), orchestrator.WorkItem{
		TaskID: "t", TaskRef: "t", Goal: executor.Goal{Title: "g"},
	})
	if err != nil {
		t.Fatalf("HandleWork: %v", err)
	}
	defer func() { _ = launched.Handle.Wait() }()

	// Attach the engine to a managed agent (mirrors maybeAttachExecutorEngine).
	c.mu.Lock()
	c.agents["agent-agg"] = &managedAgent{agentID: "agent-agg", exec: ee, state: &agentruntime.SessionState{}}
	c.mu.Unlock()

	all := c.SnapshotConcurrency()
	snap, ok := all["agent-agg"]
	if !ok {
		t.Fatalf("agent-agg missing from controller snapshot: %+v", all)
	}
	if snap.Active != 1 || len(snap.Executors) != 1 {
		t.Errorf("agent snapshot active=%d execs=%d, want 1/1", snap.Active, len(snap.Executors))
	}
}
