package service

import (
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

func TestHumanDecisionOwnerReplyDoesNotResolveAndPrerequisiteAutoWakes(t *testing.T) {
	h := planAdvanceSetup(t)
	h.svc.progress = pmsql.NewProgressControlRepo(h.db)
	pid, planID, decision, _ := seedRootDecisionPlan(t, h, "durable-human-decision", "user:a")

	assertHumanOpen := func(stage string) {
		t.Helper()
		snap, err := h.svc.progress.SnapshotPlan(h.ctx, planID, h.clk.Now())
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, o := range snap.OpenObligations {
			if o.TaskID == decision && o.Kind == pm.ObligationHumanDecision {
				found = true
				if o.OwnerRef != "user:a" || o.DeadlineAt.IsZero() || len(o.SourceFactRefs) == 0 {
					t.Fatalf("%s: incomplete owner obligation: %+v", stage, o)
				}
			}
		}
		if !found {
			t.Fatalf("%s: human decision obligation is not open: %+v", stage, snap.OpenObligations)
		}
	}
	assertHumanOpen("blocked")

	// A plan-chat message saying “keep blocked” has no write path into the
	// responsibility ledger; reading the ledger again preserves the obligation.
	assertHumanOpen("owner text only")

	prerequisite, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "upstream", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	owner := "user:a"
	if err := h.svc.BatchUpdateTask(h.ctx, prerequisite, BatchTaskPatch{Assignee: &owner}, "user:a"); err != nil {
		t.Fatal(err)
	}
	nextDeadline := h.clk.Now().Add(30 * time.Minute)
	if err := h.svc.WaitHumanDecisionForPrerequisite(h.ctx, planID, decision, prerequisite, "user:a", "task_required:"+string(prerequisite), nextDeadline); err != nil {
		t.Fatal(err)
	}
	assertHumanOpen("subscribed") // registering a wait is not resolving the owner duty.

	if err := h.svc.StartTask(h.ctx, prerequisite, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.CompleteTask(h.ctx, prerequisite, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.ReconcileProgressControl(h.ctx, 100); err != nil {
		t.Fatal(err)
	}

	snap, err := h.svc.progress.SnapshotPlan(h.ctx, planID, h.clk.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range snap.OpenObligations {
		if o.TaskID == decision {
			t.Fatalf("satisfied prerequisite left old decision obligation open: %+v", o)
		}
	}
	if b, ok := blockedOnByTask(t, h, planID)[decision]; ok && b.WaitType == pm.WaitHumanDecision {
		t.Fatalf("old human_decision projection survived atomic wake: %+v", b)
	}

	// Once released, a real running decision is classified by the next truthful
	// wait, executor_liveness, never by the stale pending-decision projection.
	if err := h.svc.StartTask(h.ctx, decision, "user:a"); err != nil {
		t.Fatal(err)
	}
	running, err := h.svc.tasks.FindByID(h.ctx, decision)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.refreshBlockedOnForTaskState(h.ctx, running); err != nil {
		t.Fatal(err)
	}
	b := blockedOnByTask(t, h, planID)[decision]
	if b.WaitType != pm.WaitExecutorLiveness {
		t.Fatalf("next wait=%q, want executor_liveness after decision release: %+v", b.WaitType, b)
	}
}
