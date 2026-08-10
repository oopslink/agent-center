package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/oopslink/agent-center/internal/airuntime"
	airuntimesql "github.com/oopslink/agent-center/internal/airuntime/sqlite"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

func wireRuntimeCatalogForTest(t *testing.T, db *sql.DB, deps *HandlerDeps, orgID string) {
	t.Helper()
	n := 0
	deps.RuntimeCatalog = airuntime.NewService(airuntimesql.NewRepository(db), func() string {
		n++
		return fmt.Sprintf("runtime-contract-%d", n)
	})
	ctx := context.Background()
	actor := "user:test"
	catalog, err := deps.RuntimeCatalog.Catalog(ctx, orgID)
	if err != nil {
		t.Fatal(err)
	}
	_, rev, err := deps.RuntimeCatalog.CreateModel(ctx, orgID, actor, catalog.Revision, airuntime.ModelDefinition{
		Key: "claude-opus-4-8", ModelKey: "claude-opus-4-8", DisplayName: "Claude Opus 4.8",
		CompatibleCLIKeys: []string{"claude-code"}, DefaultParameters: map[string]any{}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, rev, err = deps.RuntimeCatalog.CreateModel(ctx, orgID, actor, rev, airuntime.ModelDefinition{
		Key: "claude-sonnet-4-6", ModelKey: "claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6",
		CompatibleCLIKeys: []string{"claude-code"}, DefaultParameters: map[string]any{}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, rev, err = deps.RuntimeCatalog.CreateModel(ctx, orgID, actor, rev, airuntime.ModelDefinition{
		Key: "gpt-5", ModelKey: "gpt-5", DisplayName: "GPT-5",
		CompatibleCLIKeys: []string{"codex"}, DefaultParameters: map[string]any{}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAPI_RuntimeContract_AgentSelections(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	wireRuntimeCatalogForTest(t, db, &deps, sess.OrgID)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"bad","model":"gpt-5","cli":"claude-code","worker_id":"w-1"}`, sess)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid create status=%d, want 400", resp.StatusCode)
	}
	var runtimeErr airuntime.Error
	_ = json.NewDecoder(resp.Body).Decode(&runtimeErr)
	resp.Body.Close()
	if runtimeErr.Reason != airuntime.ReasonIncompatible {
		t.Fatalf("invalid create reason=%q, want %q", runtimeErr.Reason, airuntime.ReasonIncompatible)
	}

	resp = orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"ok","cli":"claude-code","worker_id":"w-1"}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("defaulted create status=%d, want 201", resp.StatusCode)
	}
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	id, _ := created["identity_id"].(string)
	a, err := deps.AgentSvc.ResolveAgent(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if a.Profile().CLI != "claude-code" || a.Profile().Model != "claude-opus-4-8" {
		t.Fatalf("defaulted runtime = %s/%s", a.Profile().CLI, a.Profile().Model)
	}

	resp = orgScopedPatch(t, s.URL+"/api/agents/"+id+"/config",
		`{"model":"claude-opus-4-8","cli":"claude-code","allowed_executors":[{"cli":"codex","model":"claude-opus-4-8"}]}`, sess)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid executor status=%d, want 400", resp.StatusCode)
	}
	_ = json.NewDecoder(resp.Body).Decode(&runtimeErr)
	resp.Body.Close()
	if runtimeErr.Reason != airuntime.ReasonIncompatible {
		t.Fatalf("invalid executor reason=%q, want %q", runtimeErr.Reason, airuntime.ReasonIncompatible)
	}

	resp = orgScopedPatch(t, s.URL+"/api/agents/"+id+"/config",
		`{"model":"claude-opus-4-8","cli":"claude-code","orchestrator_model":"not-a-runtime-model"}`, sess)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid model-only agent config status=%d, want 400", resp.StatusCode)
	}
	_ = json.NewDecoder(resp.Body).Decode(&runtimeErr)
	resp.Body.Close()
	if runtimeErr.Reason != airuntime.ReasonModelNotFound {
		t.Fatalf("invalid model-only agent config reason=%q, want %q", runtimeErr.Reason, airuntime.ReasonModelNotFound)
	}

	pid, err := deps.PM.CreateProject(context.Background(), pmservice.CreateProjectCommand{
		OrganizationID: sess.OrgID,
		Name:           "Runtime Project",
		CreatedBy:      pm.IdentityRef("user:" + sess.IdentityID),
	})
	if err != nil {
		t.Fatal(err)
	}
	resp = orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/tasks",
		`{"title":"bad runtime override","model":"not-a-runtime-model"}`, sess)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid task model status=%d, want 400", resp.StatusCode)
	}
	_ = json.NewDecoder(resp.Body).Decode(&runtimeErr)
	resp.Body.Close()
	if runtimeErr.Reason != airuntime.ReasonModelNotFound {
		t.Fatalf("invalid task model reason=%q, want %q", runtimeErr.Reason, airuntime.ReasonModelNotFound)
	}

	resp = orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/tasks",
		`{"title":"ok runtime override","model":"claude-sonnet-4-6"}`, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("valid task model status=%d, want 200", resp.StatusCode)
	}
	var task map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&task)
	resp.Body.Close()
	if got := task["model"]; got != "claude-sonnet-4-6" {
		t.Fatalf("task model=%v, want claude-sonnet-4-6", got)
	}
}

func TestAPI_RuntimeContract_TeamRoles(t *testing.T) {
	deps, db, sess := setupTeamsAPI(t)
	wireRuntimeCatalogForTest(t, db, &deps, sess.OrgID)
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedPost(t, s.URL+"/api/teams",
		`{"name":"Bad Squad","description":"","visibility":"org-private","roles":[{"role":"impl","cli":"codex","model":"claude-opus-4-8","max_concurrency":1,"count":1,"tags":""}]}`, sess)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid team role status=%d, want 400", resp.StatusCode)
	}
	var runtimeErr airuntime.Error
	_ = json.NewDecoder(resp.Body).Decode(&runtimeErr)
	resp.Body.Close()
	if runtimeErr.Reason != airuntime.ReasonIncompatible {
		t.Fatalf("invalid team role reason=%q, want %q", runtimeErr.Reason, airuntime.ReasonIncompatible)
	}

	resp = orgScopedPost(t, s.URL+"/api/teams",
		`{"name":"Good Squad","description":"","visibility":"org-private","roles":[{"role":"impl","cli":"codex","model":"gpt-5","max_concurrency":1,"count":1,"tags":""}]}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("valid team role status=%d, want 201", resp.StatusCode)
	}
	resp.Body.Close()
}
