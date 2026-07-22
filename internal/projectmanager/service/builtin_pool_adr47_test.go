package service

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// assignedEventCountForTask counts UNPROCESSED pm.task.assigned outbox events that
// reference taskID. Tests drain prior events first, so a count taken right after a
// dispatch sweep isolates exactly the pm.task.assigned the sweep emitted — the F1
// pool-dispatch wake (issue-ca51e07c).
func assignedEventCountForTask(t *testing.T, h *planAdvanceHarness, taskID pm.TaskID) int {
	t.Helper()
	ob := outboxsql.NewOutboxRepo(h.svc.db)
	evs, err := ob.FetchUnprocessed(h.ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range evs {
		if e.EventType != EvtTaskAssigned {
			continue
		}
		var pl taskEventPayload
		if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
			continue
		}
		if pl.TaskID == string(taskID) {
			n++
		}
	}
	return n
}

// findBuiltinPlan returns the project's auto-created built-in pool (ADR-0047), or
// fails the test if absent.
func findBuiltinPlan(t *testing.T, h *planAdvanceHarness, pid pm.ProjectID) *pm.Plan {
	t.Helper()
	plans, err := h.svc.ListPlans(h.ctx, pid)
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	for _, p := range plans {
		if p.IsBuiltin() {
			return p
		}
	}
	t.Fatalf("no built-in pool plan found for project %s", pid)
	return nil
}

// --- Increment 3: auto-create the built-in pool on CreateProject ------------

