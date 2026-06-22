package service

import (
	"context"
	"errors"
	"testing"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// T329 (issue-9d4b3895) — the dependency-satisfaction DISPATCH hard-gate that
// fixes the control-flow engine's initial-dispatch 抢跑 + re-dispatch churn. These
// pin the gate predicate (EnsureTaskDispatchable / EnsureTaskRunnable) for each
// required behavior, and that the WorkItemProjector honors it.

// runningTwoNodePlan builds a started plan A→B (B depends on A) and returns the
// project + the two node ids. Both nodes are assigned (human assignees, so no
// AgentDirectory resolution is needed) and the plan is RUNNING.
func runningTwoNodePlan(t *testing.T, h *planAdvanceHarness) (pm.ProjectID, pm.TaskID, pm.TaskID) {
	t.Helper()
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "chain", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:x")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:y")
	if err := h.svc.AddPlanDependency(h.ctx, planID, b, a, "user:a"); err != nil {
		t.Fatalf("AddPlanDependency (B depends on A): %v", err)
	}
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	h.drain(t)
	return pid, a, b
}

// Requirement: depends_on edges are respected — a node is dispatchable/runnable ONLY
// once its upstream is done; "满足才派发", "依赖未满足不派发".
func TestT329_DepsGate_RespectsDependsOnEdge(t *testing.T) {
	h := planAdvanceSetup(t)
	_, a, b := runningTwoNodePlan(t, h)

	// A has no upstream → deps satisfied → dispatchable + runnable.
	if err := h.svc.EnsureTaskDispatchable(h.ctx, a); err != nil {
		t.Fatalf("A (no deps) dispatchable = %v, want nil", err)
	}
	if err := h.svc.EnsureTaskRunnable(h.ctx, a); err != nil {
		t.Fatalf("A (no deps) runnable = %v, want nil", err)
	}
	// B depends on A (not done) → NOT dispatchable + NOT runnable.
	if err := h.svc.EnsureTaskDispatchable(h.ctx, b); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("B (dep unmet) dispatchable = %v, want ErrTaskNotRunnable", err)
	}
	if err := h.svc.EnsureTaskRunnable(h.ctx, b); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("B (dep unmet) runnable = %v, want ErrTaskNotRunnable", err)
	}

	// Complete A → B's only dependency is satisfied → B becomes dispatchable + runnable.
	h.setTaskStatus(t, a, pm.TaskCompleted)
	if err := h.svc.EnsureTaskDispatchable(h.ctx, b); err != nil {
		t.Fatalf("B (dep met) dispatchable = %v, want nil", err)
	}
	if err := h.svc.EnsureTaskRunnable(h.ctx, b); err != nil {
		t.Fatalf("B (dep met) runnable = %v, want nil", err)
	}
}

// Requirement: a plan that is not running (draft/stopped) dispatches NOTHING.
func TestT329_DepsGate_PlanNotRunning(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "draft", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:x") // plan left in DRAFT (never started)

	if err := h.svc.EnsureTaskDispatchable(h.ctx, a); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("node of a draft plan dispatchable = %v, want ErrTaskNotRunnable", err)
	}
	if err := h.svc.EnsureTaskRunnable(h.ctx, a); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("node of a draft plan runnable = %v, want ErrTaskNotRunnable", err)
	}
}

// Requirement: terminal nodes (completed/discarded) are NEVER re-dispatched.
func TestT329_DepsGate_TerminalNeverDispatched(t *testing.T) {
	h := planAdvanceSetup(t)
	_, a, _ := runningTwoNodePlan(t, h)
	h.setTaskStatus(t, a, pm.TaskCompleted)
	if err := h.svc.EnsureTaskDispatchable(h.ctx, a); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("completed node dispatchable = %v, want ErrTaskNotRunnable", err)
	}
	h.setTaskStatus(t, a, pm.TaskDiscarded)
	if err := h.svc.EnsureTaskDispatchable(h.ctx, a); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("discarded node dispatchable = %v, want ErrTaskNotRunnable", err)
	}
}

// Requirement: a blocked node (carrying a blocked_reason) is not blindly re-dispatched.
func TestT329_DepsGate_BlockedNotDispatched(t *testing.T) {
	h := planAdvanceSetup(t)
	_, a, _ := runningTwoNodePlan(t, h)
	// Drive A to running, then block it (the stuck annotation, ADR-0046).
	h.setTaskStatus(t, a, pm.TaskRunning)
	if err := h.svc.BlockTask(h.ctx, a, "waiting on an external dep", "user:a"); err != nil {
		t.Fatalf("BlockTask: %v", err)
	}
	if err := h.svc.EnsureTaskDispatchable(h.ctx, a); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("blocked node dispatchable = %v, want ErrTaskNotRunnable", err)
	}
}

// Requirement: a backlog task (no plan) is not dispatchable.
func TestT329_DepsGate_BacklogNotDispatched(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "backlog", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.EnsureTaskDispatchable(h.ctx, tid); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("backlog dispatchable = %v, want ErrTaskNotRunnable", err)
	}
}

