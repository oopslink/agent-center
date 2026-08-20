package authorization

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
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
	if metrics := svc.ShadowMetrics(); !metrics.ReadyToEnforce || !svc.ShadowReadyToEnforce() {
		t.Fatalf("shadow diff=0 gate should be ready after equivalent checks: %+v", metrics)
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

func TestT1411ShadowReadinessPersistsAndGatesProductionEnforce(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedProject(t, db, "project-1", "org-1")
	seedProjectMember(t, db, "pm-member", "project-1", "user:user-member", "member")
	seedTeam(t, db)

	checks := []CheckRequest{
		{SubjectRef: "user:user-member", Transport: TransportWeb, Permission: "project.write", Resource: ResourceScope{Kind: "project", ID: "project-1"}},
		{SubjectRef: "user:user-member", Transport: TransportMCP, Permission: "project.read", Resource: ResourceScope{Kind: "project", ID: "project-1"}},
		{SubjectRef: "user:user-member", Transport: TransportBackground, Permission: "org.read", Resource: ResourceScope{Kind: "org", ID: "org-1"}},
	}
	for _, req := range checks {
		if _, err := svc.Check(ctx, req); err != nil {
			t.Fatalf("shadow check %s/%s: %v", req.Transport, req.Permission, err)
		}
		svc.clock.(*clock.FakeClock).Advance(30 * time.Second)
	}
	if err := svc.ValidateEnforceReadiness(ctx, []Transport{TransportWeb, TransportMCP, TransportBackground}, 24*time.Hour); err != nil {
		t.Fatalf("persisted shadow readiness should allow enforce: %v", err)
	}
	shortWindow := New(Deps{DB: db, Store: svc.store, IDGen: svc.gen, Clock: svc.clock, Mode: EnforcementShadow, MinShadowWindow: 2 * time.Minute})
	if err := shortWindow.ValidateEnforceReadiness(ctx, []Transport{TransportWeb, TransportMCP, TransportBackground}, 24*time.Hour); !errors.Is(err, ErrDenied) {
		t.Fatalf("short readiness window err=%v, want ErrDenied", err)
	}
	restarted := New(Deps{DB: db, Store: svc.store, IDGen: svc.gen, Clock: svc.clock, Mode: EnforcementEnforce, RequireEnforceReadiness: true, RequiredShadowTransports: []Transport{TransportWeb, TransportMCP, TransportBackground}})
	if restarted.EnforcementMode() != EnforcementEnforce {
		t.Fatalf("restart did not read persisted readiness, mode=%q", restarted.EnforcementMode())
	}

	execMany(t, db, `UPDATE authorization_shadow_readiness SET window_ended_at=?, updated_at=? WHERE id='current'`, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano))
	if err := svc.ValidateEnforceReadiness(ctx, []Transport{TransportWeb, TransportMCP, TransportBackground}, time.Hour); !errors.Is(err, ErrDenied) {
		t.Fatalf("stale readiness err=%v, want ErrDenied", err)
	}
	execMany(t, db, `UPDATE authorization_shadow_readiness SET window_ended_at=?, transports_json='["web","mcp"]', updated_at=? WHERE id='current'`, svc.clock.Now().UTC().Format(time.RFC3339Nano), svc.clock.Now().UTC().Format(time.RFC3339Nano))
	if err := svc.ValidateEnforceReadiness(ctx, []Transport{TransportWeb, TransportMCP, TransportBackground}, 24*time.Hour); !errors.Is(err, ErrDenied) {
		t.Fatalf("incomplete readiness err=%v, want ErrDenied", err)
	}
}