// TestCreateProject_AutoCreatesBuiltinPool proves CreateProject auto-creates the
// per-project built-in pool: exactly one builtin plan exists, status=running,
// IsBuiltin()==true.
func TestCreateProject_AutoCreatesBuiltinPool(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	plans, err := h.svc.ListPlans(h.ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	builtins := 0
	var pool *pm.Plan
	for _, p := range plans {
		if p.IsBuiltin() {
			builtins++
			pool = p
		}
	}
	if builtins != 1 {
		t.Fatalf("builtin pool count=%d, want exactly 1", builtins)
	}
	if pool.Status() != pm.PlanRunning {
		t.Fatalf("builtin pool status=%s, want running", pool.Status())
	}
	if !pool.IsBuiltin() {
		t.Fatal("pool.IsBuiltin()=false, want true")
	}
}

// --- Increment 4: service guards reject the built-in pool --------------------

// TestBuiltinPool_GuardsReject proves the user-facing service guards reject
// mutating the built-in pool: AddPlanDependency → ErrBuiltinPlanNoEdges,
// DeletePlan + ArchivePlan → ErrBuiltinPlanImmutable.
func TestBuiltinPool_GuardsReject(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	pool := findBuiltinPlan(t, h, pid)

	// Two tasks selected into the pool (allowed) — endpoints for the edge attempt.
	t0 := h.seedAssignedTask(t, pid, pool.ID(), "p0", "user:x")
	t1 := h.seedAssignedTask(t, pid, pool.ID(), "p1", "user:y")

	if err := h.svc.AddPlanDependency(h.ctx, pool.ID(), t1, t0, "user:a"); err != pm.ErrBuiltinPlanNoEdges {
		t.Fatalf("AddPlanDependency on builtin = %v, want ErrBuiltinPlanNoEdges", err)
	}
	if err := h.svc.DeletePlan(h.ctx, pool.ID(), "user:a"); err != pm.ErrBuiltinPlanImmutable {
		t.Fatalf("DeletePlan on builtin = %v, want ErrBuiltinPlanImmutable", err)
	}
	if err := h.svc.ArchivePlan(h.ctx, pool.ID(), "user:a"); err != pm.ErrBuiltinPlanImmutable {
		t.Fatalf("ArchivePlan on builtin = %v, want ErrBuiltinPlanImmutable", err)
	}
}

// TestBuiltinPool_SelectTaskAllowedWhileRunning proves SelectTaskIntoPlan works on
// the (running) built-in pool — that is how a task ENTERS the claimable pool — even
// though SelectTaskIntoPlan otherwise requires a draft plan (ErrPlanNotDraft).
func TestBuiltinPool_SelectTaskAllowedWhileRunning(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	pool := findBuiltinPlan(t, h, pid)
	if pool.Status() != pm.PlanRunning {
		t.Fatalf("precondition: pool must be running, got %s", pool.Status())
	}

	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "pool task", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	a := "agent:bot"
	if err := h.svc.BatchUpdateTask(h.ctx, tid, BatchTaskPatch{Assignee: &a}, "user:a"); err != nil {
		t.Fatal(err)
	}
	// Selecting into the RUNNING builtin pool must be allowed (no ErrPlanNotDraft).
	if err := h.svc.SelectTaskIntoPlan(h.ctx, pool.ID(), tid, "user:a"); err != nil {
		t.Fatalf("SelectTaskIntoPlan into running builtin pool = %v, want nil", err)
	}
	got, _ := h.svc.GetTask(h.ctx, tid)
	if got.PlanID() != pool.ID() {
		t.Fatalf("task plan_id=%q, want %q", got.PlanID(), pool.ID())
	}
}

// --- Increment 5: dispatch is PULL (no @mention, no wake) for the pool -------

// TestBuiltinPool_DispatchIsPullNoWake proves the built-in pool's dispatch is a
// pull at the CONVERSATION layer: a task selected into the pool (assigned, open)
// becomes claimable after a dispatch sweep (a dispatch record exists →
// node_status=dispatched → TaskClaimable true) WITHOUT posting an @mention into a
// plan conversation. In contrast, a structured plan's ready node DOES post an
// @mention.
//
// F1 (issue-ca51e07c) NOTE: "pull/no-@mention" is NOT "no wake". An ASSIGNED pool
// member still needs a PUSH wake (it will not self-claim its own task), so the
// dispatch emits a content-free pm.task.assigned that the DispatchWakeProjector
// turns into agent.work_available — see TestBuiltinPool_DispatchWakesAssignedMember.
// The pull property asserted here is specifically the ABSENCE of an @mention /
// plan-conversation message, which the wake emit preserves.
func TestBuiltinPool_DispatchIsPullNoWake(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	pool := findBuiltinPlan(t, h, pid)

	// Select an assigned, open task into the pool.
	tid := h.seedAssignedTask(t, pid, pool.ID(), "pool work", "agent:bot")

	// Run the pull dispatch over the pool (the reconcile loop / orchestrator drives
	// this in production; ReconcileRunningPlans sweeps every running plan incl. the
	// always-running pool).
	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatalf("ReconcileRunningPlans: %v", err)
	}

	// A dispatch record exists for the task → node_status=dispatched → claimable.
	detail, err := h.svc.GetPlanDetail(h.ctx, pool.ID())
	if err != nil {
		t.Fatal(err)
	}
	var node *pm.PlanNodeView
	for i := range detail.View.Nodes {
		if detail.View.Nodes[i].TaskID == tid {
			node = &detail.View.Nodes[i]
		}
	}
	if node == nil {
		t.Fatalf("task %s missing from pool view", tid)
	}
	if node.NodeStatus != pm.NodeDispatched {
		t.Fatalf("pool node status=%s, want dispatched (claimable)", node.NodeStatus)
	}
	if !node.Dispatched {
		t.Fatal("pool node Dispatched=false, want true")
	}
	// Pull: the dispatch record carries NO message id (no @mention was posted).
	if node.DispatchMessageID != "" {
		t.Fatalf("pool dispatch message id=%q, want empty (pull = no @mention)", node.DispatchMessageID)
	}
	// The task is claimable.
	got, _ := h.svc.GetTask(h.ctx, tid)
	if !pm.TaskClaimable(got, node.NodeStatus) {
		t.Fatalf("pool task should be claimable: archived=%v status=%s assignee=%s planID=%s node=%s",
			got.IsArchived(), got.Status(), got.Assignee(), got.PlanID(), node.NodeStatus)
	}
	// NO @mention posted: the pool has no bound conversation, so no plan conversation
	// message exists. (planConvMsgCount would fail finding the conversation — assert
	// the pool has no conversation id instead.)
	if pool.ConversationID() != "" {
		t.Fatalf("builtin pool should have no conversation (pull/no-wake), got %q", pool.ConversationID())
	}
}

