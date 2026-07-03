package executor

import (
	"context"
	"testing"
	"time"
)

// T851 §4.6: the NoFinalize splits classify + (for a stall) kill, but must NOT
// finalize — so the durable dir/worktree survive for the runtime's recover decision,
// and nothing is written back yet.

func TestCheckOrphanNoFinalize_Stalled_KillsButRetainsDir(t *testing.T) {
	f := newOrphanFixture(t, time.Minute)
	id, pid := "exec-nf-stall", 5252
	f.provision(t, id)
	f.live.alive[pid] = true
	if err := f.fx.WriteStatus(Status{ExecutorID: id, State: StateRunning, Model: "m", StartedAt: f.base, LastProgressAt: f.base}); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}
	f.clk.Set(f.base.Add(2 * time.Minute)) // past the stall window

	c, terminal, killed, wasStall, err := f.mon.CheckOrphanNoFinalize(context.Background(), id, pid)
	if err != nil {
		t.Fatalf("CheckOrphanNoFinalize: %v", err)
	}
	if !terminal || !killed || !wasStall {
		t.Fatalf("stalled orphan: got terminal=%v killed=%v wasStall=%v, want all true", terminal, killed, wasStall)
	}
	if c.Kind != OutcomeFailed {
		t.Errorf("stalled Kind = %q, want failed", c.Kind)
	}
	// The stall was killed (mechanism), but NOT finalized: dir retained, no writeback.
	if !dirExists(t, f.fx, id) {
		t.Error("NoFinalize must RETAIN the dir (teardown lives only in Finalize)")
	}
	if len(f.wb.reports) != 0 {
		t.Errorf("NoFinalize must not writeback; got %v", f.wb.kinds())
	}
}

func TestCheckOrphanNoFinalize_Gone_RetainsDirNoWriteback(t *testing.T) {
	f := newOrphanFixture(t, time.Minute)
	id, pid := "exec-nf-gone", 6363
	f.provision(t, id)
	f.live.alive[pid] = false
	if err := f.fx.WriteOutput(Output{ExecutorID: id, Success: true, Result: "done", FinishedAt: f.base}); err != nil {
		t.Fatalf("WriteOutput: %v", err)
	}

	c, terminal, killed, wasStall, err := f.mon.CheckOrphanNoFinalize(context.Background(), id, pid)
	if err != nil {
		t.Fatalf("CheckOrphanNoFinalize: %v", err)
	}
	if !terminal || killed || wasStall {
		t.Fatalf("gone orphan: got terminal=%v killed=%v wasStall=%v, want (true,false,false)", terminal, killed, wasStall)
	}
	if c.Kind != OutcomeSucceeded {
		t.Errorf("gone-with-success Kind = %q, want succeeded", c.Kind)
	}
	if !dirExists(t, f.fx, id) || len(f.wb.reports) != 0 {
		t.Error("NoFinalize must retain dir + not writeback")
	}
}

func TestCheckOrphanNoFinalize_AliveFresh_NotTerminal(t *testing.T) {
	f := newOrphanFixture(t, time.Minute)
	id, pid := "exec-nf-fresh", 4242
	f.provision(t, id)
	f.live.alive[pid] = true
	if err := f.fx.WriteStatus(Status{ExecutorID: id, State: StateRunning, Model: "m", StartedAt: f.base, LastProgressAt: f.base}); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}
	f.clk.Set(f.base.Add(30 * time.Second)) // within the stall window

	c, terminal, killed, _, err := f.mon.CheckOrphanNoFinalize(context.Background(), id, pid)
	if err != nil {
		t.Fatalf("CheckOrphanNoFinalize: %v", err)
	}
	if terminal || killed || c.Kind != OutcomeRunning {
		t.Errorf("fresh orphan: got kind=%s terminal=%v killed=%v, want (running,false,false)", c.Kind, terminal, killed)
	}
}

func TestAwaitCompletionNoFinalize_RetainsDirNoWriteback(t *testing.T) {
	f := newMonitorFixture(t, 3)
	id := "e-nf-fail"
	mustProvision(t, f.fx, id)
	mustWriteStatus(t, f.fx, *failedStatus(id))
	if err := f.pool.Adopt(id); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	h := startRealProc(t, id, 3, f.sigs.signal)

	c, wasStall, err := f.mon.AwaitCompletionNoFinalize(h)
	if err != nil {
		t.Fatalf("AwaitCompletionNoFinalize: %v", err)
	}
	if c.Kind != OutcomeFailed {
		t.Fatalf("Kind = %q, want failed", c.Kind)
	}
	if wasStall {
		t.Error("a non-Sweep-killed exit must report wasStall=false")
	}
	if !dirExists(t, f.fx, id) {
		t.Error("NoFinalize must retain the dir for the runtime's recover decision")
	}
	if len(f.wb.reports) != 0 {
		t.Errorf("NoFinalize must not writeback; got %v", f.wb.kinds())
	}
}

func TestReleaseSlot_FreesSlotIdempotently(t *testing.T) {
	f := newMonitorFixture(t, 2)
	id := "recover-me"
	if _, err := f.pool.Launch(context.Background(), LaunchSpec{Input: validPoolInput(id), RunnerCmd: []string{"x"}}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if f.pool.Active() != 1 {
		t.Fatalf("Active = %d, want 1 after launch", f.pool.Active())
	}
	// The recover branch releases the dead executor's slot without finalize/writeback.
	f.mon.ReleaseSlot(id)
	if f.pool.Active() != 0 {
		t.Errorf("ReleaseSlot must free the slot, Active = %d", f.pool.Active())
	}
	if len(f.wb.reports) != 0 {
		t.Errorf("ReleaseSlot must NOT writeback; got %v", f.wb.kinds())
	}
	// Idempotent: a second release (or on an unknown id) is a harmless no-op.
	f.mon.ReleaseSlot(id)
	f.mon.ReleaseSlot("never-launched")
	if f.pool.Active() != 0 {
		t.Errorf("idempotent release changed Active to %d", f.pool.Active())
	}
}

func TestPeekStalled_NonConsuming_And_ClearStalled(t *testing.T) {
	f := newMonitorFixture(t, 1)
	id := "e-peek"
	f.mon.markStalled(id)

	// peek reports true and does NOT consume — repeated peeks stay true.
	if !f.mon.peekStalled(id) || !f.mon.peekStalled(id) {
		t.Fatal("peekStalled must report true without consuming the mark")
	}
	// AwaitCompletionNoFinalize surfaces wasStall via the same non-consuming peek.
	h := startRealProc(t, id, 0, f.sigs.signal)
	mustProvision(t, f.fx, id)
	if _, wasStall, err := f.mon.AwaitCompletionNoFinalize(h); err != nil || !wasStall {
		t.Fatalf("AwaitCompletionNoFinalize wasStall = %v (err %v), want true", wasStall, err)
	}
	if !f.mon.peekStalled(id) {
		t.Error("wasStall peek must not consume: mark should still be set for a later Finalize")
	}
	// The recover path clears it so the recovered incarnation is not mislabeled.
	f.mon.ClearStalled(id)
	if f.mon.peekStalled(id) {
		t.Error("ClearStalled must drop the mark")
	}
}
