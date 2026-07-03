package agentruntime

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
)

// T854 D6 fix: the controller-mode agent-runtime process attaches its executor engine
// itself (the daemon used to). These tests exercise the attach seam AttachExecutorEngine
// so a "fake runtime that bypasses a real fork" can't hide the wiring gap again.

// TestAttachExecutorEngine_ForksExecutor is the regression guard for the P0: after
// AttachExecutorEngine the runtime has a REAL engine that actually forks an executor
// (the harmless `true` binary stands in for the model CLI). Before the fix the agent
// process never attached → HasExecutor()==false → SpawnExecutor was a silent no-op.
func TestAttachExecutorEngine_ForksExecutor(t *testing.T) {
	base := t.TempDir()
	agentID := "agent-attach"
	rt := newExecRuntime(t, base, agentID, lookTrue(t))

	if err := rt.AttachExecutorEngine(ExecutorConfig{
		AgentID:              agentID,
		MaxConcurrentTasks:   2,
		DefaultExecutorModel: "claude-default",
	}); err != nil {
		t.Fatalf("AttachExecutorEngine: %v", err)
	}
	if !rt.HasExecutor() {
		t.Fatal("AttachExecutorEngine must leave HasExecutor()==true (the P0 was: it never attached)")
	}

	sc := &scriptedToolCaller{getTaskBody: map[string]any{
		"id": "task-9", "title": "t", "description": "d", "status": "open", "model": "claude-haiku",
	}}
	setToolCaller(rt, sc)
	if _, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-9"}); err != nil {
		t.Fatalf("SpawnExecutor: %v", err)
	}
	// A real fork happened: get_task→start_task ran and a problem is bound to the task.
	if seen := sc.toolsSeen(); len(seen) != 2 || seen[0] != "get_task" || seen[1] != "start_task" {
		t.Fatalf("expected get_task→start_task (a real fork), got %v", seen)
	}
	home := agentHomeOf(t, base, agentID)
	if probs := loadRouting(t, home); len(probs) != 1 {
		t.Fatalf("an attached engine must fork one executor problem, got %d", len(probs))
	}
}

// TestAttachExecutorEngine_BeforeBoot_RecoversInflight is the regression guard for
// tester3's §6.7 at unit level: an agent-runtime process that (re)starts with an
// in-flight executor Record on disk must ENGINE-ATTACH-BEFORE-BOOT so selfReconcile has
// an engine and recovers that executor — zero reconcile command, zero human. If the
// engine were attached AFTER Boot (the bug), selfReconcile would no-op and the executor
// would be lost.
func TestAttachExecutorEngine_BeforeBoot_RecoversInflight(t *testing.T) {
	base := t.TempDir()
	agentID := "agent-recover"
	rt := newExecRuntime(t, base, agentID, lookTrue(t))
	home := agentHomeOf(t, base, agentID)

	// Seed a prior process's in-flight executor: an executor dir with input.json (its
	// task ref) + an orchestrator.json Record whose pid is THIS test process (alive).
	execID := "exec-inflight-001"
	seedInflightExecutor(t, home, execID, "task-42")

	// Engine-attach-BEFORE-Boot (the fix): attach, THEN Boot.
	if err := rt.AttachExecutorEngine(ExecutorConfig{
		AgentID:              agentID,
		MaxConcurrentTasks:   2,
		DefaultExecutorModel: "claude-default",
	}); err != nil {
		t.Fatalf("AttachExecutorEngine: %v", err)
	}
	if err := rt.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}

	// selfReconcile ran WITH an engine → the alive in-flight executor was re-adopted
	// (it shows in the live concurrency snapshot). Without engine-attach-before-Boot the
	// snapshot would be empty (the executor lost).
	snaps := rt.SnapshotConcurrency()
	found := false
	for _, s := range snaps {
		if s.ExecutorID == execID {
			found = true
		}
	}
	if !found {
		t.Fatalf("engine-attach-before-Boot must recover the in-flight executor; snapshot=%+v", snaps)
	}
}

// recordingCaller is a ToolCaller that returns a fixed inflight set and records which
// agent-tools were called (so a test can assert block_task/complete_task did NOT fire).
type recordingCaller struct {
	mu       sync.Mutex
	inflight []string
	calls    []string
}

func (c *recordingCaller) CallAgentTool(_ context.Context, tool string, _ any, out *json.RawMessage) error {
	c.mu.Lock()
	c.calls = append(c.calls, tool)
	c.mu.Unlock()
	if tool == "list_my_inflight_tasks" && out != nil {
		tasks := make([]map[string]string, 0, len(c.inflight))
		for _, id := range c.inflight {
			tasks = append(tasks, map[string]string{"task_id": id})
		}
		b, _ := json.Marshal(map[string]any{"tasks": tasks})
		*out = b
	}
	return nil
}

func (c *recordingCaller) called(tool string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range c.calls {
		if t == tool {
			return true
		}
	}
	return false
}

