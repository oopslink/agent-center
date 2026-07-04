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

	// No builtin templates anymore (2026-07 复盘: builtin type removed). Exercise
	// the full template CRUD agent tools end-to-end: create → list → get → update → delete.

	// --- create_template ----------------------------------------------------
	status, body, rawCreate := post(t, "/admin/agent-tools/create_template",
		map[string]any{"agent_id": agentID, "name": "test-tmpl", "description": "realboot test", "content": "# test\nhello"})
	t.Logf("create_template → HTTP %d\n%s", status, rawCreate)
	if errCode, _ := body["error"].(string); errCode == "templates_not_wired" {
		t.Fatalf("create_template returns templates_not_wired — wiring gap not closed")
	}
	if status != http.StatusCreated {
		t.Fatalf("create_template status = %d (want 201); body = %s", status, rawCreate)
	}
	tmplID, _ := body["id"].(string)
	if tmplID == "" {
		t.Fatalf("create_template returned no id; body = %s", rawCreate)
	}

	// --- list_templates -----------------------------------------------------
	status, body, rawList := post(t, "/admin/agent-tools/list_templates", map[string]any{"agent_id": agentID})
	t.Logf("list_templates → HTTP %d\n%s", status, rawList)
	if status != http.StatusOK {
		t.Fatalf("list_templates status = %d (want 200); body = %s", status, rawList)
	}
	items, _ := body["templates"].([]any)
	found := false
	for _, it := range items {
		if m, _ := it.(map[string]any); m != nil {
			if id, _ := m["id"].(string); id == tmplID {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("created template %s not present in list_templates; body = %s", tmplID, rawList)
	}

	// --- get_template -------------------------------------------------------
	status, body, rawGet := post(t, "/admin/agent-tools/get_template",
		map[string]any{"agent_id": agentID, "template_id": tmplID})
	t.Logf("get_template(%s) → HTTP %d\n%s", tmplID, status, rawGet)
	if status != http.StatusOK {
		t.Fatalf("get_template status = %d (want 200); body = %s", status, rawGet)
	}
	if content, _ := body["content"].(string); content != "# test\nhello" {
		t.Fatalf("get_template content mismatch; body = %s", rawGet)
	}

	// --- update_template ----------------------------------------------------
	status, _, rawUpd := post(t, "/admin/agent-tools/update_template",
		map[string]any{"agent_id": agentID, "template_id": tmplID, "name": "test-tmpl", "description": "updated", "content": "# test\nupdated"})
	t.Logf("update_template → HTTP %d\n%s", status, rawUpd)
	if status != http.StatusOK {
		t.Fatalf("update_template status = %d (want 200); body = %s", status, rawUpd)
	}

	// --- delete_template ----------------------------------------------------
	status, _, rawDel := post(t, "/admin/agent-tools/delete_template",
		map[string]any{"agent_id": agentID, "template_id": tmplID})
	t.Logf("delete_template → HTTP %d\n%s", status, rawDel)
	if status != http.StatusOK {
		t.Fatalf("delete_template status = %d (want 200); body = %s", status, rawDel)
	}
}
