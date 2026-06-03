package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/admintoken"
	"github.com/oopslink/agent-center/internal/agent"
	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
	wfsql "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

// =============================================================================
// v2.7 D2-b1 — per-agent authorization base for the Agent MCP tool surface.
//
// These tests exercise the REAL admin server + AuthMiddleware: the worker
// identity comes from the TOKEN OWNER (worker:<id>), never the body, and the
// HARD GUARDRAIL (agent.WorkerID() == worker-from-token) is proven via the
// cross-worker 403. Access flows through the Agent BC AppService, not the DB.
// =============================================================================

const (
	atTestOrg = "org-1"
	atWorker1 = "W1"
	atWorker2 = "W2"
	atAgent1  = "AG1"
	atAgent2  = "AG2"
)

var atNow = time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)

type agentToolsFixture struct {
	deps      HandlerDeps
	verifier  *fakeVerifier
	workItems *agentsql.WorkItemRepo
	agents    *agentsql.AgentRepo
	clk       *clock.FakeClock
}

// newAgentToolsFixture seeds two workers (W1, W2) and two agents (AG1→W1,
// AG2→W2) via the real AgentService, then builds HandlerDeps wired with the
// AgentService. The fakeVerifier maps a plaintext bearer to a worker-owner
// token so the real AuthMiddleware populates AuthContext.Owner = worker:<id>.
func newAgentToolsFixture(t *testing.T) *agentToolsFixture {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(atNow)
	gen := idgen.NewGenerator(clk)

	workers := wfsql.NewWorkerRepo(db)
	agents := agentsql.NewAgentRepo(db)
	workItems := agentsql.NewWorkItemRepo(db)
	svc := agentsvc.New(agentsvc.Deps{
		DB:        db,
		Agents:    agents,
		WorkItems: workItems,
		Activity:  agentsql.NewActivityEventRepo(db),
		Workers:   workers,
		Outbox:    outboxsql.NewOutboxRepo(db),
		IDGen:     gen,
		Clock:     clk,
	})

	// Seed the two workforce.Workers (CreateAgent validates the worker exists
	// in the agent's org).
	seedWorker := func(id string) {
		w, werr := workforce.NewWorker(workforce.NewWorkerInput{
			ID:             workforce.WorkerID(id),
			Capabilities:   []string{"claude-code"},
			OrganizationID: atTestOrg,
			EnrolledAt:     atNow,
		})
		if werr != nil {
			t.Fatal(werr)
		}
		if werr := workers.Save(ctx, w); werr != nil {
			t.Fatal(werr)
		}
	}
	seedWorker(atWorker1)
	seedWorker(atWorker2)

	// Seed the two agents bound to their respective workers, then rehydrate
	// them with the fixed ids (AG1/AG2) so the tests can address them. We use
	// the agent repo directly for the fixed-id rehydrate (CreateAgent mints a
	// ULID id we can't predict).
	seedAgent := func(id, workerID string) {
		a, aerr := agent.NewAgent(agent.NewAgentInput{
			ID:             agent.AgentID(id),
			OrganizationID: atTestOrg,
			Profile:        agent.Profile{Name: id},
			WorkerID:       workerID,
			CreatedBy:      "system",
			CreatedAt:      atNow,
		})
		if aerr != nil {
			t.Fatal(aerr)
		}
		if aerr := agents.Save(ctx, a); aerr != nil {
			t.Fatal(aerr)
		}
	}
	seedAgent(atAgent1, atWorker1)
	seedAgent(atAgent2, atWorker2)

	verifier := &fakeVerifier{tokens: map[string]*admintoken.AdminToken{}}
	deps := HandlerDeps{
		DB:         db,
		AgentSvc:   svc,
		WorkerRepo: workers,
	}
	return &agentToolsFixture{
		deps: deps, verifier: verifier, workItems: workItems, agents: agents, clk: clk,
	}
}

// addWorkerToken registers a bearer plaintext whose token owner is worker:<id>.
func (f *agentToolsFixture) addWorkerToken(t *testing.T, plaintext, workerID string) {
	t.Helper()
	f.addOwnerToken(t, plaintext, admintoken.Owner("worker:"+workerID))
}

