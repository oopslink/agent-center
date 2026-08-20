package service

import (
	"errors"
	"testing"
	"time"

	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/clock"
)

func TestT1438LeaseCheckerBackgroundAuthorizationAllowThenRevokeDeny(t *testing.T) {
	svc, _, ctx := setup(t)
	authorizer := authz.New(authz.Deps{DB: svc.db, Mode: authz.EnforcementEnforce})
	svc.authorizer = authorizer

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
			Revoke: authz.RevokeInput{AssignmentID: "asgn-background-worker-lease-checker", Reason: "t1438 background loop revoke"},
		}},
	}); err != nil {
		t.Fatalf("revoke background worker binding: %v", err)
	}

	n, err := checker.Tick(ctx)
	if !errors.Is(err, authz.ErrDenied) || n != 0 {
		t.Fatalf("lease checker after revoke n=%d err=%v, want ErrDenied before sweep", n, err)
	}
}

func TestT1438MigratedBackgroundAuthorizationCoversFixedProductionOperationsOnly(t *testing.T) {
	svc, _, ctx := setup(t)
	authorizer := authz.New(authz.Deps{DB: svc.db, Mode: authz.EnforcementEnforce})
	svc.authorizer = authorizer

	for _, operation := range []string{"auto_assign_reconciler", "lease_checker", "overdue_block_reminder", "plan_reconcile", "resolved_issue_closer"} {
		if err := svc.requireBackgroundAuthorization(ctx, operation); err != nil {
			t.Fatalf("migrated background authorization for %s: %v", operation, err)
		}
	}
	if err := svc.requireBackgroundAuthorization(ctx, "unregistered_operation"); !errors.Is(err, authz.ErrDenied) {
		t.Fatalf("unregistered background operation err=%v, want ErrDenied", err)
	}

	var roleCount, assignmentCount, auditCount int
	if err := svc.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorization_roles WHERE id = 'sys-background-worker' AND kind = 'system'`).Scan(&roleCount); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorization_role_assignments WHERE subject_ref = 'agent:background' AND role_id = 'sys-background-worker' AND resource_kind = 'worker' AND resource_id LIKE 'background:%' AND revoked_at IS NULL`).Scan(&assignmentCount); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorization_audit_events WHERE request_id = 'migration:0139' AND event_type = 'authorization.assignment.created' AND subject_ref = 'agent:background'`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if roleCount != 1 || assignmentCount != 5 || auditCount != 5 {
		t.Fatalf("migrated background seed role=%d assignments=%d audit=%d, want 1/5/5", roleCount, assignmentCount, auditCount)
	}
}
