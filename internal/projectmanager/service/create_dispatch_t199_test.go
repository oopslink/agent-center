package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// T199 / WS3 — create_task gains optional one-step dispatch into the built-in
// Assignment Pool (and optional assign), so "create → dispatch" is a single call
// instead of the hidden 3-step (create_task + assign_task + add_task_to_plan).
// These service-layer tests pin the four semantic cases + the error paths.

// Dispatch=true, no assignee → the task is selected into the project's built-in
// pool and dispatched in ONE call: immediately claimable from the pool and
// runnable (EnsureTaskRunnable passes). Replaces create_task + add_task_to_plan.
func TestCreateTask_Dispatch_NoAssignee_PoolClaimable(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	pool := findBuiltinPlan(t, h, pid)
	addMember(t, h, pid, "agent:m1")

	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{
		ProjectID: pid, Title: "dispatched", CreatedBy: "user:a", Dispatch: true,
	})
	if err != nil {
		t.Fatalf("CreateTask dispatch: %v", err)
	}

	got, err := h.svc.GetTask(h.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if got.PlanID() != pool.ID() {
		t.Fatalf("plan_id = %q, want builtin pool %q", got.PlanID(), pool.ID())
	}
	if got.Assignee() != "" {
		t.Fatalf("assignee = %q, want empty (unassigned pool task)", got.Assignee())
	}
	// Runnable now (dispatched pool member), no reconcile needed.
	if err := h.svc.EnsureTaskRunnable(h.ctx, tid); err != nil {
		t.Fatalf("EnsureTaskRunnable = %v, want nil (dispatched pool member)", err)
	}
	// Discoverable in the shared pool by a member agent.
	pool2, err := h.svc.ListClaimablePool(h.ctx, "agent:m1")
	if err != nil {
		t.Fatal(err)
	}
	if !containsTask(pool2, tid) {
		t.Fatalf("ListClaimablePool missing dispatched task %s", tid)
	}
}

// Dispatch=true + assignee → assigned AND a dispatched pool member in one call:
// appears in the assignee's get_my_work claimable set and is runnable. Replaces
// create_task + assign_task + add_task_to_plan.
func TestCreateTask_Dispatch_WithAssignee_InAssigneeQueue(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	pool := findBuiltinPlan(t, h, pid)

	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{
		ProjectID: pid, Title: "assigned+dispatched", CreatedBy: "user:a",
		Assignee: "agent:dev1", Dispatch: true,
	})
	if err != nil {
		t.Fatalf("CreateTask dispatch+assignee: %v", err)
	}

	got, err := h.svc.GetTask(h.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if got.Assignee() != "agent:dev1" {
		t.Fatalf("assignee = %q, want agent:dev1", got.Assignee())
	}
	if got.PlanID() != pool.ID() {
		t.Fatalf("plan_id = %q, want builtin pool %q", got.PlanID(), pool.ID())
	}
	if err := h.svc.EnsureTaskRunnable(h.ctx, tid); err != nil {
		t.Fatalf("EnsureTaskRunnable = %v, want nil", err)
	}
	// Appears in the assignee's get_my_work claimable_tasks bucket.
	cl, err := h.svc.ListClaimableTasks(h.ctx, "agent:dev1")
	if err != nil {
		t.Fatal(err)
	}
	if !containsTask(cl, tid) {
		t.Fatalf("ListClaimableTasks(agent:dev1) missing %s", tid)
	}
	// The assign-on-create granted the agent project membership (#5a), so it can
	// pass its own MCP write-gate for this project.
	if _, merr := h.svc.GetTask(h.ctx, tid); merr != nil {
		t.Fatalf("post-create GetTask: %v", merr)
	}
}

// assignee WITHOUT dispatch → assigned backlog (T130: assignment decoupled from
// runnability). The task is owned but NOT runnable and NOT in any plan.
func TestCreateTask_Assignee_NoDispatch_BacklogNotRunnable(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{
		ProjectID: pid, Title: "assigned backlog", CreatedBy: "user:a", Assignee: "agent:dev1",
	})
	if err != nil {
		t.Fatalf("CreateTask assignee no-dispatch: %v", err)
	}
	got, err := h.svc.GetTask(h.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if got.Assignee() != "agent:dev1" {
		t.Fatalf("assignee = %q, want agent:dev1", got.Assignee())
	}
	if got.PlanID() != "" {
		t.Fatalf("plan_id = %q, want empty (backlog)", got.PlanID())
	}
	if err := h.svc.EnsureTaskRunnable(h.ctx, tid); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("EnsureTaskRunnable = %v, want ErrTaskNotRunnable (assigned backlog)", err)
	}
}

