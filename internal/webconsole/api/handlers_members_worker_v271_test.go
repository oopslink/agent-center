package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// v2.7.1 #226: the Members list reads an agent's worker binding from the Agent BC
// (set by the unified CreateAgent), not the legacy AgentInstance table (which the
// unified path never writes) — so "Running on {worker}" shows the bound worker
// instead of "Not bound to a worker".
func TestListMembers_AgentWorkerBinding_FromAgentBC(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	s := newTestServer(t, deps)
	defer s.Close()

	// Unified create: sets WorkerID on the Agent AR (no AgentInstance row).
	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"BoundBot","model":"claude","cli":"claude-code","worker_id":"w-1"}`, sess)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("create agent member status=%d", resp.StatusCode)
	}

	listResp := orgScopedGet(t, s.URL+"/api/members", sess)
	var rows []map[string]any
	if err := json.NewDecoder(listResp.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range rows {
		if row["display_name"] == "BoundBot" {
			found = true
			if row["worker_id"] != "w-1" {
				t.Fatalf("agent worker_id=%v, want w-1 (#226 read from Agent BC)", row["worker_id"])
			}
		}
	}
	if !found {
		t.Fatalf("BoundBot not in members list: %+v", rows)
	}
}
