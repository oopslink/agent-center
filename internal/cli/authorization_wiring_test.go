package cli

import (
	"testing"

	"github.com/oopslink/agent-center/internal/authorization"
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

func TestAuthorizationProductionDepsCanCutOverToEnforce(t *testing.T) {
	t.Setenv("AGENT_CENTER_AUTHZ_MODE", "enforce")
	app := newTestApp(t)
	if got := app.Authorization.EnforcementMode(); got != authorization.EnforcementEnforce {
		t.Fatalf("production authorization mode = %q, want enforce", got)
	}
	if adminDepsFromApp(app).Authorizer.EnforcementMode() != authorization.EnforcementEnforce {
		t.Fatal("admin production deps must receive the enforce-mode authorizer")
	}
}
