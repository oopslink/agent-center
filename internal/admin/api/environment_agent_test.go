package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

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
// v2.7 D2-c-i — controller→center feedback admin endpoints.
//
// These tests exercise the REAL admin server + AuthMiddleware. They reuse the
// per-agent guardrail (requireAgentOnWorker): worker from the TOKEN OWNER, target
// agent must be bound to it. The lifecycle-feedback tests assert the CRITICAL
// loop-avoidance invariant: a feedback call emits NO outbox event (so the
// AgentControlProjector never re-enqueues a reconcile).
// =============================================================================

type fbFixture struct {
	deps     HandlerDeps
	verifier *fakeVerifier
	agents   *agentsql.AgentRepo
	work     *agentsql.WorkItemRepo
	activity *agentsql.ActivityEventRepo
	outbox   *outboxsql.OutboxRepo
	ctx      context.Context
}

// newFBFixture seeds two workers (W1, W2) and builds HandlerDeps wired with the
// AgentService + the raw agent repos so tests can seed agents in arbitrary
// lifecycle states and assert persisted effects.
func newFBFixture(t *testing.T) *fbFixture {
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
	work := agentsql.NewWorkItemRepo(db)
	activity := agentsql.NewActivityEventRepo(db)
	ob := outboxsql.NewOutboxRepo(db)
	svc := agentsvc.New(agentsvc.Deps{
		DB: db, Agents: agents, WorkItems: work, Activity: activity,
		Workers: workers, Outbox: ob, IDGen: gen, Clock: clk,
	})

	for _, id := range []string{atWorker1, atWorker2} {
		w, werr := workforce.NewWorker(workforce.NewWorkerInput{
			ID: workforce.WorkerID(id), Capabilities: []string{"claude-code"},
			OrganizationID: atTestOrg, EnrolledAt: atNow,
		})
		if werr != nil {
			t.Fatal(werr)
		}
		if werr := workers.Save(ctx, w); werr != nil {
			t.Fatal(werr)
		}
	}

	verifier := &fakeVerifier{tokens: map[string]*admintoken.AdminToken{}}
	return &fbFixture{
		deps: HandlerDeps{
			DB: db, AgentSvc: svc, WorkerRepo: workers,
			AgentWorkItemRepo: work, AgentActivityRepo: activity,
		},
		verifier: verifier, agents: agents, work: work, activity: activity, outbox: ob, ctx: ctx,
	}
}

