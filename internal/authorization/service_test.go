package authorization

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
)

func TestService_LegacyOrgRolesAndDisabledOrgOwnerException(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)

	owner, err := svc.Check(ctx, CheckRequest{
		SubjectRef: "user:user-owner",
		Transport:  TransportWeb,
		Permission: "org.member.role.manage",
		Resource:   ResourceScope{Kind: "org", ID: "org-1"},
	})
	if err != nil || !owner.Allowed || owner.Source != SourceOrgRole {
		t.Fatalf("owner role manage decision = %#v err=%v", owner, err)
	}

	admin, err := svc.Check(ctx, CheckRequest{
		SubjectRef: "user:user-admin",
		Transport:  TransportWeb,
		Permission: "org.member.create.agent",
		Resource:   ResourceScope{Kind: "org", ID: "org-1"},
	})
	if err != nil || !admin.Allowed {
		t.Fatalf("admin create agent decision = %#v err=%v", admin, err)
	}
	denied, err := svc.Check(ctx, CheckRequest{
		SubjectRef: "user:user-admin",
		Transport:  TransportWeb,
		Permission: "org.member.role.manage",
		Resource:   ResourceScope{Kind: "org", ID: "org-1"},
	})
	if !errors.Is(err, ErrDenied) || denied.Allowed {
		t.Fatalf("admin role manage should deny, decision=%#v err=%v", denied, err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE organizations SET disabled_at = ? WHERE id = 'org-1'`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Check(ctx, CheckRequest{SubjectRef: "user:user-owner", Transport: TransportWeb, Permission: "org.read", Resource: ResourceScope{Kind: "org", ID: "org-1"}}); err != nil {
		t.Fatalf("disabled org owner should still read: %v", err)
	}
	member, err := svc.Check(ctx, CheckRequest{SubjectRef: "user:user-member", Transport: TransportWeb, Permission: "org.read", Resource: ResourceScope{Kind: "org", ID: "org-1"}})
	if !errors.Is(err, ErrDenied) || member.Allowed {
		t.Fatalf("disabled org member should deny, decision=%#v err=%v", member, err)
	}
}

func TestService_ProjectMembershipCustomRolesAndRevoke(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedProject(t, db, "project-1", "org-1")
	seedProjectMember(t, db, "pm-owner", "project-1", "user:user-owner", "owner")
	seedProjectMember(t, db, "pm-member", "project-1", "user:user-admin", "member")

	if _, err := svc.Check(ctx, CheckRequest{SubjectRef: "user:user-owner", Permission: "project.member.remove", Resource: ResourceScope{Kind: "project", ID: "project-1"}}); err != nil {
		t.Fatalf("project owner remove should allow: %v", err)
	}
	denied, err := svc.Check(ctx, CheckRequest{SubjectRef: "user:user-admin", Permission: "project.member.remove", Resource: ResourceScope{Kind: "project", ID: "project-1"}})
	if !errors.Is(err, ErrDenied) || denied.Allowed {
		t.Fatalf("project member remove should deny, decision=%#v err=%v", denied, err)
	}
	_, err = svc.Check(ctx, CheckRequest{SubjectRef: "user:user-owner", Permission: "project.read", Resource: ResourceScope{Kind: "project", ID: "project-1", OrgID: "org-2"}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-org project should be hidden as not found, got %v", err)
	}

	req := BatchRequest{
		IdempotencyKey: "idem-project-reader",
		ActorRef:       "user:user-owner",
		OrgID:          "org-1",
		Operations: []BatchOperation{
			{ID: "role", Type: "upsert_role", Role: RoleInput{ID: "role-project-reader", Name: "project-reader"}},
			{ID: "perms", Type: "set_role_permissions", Role: RoleInput{ID: "role-project-reader"}, Permissions: []RolePermissionInput{{PermissionKey: "project.read", ResourceKind: "project"}}},
			{ID: "assign", Type: "assign_role", Assignment: AssignmentInput{ID: "asgn-reader", SubjectRef: "user:user-member", RoleID: "role-project-reader", Resource: ResourceScope{Kind: "project", ID: "project-1"}}},
		},
	}
	applied, err := svc.ApplyBatch(ctx, req)
	if err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}
	if len(applied.Operations) != 3 || applied.Operations[2].Status != "created" {
		t.Fatalf("apply result = %#v", applied)
	}
	replay, err := svc.ApplyBatch(ctx, req)
	if err != nil || !replay.Replayed {
		t.Fatalf("idempotent replay = %#v err=%v", replay, err)
	}
	if _, err := svc.Check(ctx, CheckRequest{SubjectRef: "user:user-member", Permission: "project.read", Resource: ResourceScope{Kind: "project", ID: "project-1"}}); err != nil {
		t.Fatalf("custom role project.read should allow: %v", err)
	}
	var auditCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorization_audit_events WHERE event_type LIKE 'authorization.%'`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount < 3 {
		t.Fatalf("audit events count = %d want >=3", auditCount)
	}

	revoked, err := svc.RevokeBatch(ctx, BatchRequest{
		IdempotencyKey: "idem-revoke-project-reader",
		ActorRef:       "user:user-owner",
		OrgID:          "org-1",
		Operations:     []BatchOperation{{ID: "revoke", Revoke: RevokeInput{AssignmentID: "asgn-reader", Reason: "test"}}},
	})
	if err != nil || revoked.Operations[0].Status != "revoked" {
		t.Fatalf("revoke = %#v err=%v", revoked, err)
	}
	after, err := svc.Check(ctx, CheckRequest{SubjectRef: "user:user-member", Permission: "project.read", Resource: ResourceScope{Kind: "project", ID: "project-1"}})
	if !errors.Is(err, ErrDenied) || after.Allowed {
		t.Fatalf("revoked custom assignment should deny, decision=%#v err=%v", after, err)
	}
}

func TestSupervisorCrossOrgRevokeMustFailClosed(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedRoleAssignment(t, db, "org-1", "role-same-org", "asgn-same-org", "user:user-member", "org", "org-1")
	seedRoleAssignment(t, db, "org-2", "role-cross-org", "asgn-cross-org", "user:user-member", "org", "org-2")

	for _, tc := range []struct {
		name   string
		revoke RevokeInput
	}{
		{name: "by assignment id", revoke: RevokeInput{AssignmentID: "asgn-cross-org", Reason: "cross-org-id"}},
		{name: "by composite key", revoke: RevokeInput{SubjectRef: "user:user-member", RoleID: "role-cross-org", Resource: ResourceScope{Kind: "org", ID: "org-2"}, Reason: "cross-org-composite"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.RevokeBatch(ctx, BatchRequest{
				IdempotencyKey: "idem-" + shortHash(tc.name),
				ActorRef:       "user:user-owner",
				OrgID:          "org-1",
				Operations: []BatchOperation{
					{ID: "same-org-first", Revoke: RevokeInput{AssignmentID: "asgn-same-org", Reason: "must-roll-back"}},
					{ID: "cross-org", Revoke: tc.revoke},
				},
			})
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("cross-org revoke error = %v, want ErrNotFound fail-closed", err)
			}
			assertAssignmentRevoked(t, db, "org-1", "asgn-same-org", false)
			assertAssignmentRevoked(t, db, "org-2", "asgn-cross-org", false)
		})
	}
}

