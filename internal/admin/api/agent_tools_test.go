package api

import (
	"bytes"
	"context"
	"database/sql"
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
// Shared agent-tools test fixture + helpers (consts, postBearer, the worker /
// agent seeding fixture) reused across the agent_tools_*_test.go siblings.
//
// v2.14.0 F7 (issue I14): the AgentWorkItem world was retired — the work-item
// seeding + the get_my_work endpoint tests this file once carried were deleted
// (get_my_work was replaced by list_my_tasks; the work-item repo no longer
// exists). What remains is the per-agent authorization fixture (the cross-worker
// guardrail base) the surviving #239 tests build on.
// =============================================================================

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
	deps     HandlerDeps
	verifier *fakeVerifier
	agents   *agentsql.AgentRepo
	clk      *clock.FakeClock
	db       *sql.DB // exposed so #239 profile tests can wire PMService + IdentityOrgRepo
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
	svc := agentsvc.New(agentsvc.Deps{
		DB:       db,
		Agents:   agents,
		Activity: agentsql.NewActivityEventRepo(db),
		Skills:   agentsql.NewInstalledSkillRepo(db),
		Workers:  workers,
		Outbox:   outboxsql.NewOutboxRepo(db),
		IDGen:    gen,
		Clock:    clk,
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
		deps: deps, verifier: verifier, agents: agents, clk: clk, db: db,
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
