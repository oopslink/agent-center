package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/config"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

func TestT1438NewAppWiresMigratedBackgroundAuthorization(t *testing.T) {
	app := newEnforcedBackgroundAuthzApp(t)
	ctx := context.Background()

	for _, operation := range []string{
		"auto_assign_reconciler",
		"lease_checker",
		"overdue_block_reminder",
		"plan_reconcile",
		"resolved_issue_closer",
	} {
		if _, err := app.Authorization.Check(ctx, backgroundAuthzRequest(operation)); err != nil {
			t.Fatalf("NewApp production authz for %s: %v", operation, err)
		}
	}

	if n, err := pmservice.NewAutoAssignReconciler(app.PMService, app.Clock, time.Minute, nil).Tick(ctx); err != nil || n != 0 {
		t.Fatalf("auto_assign_reconciler Tick n=%d err=%v, want reachable clean sweep", n, err)
	}
	if n, err := pmservice.NewLeaseChecker(app.PMService, app.Clock, time.Minute, nil).Tick(ctx); err != nil || n != 0 {
		t.Fatalf("lease_checker Tick n=%d err=%v, want reachable clean sweep", n, err)
	}
	if n, err := pmservice.NewOverdueBlockedReminder(app.PMService, app.Clock, time.Hour, time.Minute, nil).Tick(ctx); err != nil || n != 0 {
		t.Fatalf("overdue_block_reminder Tick n=%d err=%v, want reachable clean sweep", n, err)
	}
	if n, err := pmservice.NewResolvedIssueCloser(app.PMService, time.Hour, time.Minute, nil).Tick(ctx); err != nil || n != 0 {
		t.Fatalf("resolved_issue_closer Tick n=%d err=%v, want reachable clean sweep", n, err)
	}

	planLogCh := make(chan string, 8)
	planCtx, cancel := context.WithCancel(ctx)
	planDone := make(chan struct{})
	go func() {
		defer close(planDone)
		pmservice.NewPlanReconcileLoop(app.PMService, time.Hour, func(msg string) {
			planLogCh <- msg
		}).Run(planCtx)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-planDone
	close(planLogCh)
	var planLogs []string
	for msg := range planLogCh {
		planLogs = append(planLogs, msg)
	}
	for _, msg := range planLogs {
		if strings.Contains(msg, "authorization") || strings.Contains(msg, "permission denied") {
			t.Fatalf("plan_reconcile logged authorization failure on clean NewApp wiring: %q", msg)
		}
	}

	if _, err := app.Authorization.Check(ctx, backgroundAuthzRequest("unregistered_operation")); !errors.Is(err, authz.ErrDenied) {
		t.Fatalf("unknown background operation err=%v, want ErrDenied", err)
	}

	if _, err := app.Authorization.RevokeBatch(ctx, authz.BatchRequest{
		IdempotencyKey: "t1438-newapp-background-lease-revoke",
		ActorRef:       "system",
		OrgID:          "system",
		Operations: []authz.BatchOperation{{
			ID:     "revoke-lease-checker",
			Revoke: authz.RevokeInput{AssignmentID: "asgn-background-worker-lease-checker", Reason: "t1438 NewApp integration revoke"},
		}},
	}); err != nil {
		t.Fatalf("production revoke of migrated lease_checker assignment: %v", err)
	}

	n, err := pmservice.NewLeaseChecker(app.PMService, app.Clock, time.Minute, nil).Tick(ctx)
	if !errors.Is(err, authz.ErrDenied) || n != 0 {
		t.Fatalf("lease_checker after production revoke n=%d err=%v, want immediate ErrDenied", n, err)
	}
}

func newEnforcedBackgroundAuthzApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("AGENT_CENTER_AUTHZ_MODE", string(authz.EnforcementEnforce))
	db := openMigratedNewAppReadinessDB(t)
	seedValidNewAppReadiness(t, db)

	cfg := config.DefaultConfig()
	mkPath := t.TempDir() + "/master.key"
	if err := writeTestMasterKey(mkPath); err != nil {
		t.Fatal(err)
	}
	cfg.SecretManagement.MasterKeyFile = mkPath
	cfg.SecretManagement.SkipPermsCheck = true

	app, err := NewApp(cfg, db, clock.NewFakeClock(newAppReadinessClock))
	if err != nil {
		t.Fatalf("NewApp enforced background authz: %v", err)
	}
	if app.Authorization == nil || app.Authorization.EnforcementMode() != authz.EnforcementEnforce {
		t.Fatalf("NewApp authorization mode=%v, want enforce", app.Authorization)
	}
	if app.PMService == nil {
		t.Fatal("NewApp must wire PMService")
	}
	return app
}

func backgroundAuthzRequest(operation string) authz.CheckRequest {
	return authz.CheckRequest{
		SubjectRef: authz.AgentSubject("background"),
		Transport:  authz.TransportBackground,
		Permission: "worker.capability.report",
		Resource: authz.ResourceScope{
			Kind:  "worker",
			ID:    "background:" + operation,
			OrgID: "system",
		},
		RequestID: "background:" + operation,
	}
}
