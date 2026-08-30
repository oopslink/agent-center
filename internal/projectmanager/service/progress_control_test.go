package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

func progressServiceFixture(t *testing.T) (*Service, *pmsql.ProgressControlRepo, *clock.FakeClock, *sql.DB, context.Context) {
	t.Helper()
	db := openMigratedTestDB(t)
	clk := clock.NewFakeClock(time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC))
	repo := pmsql.NewProgressControlRepo(db)
	svc := New(Deps{DB: db, Clock: clk, IDGen: idgen.NewGenerator(clk), ProgressControl: repo})
	return svc, repo, clk, db, context.Background()
}

func TestProgressControl_ReconcileExpiredWakeCreatesHoldAndEscalates(t *testing.T) {
	svc, repo, clk, _, ctx := progressServiceFixture(t)
	now := clk.Now()
	_, err := repo.RecordWake(ctx, pm.ProgressWake{
		ID: "wake-1", PlanID: "plan-1", TaskID: "task-1", NodeID: "node-1",
		OwnerRef: "user:owner", OwnerDisplay: "Owner", Reason: "blocked", Status: pm.ProgressWakeDelivered,
		IdempotencyKey: "chain-1", RequestedAt: now, DeliveredAt: now, AckDeadline: now.Add(time.Minute),
		MaxHoldDuration: 2 * time.Minute, NextEscalationAt: now.Add(time.Minute), OrganizationOwnerRef: "role:oncall",
	})
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(90 * time.Second)
	if err := svc.ReconcileProgressControl(ctx, 10); err != nil {
		t.Fatalf("ReconcileProgressControl: %v", err)
	}
	holds, err := repo.ListOpenHoldsByPlan(ctx, "plan-1")
	if err != nil || len(holds) != 1 {
		t.Fatalf("holds = %+v err=%v", holds, err)
	}
	if err := svc.guardTaskProgressHolds(ctx, "task-1", true, false, false); !errors.Is(err, pm.ErrProgressHoldOpen) {
		t.Fatalf("dispatch guard err=%v, want ErrProgressHoldOpen", err)
	}
	if err := svc.AcknowledgeProgressWake(ctx, "wake-1", "user:owner"); err != nil {
		t.Fatalf("AcknowledgeProgressWake: %v", err)
	}
	if err := svc.guardTaskProgressHolds(ctx, "task-1", true, false, false); err != nil {
		t.Fatalf("owner ack should release hold guard, got %v", err)
	}
	_, err = repo.RecordWake(ctx, pm.ProgressWake{
		ID: "wake-2", PlanID: "plan-1", TaskID: "task-1", NodeID: "node-1",
		OwnerRef: "user:owner", OwnerDisplay: "Owner", Reason: "blocked", Status: pm.ProgressWakeDelivered,
		IdempotencyKey: "chain-2", RequestedAt: now, DeliveredAt: now, AckDeadline: now.Add(time.Minute),
		MaxHoldDuration: 2 * time.Minute, NextEscalationAt: now.Add(time.Minute), OrganizationOwnerRef: "role:oncall",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ReconcileProgressControl(ctx, 10); err != nil {
		t.Fatalf("ReconcileProgressControl second wake: %v", err)
	}
	clk.Advance(2 * time.Minute)
	if err := svc.ReconcileProgressControl(ctx, 10); err != nil {
		t.Fatalf("ReconcileProgressControl breach: %v", err)
	}
	snap, err := repo.SnapshotPlan(ctx, "plan-1", clk.Now())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Decision != pm.ProgressDecisionBound || len(snap.OpenHolds) != 1 {
		t.Fatalf("snapshot = %+v, want responsibility_bound with open hold", snap)
	}
	foundP0 := false
	for _, inc := range snap.OpenIncidents {
		if inc.Kind == pm.ProgressIncidentHoldSLOBreach && inc.Severity == "P0" {
			foundP0 = true
		}
	}
	if !foundP0 {
		t.Fatalf("snapshot incidents = %+v, want hold_slo_breached P0", snap.OpenIncidents)
	}
}

func TestProgressControl_AckWakeExhaustsObligationIncidentHoldAndStoresFact(t *testing.T) {
	svc, repo, clk, db, ctx := progressServiceFixture(t)
	now := clk.Now()
	_, err := repo.RecordWake(ctx, pm.ProgressWake{
		ID: "wake-ack", PlanID: "plan-1", TaskID: "task-1", NodeID: "node-1",
		OwnerRef: "user:owner", OwnerDisplay: "Owner", Reason: "blocked", Status: pm.ProgressWakeDelivered,
		IdempotencyKey: "ack-chain", RequestedAt: now, DeliveredAt: now, AckDeadline: now.Add(time.Minute),
		MaxHoldDuration: time.Hour, NextEscalationAt: now.Add(time.Minute), OrganizationOwnerRef: "role:oncall",
	})
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(2 * time.Minute)
	if err := svc.ReconcileProgressControl(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if err := svc.AcknowledgeProgressWake(ctx, "wake-ack", "user:intruder"); err != nil {
		t.Fatalf("wrong actor ack should be a no-op, got err %v", err)
	}
	if snap, err := repo.SnapshotPlan(ctx, "plan-1", clk.Now()); err != nil || snap.Decision != pm.ProgressDecisionBound {
		t.Fatalf("wrong actor released chain: snapshot=%+v err=%v", snap, err)
	}
	if err := svc.AcknowledgeProgressWake(ctx, "wake-ack", "user:owner"); err != nil {
		t.Fatalf("owner ack: %v", err)
	}
	snap, err := repo.SnapshotPlan(ctx, "plan-1", clk.Now())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Decision != pm.ProgressDecisionVerified || len(snap.OpenHolds) != 0 || len(snap.OpenObligations) != 0 || len(snap.OpenIncidents) != 0 {
		t.Fatalf("ack did not exhaust chain: %+v", snap)
	}
	var fact string
	if err := db.QueryRowContext(ctx, `SELECT ack_fact_ref FROM pm_progress_wakes WHERE id='wake-ack'`).Scan(&fact); err != nil {
		t.Fatal(err)
	}
	if fact != pm.ProgressWakeAcknowledged+":wake-ack" {
		t.Fatalf("ack_fact_ref=%q, want durable wake ack fact", fact)
	}
}

func TestProgressHold_GatesFreshStartButNotInFlightResume(t *testing.T) {
	h := planAdvanceSetup(t)
	h.svc.progress = pmsql.NewProgressControlRepo(h.db)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "held", CreatedBy: "user:a"})
	h.drain(t)
	taskID := h.seedAssignedTask(t, pid, planID, "Decision", "user:a")
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	now := h.clk.Now()
	_, err := h.svc.progress.UpsertHold(ctx, pm.ProgressHold{
		ID: "hold-start", PlanID: planID, TaskID: taskID, NodeID: "node",
		ReasonKind: string(pm.WaitHumanDecision), ReasonID: "decision:" + string(taskID),
		OwnerRef: "user:a", OwnerDisplay: "user:a", EnteredAt: now,
		HoldAckDeadline: now.Add(time.Minute), MaxHoldDuration: time.Hour, NextEscalationAt: now.Add(time.Minute),
		BlocksDispatch: true, BlocksAcceptance: true, BlocksCompletion: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.EnsureTaskRunnable(ctx, taskID); !errors.Is(err, pm.ErrProgressHoldOpen) {
		t.Fatalf("fresh run gate err=%v, want ErrProgressHoldOpen", err)
	}
	gate := NewAgentTaskRunGate(h.svc)
	if err := gate.EnsureTaskRunnable(ctx, "pm://tasks/"+string(taskID)); !errors.Is(err, agentpkg.ErrTaskNotRunnable) {
		t.Fatalf("agent gate err=%v, want ErrTaskNotRunnable", err)
	}
	if err := h.svc.StartTask(ctx, taskID, "user:a"); !errors.Is(err, pm.ErrProgressHoldOpen) {
		t.Fatalf("StartTask err=%v, want ErrProgressHoldOpen", err)
	}
	if err := h.svc.RecordProgressDecision(ctx, planID, taskID, "user:a", "decision-1"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.StartTask(ctx, taskID, "user:a"); err != nil {
		t.Fatalf("StartTask after decision fact: %v", err)
	}
	_, err = h.svc.progress.UpsertHold(ctx, pm.ProgressHold{
		ID: "hold-running", PlanID: planID, TaskID: taskID, ReasonKind: string(pm.WaitHumanDecision), ReasonID: "decision:again",
		OwnerRef: "user:a", OwnerDisplay: "user:a", EnteredAt: now, HoldAckDeadline: now.Add(time.Minute),
		MaxHoldDuration: time.Hour, NextEscalationAt: now.Add(time.Minute), BlocksDispatch: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.EnsureTaskRunnable(ctx, taskID); err != nil {
		t.Fatalf("running task must not be pseudo-stopped by hold, got %v", err)
	}
}

func TestProgressHold_BlocksAuthoritativeDecisionPassToken(t *testing.T) {
	h := planAdvanceSetup(t)
	h.svc.progress = pmsql.NewProgressControlRepo(h.db)
	_, planID, decision, _ := seedRootDecisionPlan(t, h, "held-pass-token", "user:a")
	now := h.clk.Now()
	if _, err := h.svc.progress.UpsertHold(h.ctx, pm.ProgressHold{
		ID: "hold-pass-token", PlanID: planID, TaskID: decision,
		ReasonKind: string(pm.WaitHumanDecision), ReasonID: "decision:" + string(decision),
		OwnerRef: "user:a", OwnerDisplay: "user:a", EnteredAt: now,
		HoldAckDeadline: now.Add(time.Minute), MaxHoldDuration: time.Hour,
		NextEscalationAt: now.Add(time.Minute), BlocksAcceptance: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.RecordDecisionOutcome(h.ctx, decision, "pass", "user:a"); !errors.Is(err, pm.ErrProgressHoldOpen) {
		t.Fatalf("pass token err=%v, want ErrProgressHoldOpen", err)
	}
	if err := h.svc.RecordProgressDecision(h.ctx, planID, decision, "user:a", "decision-fact"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.RecordDecisionOutcome(h.ctx, decision, "pass", "user:a"); err != nil {
		t.Fatalf("pass after executable decision fact: %v", err)
	}
}

func TestProgressHold_MaterializesOnlyForMissingExecutableReleaseFact(t *testing.T) {
	h := planAdvanceSetup(t)
	h.svc.progress = pmsql.NewProgressControlRepo(h.db)
	_, planID, _, blocked := seedBlockedPlanAB(t, h, "no-upstream-hold")
	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatal(err)
	}
	bo := blockedOnByTask(t, h, planID)[blocked]
	if bo.WaitType != pm.WaitUpstreamCompletion {
		t.Fatalf("wait_type=%q, want upstream_completion", bo.WaitType)
	}
	snap, err := h.svc.progress.SnapshotPlan(h.ctx, planID, h.clk.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.OpenHolds) != 0 {
		t.Fatalf("upstream wait materialized progress_hold without missing executable release fact: %+v", snap.OpenHolds)
	}
	h.clk.Advance(defaultHoldAckDeadline + time.Minute)
	if err := h.svc.ReconcileProgressControl(h.ctx, 10); err != nil {
		t.Fatal(err)
	}
	snap, err = h.svc.progress.SnapshotPlan(h.ctx, planID, h.clk.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.OpenHolds) != 0 {
		t.Fatalf("expired upstream wake materialized progress_hold for not-yet-runnable node: %+v", snap.OpenHolds)
	}
}

func TestProgressControl_TimedOutBlockedOnRequiresDispositionUntilPlanProgresses(t *testing.T) {
	h, _ := planGraphSetup(t)
	h.svc.progress = pmsql.NewProgressControlRepo(h.db)
	h.svc.deadlinePolicy = pm.DeadlinePolicy{
		Default: pm.WaitDeadline{Timeout: time.Hour, OnTimeout: pm.TimeoutEscalate},
	}
	_, planID, upstream, blocked := seedBlockedPlanAB(t, h, "blocked-disposition")

	h.clk.Advance(2 * time.Hour)
	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatalf("timeout sweep: %v", err)
	}
	snap, err := h.svc.progress.SnapshotPlan(h.ctx, planID, h.clk.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !hasProgressObligation(snap, blocked, pm.ObligationPlanProgress) {
		t.Fatalf("snapshot obligations=%+v, want plan_progress for timed-out blocked node", snap.OpenObligations)
	}
	if !hasProgressIncident(snap, blocked, pm.IncidentBlockDispositionRequired) {
		t.Fatalf("snapshot incidents=%+v, want block_disposition_required", snap.OpenIncidents)
	}
	if len(snap.OpenHolds) != 0 {
		t.Fatalf("timed-out upstream wait should not create dispatch-blocking holds: %+v", snap.OpenHolds)
	}
	h.clk.Advance(defaultHoldAckDeadline + time.Minute)
	if err := h.svc.ReconcileProgressControl(h.ctx, 10); err != nil {
		t.Fatalf("progress-control sweep: %v", err)
	}
	snap, err = h.svc.progress.SnapshotPlan(h.ctx, planID, h.clk.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.OpenHolds) != 0 {
		t.Fatalf("unacked disposition wake should not become a dispatch-blocking hold: %+v", snap.OpenHolds)
	}

	h.setTaskStatus(t, upstream, pm.TaskCompleted)
	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatalf("progress sweep: %v", err)
	}
	snap, err = h.svc.progress.SnapshotPlan(h.ctx, planID, h.clk.Now())
	if err != nil {
		t.Fatal(err)
	}
	if hasProgressObligation(snap, blocked, pm.ObligationPlanProgress) || hasProgressIncident(snap, blocked, pm.IncidentBlockDispositionRequired) {
		t.Fatalf("authoritative progress did not resolve block disposition: obligations=%+v incidents=%+v", snap.OpenObligations, snap.OpenIncidents)
	}
}

func TestProgressControl_FailedEffectiveNodeRequiresOwnerDisposition(t *testing.T) {
	h, _ := planGraphSetup(t)
	h.svc.progress = pmsql.NewProgressControlRepo(h.db)
	_, planID, failed, _ := seedBlockedPlanAB(t, h, "failed-disposition")

	h.setTaskStatus(t, failed, pm.TaskFailed)
	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatalf("failed sweep: %v", err)
	}
	snap, err := h.svc.progress.SnapshotPlan(h.ctx, planID, h.clk.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !hasProgressObligation(snap, failed, pm.ObligationPlanProgress) {
		t.Fatalf("snapshot obligations=%+v, want plan_progress for failed effective node", snap.OpenObligations)
	}
	if !hasProgressIncident(snap, failed, pm.IncidentBlockDispositionRequired) {
		t.Fatalf("snapshot incidents=%+v, want block_disposition_required for failed effective node", snap.OpenIncidents)
	}
}

func hasProgressObligation(snap pm.ProgressControlSnapshot, taskID pm.TaskID, kind pm.ProgressObligationKind) bool {
	for _, obligation := range snap.OpenObligations {
		if obligation.TaskID == taskID && obligation.Kind == kind {
			return true
		}
	}
	return false
}

func hasProgressIncident(snap pm.ProgressControlSnapshot, taskID pm.TaskID, kind pm.ProgressIncidentKind) bool {
	for _, incident := range snap.OpenIncidents {
		if incident.TaskID == taskID && incident.Kind == kind {
			return true
		}
	}
	return false
}

func TestPlanDetail_ProgressControlCannotDetermineAllowsIncidentOnly(t *testing.T) {
	h := planAdvanceSetup(t)
	h.svc.progress = pmsql.NewProgressControlRepo(h.db)
	ctx := h.ctx
	pid, err := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "incident-only", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	taskID := h.seedAssignedTask(t, pid, planID, "running without delivery", "user:a")
	now := h.clk.Now()
	ok, err := h.svc.progress.UpsertIncident(ctx, pm.ProgressIncident{
		ID: "incident-only", PlanID: planID, TaskID: taskID, NodeID: "node-1",
		Kind: pm.IncidentProgressClassificationUnknown, Severity: "P1",
		OwnerRef: "system", OwnerDisplay: "system", Summary: "running_without_delivery",
		SourceRef: "progress:running_without_delivery", Status: pm.ResponsibilityOpen,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("incident was not inserted")
	}
	detail, err := h.svc.GetPlanDetail(ctx, planID)
	if err != nil {
		t.Fatalf("GetPlanDetail incident-only progress_control: %v", err)
	}
	if detail.ProgressControl == nil {
		t.Fatal("progress_control missing")
	}
	if detail.ProgressControl.Decision != pm.CannotDetermine {
		t.Fatalf("decision=%s, want cannot_determine", detail.ProgressControl.Decision)
	}
	if len(detail.ProgressControl.OpenHolds) != 0 {
		t.Fatalf("open holds=%+v, want none", detail.ProgressControl.OpenHolds)
	}
	if len(detail.ProgressControl.OpenIncidents) != 1 {
		t.Fatalf("open incidents=%+v, want one", detail.ProgressControl.OpenIncidents)
	}
	if len(detail.ProgressControl.RequiredActions) != 1 || detail.ProgressControl.RequiredActions[0].SourceType != "incident" {
		t.Fatalf("required actions=%+v, want incident action", detail.ProgressControl.RequiredActions)
	}
}

func TestReconcilePausedPlans_SecondClockEscalatesHoldSLOWithoutDispatch(t *testing.T) {
	h := planAdvanceSetup(t)
	h.svc.progress = pmsql.NewProgressControlRepo(h.db)
	ctx := h.ctx
	_, planID, decision, _ := seedRootDecisionPlan(t, h, "paused-clock", "user:a")
	if err := h.svc.PausePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(defaultMaxHoldDuration + time.Minute)
	breached, err := h.svc.progress.ListBreachedHolds(ctx, h.clk.Now(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(breached) == 0 {
		t.Fatalf("test setup has no breached hold before paused reconcile")
	}
	if err := h.svc.ReconcilePausedPlans(ctx, nil); err != nil {
		t.Fatalf("ReconcilePausedPlans: %v", err)
	}
	snap, err := h.svc.progress.SnapshotPlan(ctx, planID, h.clk.Now())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, inc := range snap.OpenIncidents {
		if inc.TaskID == decision && inc.Kind == pm.ProgressIncidentHoldSLOBreach && inc.Severity == "P0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("paused second clock did not escalate hold SLO: %+v", snap.OpenIncidents)
	}
}
