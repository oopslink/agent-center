package service

import (
	"context"
	"errors"
	"testing"
	"time"

	authz "github.com/oopslink/agent-center/internal/authorization"
)

func TestBackgroundAuthorizationEnforceModeLeastPrivilegeAndTicksReach(t *testing.T) {
	h := planAdvanceSetup(t)
	enforced := authz.New(authz.Deps{DB: h.rawDB, Mode: authz.EnforcementEnforce})
	ctx := context.Background()

	for _, tc := range []struct {
		capability string
		permission authz.PermissionKey
	}{
		{"auto_assign", "background.auto_assign"},
		{"lease_check", "background.lease_check"},
		{"overdue_block_reminder", "background.overdue_block_reminder"},
		{"plan_reconcile", "background.plan_reconcile"},
		{"resolved_issue_close", "background.resolved_issue_close"},
	} {
		decision, err := enforced.Check(ctx, authz.CheckRequest{
			SubjectRef: authz.BackgroundSubject,
			Transport:  authz.TransportSystem,
			Permission: tc.permission,
			Resource:   authz.BackgroundResource(tc.capability),
		})
		if err != nil || !decision.Allowed || decision.Source != authz.SourceSystem {
			t.Fatalf("background %s decision=%#v err=%v", tc.capability, decision, err)
		}
	}

	denied, err := enforced.Check(ctx, authz.CheckRequest{
		SubjectRef: "agent:background",
		Transport:  authz.TransportSystem,
		Permission: "background.auto_assign",
		Resource:   authz.BackgroundResource("auto_assign"),
	})
	if !errors.Is(err, authz.ErrDenied) || denied.Allowed {
		t.Fatalf("agent:background must not inherit system background authority: %#v err=%v", denied, err)
	}
	denied, err = enforced.Check(ctx, authz.CheckRequest{
		SubjectRef: authz.BackgroundSubject,
		Transport:  authz.TransportSystem,
		Permission: "background.auto_assign",
		Resource:   authz.BackgroundResource("lease_check"),
	})
	if !errors.Is(err, authz.ErrDenied) || denied.Allowed {
		t.Fatalf("background capability must be permission-scoped: %#v err=%v", denied, err)
	}
	if err := requireBackgroundAuthorization(ctx, enforced, "unknown"); !errors.Is(err, authz.ErrPermissionUndefined) {
		t.Fatalf("unknown background capability err=%v want ErrPermissionUndefined", err)
	}

	autoAssign := NewAutoAssignReconciler(h.svc, h.clk, time.Minute, nil, enforced)
	if n, err := autoAssign.Tick(ctx); err != nil || n != 0 {
		t.Fatalf("auto-assign Tick n=%d err=%v", n, err)
	}
	lease := NewLeaseChecker(h.svc, h.clk, time.Minute, nil, enforced)
	if n, err := lease.Tick(ctx); err != nil || n != 0 {
		t.Fatalf("lease Tick n=%d err=%v", n, err)
	}
	overdue := NewOverdueBlockedReminder(h.svc, h.clk, time.Hour, time.Minute, nil, enforced)
	if n, err := overdue.Tick(ctx); err != nil || n != 0 {
		t.Fatalf("overdue Tick n=%d err=%v", n, err)
	}
	planLoop := NewPlanReconcileLoop(h.svc, time.Minute, nil, enforced)
	if err := planLoop.Tick(ctx); err != nil {
		t.Fatalf("plan reconcile Tick err=%v", err)
	}
	closer := NewResolvedIssueCloser(h.svc, time.Hour, time.Minute, nil, enforced)
	if n, err := closer.Tick(ctx); err != nil || n != 0 {
		t.Fatalf("resolved issue closer Tick n=%d err=%v", n, err)
	}
}
