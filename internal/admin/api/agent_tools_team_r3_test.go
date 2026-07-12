package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/oopslink/agent-center/internal/cognition/memory"
	"github.com/oopslink/agent-center/internal/cognition/memory/centergit"
	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/team"
)

// wireTeamIdentity seeds an org owner identity/member and wires the identity
// provision service + member repo onto the fixture deps, so instantiate_team builds
// REAL agent identities (design §6/§8). Must be called BEFORE wireTeam (which snapshots
// f.deps into the httptest server). Returns the identity repo for row assertions.
func wireTeamIdentity(t *testing.T, f *writeToolsFixture) *identity.SQLiteIdentityRepo {
	t.Helper()
	ctx := context.Background()
	idRepo := identity.NewSQLiteIdentityRepo(f.db)
	memRepo := identity.NewSQLiteMemberRepo(f.db)

	// Seed an owner (user identity + owner member) in the test org so orgProvisioner
	// finds a valid owner/admin provisioner.
	ownerIdn, err := identity.IdentityFactory{}.NewUser("Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := idRepo.Save(ctx, ownerIdn); err != nil {
		t.Fatal(err)
	}
	ownerMember, err := identity.MemberFactory{}.New(atTestOrg, ownerIdn.ID(), identity.RoleOwner, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := memRepo.Save(ctx, ownerMember); err != nil {
		t.Fatal(err)
	}

	f.deps.TeamIdentityProvisionSvc = identity.NewAgentIdentityProvisionService(f.db, idRepo, memRepo)
	f.deps.TeamMemberRepo = memRepo
	return idRepo
}

func countAgentIdentities(t *testing.T, f *writeToolsFixture) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM identities WHERE kind='agent'`).Scan(&n); err != nil {
		t.Fatalf("count agent identities: %v", err)
	}
	return n
}

// TestExtractFromTeam_SkipsNonStandardEntries proves the R2-REJECT crash (a) is
// fixed: a member pushing a NON-STANDARD file (no frontmatter) into the team's
// memory repo no longer 500s the whole extract. The extract succeeds, drops the
// stray file, and flags skipped_nonstandard.
func TestExtractFromTeam_SkipsNonStandardEntries(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv, gitHost := wireTeam(t, f)
	ctx := t.Context()

	st, body := postBearer(t, srv.URL, "/admin/agent-tools/create_team", "acat_w1", map[string]any{
		"agent_id": atAgent1, "name": "team-stray", "roles": []map[string]any{{"role": "dev"}},
	})
	if st != http.StatusCreated {
		t.Fatalf("create_team status=%d body=%v", st, body)
	}
	teamID, _ := body["id"].(string)

	// One good portable entry so the extract still has content.
	if _, err := centergit.NewTeamMemoryProducer(gitHost, nil).SeedTeam(ctx, teamID, []centergit.Entry{
		{Slug: "prefer-tests", Description: "write tests first", Body: "TDD", Type: string(team.ExpScopeTeam)},
	}); err != nil {
		t.Fatalf("SeedTeam: %v", err)
	}
	// Now push a NON-STANDARD file (no frontmatter) directly into the bare repo, as a
	// member could. Before the fix this made extract_from_team 500.
	pushStrayFile(t, ctx, gitHost, teamID, "entries/stray.md", "i am just a note, no frontmatter\n")

	st, body = postBearer(t, srv.URL, "/admin/agent-tools/extract_from_team", "acat_w1", map[string]any{
		"agent_id": atAgent1, "team_id": teamID,
	})
	if st != http.StatusOK {
		t.Fatalf("extract_from_team status=%d body=%v — want 200 (NOT 500) despite the stray file", st, body)
	}
	if sk, _ := body["skipped_nonstandard"].(float64); int(sk) != 1 {
		t.Fatalf("skipped_nonstandard=%v want 1 (the stray file flagged)", body["skipped_nonstandard"])
	}
	draft, _ := body["draft"].(map[string]any)
	if ec, _ := draft["experience_count"].(float64); int(ec) != 1 {
		t.Fatalf("draft experience_count=%v want 1 (the good entry survived)", draft["experience_count"])
	}
}

// pushStrayFile writes a file directly into a team's bare repo history via a
// throwaway clone → commit → push (branch main), simulating a member pushing a
// non-standard file into the shared team memory repo.
func pushStrayFile(t *testing.T, ctx context.Context, host *centergit.Host, teamID, relPath, content string) {
	t.Helper()
	bareDir, err := host.RepoDir(centergit.TeamRepo(teamID))
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	runner := memory.NewExecGitRunner()
	env := append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"HOME="+work, "XDG_CONFIG_HOME="+work,
		"GIT_AUTHOR_NAME=member", "GIT_COMMITTER_NAME=member",
		"GIT_AUTHOR_EMAIL=member@example.com", "GIT_COMMITTER_EMAIL=member@example.com",
	)
	repoDir := filepath.Join(work, "clone")
	if out, err := runner.Run(ctx, work, env, "clone", bareDir, repoDir); err != nil {
		t.Fatalf("clone: %v: %s", err, out)
	}
	abs := filepath.Join(repoDir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := runner.Run(ctx, repoDir, env, "add", "-A"); err != nil {
		t.Fatalf("add: %v: %s", err, out)
	}
	if out, err := runner.Run(ctx, repoDir, env, "-c", "commit.gpgsign=false", "commit", "-m", "stray"); err != nil {
		t.Fatalf("commit: %v: %s", err, out)
	}
	if out, err := runner.Run(ctx, repoDir, env, "push", "origin", "HEAD:main"); err != nil {
		t.Fatalf("push: %v: %s", err, out)
	}
}

// TestExportImportCuration proves fix (b): export enforces the curation gate, curate
// unlocks it, the exported document round-trips through import into the caller's org.
func TestExportImportCuration(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv, _ := wireTeam(t, f)

	tmpl := map[string]any{
		"name":  "shareable",
		"roles": []map[string]any{{"role": "dev", "count": 2}},
		"experiences": []map[string]any{
			{"slug": "prefer-tdd", "description": "tests first", "scope": "team", "body": "TDD"},
		},
	}

	// export WITHOUT curation → refused (409 template_not_curated). Proves the gate
	// (ErrTemplateNotCurated) is LIVE, not dead code.
	st, body := postBearer(t, srv.URL, "/admin/agent-tools/export_team_template", "acat_w1", map[string]any{
		"agent_id": atAgent1, "template": tmpl, "curated": false,
	})
	if st != http.StatusConflict {
		t.Fatalf("export un-curated status=%d want 409 body=%v", st, body)
	}
	if code, _ := body["error"].(string); code != "template_not_curated" {
		t.Fatalf("export un-curated error=%v want template_not_curated", body["error"])
	}

	// curate → marks it curated.
	st, body = postBearer(t, srv.URL, "/admin/agent-tools/curate_team_template", "acat_w1", map[string]any{
		"agent_id": atAgent1, "template": tmpl,
	})
	if st != http.StatusOK {
		t.Fatalf("curate status=%d body=%v", st, body)
	}
	if cur, _ := body["curated"].(bool); !cur {
		t.Fatalf("curate did not mark curated: %v", body)
	}

	// export WITH curated=true → produces a JSON document.
	st, body = postBearer(t, srv.URL, "/admin/agent-tools/export_team_template", "acat_w1", map[string]any{
		"agent_id": atAgent1, "template": tmpl, "curated": true,
	})
	if st != http.StatusOK {
		t.Fatalf("export curated status=%d body=%v", st, body)
	}
	doc, ok := body["document"].(map[string]any)
	if !ok {
		t.Fatalf("export returned no document object: %v", body)
	}
	if doc["format"] != "team-template/v1" {
		t.Fatalf("export document format=%v", doc["format"])
	}

	// import the document → builds a fresh template in the caller's org (curated=false).
	st, body = postBearer(t, srv.URL, "/admin/agent-tools/import_team_template", "acat_w1", map[string]any{
		"agent_id": atAgent1, "document": doc,
	})
	if st != http.StatusCreated {
		t.Fatalf("import status=%d body=%v", st, body)
	}
	if name, _ := body["name"].(string); name != "shareable" {
		t.Fatalf("imported template name=%v want shareable", body["name"])
	}
	if cur, _ := body["curated"].(bool); cur {
		t.Fatalf("imported template must land un-curated (re-review before re-export)")
	}
	if roles, _ := body["roles"].([]any); len(roles) != 1 {
		t.Fatalf("imported roles=%v want 1", body["roles"])
	}

	// import with no document → 400.
	st, _ = postBearer(t, srv.URL, "/admin/agent-tools/import_team_template", "acat_w1", map[string]any{
		"agent_id": atAgent1,
	})
	if st != http.StatusBadRequest {
		t.Fatalf("import empty document status=%d want 400", st)
	}
}

// TestInstantiateTeam_BuildsRealIdentities proves fix (c): instantiate_team creates
// REAL identity entities (identities table gets real rows) rather than dangling refs,
// and the team member refs point at the real identity ids.
func TestInstantiateTeam_BuildsRealIdentities(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	idRepo := wireTeamIdentity(t, f) // BEFORE wireTeam snapshots deps
	_ = idRepo
	srv, _ := wireTeam(t, f)

	if before := countAgentIdentities(t, f); before != 0 {
		t.Fatalf("agent identities before instantiate=%d want 0", before)
	}

	tmpl := map[string]any{
		"name":  "squad-tmpl",
		"roles": []map[string]any{{"role": "dev", "count": 2}, {"role": "qa", "count": 1}},
		"experiences": []map[string]any{
			{"slug": "prefer-tdd", "description": "tests first", "scope": "team", "body": "TDD"},
		},
	}
	st, body := postBearer(t, srv.URL, "/admin/agent-tools/instantiate_team", "acat_w1", map[string]any{
		"agent_id": atAgent1, "project_id": "proj-9", "team_name": "squad-run", "template": tmpl,
	})
	if st != http.StatusCreated {
		t.Fatalf("instantiate_team status=%d body=%v", st, body)
	}
	// 3 agents (2 dev + 1 qa) → 3 real identity rows.
	if ic, _ := body["identities_created"].(float64); int(ic) != 3 {
		t.Fatalf("identities_created=%v want 3", body["identities_created"])
	}
	if got := countAgentIdentities(t, f); got != 3 {
		t.Fatalf("identities table has %d agent rows, want 3 (non-dangling)", got)
	}

	// Each agents[].agent_id is a REAL identity id ("agent-...") and the member_ref
	// points at it — no dangling minted refs.
	agents, _ := body["agents"].([]any)
	if len(agents) != 3 {
		t.Fatalf("agents=%d want 3", len(agents))
	}
	for _, ai := range agents {
		am, _ := ai.(map[string]any)
		agentID, _ := am["agent_id"].(string)
		idn, err := idRepo.GetByID(t.Context(), agentID)
		if err != nil || idn == nil {
			t.Fatalf("agent_id %q is not a real identity row: %v", agentID, err)
		}
		if ref, _ := am["member_ref"].(string); ref != "agent:"+agentID {
			t.Fatalf("member_ref=%q want agent:%s", ref, agentID)
		}
	}
}
