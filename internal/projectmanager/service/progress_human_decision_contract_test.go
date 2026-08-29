package service

import (
	"errors"
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

func TestBlockedOnRefreshReleasesStaleHumanDecisionProgressHold(t *testing.T) {
	h := planAdvanceSetup(t)
	h.svc.progress = pmsql.NewProgressControlRepo(h.db)
	_, planID, decision, _ := seedRootDecisionPlan(t, h, "stale-human-decision-hold", "user:a")

	snap, err := h.svc.progress.SnapshotPlan(h.ctx, planID, h.clk.Now())
	if err != nil {
		t.Fatal(err)
	}
	var staleHoldID string
	for _, hold := range snap.OpenHolds {
		if hold.TaskID == decision && hold.ReasonKind == "blocked_on" && hold.ReasonID == string(pm.WaitHumanDecision)+":"+string(decision) {
			staleHoldID = hold.ID
			break
		}
	}
	if staleHoldID == "" {
		t.Fatalf("seed did not materialize human_decision hold: %+v", snap.OpenHolds)
	}
	task, err := h.svc.tasks.FindByID(h.ctx, decision)
	if err != nil {
		t.Fatal(err)
	}
	if err := task.Start(h.clk.Now()); err != nil {
		t.Fatal(err)
	}
	task.SetDelivery(&pm.Delivery{
		Source:      "manual_recovery",
		Probed:      true,
		Pushed:      true,
		Branch:      "ac-exec/task/exec",
		HeadSHA:     "0123456789abcdef0123456789abcdef01234567",
		BaseRef:     "origin/main",
		BaseKnown:   true,
		AheadOfBase: 1,
		Evidence:    "go test ./...: PASS",
		Reason:      "manual recovery",
	})
	if err := h.svc.tasks.Update(h.ctx, task); err != nil {
		t.Fatal(err)
	}

	now := h.clk.Now()
	stale := pm.BlockedOn{PlanID: planID, TaskID: decision, NodeID: task.NodeID(), WaitType: pm.WaitHumanDecision, WaitKeys: []string{string(decision)}}
	if _, err := h.svc.progress.RecordWake(h.ctx, pm.ProgressWake{
		ID: "wake-stale-human", PlanID: planID, TaskID: decision, NodeID: task.NodeID(),
		OwnerRef: "user:a", OwnerDisplay: "user:a", Reason: "a human records the decision outcome",
		Status: pm.ProgressWakeDelivered, IdempotencyKey: blockedOnWakeKey(planID, decision, blockedOnReasonID(stale)),
		RequestedAt: now.Add(-time.Hour), DeliveredAt: now.Add(-time.Hour),
		AckDeadline: now.Add(-30 * time.Minute), MaxHoldDuration: time.Hour,
		NextEscalationAt: now.Add(-30 * time.Minute), OrganizationOwnerRef: "role:operational-owner",
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.ReconcileProgressControl(h.ctx, 100); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.CompleteTask(h.ctx, decision, "user:a"); !errors.Is(err, pm.ErrProgressHoldOpen) {
		t.Fatalf("stale hold should block before refresh, got %v", err)
	}

	if err := h.plans.UpsertBlockedOn(h.ctx, pm.BlockedOn{
		PlanID:           planID,
		TaskID:           decision,
		NodeID:           task.NodeID(),
		WaitType:         pm.WaitExecutorLiveness,
		WaitKeys:         []string{"user:a"},
		TriggerCondition: "the executor holding the lease stays alive",
		WaitedSince:      now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.progress.ReleaseHoldsByScopedReason(h.ctx, planID, decision, "blocked_on", blockedOnReasonID(stale), "user:a", "preexisting_release", now); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.progress.ResolveOpenObligationsBySourceRef(h.ctx, planID, decision, "user:a", blockedOnSourceFactRef(stale), "preexisting_release", now); err != nil {
		t.Fatal(err)
	}
	bo := blockedOnByTask(t, h, planID)[decision]
	if bo.WaitType != pm.WaitExecutorLiveness {
		t.Fatalf("refreshed wait_type=%q, want executor_liveness: %+v", bo.WaitType, bo)
	}
	if err := h.svc.CompleteTask(h.ctx, decision, "user:a"); err != nil {
		t.Fatalf("complete after stale blocked_on release: %v", err)
	}
	snap, err = h.svc.progress.SnapshotPlan(h.ctx, planID, h.clk.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, hold := range snap.OpenHolds {
		if hold.ID == staleHoldID {
			t.Fatalf("stale hold still open: %+v", hold)
		}
		if hold.ReasonKind == string(pm.ProgressObligationAckWake) && hold.ReasonID == "obl:wake-stale-human" {
			t.Fatalf("stale wake hold still open: %+v", hold)
		}
	}
	for _, obligation := range snap.OpenObligations {
		if obligation.TaskID == decision && obligation.Kind == pm.ObligationHumanDecision && len(obligation.SourceFactRefs) == 1 && obligation.SourceFactRefs[0] == blockedOnSourceFactRef(stale) {
			t.Fatalf("stale obligation still open: %+v", obligation)
		}
		if obligation.TaskID == decision && obligation.Kind == pm.ProgressObligationAckWake && len(obligation.SourceFactRefs) == 1 && obligation.SourceFactRefs[0] == "wake-stale-human" {
			t.Fatalf("stale wake obligation still open: %+v", obligation)
		}
	}
	for _, incident := range snap.OpenIncidents {
		if incident.SourceRef == "wake-stale-human" {
			t.Fatalf("stale wake incident still open: %+v", incident)
		}
	}
}
