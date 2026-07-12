package orchestration_test

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
	orchsqlite "github.com/oopslink/agent-center/internal/projectmanager/orchestration/sqlite"
)

var t0 = time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)

func setup(t *testing.T) (context.Context, *orch.Service) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	clk := clock.NewFakeClock(t0)
	gen := idgen.NewGenerator(clk)
	svc := orch.NewService(orch.ServiceDeps{
		DB:     db,
		Graphs: orchsqlite.NewGraphRepo(db),
		Nodes:  orchsqlite.NewNodeRepo(db),
		Edges:  orchsqlite.NewEdgeRepo(db),
		IDGen:  gen,
		Clock:  clk,
	})
	return context.Background(), svc
}

// TestService_CreateGraph_GetGraph verifies that CreateGraph persists the graph
// header and auto-creates start + end control nodes which are returned by
// GetGraph.
func TestService_CreateGraph_GetGraph(t *testing.T) {
	ctx, svc := setup(t)

	id, err := svc.CreateGraph(ctx, "plan-1")
	if err != nil {
		t.Fatalf("CreateGraph: %v", err)
	}
	if id == "" {
		t.Fatal("CreateGraph returned empty id")
	}

	g, err := svc.GetGraph(ctx, id)
	if err != nil {
		t.Fatalf("GetGraph: %v", err)
	}

	if g.PlanID() != "plan-1" {
		t.Errorf("PlanID = %q, want plan-1", g.PlanID())
	}
	if g.Status() != orch.GraphDraft {
		t.Errorf("Status = %s, want draft", g.Status())
	}

	nodes := g.Nodes()
	if len(nodes) != 2 {
		t.Fatalf("want 2 auto-created nodes (start+end), got %d", len(nodes))
	}

	// Verify start and end control nodes exist.
	startID := g.StartNodeID()
	endID := g.EndNodeID()
	if startID == "" || endID == "" {
		t.Fatal("start or end node ID is empty")
	}
	startNode := g.FindNode(startID)
	endNode := g.FindNode(endID)
	if startNode == nil {
		t.Fatal("start node not found in graph")
	}
	if endNode == nil {
		t.Fatal("end node not found in graph")
	}
	if startNode.ControlKind() != orch.ControlKindStart {
		t.Errorf("start node controlKind = %s", startNode.ControlKind())
	}
	if endNode.ControlKind() != orch.ControlKindEnd {
		t.Errorf("end node controlKind = %s", endNode.ControlKind())
	}
}

