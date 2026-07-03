package cli

import "testing"

// TestAdminDeps_WiresOrchService is the T768 wiring guard: the admin-api
// HandlerDeps builder (adminDepsFromApp — the ONLY admin-api builder, shared by
// the live server and the admin_client_testhelper) MUST populate OrchService, or
// all 18 agent MCP orchestration tools (create_graph / add_node / get_ready_nodes
// / resolve_condition / …) return orchestration_not_wired (501). The assertion
// goes through adminDepsFromApp on purpose: a hand-wired HandlerDeps would stay
// green while prod 501'd (the SettingsStore/Analytics "wire in BOTH builders" trap).
func TestAdminDeps_WiresOrchService(t *testing.T) {
	app, cleanup := setupAdminServerForTests(t)
	defer cleanup()

	if app.OrchService == nil {
		t.Fatal("App.OrchService is nil — NewApp did not build the orchestration engine")
	}
	if adminDepsFromApp(app).OrchService == nil {
		t.Fatal("adminDepsFromApp did not wire OrchService — the 18 orchestration MCP tools would 501")
	}
}