// Plain create (no assignee, no dispatch) → backlog, unchanged behavior.
func TestCreateTask_Plain_Backlog(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "plain", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.svc.GetTask(h.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if got.PlanID() != "" || got.Assignee() != "" {
		t.Fatalf("plain create: plan_id=%q assignee=%q, want both empty", got.PlanID(), got.Assignee())
	}
	if err := h.svc.EnsureTaskRunnable(h.ctx, tid); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("EnsureTaskRunnable = %v, want ErrTaskNotRunnable (backlog)", err)
	}
}

// A cross-org agent assignee is rejected, and because create+assign+dispatch is
// ONE tx, the whole operation rolls back — no orphan task is left behind.
func TestCreateTask_Dispatch_CrossOrgAssignee_RollsBack(t *testing.T) {
	h := planAdvanceSetup(t) // AgentDir maps every agent to org-1
	// Project in org-2 → an org-1 agent is cross-org for it.
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-2", Name: "P2", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.svc.CreateTask(h.ctx, CreateTaskCommand{
		ProjectID: pid, Title: "cross-org", CreatedBy: "user:a", Assignee: "agent:dev1", Dispatch: true,
	})
	if !errors.Is(err, pm.ErrCrossOrgAssignee) {
		t.Fatalf("CreateTask cross-org = %v, want ErrCrossOrgAssignee", err)
	}
	// Atomic rollback: no task persisted for the project.
	tasks, err := h.svc.ListProjectTasksForMember(h.ctx, pid, "user:a")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("after rollback, project has %d tasks, want 0", len(tasks))
	}
}

// Dispatch=true on a Service with NO plan repo wired → ErrPlansUnavailable (the
// one-step dispatch cannot reach the pool). The plain create still works.
func TestCreateTask_Dispatch_PlansUnavailable(t *testing.T) {
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	clk := clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	gen := idgen.NewGenerator(clk)
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Tasks: pmsql.NewTaskRepo(db), Outbox: outboxsql.NewOutboxRepo(db), IDGen: gen, Clock: clk,
	})
	ctx := context.Background()
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "t", CreatedBy: "user:a", Dispatch: true}); !errors.Is(err, ErrPlansUnavailable) {
		t.Fatalf("CreateTask dispatch with nil plans = %v, want ErrPlansUnavailable", err)
	}
}

// A malformed assignee ref at the service layer is rejected (and rolls back) —
// the AppService validates the ref even though the MCP handler also pre-checks.
func TestCreateTask_BadAssigneeRef_Rejected(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "bad", CreatedBy: "user:a", Assignee: "bob"}); err == nil {
		t.Fatal("CreateTask with bad assignee ref = nil, want validation error")
	}
	tasks, err := h.svc.ListProjectTasksForMember(h.ctx, pid, "user:a")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("after rollback, project has %d tasks, want 0", len(tasks))
	}
}

// requireBuiltinPool surfaces ErrBuiltinPoolMissing for a project with no
// built-in pool (ADR-0047 invariant breach). Seeds a project directly via the
// repo (bypassing CreateProject's pool creation) to reach the guard.
func TestRequireBuiltinPool_Missing(t *testing.T) {
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	clk := clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	gen := idgen.NewGenerator(clk)
	projRepo := pmsql.NewProjectRepo(db)
	svc := New(Deps{
		DB: db, Projects: projRepo, Members: pmsql.NewProjectMemberRepo(db),
		Tasks: pmsql.NewTaskRepo(db), Plans: pmsql.NewPlanRepo(db),
		Outbox: outboxsql.NewOutboxRepo(db), IDGen: gen, Clock: clk,
	})
	ctx := context.Background()
	p, err := pm.NewProject(pm.NewProjectInput{
		ID: pm.ProjectID(gen.NewEntityID("project")), OrganizationID: "org-1",
		Name: "poolless", CreatedBy: "user:a", CreatedAt: clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := projRepo.Save(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.requireBuiltinPool(ctx, p.ID()); !errors.Is(err, ErrBuiltinPoolMissing) {
		t.Fatalf("requireBuiltinPool(pool-less) = %v, want ErrBuiltinPoolMissing", err)
	}
}

func containsTask(list []ClaimableTask, id pm.TaskID) bool {
	for _, c := range list {
		if c.Task.ID() == id {
			return true
		}
	}
	return false
}
