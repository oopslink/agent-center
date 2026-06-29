package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/coderepo"
	"github.com/oopslink/agent-center/internal/coderepo/provider"
	coderepservice "github.com/oopslink/agent-center/internal/coderepo/service"
	coderepsql "github.com/oopslink/agent-center/internal/coderepo/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	"github.com/oopslink/agent-center/internal/secretmgmt"
)

// fakeRepoProvider returns canned remote data for the live get_repo_info path.
type fakeRepoProvider struct{}

func (fakeRepoProvider) ListCommits(_ context.Context, _ provider.Target, _ string, _ int) ([]provider.Commit, error) {
	return []provider.Commit{{SHA: "abc123", Message: "fix", Author: "Ada"}}, nil
}
func (fakeRepoProvider) ListBranches(_ context.Context, _ provider.Target) ([]provider.Branch, error) {
	return []provider.Branch{{Name: "main", IsDefault: true}}, nil
}

// wireCodeRepo attaches a CodeRepo service (fake provider + fresh key) to the
// fixture deps and returns it. Must be called before f.server(t).
func wireCodeRepo(t *testing.T, f *writeToolsFixture) *coderepservice.Service {
	t.Helper()
	mk, _ := secretmgmt.GenerateMasterKey()
	svc := coderepservice.New(coderepservice.Deps{
		DB: f.db, Repos: coderepsql.NewRepoRepo(f.db), IDGen: idgen.NewGenerator(clock.SystemClock{}),
		Clock: clock.SystemClock{}, MasterKey: mk, Providers: fakeRepoProvider{},
	})
	f.deps.CodeRepoSvc = svc
	return svc
}

// seedProjectWithRepo creates a project (AG1 a member), a workspace repo (with a
// credential), and a primary reference. Returns project + repo ids.
func seedProjectWithRepo(t *testing.T, f *writeToolsFixture, svc *coderepservice.Service) (string, string) {
	t.Helper()
	ctx := context.Background()
	owner := pm.IdentityRef("user:owner")
	pid, err := f.pmSvc.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: atTestOrg, Name: "Acme", CreatedBy: owner})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pmSvc.AddProjectMember(ctx, pmservice.AddProjectMemberCommand{
		ProjectID: pid, IdentityID: pm.IdentityRef("agent:" + atAgent1), Role: pm.RoleMember, Actor: owner,
	}); err != nil {
		t.Fatal(err)
	}
	repoID, err := svc.CreateRepo(ctx, coderepservice.CreateRepoCommand{
		OrgID: atTestOrg, Label: "app", Description: "the app", URL: "https://github.com/o/app",
		Provider: coderepo.ProviderGitHub, DefaultBranch: "main", Credential: "ghp_secret", CreatedBy: "user:owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pmSvc.AddCodeRepoReference(ctx, pmservice.AddCodeRepoReferenceCommand{
		ProjectID: pid, RepoID: repoID, IsPrimary: true, Actor: owner,
	}); err != nil {
		t.Fatal(err)
	}
	return string(pid), repoID
}

func jsonStr(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestListProjectRepos_ResolvesAndHidesCredential(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	svc := wireCodeRepo(t, f)
	pid, repoID := seedProjectWithRepo(t, f, svc)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/list_project_repos", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": pid})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%v", status, body)
	}
	repos, _ := body["repos"].([]any)
	if len(repos) != 1 {
		t.Fatalf("repos len = %d, want 1: %v", len(repos), body)
	}
	r0, _ := repos[0].(map[string]any)
	if r0["repo_id"] != repoID || r0["label"] != "app" || r0["provider"] != "github" || r0["default_branch"] != "main" || r0["is_primary"] != true {
		t.Errorf("repo entry = %v", r0)
	}
	// Credential must NEVER appear anywhere in the payload.
	if strings.Contains(jsonStr(body), "ghp_secret") {
		t.Fatal("list_project_repos leaked the credential")
	}
}

func TestGetRepoInfo_PrimaryAndLive(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	svc := wireCodeRepo(t, f)
	pid, _ := seedProjectWithRepo(t, f, svc)
	srv := f.server(t)

	// No repo_id → resolves the project's PRIMARY; live=true attaches remote data.
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_repo_info", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": pid, "live": true})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%v", status, body)
	}
	if body["provider"] != "github" || body["is_primary"] != true {
		t.Errorf("static info = %v", body)
	}
	live, ok := body["live"].(map[string]any)
	if !ok {
		t.Fatalf("missing live view: %v", body)
	}
	commits, _ := live["commits"].([]any)
	branches, _ := live["branches"].([]any)
	if len(commits) != 1 || len(branches) != 1 {
		t.Errorf("live commits=%d branches=%d, want 1/1", len(commits), len(branches))
	}
	if strings.Contains(jsonStr(body), "ghp_secret") {
		t.Fatal("get_repo_info leaked the credential")
	}
}

func TestGetRepoInfo_StaticDefault_NoLive(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	svc := wireCodeRepo(t, f)
	pid, _ := seedProjectWithRepo(t, f, svc)
	srv := f.server(t)

	// Default (live omitted) → no remote fetch, just static info (cheap path).
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_repo_info", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": pid})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if _, hasLive := body["live"]; hasLive {
		t.Errorf("static call should NOT include a live view: %v", body)
	}
}

func TestListProjectRepos_NonMember_Forbidden(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	wireCodeRepo(t, f)
	srv := f.server(t)
	// A project AG1 is NOT a member of → membership gate 403.
	ctx := context.Background()
	pid, err := f.pmSvc.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: atTestOrg, Name: "Other", CreatedBy: pm.IdentityRef("user:owner")})
	if err != nil {
		t.Fatal(err)
	}
	status, _ := postBearer(t, srv.URL, "/admin/agent-tools/list_project_repos", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid)})
	if status != http.StatusForbidden {
		t.Fatalf("non-member status = %d, want 403", status)
	}
}
