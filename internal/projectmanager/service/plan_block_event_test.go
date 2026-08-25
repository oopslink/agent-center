package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

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
	pending, err := h.plans.ListPlanBlockEvents(h.ctx, planID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].NotificationState != pm.PlanBlockNotifyPending {
		t.Fatalf("pre-relay notification state=%+v want one pending event", pending)
	}
	h.drain(t)

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

func TestPlanBlockOwnerWake_ReminderEscalationAndDuplicateScan(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if _, err := h.svc.AddProjectMember(h.ctx, AddProjectMemberCommand{ProjectID: pid, IdentityID: "user:b", Role: pm.RoleMember, Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	planID, err := h.svc.CreatePlan(h.ctx, CreatePlanCommand{
		ProjectID: pid, Name: "owner-loop", CreatedBy: "user:a", OwnerRef: "user:a", BackupOwnerRef: "user:b",
		RecoveryPolicy: pm.PlanRecoveryPolicy{NotifyAfterSeconds: 0, RemindAfterSeconds: 15 * 60, EscalateAfterSeconds: 60 * 60},
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
	h.drain(t)
	initialCount := h.planConvMsgCount(t, planID)

	proj := NewPlanBlockOwnerWakeProjector(h.svc.db, h.plans, h.svc.members, h.svc.planDispatcher, nil, h.clk)
	h.clk.Advance(14*time.Minute + 59*time.Second)
	if err := proj.Tick(h.ctx); err != nil {
		t.Fatal(err)
	}
	if got := h.planConvMsgCount(t, planID); got != initialCount {
		t.Fatalf("pre-reminder scan changed message count: got %d want %d", got, initialCount)
	}

	h.clk.Advance(time.Second)
	if err := proj.Tick(h.ctx); err != nil {
		t.Fatal(err)
	}
	afterReminder := h.planConvMsgCount(t, planID)
	if afterReminder != initialCount+1 || !strings.Contains(h.latestPlanMsgText(t, planID), "reminder: plan attention required") {
		t.Fatalf("reminder not sent once: count=%d latest=%q", afterReminder, h.latestPlanMsgText(t, planID))
	}
	if err := proj.Tick(h.ctx); err != nil {
		t.Fatal(err)
	}
	if got := h.planConvMsgCount(t, planID); got != afterReminder {
		t.Fatalf("duplicate reminder scan sent again: got %d want %d", got, afterReminder)
	}

	h.clk.Advance(45 * time.Minute)
	if err := proj.Tick(h.ctx); err != nil {
		t.Fatal(err)
	}
	afterEscalation := h.planConvMsgCount(t, planID)
	if afterEscalation != afterReminder+1 || !strings.Contains(h.latestPlanMsgText(t, planID), "escalation: plan attention required") ||
		!strings.Contains(h.latestPlanMsgText(t, planID), "@b ") {
		t.Fatalf("escalation not sent to backup once: count=%d latest=%q", afterEscalation, h.latestPlanMsgText(t, planID))
	}
	if err := proj.Tick(h.ctx); err != nil {
		t.Fatal(err)
	}
	if got := h.planConvMsgCount(t, planID); got != afterEscalation {
		t.Fatalf("duplicate escalation scan sent again: got %d want %d", got, afterEscalation)
	}
	events, err := h.svc.ListPlanBlockEvents(h.ctx, planID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].NotificationState != pm.PlanBlockNotifyEscalated {
		t.Fatalf("notification state after escalation=%+v, want escalated", events)
	}
}

func TestPlanBlockOwnerWake_FallbackWhenOwnerRemoved(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if _, err := h.svc.AddProjectMember(h.ctx, AddProjectMemberCommand{ProjectID: pid, IdentityID: "user:owner", Role: pm.RoleMember, Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.AddProjectMember(h.ctx, AddProjectMemberCommand{ProjectID: pid, IdentityID: "user:backup", Role: pm.RoleMember, Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	planID, err := h.svc.CreatePlan(h.ctx, CreatePlanCommand{
		ProjectID: pid, Name: "fallback", CreatedBy: "user:a", OwnerRef: "user:owner", BackupOwnerRef: "user:backup",
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
	if err := h.svc.RemoveProjectMember(h.ctx, RemoveProjectMemberCommand{ProjectID: pid, IdentityID: "user:owner", Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.BlockTask(h.ctx, taskID, "owner was removed", pm.BlockReasonObstacle, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	plan, err := h.plans.FindByID(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.OwnerRef() != "user:backup" {
		t.Fatalf("owner fallback = %s, want backup", plan.OwnerRef())
	}
	if latest := h.latestPlanMsgText(t, planID); !strings.Contains(latest, "@backup ") || !strings.Contains(latest, "owner was removed") {
		t.Fatalf("fallback notification mismatch: %q", latest)
	}
}

func TestPlanBlockOwnerAction_OnlyCurrentOwnerMayAckOrResolve(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if _, err := h.svc.AddProjectMember(h.ctx, AddProjectMemberCommand{ProjectID: pid, IdentityID: "user:b", Role: pm.RoleMember, Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	planID, err := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "owner-only", CreatedBy: "user:a", OwnerRef: "user:a"})
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
	if err := h.svc.BlockTask(h.ctx, taskID, "waiting", pm.BlockReasonObstacle, "user:a"); err != nil {
		t.Fatal(err)
	}
	events, err := h.svc.ListPlanBlockEvents(h.ctx, planID, true)
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	if err := h.svc.AcknowledgePlanBlock(h.ctx, AcknowledgePlanBlockCommand{EventID: events[0].EventID, Actor: "user:b"}); !errors.Is(err, pm.ErrPlanOwnerOnly) {
		t.Fatalf("ack by non-owner err=%v, want ErrPlanOwnerOnly", err)
	}
	if err := h.svc.ResolvePlanBlock(h.ctx, ResolvePlanBlockCommand{EventID: events[0].EventID, Actor: "user:b", ResolutionKind: string(pm.PlanBlockResumeOriginal), Note: "done"}); !errors.Is(err, pm.ErrPlanOwnerOnly) {
		t.Fatalf("resolve by non-owner err=%v, want ErrPlanOwnerOnly", err)
	}
}

func TestRemoveProjectMember_ImmediatelyFallbacksPlanOwner(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if _, err := h.svc.AddProjectMember(h.ctx, AddProjectMemberCommand{ProjectID: pid, IdentityID: "user:owner", Role: pm.RoleMember, Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.AddProjectMember(h.ctx, AddProjectMemberCommand{ProjectID: pid, IdentityID: "user:backup", Role: pm.RoleMember, Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	planID, err := h.svc.CreatePlan(h.ctx, CreatePlanCommand{
		ProjectID: pid, Name: "fallback-now", CreatedBy: "user:a", OwnerRef: "user:owner", BackupOwnerRef: "user:backup",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.RemoveProjectMember(h.ctx, RemoveProjectMemberCommand{ProjectID: pid, IdentityID: "user:owner", Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	plan, err := h.plans.FindByID(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.OwnerRef() != "user:backup" {
		t.Fatalf("owner after removal = %s, want backup", plan.OwnerRef())
	}
}

func TestPlanBlockOwnerWake_ReplayAfterClaimDoesNotDuplicateSend(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, err := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "replay", CreatedBy: "user:a", OwnerRef: "user:a"})
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
	if err := h.svc.BlockTask(h.ctx, taskID, "waiting", pm.BlockReasonObstacle, "user:a"); err != nil {
		t.Fatal(err)
	}
	events, err := h.svc.ListPlanBlockEvents(h.ctx, planID, true)
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	if err := h.plans.UpdatePlanBlockEventNotification(h.ctx, events[0].EventID, pm.PlanBlockNotifySending, h.clk.Now()); err != nil {
		t.Fatal(err)
	}
	proj := NewPlanBlockOwnerWakeProjector(h.svc.db, h.plans, h.svc.members, h.svc.planDispatcher, nil, h.clk)
	before := h.planConvMsgCount(t, planID)
	if err := proj.Tick(h.ctx); err != nil {
		t.Fatal(err)
	}
	if got := h.planConvMsgCount(t, planID); got != before {
		t.Fatalf("unexpired claimed notification replay sent duplicate: got %d want %d", got, before)
	}
	h.clk.Advance(planBlockNotificationClaimLease + time.Second)
	if err := proj.Tick(h.ctx); err != nil {
		t.Fatal(err)
	}
	if got := h.planConvMsgCount(t, planID); got != before+1 {
		t.Fatalf("expired claimed notification was not retried once: got %d want %d", got, before+1)
	}
}

func TestPlanBlockOwnerWake_ConcurrentTickDoesNotDuplicateSend(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, err := h.svc.CreatePlan(h.ctx, CreatePlanCommand{
		ProjectID: pid, Name: "concurrent", CreatedBy: "user:a", OwnerRef: "user:a",
		RecoveryPolicy: pm.PlanRecoveryPolicy{NotifyAfterSeconds: 0, RemindAfterSeconds: 15 * 60, EscalateAfterSeconds: 60 * 60},
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
	if err := h.svc.BlockTask(h.ctx, taskID, "waiting", pm.BlockReasonObstacle, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	h.clk.Advance(15 * time.Minute)
	before := h.planConvMsgCount(t, planID)
	projA := NewPlanBlockOwnerWakeProjector(h.svc.db, h.plans, h.svc.members, h.svc.planDispatcher, nil, h.clk)
	projB := NewPlanBlockOwnerWakeProjector(h.svc.db, h.plans, h.svc.members, h.svc.planDispatcher, nil, h.clk)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, proj := range []*PlanBlockOwnerWakeProjector{projA, projB} {
		wg.Add(1)
		go func(p *PlanBlockOwnerWakeProjector) {
			defer wg.Done()
			errs <- p.Tick(h.ctx)
		}(proj)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := h.planConvMsgCount(t, planID); got != before+1 {
		t.Fatalf("concurrent tick sends=%d want exactly one new message over %d", got, before)
	}
}

type flakyPlanDispatcher struct {
	mu      sync.Mutex
	fail    bool
	sends   int
	targets []string
}

func (d *flakyPlanDispatcher) PostMention(_ context.Context, _, assigneeRef, _ string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sends++
	d.targets = append(d.targets, assigneeRef)
	if d.fail {
		d.fail = false
		return "", errors.New("send failed")
	}
	return "msg-ok", nil
}

func TestPlanBlockOwnerWake_SendFailureRetries(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, err := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "retry", CreatedBy: "user:a", OwnerRef: "user:a"})
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
	if err := h.svc.BlockTask(h.ctx, taskID, "waiting", pm.BlockReasonObstacle, "user:a"); err != nil {
		t.Fatal(err)
	}
	events, err := h.svc.ListPlanBlockEvents(h.ctx, planID, true)
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	dispatcher := &flakyPlanDispatcher{fail: true}
	proj := NewPlanBlockOwnerWakeProjector(h.svc.db, h.plans, h.svc.members, dispatcher, nil, h.clk)
	if err := proj.Tick(h.ctx); err != nil {
		t.Fatal(err)
	}
	events, _ = h.svc.ListPlanBlockEvents(h.ctx, planID, true)
	if events[0].NotificationState != pm.PlanBlockNotifyFailed {
		t.Fatalf("state after failure=%s want failed", events[0].NotificationState)
	}
	if err := proj.Tick(h.ctx); err != nil {
		t.Fatal(err)
	}
	events, _ = h.svc.ListPlanBlockEvents(h.ctx, planID, true)
	if events[0].NotificationState != pm.PlanBlockNotifySent || dispatcher.sends != 2 {
		t.Fatalf("retry state=%s sends=%d want sent/2", events[0].NotificationState, dispatcher.sends)
	}
}
