package service

import (
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

// ============================================================================
// issue-0186f85e ③ — the zombie-reclaim window is shortened by using task.updated_at
// (real progress) as the liveness signal, so a "假 running 无进展" node is identified
// within minutes instead of waiting out the 5h execution-lease TTL, WITHOUT reversing
// the T456 slow-but-alive invariant (a node whose updated_at keeps advancing is never
// reopened).
// ============================================================================

// TestStuckNode_UpdatedAtStale_ReopenedWithoutLeaseLapse — the FAST PATH: an executor
// process died but its 5h execution lease is still VALID (nowhere near TTL). The only
// death signal is a FROZEN task.updated_at (no worker auto-renew since death). The
// reconcile accrues strikes off the stale updated_at and auto-reopens the node within
// minutes — it does NOT wait for the multi-hour lease to lapse first.
func TestStuckNode_UpdatedAtStale_ReopenedWithoutLeaseLapse(t *testing.T) {
	f := setupStuckNode(t, "agent:zombie")
	clk := f.h.clk

	// Freeze updated_at past T_stale while keeping the lease valid (advance ≪ 5h TTL).
	clk.Advance(StuckNodeProgressStaleTimeout + time.Minute)

	reopened := false
	for i := 0; i < 6; i++ {
		f.sweep(t)
		if f.nodeStatus(t) != orch.NodeRunning {
			reopened = true
			break
		}
		clk.Advance(4 * time.Minute)
	}
	if !reopened {
		t.Fatal("zombie node (frozen updated_at, still-valid lease) not auto-reopened — ③ fast path failed")
	}
	if got := f.nodeStatus(t); got != orch.NodeReopen {
		t.Fatalf("node = %q after fast-path reconcile, want reopen", got)
	}
	// The task never left running (reopen is a graph-node action, not a reclaim).
	tk := f.task(t)
	if tk.Status() != pm.TaskRunning {
		t.Fatalf("task = %q, want still running (T456 anti-orphan invariant)", tk.Status())
	}
	// Crucially: detection did NOT require the lease to lapse — it was still in the future
	// the whole time (proving we shortened the window past the 5h-TTL wait).
	if exp := tk.ExecutionLeaseExpiresAt(); exp == nil || !clk.Now().Before(*exp) {
		t.Fatalf("lease should still be valid (unlapsed) at detection: exp=%v now=%v", exp, clk.Now())
	}
	// The reopened node re-enters the engine ready-set (re-dispatchable).
	if ready := readyNodeTaskIDs(t, f.h, f.orch, f.graphID); !ready[f.tid] {
		t.Fatalf("reopened node not in ready-set: %v", ready)
	}
}

// TestStuckNode_UpdatedAtAdvancing_NotReopened — the T456 guard ON THE FAST PATH: a
// live-but-heads-down agent whose worker keeps auto-renewing (which advances updated_at)
// is NEVER auto-reopened, even though its lease is never lapsed. Every observed
// updated_at advance clears the strike count — the "有真进展" liveness signal.
func TestStuckNode_UpdatedAtAdvancing_NotReopened(t *testing.T) {
	f := setupStuckNode(t, "agent:live")
	clk := f.h.clk

	// Enter tracking once via a stale window (no renewal yet), then keep the worker
	// attesting the process is alive every 4 min — updated_at advances → activity → clear.
	clk.Advance(StuckNodeProgressStaleTimeout + time.Minute)
	f.sweep(t) // bootstrap entry (stale updated_at), no strike

	for i := 0; i < 8; i++ {
		clk.Advance(4 * time.Minute)
		if revoked, _, err := f.h.svc.WorkerRenewLease(f.h.ctx, f.tid, f.assignee); err != nil || revoked {
			t.Fatalf("WorkerRenewLease: revoked=%v err=%v (want a live renew)", revoked, err)
		}
		f.sweep(t)
		if got := f.nodeStatus(t); got != orch.NodeRunning {
			t.Fatalf("live agent (updated_at advancing) reopened to %q at step %d — T456 invariant violated", got, i)
		}
	}
	if tk := f.task(t); tk.Status() != pm.TaskRunning || tk.BlockedReason() != "" {
		t.Fatalf("live task disturbed: status=%q blocked=%q", tk.Status(), tk.BlockedReason())
	}
}

// TestProgressStale_Boundaries — the T_stale predicate: zero time is never stale (no
// baseline), a freeze exactly at T_stale is stale, just under is not.
func TestProgressStale_Boundaries(t *testing.T) {
	now := time.Unix(1700000000, 0)
	if progressStale(time.Time{}, now) {
		t.Error("zero updatedAt must not be considered stale")
	}
	if !progressStale(now.Add(-StuckNodeProgressStaleTimeout), now) {
		t.Error("a freeze of exactly T_stale must be stale")
	}
	if progressStale(now.Add(-StuckNodeProgressStaleTimeout+time.Second), now) {
		t.Error("a freeze just under T_stale must not be stale")
	}
}