// TestService_AddNode_StartNode_CompleteNode verifies adding a business node
// and walking it through open → running → completed.
func TestService_AddNode_StartNode_CompleteNode(t *testing.T) {
	ctx, svc := setup(t)

	gID, err := svc.CreateGraph(ctx, "plan-2")
	if err != nil {
		t.Fatalf("CreateGraph: %v", err)
	}

	nID, err := svc.AddNode(ctx, gID, "business", "", "Dev task", map[string]any{"branch": "feat-1"})
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	// Verify it starts as open.
	n, err := svc.GetNode(ctx, nID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if n.Status() != orch.NodeOpen {
		t.Errorf("initial status = %s, want open", n.Status())
	}
	if n.Metadata()["branch"] != "feat-1" {
		t.Error("metadata not preserved")
	}

	// Start it.
	if err := svc.StartNode(ctx, nID); err != nil {
		t.Fatalf("StartNode: %v", err)
	}
	n, _ = svc.GetNode(ctx, nID)
	if n.Status() != orch.NodeRunning {
		t.Errorf("after StartNode: status = %s, want running", n.Status())
	}

	// Complete it.
	if err := svc.CompleteNode(ctx, nID, "success"); err != nil {
		t.Fatalf("CompleteNode: %v", err)
	}
	n, _ = svc.GetNode(ctx, nID)
	if n.Status() != orch.NodeCompleted {
		t.Errorf("after CompleteNode: status = %s, want completed", n.Status())
	}
	if n.Outcome() != "success" {
		t.Errorf("outcome = %s, want success", n.Outcome())
	}
	if len(n.ActionLogs()) != 2 {
		t.Errorf("action logs = %d, want 2 (started + completed)", len(n.ActionLogs()))
	}
}

// TestService_AddEdge_ReadyNodes verifies that edges are persisted and that
// GetReadyNodes honours upstream-completion semantics.
func TestService_AddEdge_ReadyNodes(t *testing.T) {
	ctx, svc := setup(t)

	gID, _ := svc.CreateGraph(ctx, "plan-3")

	n1ID, err := svc.AddNode(ctx, gID, "business", "", "Task A", nil)
	if err != nil {
		t.Fatalf("AddNode A: %v", err)
	}
	n2ID, err := svc.AddNode(ctx, gID, "business", "", "Task B", nil)
	if err != nil {
		t.Fatalf("AddNode B: %v", err)
	}

	// Add edge: n1 → n2 (B depends on A).
	if err := svc.AddEdge(ctx, gID, n1ID, n2ID); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	// Initially only n1 (no upstream) should be ready.
	ready, err := svc.GetReadyNodes(ctx, gID)
	if err != nil {
		t.Fatalf("GetReadyNodes: %v", err)
	}
	if len(ready) != 1 || ready[0].ID() != n1ID {
		t.Errorf("ready nodes = %v, want [%s]", nodeIDs(ready), n1ID)
	}

	// Complete n1 → n2 should become ready.
	svc.StartNode(ctx, n1ID)
	svc.CompleteNode(ctx, n1ID, "success")

	ready2, err := svc.GetReadyNodes(ctx, gID)
	if err != nil {
		t.Fatalf("GetReadyNodes after complete: %v", err)
	}
	if len(ready2) != 1 || ready2[0].ID() != n2ID {
		t.Errorf("ready nodes after completing n1 = %v, want [%s]", nodeIDs(ready2), n2ID)
	}
}

// TestService_ResolveCondition verifies that a condition node routes success
// to on_success targets (marking the condition as completed) and that the
// graph state is persisted.
func TestService_ResolveCondition(t *testing.T) {
	ctx, svc := setup(t)

	gID, _ := svc.CreateGraph(ctx, "plan-4")

	// Add upstream business node.
	upstreamID, _ := svc.AddNode(ctx, gID, "business", "", "Review", nil)

	// Add condition node with on_success pointing to nothing (just verify routing).
	condMeta := map[string]any{
		"evaluator":  "manual",
		"on_success": []any{},
		"on_failure": []any{},
	}
	condID, err := svc.AddNode(ctx, gID, "control", "condition", "Gate", condMeta)
	if err != nil {
		t.Fatalf("AddNode condition: %v", err)
	}

	// Wire upstream → condition.
	if err := svc.AddEdge(ctx, gID, upstreamID, condID); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	// Complete the upstream node so condition can evaluate.
	svc.StartNode(ctx, upstreamID)
	svc.CompleteNode(ctx, upstreamID, "success")

	// Resolve condition as success.
	if err := svc.ResolveCondition(ctx, condID, "success"); err != nil {
		t.Fatalf("ResolveCondition: %v", err)
	}

	// The condition node should now be completed with outcome "success".
	condNode, err := svc.GetNode(ctx, condID)
	if err != nil {
		t.Fatalf("GetNode condition: %v", err)
	}
	if condNode.Status() != orch.NodeCompleted {
		t.Errorf("condition status = %s, want completed", condNode.Status())
	}
	if condNode.Outcome() != "success" {
		t.Errorf("condition outcome = %s, want success", condNode.Outcome())
	}
}

// TestService_BindTask verifies that BindTask stores task_id in metadata and
// UnbindTask removes it.
func TestService_BindTask(t *testing.T) {
	ctx, svc := setup(t)

	gID, _ := svc.CreateGraph(ctx, "plan-5")
	nID, _ := svc.AddNode(ctx, gID, "business", "", "Work item", nil)

	// Bind a task.
	if err := svc.BindTask(ctx, nID, "task-abc"); err != nil {
		t.Fatalf("BindTask: %v", err)
	}
	n, _ := svc.GetNode(ctx, nID)
	if n.Metadata()["task_id"] != "task-abc" {
		t.Errorf("task_id = %v, want task-abc", n.Metadata()["task_id"])
	}

	// Unbind the task.
	if err := svc.UnbindTask(ctx, nID); err != nil {
		t.Fatalf("UnbindTask: %v", err)
	}
	n2, _ := svc.GetNode(ctx, nID)
	if _, ok := n2.Metadata()["task_id"]; ok {
		t.Error("task_id still present after UnbindTask")
	}
}

// nodeIDs is a helper that extracts IDs for error messages.
func nodeIDs(nodes []*orch.Node) []orch.NodeID {
	ids := make([]orch.NodeID, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID()
	}
	return ids
}