// TestStructuredPlan_DispatchPostsMention is the contrast case: a structured
// (non-builtin) plan's ready node DOES post an @mention into its conversation (the
// push path), proving the pull/push split is real.
func TestStructuredPlan_DispatchPostsMention(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	plan, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "structured", CreatedBy: "user:a"})
	h.drain(t)
	h.seedAssignedTask(t, pid, plan, "root", "agent:bot")

	before := h.planConvMsgCount(t, plan)
	if err := h.svc.StartPlan(h.ctx, plan, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	h.drain(t)
	if _, err := h.svc.AdvancePlan(h.ctx, plan, "user:a"); err != nil {
		t.Fatalf("AdvancePlan: %v", err)
	}
	h.drain(t)
	after := h.planConvMsgCount(t, plan)
	if after <= before {
		t.Fatalf("structured plan ready node should post an @mention: msgs before=%d after=%d", before, after)
	}
}

// ADR-0047 §-1 #2: the built-in pool gets a 1:1 conversation (same EvtPlanCreated
// path as a structured plan) so pool activity has a home.
func TestADR47_BuiltinPool_GetsConversation(t *testing.T) {
	svc, convRepo, plans, _, relay, ctx := planSetup(t)
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	drain(t, relay, ctx)

	var poolID pm.PlanID
	ps, _ := svc.ListPlans(ctx, pid)
	for _, p := range ps {
		if p.IsBuiltin() {
			poolID = p.ID()
		}
	}
	if poolID == "" {
		t.Fatal("no builtin pool")
	}
	pool, _ := plans.FindByID(ctx, poolID)
	if pool.ConversationID() == "" {
		t.Fatal("builtin pool must have a bound conversation after drain")
	}
	conv, err := convRepo.FindByOwnerRef(ctx, conversation.NewPlanOwnerRef(string(poolID)))
	if err != nil {
		t.Fatalf("pool conversation should exist by owner_ref: %v", err)
	}
	if conv.Kind() != conversation.ConversationKindPlan {
		t.Fatalf("conv kind=%s want plan", conv.Kind())
	}
}

// ADR-0047 §-1 #1: archiving a project cascade-archives its built-in pool (the pool
// is "archived with its project"), even though the pool is always-running.
func TestADR47_ArchiveProject_CascadeArchivesBuiltinPool(t *testing.T) {
	svc, _, plans, _, _, ctx := planSetup(t)
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	var poolID pm.PlanID
	ps, _ := svc.ListPlans(ctx, pid)
	for _, p := range ps {
		if p.IsBuiltin() {
			poolID = p.ID()
		}
	}
	if err := svc.ArchiveProject(ctx, pid, "user:a"); err != nil {
		t.Fatalf("ArchiveProject: %v", err)
	}
	pool, err := plans.FindByID(ctx, poolID)
	if err != nil {
		t.Fatal(err)
	}
	if pool.Status() != pm.PlanArchived {
		t.Fatalf("builtin pool status=%s, want archived after project archive", pool.Status())
	}
}

