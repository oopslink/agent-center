package agent

import "testing"

func newWI(t *testing.T) *AgentWorkItem {
	t.Helper()
	w, err := NewWorkItem(NewWorkItemInput{ID: "WI1", AgentID: "A1", TaskRef: "pm://tasks/T1", CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestNewWorkItem_Requires(t *testing.T) {
	if _, err := NewWorkItem(NewWorkItemInput{ID: "WI1", AgentID: "A1", CreatedAt: t0}); err != ErrWorkItemTaskRequired {
		t.Fatalf("want ErrWorkItemTaskRequired, got %v", err)
	}
	if _, err := NewWorkItem(NewWorkItemInput{ID: "WI1", TaskRef: "t", CreatedAt: t0}); err != ErrWorkItemAgentRequired {
		t.Fatalf("want ErrWorkItemAgentRequired, got %v", err)
	}
}

// TestWorkItemWaitWakeLoop covers the multi-interaction loop (plan §2.4/OQ5):
// queued→active→waiting_input→active, with the interaction count bumping on
// each (re)activation.
func TestWorkItemWaitWakeLoop(t *testing.T) {
	w := newWI(t)
	if w.Status() != WorkItemQueued || w.Interactions() != 0 {
		t.Fatal("new work item queued, 0 interactions")
	}
	if err := w.Activate(t0); err != nil {
		t.Fatal(err)
	}
	if w.Status() != WorkItemActive || w.Interactions() != 1 {
		t.Fatalf("activate → active, 1 interaction; got %s/%d", w.Status(), w.Interactions())
	}
	if err := w.WaitInput(t0); err != nil {
		t.Fatal(err)
	}
	if err := w.Wake(t0); err != nil {
		t.Fatal(err)
	}
	if w.Status() != WorkItemActive || w.Interactions() != 2 {
		t.Fatalf("wake → active, 2 interactions; got %s/%d", w.Status(), w.Interactions())
	}
	if err := w.Done(t0); err != nil {
		t.Fatal(err)
	}
	if !w.Status().IsTerminal() {
		t.Fatal("done is terminal")
	}
	// terminal rejects further transitions
	if err := w.Activate(t0); err != ErrWorkItemIllegalMove {
		t.Fatalf("terminal activate want illegal, got %v", err)
	}
}

// TestWorkItemNoBlockedStatus verifies the §10 OQ11 simplification: there is no
// `blocked` WorkItem status. An active WorkItem can only go to waiting_input,
// done, failed, canceled, or superseded — "blocked" is a Task concept (a
// blocked Task cancels its live WorkItem).
func TestWorkItemNoBlockedStatus(t *testing.T) {
	if WorkItemStatus("blocked").IsValid() {
		t.Fatal("'blocked' must not be a valid WorkItem status")
	}
	w := newWI(t)
	_ = w.Activate(t0)
	// active → canceled is how a blocked/canceled Task ends the attempt.
	if err := w.Cancel(t0); err != nil {
		t.Fatalf("active→canceled should be legal: %v", err)
	}
	if !w.Status().IsTerminal() {
		t.Fatal("canceled is terminal")
	}
}

func TestWorkItemActiveCanFail(t *testing.T) {
	w := newWI(t)
	_ = w.Activate(t0)
	if err := w.Fail(t0); err != nil {
		t.Fatalf("active→failed should be legal: %v", err)
	}
	if w.Status() != WorkItemFailed {
		t.Fatalf("want failed, got %s", w.Status())
	}
}

func TestWorkItemSupersedeOnReassign(t *testing.T) {
	// supersede is legal from queued/active/waiting_input/blocked
	for _, drive := range []func(*AgentWorkItem){
		func(w *AgentWorkItem) {},                                          // queued
		func(w *AgentWorkItem) { _ = w.Activate(t0) },                      // active
		func(w *AgentWorkItem) { _ = w.Activate(t0); _ = w.WaitInput(t0) }, // waiting_input
	} {
		w := newWI(t)
		drive(w)
		if err := w.Supersede(t0); err != nil {
			t.Fatalf("supersede from %s should be legal: %v", w.Status(), err)
		}
		if w.Status() != WorkItemSuperseded {
			t.Fatalf("want superseded, got %s", w.Status())
		}
	}
}

func TestWorkItem_FailAndCancel(t *testing.T) {
	w := newWI(t)
	_ = w.Activate(t0)
	if err := w.Fail(t0); err != nil {
		t.Fatal(err)
	}
	if w.Status() != WorkItemFailed || !w.Status().IsTerminal() {
		t.Fatal("failed terminal")
	}
	w2 := newWI(t)
	if err := w2.Cancel(t0); err != nil { // queued→canceled
		t.Fatal(err)
	}
}

// TestWorkItem_FailFromAgentDeath pins the v2.7 GATE-7 Mode-B cascade edge + its
// STRUCTURAL guard: FailFromAgentDeath is the ONLY path that may move
// waiting_input→failed; the general Fail()/transition map stays restricted, so the
// terminal edge is reachable only via the agent-death cascade by construction.
func TestWorkItem_FailFromAgentDeath(t *testing.T) {
	// active → failed.
	a := newWI(t)
	_ = a.Activate(t0)
	if err := a.FailFromAgentDeath(t0); err != nil {
		t.Fatalf("active→FailFromAgentDeath: %v", err)
	}
	if a.Status() != WorkItemFailed {
		t.Fatalf("want failed, got %s", a.Status())
	}

	// waiting_input: the GENERAL path must STILL reject →failed (structural guard).
	b := newWI(t)
	_ = b.Activate(t0)
	_ = b.WaitInput(t0)
	if b.Status() != WorkItemWaitingInput {
		t.Fatal("setup: want waiting_input")
	}
	if WorkItemWaitingInput.CanTransitionTo(WorkItemFailed) {
		t.Fatal("transition map must NOT globally allow waiting_input→failed")
	}
	if err := b.Fail(t0); err != ErrWorkItemIllegalMove {
		t.Fatalf("general Fail() from waiting_input must stay illegal, got %v", err)
	}
	if b.Status() != WorkItemWaitingInput {
		t.Fatal("rejected Fail() must not mutate status")
	}
	// ...but FailFromAgentDeath DOES move waiting_input→failed (the dedicated edge).
	if err := b.FailFromAgentDeath(t0); err != nil {
		t.Fatalf("waiting_input→FailFromAgentDeath: %v", err)
	}
	if b.Status() != WorkItemFailed {
		t.Fatalf("want failed, got %s", b.Status())
	}

	// queued (not in flight) → illegal.
	c := newWI(t)
	if err := c.FailFromAgentDeath(t0); err != ErrWorkItemIllegalMove {
		t.Fatalf("queued FailFromAgentDeath want illegal, got %v", err)
	}

	// already terminal → no-op (idempotent), status unchanged.
	d := newWI(t)
	_ = d.Activate(t0)
	_ = d.Done(t0)
	if err := d.FailFromAgentDeath(t0); err != nil {
		t.Fatalf("terminal FailFromAgentDeath must be a no-op, got %v", err)
	}
	if d.Status() != WorkItemDone {
		t.Fatal("terminal status must be unchanged")
	}
}

func TestRehydrateWorkItem_BadStatus(t *testing.T) {
	if _, err := RehydrateWorkItem(RehydrateWorkItemInput{Status: "bad", Version: 1}); err != ErrWorkItemBadStatus {
		t.Fatalf("want ErrWorkItemBadStatus, got %v", err)
	}
}

func TestActivityEvent_New(t *testing.T) {
	e, err := NewActivityEvent(NewActivityEventInput{ID: "E1", AgentID: "A1", WorkItemRef: "WI1", InteractionRef: "i1", EventType: "tool_use", OccurredAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	if e.ID() != "E1" || e.AgentID() != "A1" || e.WorkItemRef() != "WI1" || e.InteractionRef() != "i1" ||
		e.EventType() != "tool_use" || e.Payload() != "{}" || !e.OccurredAt().Equal(t0) {
		t.Fatalf("activity event getters wrong: %+v", e)
	}
	if _, err := NewActivityEvent(NewActivityEventInput{ID: "E2", AgentID: "A1", OccurredAt: t0}); err == nil {
		t.Fatal("missing event type should fail")
	}
}
