package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// v2.18.3 BE-1 HTTP round-trips: task required_capabilities (create → GET),
// agent auto_assignable (create default + PATCH → GET), and the project-level
// auto_assign_enabled master switch (GET default ON → PATCH false → GET false).
func TestAPI_AutoAssign_BE1_RoundTrip(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	s := newTestServer(t, deps)
	defer s.Close()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, err := deps.PM.CreateProject(context.Background(), pmservice.CreateProjectCommand{
		OrganizationID: sess.OrgID, Name: "Acme", CreatedBy: caller,
	})
	if err != nil {
		t.Fatal(err)
	}

	// --- task.required_capabilities: create with caps → GET shows canonical set ---
	resp := orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/tasks",
		`{"title":"build","required_capabilities":[" Go ","go","RUST"]}`, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("create task: %d", resp.StatusCode)
	}
	var task map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&task)
	caps, _ := task["required_capabilities"].([]any)
	if len(caps) != 2 || caps[0] != "go" || caps[1] != "rust" {
		t.Fatalf("required_capabilities = %v, want [go rust]", task["required_capabilities"])
	}
	// A task created without caps emits [] (always an array).
	resp = orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/tasks", `{"title":"plain"}`, sess)
	var plain map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&plain)
	if c, ok := plain["required_capabilities"].([]any); !ok || len(c) != 0 {
		t.Fatalf("plain task required_capabilities = %v, want []", plain["required_capabilities"])
	}

	// --- agent.auto_assignable: default true on create; PATCH → false → GET ---
	resp = orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"coder","model":"claude-opus-4-8","cli":"claude-code","worker_id":"w-1"}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create agent: %d", resp.StatusCode)
	}
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	aid, _ := created["identity_id"].(string)

	resp = orgScopedGet(t, s.URL+"/api/agents/"+aid, sess)
	var ag map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&ag)
	if ag["auto_assignable"] != true {
		t.Fatalf("new agent auto_assignable = %v, want true (default)", ag["auto_assignable"])
	}
	resp = orgScopedPatch(t, s.URL+"/api/agents/"+aid+"/config",
		`{"model":"claude-opus-4-8","cli":"claude-code","auto_assignable":false}`, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch auto_assignable: %d", resp.StatusCode)
	}
	resp = orgScopedGet(t, s.URL+"/api/agents/"+aid, sess)
	_ = json.NewDecoder(resp.Body).Decode(&ag)
	if ag["auto_assignable"] != false {
		t.Fatalf("after PATCH auto_assignable = %v, want false", ag["auto_assignable"])
	}

	// --- project.auto_assign_enabled: GET default ON → PATCH false → GET false ---
	resp = orgScopedGet(t, s.URL+"/api/projects/"+string(pid), sess)
	var proj map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&proj)
	if proj["auto_assign_enabled"] != true {
		t.Fatalf("project auto_assign_enabled = %v, want true (default ON)", proj["auto_assign_enabled"])
	}
	resp = orgScopedPatch(t, s.URL+"/api/projects/"+string(pid), `{"auto_assign_enabled":false}`, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch project: %d", resp.StatusCode)
	}
	_ = json.NewDecoder(resp.Body).Decode(&proj)
	if proj["auto_assign_enabled"] != false {
		t.Fatalf("after PATCH auto_assign_enabled = %v, want false", proj["auto_assign_enabled"])
	}
	// Durable: a fresh GET still reflects it.
	resp = orgScopedGet(t, s.URL+"/api/projects/"+string(pid), sess)
	_ = json.NewDecoder(resp.Body).Decode(&proj)
	if proj["auto_assign_enabled"] != false {
		t.Fatalf("reread auto_assign_enabled = %v, want false", proj["auto_assign_enabled"])
	}
}
