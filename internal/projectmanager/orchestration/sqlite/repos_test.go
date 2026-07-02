package sqlite

import (
	"context"
	"testing"
	"time"

	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
	"github.com/oopslink/agent-center/internal/persistence"
)

var t0 = time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)

func setup(t *testing.T) (context.Context, *GraphRepo, *NodeRepo, *EdgeRepo) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return context.Background(), NewGraphRepo(db), NewNodeRepo(db), NewEdgeRepo(db)
}

func TestGraphRepo_RoundTrip(t *testing.T) {
	ctx, gr, _, _ := setup(t)

	g, _ := orch.NewGraph(orch.NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
	if err := gr.Save(ctx, g); err != nil {
		t.Fatal(err)
	}

	// Duplicate
	if err := gr.Save(ctx, g); err != orch.ErrGraphExists {
		t.Fatalf("dup save: want ErrGraphExists, got %v", err)
	}

	// FindByID
	got, err := gr.FindByID(ctx, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if got.PlanID() != "plan-1" || got.Status() != orch.GraphDraft {
		t.Fatalf("FindByID: plan=%s status=%s", got.PlanID(), got.Status())
	}

	// FindByPlanID
	got2, err := gr.FindByPlanID(ctx, "plan-1")
	if err != nil {
		t.Fatal(err)
	}
	if got2.ID() != "g1" {
		t.Fatalf("FindByPlanID: id=%s", got2.ID())
	}

	// Update
	g.Start(t0)
	if err := gr.Update(ctx, g); err != nil {
		t.Fatal(err)
	}
	got3, _ := gr.FindByID(ctx, "g1")
	if got3.Status() != orch.GraphRunning {
		t.Fatalf("after update: status=%s, want running", got3.Status())
	}

	// Not found
	if _, err := gr.FindByID(ctx, "nope"); err != orch.ErrGraphNotFound {
		t.Fatalf("want ErrGraphNotFound, got %v", err)
	}

	// Delete
	if err := gr.Delete(ctx, "g1"); err != nil {
		t.Fatal(err)
	}
	if _, err := gr.FindByID(ctx, "g1"); err != orch.ErrGraphNotFound {
		t.Fatalf("after delete: want ErrGraphNotFound, got %v", err)
	}
}

func TestNodeRepo_RoundTrip(t *testing.T) {
	ctx, gr, nr, _ := setup(t)

	g, _ := orch.NewGraph(orch.NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
	gr.Save(ctx, g)

	n, _ := orch.NewNode(orch.NewNodeInput{
		ID: "n1", GraphID: "g1", Category: orch.NodeCategoryBusiness,
		Title: "Dev task", Metadata: map[string]any{"branch": "feat-1"}, CreatedAt: t0,
	})
	if err := nr.Save(ctx, n); err != nil {
		t.Fatal(err)
	}

	got, err := nr.FindByID(ctx, "n1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title() != "Dev task" || got.Category() != orch.NodeCategoryBusiness {
		t.Fatalf("FindByID: title=%s cat=%s", got.Title(), got.Category())
	}
	if got.Metadata()["branch"] != "feat-1" {
		t.Fatal("metadata not preserved")
	}

	// Update with status change
	n.Start(t0)
	n.Complete("success", t0)
	if err := nr.Update(ctx, n); err != nil {
		t.Fatal(err)
	}
	got2, _ := nr.FindByID(ctx, "n1")
	if got2.Status() != orch.NodeCompleted || got2.Outcome() != "success" {
		t.Fatalf("after update: status=%s outcome=%s", got2.Status(), got2.Outcome())
	}
	if len(got2.ActionLogs()) != 2 {
		t.Fatalf("action logs = %d, want 2", len(got2.ActionLogs()))
	}

	// ListByGraph
	list, err := nr.ListByGraph(ctx, "g1")
	if err != nil {
		t.Fatal(err)
	}
	// g1 has auto start+end + n1 = 3 nodes total, but we only saved n1 to node repo
	if len(list) != 1 {
		t.Fatalf("ListByGraph = %d, want 1", len(list))
	}
}

func TestEdgeRepo_RoundTrip(t *testing.T) {
	ctx, gr, nr, er := setup(t)

	g, _ := orch.NewGraph(orch.NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
	gr.Save(ctx, g)

	// Save start and end nodes so FK constraints are satisfied
	for _, n := range g.Nodes() {
		nr.Save(ctx, n)
	}

	n1, _ := orch.NewNode(orch.NewNodeInput{ID: "n1", GraphID: "g1", Category: orch.NodeCategoryBusiness, Title: "A", CreatedAt: t0})
	n2, _ := orch.NewNode(orch.NewNodeInput{ID: "n2", GraphID: "g1", Category: orch.NodeCategoryBusiness, Title: "B", CreatedAt: t0})
	nr.Save(ctx, n1)
	nr.Save(ctx, n2)

	edge := orch.Edge{FromNodeID: "n1", ToNodeID: "n2"}
	if err := er.Save(ctx, "g1", edge); err != nil {
		t.Fatal(err)
	}

	edges, err := er.ListByGraph(ctx, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 || edges[0].FromNodeID != "n1" || edges[0].ToNodeID != "n2" {
		t.Fatalf("ListByGraph = %v", edges)
	}

	// Delete
	if err := er.Delete(ctx, "g1", "n1", "n2"); err != nil {
		t.Fatal(err)
	}
	edges2, _ := er.ListByGraph(ctx, "g1")
	if len(edges2) != 0 {
		t.Fatalf("after delete: edges = %d", len(edges2))
	}
}
