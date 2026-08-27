package service

import (
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func TestProgressControl_CannotDetermineWhenBlockedOnLacksDeadline(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	plan := progressControlPlan(t, now)
	svc := &Service{clock: clock.NewFakeClock(now)}
	detail := &PlanDetail{
		Plan: plan,
		View: pm.PlanView{Nodes: []pm.PlanNodeView{{TaskID: "task-a", NodeStatus: pm.NodeBlocked, Effective: true}}},
		BlockedOn: []pm.BlockedOn{{
			PlanID:           plan.ID(),
			TaskID:           "task-a",
			WaitType:         pm.WaitHumanDecision,
			TriggerCondition: "future release condition that must not be used as current evidence",
			WaitedSince:      now.Add(-2 * time.Hour),
		}},
	}

	svc.fillProgressControl(detail)

	pc := detail.ProgressControl
	if pc == nil {
		t.Fatal("ProgressControl nil")
	}
	if pc.Decision != pm.ProgressDecisionCannotDetermine {
		t.Fatalf("decision=%q want cannot_determine", pc.Decision)
	}
	if pc.Health != pm.ProgressHealthDegraded {
		t.Fatalf("health=%q want degraded", pc.Health)
	}
	if pc.Quality != pm.ProgressQualitySuspect {
		t.Fatalf("quality=%q want suspect", pc.Quality)
	}
	if len(pc.OpenIncidents) != 1 || pc.OpenIncidents[0].Kind != "progress_classification_unknown" {
		t.Fatalf("incidents=%+v want classification incident", pc.OpenIncidents)
	}
	if len(pc.OpenHolds) != 1 || pc.OpenHolds[0].InFlightPolicy != "do_not_kill_unproven_execution" {
		t.Fatalf("holds=%+v want phase-0 safety hold", pc.OpenHolds)
	}
	if len(pc.RequiredActions) != 1 {
		t.Fatalf("required_actions=%+v want one", pc.RequiredActions)
	}
	if got := pc.RequiredActions[0].Summary; got == "future release condition that must not be used as current evidence" {
		t.Fatal("required action used blocked_on.trigger_condition as current progress evidence")
	}
	if pc.Coverage.CannotDetermineNodes != 1 || pc.Coverage.MissingDeadlineHolds != 1 {
		t.Fatalf("coverage=%+v want cannot_determine/missing deadline coverage", pc.Coverage)
	}
}

func TestProgressControl_ResponsibilityBoundAndValidInFlight(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	plan := progressControlPlan(t, now)
	svc := &Service{clock: clock.NewFakeClock(now)}
	deadline := now.Add(30 * time.Minute)
	taskRunning := progressControlTask(t, "task-run", plan.ID(), "agent:r", now)
	taskBlocked := progressControlTask(t, "task-blocked", plan.ID(), "agent:b", now)
	detail := &PlanDetail{
		Plan:  plan,
		Tasks: []*pm.Task{taskRunning, taskBlocked},
		View: pm.PlanView{Nodes: []pm.PlanNodeView{
			{TaskID: taskRunning.ID(), NodeStatus: pm.NodeRunning, TaskStatus: pm.TaskRunning, Effective: true, DispatchedAt: now.Add(-time.Hour)},
			{TaskID: taskBlocked.ID(), NodeStatus: pm.NodeBlocked, TaskStatus: pm.TaskOpen, Effective: true},
		}},
		BlockedOn: []pm.BlockedOn{{
			PlanID:      plan.ID(),
			TaskID:      taskBlocked.ID(),
			WaitType:    pm.WaitAcceptanceVerdict,
			WaitedSince: now.Add(-time.Hour),
			Deadline:    deadline,
			OnTimeout:   "escalate_project_owner",
		}},
	}

	svc.fillProgressControl(detail)

	pc := detail.ProgressControl
	if pc.Decision != pm.ProgressDecisionResponsibility {
		t.Fatalf("decision=%q want responsibility_bound", pc.Decision)
	}
	if pc.Health != pm.ProgressHealthAttention {
		t.Fatalf("health=%q want attention", pc.Health)
	}
	if len(pc.ValidInFlight) != 1 || pc.ValidInFlight[0].TaskID != taskRunning.ID() || pc.ValidInFlight[0].Quality != pm.ProgressQualityValid {
		t.Fatalf("valid_in_flight=%+v want running node", pc.ValidInFlight)
	}
	if len(pc.OpenObligations) != 1 || pc.OpenObligations[0].Kind != "acceptance_verdict" || pc.OpenObligations[0].OwnerRef != "agent:b" {
		t.Fatalf("open_obligations=%+v want acceptance_verdict owned by assignee", pc.OpenObligations)
	}
	if pc.PrimaryAttention == nil || pc.PrimaryAttention.ObligationID == "" {
		t.Fatalf("primary_attention=%+v want obligation action", pc.PrimaryAttention)
	}
	if pc.Coverage.ValidInFlightNodes != 1 || pc.Coverage.ResponsibilityNodes != 1 || pc.Coverage.OpenHolds != 1 {
		t.Fatalf("coverage=%+v want inflight/responsibility/hold coverage", pc.Coverage)
	}
}

func progressControlPlan(t *testing.T, at time.Time) *pm.Plan {
	t.Helper()
	plan, err := pm.NewPlan(pm.NewPlanInput{ID: "plan-progress", ProjectID: "project-progress", Name: "Progress", CreatorRef: "user:a", CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func progressControlTask(t *testing.T, id pm.TaskID, planID pm.PlanID, assignee pm.IdentityRef, at time.Time) *pm.Task {
	t.Helper()
	task, err := pm.NewTask(pm.NewTaskInput{ID: id, ProjectID: "project-progress", Title: string(id), CreatedBy: "user:a", CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if err := task.Assign(assignee, at); err != nil {
		t.Fatal(err)
	}
	task.SetPlan(planID, at)
	if id == "task-run" {
		if err := task.Start(at); err != nil {
			t.Fatal(err)
		}
	}
	return task
}
