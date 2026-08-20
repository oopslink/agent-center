package cli

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/persistence"
)

func TestAuthorizationProductionDepsWired(t *testing.T) {
	app := newTestApp(t)
	if app.Authorization == nil {
		t.Fatal("NewApp must wire Authorization service")
	}
	adminDeps := adminDepsFromApp(app)
	if adminDeps.Authorizer != app.Authorization {
		t.Fatal("admin production deps must use App.Authorization")
	}
	handler := buildWebConsoleHandler(app, nil)
	if handler == nil {
		t.Fatal("webconsole production handler should build with Authorization wired")
	}
}

func TestAuthorizationProductionDepsRejectEnforceWithoutPersistentReadiness(t *testing.T) {
	t.Setenv("AGENT_CENTER_AUTHZ_MODE", "enforce")
	app := newTestApp(t)
	if got := app.Authorization.EnforcementMode(); got != authorization.EnforcementShadow {
		t.Fatalf("production authorization mode = %q, want shadow without persisted readiness", got)
	}
	if adminDepsFromApp(app).Authorizer.EnforcementMode() != authorization.EnforcementShadow {
		t.Fatal("admin production deps must receive the guarded shadow-mode authorizer")
	}
}

func TestAuthorizationProductionDepsCanCutOverToEnforceWithPersistentReadiness(t *testing.T) {
	t.Setenv("AGENT_CENTER_AUTHZ_MODE", "enforce")
	dir := t.TempDir()
	db, err := persistence.Open(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	if _, err := db.Exec(`INSERT INTO authorization_shadow_readiness
		(id, mode, window_started_at, window_ended_at, transports_json, checks, mismatches, legacy_only, equivalent_only, ready, reason, updated_at)
		VALUES ('current', 'shadow', ?, ?, '["background","mcp","web"]', 9, 0, 0, 0, 1, 'test evidence', ?)`,
		now.Add(-time.Hour).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	mkPath := dir + "/master.key"
	if err := writeTestMasterKey(mkPath); err != nil {
		t.Fatal(err)
	}
	cfg.SecretManagement.MasterKeyFile = mkPath
	cfg.SecretManagement.SkipPermsCheck = true
	app, err := NewApp(cfg, db, clock.NewFakeClock(now))
	if err != nil {
		t.Fatal(err)
	}
	if got := app.Authorization.EnforcementMode(); got != authorization.EnforcementEnforce {
		t.Fatalf("production authorization mode = %q, want enforce", got)
	}
	if adminDepsFromApp(app).Authorizer.EnforcementMode() != authorization.EnforcementEnforce {
		t.Fatal("admin production deps must receive the enforce-mode authorizer")
	}
}
