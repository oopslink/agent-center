package agent

import "testing"

// The AR records every status change as a pending WorkItemTransition (locus B:
// the repository drains these on persist and emits agent.work_item_transitioned
// in the same tx). prev_status + version are captured at transition time so the
// consumer never has to re-read the prior row.

func TestWorkItem_NewRecordsQueuedTransition(t *testing.T) {
	w := newWI(t)
	ts := w.DrainTransitions()
	if len(ts) != 1 {
		t.Fatalf("new work item should record 1 (creation) transition, got %d", len(ts))
	}
	got := ts[0]
	if got.PrevStatus != "" || got.Status != WorkItemQueued {
		t.Fatalf("creation transition want \"\"→queued, got %q→%q", got.PrevStatus, got.Status)
	}
	if got.Version != 1 {
		t.Fatalf("creation transition version want 1, got %d", got.Version)
	}
	if got.WorkItemID != "WI1" || got.AgentID != "A1" || got.TaskRef != "pm://tasks/T1" {
		t.Fatalf("creation transition identity wrong: %+v", got)
	}
	if !got.OccurredAt.Equal(t0.UTC()) {
		t.Fatalf("creation transition occurred_at want %v, got %v", t0.UTC(), got.OccurredAt)
	}
}

func TestWorkItem_MoveRecordsTransitionWithPrevAndVersion(t *testing.T) {
	w := newWI(t)
	_ = w.DrainTransitions() // discard creation
	if err := w.Activate(t0); err != nil {
		t.Fatal(err)
	}
	ts := w.DrainTransitions()
	if len(ts) != 1 {
		t.Fatalf("activate should record 1 transition, got %d", len(ts))
	}
	got := ts[0]
	if got.PrevStatus != WorkItemQueued || got.Status != WorkItemActive {
		t.Fatalf("activate transition want queued→active, got %q→%q", got.PrevStatus, got.Status)
	}
	if got.Version != 2 {
		t.Fatalf("activate transition version want 2 (bumped), got %d", got.Version)
	}
}

func TestWorkItem_DrainClears(t *testing.T) {
	w := newWI(t)
	if len(w.DrainTransitions()) != 1 {
		t.Fatal("first drain should return the creation transition")
	}
	if got := w.DrainTransitions(); len(got) != 0 {
		t.Fatalf("second drain should be empty (drain clears), got %d", len(got))
	}
}

func TestWorkItem_MultipleTransitionsAccumulateInOrder(t *testing.T) {
	w := newWI(t)
	_ = w.DrainTransitions()
	if err := w.Activate(t0); err != nil {
		t.Fatal(err)
	}
	if err := w.Done(t0); err != nil {
		t.Fatal(err)
	}
	ts := w.DrainTransitions()
	if len(ts) != 2 {
		t.Fatalf("want 2 accumulated transitions, got %d", len(ts))
	}
	if ts[0].Status != WorkItemActive || ts[1].Status != WorkItemDone {
		t.Fatalf("order wrong: %q then %q", ts[0].Status, ts[1].Status)
	}
	if ts[0].PrevStatus != WorkItemQueued || ts[1].PrevStatus != WorkItemActive {
		t.Fatalf("prev wrong: %q, %q", ts[0].PrevStatus, ts[1].PrevStatus)
	}
}

func TestWorkItem_FailFromAgentDeathRecordsTransition(t *testing.T) {
	w := newWI(t)
	_ = w.Activate(t0)
	_ = w.DrainTransitions()
	if err := w.FailFromAgentDeath(t0); err != nil {
		t.Fatal(err)
	}
	ts := w.DrainTransitions()
	if len(ts) != 1 {
		t.Fatalf("FailFromAgentDeath should record 1 transition, got %d", len(ts))
	}
	if ts[0].PrevStatus != WorkItemActive || ts[0].Status != WorkItemFailed {
		t.Fatalf("want active→failed, got %q→%q", ts[0].PrevStatus, ts[0].Status)
	}
}

func TestWorkItem_FailFromAgentDeathTerminalNoopRecordsNothing(t *testing.T) {
	w := newWI(t)
	_ = w.Activate(t0)
	_ = w.Done(t0)
	_ = w.DrainTransitions()
	if err := w.FailFromAgentDeath(t0); err != nil {
		t.Fatalf("terminal FailFromAgentDeath must be a no-op, got %v", err)
	}
	if got := w.DrainTransitions(); len(got) != 0 {
		t.Fatalf("no-op FailFromAgentDeath must record nothing, got %d", len(got))
	}
}

func TestWorkItem_RehydrateHasNoPendingTransitions(t *testing.T) {
	w, err := RehydrateWorkItem(RehydrateWorkItemInput{
		ID: "WI1", AgentID: "A1", TaskRef: "pm://tasks/T1",
		Status: WorkItemActive, Interactions: 1, CreatedAt: t0, UpdatedAt: t0, Version: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := w.DrainTransitions(); len(got) != 0 {
		t.Fatalf("rehydrated AR must have no pending transitions (no spurious emit on load), got %d", len(got))
	}
}

func TestWorkItem_IllegalMoveRecordsNothing(t *testing.T) {
	w := newWI(t)
	_ = w.DrainTransitions()
	// queued→done is illegal (must go through active).
	if err := w.Done(t0); err != ErrWorkItemIllegalMove {
		t.Fatalf("want ErrWorkItemIllegalMove, got %v", err)
	}
	if got := w.DrainTransitions(); len(got) != 0 {
		t.Fatalf("illegal move must record no transition, got %d", len(got))
	}
}
