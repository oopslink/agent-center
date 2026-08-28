package service

import (
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func TestProgressControl_TornReadDisappearanceRequiresSecondConfirmation(t *testing.T) {
	h, planID, taskID := progressPlanWithOneTask(t)

	if err := h.svc.reconcilePlanProgress(h.ctx, planID, progressReconcileOptions{WatermarkLagSLA: time.Minute, SourceGraceCycle: 1, SuspectMaxCycles: 2}); err != nil {
		t.Fatalf("reconcile first: %v", err)
	}
	first, ok, err := h.plans.LatestProgressObservation(h.ctx, planID, taskID)
	if err != nil || !ok {
		t.Fatalf("latest first = (%+v,%v,%v), want row", first, ok, err)
	}
	if first.Decision != pm.ResponsibilityBound || first.Quality != pm.ProgressQualitySuspect || first.SuspectCycles != 1 {
		t.Fatalf("first torn-read decision=%s quality=%s cycles=%d, want responsibility_bound suspect cycle 1",
			first.Decision, first.Quality, first.SuspectCycles)
	}
	if incs, _ := h.plans.ListOpenProgressIncidents(h.ctx, planID); incidentsOfKind(incs, pm.IncidentProgressClassificationUnknown) != 0 {
		t.Fatalf("first suspect created cannot_determine incident: %+v", incs)
	}
	if obs, _ := h.plans.ListOpenProgressObligations(h.ctx, planID); obligationsOfKind(obs, pm.ObligationSourceRecovery) != 1 {
		t.Fatalf("first suspect obligations=%+v, want one source_recovery", obs)
	}

	h.clk.Advance(time.Second)
	if err := h.svc.reconcilePlanProgress(h.ctx, planID, progressReconcileOptions{WatermarkLagSLA: time.Minute, SourceGraceCycle: 1, SuspectMaxCycles: 2}); err != nil {
		t.Fatalf("reconcile second: %v", err)
	}
	second, ok, err := h.plans.LatestProgressObservation(h.ctx, planID, taskID)
	if err != nil || !ok {
		t.Fatalf("latest second = (%+v,%v,%v), want row", second, ok, err)
	}
	if second.Decision != pm.CannotDetermine || second.SuspectCycles != 2 {
		t.Fatalf("second torn-read decision=%s cycles=%d, want cannot_determine cycle 2", second.Decision, second.SuspectCycles)
	}
	incs, _ := h.plans.ListOpenProgressIncidents(h.ctx, planID)
	if incidentsOfKind(incs, pm.IncidentProgressClassificationUnknown) != 1 {
		t.Fatalf("persistent suspect incidents=%+v, want one deduped cannot_determine", incs)
	}
	if obs, _ := h.plans.ListOpenProgressObligations(h.ctx, planID); obligationsOfKind(obs, pm.ObligationSourceRecovery) != 1 {
		t.Fatalf("source_recovery obligation was duplicated: %+v", obs)
	}
}

func TestProgressControl_WatermarkLagDedupsIncident(t *testing.T) {
	h, planID, taskID := progressPlanWithOneTask(t)
	old := h.clk.Now().Add(-10 * time.Minute)
	opt := progressReconcileOptions{WatermarkLagSLA: time.Minute, SourceWatermark: old, SuspectMaxCycles: 2}
	if err := h.svc.reconcilePlanProgress(h.ctx, planID, opt); err != nil {
		t.Fatalf("reconcile lag first: %v", err)
	}
	h.clk.Advance(time.Second)
	if err := h.svc.reconcilePlanProgress(h.ctx, planID, opt); err != nil {
		t.Fatalf("reconcile lag second: %v", err)
	}
	got, ok, err := h.plans.LatestProgressObservation(h.ctx, planID, taskID)
	if err != nil || !ok {
		t.Fatalf("latest lag = (%+v,%v,%v), want row", got, ok, err)
	}
	if got.Decision != pm.CannotDetermine {
		t.Fatalf("watermark lag decision=%s want cannot_determine", got.Decision)
	}
	incs, _ := h.plans.ListOpenProgressIncidents(h.ctx, planID)
	if incidentsOfKind(incs, pm.IncidentWatermarkLag) != 1 {
		t.Fatalf("watermark lag incidents=%+v, want one deduped row", incs)
	}
}

func TestProgressControl_MissingContractDefaultsAndCoverage(t *testing.T) {
	h, planID, taskID := progressPlanWithOneTask(t)
	h.clk.Advance(5 * time.Minute)
	if err := h.svc.reconcilePlanProgress(h.ctx, planID, progressReconcileOptions{WatermarkLagSLA: time.Minute, SuspectMaxCycles: 2}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got, ok, err := h.plans.LatestProgressObservation(h.ctx, planID, taskID)
	if err != nil || !ok {
		t.Fatalf("latest = (%+v,%v,%v), want row", got, ok, err)
	}
	if got.ProgressContract != pm.DeliveryCodeChange || !got.ProgressContractDefaulted {
		t.Fatalf("contract=(%q defaulted=%v), want conservative code_change default", got.ProgressContract, got.ProgressContractDefaulted)
	}
	if got.Coverage.TotalNodes != 1 || got.Coverage.CoveredNodes != 0 || got.Coverage.CoverageRatio != 0 {
		t.Fatalf("coverage=%+v, want one uncovered node", got.Coverage)
	}
	if got.Coverage.UncoveredProgressWindowSeconds <= 0 || got.UncoveredProgressWindowSeconds <= 0 {
		t.Fatalf("uncovered windows not recorded: vector=%d coverage=%d", got.UncoveredProgressWindowSeconds, got.Coverage.UncoveredProgressWindowSeconds)
	}
	incs, _ := h.plans.ListOpenProgressIncidents(h.ctx, planID)
	if incidentsOfKind(incs, pm.IncidentMigrationGap) != 1 {
		t.Fatalf("missing contract incidents=%+v, want migration_gap", incs)
	}
}

func TestProgressControl_ReconcileRunningPlansPersistsProductionPath(t *testing.T) {
	h, planID, _ := progressPlanWithOneTask(t)
	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatalf("ReconcileRunningPlans: %v", err)
	}
	incs, err := h.plans.ListOpenProgressIncidents(h.ctx, planID)
	if err != nil {
		t.Fatalf("ListOpenProgressIncidents: %v", err)
	}
	if incidentsOfKind(incs, pm.IncidentMigrationGap) != 1 {
		t.Fatalf("production reconcile did not persist progress-control incident: %+v", incs)
	}
}

func TestProgressControl_EmptyFrontierUnresolvedContinuationCreatesOwnerObligation(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	projectID, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: projectID, Name: "empty frontier", CreatedBy: "user:a"})
	h.drain(t)
	stageA, err := h.svc.CreateStage(ctx, CreateStageCommand{PlanID: planID, Name: "A", MaxRounds: 3, Actor: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	a1 := h.seedAssignedTask(t, projectID, planID, "a1", "user:a1")
	a2 := h.seedAssignedTask(t, projectID, planID, "a2", "user:a2")
	for _, taskID := range []pm.TaskID{a1, a2} {
		if err := h.svc.AssignTaskToStage(ctx, planID, taskID, stageA, "user:a"); err != nil {
			t.Fatal(err)
		}
	}
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: a2, ToTaskID: a1, Kind: pm.EdgeSeq})
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, a1, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, a2, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	stage, _ := h.svc.GetStage(ctx, stageA)
	gateTaskID := stage.Stage.GateTaskID()
	h.setTaskStatus(t, gateTaskID, pm.TaskCompleted)
	if err := h.svc.PausePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	_, err = h.svc.RecordStageGateVerdict(ctx, RecordStageGateVerdictCommand{
		GateTaskID: gateTaskID, Outcome: pm.GateVerdictReject,
		Evidence: "reject recorded while remediation planner is unavailable", ReviewedSHA: "deadbeef",
		IdempotencyKey: "empty-frontier-reject", Actor: "user:a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.ResumePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}

	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatalf("ReconcileRunningPlans: %v", err)
	}
	plan, _ := h.plans.FindByID(ctx, planID)
	if plan.Status() != pm.PlanRunning {
		t.Fatalf("plan status=%s, want running until remediation is appended/approved", plan.Status())
	}
	if blocked, err := h.plans.ListBlockedOn(ctx, planID); err != nil || len(blocked) != 0 {
		t.Fatalf("blocked_on=%+v err=%v, want empty frontier", blocked, err)
	}
	obs, err := h.plans.ListOpenProgressObligations(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if obligationsOfKind(obs, pm.ObligationPlanProgress) != 1 {
		t.Fatalf("plan-level obligations=%+v, want one plan_progress", obs)
	}
	var planOb pm.ProgressObligation
	for _, o := range obs {
		if o.Kind == pm.ObligationPlanProgress {
			planOb = o
			break
		}
	}
	if planOb.TaskID != "" || planOb.OwnerRef != "user:a" || planOb.DeadlineAt.IsZero() || len(planOb.SourceFactRefs) == 0 {
		t.Fatalf("obligation lacks plan-level owner/deadline/fact binding: %+v", planOb)
	}
	incs, err := h.plans.ListOpenProgressIncidents(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if incidentsOfKind(incs, pm.IncidentEmptyFrontierUnresolvedPlan) != 1 {
		t.Fatalf("incidents=%+v, want one empty_frontier_unresolved_plan", incs)
	}
	wakes := planOwnerBlockWakeEvents(t, h, planID)
	if len(wakes) == 0 || wakes[len(wakes)-1].TaskID != "" || wakes[len(wakes)-1].WaitType != string(pm.IncidentEmptyFrontierUnresolvedPlan) {
		t.Fatalf("plan-level wake missing: %+v", wakes)
	}

	h.clk.Advance(time.Second)
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatalf("second ReconcileRunningPlans: %v", err)
	}
	obs, _ = h.plans.ListOpenProgressObligations(ctx, planID)
	if obligationsOfKind(obs, pm.ObligationPlanProgress) != 1 {
		t.Fatalf("second sweep duplicated plan_progress obligation: %+v", obs)
	}
}

func TestProgressControl_MissingGateEvaluatorCreatesOwnerObligation(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	projectID, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: projectID, Name: "missing gate", CreatedBy: "user:a"})
	h.drain(t)
	member, _, _, stageA, _ := seedTwoStagePlan(t, h, projectID, planID, 3)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, member, pm.TaskCompleted)
	if _, err := h.svc.db.ExecContext(ctx, `UPDATE pm_stages SET gate_task_id = '' WHERE id = ?`, string(stageA)); err != nil {
		t.Fatal(err)
	}

	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatalf("ReconcileRunningPlans: %v", err)
	}
	obs, err := h.plans.ListOpenProgressObligations(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if obligationsOfKind(obs, pm.ObligationGateEvaluation) != 1 {
		t.Fatalf("gate obligations=%+v, want one gate_evaluation", obs)
	}
	incs, err := h.plans.ListOpenProgressIncidents(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if incidentsOfKind(incs, pm.IncidentMissingGateEvaluator) != 1 {
		t.Fatalf("gate incidents=%+v, want one missing_gate_evaluator", incs)
	}
	ready, err := h.svc.AdvancePlan(ctx, planID, "user:a")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ready {
		if id != member {
			t.Fatalf("missing gate evaluator released unexpected task %s; dispatched=%v", id, ready)
		}
	}
}

func progressPlanWithOneTask(t *testing.T) (*planAdvanceHarness, pm.PlanID, pm.TaskID) {
	t.Helper()
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-progress", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "progress", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	taskID := h.seedAssignedTask(t, pid, planID, "node", "user:x")
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	return h, planID, taskID
}

func incidentsOfKind(incs []pm.ProgressIncident, kind pm.ProgressIncidentKind) int {
	n := 0
	for _, i := range incs {
		if i.Kind == kind {
			n++
		}
	}
	return n
}

func obligationsOfKind(obs []pm.ProgressObligation, kind pm.ProgressObligationKind) int {
	n := 0
	for _, o := range obs {
		if o.Kind == kind {
			n++
		}
	}
	return n
}
