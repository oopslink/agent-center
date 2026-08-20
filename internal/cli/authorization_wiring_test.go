package cli

import "testing"

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