func TestService_RevokeAssignmentSameOrgValidAndIdempotent(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedRoleAssignment(t, db, "org-1", "role-revoke-ok", "asgn-revoke-ok", "user:user-member", "org", "org-1")

	req := BatchRequest{
		IdempotencyKey: "idem-revoke-ok",
		ActorRef:       "user:user-owner",
		OrgID:          "org-1",
		Operations:     []BatchOperation{{ID: "revoke", Revoke: RevokeInput{AssignmentID: "asgn-revoke-ok", Reason: "same-org"}}},
	}
	res, err := svc.RevokeBatch(ctx, req)
	if err != nil {
		t.Fatalf("same-org RevokeBatch: %v", err)
	}
	if len(res.Operations) != 1 || res.Operations[0].Status != "revoked" || res.Operations[0].AssignmentID != "asgn-revoke-ok" {
		t.Fatalf("same-org revoke result = %#v", res)
	}
	assertAssignmentRevoked(t, db, "org-1", "asgn-revoke-ok", true)

	replay, err := svc.RevokeBatch(ctx, req)
	if err != nil {
		t.Fatalf("same-key revoke replay: %v", err)
	}
	if !replay.Replayed || len(replay.Operations) != 1 || replay.Operations[0].Status != "revoked" {
		t.Fatalf("same-key replay = %#v", replay)
	}

	second, err := svc.RevokeBatch(ctx, BatchRequest{
		IdempotencyKey: "idem-revoke-ok-second",
		ActorRef:       "user:user-owner",
		OrgID:          "org-1",
		Operations:     []BatchOperation{{ID: "revoke-again", Revoke: RevokeInput{SubjectRef: "user:user-member", RoleID: "role-revoke-ok", Resource: ResourceScope{Kind: "org", ID: "org-1"}, Reason: "again"}}},
	})
	if err != nil {
		t.Fatalf("second revoke should be idempotent: %v", err)
	}
	if len(second.Operations) != 1 || second.Operations[0].Status != "unchanged" {
		t.Fatalf("second revoke = %#v, want unchanged", second)
	}
}

