package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// v2.7.1 #236: POST /api/members/agent is the single agent-create path (both the
// AgentCreateModal and MemberNew UIs, plus any direct API caller). An empty model
// must be defaulted at this API boundary so the stored model is never null — the
// bulletproof floor for @oopslink's "AgentDetail Profile renders blank" dogfood
// pain, which recurred via the MemberNew path the #232 frontend prefill missed.
func TestAPI_AddAgentMember_236_EmptyModelDefaults(t *testing.T) {
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
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("create: got %d, want 201", resp.StatusCode)
			}
			var res map[string]any
			_ = json.NewDecoder(resp.Body).Decode(&res)
			identityID, _ := res["identity_id"].(string)
			if identityID == "" {
				t.Fatalf("missing identity_id: %v", res)
			}
			a, err := deps.AgentSvc.ResolveAgent(context.Background(), identityID)
			if err != nil {
				t.Fatalf("agent not resolvable: %v", err)
			}
			if got := a.Profile().Model; got != defaultAgentModel {
				t.Fatalf("empty-model create stored model=%q, want %q (#236 backend floor)", got, defaultAgentModel)
			}
		})
	}
}

// An explicit model is preserved verbatim — the default is a floor, not an override.
func TestAPI_AddAgentMember_236_ExplicitModelPreserved(t *testing.T) {
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
