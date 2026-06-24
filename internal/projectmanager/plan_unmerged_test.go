package projectmanager

import "testing"

// nodeView is a tiny helper to build a PlanNodeView for the projection tests.
func nodeView(id string, ns NodeStatus) PlanNodeView {
	return PlanNodeView{TaskID: TaskID(id), NodeStatus: ns}
}

func TestCycleNodeRole_IsValid(t *testing.T) {
	for _, r := range []CycleNodeRole{
		CycleRoleS0, CycleRoleDev, CycleRoleReview, CycleRoleIntegrate,
		CycleRoleGate, CycleRoleAccept, CycleRoleShip,
	} {
		if !r.IsValid() {
			t.Errorf("role %q should be valid", r)
		}
	}
	for _, r := range []CycleNodeRole{"", "Integrate", "merge", "unknown"} {
		if r.IsValid() {
			t.Errorf("role %q should be invalid", r)
		}
	}
}

func TestUnmergedIntegrations(t *testing.T) {
	// A small cycle: S0, one feature chain Dev→Review→Integrate, plus a second
	// feature whose Integrate is still blocked, and a third whose Integrate is done.
	view := PlanView{Nodes: []PlanNodeView{
		nodeView("s0", NodeDone),
		nodeView("f1-dev", NodeDone),
		nodeView("f1-review", NodeDone),
		nodeView("f1-int", NodeRunning), // unmerged: still running
		nodeView("f2-dev", NodeDone),
		nodeView("f2-review", NodeDispatched),
		nodeView("f2-int", NodeBlocked), // unmerged: blocked upstream
		nodeView("f3-int", NodeDone),    // merged: dropped from the board
		nodeView("gate", NodeBlocked),
	}}
	meta := map[TaskID]CycleNodeMeta{
		"s0":        {Role: CycleRoleS0, Branch: "dev/v2.13.0", Base: "main"},
		"f1-dev":    {Role: CycleRoleDev, Branch: "f1", Base: "dev/v2.13.0"},
		"f1-review": {Role: CycleRoleReview, Branch: "f1", Base: "dev/v2.13.0"},
		"f1-int":    {Role: CycleRoleIntegrate, Branch: "f1-spec", Base: "dev/v2.13.0"},
		"f2-dev":    {Role: CycleRoleDev, Branch: "f2", Base: "dev/v2.13.0"},
		"f2-review": {Role: CycleRoleReview, Branch: "f2", Base: "dev/v2.13.0"},
		"f2-int":    {Role: CycleRoleIntegrate, Branch: "f2-scaffold", Base: "dev/v2.13.0", SkipMergeCheck: true},
		"f3-int":    {Role: CycleRoleIntegrate, Branch: "f3", Base: "dev/v2.13.0"},
		"gate":      {Role: CycleRoleGate},
	}

	got := UnmergedIntegrations(view, meta)
	if len(got) != 2 {
		t.Fatalf("want 2 unmerged integrate nodes, got %d: %+v", len(got), got)
	}
	// Order follows the view's node order: f1-int then f2-int.
	if got[0].TaskID != "f1-int" || got[0].NodeStatus != NodeRunning || got[0].Branch != "f1-spec" {
		t.Errorf("row0 = %+v, want f1-int/running/f1-spec", got[0])
	}
	if got[1].TaskID != "f2-int" || got[1].NodeStatus != NodeBlocked || !got[1].SkipMergeCheck {
		t.Errorf("row1 = %+v, want f2-int/blocked/skip=true", got[1])
	}
}

func TestUnmergedIntegrations_AllMergedIsEmpty(t *testing.T) {
	view := PlanView{Nodes: []PlanNodeView{
		nodeView("a-int", NodeDone),
		nodeView("b-int", NodeDone),
	}}
	meta := map[TaskID]CycleNodeMeta{
		"a-int": {Role: CycleRoleIntegrate},
		"b-int": {Role: CycleRoleIntegrate},
	}
	got := UnmergedIntegrations(view, meta)
	if len(got) != 0 {
		t.Fatalf("all Integrate nodes done ⇒ empty board, got %+v", got)
	}
	// PURE/non-nil contract: always a (possibly empty) slice, never nil — the HTTP
	// layer serializes it as [] not null.
	if got == nil {
		t.Error("UnmergedIntegrations must return a non-nil slice")
	}
}

func TestUnmergedIntegrations_NoMetaSkipsNode(t *testing.T) {
	// Nodes with no metadata (non-scaffolded plan, or F2's port not wired) are not
	// Integrate nodes and must be skipped — empty list, never a false positive.
	view := PlanView{Nodes: []PlanNodeView{
		nodeView("x", NodeRunning),
		nodeView("y", NodeBlocked),
	}}
	if got := UnmergedIntegrations(view, nil); len(got) != 0 {
		t.Fatalf("nil meta ⇒ empty board, got %+v", got)
	}
	if got := UnmergedIntegrations(view, map[TaskID]CycleNodeMeta{}); len(got) != 0 {
		t.Fatalf("empty meta ⇒ empty board, got %+v", got)
	}
}
