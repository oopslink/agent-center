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

func TestWorkItemBlockedOnlyCancelOrSupersede(t *testing.T) {
	w := newWI(t)
	_ = w.Activate(t0)
	if err := w.Block(t0); err != nil {
		t.Fatal(err)
	}
	// per plan §2.4, blocked has no blocked→active edge
	if err := w.Wake(t0); err != ErrWorkItemIllegalMove {
		t.Fatalf("blocked→active should be illegal, got %v", err)
	}
	if err := w.Cancel(t0); err != nil {
		t.Fatalf("blocked→canceled should be legal: %v", err)
	}
}

func TestWorkItemSupersedeOnReassign(t *testing.T) {
	// supersede is legal from queued/active/waiting_input/blocked
	for _, drive := range []func(*AgentWorkItem){
		func(w *AgentWorkItem) {},                                          // queued
		func(w *AgentWorkItem) { _ = w.Activate(t0) },                      // active
		func(w *AgentWorkItem) { _ = w.Activate(t0); _ = w.WaitInput(t0) }, // waiting_input
		func(w *AgentWorkItem) { _ = w.Activate(t0); _ = w.Block(t0) },     // blocked
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
