package projectmanager

import "testing"

// v2.13.0 I18/B1 control-flow engine domain tests (docs/design/v2.13.0/
// control-flow-engine-spec.md). Cover conditional routing + dead-branch pruning
// (NodeSkipped), AllDone with skipped, loopback exclusion from forward readiness,
// cycle/loopback validation, LoopbackResetSet, and pure-DAG backward compatibility.

func nodeStatusByID(v PlanView) map[TaskID]NodeStatus {
	m := map[TaskID]NodeStatus{}
	for _, n := range v.Nodes {
		m[n.TaskID] = n.NodeStatus
	}
	return m
}

// A decision routes by outcome: the matching conditional branch becomes ready, the
// non-matching branch (and its downstream) is SKIPPED; the plan still reaches AllDone.
func TestComputePlanView_ConditionalRouting(t *testing.T) {
	// D(decision, done, outcome=pass) → pass:Integrate, reject:Rework→Rework2.
	d := newTaskWithStatus(t, "D", TaskCompleted)
	integ := newTaskWithStatus(t, "I", TaskOpen)
	rework := newTaskWithStatus(t, "R", TaskOpen)
	rework2 := newTaskWithStatus(t, "R2", TaskOpen)
	tasks := []*Task{d, integ, rework, rework2}
	edges := []Dependency{
		{PlanID: "pl", FromTaskID: "I", ToTaskID: "D", Kind: EdgeConditional, When: "pass"},
		{PlanID: "pl", FromTaskID: "R", ToTaskID: "D", Kind: EdgeConditional, When: "reject"},
		{PlanID: "pl", FromTaskID: "R2", ToTaskID: "R", Kind: EdgeSeq}, // downstream of the dead branch
	}
	outcomes := []DecisionOutcome{{PlanID: "pl", TaskID: "D", Outcome: "pass"}}

	v := ComputePlanView(tasks, edges, nil, outcomes, nil)
	st := nodeStatusByID(v)
	if st["I"] != NodeReady {
		t.Fatalf("pass-branch I = %s, want ready", st["I"])
	}
	if st["R"] != NodeSkipped {
		t.Fatalf("not-taken branch R = %s, want skipped", st["R"])
	}
	if st["R2"] != NodeSkipped {
		t.Fatalf("downstream of skipped R2 = %s, want skipped (transitive prune)", st["R2"])
	}
	if len(v.ReadySet) != 1 || v.ReadySet[0] != "I" {
		t.Fatalf("ready-set = %v, want [I]", v.ReadySet)
	}
}

// A pending (not-done) decision keeps its conditional downstream BLOCKED (not ready,
// not skipped) until the decision resolves.
func TestComputePlanView_PendingDecisionBlocks(t *testing.T) {
	d := newTaskWithStatus(t, "D", TaskRunning) // decision not done yet
	integ := newTaskWithStatus(t, "I", TaskOpen)
	tasks := []*Task{d, integ}
	edges := []Dependency{{PlanID: "pl", FromTaskID: "I", ToTaskID: "D", Kind: EdgeConditional, When: "pass"}}
	v := ComputePlanView(tasks, edges, nil, nil, nil)
	st := nodeStatusByID(v)
	if st["I"] != NodeBlocked {
		t.Fatalf("I = %s, want blocked (decision pending)", st["I"])
	}
}

// AllDone counts SKIPPED as settled: done pass-branch + skipped reject-branch == done.
func TestComputePlanView_AllDoneWithSkipped(t *testing.T) {
	d := newTaskWithStatus(t, "D", TaskCompleted)
	integ := newTaskWithStatus(t, "I", TaskCompleted) // pass branch ran + done
	rework := newTaskWithStatus(t, "R", TaskOpen)     // reject branch, never taken
	tasks := []*Task{d, integ, rework}
	edges := []Dependency{
		{PlanID: "pl", FromTaskID: "I", ToTaskID: "D", Kind: EdgeConditional, When: "pass"},
		{PlanID: "pl", FromTaskID: "R", ToTaskID: "D", Kind: EdgeConditional, When: "reject"},
	}
	outcomes := []DecisionOutcome{{PlanID: "pl", TaskID: "D", Outcome: "pass"}}
	v := ComputePlanView(tasks, edges, nil, outcomes, nil)
	if !v.AllDone {
		t.Fatalf("AllDone = false, want true (done + skipped == settled). statuses=%v", nodeStatusByID(v))
	}
}

// A loopback edge does NOT gate forward readiness: Dev (the loop target) stays ready
// off its real upstream even though a later decision has a loopback edge to it.
func TestComputePlanView_LoopbackNotForwardBlocking(t *testing.T) {
	s0 := newTaskWithStatus(t, "S0", TaskCompleted)
	dev := newTaskWithStatus(t, "Dev", TaskOpen)
	dec := newTaskWithStatus(t, "Dec", TaskOpen)
	tasks := []*Task{s0, dev, dec}
	edges := []Dependency{
		{PlanID: "pl", FromTaskID: "Dev", ToTaskID: "S0", Kind: EdgeSeq},
		{PlanID: "pl", FromTaskID: "Dec", ToTaskID: "Dev", Kind: EdgeSeq},
		// loopback Dec → Dev (back-edge). Must NOT make Dev depend on Dec.
		{PlanID: "pl", FromTaskID: "Dec", ToTaskID: "Dev", Kind: EdgeLoopback, When: "reject", MaxRounds: 3},
	}
	v := ComputePlanView(tasks, edges, nil, nil, nil)
	st := nodeStatusByID(v)
	if st["Dev"] != NodeReady {
		t.Fatalf("Dev = %s, want ready (loopback must not gate forward readiness)", st["Dev"])
	}
}

