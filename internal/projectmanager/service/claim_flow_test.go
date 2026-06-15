package service

import (
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// dispatchedPoolTask seeds a project (auto built-in pool), a backlog task,
// selects it into the pool, and reconciles so the node becomes `dispatched`
// (open-claimable). Returns (projectID, taskID).
func dispatchedPoolTask(t *testing.T, h *planAdvanceHarness, org, projName string) (pm.ProjectID, pm.TaskID) {
	t.Helper()
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: org, Name: projName, CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	pool := findBuiltinPlan(t, h, pid)
	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "pool task", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.SelectTaskIntoPlan(h.ctx, pool.ID(), tid, "user:a"); err != nil {
		t.Fatalf("SelectTaskIntoPlan: %v", err)
	}
	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatalf("ReconcileRunningPlans: %v", err)
	}
	return pid, tid
}

func addMember(t *testing.T, h *planAdvanceHarness, pid pm.ProjectID, ref pm.IdentityRef) {
	t.Helper()
	if _, err := h.svc.AddProjectMember(h.ctx, AddProjectMemberCommand{ProjectID: pid, IdentityID: ref, Actor: "user:a"}); err != nil {
		t.Fatalf("AddProjectMember %s: %v", ref, err)
	}
}

// 4.1: a backlog task (no plan) is never claimable from the pool.
func TestClaimPoolTask_BacklogRejected(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "backlog", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	addMember(t, h, pid, "agent:m1")
	if err := h.svc.ClaimPoolTask(h.ctx, tid, "agent:m1"); err != pm.ErrTaskNotClaimable {
		t.Fatalf("backlog claim = %v, want ErrTaskNotClaimable", err)
	}
}

// 4.2: a project-member agent claims an open dispatched pool task → assignee set
// to the claimer, status open→running.
func TestClaimPoolTask_MemberClaimsOpenPoolTask(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, tid := dispatchedPoolTask(t, h, "org-1", "P")
	addMember(t, h, pid, "agent:m1")

	if err := h.svc.ClaimPoolTask(h.ctx, tid, "agent:m1"); err != nil {
		t.Fatalf("claim = %v, want nil", err)
	}
	got, err := h.svc.GetTask(h.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if got.Assignee() != "agent:m1" {
		t.Fatalf("assignee=%q, want agent:m1", got.Assignee())
	}
	if got.Status() != pm.TaskRunning {
		t.Fatalf("status=%s, want running", got.Status())
	}
}

// 4.3: a NON-member agent cannot claim — fail-closed via membership (the edge maps
// this to an opaque 403/404 so existence is not leaked).
func TestClaimPoolTask_NonMemberRejected(t *testing.T) {
	h := planAdvanceSetup(t)
	_, tid := dispatchedPoolTask(t, h, "org-1", "P")
	// agent:outsider is NOT added as a project member.
	if err := h.svc.ClaimPoolTask(h.ctx, tid, "agent:outsider"); err != ErrNotMember {
		t.Fatalf("non-member claim = %v, want ErrNotMember", err)
	}
	// and the task stays unclaimed.
	got, _ := h.svc.GetTask(h.ctx, tid)
	if got.Assignee() != "" || got.Status() != pm.TaskOpen {
		t.Fatalf("task mutated by rejected claim: assignee=%q status=%s", got.Assignee(), got.Status())
	}
}

// 4.4: once claimed, a second agent's claim of the SAME task loses → already
// claimed (the ClaimIfUnassigned CAS is the true-concurrency guard; sequentially
// the second claim sees the now-assigned row).
func TestClaimPoolTask_SecondClaimLoses(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, tid := dispatchedPoolTask(t, h, "org-1", "P")
	addMember(t, h, pid, "agent:m1")
	addMember(t, h, pid, "agent:m2")

	if err := h.svc.ClaimPoolTask(h.ctx, tid, "agent:m1"); err != nil {
		t.Fatalf("first claim = %v, want nil", err)
	}
	if err := h.svc.ClaimPoolTask(h.ctx, tid, "agent:m2"); err != pm.ErrTaskAlreadyClaimed {
		t.Fatalf("second claim = %v, want ErrTaskAlreadyClaimed", err)
	}
	got, _ := h.svc.GetTask(h.ctx, tid)
	if got.Assignee() != "agent:m1" {
		t.Fatalf("assignee=%q, want agent:m1 (first claimer keeps it)", got.Assignee())
	}
}

// 4.5: a structured-plan node is NOT open-claimable from the pool path (only the
// built-in pool is open-claim; structured plans stay assignee-gated).
func TestClaimPoolTask_StructuredPlanRejected(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "structured", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "structured plan", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.SelectTaskIntoPlan(h.ctx, planID, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	addMember(t, h, pid, "agent:m1")
	if err := h.svc.ClaimPoolTask(h.ctx, tid, "agent:m1"); err != pm.ErrTaskNotClaimable {
		t.Fatalf("structured-plan claim = %v, want ErrTaskNotClaimable", err)
	}
}

// 4.6: holding cap — an agent holds at most N (default 3) concurrent claimed pool
// tasks; the (N+1)th is rejected; completing one frees a slot.
func TestClaimPoolTask_HoldingCap(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	pool := findBuiltinPlan(t, h, pid)
	addMember(t, h, pid, "agent:m1")

	mkPoolTask := func() pm.TaskID {
		tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "t", CreatedBy: "user:a"})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.svc.SelectTaskIntoPlan(h.ctx, pool.ID(), tid, "user:a"); err != nil {
			t.Fatal(err)
		}
		return tid
	}
	ids := []pm.TaskID{mkPoolTask(), mkPoolTask(), mkPoolTask(), mkPoolTask()}
	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatal(err)
	}

	// Claim the first 3 → all succeed (cap=3).
	for i := 0; i < 3; i++ {
		if err := h.svc.ClaimPoolTask(h.ctx, ids[i], "agent:m1"); err != nil {
			t.Fatalf("claim #%d = %v, want nil", i+1, err)
		}
	}
	// The 4th exceeds the cap → rejected.
	if err := h.svc.ClaimPoolTask(h.ctx, ids[3], "agent:m1"); err != pm.ErrPoolClaimLimitReached {
		t.Fatalf("claim #4 = %v, want ErrPoolClaimLimitReached", err)
	}
	// Complete one held task → frees a slot → the 4th now claims.
	if err := h.svc.SetTaskStatus(h.ctx, ids[0], pm.TaskCompleted, "agent:m1"); err != nil {
		t.Fatalf("complete held task: %v", err)
	}
	if err := h.svc.ClaimPoolTask(h.ctx, ids[3], "agent:m1"); err != nil {
		t.Fatalf("claim #4 after freeing a slot = %v, want nil", err)
	}
}
