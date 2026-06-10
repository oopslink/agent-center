package projectmanager

import "testing"

func dep(from, to TaskID) Dependency {
	return Dependency{PlanID: "PL-1", FromTaskID: from, ToTaskID: to}
}

func TestValidateNoCycle_Empty(t *testing.T) {
	if err := ValidateNoCycle(nil); err != nil {
		t.Fatalf("empty edge set must be acyclic, got %v", err)
	}
}

func TestValidateNoCycle_SimpleChainOK(t *testing.T) {
	// a→b→c (acyclic).
	edges := []Dependency{dep("a", "b"), dep("b", "c")}
	if err := ValidateNoCycle(edges); err != nil {
		t.Fatalf("a→b→c must be acyclic, got %v", err)
	}
}

func TestValidateNoCycle_DirectCycle(t *testing.T) {
	// a→b→c→a (cycle).
	edges := []Dependency{dep("a", "b"), dep("b", "c"), dep("c", "a")}
	if err := ValidateNoCycle(edges); err != ErrPlanCycle {
		t.Fatalf("a→b→c→a must be rejected, got %v", err)
	}
}

func TestValidateNoCycle_TwoNodeCycle(t *testing.T) {
	edges := []Dependency{dep("a", "b"), dep("b", "a")}
	if err := ValidateNoCycle(edges); err != ErrPlanCycle {
		t.Fatalf("a→b→a must be rejected, got %v", err)
	}
}

func TestValidateNoCycle_DiamondOK(t *testing.T) {
	// Diamond: a→b, a→c, b→d, c→d (acyclic, shared downstream node d).
	edges := []Dependency{dep("a", "b"), dep("a", "c"), dep("b", "d"), dep("c", "d")}
	if err := ValidateNoCycle(edges); err != nil {
		t.Fatalf("diamond must be acyclic, got %v", err)
	}
}

func TestWouldCreateCycle_SelfEdge(t *testing.T) {
	if err := WouldCreateCycle(nil, dep("a", "a")); err != ErrSelfDependency {
		t.Fatalf("self-edge must be rejected with ErrSelfDependency, got %v", err)
	}
}

func TestWouldCreateCycle_CreatesCycle(t *testing.T) {
	// existing a→b→c; adding c→a closes a cycle.
	existing := []Dependency{dep("a", "b"), dep("b", "c")}
	if err := WouldCreateCycle(existing, dep("c", "a")); err != ErrPlanCycle {
		t.Fatalf("adding c→a must create a cycle, got %v", err)
	}
}

func TestWouldCreateCycle_Acceptable(t *testing.T) {
	// existing a→b; adding b→c keeps it acyclic.
	existing := []Dependency{dep("a", "b")}
	if err := WouldCreateCycle(existing, dep("b", "c")); err != nil {
		t.Fatalf("adding b→c must be accepted, got %v", err)
	}
	// Diamond closure: existing a→b, a→c, b→d; add c→d → still acyclic.
	existing = []Dependency{dep("a", "b"), dep("a", "c"), dep("b", "d")}
	if err := WouldCreateCycle(existing, dep("c", "d")); err != nil {
		t.Fatalf("diamond closure c→d must be accepted, got %v", err)
	}
}