// Cycle validation excludes loopback edges (the back-edge is intentional); a
// seq/conditional cycle is still rejected.
func TestValidateNoCycle_ExcludesLoopback(t *testing.T) {
	// A→B→A where the back-edge is a loopback: acyclic over the forward graph → OK.
	loopOK := []Dependency{
		{PlanID: "pl", FromTaskID: "B", ToTaskID: "A", Kind: EdgeSeq},
		{PlanID: "pl", FromTaskID: "A", ToTaskID: "B", Kind: EdgeLoopback, When: "reject", MaxRounds: 2},
	}
	if err := ValidateNoCycle(loopOK); err != nil {
		t.Fatalf("loopback back-edge should be excluded from cycle check, got %v", err)
	}
	// Same shape but both seq → a real forward cycle → rejected.
	seqCycle := []Dependency{
		{PlanID: "pl", FromTaskID: "B", ToTaskID: "A", Kind: EdgeSeq},
		{PlanID: "pl", FromTaskID: "A", ToTaskID: "B", Kind: EdgeSeq},
	}
	if err := ValidateNoCycle(seqCycle); err != ErrPlanCycle {
		t.Fatalf("seq cycle err = %v, want ErrPlanCycle", err)
	}
}

// WouldCreateCycle: a valid loopback (To is a forward ancestor of From) passes; a
// loopback to a non-ancestor, or missing When/MaxRounds, is rejected.
func TestWouldCreateCycle_LoopbackValidity(t *testing.T) {
	existing := []Dependency{
		{PlanID: "pl", FromTaskID: "Review", ToTaskID: "Dev", Kind: EdgeSeq},
		{PlanID: "pl", FromTaskID: "Dec", ToTaskID: "Review", Kind: EdgeSeq},
	}
	good := Dependency{PlanID: "pl", FromTaskID: "Dec", ToTaskID: "Dev", Kind: EdgeLoopback, When: "reject", MaxRounds: 3}
	if err := WouldCreateCycle(existing, good); err != nil {
		t.Fatalf("valid loopback (Dev is ancestor of Dec) rejected: %v", err)
	}
	noWhen := Dependency{PlanID: "pl", FromTaskID: "Dec", ToTaskID: "Dev", Kind: EdgeLoopback, MaxRounds: 3}
	if err := WouldCreateCycle(existing, noWhen); err != ErrInvalidLoopback {
		t.Fatalf("loopback without When err = %v, want ErrInvalidLoopback", err)
	}
	noBound := Dependency{PlanID: "pl", FromTaskID: "Dec", ToTaskID: "Dev", Kind: EdgeLoopback, When: "reject", MaxRounds: 0}
	if err := WouldCreateCycle(existing, noBound); err != ErrInvalidLoopback {
		t.Fatalf("loopback without MaxRounds err = %v, want ErrInvalidLoopback", err)
	}
	notAncestor := Dependency{PlanID: "pl", FromTaskID: "Dec", ToTaskID: "Elsewhere", Kind: EdgeLoopback, When: "reject", MaxRounds: 3}
	if err := WouldCreateCycle(existing, notAncestor); err != ErrInvalidLoopback {
		t.Fatalf("loopback to non-ancestor err = %v, want ErrInvalidLoopback", err)
	}
}

// LoopbackResetSet returns exactly the Dev→Review→Decision chain (inclusive).
func TestLoopbackResetSet(t *testing.T) {
	edges := []Dependency{
		{PlanID: "pl", FromTaskID: "Review", ToTaskID: "Dev", Kind: EdgeSeq},
		{PlanID: "pl", FromTaskID: "Dec", ToTaskID: "Review", Kind: EdgeSeq},
		{PlanID: "pl", FromTaskID: "Dec", ToTaskID: "Dev", Kind: EdgeLoopback, When: "reject", MaxRounds: 3},
		{PlanID: "pl", FromTaskID: "S0", ToTaskID: "Other", Kind: EdgeSeq}, // unrelated
	}
	set := LoopbackResetSet(edges, "Dev", "Dec")
	got := map[TaskID]bool{}
	for _, id := range set {
		got[id] = true
	}
	for _, want := range []TaskID{"Dev", "Review", "Dec"} {
		if !got[want] {
			t.Fatalf("reset set %v missing %s", set, want)
		}
	}
	if len(set) != 3 {
		t.Fatalf("reset set = %v, want exactly {Dev,Review,Dec}", set)
	}
}

// Backward compatibility: a pure seq DAG with NO edge kinds / NO outcomes derives
// identically to the pre-B1 engine — no node is ever skipped, AllDone == all done.
func TestComputePlanView_PureDAGBackCompat(t *testing.T) {
	a := newTaskWithStatus(t, "A", TaskCompleted)
	b := newTaskWithStatus(t, "B", TaskOpen)
	c := newTaskWithStatus(t, "C", TaskCompleted)
	tasks := []*Task{a, b, c}
	edges := []Dependency{{PlanID: "pl", FromTaskID: "B", ToTaskID: "A"}} // zero-value kind == seq
	v := ComputePlanView(tasks, edges, nil, nil, nil)
	st := nodeStatusByID(v)
	if st["B"] != NodeReady {
		t.Fatalf("B = %s, want ready (A done)", st["B"])
	}
	for _, n := range v.Nodes {
		if n.NodeStatus == NodeSkipped {
			t.Fatalf("no node should be skipped in a pure DAG, got %s skipped", n.TaskID)
		}
	}
}
