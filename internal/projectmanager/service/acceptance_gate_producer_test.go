package service

import (
	"errors"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// hasMergeGateTag reports whether the stored task carries the auto-stamped gate tag.
func hasMergeGateTag(t *testing.T, h *planAdvanceHarness, id pm.TaskID) bool {
	t.Helper()
	tk, err := h.tasks.FindByID(h.ctx, id)
	if err != nil {
		t.Fatalf("FindByID(%s): %v", id, err)
	}
	return pm.HasTag(tk.Tags(), pm.TagMergeToMain)
}

// TestBuildPlanGraph_AutoStampsShipNode_AndHardGates is the PRODUCER regression
// (issue-d2f14e0e, pd decision-gate): the plan builder must AUTO-STAMP TagMergeToMain
// onto a ship / merge-to-main node so the run-gate is un-bypassable WITHOUT the author
// tagging it manually — closing the "consumer wired, source not provisioned" gap that
// left b5ddb42e's gate opt-in / inert. A ship node detected purely by TITLE ("Merge to
// main", NO manual tag) is stamped at StartPlan and then hard-rejected by
// EnsureTaskRunnable while its acceptance verdict has not passed — even though its plain
// Dev dep is satisfied (the P67 topology). A same-position non-ship node stays untagged
// and runnable (zero-regression).
func TestBuildPlanGraph_AutoStampsShipNode_AndHardGates(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "autostamp", CreatedBy: "user:a"})
	h.drain(t)

	dev := h.seedAssignedTask(t, pid, planID, "Dev", "user:dev")
	// Ship node detected by TITLE only — NO manual tag — using the repo's MAINSTREAM
	// arrow-style naming (the actual P67 accident title shape). This is the production
	// path pd requires the lock to exercise (not a hand-SetTags'd task).
	ship := h.seedAssignedTask(t, pid, planID, "merge(release): dev/team-phase1 → main for v2.44.0", "user:ship")
	plain := h.seedAssignedTask(t, pid, planID, "Downstream work", "user:x")
	// Both downstream nodes depend ONLY on Dev (seq) — no acceptance/decision gate: the
	// exact P67 topology where a Ship node races the verdict.
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: ship, ToTaskID: dev, Kind: pm.EdgeSeq})
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: plain, ToTaskID: dev, Kind: pm.EdgeSeq})
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	h.drain(t)

	// PRODUCER proven: the builder auto-stamped the ship node; the plain node stayed clean.
	if !hasMergeGateTag(t, h, ship) {
		t.Fatal("plan builder did NOT auto-stamp TagMergeToMain on the ship node (gate is inert — the P0 producer gap)")
	}
	if hasMergeGateTag(t, h, plain) {
		t.Fatal("non-ship node was stamped TagMergeToMain (false positive — must stay ungated)")
	}

	// Dev completes → both downstream nodes' plain deps satisfied (node `ready`).
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan #1: %v", err)
	}
	h.setTaskStatus(t, dev, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan #2: %v", err)
	}

	// The auto-stamped ship node is HARD-BLOCKED despite satisfied deps (no passed verdict).
	if err := h.svc.EnsureTaskRunnable(ctx, ship); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("auto-stamped ship node runnable = %v, want ErrTaskNotRunnable (un-bypassable gate)", err)
	}
	// The plain node is runnable — the gate is specific to ship nodes.
	if err := h.svc.EnsureTaskRunnable(ctx, plain); err != nil {
		t.Fatalf("plain downstream node runnable = %v, want nil (non-ship unaffected)", err)
	}
}

// TestBuildPlanGraph_AutoStampedShipNode_ReleasedOnPass proves the auto-stamped gate is
// not a dead end: a correctly-gated ship node (title-detected, wired conditional behind
// the Decision) is blocked while the verdict is unresolved and becomes runnable ONLY
// after a PASS — the legitimate flow via the production auto-stamp path.
func TestBuildPlanGraph_AutoStampedShipNode_ReleasedOnPass(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "gated-autostamp", CreatedBy: "user:a"})
	h.drain(t)

	dev := h.seedAssignedTask(t, pid, planID, "Dev", "user:dev")
	rev := h.seedAssignedTask(t, pid, planID, "Review", "user:rev")
	dec := h.seedAssignedTask(t, pid, planID, "Decision", "user:pd")
	ship := h.seedAssignedTask(t, pid, planID, "Merge to main branch", "user:ship") // title-detected
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: rev, ToTaskID: dev, Kind: pm.EdgeSeq})
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: dec, ToTaskID: rev, Kind: pm.EdgeSeq})
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: ship, ToTaskID: dec, Kind: pm.EdgeConditional, When: "pass"})
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: dec, ToTaskID: dev, Kind: pm.EdgeLoopback, When: "reject", MaxRounds: 2})
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	h.drain(t)

	if !hasMergeGateTag(t, h, ship) {
		t.Fatal("gated ship node was not auto-stamped")
	}

	// Walk Dev → Review → Decision.
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan #1: %v", err)
	}
	h.setTaskStatus(t, dev, pm.TaskCompleted)
	_, _ = h.svc.AdvancePlan(ctx, planID, "user:a")
	h.setTaskStatus(t, rev, pm.TaskCompleted)
	_, _ = h.svc.AdvancePlan(ctx, planID, "user:a")

	// Verdict unresolved → ship node blocked.
	if err := h.svc.EnsureTaskRunnable(ctx, ship); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("ship node runnable before verdict = %v, want ErrTaskNotRunnable", err)
	}

	// PASS → condition resolves to success → ship node released.
	if err := h.svc.RecordDecisionOutcome(ctx, dec, "pass", "user:a"); err != nil {
		t.Fatalf("RecordDecisionOutcome(pass): %v", err)
	}
	h.setTaskStatus(t, dec, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan release: %v", err)
	}
	if err := h.svc.EnsureTaskRunnable(ctx, ship); err != nil {
		t.Fatalf("ship node runnable after PASS = %v, want nil", err)
	}
}