func TestService_RevokeAssignmentConcurrentRaceIdempotent(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedRoleAssignment(t, db, "org-1", "role-race", "asgn-race", "user:user-member", "org", "org-1")

	const n = 8
	results := make(chan OperationResult, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := svc.RevokeBatch(ctx, BatchRequest{
				IdempotencyKey: fmt.Sprintf("idem-race-%d", i),
				ActorRef:       "user:user-owner",
				OrgID:          "org-1",
				Operations:     []BatchOperation{{ID: fmt.Sprintf("revoke-%d", i), Revoke: RevokeInput{AssignmentID: "asgn-race", Reason: "race"}}},
			})
			if err != nil {
				errs <- err
				return
			}
			if len(res.Operations) != 1 {
				errs <- fmt.Errorf("operation count = %d", len(res.Operations))
				return
			}
			results <- res.Operations[0]
		}(i)
	}
	wg.Wait()
	close(errs)
	close(results)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent revoke error: %v", err)
		}
	}
	var revoked, unchanged int
	for res := range results {
		switch res.Status {
		case "revoked":
			revoked++
		case "unchanged":
			unchanged++
		default:
			t.Fatalf("unexpected concurrent revoke result: %#v", res)
		}
	}
	if revoked != 1 || unchanged != n-1 {
		t.Fatalf("concurrent statuses: revoked=%d unchanged=%d want 1/%d", revoked, unchanged, n-1)
	}
	assertAssignmentRevoked(t, db, "org-1", "asgn-race", true)
}

