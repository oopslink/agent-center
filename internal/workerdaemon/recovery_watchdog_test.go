package workerdaemon

// recovery_watchdog_test.go — DAEMON-side recovery-ONCE guard kept after Phase 0c
// (the Recover/RunWatchdog behavior tests moved into package agentruntime). This
// proves the daemon's recoveredExec guard runs executor crash recovery exactly once
// per agent per process: a second in-process attach must NOT re-scan (its running
// executors are this process's own children — re-adopting would double-finalize).

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
)

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

func seedTerminalOrphan(t *testing.T, fx *executor.FileExchange, tr *executor.Tracker, id string) {
	t.Helper()
	now := time.Now()
	if _, err := fx.Provision(id); err != nil {
		t.Fatalf("Provision %s: %v", id, err)
	}
	if err := fx.WriteInput(executor.Input{ExecutorID: id, Goal: executor.Goal{Title: "t"}, Model: "m", CreatedAt: now}); err != nil {
		t.Fatalf("WriteInput %s: %v", id, err)
	}
	if err := fx.WriteStatus(executor.Status{ExecutorID: id, State: executor.StateDone, Model: "m", StartedAt: now, LastProgressAt: now}); err != nil {
		t.Fatalf("WriteStatus %s: %v", id, err)
	}
	if err := fx.WriteOutput(executor.Output{ExecutorID: id, Success: true, Result: "ok", FinishedAt: now}); err != nil {
		t.Fatalf("WriteOutput %s: %v", id, err)
	}
	if err := tr.Write(executor.Record{ExecutorID: id, PID: deadPID(t), SpawnedAt: now}); err != nil {
		t.Fatalf("Tracker.Write %s: %v", id, err)
	}
}

func dirGone(t *testing.T, fx *executor.FileExchange, id string) bool {
	t.Helper()
	snaps, err := fx.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, s := range snaps {
		if s.ExecutorID == id {
			return false
		}
	}
	return true
}

// TestMaybeAttach_RecoversOnlyOnFirstAttach: recovery runs exactly once per agent per
// process — a second attach (in-process reconcile) must NOT re-scan.
func TestMaybeAttach_RecoversOnlyOnFirstAttach(t *testing.T) {
	trueBin := lookTrue(t)
	base := t.TempDir()
	c, _, _ := newTestController(t, base)
	c.cfg.BinaryPath = trueBin

	reserveRuntime(t, c, "agent-once")
	home, _, _, err := c.agentPaths("agent-once")
	if err != nil {
		t.Fatalf("agentPaths: %v", err)
	}
	fx, tr := seedExchange(t, home)

	// A terminal orphan present before any attach.
	seedTerminalOrphan(t, fx, tr, "exec-ddd444")

	pl := reconcilePayload{AgentID: "agent-once", MaxConcurrentTasks: 2, AllowedExecutors: testExecs, DefaultExecutorModel: "d"}
	// First attach → recovery runs → the terminal orphan is finalized (dir gone).
	c.maybeAttachExecutorEngine(context.Background(), pl)
	if !dirGone(t, fx, "exec-ddd444") {
		t.Fatal("first attach should have recovered+finalized the terminal orphan")
	}

	// Re-seed a terminal orphan, then attach AGAIN: recovery must NOT run, so the dir
	// stays (a second attach is an in-process reconcile, not a restart).
	seedTerminalOrphan(t, fx, tr, "exec-eee555")
	c.maybeAttachExecutorEngine(context.Background(), pl)
	if dirGone(t, fx, "exec-eee555") {
		t.Error("second attach must NOT re-run recovery (in-process reconcile, not a restart)")
	}
}
