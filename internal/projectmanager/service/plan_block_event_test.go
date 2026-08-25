package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/identity"
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

func TestPlanOwnerAccessLossProductionPathsFallbackAndFailClosed(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		fallback bool
	}{
		{name: "removed fallback", path: "removed", fallback: true},
		{name: "removed fail closed", path: "removed", fallback: false},
		{name: "disabled fallback", path: "disabled", fallback: true},
		{name: "disabled fail closed", path: "disabled", fallback: false},
		{name: "permission loss fallback", path: "permission", fallback: true},
		{name: "permission loss fail closed", path: "permission", fallback: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := planAdvanceSetup(t)
			h.svc.authorizer = authz.New(authz.Deps{DB: h.svc.db, Mode: authz.EnforcementEnforce, IDGen: h.svc.idgen, Clock: h.clk})
			seedIdentityMember(t, h, "org-1", "a", "owner", "joined")
			seedIdentityMember(t, h, "org-1", "owner", "member", "joined")
			if tc.fallback {
				seedIdentityMember(t, h, "org-1", "backup", "member", "joined")
			}
			pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
			if _, err := h.svc.AddProjectMember(h.ctx, AddProjectMemberCommand{ProjectID: pid, IdentityID: "user:owner", Role: pm.RoleMember, Actor: "user:a"}); err != nil {
				t.Fatal(err)
			}
			if tc.fallback {
				if _, err := h.svc.AddProjectMember(h.ctx, AddProjectMemberCommand{ProjectID: pid, IdentityID: "user:backup", Role: pm.RoleMember, Actor: "user:a"}); err != nil {
					t.Fatal(err)
				}
			}
			backup := pm.IdentityRef("")
			if tc.fallback {
				backup = "user:backup"
			}
			planID, err := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "owner-loss", CreatedBy: "user:a", OwnerRef: "user:owner", BackupOwnerRef: backup})
			if err != nil {
				t.Fatal(err)
			}
			if !tc.fallback {
				if _, err := h.svc.db.ExecContext(h.ctx, `UPDATE pm_project_members SET role='member' WHERE project_id=? AND identity_id='user:a'`, string(pid)); err != nil {
					t.Fatal(err)
				}
			}
			h.drain(t)

			switch tc.path {
			case "removed":
				memberSvc := identity.NewMemberRemoveService(h.svc.db, identity.NewSQLiteMemberRepo(h.svc.db), identity.NewOrganizationLockManager()).WithAccessLossHandler(h.svc)
				if err := memberSvc.Remove(h.ctx, "mem-owner", "a"); err != nil {
					t.Fatal(err)
				}
			case "disabled":
				memberSvc := identity.NewMemberDisableService(h.svc.db, identity.NewSQLiteMemberRepo(h.svc.db), identity.NewOrganizationLockManager()).WithAccessLossHandler(h.svc)
				if err := memberSvc.Disable(h.ctx, "mem-owner", "test"); err != nil {
					t.Fatal(err)
				}
			case "permission":
				grantProjectWrite(t, h, "org-1", "user:owner", string(pid), "asgn-owner-project-write")
				if _, err := h.svc.db.ExecContext(h.ctx, `DELETE FROM pm_project_members WHERE project_id=? AND identity_id='user:owner'`, string(pid)); err != nil {
					t.Fatal(err)
				}
				authSvc := authz.New(authz.Deps{DB: h.svc.db, Mode: authz.EnforcementEnforce, IDGen: h.svc.idgen, Clock: h.clk}).WithAccessLossHandler(h.svc)
				if _, err := authSvc.RevokeBatch(h.ctx, authz.BatchRequest{
					IdempotencyKey: "revoke-owner-project-write-" + tc.name,
					ActorRef:       "system",
					OrgID:          "org-1",
					Operations:     []authz.BatchOperation{{ID: "revoke", Revoke: authz.RevokeInput{AssignmentID: "asgn-owner-project-write", Reason: "test"}}},
				}); err != nil {
					t.Fatal(err)
				}
			}

			plan, err := h.plans.FindByID(h.ctx, planID)
			if err != nil {
				t.Fatal(err)
			}
			if tc.fallback && plan.OwnerRef() != "user:backup" {
				t.Fatalf("owner=%s want fallback backup", plan.OwnerRef())
			}
			if !tc.fallback && plan.OwnerRef() != "user:owner" {
				t.Fatalf("owner=%s want original fail-closed owner", plan.OwnerRef())
			}
			if plan.AttentionStatus() != pm.PlanAttentionRequired {
				t.Fatalf("attention=%s want attention_required", plan.AttentionStatus())
			}
			assertPlanOwnerLossEvent(t, h, planID, tc.path, !tc.fallback)
			assertPlanOwnerLossAudit(t, h, planID, !tc.fallback)
		})
	}
}

