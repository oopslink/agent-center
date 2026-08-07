package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// T728: POST /api/members/agent exposes include_description_in_system_prompt.
// Omitted → the backend floor default (true); explicit false → stored false. The GET
// serialization round-trips the flag so the edit UI can echo the current value.
func TestAPI_AddAgentMember_T728_IncludeDescriptionSwitch(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	s := newTestServer(t, deps)
	defer s.Close()

	cases := []struct {
		name string
		body string
		want bool
	}{
		{"omitted defaults true", `{"display_name":"BotA","cli":"claude-code","worker_id":"w-1"}`, true},
		{"explicit true", `{"display_name":"BotB","cli":"claude-code","worker_id":"w-1","include_description_in_system_prompt":true}`, true},
		{"explicit false", `{"display_name":"BotC","cli":"claude-code","worker_id":"w-1","include_description_in_system_prompt":false}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := orgScopedPost(t, s.URL+"/api/members/agent", c.body, sess)
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("create: got %d, want 201", resp.StatusCode)
			}
			var res map[string]any
			_ = json.NewDecoder(resp.Body).Decode(&res)
			identityID, _ := res["identity_id"].(string)
			if identityID == "" {
				t.Fatalf("missing identity_id: %v", res)
			}
			// Persisted value.
			a, err := deps.AgentSvc.ResolveAgent(context.Background(), identityID)
			if err != nil {
				t.Fatalf("agent not resolvable: %v", err)
			}
			if got := a.Profile().IncludeDescriptionInSystemPrompt; got != c.want {
				t.Fatalf("stored IncludeDescriptionInSystemPrompt=%v, want %v", got, c.want)
			}
			// GET serialization echoes the flag for the edit UI.
			gresp := orgScopedGet(t, s.URL+"/api/agents/"+identityID, sess)
			if gresp.StatusCode != http.StatusOK {
				t.Fatalf("GET: got %d, want 200", gresp.StatusCode)
			}
			var got map[string]any
			_ = json.NewDecoder(gresp.Body).Decode(&got)
			v, ok := got["include_description_in_system_prompt"].(bool)
			if !ok {
				t.Fatalf("GET missing include_description_in_system_prompt bool, got: %v", got["include_description_in_system_prompt"])
			}
			if v != c.want {
				t.Fatalf("GET include_description_in_system_prompt=%v, want %v", v, c.want)
			}
		})
	}
}
