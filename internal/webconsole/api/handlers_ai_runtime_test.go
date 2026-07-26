package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/airuntime"
	airuntimesql "github.com/oopslink/agent-center/internal/airuntime/sqlite"
)

func TestAIRuntimeCatalogHTTPFlowAndPermissions(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	n := 0
	deps.RuntimeCatalog = airuntime.NewService(airuntimesql.NewRepository(db), func() string { n++; return fmt.Sprintf("runtime-%d", n) })
	owner := setupTestSession(t, db, deps)
	server := newTestServer(t, deps)
	defer server.Close()

	resp := orgScopedPost(t, server.URL+"/api/ai-runtime/models", `{"expected_revision":0,"value":{"key":"gpt-5","model_key":"gpt-5","display_name":"GPT-5","compatible_cli_keys":["codex"],"default_parameters":{},"enabled":true}}`, owner)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create model status=%d", resp.StatusCode)
	}
	var modelResult struct {
		Revision int64                     `json:"revision"`
		Entry    airuntime.ModelDefinition `json:"entry"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&modelResult)
	resp.Body.Close()

	resp = orgScopedPost(t, server.URL+"/api/ai-runtime/profiles", fmt.Sprintf(`{"expected_revision":%d,"value":{"key":"default-coding","name":"Default coding","cli_key":"codex","model_key":"%s","parameters":{},"enabled":true}}`, modelResult.Revision, modelResult.Entry.Key), owner)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create profile status=%d", resp.StatusCode)
	}
	var profileResult struct {
		Revision int64                    `json:"revision"`
		Entry    airuntime.RuntimeProfile `json:"entry"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&profileResult)
	resp.Body.Close()

	url := orgScopedURL(server.URL+"/api/ai-runtime/default-profile", owner.OrgSlug)
	req, _ := http.NewRequest(http.MethodPut, url, strings.NewReader(fmt.Sprintf(`{"expected_revision":%d,"profile_id":"%s"}`, profileResult.Revision, profileResult.Entry.ID)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(owner.Cookie)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set default status=%d", resp.StatusCode)
	}
	resp.Body.Close()

	member := memberSessionInOrg(t, db, owner.OrgID, owner.OrgSlug)
	resp = orgScopedGet(t, server.URL+"/api/ai-runtime", member)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("member read status=%d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = orgScopedPost(t, server.URL+"/api/ai-runtime/clis", `{"expected_revision":3,"value":{"key":"custom","display_name":"Custom","executable":"custom","enabled":true}}`, member)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member write status=%d want 403", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAIRuntimeProfileInvalidParametersReturnsDiagnosticRuntimeError(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	n := 0
	deps.RuntimeCatalog = airuntime.NewService(airuntimesql.NewRepository(db), func() string { n++; return fmt.Sprintf("runtime-%d", n) })
	owner := setupTestSession(t, db, deps)
	server := newTestServer(t, deps)
	defer server.Close()

	schema := `{"type":"object","properties":{"label":{"type":"string","minLength":3}},"additionalProperties":false}`
	resp := orgScopedPost(t, server.URL+"/api/ai-runtime/clis", fmt.Sprintf(`{"expected_revision":0,"value":{"key":"custom","display_name":"Custom","executable":"custom","parameter_schema":%s,"enabled":true}}`, schema), owner)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create cli status=%d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = orgScopedPost(t, server.URL+"/api/ai-runtime/models", `{"expected_revision":1,"value":{"key":"custom-model","model_key":"custom-model","display_name":"Custom Model","compatible_cli_keys":["custom"],"default_parameters":{"label":"safe"},"enabled":true}}`, owner)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create model status=%d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = orgScopedPost(t, server.URL+"/api/ai-runtime/profiles", `{"expected_revision":2,"value":{"key":"bad-profile","name":"Bad Profile","cli_key":"custom","model_key":"custom-model","parameters":{"label":"x"},"enabled":true}}`, owner)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("create invalid profile status=%d want 400", resp.StatusCode)
	}
	var runtimeErr airuntime.Error
	if err := json.NewDecoder(resp.Body).Decode(&runtimeErr); err != nil {
		t.Fatal(err)
	}
	if runtimeErr.Reason != airuntime.ReasonParametersInvalid {
		t.Fatalf("reason=%q want %q body=%+v", runtimeErr.Reason, airuntime.ReasonParametersInvalid, runtimeErr)
	}
	if runtimeErr.Details["error"] == "" {
		t.Fatalf("missing diagnostic details: %#v", runtimeErr.Details)
	}
}
