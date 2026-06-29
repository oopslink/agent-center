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

// saveWorkerWithStatus saves a worker with an explicit online/offline status (T606
// reachability tests) — NewWorker always starts offline, so reaching the online
// state needs a rehydrate with the chosen status.
func saveWorkerWithStatus(t *testing.T, db *sql.DB, orgID, workerID string, status workforce.WorkerStatus) {
	t.Helper()
	enrolled := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	w, err := workforce.RehydrateWorker(workforce.RehydrateWorkerInput{
		ID:             workforce.WorkerID(workerID),
		Status:         status,
		Capabilities:   []string{"claude-code"},
		EnrolledAt:     enrolled,
		CreatedAt:      enrolled,
		UpdatedAt:      enrolled,
		Version:        1,
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

	// v2.7 #185: the single creation entry is POST /api/members/agent (atomic
	// identity+member+execution agent). The response is the member object; the
	// agent's business id is identity_id ("agent-<ulid>", == IdentityMemberID),
	// NOT the membership row id. The execution-entity ULID never appears.
	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"coder","description":"d","model":"claude","cli":"claude-code","worker_id":"w-1"}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: got %d", resp.StatusCode)
	}
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	if _, leaked := created["agent_id"]; leaked {
		t.Fatalf("create response must not leak execution-entity agent_id: %v", created)
	}
	id, _ := created["identity_id"].(string)
	if id == "" {
		t.Fatalf("missing agent business id (identity_id): %v", created)
	}

	// GET by id (member id) → resolves via the bridge to the agentMap. This is
	// where lifecycle/skills/env_vars live.
	resp = orgScopedGet(t, s.URL+"/api/agents/"+id, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: got %d", resp.StatusCode)
	}
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got["id"] != id || got["name"] != "coder" {
		t.Fatalf("get = %+v (id should be the member id, name coder)", got)
	}
	if got["lifecycle"] != "stopped" {
		t.Fatalf("created lifecycle = %v, want stopped", got["lifecycle"])
	}
	// v2.7 #183 (FINDING): a no-skills agent must serialize skills/env_vars as
	// [] / {} never null — the SPA types them non-null and reads a.skills.length.
	if _, ok := got["skills"].([]any); !ok {
		t.Fatalf("skills must be a JSON array (not null), got %T %v", got["skills"], got["skills"])
	}
	if _, ok := got["env_vars"].(map[string]any); !ok {
		t.Fatalf("env_vars must be a JSON object (not null), got %T %v", got["env_vars"], got["env_vars"])
	}

	// GET list contains it, addressed by the same member id.
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

	// POST reset without confirm → 400 (confirm is checked before the precondition).
	resp = orgScopedPost(t, s.URL+"/api/agents/"+id+"/reset", `{"scope":"memory","confirm":false}`, sess)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("reset no-confirm: got %d, want 400", resp.StatusCode)
	}

	// POST reset with confirm WHILE RUNNING → 409 reset_requires_stopped (v2.16 W5:
	// Reset is only legal from a settled state; the operator must stop the agent first).
	resp = orgScopedPost(t, s.URL+"/api/agents/"+id+"/reset", `{"scope":"memory","confirm":true}`, sess)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("reset while running: got %d, want 409", resp.StatusCode)
	}
	var resetErr map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&resetErr)
	if resetErr["error"] != "reset_requires_stopped" {
		t.Fatalf("reset while running error = %v, want reset_requires_stopped", resetErr["error"])
	}

	// Stop → stopping; the agent leaves running. (Settling to stopped is Environment
	// feedback not exercised here; the happy stopped→resetting reset path is covered
	// at the service layer in TestResetAgent.)
	resp = orgScopedPost(t, s.URL+"/api/agents/"+id+"/stop", `{}`, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stop: got %d", resp.StatusCode)
	}

	// tasks + activity → empty collections.
	resp = orgScopedGet(t, s.URL+"/api/agents/"+id+"/tasks", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tasks: got %d", resp.StatusCode)
	}
	var wis struct {
		Tasks []map[string]any `json:"tasks"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&wis)
	if wis.Tasks == nil || len(wis.Tasks) != 0 {
		t.Fatalf("tasks = %+v, want []", wis.Tasks)
	}

	resp = orgScopedGet(t, s.URL+"/api/agents/"+id+"/activity", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("activity: got %d", resp.StatusCode)
	}
	var act struct {
		Activity []map[string]any `json:"activity"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&act)
	// v2.14.0 ships main's T338: agent lifecycle ops (start/reset/…) are recorded on
	// the activity timeline, so after the start+reset above the feed carries those
	// lifecycle events (the pre-T338 expectation of an empty feed no longer holds).
	if len(act.Activity) == 0 {
		t.Fatalf("activity = %+v, want lifecycle events (T338)", act.Activity)
	}
	for _, ev := range act.Activity {
		if ev["event_type"] != "lifecycle" {
			t.Fatalf("activity event_type = %v, want lifecycle", ev["event_type"])
		}
	}
}

