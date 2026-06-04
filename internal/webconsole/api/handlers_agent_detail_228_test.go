package api

import (
	"encoding/json"
	"net/http"
	"testing"

	wfsqlite "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

// v2.7.1 #228: GET /api/agents/{id} (AgentDetail) enriches the base agentMap
// with the Profile-only fields — created_by_display_name (the creator),
// computer (the bound worker's label + connected state), and created_agents
// (always a slice, never null). These are detail-only: the list endpoint must
// stay lean (no per-row worker/identity/org-list fan-out).
func TestAPI_AgentDetail_228_ProfileEnrich(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	// WorkerRepo backs the #228 computer enrich; not wired by default.
	deps.WorkerRepo = wfsqlite.NewWorkerRepo(db)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	s := newTestServer(t, deps)
	defer s.Close()

	// The session user ("testuser") creates the agent → created_by = "user:<id>".
	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"coder","model":"claude","cli":"claude-code","worker_id":"w-1"}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: got %d", resp.StatusCode)
	}
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	id, _ := created["identity_id"].(string)
	if id == "" {
		t.Fatalf("missing agent business id: %v", created)
	}

	resp = orgScopedGet(t, s.URL+"/api/agents/"+id, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: got %d", resp.StatusCode)
	}
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)

	// Creator display name — resolved from the "user:"/"agent:" created_by ref.
	if got["created_by_display_name"] != "testuser" {
		t.Errorf("created_by_display_name=%v want testuser", got["created_by_display_name"])
	}

	// Computer — the bound worker w-1, offline (just enrolled, no heartbeat). Name
	// defaults to the worker id; daemon version is deliberately absent (no field).
	comp, ok := got["computer"].(map[string]any)
	if !ok {
		t.Fatalf("computer must be an object, got %T %v", got["computer"], got["computer"])
	}
	if comp["worker_id"] != "w-1" || comp["name"] != "w-1" {
		t.Errorf("computer id/name = %+v want w-1", comp)
	}
	if comp["status"] != "offline" || comp["connected"] != false {
		t.Errorf("computer status/connected = %+v want offline/false", comp)
	}

	// Created agents — always a JSON array (never null), empty for a leaf agent.
	ca, ok := got["created_agents"].([]any)
	if !ok {
		t.Fatalf("created_agents must be a JSON array, got %T %v", got["created_agents"], got["created_agents"])
	}
	if len(ca) != 0 {
		t.Errorf("created_agents = %+v, want []", ca)
	}

	// The list endpoint must NOT carry the detail-only enrich fields (#228 keeps
	// agentMap lean; only the single-agent detail load pays for the fan-out).
	resp = orgScopedGet(t, s.URL+"/api/agents", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: got %d", resp.StatusCode)
	}
	var list struct {
		Agents []map[string]any `json:"agents"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&list)
	if len(list.Agents) != 1 {
		t.Fatalf("list = %+v, want 1 agent", list.Agents)
	}
	if _, leaked := list.Agents[0]["created_agents"]; leaked {
		t.Errorf("list must NOT carry detail-only created_agents: %+v", list.Agents[0])
	}
	if _, leaked := list.Agents[0]["computer"]; leaked {
		t.Errorf("list must NOT carry detail-only computer: %+v", list.Agents[0])
	}
	if _, leaked := list.Agents[0]["created_by_display_name"]; leaked {
		t.Errorf("list must NOT carry detail-only created_by_display_name: %+v", list.Agents[0])
	}
}
