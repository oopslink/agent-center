package authorization

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestT1343ServiceContractMixedBatchSourcesExpiryReclaimAndAudit(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedTeam(t, db)

	execMany(t, db, `INSERT INTO identities (id, kind, display_name, passcode_hash, created_at, updated_at)
		VALUES ('agent-ident-two', 'agent', 'Agent Two', '', ?, ?)`, testNow(), testNow())
	execMany(t, db, `INSERT INTO members (id, organization_id, identity_id, role, status, joined_at)
		VALUES ('mem-agent-two', 'org-1', 'agent-ident-two', 'member', 'joined', ?)`, testNow())
	execMany(t, db, `INSERT INTO team_members (team_id, member_ref, member_kind, role, created_at)
		VALUES ('team-1', 'agent:mem-agent-two', 'agent', 'reviewer', ?)`, testNow())

	expires := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	req := BatchRequest{
		IdempotencyKey: "t1343-mixed-human-agent",
		ActorRef:       "user:user-owner",
		OrgID:          "org-1",
		Operations: []BatchOperation{
			{ID: "role", Type: "upsert_role", Role: RoleInput{ID: "role-team-proposer", Name: "team-proposer"}},
			{ID: "perms", Type: "set_role_permissions", Role: RoleInput{ID: "role-team-proposer"}, Permissions: []RolePermissionInput{{PermissionKey: "team.memory.propose", ResourceKind: "team"}}},
			{ID: "human", Type: "assign_role", Assignment: AssignmentInput{ID: "asgn-human-proposer", SubjectRef: "user:user-member", RoleID: "role-team-proposer", Resource: ResourceScope{Kind: "team", ID: "team-1"}, ExpiresAt: &expires}},
			{ID: "agent", Type: "assign_role", Assignment: AssignmentInput{ID: "asgn-agent-proposer", SubjectRef: "agent:mem-agent", RoleID: "role-team-proposer", Resource: ResourceScope{Kind: "team", ID: "team-1"}, ExpiresAt: &expires}},
		},
	}
	applied, err := svc.ApplyBatch(ctx, req)
	if err != nil {
		t.Fatalf("mixed human/agent batch apply: %v", err)
	}
	if len(applied.Operations) != 4 || applied.Operations[2].Status != "created" || applied.Operations[3].Status != "created" {
		t.Fatalf("mixed batch result = %#v", applied)
	}
	replay, err := svc.ApplyBatch(ctx, req)
	if err != nil || !replay.Replayed {
		t.Fatalf("mixed batch replay = %#v err=%v", replay, err)
	}

	human, err := svc.Check(ctx, CheckRequest{SubjectRef: "user:user-member", Transport: TransportWeb, Permission: "team.memory.propose", Resource: ResourceScope{Kind: "team", ID: "team-1"}})
	if err != nil || human.Source != SourceCustomRole || human.EvidenceRef != "authorization_role_assignments:asgn-human-proposer" {
		t.Fatalf("human custom source decision = %#v err=%v", human, err)
	}
	effective, err := svc.ListEffective(ctx, "agent:mem-agent", ResourceScope{Kind: "team", ID: "team-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !hasEffectiveSource(effective, "team.memory.propose", SourceTeamMember) || !hasEffectiveSource(effective, "team.memory.propose", SourceCustomRole) {
		t.Fatalf("agent did not inherit both team_member and custom sources: %#v", effective.Permissions)
	}

	if _, err := svc.ApplyBatch(ctx, BatchRequest{IdempotencyKey: "t1343-agent-high-risk", ActorRef: "user:user-owner", OrgID: "org-1", Operations: []BatchOperation{{Type: "assign_role", Assignment: AssignmentInput{SubjectRef: "agent:mem-agent", RoleID: "sys-org-owner", Resource: ResourceScope{Kind: "org", ID: "org-1"}}}}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("agent high-risk assignment err=%v, want ErrInvalid", err)
	}
	if _, err := svc.ApplyBatch(ctx, BatchRequest{IdempotencyKey: "t1343-self-escalate", ActorRef: "user:user-admin", OrgID: "org-1", Operations: []BatchOperation{{Type: "assign_role", Assignment: AssignmentInput{SubjectRef: "user:user-admin", RoleID: "sys-org-owner", Resource: ResourceScope{Kind: "org", ID: "org-1"}}}}}); err == nil {
		t.Fatal("non-owner self-escalation unexpectedly succeeded")
	}

	if _, err := svc.ApplyBatch(ctx, BatchRequest{IdempotencyKey: "t1343-custom-role-impact", ActorRef: "user:user-owner", OrgID: "org-1", Operations: []BatchOperation{{Type: "set_role_permissions", Role: RoleInput{ID: "role-team-proposer"}, Permissions: []RolePermissionInput{{PermissionKey: "team.git.read", ResourceKind: "team"}}}}}); err != nil {
		t.Fatalf("custom role permission update: %v", err)
	}
	if _, err := svc.Check(ctx, CheckRequest{SubjectRef: "user:user-member", Permission: "team.memory.propose", Resource: ResourceScope{Kind: "team", ID: "team-1"}}); !errors.Is(err, ErrDenied) {
		t.Fatalf("old custom permission still allowed after role edit: %v", err)
	}
	if got, err := svc.Check(ctx, CheckRequest{SubjectRef: "user:user-member", Permission: "team.git.read", Resource: ResourceScope{Kind: "team", ID: "team-1"}}); err != nil || got.Source != SourceCustomRole {
		t.Fatalf("new custom permission not effective: %#v err=%v", got, err)
	}
	for _, transport := range []Transport{TransportWeb, TransportMCP, TransportSystem} {
		got, err := svc.Check(ctx, CheckRequest{SubjectRef: "user:user-member", Transport: transport, Permission: "team.git.read", Resource: ResourceScope{Kind: "team", ID: "team-1"}})
		if err != nil || got.Source != SourceCustomRole || got.EvidenceRef != "authorization_role_assignments:asgn-human-proposer" {
			t.Fatalf("transport %s custom decision = %#v err=%v", transport, got, err)
		}
	}

	if _, err := svc.Check(ctx, CheckRequest{SubjectRef: "agent:mem-agent-two", Permission: "team.git.write", Resource: ResourceScope{Kind: "team", ID: "team-1"}}); err != nil {
		t.Fatalf("agent two before removal: %v", err)
	}
	execMany(t, db, `DELETE FROM team_members WHERE team_id='team-1' AND member_ref='agent:mem-agent-two'`)
	if _, err := svc.Check(ctx, CheckRequest{SubjectRef: "agent:mem-agent-two", Permission: "team.git.write", Resource: ResourceScope{Kind: "team", ID: "team-1"}}); !errors.Is(err, ErrDenied) {
		t.Fatalf("removed team membership still grants team-1: %v", err)
	}
	if got, err := svc.Check(ctx, CheckRequest{SubjectRef: "user:user-member", Permission: "team.git.read", Resource: ResourceScope{Kind: "team", ID: "team-1"}}); err != nil || got.Source != SourceCustomRole {
		t.Fatalf("team membership removal affected unrelated custom grant: %#v err=%v", got, err)
	}

	past := time.Date(2026, 8, 14, 11, 0, 0, 0, time.UTC)
	if _, err := svc.ApplyBatch(ctx, BatchRequest{IdempotencyKey: "t1343-expired-grant", ActorRef: "system", OrgID: "org-1", Operations: []BatchOperation{
		{Type: "upsert_role", Role: RoleInput{ID: "role-expired-settings", Name: "expired-settings"}},
		{Type: "set_role_permissions", Role: RoleInput{ID: "role-expired-settings"}, Permissions: []RolePermissionInput{{PermissionKey: "org.settings.manage", ResourceKind: "org"}}},
		{Type: "assign_role", Assignment: AssignmentInput{ID: "asgn-expired-settings", SubjectRef: "user:user-member", RoleID: "role-expired-settings", Resource: ResourceScope{Kind: "org", ID: "org-1"}, ExpiresAt: &past}},
	}}); err != nil {
		t.Fatalf("expired grant setup: %v", err)
	}
	if _, err := svc.Check(ctx, CheckRequest{SubjectRef: "user:user-member", Permission: "org.settings.manage", Resource: ResourceScope{Kind: "org", ID: "org-1"}}); !errors.Is(err, ErrDenied) {
		t.Fatalf("expired assignment allowed: %v", err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE members SET role='member' WHERE organization_id='org-1' AND role='owner'`); err != nil {
		t.Fatal(err)
	}
	execMany(t, db, `INSERT INTO authorization_role_assignments
		(id, org_id, subject_ref, role_id, resource_kind, resource_id, created_by, created_at)
		VALUES ('asgn-t1343-last-owner', 'org-1', 'user:user-owner', 'sys-org-owner', 'org', 'org-1', 'system', ?)`, testNow())
	if _, err := svc.RevokeBatch(ctx, BatchRequest{IdempotencyKey: "t1343-last-owner", ActorRef: "system", OrgID: "org-1", Operations: []BatchOperation{{Revoke: RevokeInput{AssignmentID: "asgn-t1343-last-owner"}}}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("last org owner revoke err=%v, want ErrConflict", err)
	}

	var auditEvents int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorization_audit_events WHERE event_type LIKE 'authorization.%'`).Scan(&auditEvents); err != nil {
		t.Fatal(err)
	}
	if auditEvents < 8 {
		t.Fatalf("audit events = %d, want >= 8", auditEvents)
	}
}

func hasEffectiveSource(effective EffectivePermissions, key PermissionKey, source DecisionSource) bool {
	for _, permission := range effective.Permissions {
		if permission.Key == key && permission.Source == source {
			return true
		}
	}
	return false
}

func testNow() string {
	return time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
}
