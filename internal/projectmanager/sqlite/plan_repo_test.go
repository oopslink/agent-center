package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// planSetup spins up an in-memory DB with all migrations applied and returns a
// PlanRepo + TaskRepo (mirrors repos_test.go's setup harness).
func planSetup(t *testing.T) (context.Context, *PlanRepo, *TaskRepo) {
	t.Helper()
	d, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(d).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return context.Background(), NewPlanRepo(d), NewTaskRepo(d)
}

func newPlanFixture(id pm.PlanID, project pm.ProjectID) *pm.Plan {
	p, _ := pm.NewPlan(pm.NewPlanInput{
		ID: id, ProjectID: project, Name: "v3.0", Description: "goal",
		CreatorRef: "user:alice", CreatedAt: t0,
	})
	return p
}

func TestPlanRepo_RoundTrip(t *testing.T) {
	ctx, pr, _ := planSetup(t)
	p := newPlanFixture("PL-1", "P-1")
	if err := pr.Save(ctx, p); err != nil {
		t.Fatal(err)
	}
	// duplicate
	if err := pr.Save(ctx, p); err != pm.ErrPlanExists {
		t.Fatalf("dup save = %v, want ErrPlanExists", err)
	}
	got, err := pr.FindByID(ctx, "PL-1")
	if err != nil || got.Name() != "v3.0" || got.ProjectID() != "P-1" || got.Status() != pm.PlanDraft {
		t.Fatalf("FindByID = %+v, %v", got, err)
	}
	if got.ConversationID() != "" || got.TargetDate() != nil {
		t.Fatalf("empty conversation/target should round-trip empty: %+v", got)
	}
	if _, err := pr.FindByID(ctx, "nope"); err != pm.ErrPlanNotFound {
		t.Fatalf("FindByID missing = %v, want ErrPlanNotFound", err)
	}

	// update: rename + target date + conversation + start
	td := t0.Add(48 * time.Hour)
	_ = got.Rename("v3.1", t0)
	got.SetTargetDate(&td, t0)
	got.SetConversationID("C-7", t0)
	_ = got.Start(t0)
	if err := pr.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	re, _ := pr.FindByID(ctx, "PL-1")
	if re.Name() != "v3.1" || re.Status() != pm.PlanRunning || re.ConversationID() != "C-7" {
		t.Fatalf("update not persisted: %+v", re)
	}
	if re.TargetDate() == nil || !re.TargetDate().Equal(td.UTC()) {
		t.Fatalf("target date not persisted: %v", re.TargetDate())
	}

	// ListByProject (second plan in another project must not show)
	p2 := newPlanFixture("PL-2", "P-2")
	_ = pr.Save(ctx, p2)
	list, _ := pr.ListByProject(ctx, "P-1")
	if len(list) != 1 || list[0].ID() != "PL-1" {
		t.Fatalf("ListByProject(P-1) = %+v", list)
	}

	// Delete
	if err := pr.Delete(ctx, "PL-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := pr.FindByID(ctx, "PL-1"); err != pm.ErrPlanNotFound {
		t.Fatalf("after delete FindByID = %v, want ErrPlanNotFound", err)
	}
	if err := pr.Delete(ctx, "PL-1"); err != pm.ErrPlanNotFound {
		t.Fatalf("delete missing = %v, want ErrPlanNotFound", err)
	}
	// Update missing
	if err := pr.Update(ctx, newPlanFixture("PX", "P")); err != pm.ErrPlanNotFound {
		t.Fatalf("update missing = %v, want ErrPlanNotFound", err)
	}
}

func TestPlanRepo_Dependencies_AddListRemove(t *testing.T) {
	ctx, pr, _ := planSetup(t)
	_ = pr.Save(ctx, newPlanFixture("PL-1", "P-1"))

	// a→b, b→c
	for _, d := range []pm.Dependency{
		{PlanID: "PL-1", FromTaskID: "a", ToTaskID: "b"},
		{PlanID: "PL-1", FromTaskID: "b", ToTaskID: "c"},
	} {
		if err := pr.AddDependency(ctx, d); err != nil {
			t.Fatalf("AddDependency %+v: %v", d, err)
		}
	}
	deps, err := pr.ListDependencies(ctx, "PL-1")
	if err != nil || len(deps) != 2 {
		t.Fatalf("ListDependencies = %+v, %v", deps, err)
	}

	// remove one
	if err := pr.RemoveDependency(ctx, pm.Dependency{PlanID: "PL-1", FromTaskID: "a", ToTaskID: "b"}); err != nil {
		t.Fatal(err)
	}
	deps, _ = pr.ListDependencies(ctx, "PL-1")
	if len(deps) != 1 || deps[0].FromTaskID != "b" {
		t.Fatalf("after remove = %+v", deps)
	}
}