func TestT1411ForgedReadinessSummaryWithoutAuditFailsClosed(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	now := svc.clock.Now().UTC()
	execMany(t, db, `INSERT INTO authorization_shadow_readiness
		(id, mode, window_started_at, window_ended_at, transports_json, checks, mismatches, legacy_only, equivalent_only, ready, reason, updated_at)
		VALUES ('current', 'shadow', ?, ?, '["web","mcp","background"]', 100, 0, 0, 0, 1, 'forged', ?)`,
		now.Add(-time.Hour).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err := svc.ValidateEnforceReadiness(ctx, []Transport{TransportWeb, TransportMCP, TransportBackground}, 24*time.Hour); !errors.Is(err, ErrDenied) {
		t.Fatalf("forged readiness summary err=%v, want ErrDenied", err)
	}
}

func TestT1411ReadinessMissingEvidenceFailsClosed(t *testing.T) {
	ctx := context.Background()
	db, base := newAuthzTestService(t)
	execMany(t, db, `DELETE FROM authorization_shadow_readiness`)
	enforced := New(Deps{DB: db, Store: base.store, IDGen: base.gen, Clock: base.clock, Mode: EnforcementEnforce, RequireEnforceReadiness: true, RequiredShadowTransports: []Transport{TransportWeb}})
	if enforced.EnforcementMode() != EnforcementShadow {
		t.Fatalf("missing readiness must keep production in shadow, got %q", enforced.EnforcementMode())
	}
	var reason string
	if err := db.QueryRowContext(ctx, `SELECT reason FROM authorization_shadow_readiness WHERE id='current'`).Scan(&reason); err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	if reason == "" {
		t.Fatal("readiness rejection should be persisted for audit")
	}
	var auditCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorization_audit_events WHERE event_type IN ('authorization.enforce_readiness_rejected','authorization.enforce_rollback')`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 2 {
		t.Fatalf("readiness rejection/rollback audit rows = %d, want 2", auditCount)
	}
}

func TestT1411TransportConsistencyForSameEffectivePermission(t *testing.T) {
	ctx := context.Background()
	db, base := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedProject(t, db, "project-1", "org-1")
	seedTeamRAM(t, db)
	enforced := New(Deps{DB: db, Store: base.store, IDGen: base.gen, Clock: base.clock, Mode: EnforcementEnforce})
	var source DecisionSource
	for _, transport := range []Transport{TransportWeb, TransportMCP, TransportBackground} {
		decision, err := enforced.Check(ctx, CheckRequest{
			SubjectRef: "user:user-member",
			Transport:  transport,
			Permission: "project.read",
			Resource:   ResourceScope{Kind: "project", ID: "project-1"},
		})
		if err != nil || !decision.Allowed {
			t.Fatalf("%s decision=%#v err=%v", transport, decision, err)
		}
		if source == "" {
			source = decision.Source
		} else if decision.Source != source {
			t.Fatalf("source mismatch for %s: got %s want %s", transport, decision.Source, source)
		}
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
	if metrics.ReadyToEnforce || svc.ShadowReadyToEnforce() {
		t.Fatalf("shadow diff gate must block enforce while mismatches exist: %+v", metrics)
	}

	enforced := New(Deps{DB: db, Store: svc.store, IDGen: svc.gen, Clock: svc.clock, Mode: EnforcementEnforce})
	if _, err := enforced.Check(ctx, req); !errors.Is(err, ErrDenied) {
		t.Fatalf("enforce should fail closed on equivalent drift, err=%v", err)
	}
}

func TestT1412ProjectMembershipRevokeInvalidatesCachedEffective(t *testing.T) {
	ctx := context.Background()
	db, base := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedProject(t, db, "project-1", "org-1")
	seedProjectMember(t, db, "pm-cached", "project-1", "user:user-member", "member")

	enforced := New(Deps{DB: db, Store: base.store, IDGen: base.gen, Clock: base.clock, Mode: EnforcementEnforce})
	req := CheckRequest{SubjectRef: "user:user-member", Transport: TransportWeb, Permission: "project.write", Resource: ResourceScope{Kind: "project", ID: "project-1"}}
	if _, err := enforced.Check(ctx, req); err != nil {
		t.Fatalf("warm project member cache: %v", err)
	}
	execMany(t, db, `DELETE FROM pm_project_members WHERE id='pm-cached'`)
	decision, err := enforced.Check(ctx, req)
	if !errors.Is(err, ErrDenied) || decision.Allowed {
		t.Fatalf("project membership revoke must invalidate cached effective permissions, decision=%#v err=%v", decision, err)
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
