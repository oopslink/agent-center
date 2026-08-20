package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/persistence"
)

var newAppReadinessClock = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

func TestNewAppEnforceModeAcceptsValidPersistedReadiness(t *testing.T) {
	t.Setenv("AGENT_CENTER_AUTHZ_MODE", string(authorization.EnforcementEnforce))
	db := openMigratedNewAppReadinessDB(t)
	seedValidNewAppReadiness(t, db)

	app, err := NewApp(config.DefaultConfig(), db, clock.NewFakeClock(newAppReadinessClock))
	if err != nil {
		t.Fatalf("NewApp enforce with durable readiness: %v", err)
	}
	if app.Authorization == nil || app.Authorization.EnforcementMode() != authorization.EnforcementEnforce {
		t.Fatalf("authorization not wired in enforce mode: %#v", app.Authorization)
	}
}

func TestNewAppEnforceModeRejectsMissingReadinessState(t *testing.T) {
	t.Setenv("AGENT_CENTER_AUTHZ_MODE", string(authorization.EnforcementEnforce))
	db := openMigratedNewAppReadinessDB(t)

	expectNewAppReadinessReject(t, db, "shadow readiness evidence missing")
}

func TestNewAppEnforceModeRejectsInvalidPersistedReadiness(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(t *testing.T, db *sql.DB)
		wantErr string
	}{
		{
			name: "stale evidence",
			mutate: func(t *testing.T, db *sql.DB) {
				seedValidNewAppReadiness(t, db)
				mustExec(t, db, `UPDATE authorization_shadow_readiness SET window_started_at = ?, window_ended_at = ? WHERE id = 'current'`,
					newAppReadinessClock.Add(-25*time.Hour-2*time.Minute).Format(time.RFC3339Nano),
					newAppReadinessClock.Add(-25*time.Hour).Format(time.RFC3339Nano))
			},
			wantErr: "shadow readiness evidence is stale",
		},
		{
			name: "stable window too short",
			mutate: func(t *testing.T, db *sql.DB) {
				seedValidNewAppReadiness(t, db)
				mustExec(t, db, `UPDATE authorization_shadow_readiness SET window_started_at = ?, window_ended_at = ? WHERE id = 'current'`,
					newAppReadinessClock.Add(-30*time.Second).Format(time.RFC3339Nano),
					newAppReadinessClock.Format(time.RFC3339Nano))
			},
			wantErr: "shadow readiness window is too short",
		},
		{
			name: "checks below minimum",
			mutate: func(t *testing.T, db *sql.DB) {
				seedValidNewAppReadiness(t, db)
				mustExec(t, db, `UPDATE authorization_shadow_readiness SET checks = 5 WHERE id = 'current'`)
			},
			wantErr: "shadow readiness has too few checks",
		},
		{
			name: "missing transport coverage",
			mutate: func(t *testing.T, db *sql.DB) {
				seedValidNewAppReadiness(t, db)
				mustExec(t, db, `UPDATE authorization_shadow_readiness SET transports_json = '["web","mcp"]' WHERE id = 'current'`)
			},
			wantErr: "shadow readiness missing background coverage",
		},
		{
			name: "missing resource permission pair coverage",
			mutate: func(t *testing.T, db *sql.DB) {
				seedNewAppReadiness(t, db, []newAppReadinessAuditRow{
					{Transport: "web", ResourceKind: "org", Permission: "org.read"},
					{Transport: "mcp", ResourceKind: "project", Permission: "project.read"},
					{Transport: "background", ResourceKind: "org", Permission: "org.read"},
					{Transport: "web", ResourceKind: "project", Permission: "project.read"},
					{Transport: "mcp", ResourceKind: "org", Permission: "org.read"},
					{Transport: "background", ResourceKind: "project", Permission: "project.read"},
				}, nil)
			},
			wantErr: "shadow readiness missing project:project.write coverage",
		},
		{
			name: "audit evidence incomplete",
			mutate: func(t *testing.T, db *sql.DB) {
				seedValidNewAppReadiness(t, db)
				mustExec(t, db, `UPDATE authorization_shadow_readiness SET checks = 7 WHERE id = 'current'`)
			},
			wantErr: "shadow readiness audit evidence is incomplete",
		},
		{
			name: "audit mismatch",
			mutate: func(t *testing.T, db *sql.DB) {
				seedValidNewAppReadiness(t, db)
				mustExec(t, db, `UPDATE authorization_audit_events SET payload_json = ? WHERE id = 'newapp-readiness-0'`,
					`{"transport":"web","permission":"org.read","resource_kind":"org","mismatch":true}`)
			},
			wantErr: "shadow readiness audit evidence has mismatches",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AGENT_CENTER_AUTHZ_MODE", string(authorization.EnforcementEnforce))
			db := openMigratedNewAppReadinessDB(t)
			tt.mutate(t, db)
			expectNewAppReadinessReject(t, db, tt.wantErr)
		})
	}
}

