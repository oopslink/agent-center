package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestAPI_AddAgentMember_Stage6RequiresExplicitRuntimeMirror(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	s := newTestServer(t, deps)
	defer s.Close()

	cases := map[string]string{
		"omitted":         `{"display_name":"Bot","cli":"claude-code","worker_id":"w-1"}`,
		"empty string":    `{"display_name":"Bot2","model":"","cli":"claude-code","worker_id":"w-1"}`,
		"whitespace only": `{"display_name":"Bot3","model":"   ","cli":"claude-code","worker_id":"w-1"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			resp := orgScopedPost(t, s.URL+"/api/members/agent", body, sess)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("create: got %d, want 400", resp.StatusCode)
			}
		})
	}
}

func TestAPI_AddAgentMember_Stage6ExplicitRuntimeMirrorPreserved(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"Bot","model":"claude-sonnet-4-6","cli":"claude-code","worker_id":"w-1"}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: got %d, want 201", resp.StatusCode)
	}
	var res map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&res)
	identityID, _ := res["identity_id"].(string)
	a, err := deps.AgentSvc.ResolveAgent(context.Background(), identityID)
	if err != nil {
		t.Fatalf("agent not resolvable: %v", err)
	}
	if got := a.Profile().Model; got != "claude-sonnet-4-6" {
		t.Fatalf("explicit model overwritten: got %q, want claude-sonnet-4-6", got)
	}
}
