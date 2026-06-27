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

// TestPlanRepo_BuiltinRoundTrip (ADR-0047) proves the is_builtin flag round-trips:
// a builtin plan saved + reloaded reports IsBuiltin()==true, and a normal plan
// reports false.
func TestPlanRepo_BuiltinRoundTrip(t *testing.T) {
	ctx, pr, _ := planSetup(t)
	bp, err := pm.NewPlan(pm.NewPlanInput{
		ID: "PL-builtin", ProjectID: "P-1", Name: "[Built-in]",
		CreatorRef: "system", Builtin: true, CreatedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pr.Save(ctx, bp); err != nil {
		t.Fatal(err)
	}
	got, err := pr.FindByID(ctx, "PL-builtin")
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsBuiltin() {
		t.Fatalf("builtin plan round-trip: IsBuiltin()=false, want true")
	}
	// A normal plan stays non-builtin.
	np := newPlanFixture("PL-normal", "P-1")
	if err := pr.Save(ctx, np); err != nil {
		t.Fatal(err)
	}
	rn, _ := pr.FindByID(ctx, "PL-normal")
	if rn.IsBuiltin() {
		t.Fatalf("normal plan round-trip: IsBuiltin()=true, want false")
	}
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

// §9.3 dispatch records: RecordDispatch is idempotent on the PK (a re-write for
// an already-dispatched node is a no-op, never an error nor a second record);
// ListDispatchRecords is per-plan scoped; ClearDispatch removes one node's record.
func TestPlanRepo_DispatchRecords(t *testing.T) {
	ctx, pr, _ := planSetup(t)
	if err := pr.Save(ctx, newPlanFixture("PL-1", "P-1")); err != nil {
		t.Fatal(err)
	}
	if err := pr.Save(ctx, newPlanFixture("PL-2", "P-1")); err != nil {
		t.Fatal(err)
	}

	at := t0
	if err := pr.RecordDispatch(ctx, "PL-1", "T-1", at, "m-1"); err != nil {
		t.Fatal(err)
	}
	// §9.3 idempotency: a second RecordDispatch for the same {plan,task} is a no-op
	// (INSERT OR IGNORE) — no error, no second row, keeps the original message id.
	if err := pr.RecordDispatch(ctx, "PL-1", "T-1", at.Add(time.Hour), "m-2-SHOULD-NOT-WIN"); err != nil {
		t.Fatalf("idempotent re-record should not error: %v", err)
	}
	recs, err := pr.ListDispatchRecords(ctx, "PL-1")
	if err != nil || len(recs) != 1 {
		t.Fatalf("ListDispatchRecords = %+v, %v want exactly 1", recs, err)
	}
	if recs[0].TaskID != "T-1" || recs[0].DispatchMessageID != "m-1" {
		t.Fatalf("record = %+v want T-1/m-1 (first write wins)", recs[0])
	}

	// per-plan scoping (§9.8): PL-2 sees nothing of PL-1's records.
	if other, _ := pr.ListDispatchRecords(ctx, "PL-2"); len(other) != 0 {
		t.Fatalf("PL-2 records = %+v want empty (per-plan isolation)", other)
	}

	// ClearDispatch removes the node's record (re-run path); clearing a missing
	// record is a no-op.
	if err := pr.ClearDispatch(ctx, "PL-1", "T-1"); err != nil {
		t.Fatal(err)
	}
	if recs, _ := pr.ListDispatchRecords(ctx, "PL-1"); len(recs) != 0 {
		t.Fatalf("after clear = %+v want empty", recs)
	}
	if err := pr.ClearDispatch(ctx, "PL-1", "missing"); err != nil {
		t.Fatalf("clear missing should be no-op: %v", err)
	}
}

// TestTaskRepo_ListUnplannedByProject pins the v2.9 backlog complement: with two
// tasks in a project, one selected into a Plan and one left in the backlog,
// ListUnplannedByProject returns ONLY the unplanned one (empty plan_id).
func TestTaskRepo_ListUnplannedByProject(t *testing.T) {
	ctx, _, tr := planSetup(t)
	planned, _ := pm.NewTask(pm.NewTaskInput{ID: "T-planned", ProjectID: "P-1", Title: "in a plan", CreatedBy: "user:a", CreatedAt: t0})
	backlog, _ := pm.NewTask(pm.NewTaskInput{ID: "T-backlog", ProjectID: "P-1", Title: "not in a plan", CreatedBy: "user:a", CreatedAt: t0.Add(time.Second)})
	if err := tr.Save(ctx, planned); err != nil {
		t.Fatal(err)
	}
	if err := tr.Save(ctx, backlog); err != nil {
		t.Fatal(err)
	}

	// select one into a plan; leave the other in the backlog.
	planned.SetPlan("PL-1", t0)
	if err := tr.Update(ctx, planned); err != nil {
		t.Fatal(err)
	}

	got, err := tr.ListUnplannedByProject(ctx, "P-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID() != "T-backlog" || got[0].PlanID() != "" {
		t.Fatalf("ListUnplannedByProject = %+v, want only T-backlog with empty plan_id", got)
	}

	// clearing the planned task back to backlog returns both.
	planned.ClearPlan(t0)
	if err := tr.Update(ctx, planned); err != nil {
		t.Fatal(err)
	}
	if got, _ := tr.ListUnplannedByProject(ctx, "P-1"); len(got) != 2 {
		t.Fatalf("ListUnplannedByProject after clear = %d, want 2", len(got))
	}
}

// TestTaskRepo_CountActiveByAssignee_ExcludesTerminalPlanTasks pins T342d: open
// tasks in a TERMINAL plan (archived/done) are dead work and must NOT count toward
// agent load/backlog (the agent's runnable Tasks panel already excludes them — the
// reported "backlog=7 but empty task list" was archived-plan tasks). Tasks with no
// plan or in a draft/running plan still count.
func TestTaskRepo_CountActiveByAssignee_ExcludesTerminalPlanTasks(t *testing.T) {
	ctx, pr, tr := planSetup(t)
	mkPlan := func(id pm.PlanID, status pm.PlanStatus) {
		p, err := pm.RehydratePlan(pm.RehydratePlanInput{
			ID: id, ProjectID: "P-1", Name: "pln", Status: status,
			CreatorRef: "user:a", CreatedAt: t0, UpdatedAt: t0, Version: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := pr.Save(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	mkTask := func(id pm.TaskID, plan pm.PlanID, status pm.TaskStatus) {
		tk, err := pm.RehydrateTask(pm.RehydrateTaskInput{
			ID: id, ProjectID: "P-1", Title: string(id), Status: status,
			Assignee: "agent:agent-b5036ea8", PlanID: plan,
			CreatedBy: "user:a", CreatedAt: t0, UpdatedAt: t0, Version: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := tr.Save(ctx, tk); err != nil {
			t.Fatal(err)
		}
	}
	const ag = "agent:agent-b5036ea8"
	mkPlan("PL-ARCH", pm.PlanArchived)
	mkPlan("PL-DONE", pm.PlanDone)
	mkPlan("PL-RUN", pm.PlanRunning)
	mkTask("TA1", "PL-ARCH", pm.TaskOpen)   // archived plan → excluded
	mkTask("TA2", "PL-ARCH", pm.TaskOpen)   // archived plan → excluded
	mkTask("TD1", "PL-DONE", pm.TaskOpen)   // done plan → excluded
	mkTask("TR1", "PL-RUN", pm.TaskOpen)    // running plan → counted
	mkTask("TRr", "PL-RUN", pm.TaskRunning) // running plan, doing → counted
	mkTask("TB1", "", pm.TaskOpen)          // no plan (backlog) → counted

	got, err := tr.CountActiveByAssignee(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Pending = TR1 + TB1 (the 2 archived + 1 done excluded); Running = TRr.
	if got[ag].Pending != 2 || got[ag].Running != 1 {
		t.Fatalf("got %+v, want {Running:1 Pending:2} (terminal-plan tasks excluded)", got[ag])
	}
}

// TestTaskRepo_ListActiveByAssignee_MatchesBacklogCount pins that the list-shaped
// twin returns EXACTLY the rows CountActiveByAssignee counts for an assignee:
// open+running, terminal-plan tasks excluded, dependency-blocked tasks included
// (no runnable filter). This is what reconciles the Agent Tasks panel with the
// "backlog: N" badge.
func TestTaskRepo_ListActiveByAssignee_MatchesBacklogCount(t *testing.T) {
	ctx, pr, tr := planSetup(t)
	mkPlan := func(id pm.PlanID, status pm.PlanStatus) {
		p, err := pm.RehydratePlan(pm.RehydratePlanInput{
			ID: id, ProjectID: "P-1", Name: "pln", Status: status,
			CreatorRef: "user:a", CreatedAt: t0, UpdatedAt: t0, Version: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := pr.Save(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	mkTask := func(id pm.TaskID, plan pm.PlanID, status pm.TaskStatus, assignee string) {
		tk, err := pm.RehydrateTask(pm.RehydrateTaskInput{
			ID: id, ProjectID: "P-1", Title: string(id), Status: status,
			Assignee: pm.IdentityRef(assignee), PlanID: plan,
			CreatedBy: "user:a", CreatedAt: t0, UpdatedAt: t0, Version: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := tr.Save(ctx, tk); err != nil {
			t.Fatal(err)
		}
	}
	const ag = "agent:agent-b5036ea8"
	const other = "agent:agent-cccc3333"
	mkPlan("PL-ARCH", pm.PlanArchived)
	mkPlan("PL-RUN", pm.PlanRunning)
	mkTask("TR1", "PL-RUN", pm.TaskOpen, ag)      // running plan, open → listed
	mkTask("TRr", "PL-RUN", pm.TaskRunning, ag)   // running plan, doing → listed
	mkTask("TB1", "", pm.TaskOpen, ag)            // no plan (backlog) → listed
	mkTask("TA1", "PL-ARCH", pm.TaskOpen, ag)     // archived plan → excluded
	mkTask("TC1", "PL-RUN", pm.TaskCompleted, ag) // terminal task → excluded
	mkTask("TX1", "PL-RUN", pm.TaskOpen, other)   // other assignee → excluded

	got, err := tr.ListActiveByAssignee(ctx, ag)
	if err != nil {
		t.Fatal(err)
	}
	gotIDs := make([]string, 0, len(got))
	for _, tk := range got {
		gotIDs = append(gotIDs, string(tk.ID()))
	}
	want := []string{"TB1", "TR1", "TRr"} // order = (created_at, id); same t0 → id asc
	if len(gotIDs) != len(want) {
		t.Fatalf("got %v, want %v", gotIDs, want)
	}
	for i := range want {
		if gotIDs[i] != want[i] {
			t.Fatalf("got %v, want %v (order = created_at,id)", gotIDs, want)
		}
	}
	// Reconciles with the backlog metric: list length == Running+Pending count.
	counts, err := tr.CountActiveByAssignee(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if total := counts[ag].Running + counts[ag].Pending; total != len(got) {
		t.Fatalf("list len %d != backlog count %d", len(got), total)
	}
}
