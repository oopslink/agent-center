package agentruntime

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/workerdaemon/orchestrator"
)

// (5) A tier-3 (RecoverFresh, workspace-gone) recovery calls the center's reset_task so
// the still-running-under-a-dead-owner task is returned to the pool for a fresh executor.
// Alive=false so enactCancel only cleans residue (no real process kill).
func TestEnactRecover_Fresh_CallsResetTask(t *testing.T) {
	rt, ee, _ := engineForAgent(t, "agent-x")
	attach(rt, ee)
	tc := &scriptedToolCaller{}
	setToolCaller(rt, tc)

	// The supervisor is tracking this task (lease_gc renews on state.CurrentTaskID); the
	// tier-3 reset must stop that renewal, else the lease never lapses (THE-gate root).
	rt.state.CurrentTaskID = "task-42"

	d := execReconcileDecision{
		ExecutorID: "e-gone",
		TaskRef:    "task-42",
		Alive:      false,
		Plan:       orchestrator.RecoveryPlan{Action: orchestrator.RecoverFresh},
	}
	rt.enactRecover(context.Background(), ee, d)

	body, ok := tc.callFor("reset_task")
	if !ok {
		t.Fatalf("tier-3 RecoverFresh must call reset_task; tools seen: %v", tc.toolsSeen())
	}
	if body["task_id"] != "task-42" {
		t.Fatalf("reset_task task_id = %v, want task-42", body["task_id"])
	}
	if body["agent_id"] != "agent-x" {
		t.Fatalf("reset_task agent_id = %v, want agent-x", body["agent_id"])
	}
	// 修1: the owner's tier-3 confirmation must ride along so the center skips the
	// live-lease guard the owner is itself renewing.
	if body["confirmed_dead"] != true {
		t.Fatalf("reset_task confirmed_dead = %v, want true", body["confirmed_dead"])
	}
	// 修2: CurrentTaskID must be cleared so lease_gc stops renewing the dead executor's
	// lease (and won't reclaim the task once it is re-dispatched).
	if rt.state.CurrentTaskID != "" {
		t.Fatalf("tier-3 reset must clear CurrentTaskID, got %q", rt.state.CurrentTaskID)
	}
}

// A RecoverResume/Rerun (tier 1/2, workspace intact) must NOT call reset_task — those
// relaunch the executor in place; only the workspace-gone tier resets the task.
func TestEnactRecover_Resume_NoResetTask(t *testing.T) {
	rt, ee, _ := engineForAgent(t, "agent-x")
	attach(rt, ee)
	tc := &scriptedToolCaller{}
	setToolCaller(rt, tc)

	d := execReconcileDecision{
		ExecutorID: "e-live",
		TaskRef:    "task-42",
		Plan:       orchestrator.RecoveryPlan{Action: orchestrator.RecoverResume, RunnerCmd: []string{"true"}},
	}
	rt.enactRecover(context.Background(), ee, d)

	if _, ok := tc.callFor("reset_task"); ok {
		t.Fatalf("tier-1 resume must NOT call reset_task; tools seen: %v", tc.toolsSeen())
	}
}

// (residual, THE-gate root) The recovery-exhausted escalate must ALSO stop renewing the
// task's lease: it blocks the task (running + blocked_reason, non-terminal) for PD triage,
// so leaving state.CurrentTaskID set would make lease_gc 续租 it forever → the lease never
// lapses → a later MANUAL PD reset (confirmedDead=false) hits ErrLeaseStillLive. Mirror of
// resetRecoveredTask's clear.
func TestEscalateRecoveryExhausted_ClearsCurrentTaskID_T887(t *testing.T) {
	rt, _, _ := engineForAgent(t, "agent-x")
	tc := &scriptedToolCaller{}
	setToolCaller(rt, tc)
	rt.state.CurrentTaskID = "task-77"

	rt.escalateRecoveryExhausted(context.Background(), "task-77", "exec-1", true)

	body, ok := tc.callFor("block_task")
	if !ok {
		t.Fatalf("escalate must call block_task; tools seen: %v", tc.toolsSeen())
	}
	if body["task_id"] != "task-77" {
		t.Fatalf("block_task task_id = %v, want task-77", body["task_id"])
	}
	if rt.state.CurrentTaskID != "" {
		t.Fatalf("escalate must clear CurrentTaskID (stop lease renewal), got %q", rt.state.CurrentTaskID)
	}
}

// Guard: the clear is task-id scoped — an unrelated executor's escalate must NOT clear a
// DIFFERENT task the supervisor is currently tracking.
func TestEscalateRecoveryExhausted_LeavesOtherTask_T887(t *testing.T) {
	rt, _, _ := engineForAgent(t, "agent-x")
	tc := &scriptedToolCaller{}
	setToolCaller(rt, tc)
	rt.state.CurrentTaskID = "task-OTHER"

	rt.escalateRecoveryExhausted(context.Background(), "task-77", "exec-1", false)

	if rt.state.CurrentTaskID != "task-OTHER" {
		t.Fatalf("escalate for task-77 must not clear a different CurrentTaskID, got %q", rt.state.CurrentTaskID)
	}
}