// addOwnerToken registers a bearer plaintext with an arbitrary owner.
func (f *agentToolsFixture) addOwnerToken(t *testing.T, plaintext string, owner admintoken.Owner) {
	t.Helper()
	tok, err := admintoken.New(admintoken.NewAdminTokenInput{
		ID: admintoken.TokenID("T-" + plaintext), Owner: owner,
		Scopes:    []admintoken.Scope{"*"},
		ValueHash: admintoken.HashPlaintext(plaintext),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.verifier.tokens[plaintext] = tok
}

// seedWorkItem inserts a queued work item for the agent via the repo.
func (f *agentToolsFixture) seedWorkItem(t *testing.T, id, agentID, taskRef string) {
	t.Helper()
	wi, err := agent.NewWorkItem(agent.NewWorkItemInput{
		ID: id, AgentID: agent.AgentID(agentID), TaskRef: taskRef, CreatedAt: atNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.workItems.Save(context.Background(), wi); err != nil {
		t.Fatal(err)
	}
}

// server builds the real admin server wrapped with AuthMiddleware + WithDeps so
// the per-agent gate runs over the actual middleware chain.
func (f *agentToolsFixture) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := NewServerWithDeps("", ServerDeps{})
	h := AuthMiddleware(f.verifier)(WithDeps(f.deps)(srv.Handler()))
	httpsrv := httptest.NewServer(h)
	t.Cleanup(httpsrv.Close)
	return httpsrv
}

// postBearer POSTs body with an Authorization: Bearer <plaintext> header.
func postBearer(t *testing.T, base, path, bearer string, body any) (int, map[string]any) {
	t.Helper()
	buf, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// TestGetMyWork_OwnAgent_OK proves the happy path: a W1 bearer reading AG1's
// (its own) work items succeeds and returns them.
func TestGetMyWork_OwnAgent_OK(t *testing.T) {
	f := newAgentToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.seedWorkItem(t, "wi-1", atAgent1, "task:t-1")
	f.seedWorkItem(t, "wi-2", atAgent1, "task:t-2")
	// AG2 has an item that must NOT leak into AG1's read.
	f.seedWorkItem(t, "wi-3", atAgent2, "task:t-3")
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_my_work", "acat_w1",
		map[string]any{"agent_id": atAgent1})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	items, ok := body["work_items"].([]any)
	if !ok {
		t.Fatalf("body has no work_items array: %v", body)
	}
	if len(items) != 2 {
		t.Fatalf("got %d work items, want 2 (own-scoped to AG1): %v", len(items), items)
	}
	gotIDs := map[string]bool{}
	for _, raw := range items {
		m := raw.(map[string]any)
		gotIDs[m["id"].(string)] = true
		// Spot-check the serializer fields.
		if m["task_ref"] == "" || m["status"] != "queued" {
			t.Fatalf("unexpected work item shape: %v", m)
		}
	}
	if !gotIDs["wi-1"] || !gotIDs["wi-2"] {
		t.Fatalf("missing expected work items: %v", gotIDs)
	}
	if gotIDs["wi-3"] {
		t.Fatalf("AG2's work item leaked into AG1's read: %v", gotIDs)
	}
}

// TestGetMyWork_OwnAgent_EmptyOK proves an agent with no work items returns an
// empty list (not an error).
func TestGetMyWork_OwnAgent_EmptyOK(t *testing.T) {
	f := newAgentToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_my_work", "acat_w1",
		map[string]any{"agent_id": atAgent1})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	items, ok := body["work_items"].([]any)
	if !ok || len(items) != 0 {
		t.Fatalf("want empty work_items array, got %v", body["work_items"])
	}
}

// TestGetMyWork_CrossWorker_403 is the GUARDRAIL acceptance: a W1 bearer trying
// to operate AG2 (bound to W2) is rejected with 403 — a worker can NOT operate
// another worker's agent, even with a valid token.
func TestGetMyWork_CrossWorker_403(t *testing.T) {
	f := newAgentToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.seedWorkItem(t, "wi-3", atAgent2, "task:t-3")
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_my_work", "acat_w1",
		map[string]any{"agent_id": atAgent2})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (cross-worker guardrail); body = %v", status, body)
	}
	if body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("error code = %v, want agent_not_bound_to_worker", body["error"])
	}
}

// TestGetMyWork_NonWorkerToken_403 proves a non-worker owner is rejected: the
// gate requires a worker:<id> token (identity comes from the token, not body).
func TestGetMyWork_NonWorkerToken_403(t *testing.T) {
	f := newAgentToolsFixture(t)
	f.addOwnerToken(t, "acat_cli", admintoken.Owner("cli:admin"))
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_my_work", "acat_cli",
		map[string]any{"agent_id": atAgent1})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (non-worker token); body = %v", status, body)
	}
	if body["error"] != "not_a_worker_token" {
		t.Fatalf("error code = %v, want not_a_worker_token", body["error"])
	}
}

// TestGetMyWork_MissingAgentID_400 proves the missing-agent_id 400.
func TestGetMyWork_MissingAgentID_400(t *testing.T) {
	f := newAgentToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_my_work", "acat_w1",
		map[string]any{})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %v", status, body)
	}
	if body["error"] != "missing_agent_id" {
		t.Fatalf("error code = %v, want missing_agent_id", body["error"])
	}
}

// TestGetMyWork_UnknownAgent_404 proves an unknown agent id (bound to no one)
// returns 404.
func TestGetMyWork_UnknownAgent_404(t *testing.T) {
	f := newAgentToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_my_work", "acat_w1",
		map[string]any{"agent_id": "ghost"})
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %v", status, body)
	}
	if body["error"] != "not_found" {
		t.Fatalf("error code = %v, want not_found", body["error"])
	}
}

// TestGetMyWork_NoBearer_401 confirms the middleware rejects an unauthenticated
// call before the handler runs.
func TestGetMyWork_NoBearer_401(t *testing.T) {
	f := newAgentToolsFixture(t)
	srv := f.server(t)
	status, _ := postBearer(t, srv.URL, "/admin/agent-tools/get_my_work", "",
		map[string]any{"agent_id": atAgent1})
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", status)
	}
}

// TestGetMyWork_SvcNotWired_501 proves the gate fails to 501 when AgentSvc is
// nil (defensive: never run a tool against an unwired service).
func TestGetMyWork_SvcNotWired_501(t *testing.T) {
	f := newAgentToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.deps.AgentSvc = nil
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_my_work", "acat_w1",
		map[string]any{"agent_id": atAgent1})
	if status != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501; body = %v", status, body)
	}
}
