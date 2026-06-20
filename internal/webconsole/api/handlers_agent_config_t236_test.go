package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// T236: PATCH /api/agents/{id}/config edits the agent's LLM config (model / cli /
// reasoning / mode / provider). It persists immediately (applies on next restart)
// and returns the refreshed agent with the new values.
func TestAPI_Agent_UpdateConfig_236(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	s := newTestServer(t, deps)
	defer s.Close()

	// Create an agent (model defaults applied at the create boundary). No
	// reasoning/mode/provider yet → they read back empty (= runtime default).
	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"coder","model":"claude-opus-4-8","cli":"claude-code","worker_id":"w-1"}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: got %d", resp.StatusCode)
	}
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	id, _ := created["identity_id"].(string)
	if id == "" {
		t.Fatalf("missing agent id: %v", created)
	}

	// PATCH config: change model + cli + set reasoning/mode/provider.
	resp = orgScopedPatch(t, s.URL+"/api/agents/"+id+"/config",
		`{"model":"claude-sonnet-4-6","cli":"codex","reasoning":"high","mode":"plan","provider":"anthropic"}`, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update config: got %d, want 200", resp.StatusCode)
	}
	var updated map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&updated)
	for k, want := range map[string]string{
		"model": "claude-sonnet-4-6", "cli": "codex",
		"reasoning": "high", "mode": "plan", "provider": "anthropic",
	} {
		if got, _ := updated[k].(string); got != want {
			t.Fatalf("after update %s = %q, want %q (full: %+v)", k, got, want, updated)
		}
	}

	// The change is durable: a fresh GET reflects it.
	resp = orgScopedGet(t, s.URL+"/api/agents/"+id, sess)
	var reread map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&reread)
	if reread["reasoning"] != "high" || reread["provider"] != "anthropic" || reread["cli"] != "codex" {
		t.Fatalf("reread config not persisted: %+v", reread)
	}

	// Invalid reasoning → 400 invalid_reasoning (allowlist enforced).
	resp = orgScopedPatch(t, s.URL+"/api/agents/"+id+"/config",
		`{"model":"claude-sonnet-4-6","cli":"claude-code","reasoning":"turbo"}`, sess)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid reasoning: got %d, want 400", resp.StatusCode)
	}
	var errBody map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&errBody)
	if errBody["error"] != "invalid_reasoning" {
		t.Fatalf("invalid reasoning error = %v, want invalid_reasoning", errBody["error"])
	}

	// Invalid cli → 400 invalid_cli.
	resp = orgScopedPatch(t, s.URL+"/api/agents/"+id+"/config",
		`{"model":"m","cli":"opencode"}`, sess)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid cli: got %d, want 400", resp.StatusCode)
	}
}

// T236: create accepts the optional reasoning/mode/provider and round-trips them.
func TestAPI_Agent_Create_WithLLMTuning_236(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"coder","model":"claude-opus-4-8","cli":"claude-code","reasoning":"low","mode":"default","provider":"anthropic","worker_id":"w-1"}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: got %d", resp.StatusCode)
	}
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	id, _ := created["identity_id"].(string)

	resp = orgScopedGet(t, s.URL+"/api/agents/"+id, sess)
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got["reasoning"] != "low" || got["mode"] != "default" || got["provider"] != "anthropic" {
		t.Fatalf("create LLM tuning not persisted: %+v", got)
	}
}