func TestService_TeamMemoryCuratorAndRuntimeTagsDoNotGrantAccess(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedTeam(t, db)

	denied, err := svc.Check(ctx, CheckRequest{
		SubjectRef: "agent:mem-agent",
		Transport:  TransportMCP,
		Permission: "team.memory.review",
		Resource:   ResourceScope{Kind: "team", ID: "team-1"},
	})
	if !errors.Is(err, ErrDenied) || denied.Allowed {
		t.Fatalf("team role capability_tags must not grant review, decision=%#v err=%v", denied, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO team_memory_policy_curators (team_id, agent_ref, created_at) VALUES ('team-1', 'agent:mem-agent', ?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	allowed, err := svc.Check(ctx, CheckRequest{
		SubjectRef: "agent:mem-agent",
		Transport:  TransportMCP,
		Permission: "team.memory.review",
		Resource:   ResourceScope{Kind: "team", ID: "team-1"},
	})
	if err != nil || !allowed.Allowed || allowed.Source != SourceTeamMemoryPolicy {
		t.Fatalf("curator should review, decision=%#v err=%v", allowed, err)
	}
}

func TestService_AdminBearerScopeMapping(t *testing.T) {
	ctx := context.Background()
	_, svc := newAuthzTestService(t)
	for _, tc := range []struct {
		name        string
		bearerScope string
		permission  PermissionKey
		resource    ResourceScope
	}{
		{"exact", "secret:resolve", "secret.resolve", ResourceScope{Kind: "secret", ID: "*"}},
		{"wildcard", "*", "blob.put", ResourceScope{Kind: "blob", ID: "*"}},
		{"task wildcard", "task:*", "task.internal.report", ResourceScope{Kind: "task", ID: "*"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decision, err := svc.Check(ctx, CheckRequest{
				SubjectRef:  "user:cli:test",
				Transport:   TransportAdminHTTP,
				BearerScope: tc.bearerScope,
				Permission:  tc.permission,
				Resource:    tc.resource,
			})
			if err != nil || !decision.Allowed || decision.Source != SourceAdminTokenScope {
				t.Fatalf("decision=%#v err=%v", decision, err)
			}
		})
	}
	denied, err := svc.Check(ctx, CheckRequest{
		SubjectRef: "user:cli:test",
		Transport:  TransportAdminHTTP,
		Permission: "secret.resolve",
		Resource:   ResourceScope{Kind: "secret", ID: "*"},
	})
	if !errors.Is(err, ErrDenied) || denied.Allowed {
		t.Fatalf("missing bearer scope should deny, decision=%#v err=%v", denied, err)
	}
}

func TestService_ApplyBatchConcurrentIdempotency(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	req := BatchRequest{
		IdempotencyKey: "idem-concurrent-role",
		ActorRef:       "user:user-owner",
		OrgID:          "org-1",
		Operations:     []BatchOperation{{ID: "role", Type: "upsert_role", Role: RoleInput{ID: "role-concurrent", Name: "concurrent"}}},
	}
	const n = 8
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.ApplyBatch(ctx, req)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent ApplyBatch error: %v", err)
		}
	}
	var roles int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorization_roles WHERE id = 'role-concurrent'`).Scan(&roles); err != nil {
		t.Fatal(err)
	}
	if roles != 1 {
		t.Fatalf("role rows = %d want 1", roles)
	}
}

func TestService_CustomRoleNegativeAuthorization(t *testing.T) {
	ctx := context.Background()
	_, svc := newAuthzTestService(t)
	seedAuthzBase(t, mustDB(t, svc))
	preview, err := svc.PreviewBatch(ctx, BatchRequest{
		ActorRef: "user:user-admin",
		OrgID:    "org-1",
		Operations: []BatchOperation{{
			ID:   "role",
			Type: "upsert_role",
			Role: RoleInput{ID: "role-admin-escalate", Name: "admin-escalate"},
		}},
	})
	if err != nil {
		t.Fatalf("preview should return denied operation, not error: %v", err)
	}
	if len(preview.Operations) != 1 || preview.Operations[0].Status != "denied" {
		t.Fatalf("preview result = %#v", preview)
	}
	_, err = svc.ApplyBatch(ctx, BatchRequest{
		IdempotencyKey: "idem-admin-escalate",
		ActorRef:       "user:user-admin",
		OrgID:          "org-1",
		Operations:     []BatchOperation{{ID: "role", Type: "upsert_role", Role: RoleInput{ID: "role-admin-escalate", Name: "admin-escalate"}}},
	})
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("admin upsert role should deny, got %v", err)
	}
}

func newAuthzTestService(t *testing.T) (*sql.DB, *Service) {
	t.Helper()
	db, err := persistence.Open(t.TempDir() + "/authz.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc := clock.NewFakeClock(time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGeneratorWithReader(fc, idgen.DeterministicReader(7))
	return db, New(Deps{DB: db, IDGen: gen, Clock: fc})
}

func mustDB(t *testing.T, svc *Service) *sql.DB {
	t.Helper()
	if svc == nil || svc.db == nil {
		t.Fatal("nil service db")
	}
	return svc.db
}

func seedAuthzBase(t *testing.T, db *sql.DB) {
	t.Helper()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	execMany(t, db,
		`INSERT INTO identities (id, kind, display_name, passcode_hash, created_at, updated_at) VALUES ('user-owner', 'user', 'Owner', 'x', ?, ?)`,
		now, now,
	)
	execMany(t, db,
		`INSERT INTO identities (id, kind, display_name, passcode_hash, created_at, updated_at) VALUES ('user-admin', 'user', 'Admin', 'x', ?, ?)`,
		now, now,
	)
	execMany(t, db,
		`INSERT INTO identities (id, kind, display_name, passcode_hash, created_at, updated_at) VALUES ('user-member', 'user', 'Member', 'x', ?, ?)`,
		now, now,
	)
	execMany(t, db,
		`INSERT INTO identities (id, kind, display_name, passcode_hash, created_at, updated_at) VALUES ('agent-ident', 'agent', 'Agent', '', ?, ?)`,
		now, now,
	)
	execMany(t, db,
		`INSERT INTO organizations (id, slug, name, created_by_identity_id, created_at, updated_at) VALUES ('org-1', 'org-one', 'Org One', 'user-owner', ?, ?)`,
		now, now,
	)
	execMany(t, db,
		`INSERT INTO organizations (id, slug, name, created_by_identity_id, created_at, updated_at) VALUES ('org-2', 'org-two', 'Org Two', 'user-owner', ?, ?)`,
		now, now,
	)
	for _, m := range []struct {
		id, identityID, role string
	}{
		{"mem-owner", "user-owner", "owner"},
		{"mem-admin", "user-admin", "admin"},
		{"mem-member", "user-member", "member"},
		{"mem-agent", "agent-ident", "member"},
	} {
		execMany(t, db,
			`INSERT INTO members (id, organization_id, identity_id, role, status, joined_at) VALUES (?, 'org-1', ?, ?, 'joined', ?)`,
			m.id, m.identityID, m.role, now,
		)
	}
}

func seedProject(t *testing.T, db *sql.DB, projectID, orgID string) {
	t.Helper()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	execMany(t, db,
		`INSERT INTO pm_projects (id, organization_id, name, description, status, created_by, created_at, updated_at) VALUES (?, ?, 'Project', '', 'active', 'user:user-owner', ?, ?)`,
		projectID, orgID, now, now,
	)
}

func seedProjectMember(t *testing.T, db *sql.DB, id, projectID, identityRef, role string) {
	t.Helper()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	execMany(t, db,
		`INSERT INTO pm_project_members (id, project_id, identity_id, role, added_by, created_at) VALUES (?, ?, ?, ?, 'test', ?)`,
		id, projectID, identityRef, role, now,
	)
}

func seedTeam(t *testing.T, db *sql.DB) {
	t.Helper()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	execMany(t, db, `INSERT INTO teams (id, org_id, name, description, created_at, updated_at) VALUES ('team-1', 'org-1', 'Team', '', ?, ?)`, now, now)
	execMany(t, db, `INSERT INTO team_roles (team_id, role, cli, model, capability_tags, max_concurrency, created_at) VALUES ('team-1', 'reviewer', 'codex', 'gpt-5', '["team.memory.review"]', 1, ?)`, now)
	execMany(t, db, `INSERT INTO team_members (team_id, member_ref, member_kind, role, created_at) VALUES ('team-1', 'agent:mem-agent', 'agent', 'reviewer', ?)`, now)
}

func seedRoleAssignment(t *testing.T, db *sql.DB, orgID, roleID, assignmentID string, subject SubjectRef, resourceKind, resourceID string) {
	t.Helper()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	execMany(t, db,
		`INSERT INTO authorization_roles (id, org_id, kind, name, description, created_by, created_at, updated_at, version)
		 VALUES (?, ?, 'custom', ?, '', 'system', ?, ?, 1)`,
		roleID, orgID, roleID, now, now,
	)
	execMany(t, db,
		`INSERT INTO authorization_role_permissions (role_id, permission_key, resource_kind, delegatable, created_at)
		 VALUES (?, 'org.read', 'org', 1, ?)`,
		roleID, now,
	)
	execMany(t, db,
		`INSERT INTO authorization_role_assignments
		 (id, org_id, subject_ref, role_id, resource_kind, resource_id, created_by, created_at, version)
		 VALUES (?, ?, ?, ?, ?, ?, 'system', ?, 1)`,
		assignmentID, orgID, subject, roleID, resourceKind, resourceID, now,
	)
}

func assertAssignmentRevoked(t *testing.T, db *sql.DB, orgID, assignmentID string, want bool) {
	t.Helper()
	var revoked sql.NullString
	if err := db.QueryRowContext(context.Background(),
		`SELECT revoked_at FROM authorization_role_assignments WHERE org_id = ? AND id = ?`,
		orgID, assignmentID,
	).Scan(&revoked); err != nil {
		t.Fatalf("read assignment %s/%s: %v", orgID, assignmentID, err)
	}
	if got := revoked.Valid && revoked.String != ""; got != want {
		t.Fatalf("assignment %s/%s revoked=%v want %v", orgID, assignmentID, got, want)
	}
}

func execMany(t *testing.T, db *sql.DB, stmt string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), stmt, args...); err != nil {
		t.Fatalf("exec %s: %v", stmt, err)
	}
}
