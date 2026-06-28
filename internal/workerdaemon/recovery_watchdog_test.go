package workerdaemon

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
)

// seedExchange builds a FileExchange + Tracker rooted at the same agent home the
// executor engine uses, so a test can plant orphan executor dirs (input/status/
// output + orchestrator.json Record) exactly as a prior daemon process would have.
func seedExchange(t *testing.T, home string) (*executor.FileExchange, *executor.Tracker) {
	t.Helper()
	layout, err := executor.NewLayout(home)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	fx, err := executor.NewFileExchange(layout, nil)
	if err != nil {
		t.Fatalf("NewFileExchange: %v", err)
	}
	tr, err := executor.NewTracker(layout)
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	return fx, tr
}

// liveChild starts a harmless long-lived real process and returns it with its pid,
// to stand in for an orphan executor that is still alive after a restart. The test
// is responsible for killing it.
func liveChild(t *testing.T) (*exec.Cmd, int) {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "sleep 60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start live child: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	return cmd, cmd.Process.Pid
}

// deadPID starts then reaps a process so its pid is a real-but-gone pid (a
// deterministic "not alive" value for SignalLiveness, avoiding pid-guess flakiness).
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Wait()
	return pid
}

func seedOrphan(t *testing.T, fx *executor.FileExchange, tr *executor.Tracker, id string, pid int, st executor.Status, out *executor.Output) {
	t.Helper()
	if _, err := fx.Provision(id); err != nil {
		t.Fatalf("Provision %s: %v", id, err)
	}
	if err := fx.WriteInput(executor.Input{ExecutorID: id, Goal: executor.Goal{Title: "t"}, Model: "m", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("WriteInput %s: %v", id, err)
	}
	if err := fx.WriteStatus(st); err != nil {
		t.Fatalf("WriteStatus %s: %v", id, err)
	}
	if out != nil {
		if err := fx.WriteOutput(*out); err != nil {
			t.Fatalf("WriteOutput %s: %v", id, err)
		}
	}
	if err := tr.Write(executor.Record{ExecutorID: id, PID: pid, SpawnedAt: time.Now()}); err != nil {
		t.Fatalf("Tracker.Write %s: %v", id, err)
	}
}

func dirGone(t *testing.T, fx *executor.FileExchange, id string) bool {
	t.Helper()
	d, err := fx.Layout().Dir(id)
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	snaps, err := fx.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	_ = d
	for _, s := range snaps {
		if s.ExecutorID == id {
			return false
		}
	}
	return true
}

// W3 acceptance: after a restart the orchestrator rebuilds in-flight executor state
// from durable files — a still-alive orphan is re-adopted (counts toward the cap and
// is registered for watchdog polling), and a terminal orphan is finalized (not lost,
// dir torn down). It re-spawns nothing.
func TestRecoverExecutors_AdoptsRunningFinalizesTerminal(t *testing.T) {
	c, ee, home := engineForAgent(t, "agent-rec")
	c.mu.Lock()
	c.agents["agent-rec"] = &managedAgent{agentID: "agent-rec", exec: ee}
	c.mu.Unlock()

	fx, tr := seedExchange(t, home)
	_, alivePID := liveChild(t)
	now := time.Now()

	// Orphan A: still running (alive pid, fresh status) → must be re-adopted.
	seedOrphan(t, fx, tr, "exec-aaa111", alivePID,
		executor.Status{ExecutorID: "exec-aaa111", State: executor.StateRunning, Model: "m", StartedAt: now, LastProgressAt: now}, nil)
	// Orphan B: finished successfully before/at the crash (dead pid + success output)
	// → must be finalized and its dir torn down.
	seedOrphan(t, fx, tr, "exec-bbb222", deadPID(t),
		executor.Status{ExecutorID: "exec-bbb222", State: executor.StateDone, Model: "m", StartedAt: now, LastProgressAt: now},
		&executor.Output{ExecutorID: "exec-bbb222", Success: true, Result: "ok", FinishedAt: now})

	c.recoverExecutors(context.Background(), "agent-rec", ee)

	// A re-adopted: registered for watchdog polling AND counted in the pool.
	orphans := ee.snapshotOrphans()
	if pid, ok := orphans["exec-aaa111"]; !ok || pid != alivePID {
		t.Errorf("running orphan A not adopted for watchdog: %+v", orphans)
	}
	if _, ok := orphans["exec-bbb222"]; ok {
		t.Error("terminal orphan B must NOT be registered for polling")
	}
	if ee.engine.Pool().Active() != 1 {
		t.Errorf("pool active = %d, want 1 (A re-adopted toward the cap)", ee.engine.Pool().Active())
	}
	// B finalized: its dir is gone (result not lost across the restart).
	if !dirGone(t, fx, "exec-bbb222") {
		t.Error("terminal orphan B dir must be torn down by recovery")
	}
	// A retained (still running).
	if dirGone(t, fx, "exec-aaa111") {
		t.Error("running orphan A dir must be retained")
	}
}

// W3 acceptance: an adopted orphan (no reapable handle) is watched by the watchdog
// tick — when its process exits, the poll detects it, finalizes it, and stops
// polling. This is the gap the handle-based reap path cannot cover.
func TestExecutorWatchdog_PollsAdoptedOrphanToCompletion(t *testing.T) {
	c, ee, home := engineForAgent(t, "agent-wd")
	c.mu.Lock()
	c.agents["agent-wd"] = &managedAgent{agentID: "agent-wd", exec: ee}
	c.mu.Unlock()

	fx, tr := seedExchange(t, home)
	child, alivePID := liveChild(t)
	now := time.Now()
	seedOrphan(t, fx, tr, "exec-ccc333", alivePID,
		executor.Status{ExecutorID: "exec-ccc333", State: executor.StateRunning, Model: "m", StartedAt: now, LastProgressAt: now}, nil)

	c.recoverExecutors(context.Background(), "agent-wd", ee)
	if _, ok := ee.snapshotOrphans()["exec-ccc333"]; !ok {
		t.Fatal("orphan C should be adopted for watchdog polling")
	}

	// Tick 1 (orphan alive): still polled, not finalized.
	c.maybeRunExecutorWatchdog(context.Background(), now)
	if _, ok := ee.snapshotOrphans()["exec-ccc333"]; !ok {
		t.Fatal("orphan C must remain tracked while alive")
	}

	// The orphan process exits (and is reaped, so its pid is truly gone).
	_ = child.Process.Kill()
	_, _ = child.Process.Wait()

	// Tick 2 (>throttle later, orphan gone): the poll finalizes it and drops it.
	c.maybeRunExecutorWatchdog(context.Background(), now.Add(defaultExecutorWatchdogInterval+time.Second))
	if _, ok := ee.snapshotOrphans()["exec-ccc333"]; ok {
		t.Error("orphan C must be dropped after the watchdog observes its exit")
	}
	if ee.engine.Pool().Active() != 0 {
		t.Errorf("pool active = %d, want 0 after orphan finalized", ee.engine.Pool().Active())
	}
}

// Recovery runs exactly once per agent per process: a second attach (in-process
// reconcile) must NOT re-scan, so this-process executors are never mis-adopted as
// orphans (which would double-finalize).
func TestMaybeAttach_RecoversOnlyOnFirstAttach(t *testing.T) {
	c, _, home := engineForAgent(t, "agent-once")
	fx, tr := seedExchange(t, home)
	now := time.Now()
	// A terminal orphan present before any attach.
	seedOrphan(t, fx, tr, "exec-ddd444", deadPID(t),
		executor.Status{ExecutorID: "exec-ddd444", State: executor.StateDone, Model: "m", StartedAt: now, LastProgressAt: now},
		&executor.Output{ExecutorID: "exec-ddd444", Success: true, Result: "ok", FinishedAt: now})

	pl := reconcilePayload{AgentID: "agent-once", MaxConcurrentTasks: 2, AllowedModels: []string{"m"}, DefaultExecutorModel: "d"}
	// First attach → recovery runs → the terminal orphan is finalized (dir gone).
	c.mu.Lock()
	c.agents["agent-once"] = &managedAgent{agentID: "agent-once"}
	c.mu.Unlock()
	c.maybeAttachExecutorEngine(context.Background(), pl)
	if !dirGone(t, fx, "exec-ddd444") {
		t.Fatal("first attach should have recovered+finalized the terminal orphan")
	}

	// Re-seed a terminal orphan, then attach AGAIN: recovery must NOT run, so the dir
	// stays (a second attach is an in-process reconcile, not a restart).
	seedOrphan(t, fx, tr, "exec-eee555", deadPID(t),
		executor.Status{ExecutorID: "exec-eee555", State: executor.StateDone, Model: "m", StartedAt: now, LastProgressAt: now},
		&executor.Output{ExecutorID: "exec-eee555", Success: true, Result: "ok", FinishedAt: now})
	c.maybeAttachExecutorEngine(context.Background(), pl)
	if dirGone(t, fx, "exec-eee555") {
		t.Error("second attach must NOT re-run recovery (in-process reconcile, not a restart)")
	}
}
