package executor

import (
	"context"
	"testing"
)

// TestMonitor_FuseKillTask_MarksFusedNotStalled locks issue-88e32d98 root cause ①: a
// center lease-revoke fuse must label the kill FUSED (do-not-recover), NOT stalled. The
// pre-fix markStalled made the drain's point-recovery treat the deliberate stop as a
// recoverable stall → relaunch of a blocked task (the P67 Ship 事故).
func TestMonitor_FuseKillTask_MarksFusedNotStalled(t *testing.T) {
	f := newMonitorFixture(t, 3)
	id := "e-fuse"
	if _, err := f.pool.Launch(context.Background(), LaunchSpec{Input: inputWithTaskRef(id, "task-blocked")}); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	killed, err := f.mon.FuseKillTask(context.Background(), "task-blocked")
	if err != nil {
		t.Fatalf("FuseKillTask: %v", err)
	}
	if !killed {
		t.Fatal("FuseKillTask must report killed=true for a live pool executor on the task")
	}
	// The kill must NOT be labeled a stall (that is the root-cause mislabel).
	if f.mon.peekStalled(id) {
		t.Fatal("fuse-kill must NOT mark the executor stalled (root cause ①)")
	}
	// It MUST be labeled fused so point-recovery quiet-finalizes instead of relaunching.
	if !f.mon.TakeFused(id) {
		t.Fatal("fuse-kill must mark the executor fused (do-not-recover)")
	}
	// TakeFused consumes the mark.
	if f.mon.TakeFused(id) {
		t.Fatal("TakeFused must consume the fused mark (second take is false)")
	}
}

// TestMonitor_FuseKillTask_NoLiveExecutor: with no live pool handle for the task (e.g. an
// adopted orphan or already gone), FuseKillTask is a best-effort no-op (false, nil).
func TestMonitor_FuseKillTask_NoLiveExecutor(t *testing.T) {
	f := newMonitorFixture(t, 3)
	killed, err := f.mon.FuseKillTask(context.Background(), "task-absent")
	if err != nil {
		t.Fatalf("FuseKillTask: %v", err)
	}
	if killed {
		t.Fatal("FuseKillTask on a task with no live pool executor must return killed=false")
	}
}

// TestMonitor_FinalizeFused_NoWriteback locks the quiet-finalize contract: a fused
// executor frees its slot and tears down, but does NOT write back to the center (the
// center already blocked/owns the task — a writeback would inject a supervisor judgment
// that could complete/merge the deliberately stopped task, the P67 Ship 事故).
func TestMonitor_FinalizeFused_NoWriteback(t *testing.T) {
	f := newMonitorFixture(t, 3)
	obs := attachObserver(f)
	id := "e-fused-final"
	if _, err := f.pool.Launch(context.Background(), LaunchSpec{Input: inputWithTaskRef(id, "task-blocked")}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if got := f.pool.Active(); got != 1 {
		t.Fatalf("pool Active=%d after Launch, want 1", got)
	}

	comp := Completion{ExecutorID: id, Kind: OutcomeCrashed, Retryable: true}
	if err := f.mon.FinalizeFused(context.Background(), comp); err != nil {
		t.Fatalf("FinalizeFused: %v", err)
	}

	// The sole-writer sink must have received NOTHING (no writeback).
	if kinds := f.wb.kinds(); len(kinds) != 0 {
		t.Fatalf("FinalizeFused must NOT write back; got reports=%v", kinds)
	}
	// The slot is freed (a queued executor can launch).
	if got := f.pool.Active(); got != 0 {
		t.Fatalf("pool Active=%d after FinalizeFused, want 0 (slot freed)", got)
	}
	// A terminal stop is still emitted for observability.
	if ev := obs.lastStop(t); ev.ExecutorID != id {
		t.Fatalf("FinalizeFused must emit a terminal stop for %s, got %+v", id, ev)
	}
	// Even a "retryable" crash is finalized (not retained as re-launchable residue): it is
	// stamped finalized and reaped, so a later boot Scan cannot re-recover it.
	if n, err := f.mon.ReapFinalized(context.Background(), 0, 0); err != nil {
		t.Fatalf("ReapFinalized: %v", err)
	} else if n != 1 {
		t.Fatalf("fused executor must be MarkFinalized (reapable), reaped=%d want 1", n)
	}
}
