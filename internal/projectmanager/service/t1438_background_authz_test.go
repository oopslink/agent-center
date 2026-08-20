package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/clock"
)

func TestT1438LeaseCheckerBackgroundAuthorizationAllowThenRevokeDeny(t *testing.T) {
	svc, _, ctx := setup(t)
	authorizer := authz.New(authz.Deps{DB: svc.db, Mode: authz.EnforcementEnforce})
	svc.authorizer = authorizer

	grantBackgroundWorkerCapability(t, ctx, svc.db, "lease_checker")
	checker := NewLeaseChecker(svc, clock.NewFakeClock(time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)), time.Minute, nil)

	if n, err := checker.Tick(ctx); err != nil || n != 0 {
		t.Fatalf("lease checker before revoke n=%d err=%v, want allow and no work", n, err)
	}

	if _, err := authorizer.RevokeBatch(ctx, authz.BatchRequest{
		IdempotencyKey: "t1438-background-worker-revoke",
		ActorRef:       "system",
		OrgID:          "system",
		Operations: []authz.BatchOperation{{
			ID:     "revoke-background-worker",
			Revoke: authz.RevokeInput{AssignmentID: "asgn-t1438-background-worker-lease-checker", Reason: "t1438 background loop revoke"},
		}},
	}); err != nil {
		t.Fatalf("revoke background worker binding: %v", err)
	}

	n, err := checker.Tick(ctx)
	if !errors.Is(err, authz.ErrDenied) || n != 0 {
		t.Fatalf("lease checker after revoke n=%d err=%v, want ErrDenied before sweep", n, err)
	}
}

func grantBackgroundWorkerCapability(t *testing.T, ctx context.Context, db *sql.DB, operation string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	assignmentID := "asgn-t1438-background-worker-" + strings.ReplaceAll(operation, "_", "-")
	if _, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO authorization_roles (id, org_id, kind, name, description, created_by, created_at, updated_at, version)
		VALUES ('role-t1438-background-worker', 'system', 'custom', 'T1438 background worker', '', 'system', ?, ?, 1)`, now, now); err != nil {
		t.Fatalf("grant background worker role: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO authorization_role_permissions (role_id, permission_key, resource_kind, delegatable, created_at)
		VALUES ('role-t1438-background-worker', 'worker.capability.report', 'worker', 0, ?)`, now); err != nil {
		t.Fatalf("grant background worker permission: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO authorization_role_assignments
			(id, org_id, subject_ref, role_id, resource_kind, resource_id, created_by, created_at, version)
		VALUES (?, 'system', ?, 'role-t1438-background-worker', 'worker', ?, 'system', ?, 1)`,
		assignmentID, authz.AgentSubject("background"), "background:"+operation, now); err != nil {
		t.Fatalf("grant background worker binding: %v", err)
	}
}