// A dispatched built-in pool member IS dispatchable; a selected-but-not-dispatched
// pool member is NOT (the pool gate is unchanged by T329).
func TestT329_DepsGate_PoolMember(t *testing.T) {
	h := planAdvanceSetup(t)
	_, dispatched := dispatchedPoolTask(t, h, "org-1", "P")
	if err := h.svc.EnsureTaskDispatchable(h.ctx, dispatched); err != nil {
		t.Fatalf("dispatched pool member dispatchable = %v, want nil", err)
	}

	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-2", Name: "P2", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	pool := findBuiltinPlan(t, h, pid)
	pending, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "pending", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.SelectTaskIntoPlan(h.ctx, pool.ID(), pending, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.EnsureTaskDispatchable(h.ctx, pending); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("non-dispatched pool member dispatchable = %v, want ErrTaskNotRunnable", err)
	}
}

// The WorkItemProjector consults the dispatch gate before minting: a gate that
// rejects (ErrTaskNotRunnable) → NO work item; a passing (nil) gate → one work
// item. A nil gate keeps the legacy unconditional mint. This isolates the
// projector's skip-vs-mint behavior from the gate's dependency logic (tested above).
func TestT329_WorkItemProjector_HonorsDispatchGate(t *testing.T) {
	newProj := func(t *testing.T, gate DispatchGateFunc) (*WorkItemProjector, *agentsql.WorkItemRepo, context.Context) {
		t.Helper()
		db, err := persistence.Open(persistence.MemoryDSN())
		if err != nil {
			t.Fatal(err)
		}
		if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		clk := clock.NewFakeClock(clock.SystemClock{}.Now())
		gen := idgen.NewGenerator(clk)
		wiRepo := agentsql.NewWorkItemRepo(db)
		proj := NewWorkItemProjectorWithDeps(WorkItemProjectorDeps{
			DB: db, WorkItems: wiRepo, Applied: outboxsql.NewAppliedRepo(db),
			IDGen: gen, Clock: clk, DispatchGate: gate,
		})
		return proj, wiRepo, context.Background()
	}
	const taskRef = "pm://tasks/T1"
	assignEvt := func(id string) outbox.Event {
		return outbox.Event{
			ID: id, EventType: EvtTaskAssigned,
			Payload: `{"owner_ref":"` + taskRef + `","assignee":"agent:AG1","status":"open"}`,
		}
	}
	countItems := func(t *testing.T, repo *agentsql.WorkItemRepo, ctx context.Context) int {
		t.Helper()
		items, err := repo.ListByTask(ctx, taskRef)
		if err != nil {
			t.Fatal(err)
		}
		return len(items)
	}

	// Gate rejects → projection succeeds (event consumed) but NO work item is minted.
	rejectGate := func(context.Context, string) error { return pm.ErrTaskNotRunnable }
	proj, repo, ctx := newProj(t, rejectGate)
	if err := proj.Project(ctx, assignEvt("e1")); err != nil {
		t.Fatalf("Project (reject gate) = %v, want nil (skip, not error)", err)
	}
	if n := countItems(t, repo, ctx); n != 0 {
		t.Fatalf("reject gate minted %d work items, want 0", n)
	}

	// Gate allows → exactly one work item minted.
	proj2, repo2, ctx2 := newProj(t, func(context.Context, string) error { return nil })
	if err := proj2.Project(ctx2, assignEvt("e2")); err != nil {
		t.Fatalf("Project (allow gate) = %v", err)
	}
	if n := countItems(t, repo2, ctx2); n != 1 {
		t.Fatalf("allow gate minted %d work items, want 1", n)
	}

	// Nil gate → legacy unconditional mint (one work item).
	proj3, repo3, ctx3 := newProj(t, nil)
	if err := proj3.Project(ctx3, assignEvt("e3")); err != nil {
		t.Fatalf("Project (nil gate) = %v", err)
	}
	if n := countItems(t, repo3, ctx3); n != 1 {
		t.Fatalf("nil gate minted %d work items, want 1 (legacy behavior)", n)
	}
}

// Guard: a non-task ref through the port adapter is a defensive no-op (nil), not an
// error — mirrors EnsureWorkItemRunnable.
func TestT329_EnsureWorkItemDispatchable_NonTaskRef(t *testing.T) {
	h := planAdvanceSetup(t)
	gate := NewAgentTaskRunGate(h.svc)
	if err := gate.EnsureWorkItemDispatchable(h.ctx, "not-a-task-ref"); err != nil {
		t.Fatalf("non-task ref = %v, want nil", err)
	}
}

// Compile-time guard that the agent-BC sentinel exists for the start-side mapping
// (the dispatch side maps to pm.ErrTaskNotRunnable directly, asserted above).
var _ = agentpkg.ErrWorkItemTaskNotRunnable
