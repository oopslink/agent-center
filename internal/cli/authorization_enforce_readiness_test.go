package cli

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/persistence"
)

func TestNewAppEnforceModeRequiresReadyAuthorizationRegistry(t *testing.T) {
	t.Setenv("AGENT_CENTER_AUTHZ_MODE", string(authorization.EnforcementEnforce))
	db := openMigratedNewAppReadinessDB(t)

	app, err := NewApp(config.DefaultConfig(), db, clock.NewFakeClock(time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("NewApp enforce on migrated DB: %v", err)
	}
	if app.Authorization == nil || app.Authorization.EnforcementMode() != authorization.EnforcementEnforce {
		t.Fatalf("authorization not wired in enforce mode: %#v", app.Authorization)
	}
}

func TestNewAppEnforceModeRejectsMissingReadinessState(t *testing.T) {
	t.Setenv("AGENT_CENTER_AUTHZ_MODE", string(authorization.EnforcementEnforce))
	db := openMigratedNewAppReadinessDB(t)
	if _, err := db.Exec(`DELETE FROM permission_definitions WHERE key = 'project.read'`); err != nil {
		t.Fatal(err)
	}

	app, err := NewApp(config.DefaultConfig(), db, clock.NewFakeClock(time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)))
	if err == nil {
		t.Fatalf("NewApp returned app=%#v, want readiness error", app)
	}
	if !errors.Is(err, authorization.ErrEnforceNotReady) || !strings.Contains(err.Error(), "project.read") {
		t.Fatalf("NewApp err=%v, want ErrEnforceNotReady mentioning project.read", err)
	}
}

func TestNewAppEnforceModeRejectsIncompleteRoleCoverage(t *testing.T) {
	t.Setenv("AGENT_CENTER_AUTHZ_MODE", string(authorization.EnforcementEnforce))
	db := openMigratedNewAppReadinessDB(t)
	if _, err := db.Exec(`DELETE FROM authorization_role_permissions WHERE role_id = 'sys-project-member' AND permission_key = 'project.read'`); err != nil {
		t.Fatal(err)
	}

	_, err := NewApp(config.DefaultConfig(), db, clock.NewFakeClock(time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)))
	if err == nil {
		t.Fatal("NewApp succeeded with incomplete role permission coverage")
	}
	if !errors.Is(err, authorization.ErrEnforceNotReady) || !strings.Contains(err.Error(), "sys-project-member") {
		t.Fatalf("NewApp err=%v, want ErrEnforceNotReady mentioning sys-project-member", err)
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

	app, err := NewApp(config.DefaultConfig(), db, clock.NewFakeClock(time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)))
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
