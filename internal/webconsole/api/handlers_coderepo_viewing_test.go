package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/coderepo/provider"
	coderepservice "github.com/oopslink/agent-center/internal/coderepo/service"
	coderepsql "github.com/oopslink/agent-center/internal/coderepo/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
	"github.com/oopslink/agent-center/internal/secretmgmt"
)

// fakeViewProvider returns canned commits/branches and captures the credential it
// received (to assert the server decrypted + used it, but never returned it).
type fakeViewProvider struct{ gotCredential string }

func (f *fakeViewProvider) ListCommits(_ context.Context, t provider.Target, _ string, _ int) ([]provider.Commit, error) {
	f.gotCredential = t.Credential
	return []provider.Commit{{SHA: "abc123", Message: "fix bug", Author: "Ada"}}, nil
}

func (f *fakeViewProvider) ListBranches(_ context.Context, t provider.Target) ([]provider.Branch, error) {
	f.gotCredential = t.Credential
	return []provider.Branch{{Name: "main", CommitSHA: "abc123", IsDefault: true}}, nil
}

// v2.18.4 BE-2 HTTP: remote viewing (commits/branches) — member-readable, via the
// provider abstraction; credential used server-side, never returned.
func TestAPI_CodeRepo_BE2_RemoteViewing(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	// Rebuild the CodeRepo service with a fake provider + a fixed key (so the repo we
	// create and then view share one service instance / key).
	fp := &fakeViewProvider{}
	mk, _ := secretmgmt.GenerateMasterKey()
	deps.CodeRepoSvc = coderepservice.New(coderepservice.Deps{
		DB: db, Repos: coderepsql.NewRepoRepo(db), IDGen: idgen.NewGenerator(clock.SystemClock{}),
		Clock: clock.SystemClock{}, MasterKey: mk, Unlinker: pmsql.NewCodeRepoRefRepo(db), Providers: fp,
	})
	owner := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	// Create a private repo (with credential).
	resp := orgScopedPost(t, s.URL+"/api/code-repos",
		`{"label":"app","url":"https://github.com/o/app","provider":"github","default_branch":"main","credential":"ghp_secret"}`, owner)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create repo: %d", resp.StatusCode)
	}
	var repo map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&repo)
	repoID, _ := repo["id"].(string)

	// Commits (member-readable).
	resp = orgScopedGet(t, s.URL+"/api/code-repos/"+repoID+"/commits?branch=main&limit=10", owner)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("commits: %d", resp.StatusCode)
	}
	bodyB, _ := io.ReadAll(resp.Body)
	body := string(bodyB)
	var commits struct {
		Commits []map[string]any `json:"commits"`
		Branch  string           `json:"branch"`
		Source  string           `json:"source"`
	}
	_ = json.Unmarshal([]byte(body), &commits)
	if len(commits.Commits) != 1 || commits.Commits[0]["sha"] != "abc123" {
		t.Fatalf("commits = %v", commits.Commits)
	}
	// FE-locked shape: {sha,message,author,date} + branch + source.
	if commits.Commits[0]["author"] != "Ada" || commits.Commits[0]["message"] != "fix bug" {
		t.Errorf("commit shape = %v", commits.Commits[0])
	}
	if _, hasDate := commits.Commits[0]["date"]; !hasDate {
		t.Errorf("commit missing 'date' field: %v", commits.Commits[0])
	}
	if commits.Branch != "main" || commits.Source != "github" {
		t.Errorf("branch=%q source=%q, want main/github", commits.Branch, commits.Source)
	}
	// The credential must NEVER appear in the response payload.
	if strings.Contains(body, "ghp_secret") {
		t.Fatal("commits response leaked the credential")
	}

	// Branches.
	resp = orgScopedGet(t, s.URL+"/api/code-repos/"+repoID+"/branches", owner)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("branches: %d", resp.StatusCode)
	}
	bodyB, _ = io.ReadAll(resp.Body)
	body = string(bodyB)
	if !strings.Contains(body, `"main"`) || !strings.Contains(body, `"is_default":true`) {
		t.Fatalf("branches body = %s", body)
	}
	if strings.Contains(body, "ghp_secret") {
		t.Fatal("branches response leaked the credential")
	}

	// The provider DID receive the decrypted credential server-side.
	if fp.gotCredential != "ghp_secret" {
		t.Errorf("provider got credential %q, want decrypted ghp_secret", fp.gotCredential)
	}

	// Unknown repo → 404 (not leaked across the org boundary).
	resp = orgScopedGet(t, s.URL+"/api/code-repos/repo-nope/commits", owner)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown repo commits = %d, want 404", resp.StatusCode)
	}
}
