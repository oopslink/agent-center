package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	"github.com/oopslink/agent-center/internal/workforce"
	wfsqlite "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

// saveWorkerInOrg saves a workforce.Worker row owned by orgID directly via the
// sqlite repo so the Agent BC worker-in-org check passes for /api/agents tests.
func saveWorkerInOrg(t *testing.T, db *sql.DB, orgID, workerID string) {
	t.Helper()
	w, err := workforce.NewWorker(workforce.NewWorkerInput{
		ID:             workforce.WorkerID(workerID),
		Capabilities:   []string{"claude-code"},
		EnrolledAt:     time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC),
		OrganizationID: orgID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := wfsqlite.NewWorkerRepo(db).Save(context.Background(), w); err != nil {
		t.Fatal(err)
	}
}

func TestAPI_Agent_FullLifecycle(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	s := newTestServer(t, deps)
	defer s.Close()

	// POST /api/agents → 200, stopped.
	resp := orgScopedPost(t, s.URL+"/api/agents",
		`{"name":"coder","description":"d","model":"claude","cli":"claude-code","worker_id":"w-1"}`, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create: got %d", resp.StatusCode)
	}
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	if created["lifecycle"] != "stopped" {
		t.Fatalf("created lifecycle = %v, want stopped", created["lifecycle"])
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("missing agent id: %v", created)
	}

	// GET list contains it.
	resp = orgScopedGet(t, s.URL+"/api/agents", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: got %d", resp.StatusCode)
	}
	var list struct {
		Agents []map[string]any `json:"agents"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&list)
	if len(list.Agents) != 1 || list.Agents[0]["id"] != id {
		t.Fatalf("list = %+v", list.Agents)
	}

	// GET by id.
	resp = orgScopedGet(t, s.URL+"/api/agents/"+id, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: got %d", resp.StatusCode)
	}
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got["id"] != id || got["name"] != "coder" {
		t.Fatalf("get = %+v", got)
	}

	// POST start → running.
	resp = orgScopedPost(t, s.URL+"/api/agents/"+id+"/start", `{}`, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("start: got %d", resp.StatusCode)
	}
	var started map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&started)
	if started["lifecycle"] != "running" {
		t.Fatalf("started lifecycle = %v, want running", started["lifecycle"])
	}

	// POST reset without confirm → 400.
	resp = orgScopedPost(t, s.URL+"/api/agents/"+id+"/reset", `{"scope":"memory","confirm":false}`, sess)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("reset no-confirm: got %d, want 400", resp.StatusCode)
	}

	// POST reset with confirm → 200, resetting.
	resp = orgScopedPost(t, s.URL+"/api/agents/"+id+"/reset", `{"scope":"memory","confirm":true}`, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reset confirm: got %d", resp.StatusCode)
	}
	var reset map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&reset)
	if reset["lifecycle"] != "resetting" {
		t.Fatalf("reset lifecycle = %v, want resetting", reset["lifecycle"])
	}

	// work-items + activity → empty collections.
	resp = orgScopedGet(t, s.URL+"/api/agents/"+id+"/work-items", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("work-items: got %d", resp.StatusCode)
	}
	var wis struct {
		WorkItems []map[string]any `json:"work_items"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&wis)
	if wis.WorkItems == nil || len(wis.WorkItems) != 0 {
		t.Fatalf("work_items = %+v, want []", wis.WorkItems)
	}

	resp = orgScopedGet(t, s.URL+"/api/agents/"+id+"/activity", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("activity: got %d", resp.StatusCode)
	}
	var act struct {
		Activity []map[string]any `json:"activity"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&act)
	if act.Activity == nil || len(act.Activity) != 0 {
		t.Fatalf("activity = %+v, want []", act.Activity)
	}
}

func TestAPI_Agent_CreateWorkerNotInOrg(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	// Seed a worker in a DIFFERENT org → create must 400.
	saveWorkerInOrg(t, db, "org-other", "w-other")
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/agents",
		`{"name":"coder","model":"claude","cli":"claude-code","worker_id":"w-other"}`, sess)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("cross-org worker: got %d, want 400", resp.StatusCode)
	}
}

// TestAPI_Agent_CrossOrgAgent404 proves the org-in gate: an agent that belongs
// to another org is invisible (404) to a member of a different org, and an
// unknown id is 404 too.
func TestAPI_Agent_CrossOrgAgent404(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	// Seed a worker + create an agent in ANOTHER org via the wired service.
	saveWorkerInOrg(t, db, "org-other", "w-other")
	foreignID, err := deps.AgentSvc.CreateAgent(context.Background(), agentsvc.CreateAgentCommand{
		OrganizationID: "org-other", Name: "foreign", Model: "claude", CLI: "claude-code",
		WorkerID: "w-other", CreatedBy: "user:someone",
	})
	if err != nil {
		t.Fatal(err)
	}

	// GET that foreign agent as the session member → 404 (cross-org hidden).
	resp := orgScopedGet(t, s.URL+"/api/agents/"+string(foreignID), sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-org agent get: got %d, want 404", resp.StatusCode)
	}
	// Unknown id → 404 too.
	resp = orgScopedGet(t, s.URL+"/api/agents/does-not-exist", sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown agent get: got %d, want 404", resp.StatusCode)
	}
}