func TestPlanRepo_AddDependency_RejectsCycleAndSelfEdge(t *testing.T) {
	ctx, pr, _ := planSetup(t)
	_ = pr.Save(ctx, newPlanFixture("PL-1", "P-1"))

	// self-edge rejected
	if err := pr.AddDependency(ctx, pm.Dependency{PlanID: "PL-1", FromTaskID: "a", ToTaskID: "a"}); err != pm.ErrSelfDependency {
		t.Fatalf("self-edge = %v, want ErrSelfDependency", err)
	}

	// build a→b→c then reject c→a (cycle)
	for _, d := range []pm.Dependency{
		{PlanID: "PL-1", FromTaskID: "a", ToTaskID: "b"},
		{PlanID: "PL-1", FromTaskID: "b", ToTaskID: "c"},
	} {
		if err := pr.AddDependency(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	if err := pr.AddDependency(ctx, pm.Dependency{PlanID: "PL-1", FromTaskID: "c", ToTaskID: "a"}); err != pm.ErrPlanCycle {
		t.Fatalf("cycle-closing edge = %v, want ErrPlanCycle", err)
	}
	// the rejected edge must NOT have been persisted
	deps, _ := pr.ListDependencies(ctx, "PL-1")
	if len(deps) != 2 {
		t.Fatalf("rejected cycle edge should not persist, edges = %+v", deps)
	}
}

// TestPlanRepo_DependencyIsolation pins §9.8: depends_on edges are scoped to one
// Plan. Two plans each with their own edges → ListDependencies(planA) returns
// ONLY plan A's edges, never plan B's. A cycle in plan A's edges does not block
// the same shaped edge in plan B (separate DAGs).
func TestPlanRepo_DependencyIsolation(t *testing.T) {
	ctx, pr, _ := planSetup(t)
	_ = pr.Save(ctx, newPlanFixture("PL-A", "P-1"))
	_ = pr.Save(ctx, newPlanFixture("PL-B", "P-1"))

	// Plan A: x→y. Plan B: y→x (which would be a cycle IF the DAGs were shared,
	// but they are isolated per plan, so both insert cleanly).
	if err := pr.AddDependency(ctx, pm.Dependency{PlanID: "PL-A", FromTaskID: "x", ToTaskID: "y"}); err != nil {
		t.Fatal(err)
	}
	if err := pr.AddDependency(ctx, pm.Dependency{PlanID: "PL-B", FromTaskID: "y", ToTaskID: "x"}); err != nil {
		t.Fatalf("PL-B edge must be isolated from PL-A's DAG: %v", err)
	}

	a, _ := pr.ListDependencies(ctx, "PL-A")
	if len(a) != 1 || a[0].PlanID != "PL-A" || a[0].FromTaskID != "x" || a[0].ToTaskID != "y" {
		t.Fatalf("PL-A edges = %+v, want only x→y", a)
	}
	b, _ := pr.ListDependencies(ctx, "PL-B")
	if len(b) != 1 || b[0].PlanID != "PL-B" || b[0].FromTaskID != "y" || b[0].ToTaskID != "x" {
		t.Fatalf("PL-B edges = %+v, want only y→x", b)
	}
}

// TestTaskRepo_PlanMembership pins Task↔Plan = 0..1 round-trip: SetPlan persists,
// ListByPlan returns it, ClearPlan removes it from the plan.
func TestTaskRepo_PlanMembership(t *testing.T) {
	ctx, _, tr := planSetup(t)
	task, _ := pm.NewTask(pm.NewTaskInput{ID: "T-1", ProjectID: "P-1", Title: "do it", CreatedBy: "user:a", CreatedAt: t0})
	if task.PlanID() != "" {
		t.Fatalf("new task should be in no plan, got %q", task.PlanID())
	}
	if err := tr.Save(ctx, task); err != nil {
		t.Fatal(err)
	}
	// not in any plan yet
	if got, _ := tr.ListByPlan(ctx, "PL-1"); len(got) != 0 {
		t.Fatalf("ListByPlan before select = %+v, want empty", got)
	}

	// select into plan
	task.SetPlan("PL-1", t0)
	if err := tr.Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	got, err := tr.ListByPlan(ctx, "PL-1")
	if err != nil || len(got) != 1 || got[0].ID() != "T-1" || got[0].PlanID() != "PL-1" {
		t.Fatalf("ListByPlan after select = %+v, %v", got, err)
	}

	// clear (back to backlog)
	task.ClearPlan(t0)
	if err := tr.Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	if got, _ := tr.ListByPlan(ctx, "PL-1"); len(got) != 0 {
		t.Fatalf("ListByPlan after clear = %+v, want empty", got)
	}
	re, _ := tr.FindByID(ctx, "T-1")
	if re.PlanID() != "" {
		t.Fatalf("cleared plan_id should round-trip empty, got %q", re.PlanID())
	}
}