func TestPlanOwnerAccessLossHookFailureRollsBackUpstreamMutation(t *testing.T) {
	h := planAdvanceSetup(t)
	seedIdentityMember(t, h, "org-1", "a", "owner", "joined")
	seedIdentityMember(t, h, "org-1", "owner", "member", "joined")
	memberSvc := identity.NewMemberDisableService(h.svc.db, identity.NewSQLiteMemberRepo(h.svc.db), identity.NewOrganizationLockManager()).
		WithAccessLossHandler(failingAccessLossHandler{})
	if err := memberSvc.Disable(h.ctx, "mem-owner", "test"); err == nil {
		t.Fatal("Disable succeeded despite failing access-loss hook")
	}
	var status string
	if err := h.svc.db.QueryRowContext(h.ctx, `SELECT status FROM members WHERE id='mem-owner'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "joined" {
		t.Fatalf("member status=%s want joined after rollback", status)
	}

	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	grantProjectWrite(t, h, "org-1", "user:owner", string(pid), "asgn-owner-project-write")
	authSvc := authz.New(authz.Deps{DB: h.svc.db, Mode: authz.EnforcementEnforce, IDGen: h.svc.idgen, Clock: h.clk}).
		WithAccessLossHandler(failingAccessLossHandler{})
	if _, err := authSvc.RevokeBatch(h.ctx, authz.BatchRequest{
		IdempotencyKey: "revoke-failing-hook",
		ActorRef:       "system",
		OrgID:          "org-1",
		Operations:     []authz.BatchOperation{{ID: "revoke", Revoke: authz.RevokeInput{AssignmentID: "asgn-owner-project-write", Reason: "test"}}},
	}); err == nil {
		t.Fatal("RevokeBatch succeeded despite failing access-loss hook")
	}
	var revokedAt any
	if err := h.svc.db.QueryRowContext(h.ctx, `SELECT revoked_at FROM authorization_role_assignments WHERE id='asgn-owner-project-write'`).Scan(&revokedAt); err != nil {
		t.Fatal(err)
	}
	if revokedAt != nil {
		t.Fatalf("assignment revoked_at=%v want nil after rollback", revokedAt)
	}
}

type failingAccessLossHandler struct{}

func (failingAccessLossHandler) ReconcilePlanOwnerAccessLoss(context.Context, string, string, string, string) error {
	return errors.New("hook failed")
}

func (failingAccessLossHandler) ReconcilePlanOwnersAccessLoss(context.Context, string, string, string) error {
	return errors.New("hook failed")
}

func seedIdentityMember(t *testing.T, h *planAdvanceHarness, orgID, identityID, role, status string) {
	t.Helper()
	joined := h.clk.Now().Format(time.RFC3339Nano)
	disabledAt := ""
	if status == "disabled" {
		disabledAt = joined
	}
	if _, err := h.svc.db.ExecContext(h.ctx, `INSERT OR IGNORE INTO organizations
		(id, slug, name, created_by_identity_id, created_at, updated_at)
		VALUES (?, ?, ?, 'a', ?, ?)`, orgID, orgID, orgID, joined, joined); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.db.ExecContext(h.ctx, `INSERT INTO members
		(id, organization_id, identity_id, role, status, joined_at, disabled_at, disabled_reason)
		VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''), '')`,
		"mem-"+identityID, orgID, identityID, role, status, joined, disabledAt); err != nil {
		t.Fatal(err)
	}
}

func grantProjectWrite(t *testing.T, h *planAdvanceHarness, orgID, subjectRef, projectID, assignmentID string) {
	t.Helper()
	authSvc := authz.New(authz.Deps{DB: h.svc.db, Mode: authz.EnforcementEnforce, IDGen: h.svc.idgen, Clock: h.clk})
	if _, err := authSvc.ApplyBatch(h.ctx, authz.BatchRequest{
		IdempotencyKey: "grant-" + assignmentID,
		ActorRef:       "system",
		OrgID:          orgID,
		Operations: []authz.BatchOperation{{
			ID:   "grant",
			Type: "direct_grant",
			DirectGrant: authz.DirectGrantInput{
				ID: assignmentID, SubjectRef: authz.SubjectRef(subjectRef), PermissionKey: "project.write",
				Resource: authz.ResourceScope{Kind: "project", ID: projectID, OrgID: orgID},
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
}

func assertPlanOwnerLossEvent(t *testing.T, h *planAdvanceHarness, planID pm.PlanID, reasonFragment string, failClosed bool) {
	t.Helper()
	var refs, payload string
	if err := h.svc.db.QueryRowContext(h.ctx, `SELECT refs, payload FROM outbox_events WHERE event_type = ? ORDER BY created_at DESC, id DESC LIMIT 1`, EvtPlanOwnerAccessLost).Scan(&refs, &payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(refs, string(planID)) || !strings.Contains(payload, reasonFragment) {
		t.Fatalf("owner-loss event refs=%s payload=%s", refs, payload)
	}
	if failClosed && !strings.Contains(payload, `"fail_closed":true`) {
		t.Fatalf("owner-loss event must record fail_closed=true: %s", payload)
	}
	if !failClosed && !strings.Contains(payload, `"owner_ref":"user:backup"`) {
		t.Fatalf("owner-loss event must record fallback owner: %s", payload)
	}
}

func assertPlanOwnerLossAudit(t *testing.T, h *planAdvanceHarness, planID pm.PlanID, failClosed bool) {
	t.Helper()
	entry := hasChange(auditOf(t, h.svc, h.ctx, pm.AuditObjectPlan, string(planID)), pm.AuditPlanOwnerTransferred)
	if entry == nil {
		t.Fatal("missing owner_transferred audit entry")
	}
	if !strings.Contains(entry.Detail, `"attention_status":"attention_required"`) {
		t.Fatalf("audit must record attention_required: %s", entry.Detail)
	}
	if failClosed && !strings.Contains(entry.Detail, `"fail_closed":true`) {
		t.Fatalf("audit must record fail_closed=true: %s", entry.Detail)
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