func TestNewAppInvalidAuthorizationModeReturnsError(t *testing.T) {
	t.Setenv("AGENT_CENTER_AUTHZ_MODE", "dual_allow")
	db := openMigratedNewAppReadinessDB(t)

	_, err := NewApp(config.DefaultConfig(), db, nil)
	if err == nil {
		t.Fatal("NewApp succeeded with invalid authorization mode")
	}
	if !errors.Is(err, authorization.ErrInvalid) || !strings.Contains(err.Error(), "authorization mode") {
		t.Fatalf("NewApp err=%v, want wrapped invalid mode error", err)
	}
}

func TestNewAppLegacyModeRollbackIsExplicitAndAudited(t *testing.T) {
	t.Setenv("AGENT_CENTER_AUTHZ_MODE", string(authorization.EnforcementLegacy))
	db := openMigratedNewAppReadinessDB(t)

	app, err := NewApp(config.DefaultConfig(), db, clock.NewFakeClock(newAppReadinessClock))
	if err != nil {
		t.Fatalf("NewApp legacy rollback: %v", err)
	}
	if app.Authorization == nil || app.Authorization.EnforcementMode() != authorization.EnforcementLegacy {
		t.Fatalf("authorization mode=%v", app.Authorization)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM authorization_audit_events
		WHERE event_type='authorization.enforcement_mode.selected'
		  AND actor_ref='system'
		  AND resource_kind='authorization'
		  AND resource_id='legacy'
		  AND payload_json LIKE '%explicit AGENT_CENTER_AUTHZ_MODE=legacy rollback%'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("legacy rollback audit count=%d want 1", count)
	}
}

func openMigratedNewAppReadinessDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := persistence.Open(t.TempDir() + "/readiness.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

type newAppReadinessAuditRow struct {
	Transport    string
	ResourceKind string
	Permission   string
	Mismatch     bool
}

func seedValidNewAppReadiness(t *testing.T, db *sql.DB) {
	t.Helper()
	seedNewAppReadiness(t, db, []newAppReadinessAuditRow{
		{Transport: "web", ResourceKind: "org", Permission: "org.read"},
		{Transport: "mcp", ResourceKind: "project", Permission: "project.read"},
		{Transport: "background", ResourceKind: "project", Permission: "project.write"},
		{Transport: "web", ResourceKind: "project", Permission: "project.read"},
		{Transport: "mcp", ResourceKind: "org", Permission: "org.read"},
		{Transport: "background", ResourceKind: "org", Permission: "org.read"},
	}, nil)
}

func seedNewAppReadiness(t *testing.T, db *sql.DB, rows []newAppReadinessAuditRow, mutate func(*newAppReadinessAuditRow)) {
	t.Helper()
	started := newAppReadinessClock.Add(-2 * time.Minute)
	ended := newAppReadinessClock.Add(-1 * time.Minute)
	mustExec(t, db, `INSERT INTO authorization_shadow_readiness
		(id, mode, window_started_at, window_ended_at, transports_json, checks, mismatches, legacy_only, equivalent_only, ready, reason, updated_at)
		VALUES ('current', 'shadow', ?, ?, '["web","mcp","background"]', ?, 0, 0, 0, 1, 'test durable readiness', ?)`,
		started.Format(time.RFC3339Nano), ended.Format(time.RFC3339Nano), len(rows), ended.Format(time.RFC3339Nano))
	for i, row := range rows {
		if mutate != nil {
			mutate(&row)
		}
		created := started.Add(time.Duration(i+1) * 10 * time.Second)
		mismatch := "false"
		if row.Mismatch {
			mismatch = "true"
		}
		payload := fmt.Sprintf(`{"transport":%q,"permission":%q,"resource_kind":%q,"mismatch":%s}`,
			row.Transport, row.Permission, row.ResourceKind, mismatch)
		mustExec(t, db, `INSERT INTO authorization_audit_events
			(id, event_type, actor_ref, subject_ref, permission_key, resource_kind, resource_id, payload_json, created_at)
			VALUES (?, 'authorization.shadow.compare', 'user:test', 'user:test', ?, ?, 'resource-test', ?, ?)`,
			fmt.Sprintf("newapp-readiness-%d", i), row.Permission, row.ResourceKind, payload, created.Format(time.RFC3339Nano))
	}
}

func expectNewAppReadinessReject(t *testing.T, db *sql.DB, want string) {
	t.Helper()
	_, err := NewApp(config.DefaultConfig(), db, clock.NewFakeClock(newAppReadinessClock))
	if err == nil {
		t.Fatal("NewApp succeeded in enforce mode without valid durable readiness")
	}
	if !errors.Is(err, authorization.ErrDenied) || !strings.Contains(err.Error(), want) {
		t.Fatalf("NewApp err=%v, want ErrDenied containing %q", err, want)
	}
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}
