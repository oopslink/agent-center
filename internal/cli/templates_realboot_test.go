// Package cli — templates_realboot_test.go: T764 hotfix regression guard.
//
// Boots a REAL admin HTTP server through the exact production wiring
// (adminDepsFromApp — the ONLY admin-api HandlerDeps builder, shared by the
// live server and the admin_client_testhelper) on top of a REAL migrated DB
// built by NewApp (which now seeds the builtin templates at boot). It then
// drives the agent MCP tools over real HTTP with a real worker bearer +
// bound agent, asserting list_templates / get_template return the builtin
// "cycle" template instead of the old templates_not_wired 501.
//
// This is the regression guard for the exact gap the hotfix closed: a test
// that hand-wired TemplateRepo would stay green while prod 501'd, so the
// assertion goes through adminDepsFromApp on purpose.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/admin/api"
	"github.com/oopslink/agent-center/internal/admintoken"
	admintokensvc "github.com/oopslink/agent-center/internal/admintoken/service"
	agentpkg "github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
)

func TestTemplateTools_RealBoot_ReturnBuiltin(t *testing.T) {
	ctx := context.Background()
	const (
		org      = "org-realboot"
		workerID = "W-realboot"
		agentID  = "AG-realboot"
	)
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)

	// Real migrated DB.
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// NewApp is the single boot choke point — this is where the hotfix seeds
	// the builtin templates. If the seed didn't run, ListByOrg below is empty.
	app, err := NewApp(config.DefaultConfig(), db, clock.NewFakeClock(now))
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}

	// Seed a worker + an agent bound to it (same org), so requireAgentOnWorker
	// passes for a worker:<id> bearer operating its own agent.
	w, err := workforce.NewWorker(workforce.NewWorkerInput{
		ID: workforce.WorkerID(workerID), Capabilities: []string{"claude-code"},
		OrganizationID: org, EnrolledAt: now,
	})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	if err := app.WorkerRepo.Save(ctx, w); err != nil {
		t.Fatalf("save worker: %v", err)
	}
	ag, err := agentpkg.NewAgent(agentpkg.NewAgentInput{
		ID: agentpkg.AgentID(agentID), OrganizationID: org,
		Profile: agentpkg.Profile{Name: agentID}, WorkerID: workerID,
		CreatedBy: "system", CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if err := app.AgentRepo.Save(ctx, ag); err != nil {
		t.Fatalf("save agent: %v", err)
	}

	// Mint a real worker-scoped bearer, then stop the token bookkeeping pump
	// (mirrors admin_client_testhelper) before serving to avoid SQLITE_BUSY.
	res, err := app.AdminTokenSvc.Create(ctx, admintokensvc.CreateCommand{
		Owner: admintoken.Owner("worker:" + workerID), Scopes: []admintoken.Scope{"*"}, CreatedBy: "test",
	})
	if err != nil {
		t.Fatalf("mint worker token: %v", err)
	}
	app.AdminTokenSvc.Close()

	// Boot the REAL admin routes with the REAL production wiring + auth.
	srv := api.NewServerWithDeps("", api.ServerDeps{})
	handler := api.AuthMiddleware(app.AdminTokenSvc)(api.WithDeps(adminDepsFromApp(app))(srv.Handler()))
	ts := httptest.NewServer(handler)
	defer ts.Close()

	post := func(t *testing.T, path string, payload map[string]any) (int, map[string]any, string) {
		t.Helper()
		raw, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, ts.URL+path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+res.Plaintext)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		return resp.StatusCode, m, string(b)
	}

	// --- list_templates -----------------------------------------------------
	status, body, rawList := post(t, "/admin/agent-tools/list_templates", map[string]any{"agent_id": agentID})
	t.Logf("list_templates → HTTP %d\n%s", status, rawList)
	if status != http.StatusOK {
		t.Fatalf("list_templates status = %d (want 200); body = %s", status, rawList)
	}
	if errCode, _ := body["error"].(string); errCode == "templates_not_wired" {
		t.Fatalf("list_templates still returns templates_not_wired — wiring gap not closed")
	}
	items, _ := body["templates"].([]any)
	if len(items) == 0 {
		t.Fatalf("list_templates returned no templates — builtin seed did not run; body = %s", rawList)
	}
	var cycleID string
	for _, it := range items {
		m, _ := it.(map[string]any)
		if m["name"] == "cycle" {
			if b, _ := m["builtin"].(bool); b {
				cycleID, _ = m["id"].(string)
			}
		}
	}
	if cycleID == "" {
		t.Fatalf("builtin 'cycle' template not present in list_templates; body = %s", rawList)
	}

	// --- get_template -------------------------------------------------------
	status, body, rawGet := post(t, "/admin/agent-tools/get_template",
		map[string]any{"agent_id": agentID, "template_id": cycleID})
	t.Logf("get_template(%s) → HTTP %d\n%s", cycleID, status, rawGet)
	if status != http.StatusOK {
		t.Fatalf("get_template status = %d (want 200); body = %s", status, rawGet)
	}
	if content, _ := body["content"].(string); content == "" {
		t.Fatalf("get_template returned empty content; body = %s", rawGet)
	}
}