// TestBuiltinPool_DispatchWakesAssignedMember is the F1 fix (issue-ca51e07c): an
// ASSIGNED built-in-pool member, when dispatched (the NodeReady→NodeDispatched
// transition that makes it runnable), emits exactly ONE pm.task.assigned so the
// EXISTING DispatchWakeProjector pushes agent.work_available to its assignee —
// closing the "selected into pool but the assignee is never auto-woken" gap the PD
// hit in the v2.21.0 deployed run. Idempotent: a second dispatch sweep re-emits
// nothing (the node has left the ready-set), so there is no double-wake loop.
func TestBuiltinPool_DispatchWakesAssignedMember(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	pool := findBuiltinPlan(t, h, pid)
	// seedAssignedTask assigns "agent:bot" then selects into the pool, draining — so
	// the assign event from seeding is already processed; any pm.task.assigned counted
	// after the sweep below is emitted BY the dispatch, not the seed.
	tid := h.seedAssignedTask(t, pid, pool.ID(), "pool work", "agent:bot")
	h.drain(t)

	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatalf("ReconcileRunningPlans: %v", err)
	}
	if got := assignedEventCountForTask(t, h, tid); got != 1 {
		t.Fatalf("pm.task.assigned emitted on pool dispatch = %d, want 1 (the assignee wake)", got)
	}
	// The emitted wake event carries the right assignee.
	ob := outboxsql.NewOutboxRepo(h.svc.db)
	evs, _ := ob.FetchUnprocessed(h.ctx, 1000)
	for _, e := range evs {
		if e.EventType != EvtTaskAssigned {
			continue
		}
		var pl taskEventPayload
		_ = json.Unmarshal([]byte(e.Payload), &pl)
		if pl.TaskID == string(tid) && pl.Assignee != "agent:bot" {
			t.Fatalf("pool-dispatch wake event assignee=%q, want agent:bot", pl.Assignee)
		}
	}
	// The dispatched, assigned member is now runnable (the wake's runnable gate passes).
	if err := h.svc.EnsureTaskRunnable(h.ctx, tid); err != nil {
		t.Fatalf("dispatched assigned pool member should be runnable: %v", err)
	}
	// No double-wake: drain the wake, sweep again → the node is already NodeDispatched
	// (out of the ready-set), so nothing new is emitted.
	h.drain(t)
	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatalf("ReconcileRunningPlans (2nd): %v", err)
	}
	if got := assignedEventCountForTask(t, h, tid); got != 0 {
		t.Fatalf("2nd dispatch sweep re-emitted %d pm.task.assigned, want 0 (no double-wake)", got)
	}
}

// TestBuiltinPool_DispatchUnassignedMember_NoWake guards the other side: an
// UNASSIGNED pool member must NOT emit pm.task.assigned on dispatch — there is no
// specific assignee to push-wake. It stays claimable (pull), and the auto-assign
// path wakes whoever it is later assigned to (that assign emits pm.task.assigned on
// the already-dispatched, runnable member). This prevents a false wake.
func TestBuiltinPool_DispatchUnassignedMember_NoWake(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	pool := findBuiltinPlan(t, h, pid)
	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "ownerless pool work", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.SelectTaskIntoPlan(h.ctx, pool.ID(), tid, "user:a"); err != nil {
		t.Fatalf("SelectTaskIntoPlan: %v", err)
	}
	h.drain(t)

	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatalf("ReconcileRunningPlans: %v", err)
	}
	if got := assignedEventCountForTask(t, h, tid); got != 0 {
		t.Fatalf("unassigned pool dispatch emitted %d pm.task.assigned, want 0 (no false wake)", got)
	}
}

// ADR-0047 §-1 #3: TaskClaimableByID — a backlog task (no plan) is never claimable.
func TestADR47_TaskClaimableByID_BacklogFalse(t *testing.T) {
	svc, _, _, _, _, ctx := planSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, err := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "backlog", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	claimable, err := svc.TaskClaimableByID(ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if claimable {
		t.Fatal("a backlog task (no plan) must not be claimable")
	}
}

// TaskClaimableByID must not report stale dispatched nodes as claimable after
// their structured plan stops. start_task uses EnsureTaskRunnable, so the read
// model must not advertise work that the write gate will reject.
func TestADR47_TaskClaimableByID_StoppedPlanFalse(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "p", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	tid := h.seedAssignedTask(t, pid, planID, "work", "agent:m1")
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	if _, err := h.svc.AdvancePlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan: %v", err)
	}
	if err := h.svc.EnsureTaskRunnable(h.ctx, tid); err != nil {
		t.Fatalf("running dispatched task should be runnable before stop: %v", err)
	}
	if err := h.svc.StopPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("StopPlan: %v", err)
	}
	if err := h.svc.EnsureTaskRunnable(h.ctx, tid); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("stopped-plan task runnable = %v, want ErrTaskNotRunnable", err)
	}
	claimable, err := h.svc.TaskClaimableByID(h.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if claimable {
		t.Fatal("stopped-plan task must not be reported claimable")
	}
}
