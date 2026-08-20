package authorization

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestT1411EnforcementModesRejectLongTermORAllow(t *testing.T) {
	if got := NormalizeEnforcementMode(""); got != EnforcementShadow {
		t.Fatalf("default mode = %q want shadow", got)
	}
	for _, raw := range []string{"or", "dual_allow", "fallback"} {
		if _, err := ParseEnforcementMode(raw); !errors.Is(err, ErrInvalid) {
			t.Fatalf("mode %q err=%v want ErrInvalid", raw, err)
		}
	}
	if got, err := ParseEnforcementMode("enforce"); err != nil || got != EnforcementEnforce {
		t.Fatalf("parse enforce = %q err=%v", got, err)
	}
}

func TestT1411ShadowCompareBuiltinRoleEquivalenceAndEnforce(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedProject(t, db, "project-1", "org-1")
	seedProjectMember(t, db, "pm-member", "project-1", "user:user-member", "member")
	seedTeam(t, db)

	reqs := []CheckRequest{
		{SubjectRef: "user:user-member", Permission: "org.read", Resource: ResourceScope{Kind: "org", ID: "org-1"}},
		{SubjectRef: "user:user-member", Permission: "project.write", Resource: ResourceScope{Kind: "project", ID: "project-1"}},
		{SubjectRef: "user:user-member", Permission: "team.member.manage", Resource: ResourceScope{Kind: "team", ID: "team-1"}},
		{SubjectRef: "agent:mem-agent", Permission: "team.git.write", Resource: ResourceScope{Kind: "team", ID: "team-1"}},
	}

	for _, req := range reqs {
		if _, err := svc.Check(ctx, req); err != nil {
			t.Fatalf("shadow legacy decision for %s/%s: %v", req.Resource.Kind, req.Permission, err)
		}
	}
	if metrics := svc.ShadowMetrics(); metrics.Checks != int64(len(reqs)) || metrics.Mismatches != 0 {
		t.Fatalf("shadow metrics after equivalent checks = %+v", metrics)
	}

	enforced := New(Deps{DB: db, Store: svc.store, IDGen: svc.gen, Clock: svc.clock, Mode: EnforcementEnforce})
	for _, req := range reqs {
		if _, err := enforced.Check(ctx, req); err != nil {
			t.Fatalf("enforce equivalent decision for %s/%s: %v", req.Resource.Kind, req.Permission, err)
		}
	}

	legacy := New(Deps{DB: db, Store: svc.store, IDGen: svc.gen, Clock: svc.clock, Mode: EnforcementLegacy})
	if _, err := legacy.Check(ctx, reqs[0]); err != nil {
		t.Fatalf("legacy rollback decision: %v", err)
	}
	if metrics := legacy.ShadowMetrics(); metrics.Checks != 0 {
		t.Fatalf("legacy mode should not shadow-compare, metrics=%+v", metrics)
	}
}

func TestT1438PersistedReadinessControlsEnforceStartup(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedProject(t, db, "project-1", "org-1")
	seedProjectMember(t, db, "pm-member", "project-1", "user:user-member", "member")

	required := []Transport{TransportWeb, TransportMCP, TransportBackground}
	reqs := []CheckRequest{
		{SubjectRef: "user:user-member", Transport: TransportWeb, Permission: "org.read", Resource: ResourceScope{Kind: "org", ID: "org-1"}},
		{SubjectRef: "user:user-member", Transport: TransportMCP, Permission: "project.read", Resource: ResourceScope{Kind: "project", ID: "project-1"}},
		{SubjectRef: "user:user-member", Transport: TransportBackground, Permission: "project.write", Resource: ResourceScope{Kind: "project", ID: "project-1"}},
	}
	for _, req := range reqs {
		if _, err := svc.Check(ctx, req); err != nil {
			t.Fatalf("shadow check %s/%s/%s: %v", req.Transport, req.Resource.Kind, req.Permission, err)
		}
	}
	if err := svc.ValidateEnforceReadiness(ctx, required, time.Hour); err != nil {
		t.Fatalf("persisted readiness should validate: %v", err)
	}
	ready, err := svc.ShadowReadiness(ctx)
	if err != nil {
		t.Fatalf("readiness readback: %v", err)
	}
	if !ready.Ready || ready.Checks != int64(len(reqs)) || ready.Mismatches != 0 {
		t.Fatalf("readiness = %+v", ready)
	}
	execMany(t, db, `UPDATE authorization_shadow_readiness SET window_started_at = '2000-01-01T00:00:00Z' WHERE id = 'current'`)

	enforced := New(Deps{
		DB: db, Store: svc.store, IDGen: svc.gen, Clock: svc.clock, Mode: EnforcementEnforce,
		RequireEnforceReadiness: true, RequiredShadowTransports: required, MinShadowChecks: int64(len(reqs)),
	})
	if enforced.EnforcementMode() != EnforcementEnforce {
		t.Fatalf("startup should remain enforce after durable readiness, got %s", enforced.EnforcementMode())
	}

	execMany(t, db, `DELETE FROM authorization_shadow_readiness`)
	rolledBack := New(Deps{
		DB: db, Store: svc.store, IDGen: svc.gen, Clock: svc.clock, Mode: EnforcementEnforce,
		RequireEnforceReadiness: true, RequiredShadowTransports: required, MinShadowChecks: int64(len(reqs)),
	})
	if rolledBack.EnforcementMode() != EnforcementShadow {
		t.Fatalf("startup without readiness must fail closed to shadow, got %s", rolledBack.EnforcementMode())
	}
	events, err := rolledBack.store.listAuditEventsForSubject(ctx, "system", 10)
	if err != nil {
		t.Fatalf("rollback audit readback: %v", err)
	}
	if !hasAuditEventType(events, "authorization.enforce_rollback") {
		t.Fatalf("missing enforce rollback audit event: %+v", events)
	}
}

