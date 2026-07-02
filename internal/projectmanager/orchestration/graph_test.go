package orchestration

import (
	"testing"
)

func TestNewGraph_AutoStartEnd(t *testing.T) {
	g, err := NewGraph(NewGraphInput{
		ID:        "g1",
		PlanID:    "plan-1",
		CreatedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if g.Status() != GraphDraft {
		t.Fatalf("status = %s, want draft", g.Status())
	}
	nodes := g.Nodes()
	if len(nodes) != 2 {
		t.Fatalf("nodes = %d, want 2 (start + end)", len(nodes))
	}
	var hasStart, hasEnd bool
	for _, n := range nodes {
		if n.ControlKind() == ControlKindStart {
			hasStart = true
		}
		if n.ControlKind() == ControlKindEnd {
			hasEnd = true
		}
	}
	if !hasStart || !hasEnd {
		t.Fatal("missing start or end node")
	}
}

func TestGraph_AddNode_AddEdge(t *testing.T) {
	g, _ := NewGraph(NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
	startID := g.StartNodeID()
	endID := g.EndNodeID()

	dev, err := g.AddNode(NewNodeInput{
		ID: "dev-1", GraphID: "g1", Category: NodeCategoryBusiness, Title: "Dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	if dev.Status() != NodeOpen {
		t.Fatalf("new node status = %s, want open", dev.Status())
	}

	// start -> dev -> end
	if err := g.AddEdge(startID, dev.ID()); err != nil {
		t.Fatal(err)
	}
	if err := g.AddEdge(dev.ID(), endID); err != nil {
		t.Fatal(err)
	}
	if len(g.Edges()) != 2 {
		t.Fatalf("edges = %d, want 2", len(g.Edges()))
	}
}

func TestGraph_AddEdge_CycleRejected(t *testing.T) {
	g, _ := NewGraph(NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
	g.AddNode(NewNodeInput{ID: "a", GraphID: "g1", Category: NodeCategoryBusiness, Title: "A"})
	g.AddNode(NewNodeInput{ID: "b", GraphID: "g1", Category: NodeCategoryBusiness, Title: "B"})
	g.AddEdge("a", "b")
	if err := g.AddEdge("b", "a"); err != ErrCycleDetected {
		t.Fatalf("want ErrCycleDetected, got %v", err)
	}
}

func TestGraph_RemoveNode(t *testing.T) {
	g, _ := NewGraph(NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
	g.AddNode(NewNodeInput{ID: "a", GraphID: "g1", Category: NodeCategoryBusiness, Title: "A"})

	if err := g.RemoveNode("a"); err != nil {
		t.Fatal(err)
	}
	if g.FindNode("a") != nil {
		t.Fatal("node should be removed")
	}
}

func TestGraph_RemoveNode_RunningBlocked(t *testing.T) {
	g, _ := NewGraph(NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
	g.AddNode(NewNodeInput{ID: "a", GraphID: "g1", Category: NodeCategoryBusiness, Title: "A"})
	n := g.FindNode("a")
	n.Start(t0)

	if err := g.RemoveNode("a"); err != ErrNodeNotRemovable {
		t.Fatalf("want ErrNodeNotRemovable, got %v", err)
	}
}

func TestGraph_RemoveEdge(t *testing.T) {
	g, _ := NewGraph(NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
	g.AddNode(NewNodeInput{ID: "a", GraphID: "g1", Category: NodeCategoryBusiness, Title: "A"})
	g.AddNode(NewNodeInput{ID: "b", GraphID: "g1", Category: NodeCategoryBusiness, Title: "B"})
	g.AddEdge("a", "b")

	if err := g.RemoveEdge("a", "b"); err != nil {
		t.Fatal(err)
	}
	if len(g.Edges()) != 0 {
		t.Fatalf("edges = %d, want 0", len(g.Edges()))
	}
}

func TestGraph_ReadyNodes(t *testing.T) {
	g, _ := NewGraph(NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
	startID := g.StartNodeID()

	g.AddNode(NewNodeInput{ID: "a", GraphID: "g1", Category: NodeCategoryBusiness, Title: "A"})
	g.AddNode(NewNodeInput{ID: "b", GraphID: "g1", Category: NodeCategoryBusiness, Title: "B"})
	g.AddEdge(startID, "a")
	g.AddEdge("a", "b")

	// No need to complete the start control node: ReadyNodes treats control nodes as
	// passthrough-satisfied regardless of their status, so "a" is immediately ready.
	ready := g.ReadyNodes()
	if len(ready) != 1 || ready[0].ID() != "a" {
		t.Fatalf("ready = %v, want [a]", nodeIDs(ready))
	}

	// Complete a -> b becomes ready
	g.FindNode("a").Start(t0)
	g.FindNode("a").Complete("success", t0)
	ready = g.ReadyNodes()
	if len(ready) != 1 || ready[0].ID() != "b" {
		t.Fatalf("ready = %v, want [b]", nodeIDs(ready))
	}
}

func TestGraph_StatusTransitions(t *testing.T) {
	g, _ := NewGraph(NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})

	if err := g.Start(t0); err != nil {
		t.Fatal(err)
	}
	if g.Status() != GraphRunning {
		t.Fatalf("status = %s, want running", g.Status())
	}

	// running -> draft (revert)
	if err := g.Revert(t0); err != nil {
		t.Fatal(err)
	}
	if g.Status() != GraphDraft {
		t.Fatalf("status = %s, want draft", g.Status())
	}

	// draft -> archived
	if err := g.Archive(t0); err != nil {
		t.Fatal(err)
	}
	if g.Status() != GraphArchived {
		t.Fatalf("status = %s, want archived", g.Status())
	}
}

func TestGraph_Finish(t *testing.T) {
	g, _ := NewGraph(NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
	g.Start(t0)
	if err := g.Finish(t0); err != nil {
		t.Fatal(err)
	}
	if g.Status() != GraphDone {
		t.Fatalf("status = %s, want done", g.Status())
	}
}

func TestNewGraph_EmptyID(t *testing.T) {
	_, err := NewGraph(NewGraphInput{ID: "", PlanID: "plan-1", CreatedAt: t0})
	if err != ErrMissingRequiredField {
		t.Fatalf("want ErrMissingRequiredField, got %v", err)
	}
}

func TestGraph_RemoveNode_StartEndBlocked(t *testing.T) {
	g, _ := NewGraph(NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})

	if err := g.RemoveNode(g.StartNodeID()); err != ErrNodeNotRemovable {
		t.Fatalf("removing start: want ErrNodeNotRemovable, got %v", err)
	}
	if err := g.RemoveNode(g.EndNodeID()); err != ErrNodeNotRemovable {
		t.Fatalf("removing end: want ErrNodeNotRemovable, got %v", err)
	}
}

func TestGraph_IsAutoDone(t *testing.T) {
	t.Run("empty graph returns false", func(t *testing.T) {
		g, _ := NewGraph(NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
		if g.IsAutoDone() {
			t.Fatal("want false for graph with no business nodes")
		}
	})

	t.Run("all business nodes completed returns true", func(t *testing.T) {
		g, _ := NewGraph(NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
		g.AddNode(NewNodeInput{ID: "a", GraphID: "g1", Category: NodeCategoryBusiness, Title: "A"})
		g.AddNode(NewNodeInput{ID: "b", GraphID: "g1", Category: NodeCategoryBusiness, Title: "B"})
		n := g.FindNode("a")
		n.Start(t0)
		n.Complete("ok", t0)
		m := g.FindNode("b")
		m.Start(t0)
		m.Complete("ok", t0)
		if !g.IsAutoDone() {
			t.Fatal("want true when all business nodes completed")
		}
	})

	t.Run("one running node returns false", func(t *testing.T) {
		g, _ := NewGraph(NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
		g.AddNode(NewNodeInput{ID: "a", GraphID: "g1", Category: NodeCategoryBusiness, Title: "A"})
		g.AddNode(NewNodeInput{ID: "b", GraphID: "g1", Category: NodeCategoryBusiness, Title: "B"})
		n := g.FindNode("a")
		n.Start(t0)
		n.Complete("ok", t0)
		g.FindNode("b").Start(t0) // still running
		if g.IsAutoDone() {
			t.Fatal("want false when a business node is still running")
		}
	})
}

func TestGraph_AddNode_Duplicate(t *testing.T) {
	g, _ := NewGraph(NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
	g.AddNode(NewNodeInput{ID: "a", GraphID: "g1", Category: NodeCategoryBusiness, Title: "A"})
	_, err := g.AddNode(NewNodeInput{ID: "a", GraphID: "g1", Category: NodeCategoryBusiness, Title: "A"})
	if err != ErrNodeExists {
		t.Fatalf("want ErrNodeExists, got %v", err)
	}
}

func nodeIDs(nodes []*Node) []NodeID {
	ids := make([]NodeID, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID()
	}
	return ids
}