// deadPID returns a pid that is guaranteed dead (a /bin/true we forked + reaped), so the
// recovery scan classifies the seeded executor as a DEATH, not alive.
func reapedPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(lookTrue(t))
	if err := cmd.Start(); err != nil {
		t.Fatalf("start true: %v", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Wait() // reap → pid is now dead
	return pid
}

// TestReconcile_DeadShouldContinue_RecoversNotFinalizes is the regression guard for the
// T854 D6 P0-2 boot-recovery bug tester3 caught: at boot, a DEAD but still-in-flight
// executor must be routed to the §4.3 tier ladder (recover) — NOT finalized (block_task
// + teardown) by the scan before the ladder runs. Before the fix, monitor.Recover
// finalized every dead executor first, so tier recovery never triggered.
func TestReconcile_DeadShouldContinue_RecoversNotFinalizes(t *testing.T) {
	base := t.TempDir()
	agentID := "agent-dead-recover"
	rt := newExecRuntime(t, base, agentID, lookTrue(t))
	home := agentHomeOf(t, base, agentID)

	// Seed a DEAD executor whose task is still in-flight, with a session-id + a resumable
	// argv + its workspace present (so the ladder would resume in place).
	execID, taskRef := "exec-dead-001", "task-42"
	seedDeadExecutor(t, home, execID, taskRef, reapedPID(t))

	caller := &recordingCaller{inflight: []string{taskRef}} // task IS in-flight → should-continue
	setToolCaller(rt, caller)
	if err := rt.AttachExecutorEngine(ExecutorConfig{AgentID: agentID, MaxConcurrentTasks: 2, DefaultExecutorModel: "claude-default"}); err != nil {
		t.Fatalf("AttachExecutorEngine: %v", err)
	}
	if err := rt.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}

	// It consulted the inflight set...
	if !caller.called("list_my_inflight_tasks") {
		t.Error("reconcile must consult the in-flight set")
	}
	// ...and did NOT finalize/block the dead should-continue executor (the P0-2 fix): it
	// was routed to the recovery ladder, not written back as blocked/complete.
	if caller.called("block_task") || caller.called("complete_task") {
		t.Fatalf("dead should-continue executor must be RECOVERED, not finalized; calls=%v", caller.calls)
	}
}

// seedDeadExecutor seeds a dead executor (input + status + a Record with session-id +
// resumable argv) AND its workspace dir, as a prior process left it before dying.
func seedDeadExecutor(t *testing.T, home, execID, taskRef string, pid int) {
	t.Helper()
	layout, err := executor.NewLayout(home)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	fx, err := executor.NewFileExchange(layout, nil)
	if err != nil {
		t.Fatalf("NewFileExchange: %v", err)
	}
	if _, err := fx.Provision(execID); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if err := fx.WriteInput(executor.Input{
		ExecutorID: execID, Goal: executor.Goal{Title: "resume me"}, Model: "claude-haiku",
		Source: executor.SourceRefs{TaskRef: taskRef}, CreatedAt: time.Unix(1700000000, 0),
	}); err != nil {
		t.Fatalf("WriteInput: %v", err)
	}
	if err := fx.WriteStatus(executor.Status{ExecutorID: execID, State: executor.StateRunning, Model: "m"}); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}
	// Workspace present → tier-1/2 (resume/rerun in place), not tier-3.
	if ws, err := layout.WorkspaceDir(execID); err == nil {
		_ = os.MkdirAll(ws, 0o700)
	}
	tr, _ := executor.NewTracker(layout)
	if err := tr.Write(executor.Record{
		ExecutorID: execID, PID: pid, SpawnedAt: time.Unix(1700000000, 0),
		SessionID: "11111111-2222-5333-8444-555555555555",
		RunnerCmd: []string{"claude", "-p", "goal", "--session-id", "11111111-2222-5333-8444-555555555555"},
	}); err != nil {
		t.Fatalf("tracker.Write: %v", err)
	}
}

// agentHomeOf computes the per-agent home the runtime uses (base/agents/<id>, matching
// agentPaths).
func agentHomeOf(t *testing.T, base, agentID string) string {
	t.Helper()
	return filepath.Join(base, "agents", agentID)
}

// seedInflightExecutor writes an executor dir (input.json + orchestrator.json Record)
// under home/executors/<id> as a prior, still-alive process would have left it.
func seedInflightExecutor(t *testing.T, home, execID, taskRef string) {
	t.Helper()
	layout, err := executor.NewLayout(home)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	fx, err := executor.NewFileExchange(layout, nil)
	if err != nil {
		t.Fatalf("NewFileExchange: %v", err)
	}
	if _, err := fx.Provision(execID); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if err := fx.WriteInput(executor.Input{
		ExecutorID: execID,
		Goal:       executor.Goal{Title: "recover me"},
		Model:      "claude-haiku",
		Source:     executor.SourceRefs{TaskRef: taskRef},
		CreatedAt:  time.Unix(1700000000, 0),
	}); err != nil {
		t.Fatalf("WriteInput: %v", err)
	}
	if err := fx.WriteStatus(executor.Status{ExecutorID: execID, State: executor.StateRunning, Model: "m"}); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}
	tr, err := executor.NewTracker(layout)
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	if err := tr.Write(executor.Record{
		ExecutorID: execID,
		PID:        os.Getpid(), // alive → recovery re-adopts it
		SpawnedAt:  time.Unix(1700000000, 0),
		RunnerCmd:  []string{"true"},
	}); err != nil {
		t.Fatalf("tracker.Write: %v", err)
	}
}
