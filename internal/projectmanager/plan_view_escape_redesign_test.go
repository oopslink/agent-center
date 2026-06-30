package projectmanager

import "testing"

// v2.23.0 escape-redesign (A, issue-624bfb53) plan-view shape: a NEW-scaffold cycle
// has NO Escape vertex — Decision's only conditional out-edge is pass→Integrate, plus
// the reject loopback. When the loopback exhausts, the engine records the Decision's
// terminal `reject_exhausted` outcome. The feature's Integrate (When=pass) is then a
// DEAD branch → NodeSkipped, and has_failed must stay FALSE (the exhausted loop is
// awaiting a human ruling, not a failure — design Q1). The plan settles (AllDone).
func newScaffoldCycleEdges() []Dependency {
	return []Dependency{
		{PlanID: "pl", FromTaskID: "Dev", ToTaskID: "S0", Kind: EdgeSeq},
		{PlanID: "pl", FromTaskID: "Review", ToTaskID: "Dev", Kind: EdgeSeq},
		{PlanID: "pl", FromTaskID: "Dec", ToTaskID: "Review", Kind: EdgeSeq},
		{PlanID: "pl", FromTaskID: "Integ", ToTaskID: "Dec", Kind: EdgeConditional, When: "pass"},
		{PlanID: "pl", FromTaskID: "Dec", ToTaskID: "Dev", Kind: EdgeLoopback, When: "reject", MaxRounds: 3},
		// NO {Esc -reject_exhausted-> Dec} edge — escape is no longer a vertex.
	}
}

func TestComputePlanView_ExhaustedNewScaffold_IntegrateSkips_NoFailure(t *testing.T) {
	tasks := []*Task{
		newTaskWithStatus(t, "S0", TaskCompleted),
		newTaskWithStatus(t, "Dev", TaskCompleted),
		newTaskWithStatus(t, "Review", TaskCompleted),
		newTaskWithStatus(t, "Dec", TaskCompleted), // exhausted → reject_exhausted terminal
		newTaskWithStatus(t, "Integ", TaskOpen),    // pass branch never taken
	}
	v := ComputePlanView(tasks, newScaffoldCycleEdges(), nil,
		[]DecisionOutcome{{PlanID: "pl", TaskID: "Dec", Outcome: "reject_exhausted"}}, nil)
	st := nodeStatusByID(v)

	if st["Integ"] != NodeSkipped {
		t.Fatalf("Integrate on exhausted (reject_exhausted) decision = %s, want skipped (dead pass-branch)", st["Integ"])
	}
	if v.HasFailed {
		t.Fatalf("has_failed must be FALSE on exhaustion — an exhausted loop awaiting a human ruling is not a failure (Q1). statuses=%v", st)
	}
	if !v.AllDone {
		t.Fatalf("AllDone = false, want true (done + skipped == settled). statuses=%v", st)
	}
}
