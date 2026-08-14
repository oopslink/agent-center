package authorization

import (
	"context"
	"testing"
	"time"
)

func TestSupervisorCrossOrgRevokeMustFailClosed(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.ExecContext(ctx, `INSERT INTO authorization_role_assignments
		(id, org_id, subject_ref, role_id, resource_kind, resource_id, created_by, created_at)
		VALUES ('asgn-org-2', 'org-2', 'user:victim', 'sys-org-member', 'org', 'org-2', 'system', ?)`, now); err != nil {
		t.Fatal(err)
	}
	_, err := svc.RevokeBatch(ctx, BatchRequest{
		IdempotencyKey: "cross-org-revoke",
		ActorRef:       "user:user-owner",
		OrgID:          "org-1",
		Operations:     []BatchOperation{{Revoke: RevokeInput{AssignmentID: "asgn-org-2"}}},
	})
	if err == nil {
		t.Fatal("org-1 owner revoked org-2 assignment by id")
	}
}

func TestSupervisorCrossOrgRolePermissionUpdateMustFailClosed(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.ExecContext(ctx, `INSERT INTO authorization_roles
		(id, org_id, kind, name, created_by, created_at, updated_at)
		VALUES ('role-org-2', 'org-2', 'custom', 'foreign', 'system', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	_, err := svc.ApplyBatch(ctx, BatchRequest{
		IdempotencyKey: "cross-org-role-update",
		ActorRef:       "user:user-owner",
		OrgID:          "org-1",
		Operations: []BatchOperation{{
			Type:        "set_role_permissions",
			Role:        RoleInput{ID: "role-org-2"},
			Permissions: []RolePermissionInput{{PermissionKey: "org.read", ResourceKind: "org"}},
		}},
	})
	if err == nil {
		t.Fatal("org-1 owner changed org-2 custom role permissions")
	}
}
