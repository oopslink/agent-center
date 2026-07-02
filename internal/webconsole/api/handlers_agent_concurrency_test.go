package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/concurrency"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsqlite "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
	"github.com/oopslink/agent-center/internal/workforce"
	wfsqlite "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

// arID resolves an agent's EXECUTION-entity id (string(a.ID()), the AR ULID) from its
// member id — this is the id the WORKER keys its concurrency snapshot by (the lifecycle
// event's agent_id). Tests MUST seed the LiveState store under THIS key, not the member
// id, to exercise the real production write path (issue-c44ccf6b): keying by the member
// id (as the pre-fix tests did) accidentally matched the buggy read key and hid the bug.
func arID(t *testing.T, deps HandlerDeps, memberID string) string {
	t.Helper()
	a, err := deps.AgentSvc.ResolveAgent(context.Background(), memberID)
	if err != nil {
		t.Fatalf("resolve agent %s: %v", memberID, err)
	}
	return string(a.ID())
}

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

	// Seed a fresh snapshot under the REAL worker write key (the AR id), NOT the member
	// id — matching production (issue-c44ccf6b). The handler must resolve the member-id
	// URL to a.ID() and find it there.
	store.Put(arID(t, deps, id), concurrency.AgentSnapshot{
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

// issue-c44ccf6b root-cause regression: the worker writes the snapshot under the AR id
// (string(a.ID())), while the read handler must look it up by the SAME AR id — NOT the
// member id (agent-<hex>) that agentFacingID prefers. For a member-provisioned agent the
// two ids DIFFER, so a member-keyed read missed every running snapshot and reported
// "concurrency not active" while the agent was busy. This test seeds under the real write
// key and asserts the read finds it; a negative control seeds under the OLD (member-id)
// key and asserts the read does NOT find it — guarding against a regression back to a
// member-keyed lookup. The outward `agent_id` field stays the member id (contract).
func TestAPI_AgentConcurrency_ReadsByARKeyNotMemberID(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	store := concurrency.NewInMemoryStore()
	deps.LiveState = store
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"named","model":"claude","cli":"claude-code","worker_id":"w-1"}`, sess)
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	memberID, _ := created["identity_id"].(string)
	arKey := arID(t, deps, memberID)

	// Precondition: the two ids are genuinely different (else the test proves nothing).
	if arKey == memberID {
		t.Fatalf("expected member id (%s) != AR id (%s) for a member-provisioned agent", memberID, arKey)
	}

	snap := concurrency.AgentSnapshot{
		Active:    2,
		Executors: []concurrency.ExecutorSnapshot{{ExecutorID: "e1", TaskID: "t1", State: concurrency.StateRunning, StartedAt: time.Now()}},
	}

	// Negative control: seeding under the MEMBER id (the pre-fix buggy key) must NOT be
	// found — the read is keyed by the AR id.
	store.Put(memberID, snap, time.Now())
	resp = orgScopedGet(t, s.URL+"/api/agents/"+memberID+"/concurrency", sess)
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if hs, _ := body["has_snapshot"].(bool); hs {
		t.Fatalf("member-id-keyed snapshot must NOT be found (read keys by AR id); got has_snapshot=true")
	}

	// Real write path: seed under the AR id → the member-id URL resolves and finds it.
	store.Put(arKey, snap, time.Now())
	resp = orgScopedGet(t, s.URL+"/api/agents/"+memberID+"/concurrency", sess)
	body = map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if hs, _ := body["has_snapshot"].(bool); !hs {
		t.Errorf("AR-id-keyed snapshot must be found; got has_snapshot=false")
	}
	if active, _ := body["active"].(float64); active != 2 {
		t.Errorf("active = %v, want 2 (from the live snapshot)", body["active"])
	}
	if body["agent_id"] != memberID {
		t.Errorf("agent_id = %v, want member id %s (outward contract unchanged)", body["agent_id"], memberID)
	}
}

// issue-c44ccf6b fallback: with NO live snapshot, the occupancy must NOT collapse to a
// bare "—" — the handler surfaces the center-known in-progress count (PM
// AgentTaskLoad.Running) as `running`, and `concurrency_enabled` so the UI can tell a
// genuinely single-active agent apart from an enabled-but-awaiting one.
func TestAPI_AgentConcurrency_RunningFallback_NoSnapshot(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	deps.LiveState = concurrency.NewInMemoryStore() // empty: no snapshot
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"busy","model":"claude","cli":"claude-code","worker_id":"w-1"}`, sess)
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	memberID, _ := created["identity_id"].(string)

	// Seed one RUNNING task assigned to this agent (agent:<member-id>) straight into the
	// task repo on the same db PM reads — CountActiveByAssignee then reports Running=1.
	now := time.Now()
	tk, err := pm.RehydrateTask(pm.RehydrateTaskInput{
		ID: "task-run-1", ProjectID: "proj-x", Title: "in progress",
		Status: pm.TaskRunning, Assignee: pm.IdentityRef("agent:" + memberID),
		CreatedBy: "user:a", CreatedAt: now, UpdatedAt: now, Version: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pmsqlite.NewTaskRepo(db).Save(context.Background(), tk); err != nil {
		t.Fatal(err)
	}

	resp = orgScopedGet(t, s.URL+"/api/agents/"+memberID+"/concurrency", sess)
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if hs, _ := body["has_snapshot"].(bool); hs {
		t.Error("no snapshot must report has_snapshot=false")
	}
	if active, _ := body["active"].(float64); active != 0 {
		t.Errorf("active = %v, want 0 (no live snapshot)", body["active"])
	}
	if running, _ := body["running"].(float64); running != 1 {
		t.Errorf("running = %v, want 1 (center-known in-progress fallback)", body["running"])
	}
	// A default agent (no allowed_executors) is single-active → concurrency_enabled=false.
	if ce, ok := body["concurrency_enabled"].(bool); !ok || ce {
		t.Errorf("concurrency_enabled = %v, want false (default single-active agent)", body["concurrency_enabled"])
	}
}
