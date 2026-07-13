package service

import (
	"errors"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// tagMergeToMain marks a seeded task as a merge-to-main (ship) task, persisting the
// tag so EnsureTaskRunnable (which loads the task fresh) sees it.
func tagMergeToMain(t *testing.T, h *planAdvanceHarness, id pm.TaskID) {
	t.Helper()
	tk, err := h.tasks.FindByID(h.ctx, id)
	if err != nil {
		t.Fatalf("FindByID(%s): %v", id, err)
	}
	if err := tk.SetTags([]string{pm.TagMergeToMain}, h.clk.Now()); err != nil {
		t.Fatalf("SetTags: %v", err)
	}
	if err := h.tasks.Update(h.ctx, tk); err != nil {
		t.Fatalf("tasks.Update: %v", err)
	}
}

// TestEnsureTaskRunnable_MergeToMain_UngatedNode_Blocked is the P0 (issue-d2f14e0e)
// regression lock: the P67 hole. A merge-to-main node whose ONLY upstream is a plain
// business dep (no acceptance/decision gate) — the exact P67 topology where the Ship
// node ran in parallel with the verdict — is FAIL-CLOSED: not runnable even once its
// plain deps are satisfied and its node derives `ready`. An identical UNtagged node at
// the same position stays runnable, proving (a) the tag+verdict gate is what blocks and
// (b) non-merge nodes are untouched.
func TestEnsureTaskRunnable_MergeToMain_UngatedNode_Blocked(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "ungated-merge", CreatedBy: "user:a"})
	h.drain(t)

	dev := h.seedAssignedTask(t, pid, planID, "Dev", "user:dev")
	mergeTagged := h.seedAssignedTask(t, pid, planID, "Merge", "user:ship")
	mergePlain := h.seedAssignedTask(t, pid, planID, "PlainDownstream", "user:x")
	// Both downstream nodes depend ONLY on Dev (seq) — no acceptance/decision gate.
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: mergeTagged, ToTaskID: dev, Kind: pm.EdgeSeq})
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: mergePlain, ToTaskID: dev, Kind: pm.EdgeSeq})
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	h.drain(t)
	tagMergeToMain(t, h, mergeTagged)

	// Dev completes → both downstream nodes' plain deps are satisfied (node `ready`).
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan #1: %v", err)
	}
	h.setTaskStatus(t, dev, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan #2: %v", err)
	}

	// The merge-to-main node is HARD-BLOCKED despite satisfied deps (no passed verdict
	// upstream) — this is the hole the executor previously walked straight through.
	if err := h.svc.EnsureTaskRunnable(ctx, mergeTagged); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("ungated merge-to-main node runnable = %v, want ErrTaskNotRunnable (P67 hole must be closed)", err)
	}
	// Control: the same-position UNtagged node is runnable — the gate is specific to the
	// merge-to-main tag and does not regress ordinary downstream dispatch.
	if err := h.svc.EnsureTaskRunnable(ctx, mergePlain); err != nil {
		t.Fatalf("plain downstream node runnable = %v, want nil (non-merge unaffected)", err)
	}
}

// TestEnsureTaskRunnable_MergeToMain_GatedNode_ReleasedOnlyOnPass proves the gate does
// NOT over-block a correctly-authored merge node: an Integrate node gated behind the
// decision's condition is not runnable while the verdict is unresolved, and becomes
// runnable ONLY after the decision records a PASS (condition Completed + outcome
// "success"). This is the legitimate flow the P0 gate must still permit.
func TestEnsureTaskRunnable_MergeToMain_GatedNode_ReleasedOnlyOnPass(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "gated-merge", CreatedBy: "user:a"})
	h.drain(t)
	dev, rev, dec, integ := buildGraphCycle(t, h, pid, planID)
	tagMergeToMain(t, h, integ) // Integrate = the merge-to-main node, gated behind Decision.

	// Walk Dev → Review → Decision.
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan #1: %v", err)
	}
	h.setTaskStatus(t, dev, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan after Dev: %v", err)
	}
	h.setTaskStatus(t, rev, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan after Review: %v", err)
	}

	// Decision unresolved → merge node not runnable (verdict gate holds).
	if err := h.svc.EnsureTaskRunnable(ctx, integ); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("merge node runnable before verdict = %v, want ErrTaskNotRunnable", err)
	}

	// Decision passes → condition resolves to success → merge node released.
	if err := h.svc.RecordDecisionOutcome(ctx, dec, "pass", "user:a"); err != nil {
		t.Fatalf("RecordDecisionOutcome(pass): %v", err)
	}
	h.setTaskStatus(t, dec, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan release: %v", err)
	}
	if err := h.svc.EnsureTaskRunnable(ctx, integ); err != nil {
		t.Fatalf("merge node runnable after PASS verdict = %v, want nil", err)
	}
}

// TestEnsureTaskRunnable_MergeToMain_RejectVerdict_Blocked proves a REJECT verdict does
// not release the merge node: after the decision rejects, the condition stays unresolved
// (bounded loopback) and the merge-to-main node remains non-runnable — the executor
// cannot merge on a rejected verdict.
func TestEnsureTaskRunnable_MergeToMain_RejectVerdict_Blocked(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "reject-merge", CreatedBy: "user:a"})
	h.drain(t)
	dev, rev, dec, integ := buildGraphCycle(t, h, pid, planID)
	tagMergeToMain(t, h, integ)

	// Walk to Decision.
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan #1: %v", err)
	}
	h.setTaskStatus(t, dev, pm.TaskCompleted)
	_, _ = h.svc.AdvancePlan(ctx, planID, "user:a")
	h.setTaskStatus(t, rev, pm.TaskCompleted)
	_, _ = h.svc.AdvancePlan(ctx, planID, "user:a")

	// Decision REJECTS → condition reopens (unresolved) → merge node stays blocked.
	if err := h.svc.RecordDecisionOutcome(ctx, dec, "reject", "user:a"); err != nil {
		t.Fatalf("RecordDecisionOutcome(reject): %v", err)
	}
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan after reject: %v", err)
	}
	if err := h.svc.EnsureTaskRunnable(ctx, integ); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("merge node runnable after REJECT verdict = %v, want ErrTaskNotRunnable", err)
	}
}

// TestEnsureTaskRunnable_MergeToMain_BuiltinPool_Blocked proves a merge-to-main task in a
// built-in Assignment Pool (no orchestration graph → no verdict can be proved) is
// fail-closed: a merge-to-main action must be authored as a gated node in a structured
// plan, never run as ownerless/flat pool work.
func TestEnsureTaskRunnable_MergeToMain_BuiltinPool_Blocked(t *testing.T) {
	h, _ := planGraphSetup(t)
	pid, tid := dispatchedPoolTask(t, h, "org-pool", "P")
	// Without the tag, a dispatched pool member is runnable (baseline).
	if err := h.svc.EnsureTaskRunnable(h.ctx, tid); err != nil {
		t.Fatalf("dispatched pool member runnable (baseline) = %v, want nil", err)
	}
	// Tag it merge-to-main → fail-closed (no structured verdict gate possible).
	tagMergeToMain(t, h, tid)
	_ = pid
	if err := h.svc.EnsureTaskRunnable(h.ctx, tid); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("merge-to-main pool member runnable = %v, want ErrTaskNotRunnable (fail-closed)", err)
	}
}
