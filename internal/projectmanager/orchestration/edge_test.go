package orchestration

import "testing"

func TestValidateNoCycle_SelfEdge(t *testing.T) {
	err := ValidateNoCycle(nil, Edge{FromNodeID: "a", ToNodeID: "a"})
	if err != ErrSelfEdge {
		t.Fatalf("want ErrSelfEdge, got %v", err)
	}
}

func TestValidateNoCycle_SimpleChain(t *testing.T) {
	// a -> b -> c — adding c -> a creates a cycle
	existing := []Edge{
		{FromNodeID: "a", ToNodeID: "b"},
		{FromNodeID: "b", ToNodeID: "c"},
	}
	if err := ValidateNoCycle(existing, Edge{FromNodeID: "c", ToNodeID: "a"}); err != ErrCycleDetected {
		t.Fatalf("want ErrCycleDetected, got %v", err)
	}
}

func TestValidateNoCycle_NoCycle(t *testing.T) {
	existing := []Edge{
		{FromNodeID: "a", ToNodeID: "b"},
		{FromNodeID: "b", ToNodeID: "c"},
	}
	// a -> c is fine (shortcut, no cycle)
	if err := ValidateNoCycle(existing, Edge{FromNodeID: "a", ToNodeID: "c"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateNoCycle_Diamond(t *testing.T) {
	// a -> b, a -> c, b -> d, c -> d — adding d -> a creates cycle
	existing := []Edge{
		{FromNodeID: "a", ToNodeID: "b"},
		{FromNodeID: "a", ToNodeID: "c"},
		{FromNodeID: "b", ToNodeID: "d"},
		{FromNodeID: "c", ToNodeID: "d"},
	}
	if err := ValidateNoCycle(existing, Edge{FromNodeID: "d", ToNodeID: "a"}); err != ErrCycleDetected {
		t.Fatalf("want ErrCycleDetected, got %v", err)
	}
	// d -> e is fine
	if err := ValidateNoCycle(existing, Edge{FromNodeID: "d", ToNodeID: "e"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateNoCycle_EmptyGraph(t *testing.T) {
	if err := ValidateNoCycle(nil, Edge{FromNodeID: "a", ToNodeID: "b"}); err != nil {
		t.Fatalf("unexpected error on empty graph: %v", err)
	}
}

func TestReopenChain_LinearPath(t *testing.T) {
	// a -> b -> c -> d. Reopen from d back to target a: should return [c, b, a]
	edges := []Edge{
		{FromNodeID: "a", ToNodeID: "b"},
		{FromNodeID: "b", ToNodeID: "c"},
		{FromNodeID: "c", ToNodeID: "d"},
	}
	chain := ReopenChain(edges, "d", "a")
	expected := []NodeID{"c", "b", "a"}
	if len(chain) != len(expected) {
		t.Fatalf("chain = %v, want %v", chain, expected)
	}
	for i, id := range expected {
		if chain[i] != id {
			t.Fatalf("chain[%d] = %s, want %s", i, chain[i], id)
		}
	}
}

func TestReopenChain_BranchingPath(t *testing.T) {
	// a -> b -> d, a -> c -> d. Reopen from d to a: should include b, c, a (order may vary but all present)
	edges := []Edge{
		{FromNodeID: "a", ToNodeID: "b"},
		{FromNodeID: "a", ToNodeID: "c"},
		{FromNodeID: "b", ToNodeID: "d"},
		{FromNodeID: "c", ToNodeID: "d"},
	}
	chain := ReopenChain(edges, "d", "a")
	found := map[NodeID]bool{}
	for _, id := range chain {
		found[id] = true
	}
	for _, want := range []NodeID{"a", "b", "c"} {
		if !found[want] {
			t.Fatalf("chain %v missing %s", chain, want)
		}
	}
	if found["d"] {
		t.Fatal("chain should not include the source node d")
	}
}

func TestReopenChain_NoPath(t *testing.T) {
	// a -> b, c -> d. Reopen from d to a: no path, empty chain
	edges := []Edge{
		{FromNodeID: "a", ToNodeID: "b"},
		{FromNodeID: "c", ToNodeID: "d"},
	}
	chain := ReopenChain(edges, "d", "a")
	if len(chain) != 0 {
		t.Fatalf("chain = %v, want empty", chain)
	}
}
