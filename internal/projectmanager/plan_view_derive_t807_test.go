package projectmanager

import (
	"testing"
	"time"
)

// T807 ④ — DerivePlanView is the graph-era read-view derivation the readers switched
// to (get_plan detail / list enrich / builtin pool). These tests pin its 8 derived
// node statuses for a RUNNING-shaped plan (done/failed/running/paused/blocked/ready/
// dispatched/skipped), a DRAFT-shaped plan, and a FLAT pool. (T810 ⑤: the old
// ComputePlanView shell was deleted — DerivePlanView is the single derivation, so the
// prior DeepEqual-vs-ComputePlanView parity assertions were removed.)

// (nodeStatusByID lives in plan_controlflow_test.go — reused here.)

// TestDerivePlanView_AllEightStates exercises every derived node status in ONE plan
// and asserts each node's status.
func TestDerivePlanView_AllEightStates(t *testing.T) {
	// A(done, root) → D(done decision, outcome=pass), Y(ready), Z(dispatched).
	// R(running, root) → B(blocked). P(paused). F(failed, root).
	// Sreject(conditional on D When=reject; D chose pass → skipped).
	a := newTaskWithStatus(t, "A", TaskCompleted)
	d := newTaskWithStatus(t, "D", TaskCompleted)
	r := newTaskWithStatus(t, "R", TaskRunning)
	p := newTaskWithStatus(t, "P", TaskRunning)
	f := newTaskWithStatus(t, "F", TaskDiscarded)
	y := newTaskWithStatus(t, "Y", TaskOpen)
	z := newTaskWithStatus(t, "Z", TaskOpen)
	b := newTaskWithStatus(t, "B", TaskOpen)
	sRej := newTaskWithStatus(t, "Sreject", TaskOpen)
	tasks := []*Task{a, d, r, p, f, y, z, b, sRej}

	edges := []Dependency{
		{PlanID: "pl", FromTaskID: "D", ToTaskID: "A"},                                              // D depends_on A (seq)
		{PlanID: "pl", FromTaskID: "Y", ToTaskID: "A"},                                              // Y depends_on A (seq)
		{PlanID: "pl", FromTaskID: "Z", ToTaskID: "A"},                                              // Z depends_on A (seq)
		{PlanID: "pl", FromTaskID: "B", ToTaskID: "R"},                                              // B depends_on R (running → blocked)
		{PlanID: "pl", FromTaskID: "Sreject", ToTaskID: "D", Kind: EdgeConditional, When: "reject"}, // dead branch
	}
	records := []DispatchRecord{{PlanID: "pl", TaskID: "Z", DispatchedAt: time.Now(), DispatchMessageID: "m1"}}
	outcomes := []DecisionOutcome{{PlanID: "pl", TaskID: "D", Outcome: "pass"}}
	paused := map[TaskID]bool{"P": true}

	view := DerivePlanView(tasks, edges, records, outcomes, paused)

	want := map[TaskID]NodeStatus{
		"A": NodeDone, "D": NodeDone, "R": NodeRunning, "P": NodePaused, "F": NodeFailed,
		"Y": NodeReady, "Z": NodeDispatched, "B": NodeBlocked, "Sreject": NodeSkipped,
	}
	got := nodeStatusByID(view)
	for id, w := range want {
		if got[id] != w {
			t.Errorf("node %s = %s, want %s", id, got[id], w)
		}
	}
	// Progress / has_failed / all_done.
	if view.Progress.Done != 2 || view.Progress.Total != 9 {
		t.Errorf("progress = %+v, want {2 9}", view.Progress)
	}
	if !view.HasFailed {
		t.Error("HasFailed = false, want true (F discarded on a live branch)")
	}
	if view.AllDone {
		t.Error("AllDone = true, want false")
	}
	// ReadySet is exactly the `ready` nodes (Y) — Z is dispatched, not ready.
	if len(view.ReadySet) != 1 || view.ReadySet[0] != "Y" {
		t.Errorf("ReadySet = %v, want [Y]", view.ReadySet)
	}

}

// TestDerivePlanView_DraftPlan pins the draft-shaped derivation (option (c)): a plan
// with no dispatch records and no decision outcomes (never started) derives roots to
// `ready` and their dependents to `blocked` — no graph needed.
func TestDerivePlanView_DraftPlan(t *testing.T) {
	root1 := newTaskWithStatus(t, "R1", TaskOpen)
	root2 := newTaskWithStatus(t, "R2", TaskOpen)
	child := newTaskWithStatus(t, "C", TaskOpen)
	tasks := []*Task{root1, root2, child}
	edges := []Dependency{{PlanID: "pl", FromTaskID: "C", ToTaskID: "R1"}}

	view := DerivePlanView(tasks, edges, nil, nil, nil)
	got := nodeStatusByID(view)
	if got["R1"] != NodeReady || got["R2"] != NodeReady {
		t.Errorf("draft roots = R1:%s R2:%s, want both ready", got["R1"], got["R2"])
	}
	if got["C"] != NodeBlocked {
		t.Errorf("draft child = %s, want blocked (upstream R1 not done)", got["C"])
	}
	if view.AllDone || view.HasFailed || view.Progress.Done != 0 {
		t.Errorf("draft view = allDone:%v hasFailed:%v done:%d, want false/false/0", view.AllDone, view.HasFailed, view.Progress.Done)
	}
}

// TestDerivePlanView_FlatPool pins the builtin-pool equivalent read: a FLAT plan (no
// edges, no decisions) derives each in-plan open task to `ready` (undispatched) or
// `dispatched` (has a record) — the pool ready-set with zero conditional gating.
func TestDerivePlanView_FlatPool(t *testing.T) {
	m1 := newTaskWithStatus(t, "M1", TaskOpen) // ready
	m2 := newTaskWithStatus(t, "M2", TaskOpen) // dispatched
	m3 := newTaskWithStatus(t, "M3", TaskCompleted)
	tasks := []*Task{m1, m2, m3}
	records := []DispatchRecord{{PlanID: "pool", TaskID: "M2", DispatchedAt: time.Now()}}

	view := DerivePlanView(tasks, nil, records, nil, nil)
	got := nodeStatusByID(view)
	if got["M1"] != NodeReady || got["M2"] != NodeDispatched || got["M3"] != NodeDone {
		t.Errorf("pool = M1:%s M2:%s M3:%s, want ready/dispatched/done", got["M1"], got["M2"], got["M3"])
	}
	// The pool dispatch loop consumes ReadySet — only the undispatched member is in it.
	if len(view.ReadySet) != 1 || view.ReadySet[0] != "M1" {
		t.Errorf("pool ReadySet = %v, want [M1]", view.ReadySet)
	}
}