func (f *fbFixture) addWorkerToken(t *testing.T, plaintext, workerID string) {
	t.Helper()
	tok, err := admintoken.New(admintoken.NewAdminTokenInput{
		ID: admintoken.TokenID("T-" + plaintext), Owner: admintoken.Owner("worker:" + workerID),
		Scopes: []admintoken.Scope{"*"}, ValueHash: admintoken.HashPlaintext(plaintext),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.verifier.tokens[plaintext] = tok
}

// seedAgentLifecycle inserts an Agent bound to workerID in the given lifecycle.
func (f *fbFixture) seedAgentLifecycle(t *testing.T, id, workerID string, lc agent.AgentLifecycle) {
	t.Helper()
	a, err := agent.RehydrateAgent(agent.RehydrateAgentInput{
		ID: agent.AgentID(id), OrganizationID: atTestOrg,
		Profile: agent.Profile{Name: id}, WorkerID: workerID,
		Lifecycle: lc, CreatedBy: "system",
		CreatedAt: atNow, UpdatedAt: atNow, Version: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.agents.Save(f.ctx, a); err != nil {
		t.Fatal(err)
	}
}

func (f *fbFixture) seedWorkItem(t *testing.T, id, agentID, taskRef string, status agent.WorkItemStatus) {
	t.Helper()
	wi, err := agent.RehydrateWorkItem(agent.RehydrateWorkItemInput{
		ID: id, AgentID: agent.AgentID(agentID), TaskRef: taskRef,
		Status: status, CreatedAt: atNow, UpdatedAt: atNow, Version: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work.Save(f.ctx, wi); err != nil {
		t.Fatal(err)
	}
}

func (f *fbFixture) outboxCount(t *testing.T) int {
	t.Helper()
	evts, err := f.outbox.FetchUnprocessed(f.ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	return len(evts)
}

func (f *fbFixture) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := NewServerWithDeps("", ServerDeps{})
	h := AuthMiddleware(f.verifier)(WithDeps(f.deps)(srv.Handler()))
	httpsrv := httptest.NewServer(h)
	t.Cleanup(httpsrv.Close)
	return httpsrv
}

// --- activity sink ----------------------------------------------------------

func TestEnvAgentActivity_Appends(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent1, atWorker1, agent.LifecycleRunning)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/environment/agent/activity", "acat_fb_w1", map[string]any{
		"agent_id": atAgent1, "event_type": "assistant_text",
		"payload": `{"text":"hi"}`, "work_item_ref": "wi-1",
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	if body["ok"] != true || body["id"] == "" || body["id"] == nil {
		t.Fatalf("want ok + id, got %v", body)
	}
	evts, err := f.activity.ListByAgent(f.ctx, agent.AgentID(atAgent1), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(evts) != 1 || evts[0].EventType() != "assistant_text" || evts[0].WorkItemRef() != "wi-1" {
		t.Fatalf("activity event not appended as expected: %+v", evts)
	}
}

func TestEnvAgentActivity_CrossWorker_403(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent2, atWorker2, agent.LifecycleRunning)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/environment/agent/activity", "acat_fb_w1", map[string]any{
		"agent_id": atAgent2, "event_type": "status",
	})
	if status != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("want 403 agent_not_bound_to_worker, got %d %v", status, body)
	}
}

// --- lifecycle feedback (loop-avoidance) ------------------------------------

func TestEnvAgentLifecycleFeedback_Stopped_NoReconcileEvent(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent1, atWorker1, agent.LifecycleStopping)
	srv := f.server(t)

	before := f.outboxCount(t)
	status, body := postBearer(t, srv.URL, "/admin/environment/agent/lifecycle-feedback", "acat_fb_w1", map[string]any{
		"agent_id": atAgent1, "state": "stopped",
	})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d, want 200 ok; body = %v", status, body)
	}
	// Agent settled to stopped.
	a, _ := f.agents.FindByID(f.ctx, agent.AgentID(atAgent1))
	if a.Lifecycle() != agent.LifecycleStopped {
		t.Fatalf("agent lifecycle = %s, want stopped", a.Lifecycle())
	}
	// CRITICAL: NO outbox event emitted (no agent.lifecycle_changed → no reconcile
	// loop). The outbox count is unchanged.
	if after := f.outboxCount(t); after != before {
		evts, _ := f.outbox.FetchUnprocessed(f.ctx, 1000)
		t.Fatalf("lifecycle-feedback emitted %d new outbox events (loop risk!): %+v", after-before, evts)
	}
}

func TestEnvAgentLifecycleFeedback_Error_SetsMessage(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent1, atWorker1, agent.LifecycleRunning)
	srv := f.server(t)

	before := f.outboxCount(t)
	status, body := postBearer(t, srv.URL, "/admin/environment/agent/lifecycle-feedback", "acat_fb_w1", map[string]any{
		"agent_id": atAgent1, "state": "error", "error": "boom",
	})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d, want 200 ok; body = %v", status, body)
	}
	a, _ := f.agents.FindByID(f.ctx, agent.AgentID(atAgent1))
	if a.Lifecycle() != agent.LifecycleError || a.LifecycleError() != "boom" {
		t.Fatalf("agent = %s/%q, want error/boom", a.Lifecycle(), a.LifecycleError())
	}
	if after := f.outboxCount(t); after != before {
		t.Fatalf("error feedback emitted %d new outbox events (loop risk!)", after-before)
	}
}

// TestEnvAgentLifecycleFeedback_IllegalStopped_409: MarkStopped on a RUNNING
// agent violates the AR precondition (must be stopping/resetting) → 409.
func TestEnvAgentLifecycleFeedback_IllegalStopped_409(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent1, atWorker1, agent.LifecycleRunning)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/environment/agent/lifecycle-feedback", "acat_fb_w1", map[string]any{
		"agent_id": atAgent1, "state": "stopped",
	})
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (illegal MarkStopped on running); body = %v", status, body)
	}
}