func TestT1411ShadowMetricsExposeLegacyEquivalentDrift(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedTeam(t, db)
	execMany(t, db, `DELETE FROM authorization_role_permissions WHERE role_id = 'sys-team-web-member' AND permission_key = 'team.member.manage'`)

	req := CheckRequest{
		SubjectRef: "user:user-member",
		Permission: "team.member.manage",
		Resource:   ResourceScope{Kind: "team", ID: "team-1"},
	}
	if _, err := svc.Check(ctx, req); err != nil {
		t.Fatalf("shadow mode must preserve legacy allow despite drift: %v", err)
	}
	metrics := svc.ShadowMetrics()
	if metrics.Checks != 1 || metrics.Mismatches != 1 || metrics.LegacyOnly == 0 {
		t.Fatalf("shadow metrics did not capture drift: %+v", metrics)
	}

	enforced := New(Deps{DB: db, Store: svc.store, IDGen: svc.gen, Clock: svc.clock, Mode: EnforcementEnforce})
	if _, err := enforced.Check(ctx, req); !errors.Is(err, ErrDenied) {
		t.Fatalf("enforce should fail closed on equivalent drift, err=%v", err)
	}
}

func TestT1412EnforceIncludesTeamRoleRAMAndRevokesImmediately(t *testing.T) {
	ctx := context.Background()
	db, base := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedProject(t, db, "project-1", "org-1")
	seedTeamRAM(t, db)

	enforced := New(Deps{DB: db, Store: base.store, IDGen: base.gen, Clock: base.clock, Mode: EnforcementEnforce})
	req := CheckRequest{
		SubjectRef: "user:user-member",
		Transport:  TransportWeb,
		Permission: "project.read",
		Resource:   ResourceScope{Kind: "project", ID: "project-1"},
	}
	decision, err := enforced.Check(ctx, req)
	if err != nil || !decision.Allowed || decision.Source != SourceTeamRoleRAM {
		t.Fatalf("enforce team RAM decision=%#v err=%v", decision, err)
	}

	execMany(t, db, `DELETE FROM team_role_ram_role_mappings WHERE team_id='team-ram' AND team_role='developer'`)
	execMany(t, db, `UPDATE team_role_ram_role_versions SET version=version+1 WHERE team_id='team-ram' AND team_role='developer'`)
	decision, err = enforced.Check(ctx, req)
	if !errors.Is(err, ErrDenied) || decision.Allowed {
		t.Fatalf("enforce must fail closed after mapping revoke, decision=%#v err=%v", decision, err)
	}
}

func TestT1413EnforceConversationPostUsesOwnedTaskProjectScope(t *testing.T) {
	ctx := context.Background()
	db, base := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedProject(t, db, "project-1", "org-1")
	seedProjectMember(t, db, "pm-member", "project-1", "user:user-member", "member")
	now := "2026-08-14T12:00:00Z"
	execMany(t, db, `INSERT INTO pm_tasks (id, project_id, title, status, created_by, created_at, updated_at)
		VALUES ('task-owned-conv', 'project-1', 'Task', 'open', 'user:user-owner', ?, ?)`, now, now)
	execMany(t, db, `INSERT INTO conversations (id, kind, owner_ref, organization_id, status, opened_at, participants, created_by, created_at, updated_at, version)
		VALUES ('conv-task-owned', 'task', 'pm://tasks/task-owned-conv', 'org-1', 'active', ?, '[]', 'system', ?, ?, 1)`, now, now, now)

	enforced := New(Deps{DB: db, Store: base.store, IDGen: base.gen, Clock: base.clock, Mode: EnforcementEnforce})
	decision, err := enforced.Check(ctx, CheckRequest{
		SubjectRef: "user:user-member",
		Transport:  TransportWeb,
		Permission: "conversation.post",
		Resource:   ResourceScope{Kind: "conversation", ID: "conv-task-owned"},
	})
	if err != nil || !decision.Allowed || decision.Source != SourceProjectMember {
		t.Fatalf("task-owned conversation post decision=%#v err=%v", decision, err)
	}

	execMany(t, db, `DELETE FROM pm_project_members WHERE id='pm-member'`)
	enforced = New(Deps{DB: db, Store: base.store, IDGen: base.gen, Clock: base.clock, Mode: EnforcementEnforce})
	decision, err = enforced.Check(ctx, CheckRequest{
		SubjectRef: "user:user-member",
		Transport:  TransportWeb,
		Permission: "conversation.post",
		Resource:   ResourceScope{Kind: "conversation", ID: "conv-task-owned"},
	})
	if !errors.Is(err, ErrDenied) || decision.Allowed {
		t.Fatalf("conversation post must fail closed after project membership revoke, decision=%#v err=%v", decision, err)
	}
}

func hasAuditEventType(events []AuditEvent, eventType string) bool {
	for _, event := range events {
		if event.EventType == eventType {
			return true
		}
	}
	return false
}
