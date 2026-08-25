package service

import (
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func TestBlockTask_ActivePlanNodeCreatesOwnerBlockEventAndAttention(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, err := h.svc.CreatePlan(h.ctx, CreatePlanCommand{
		ProjectID: pid, Name: "owner-loop", CreatedBy: "user:a", OwnerRef: "user:a", BackupOwnerRef: "user:a",
	})
	if err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	taskID := h.seedAssignedTask(t, pid, planID, "blocked work", "user:a")
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	if err := h.svc.StartTask(h.ctx, taskID, "user:a"); err != nil {
		t.Fatal(err)
	}

	if err := h.svc.BlockTask(h.ctx, taskID, "waiting for owner decision", pm.BlockReasonObstacle, "user:a"); err != nil {
		t.Fatal(err)
	}

	p, err := h.plans.FindByID(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if p.AttentionStatus() != pm.PlanAttentionRequired || p.LastAttentionEventID() == "" {
		t.Fatalf("attention=%s last=%s, want attention_required with event", p.AttentionStatus(), p.LastAttentionEventID())
	}
	events, err := h.plans.ListPlanBlockEvents(h.ctx, planID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("block events=%d want 1: %+v", len(events), events)
	}
	ev := events[0]
	if ev.TaskID != taskID || ev.GenerationID != p.ActiveGenerationID() || ev.OwnerRef != "user:a" || !ev.Active || !ev.Effective {
		t.Fatalf("event mismatch: %+v active_generation=%s", ev, p.ActiveGenerationID())
	}
	if ev.NotificationState != pm.PlanBlockNotifySent {
		t.Fatalf("notification_state=%s want sent", ev.NotificationState)
	}
	blocked, err := h.plans.ListBlockedOn(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked) != 1 || blocked[0].WaitType != pm.WaitHumanDecision {
		t.Fatalf("blocked_on=%+v want one human_decision snapshot", blocked)
	}
	var wakeCount int
	if err := h.svc.db.QueryRowContext(h.ctx, `SELECT COUNT(*) FROM outbox_events WHERE event_type = ?`, EvtPlanBlockOwnerWake).Scan(&wakeCount); err != nil {
		t.Fatal(err)
	}
	if wakeCount != 1 {
		t.Fatalf("owner wake events=%d want 1", wakeCount)
	}
}

func TestPlanBlockEvent_AcknowledgeAndUnblockResolveClearsAttention(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, err := h.svc.CreatePlan(h.ctx, CreatePlanCommand{
		ProjectID: pid, Name: "owner-loop", CreatedBy: "user:a", OwnerRef: "user:a",
	})
	if err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	taskID := h.seedAssignedTask(t, pid, planID, "blocked work", "user:a")
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	if err := h.svc.StartTask(h.ctx, taskID, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.BlockTask(h.ctx, taskID, "waiting for owner decision", pm.BlockReasonObstacle, "user:a"); err != nil {
		t.Fatal(err)
	}
	events, err := h.svc.ListPlanBlockEvents(h.ctx, planID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("active events=%d want 1", len(events))
	}
	eventID := events[0].EventID

	if err := h.svc.AcknowledgePlanBlock(h.ctx, AcknowledgePlanBlockCommand{EventID: eventID, Actor: "user:a"}); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	if err := h.svc.UnblockTask(h.ctx, UnblockTaskCommand{TaskID: taskID, Comment: "external blocker gone", Actor: "user:a"}); err != nil {
		t.Fatalf("unblock: %v", err)
	}

	active, err := h.svc.ListPlanBlockEvents(h.ctx, planID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("active events after unblock=%d want 0: %+v", len(active), active)
	}
	all, err := h.svc.ListPlanBlockEvents(h.ctx, planID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].EventID != eventID || all[0].Active || all[0].ResolvedBy != "user:a" || all[0].ResolutionKind != "resume_original" {
		t.Fatalf("resolved history mismatch: %+v", all)
	}
	p, err := h.plans.FindByID(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if p.AttentionStatus() != pm.PlanAttentionNone || p.LastAttentionEventID() != "" {
		t.Fatalf("attention after resolution=%s last=%s, want none", p.AttentionStatus(), p.LastAttentionEventID())
	}
}

func TestPlanBlockEvent_IgnoresIneffectiveHistoricalGeneration(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, err := h.svc.CreatePlan(h.ctx, CreatePlanCommand{
		ProjectID: pid, Name: "owner-loop", CreatedBy: "user:a", OwnerRef: "user:a",
	})
	if err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	taskID := h.seedAssignedTask(t, pid, planID, "old work", "user:a")
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	p, err := h.plans.FindByID(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	now := h.clk.Now()
	_, err = h.plans.UpsertPlanBlockEvent(h.ctx, &pm.PlanBlockEvent{
		EventID: "block-old", IdempotencyKey: "plan_block:old", PlanID: planID, GenerationID: p.ActiveGenerationID(), TaskID: taskID,
		NodeID: "old-node", BlockVersion: 1, BlockedReason: "superseded historical executor", ReasonType: pm.BlockReasonObstacle,
		BlockedBy: "user:a", BlockedAt: now, Active: true, Effective: false, OwnerRef: "user:a",
		ImpactedDownstreamJSON: "[]", NextActionsJSON: "[]", NotificationState: pm.PlanBlockNotifyPending, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := h.svc.ListPlanBlockEvents(h.ctx, planID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("historical ineffective block created active event: %+v", events)
	}
	all, err := h.svc.ListPlanBlockEvents(h.ctx, planID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].Effective {
		t.Fatalf("historical event should remain auditable but ineffective: %+v", all)
	}
}
