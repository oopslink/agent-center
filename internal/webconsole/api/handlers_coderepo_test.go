package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// v2.18.4 BE-1 HTTP: workspace Repos CRUD (org-admin gated, credential masked) +
// project references (member gated, set-primary) + strong-delete unref count.
func TestAPI_CodeRepo_BE1_RoundTrip(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	owner := setupTestSession(t, db, deps) // RoleOwner (admin)
	s := newTestServer(t, deps)
	defer s.Close()

	// Create a workspace repo WITH a credential (admin).
	resp := orgScopedPost(t, s.URL+"/api/code-repos",
		`{"label":"app","url":"https://github.com/o/app","provider":"github","credential":"ghp_secret"}`, owner)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create repo: %d", resp.StatusCode)
	}
	var repo map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&repo)
	repoID, _ := repo["id"].(string)
	if repo["has_credential"] != true {
		t.Fatalf("has_credential = %v, want true", repo["has_credential"])
	}
	// The plaintext credential must NEVER appear in the response.
	if _, leaked := repo["credential"]; leaked {
		t.Fatal("response must NOT include the credential")
	}

	// List shows it.
	resp = orgScopedGet(t, s.URL+"/api/code-repos", owner)
	var list struct {
		Repos []map[string]any `json:"repos"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&list)
	if len(list.Repos) != 1 {
		t.Fatalf("list len = %d, want 1", len(list.Repos))
	}

	// Permission gate: a non-admin member cannot create a workspace repo.
	member := memberSessionInOrg(t, db, owner.OrgID, owner.OrgSlug)
	resp = orgScopedPost(t, s.URL+"/api/code-repos",
		`{"label":"x","url":"u","provider":"git"}`, member)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member create repo = %d, want 403", resp.StatusCode)
	}

	// Project reference (member-gated): create a project, reference the repo as primary.
	pid, err := deps.PM.CreateProject(context.Background(), pmservice.CreateProjectCommand{
		OrganizationID: owner.OrgID, Name: "Acme", CreatedBy: pm.IdentityRef("user:" + owner.IdentityID),
	})
	if err != nil {
		t.Fatal(err)
	}
	resp = orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/code-repos",
		`{"repo_id":"`+repoID+`","is_primary":true}`, owner)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add project ref: %d", resp.StatusCode)
	}
	var ref map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&ref)
	if ref["repo_id"] != repoID || ref["is_primary"] != true {
		t.Fatalf("ref = %v, want repo_id=%s is_primary=true", ref, repoID)
	}

	// Delete the repo (admin) → strong-delete unrefs the 1 project.
	resp = orgScopedDelete(t, s.URL+"/api/code-repos/"+repoID, owner)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete repo: %d", resp.StatusCode)
	}
	var del map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&del)
	if n, _ := del["unlinked_projects"].(float64); n != 1 {
		t.Fatalf("unlinked_projects = %v, want 1", del["unlinked_projects"])
	}
	// Repo gone.
	resp = orgScopedGet(t, s.URL+"/api/code-repos", owner)
	_ = json.NewDecoder(resp.Body).Decode(&list)
	if len(list.Repos) != 0 {
		t.Fatalf("after delete list len = %d, want 0", len(list.Repos))
	}
}
