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

// v2.18.1 BE-1: PATCH /config accepts allowed_executors [{cli,model}] + max_concurrent_tasks;
// GET emits the authoritative list, the derived allowed_models mirror, and the
// concurrency status. An invalid executor cli → 400 invalid_executor_profile.
func TestAPI_Agent_AllowedExecutors_RoundTrip(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"coder","model":"claude-opus-4-8","cli":"claude-code","worker_id":"w-1"}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: got %d", resp.StatusCode)
	}
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	id, _ := created["identity_id"].(string)

	// Opt into concurrency with a two-CLI executor list.
	resp = orgScopedPatch(t, s.URL+"/api/agents/"+id+"/config",
		`{"model":"claude-opus-4-8","cli":"claude-code","max_concurrent_tasks":2,`+
			`"allowed_executors":[{"cli":"claude-code","model":"opus"},{"cli":"codex","model":"gpt-5-codex"}]}`, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update with executors: got %d, want 200", resp.StatusCode)
	}

	resp = orgScopedGet(t, s.URL+"/api/agents/"+id, sess)
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)
	execs, _ := got["allowed_executors"].([]any)
	if len(execs) != 2 {
		t.Fatalf("allowed_executors = %v, want 2 entries", got["allowed_executors"])
	}
	first, _ := execs[0].(map[string]any)
	if first["cli"] != "claude-code" || first["model"] != "opus" {
		t.Fatalf("first executor = %v, want {claude-code, opus}", first)
	}
	if got["concurrency_enabled"] != true {
		t.Fatalf("concurrency_enabled = %v, want true", got["concurrency_enabled"])
	}
	if cap, _ := got["effective_concurrency_cap"].(float64); cap != 2 {
		t.Fatalf("effective_concurrency_cap = %v, want 2", got["effective_concurrency_cap"])
	}
	// Derived allowed_models mirror = distinct models.
	models, _ := got["allowed_models"].([]any)
	if len(models) != 2 || models[0] != "opus" || models[1] != "gpt-5-codex" {
		t.Fatalf("derived allowed_models = %v, want [opus gpt-5-codex]", got["allowed_models"])
	}

	// Invalid executor cli → 400 invalid_executor_profile.
	resp = orgScopedPatch(t, s.URL+"/api/agents/"+id+"/config",
		`{"model":"m","cli":"claude-code","allowed_executors":[{"cli":"opencode","model":"x"}]}`, sess)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid executor: got %d, want 400", resp.StatusCode)
	}
	var errBody map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&errBody)
	if errBody["error"] != "invalid_executor_profile" {
		t.Fatalf("error = %v, want invalid_executor_profile", errBody["error"])
	}
}
