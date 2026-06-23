package service

import (
	"errors"
	"testing"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// T130 — the open→running invariant (EnsureTaskRunnable + the AgentTaskRunGate
// port). A task may run ONLY as a real (non-builtin) Plan node or a DISPATCHED
// Assignment-Pool member; everything else is backlog and must be rejected. These
// guard the direct-assign→start_work path the T83 claim guard did not cover.

// Backlog (no plan at all) is NOT runnable.
func TestEnsureTaskRunnable_BacklogNoPlan_Rejected(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "backlog", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.EnsureTaskRunnable(h.ctx, tid); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("backlog runnable = %v, want ErrTaskNotRunnable", err)
	}
}

// A built-in pool task that has NOT been dispatched (selected but not reconciled)
// is still backlog — the built-in plan is NOT a "real plan" (requirement 2).
func TestEnsureTaskRunnable_BuiltinNotDispatched_Rejected(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	pool := findBuiltinPlan(t, h, pid)
	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "pool-pending", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.SelectTaskIntoPlan(h.ctx, pool.ID(), tid, "user:a"); err != nil {
		t.Fatalf("SelectTaskIntoPlan: %v", err)
	}
	// NOTE: no ReconcileRunningPlans → the node is `ready`, not `dispatched`.
	if err := h.svc.EnsureTaskRunnable(h.ctx, tid); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("builtin-not-dispatched runnable = %v, want ErrTaskNotRunnable", err)
	}
}

// A DISPATCHED built-in pool member IS runnable (it is in the Assignment Pool).
func TestEnsureTaskRunnable_DispatchedPoolMember_OK(t *testing.T) {
	h := planAdvanceSetup(t)
	_, tid := dispatchedPoolTask(t, h, "org-1", "P")
	if err := h.svc.EnsureTaskRunnable(h.ctx, tid); err != nil {
		t.Fatalf("dispatched pool member runnable = %v, want nil", err)
	}
}

// A real (non-builtin) structured-plan node whose plan is RUNNING and whose DAG
// dependencies are satisfied IS runnable. T329 (issue-9d4b3895 §13.A): being a plan
// member is NO LONGER sufficient — the dependency/plan-state gate now governs start
// too (the pre-fix unconditional "return nil" was the 抢跑 bug; deeper dependency
// coverage lives in dispatch_gate_t329_test.go).
func TestEnsureTaskRunnable_RealPlanNode_OK(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "structured", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	// A single assigned node with no upstream → ready once the plan is running.
	tid := h.seedAssignedTask(t, pid, planID, "node", "user:x")
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	h.drain(t)
	if err := h.svc.EnsureTaskRunnable(h.ctx, tid); err != nil {
		t.Fatalf("ready node of a running plan runnable = %v, want nil", err)
	}
}

// T329: a real-plan node whose plan is still DRAFT is NOT runnable (the dispatch/
// start gate respects plan run-state). This pins the behavior the pre-fix gate got
// wrong (it returned nil for any structured-plan member regardless of plan state).
func TestEnsureTaskRunnable_RealPlanNode_DraftPlan_Rejected(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "node", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "structured", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.SelectTaskIntoPlan(h.ctx, planID, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.EnsureTaskRunnable(h.ctx, tid); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("node of a draft plan runnable = %v, want ErrTaskNotRunnable", err)
	}
}

// The AgentTaskRunGate port resolves the work item's task ref and translates the
// pm sentinel to the agent-BC sentinel the start_work HTTP layer maps. Backlog →
// agentpkg.ErrTaskNotRunnable; a dispatched pool member → nil.
func TestAgentTaskRunGate_TranslatesAndGates(t *testing.T) {
	h := planAdvanceSetup(t)
	gate := NewAgentTaskRunGate(h.svc)

	// backlog → rejected, mapped to the agent-BC sentinel.
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	backlog, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "backlog", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.EnsureTaskRunnable(h.ctx, "pm://tasks/"+string(backlog)); !errors.Is(err, agentpkg.ErrTaskNotRunnable) {
		t.Fatalf("gate(backlog) = %v, want ErrTaskNotRunnable", err)
	}

	// dispatched pool member → allowed.
	_, runnable := dispatchedPoolTask(t, h, "org-2", "P2")
	if err := gate.EnsureTaskRunnable(h.ctx, "pm://tasks/"+string(runnable)); err != nil {
		t.Fatalf("gate(pool member) = %v, want nil", err)
	}

	// a non-task ref is left to the caller (defensive no-op, not an error).
	if err := gate.EnsureTaskRunnable(h.ctx, "not-a-task-ref"); err != nil {
		t.Fatalf("gate(non-task ref) = %v, want nil", err)
	}
}

// A RUNNING built-in pool task IS runnable. Re-activating a work item for an
// already-running (in-motion) task — e.g. after the prior work item failed or the
// agent's session died and a fresh queued item was minted — is a resume, NOT a
// backlog start. Regression: a running pool task derives nodeStatus=NodeRunning
// (never NodeDispatched), so the pre-fix gate wedged the requeued work item behind
// a misleading task_backlog_not_actionable.
func TestEnsureTaskRunnable_RunningPoolTask_OK(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, tid := dispatchedPoolTask(t, h, "org-run", "P")
	addMember(t, h, pid, "agent:m1")
	// Claim it: open → running (the in-motion state the requeue lands on).
	if err := h.svc.ClaimPoolTask(h.ctx, tid, "agent:m1"); err != nil {
		t.Fatalf("ClaimPoolTask (open→running): %v", err)
	}
	if err := h.svc.EnsureTaskRunnable(h.ctx, tid); err != nil {
		t.Fatalf("running pool task runnable = %v, want nil", err)
	}
	// The port adapter must agree (it is what start_work calls).
	gate := NewAgentTaskRunGate(h.svc)
	if err := gate.EnsureTaskRunnable(h.ctx, "pm://tasks/"+string(tid)); err != nil {
		t.Fatalf("gate(running pool task) = %v, want nil", err)
	}
}
