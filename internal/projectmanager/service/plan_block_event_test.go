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
