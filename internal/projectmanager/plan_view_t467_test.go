package projectmanager

import "testing"

// T467 (issue-f5067ad2): an UNTAKEN escape/fallback node that was DISCARDED (as cleanup)
// must converge to NodeSkipped and NOT pollute has_failed — while a REAL failure (a
// discarded node on a LIVE/taken branch, or a discarded dev node) stays NodeFailed.

// The cycle shape shared by the T467 cases: Dev→Review→Dec, Dec --pass--> Integ,
// Dec --reject_exhausted--> Esc, Dec --loopback(reject)--> Dev.
func t467CycleEdges() []Dependency {
	return []Dependency{
		{PlanID: "pl", FromTaskID: "Dev", ToTaskID: "S0", Kind: EdgeSeq},
		{PlanID: "pl", FromTaskID: "Review", ToTaskID: "Dev", Kind: EdgeSeq},
		{PlanID: "pl", FromTaskID: "Dec", ToTaskID: "Review", Kind: EdgeSeq},
		{PlanID: "pl", FromTaskID: "Integ", ToTaskID: "Dec", Kind: EdgeConditional, When: "pass"},
		{PlanID: "pl", FromTaskID: "Esc", ToTaskID: "Dec", Kind: EdgeConditional, When: "reject_exhausted"},
		{PlanID: "pl", FromTaskID: "Dec", ToTaskID: "Dev", Kind: EdgeLoopback, When: "reject", MaxRounds: 3},
	}
}

// pass route + the (never-taken) escape node was DISCARDED → it must read as SKIPPED,
// has_failed stays false, and the plan reaches AllDone (success terminal).
func TestComputePlanView_DiscardedUntakenEscape_IsSkippedNotFailed_T467(t *testing.T) {
	tasks := []*Task{
		newTaskWithStatus(t, "S0", TaskCompleted),
		newTaskWithStatus(t, "Dev", TaskCompleted),
		newTaskWithStatus(t, "Review", TaskCompleted),
		newTaskWithStatus(t, "Dec", TaskCompleted),
		newTaskWithStatus(t, "Integ", TaskCompleted), // pass branch ran + done
		newTaskWithStatus(t, "Esc", TaskDiscarded),   // untaken escape, discarded as cleanup
	}
	v := ComputePlanView(tasks, t467CycleEdges(), nil,
		[]DecisionOutcome{{PlanID: "pl", TaskID: "Dec", Outcome: "pass"}}, nil)
	st := nodeStatusByID(v)
	if st["Esc"] != NodeSkipped {
		t.Fatalf("discarded untaken escape = %s, want skipped (T467)", st["Esc"])
	}
	if v.HasFailed {
		t.Fatalf("has_failed must be false — the only non-done node is an untaken (skipped) branch")
	}
	if !v.AllDone {
		t.Fatalf("AllDone = false, want true (done + skipped == settled). statuses=%v", st)
	}
}

// reject_exhausted route (escape TAKEN) + the escape node was then DISCARDED → a REAL
// failure: it stays NodeFailed and sets has_failed. The pass-branch Integrate that was
// not taken is the one that skips.
func TestComputePlanView_DiscardedTakenEscape_StaysFailed_T467(t *testing.T) {
	tasks := []*Task{
		newTaskWithStatus(t, "S0", TaskCompleted),
		newTaskWithStatus(t, "Dev", TaskCompleted),
		newTaskWithStatus(t, "Review", TaskCompleted),
		newTaskWithStatus(t, "Dec", TaskCompleted),
		newTaskWithStatus(t, "Integ", TaskOpen),    // pass branch NOT taken
		newTaskWithStatus(t, "Esc", TaskDiscarded), // escape WAS taken, then discarded → real fail
	}
	v := ComputePlanView(tasks, t467CycleEdges(), nil,
		[]DecisionOutcome{{PlanID: "pl", TaskID: "Dec", Outcome: "reject_exhausted"}}, nil)
	st := nodeStatusByID(v)
	if st["Esc"] != NodeFailed {
		t.Fatalf("discarded TAKEN escape = %s, want failed (real failure preserved)", st["Esc"])
	}
	if !v.HasFailed {
		t.Fatalf("has_failed must be true — the taken escape branch really failed")
	}
	if st["Integ"] != NodeSkipped {
		t.Fatalf("untaken pass branch Integ = %s, want skipped", st["Integ"])
	}
}

// A DISCARDED node on a plain (non-conditional) path is a real failure — pruning never
// reaches it, so it stays NodeFailed (a dev node that truly failed).
func TestComputePlanView_DiscardedDevNode_StaysFailed_T467(t *testing.T) {
	tasks := []*Task{
		newTaskWithStatus(t, "S0", TaskCompleted),
		newTaskWithStatus(t, "Dev", TaskDiscarded), // dev truly failed
	}
	edges := []Dependency{{PlanID: "pl", FromTaskID: "Dev", ToTaskID: "S0", Kind: EdgeSeq}}
	v := ComputePlanView(tasks, edges, nil, nil, nil)
	if nodeStatusByID(v)["Dev"] != NodeFailed {
		t.Fatalf("discarded dev node must stay failed (no conditional → never pruned)")
	}
	if !v.HasFailed {
		t.Fatalf("has_failed must be true for a real dev failure")
	}
}

// A RUNNING node on the escape branch tells the truth (NodeRunning), never hidden behind
// `skipped` — a loopback can still re-decide the decision and re-activate it.
func TestComputePlanView_RunningEscape_NotSkipped_T467(t *testing.T) {
	tasks := []*Task{
		newTaskWithStatus(t, "S0", TaskCompleted),
		newTaskWithStatus(t, "Dev", TaskCompleted),
		newTaskWithStatus(t, "Review", TaskCompleted),
		newTaskWithStatus(t, "Dec", TaskCompleted),
		newTaskWithStatus(t, "Integ", TaskCompleted),
		newTaskWithStatus(t, "Esc", TaskRunning), // somehow running on the dead branch
	}
	v := ComputePlanView(tasks, t467CycleEdges(), nil,
		[]DecisionOutcome{{PlanID: "pl", TaskID: "Dec", Outcome: "pass"}}, nil)
	if got := nodeStatusByID(v)["Esc"]; got != NodeRunning {
		t.Fatalf("running escape = %s, want running (never hidden behind skipped)", got)
	}
}

// During a bounded loopback that has NOT yet resolved, the decision is re-opened
// (not done) so the escape node is NOT prematurely skipped — it stays blocked/pending.
func TestComputePlanView_ActiveLoopback_EscapeNotPrematurelySkipped_T467(t *testing.T) {
	tasks := []*Task{
		newTaskWithStatus(t, "S0", TaskCompleted),
		newTaskWithStatus(t, "Dev", TaskRunning), // re-activated by a reject loopback round
		newTaskWithStatus(t, "Review", TaskOpen),
		newTaskWithStatus(t, "Dec", TaskReopened), // re-decided this round → not done
		newTaskWithStatus(t, "Integ", TaskOpen),
		newTaskWithStatus(t, "Esc", TaskOpen),
	}
	// No (stale) outcome recorded for Dec this round.
	v := ComputePlanView(tasks, t467CycleEdges(), nil, nil, nil)
	st := nodeStatusByID(v)
	if st["Esc"] == NodeSkipped {
		t.Fatalf("escape must NOT be skipped while the loopback is unresolved (decision not done)")
	}
	if st["Esc"] != NodeBlocked {
		t.Fatalf("escape = %s, want blocked (decision pending)", st["Esc"])
	}
}
