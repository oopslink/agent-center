package authorization

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolverFailClosedWhenUnwired(t *testing.T) {
	decision, err := Authorize(context.Background(), nil, CheckRequest{
		SubjectRef: "user:a",
		Transport:  TransportWeb,
		Permission: "org.read",
		Resource:   ResourceScope{Kind: "org", ID: "org-1"},
	})
	if !errors.Is(err, ErrDenied) || decision.Allowed || decision.Reason != "authorization_not_wired" {
		t.Fatalf("decision=%#v err=%v", decision, err)
	}
}

func TestReadinessGateRejectsCoverageMismatchAndAudits(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	now := svc.clock.Now()
	err := svc.RecordReadiness(ctx, ReadinessSnapshot{
		Mode:        EnforcementEnforce,
		Transports:  []Transport{TransportWeb},
		Permissions: RequiredReadinessPermissions(),
		Resources:   RequiredReadinessResources(),
		Checks:      100,
		StartedAt:   now.Add(-10 * time.Minute),
		ObservedAt:  now,
		ExpiresAt:   now.Add(time.Hour),
	})
	if !errors.Is(err, ErrReadinessRejected) {
		t.Fatalf("RecordReadiness err=%v, want readiness rejection", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorization_audit_events WHERE event_type = 'authorization.readiness.rejected'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("readiness rejection audit count=%d, want 1", count)
	}
}

func TestEnforceReadinessAndRollbackAudit(t *testing.T) {
	ctx := context.Background()
	db, base := newAuthzTestService(t)
	seedAuthzBase(t, db)
	svc := New(Deps{DB: db, Store: base.store, IDGen: base.gen, Clock: base.clock, Mode: EnforcementEnforce})
	if err := svc.RequireEnforceReadiness(ctx); !errors.Is(err, ErrReadinessRejected) {
		t.Fatalf("RequireEnforceReadiness without gate err=%v", err)
	}
	now := svc.clock.Now()
	if err := svc.RecordReadiness(ctx, ReadinessSnapshot{
		Mode:        EnforcementEnforce,
		Transports:  RequiredReadinessTransports(),
		Permissions: RequiredReadinessPermissions(),
		Resources:   RequiredReadinessResources(),
		Checks:      50,
		StartedAt:   now.Add(-10 * time.Minute),
		ObservedAt:  now,
		ExpiresAt:   now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("RecordReadiness: %v", err)
	}
	if err := svc.RequireEnforceReadiness(ctx); err != nil {
		t.Fatalf("RequireEnforceReadiness accepted gate: %v", err)
	}
	if err := svc.RollbackEnforcement(ctx, "test rollback"); err != nil {
		t.Fatalf("RollbackEnforcement: %v", err)
	}
	if got := svc.EnforcementMode(); got != EnforcementLegacy {
		t.Fatalf("mode=%s, want legacy", got)
	}
	var rollback int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorization_audit_events WHERE event_type = 'authorization.enforce.rollback'`).Scan(&rollback); err != nil {
		t.Fatal(err)
	}
	if rollback != 1 {
		t.Fatalf("rollback audit count=%d, want 1", rollback)
	}
}

func TestProductionCallersUseResolverEntryPoint(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{
		"internal/webconsole/api",
		"internal/admin/api",
		"internal/projectmanager/service",
	} {
		err := filepath.WalkDir(filepath.Join(root, rel), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			body := string(b)
			if strings.Contains(body, "Authorizer.Check(") || strings.Contains(body, ".Check(r.Context(), authz.CheckRequest") || strings.Contains(body, "svc.Check(r.Context()") {
				t.Fatalf("%s bypasses unified resolver", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatal("go.mod not found")
		}
		dir = next
	}
}
