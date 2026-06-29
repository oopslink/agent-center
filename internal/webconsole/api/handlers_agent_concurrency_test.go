package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/concurrency"
	"github.com/oopslink/agent-center/internal/workforce"
	wfsqlite "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

// v2.19.0 GET .../agents/{id}/concurrency: the cap (profile) + queued (pm) joined
// with the worker's last-known live executor snapshot (store).
func TestAPI_AgentConcurrency_JoinsCapAndSnapshot(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	store := concurrency.NewInMemoryStore()
	deps.LiveState = store
	s := newTestServer(t, deps)
	defer s.Close()

	// Create an agent → its business id (identity_id) is the store key the worker uses.
	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"coder","description":"d","model":"claude","cli":"claude-code","worker_id":"w-1"}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create agent: %d", resp.StatusCode)
	}
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	id, _ := created["identity_id"].(string)
	if id == "" {
		t.Fatal("missing agent id")
	}

	// Seed a fresh snapshot for this agent.
	store.Put(id, concurrency.AgentSnapshot{
		Active: 1,
		Executors: []concurrency.ExecutorSnapshot{
			{ExecutorID: "e1", TaskID: "t1", CLI: "codex", Model: "gpt-5.5", State: concurrency.StateRunning, PID: 99, StartedAt: time.Now()},
		},
	}, time.Now())

	resp = orgScopedGet(t, s.URL+"/api/agents/"+id+"/concurrency", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("concurrency: %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)

	if body["agent_id"] != id {
		t.Errorf("agent_id = %v, want %s", body["agent_id"], id)
	}
	// A default agent (no allowed_executors) has effective cap 1 (single-active).
	if cap, _ := body["cap"].(float64); cap != 1 {
		t.Errorf("cap = %v, want 1 (default single-active)", body["cap"])
	}
	if active, _ := body["active"].(float64); active != 1 {
		t.Errorf("active = %v, want 1", body["active"])
	}
	if stale, _ := body["stale"].(bool); stale {
		t.Errorf("fresh snapshot must not be stale")
	}
	if q, ok := body["queued"].(float64); !ok || q != 0 {
		t.Errorf("queued = %v, want 0 (no pending tasks)", body["queued"])
	}
	execs, _ := body["executors"].([]any)
	if len(execs) != 1 {
		t.Fatalf("executors len = %d, want 1", len(execs))
	}
	e0, _ := execs[0].(map[string]any)
	if e0["executor_id"] != "e1" || e0["cli"] != "codex" || e0["task_id"] != "t1" || e0["state"] != "running" {
		t.Errorf("executor = %v", e0)
	}
}

// No snapshot for the agent → stale=true, active=0, but cap still resolves.
func TestAPI_AgentConcurrency_NoSnapshot_Stale(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	deps.LiveState = concurrency.NewInMemoryStore() // empty
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"c2","model":"claude","cli":"claude-code","worker_id":"w-1"}`, sess)
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	id, _ := created["identity_id"].(string)

	resp = orgScopedGet(t, s.URL+"/api/agents/"+id+"/concurrency", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("concurrency: %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if stale, _ := body["stale"].(bool); !stale {
		t.Error("no snapshot must report stale=true")
	}
	if active, _ := body["active"].(float64); active != 0 {
		t.Errorf("active = %v, want 0", body["active"])
	}
	if cap, _ := body["cap"].(float64); cap != 1 {
		t.Errorf("cap = %v, want 1", body["cap"])
	}
}

// T606 (issue-af03da2f): an ONLINE worker that never reported a snapshot →
// has_snapshot=false + reachable=true, so the UI renders the NEUTRAL "no live data"
// state (concurrency not active) rather than the misleading "worker unreachable".
func TestAPI_AgentConcurrency_OnlineNoSnapshot_NoData(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerWithStatus(t, db, sess.OrgID, "w-on", workforce.WorkerOnline)
	deps.WorkerRepo = wfsqlite.NewWorkerRepo(db)
	deps.LiveState = concurrency.NewInMemoryStore() // empty
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"c-on","model":"claude","cli":"claude-code","worker_id":"w-on"}`, sess)
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	id, _ := created["identity_id"].(string)

	resp = orgScopedGet(t, s.URL+"/api/agents/"+id+"/concurrency", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("concurrency: %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if hs, ok := body["has_snapshot"].(bool); !ok || hs {
		t.Errorf("has_snapshot = %v, want false (never reported)", body["has_snapshot"])
	}
	if rc, ok := body["reachable"].(bool); !ok || !rc {
		t.Errorf("reachable = %v, want true (worker online)", body["reachable"])
	}
}

// T606: an OFFLINE worker → reachable=false, so the UI shows the distinct "worker
// offline" state rather than the generic "unreachable".
func TestAPI_AgentConcurrency_WorkerOffline_NotReachable(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerWithStatus(t, db, sess.OrgID, "w-off", workforce.WorkerOffline)
	deps.WorkerRepo = wfsqlite.NewWorkerRepo(db)
	deps.LiveState = concurrency.NewInMemoryStore() // empty
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"c-off","model":"claude","cli":"claude-code","worker_id":"w-off"}`, sess)
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	id, _ := created["identity_id"].(string)

	resp = orgScopedGet(t, s.URL+"/api/agents/"+id+"/concurrency", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("concurrency: %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if rc, ok := body["reachable"].(bool); !ok || rc {
		t.Errorf("reachable = %v, want false (worker offline)", body["reachable"])
	}
}

// Non-member / unauthenticated callers are rejected.
func TestAPI_AgentConcurrency_RequiresAuth(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	deps.LiveState = concurrency.NewInMemoryStore()
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"c3","model":"claude","cli":"claude-code","worker_id":"w-1"}`, sess)
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	id, _ := created["identity_id"].(string)

	// No session cookie → unauthorized.
	r, err := http.Get(s.URL + "/api/orgs/" + sess.OrgSlug + "/agents/" + id + "/concurrency")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusUnauthorized && r.StatusCode != http.StatusForbidden {
		t.Fatalf("unauthenticated status = %d, want 401/403", r.StatusCode)
	}
}