func TestEnvAgentLifecycleFeedback_CrossWorker_403(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent2, atWorker2, agent.LifecycleStopping)
	srv := f.server(t)

	status, _ := postBearer(t, srv.URL, "/admin/environment/agent/lifecycle-feedback", "acat_fb_w1", map[string]any{
		"agent_id": atAgent2, "state": "stopped",
	})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (cross-worker)", status)
	}
}

// --- work-item state --------------------------------------------------------

func TestEnvAgentWorkItemState_ActiveDoneFailed(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent1, atWorker1, agent.LifecycleRunning)
	f.seedWorkItem(t, "wi-act", atAgent1, "pm://tasks/T1", agent.WorkItemQueued)
	f.seedWorkItem(t, "wi-done", atAgent1, "pm://tasks/T2", agent.WorkItemActive)
	f.seedWorkItem(t, "wi-fail", atAgent1, "pm://tasks/T3", agent.WorkItemActive)
	srv := f.server(t)

	cases := []struct {
		id, state string
		want      agent.WorkItemStatus
	}{
		{"wi-act", "active", agent.WorkItemActive},
		{"wi-done", "done", agent.WorkItemDone},
		{"wi-fail", "failed", agent.WorkItemFailed},
	}
	for _, c := range cases {
		status, body := postBearer(t, srv.URL, "/admin/environment/agent/work-item-state", "acat_fb_w1", map[string]any{
			"agent_id": atAgent1, "work_item_id": c.id, "state": c.state,
		})
		if status != http.StatusOK || body["ok"] != true {
			t.Fatalf("[%s] status = %d, want 200 ok; body = %v", c.state, status, body)
		}
		wi, _ := f.work.FindByID(f.ctx, c.id)
		if wi.Status() != c.want {
			t.Fatalf("[%s] work item status = %s, want %s", c.state, wi.Status(), c.want)
		}
	}
}

// TestEnvAgentWorkItemState_IllegalTransition_409: done→active is rejected by
// the AR state machine (terminal) → 409.
func TestEnvAgentWorkItemState_IllegalTransition_409(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent1, atWorker1, agent.LifecycleRunning)
	f.seedWorkItem(t, "wi-term", atAgent1, "pm://tasks/T1", agent.WorkItemDone)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/environment/agent/work-item-state", "acat_fb_w1", map[string]any{
		"agent_id": atAgent1, "work_item_id": "wi-term", "state": "active",
	})
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (illegal terminal transition); body = %v", status, body)
	}
}

// TestEnvAgentWorkItemState_NotOwned_404: a WorkItem belonging to a different
// agent → 404 (ownership guardrail), even though the worker owns the asserted
// agent.
func TestEnvAgentWorkItemState_NotOwned_404(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent1, atWorker1, agent.LifecycleRunning)
	// WorkItem belongs to a different agent (also on W1, but not atAgent1).
	f.seedWorkItem(t, "wi-other", "OTHER", "pm://tasks/T1", agent.WorkItemQueued)
	srv := f.server(t)

	status, _ := postBearer(t, srv.URL, "/admin/environment/agent/work-item-state", "acat_fb_w1", map[string]any{
		"agent_id": atAgent1, "work_item_id": "wi-other", "state": "active",
	})
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (work item not owned by agent)", status)
	}
}

func TestEnvAgentWorkItemState_CrossWorker_403(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent2, atWorker2, agent.LifecycleRunning)
	f.seedWorkItem(t, "wi-x", atAgent2, "pm://tasks/T1", agent.WorkItemQueued)
	srv := f.server(t)

	status, _ := postBearer(t, srv.URL, "/admin/environment/agent/work-item-state", "acat_fb_w1", map[string]any{
		"agent_id": atAgent2, "work_item_id": "wi-x", "state": "active",
	})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (cross-worker)", status)
	}
}