func TestAPI_Agent_CreateWorkerNotInOrg(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	// Seed a worker in a DIFFERENT org → unified create must 400 (and roll back).
	saveWorkerInOrg(t, db, "org-other", "w-other")
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"coder","model":"claude","cli":"claude-code","worker_id":"w-other"}`, sess)
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

// v2.7 #197: DELETE /api/agents/{id} hard-deletes a stopped, idle agent AND
// cascade-deletes its identity-member + identity (symmetric to #157's atomic
// create — no orphan member left). Addressed by the business (member) id.
func TestAPI_AgentDelete_CascadesIdentityMember(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()

	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"DelBot","model":"claude","cli":"claude-code","worker_id":"w-1"}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d", resp.StatusCode)
	}
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	memberID, _ := created["identity_id"].(string)
	if memberID == "" {
		t.Fatalf("no identity_id: %v", created)
	}

	del := orgScopedDelete(t, s.URL+"/api/agents/"+memberID, sess)
	if del.StatusCode != http.StatusOK {
		t.Fatalf("delete: status %d", del.StatusCode)
	}
	del.Body.Close()

	if _, err := deps.AgentSvc.ResolveAgent(ctx, memberID); err == nil {
		t.Fatalf("agent must be gone after delete")
	}
	if _, err := deps.IdentityRepo.GetByID(ctx, memberID); err == nil {
		t.Fatalf("identity must be cascade-deleted")
	}
	if m, _ := deps.MemberRepo.GetByOrganizationAndIdentity(ctx, sess.OrgID, memberID); m != nil {
		t.Fatalf("member must be cascade-deleted")
	}
}

// v2.7 #197: a non-Stopped agent cannot be deleted (operator stops it first).
func TestAPI_AgentDelete_RunningRejected(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()

	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"RunBot","model":"claude","cli":"claude-code","worker_id":"w-1"}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d", resp.StatusCode)
	}
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	memberID, _ := created["identity_id"].(string)

	a, err := deps.AgentSvc.ResolveAgent(ctx, memberID)
	if err != nil {
		t.Fatal(err)
	}
	if err := deps.AgentSvc.StartAgent(ctx, a.ID()); err != nil {
		t.Fatalf("start: %v", err)
	}

	del := orgScopedDelete(t, s.URL+"/api/agents/"+memberID, sess)
	defer del.Body.Close()
	if del.StatusCode != http.StatusConflict {
		t.Fatalf("running agent delete must be 409, got %d", del.StatusCode)
	}
	var e map[string]any
	_ = json.NewDecoder(del.Body).Decode(&e)
	if e["error"] != "agent_running" {
		t.Fatalf("want error agent_running, got %v", e)
	}
	// Still present (delete was rejected).
	if _, err := deps.AgentSvc.ResolveAgent(ctx, memberID); err != nil {
		t.Fatalf("agent must still exist after rejected delete")
	}
}
