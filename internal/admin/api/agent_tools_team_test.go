package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oopslink/agent-center/internal/cognition/memory/centergit"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/team"
	teamservice "github.com/oopslink/agent-center/internal/team/service"
	teamsql "github.com/oopslink/agent-center/internal/team/sqlite"
)

// wireTeam attaches a live team service + git host to the fixture deps and
// returns an httptest server mounting the FULL route surface (team tools +
// /admin/git/) behind the same bearer-auth middleware the production wiring uses.
// gitHost is returned so tests can provision repos directly.
func wireTeam(t *testing.T, f *writeToolsFixture) (*httptest.Server, *centergit.Host) {
	t.Helper()
	gen := idgen.NewGenerator(f.clk)
	f.deps.TeamSvc = teamservice.New(teamsql.NewRepo(f.db), f.db, gen, f.clk)
	f.deps.TeamIDGen = gen
	gitHost := centergit.NewHost(t.TempDir(), nil)
	f.deps.TeamGitHost = gitHost

	gitHandler, err := NewGitHandler(gitHost, centergit.NewMapMembership())
	if err != nil {
		t.Skipf("git smart-HTTP unavailable in this environment: %v", err)
	}
	srv := NewServerWithDeps("", ServerDeps{GitHandler: gitHandler})
	h := AuthMiddleware(f.verifier)(WithDeps(f.deps)(srv.Handler()))
	httpsrv := httptest.NewServer(h)
	t.Cleanup(httpsrv.Close)
	return httpsrv, gitHost
}

// TestTeamTools_CRUDAndInstantiate proves the Team agent-tool surface is LIVE
// (not a 404): create_team persists a team with its declared roles, membership +
// project association round-trip, and instantiate_team expands a template into a
// real team + agent members and reports the runtime-provisioning plan.
func TestTeamTools_CRUDAndInstantiate(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv, _ := wireTeam(t, f)

	// create_team — the T1004 acceptance endpoint. NON-404.
	st, body := postBearer(t, srv.URL, "/admin/agent-tools/create_team", "acat_w1", map[string]any{
		"agent_id": atAgent1, "name": "team-alpha", "description": "the alpha team",
		"roles": []map[string]any{{"role": "dev", "cli": "claude-code", "max_concurrency": 2}, {"role": "reviewer"}},
	})
	if st != http.StatusCreated {
		t.Fatalf("create_team status=%d body=%v", st, body)
	}
	teamID, _ := body["id"].(string)
	if teamID == "" {
		t.Fatalf("create_team returned no id: %v", body)
	}

	// get_team round-trips the roles.
	st, body = postBearer(t, srv.URL, "/admin/agent-tools/get_team", "acat_w1", map[string]any{"agent_id": atAgent1, "team_id": teamID})
	if st != http.StatusOK {
		t.Fatalf("get_team status=%d body=%v", st, body)
	}
	if roles, _ := body["roles"].([]any); len(roles) != 2 {
		t.Fatalf("get_team roles=%v want 2", body["roles"])
	}

	// add_member under a declared role.
	st, body = postBearer(t, srv.URL, "/admin/agent-tools/add_member", "acat_w1", map[string]any{
		"agent_id": atAgent1, "team_id": teamID, "member_ref": "agent:worker-bee", "role": "dev",
	})
	if st != http.StatusCreated {
		t.Fatalf("add_member status=%d body=%v", st, body)
	}
	// undeclared role → 400.
	st, _ = postBearer(t, srv.URL, "/admin/agent-tools/add_member", "acat_w1", map[string]any{
		"agent_id": atAgent1, "team_id": teamID, "member_ref": "user:alice", "role": "ghost",
	})
	if st != http.StatusBadRequest {
		t.Fatalf("add_member undeclared-role status=%d want 400", st)
	}

	// associate_project.
	st, _ = postBearer(t, srv.URL, "/admin/agent-tools/associate_project", "acat_w1", map[string]any{
		"agent_id": atAgent1, "team_id": teamID, "project_id": "proj-1",
	})
	if st != http.StatusOK {
		t.Fatalf("associate_project status=%d", st)
	}

	// assign_roles resolves a node's role to the team member off the roster.
	st, body = postBearer(t, srv.URL, "/admin/agent-tools/assign_roles", "acat_w1", map[string]any{
		"agent_id": atAgent1, "team_id": teamID,
		"requests": []map[string]any{{"node_key": "n1", "role": "dev"}},
	})
	if st != http.StatusOK {
		t.Fatalf("assign_roles status=%d body=%v", st, body)
	}
	assigns, _ := body["assignments"].([]any)
	if len(assigns) != 1 {
		t.Fatalf("assign_roles assignments=%v want 1", body["assignments"])
	}

	// create_team_template authors + validates a template (non-404, 201).
	tmpl := map[string]any{
		"agent_id": atAgent1, "name": "squad-tmpl",
		"roles": []map[string]any{{"role": "dev", "count": 2}, {"role": "qa", "count": 1}},
		"experiences": []map[string]any{
			{"slug": "prefer-tdd", "description": "write tests first", "scope": "team", "body": "always TDD"},
			{"slug": "secret-fact", "description": "proj only", "scope": "project"}, // dropped on instantiate seed
		},
	}
	st, body = postBearer(t, srv.URL, "/admin/agent-tools/create_team_template", "acat_w1", tmpl)
	if st != http.StatusCreated {
		t.Fatalf("create_team_template status=%d body=%v", st, body)
	}

	// instantiate_team expands the template into the org (project-independent,
	// issue-c4dccae0): 2 dev + 1 qa = 3 agents.
	st, body = postBearer(t, srv.URL, "/admin/agent-tools/instantiate_team", "acat_w1", map[string]any{
		"agent_id": atAgent1, "team_name": "squad-run", "template": tmpl,
	})
	if st != http.StatusCreated {
		t.Fatalf("instantiate_team status=%d body=%v", st, body)
	}
	agents, _ := body["agents"].([]any)
	if len(agents) != 3 {
		t.Fatalf("instantiate_team agents=%d want 3 (2 dev + 1 qa)", len(agents))
	}
	if seeded, _ := body["memory_seeded"].(bool); !seeded {
		t.Errorf("instantiate_team memory_seeded=false, want true (team repo should be seeded)")
	}
	// Only the portable (team-scope) experience seeds the memory repo.
	if n, _ := body["memory_seed_count"].(float64); int(n) != 1 {
		t.Errorf("memory_seed_count=%v want 1 (project-scope dropped)", body["memory_seed_count"])
	}
	rt, _ := body["runtime_provisioning"].(map[string]any)
	if enr, _ := rt["enrollments"].([]any); len(enr) != 3 {
		t.Errorf("runtime_provisioning enrollments=%v want 3", rt["enrollments"])
	}
}

// TestTeamTools_Unwired proves the route is MOUNTED even when the team service is
// not wired: a call returns team_not_wired (501), never a 404.
func TestTeamTools_Unwired(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv := f.server(t) // ServerDeps{} + no TeamSvc
	st, body := postBearer(t, srv.URL, "/admin/agent-tools/create_team", "acat_w1", map[string]any{"agent_id": atAgent1, "name": "x"})
	if st != http.StatusNotImplemented {
		t.Fatalf("unwired create_team status=%d body=%v, want 501 (NOT 404)", st, body)
	}
}

// TestCenterGit_InfoRefs proves /admin/git/ is live: an authenticated agent
// fetching info/refs for its own provisioned repo gets a 200 git advertisement —
// NOT a 404. It also asserts the auth spine: missing agent header → 401, and an
// unprovisioned repo → 404 (a real handler response, not an unmounted route).
func TestCenterGit_InfoRefs(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv, gitHost := wireTeam(t, f)

	// Provision atAgent1's private repo (agent bound to worker1 → resolver ok).
	if err := gitHost.EnsureRepo(t.Context(), centergit.AgentRepo(atAgent1)); err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	base := srv.URL + "/admin/git/agent/" + atAgent1 + ".git/info/refs?service=git-upload-pack"

	// Authenticated + agent header → 200 smart-HTTP advertisement.
	req, _ := http.NewRequest(http.MethodGet, base, nil)
	req.Header.Set("Authorization", "Bearer acat_w1")
	req.Header.Set("X-Agent-Id", atAgent1)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("git info/refs status=%d (want 200, NON-404) body=%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/x-git-upload-pack-advertisement" {
		t.Errorf("git info/refs content-type=%q want git advertisement", ct)
	}

	// Missing X-Agent-Id → 401 (resolver can't identify the agent).
	req2, _ := http.NewRequest(http.MethodGet, base, nil)
	req2.Header.Set("Authorization", "Bearer acat_w1")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("git info/refs no-agent status=%d want 401", resp2.StatusCode)
	}

	// Authenticated but repo not provisioned → 404 from the handler (not an
	// unmounted route — the request reached the git backend).
	req3, _ := http.NewRequest(http.MethodGet, srv.URL+"/admin/git/agent/"+atAgent2+".git/info/refs?service=git-upload-pack", nil)
	req3.Header.Set("Authorization", "Bearer acat_w1")
	req3.Header.Set("X-Agent-Id", atAgent1)
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatal(err)
	}
	resp3.Body.Close()
	// atAgent1 may not read atAgent2's repo → 403 (authz), which is also a live
	// handler response (never 404-as-unmounted). Accept 403 OR (if same) 404.
	if resp3.StatusCode != http.StatusForbidden && resp3.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-agent repo status=%d want 403 or 404 (a live handler response)", resp3.StatusCode)
	}
}

// TestExtractFromTeam_LiveDraft proves the §1/§6 headline "从活 team 抽经验草稿" is
// REACHABLE under real deployment wiring (T1019 ③): extract_from_team is NOT a 404,
// it reads a live team's center-hosted memory, keeps the portable (team/global)
// layer while DROPPING project scope, HIGHLIGHTS suspected proprietary tokens via
// the scrub pass, and returns an un-curated draft. Before this wiring the domain
// existed in template.go but no deployment surface exposed it (404).
func TestExtractFromTeam_LiveDraft(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv, gitHost := wireTeam(t, f)
	ctx := t.Context()

	// A live team with a role composition.
	st, body := postBearer(t, srv.URL, "/admin/agent-tools/create_team", "acat_w1", map[string]any{
		"agent_id": atAgent1, "name": "team-extract", "description": "extract me",
		"roles": []map[string]any{{"role": "dev", "cli": "claude-code", "max_concurrency": 2}, {"role": "reviewer"}},
	})
	if st != http.StatusCreated {
		t.Fatalf("create_team status=%d body=%v", st, body)
	}
	teamID, _ := body["id"].(string)

	// Seed the team's center-hosted memory as if agents had accumulated it: two
	// portable experiences (one naming a proprietary token, so scrub fires) and one
	// project-scope fact that MUST be dropped on extraction.
	written, err := centergit.NewTeamMemoryProducer(gitHost, nil).SeedTeam(ctx, teamID, []centergit.Entry{
		{Slug: "prefer-tests", Description: "write tests first", Body: "we learned this fixing T950 in internal/team/foo.go", Type: string(team.ExpScopeTeam)},
		{Slug: "platform-rule", Description: "a platform-wide rule", Body: "applies everywhere", Type: string(team.ExpScopeGlobal)},
		{Slug: "proj-secret", Description: "project-only fact", Body: "acme project credentials", Type: string(team.ExpScopeProject)},
	})
	if err != nil {
		t.Fatalf("SeedTeam: %v", err)
	}
	if written != 3 {
		t.Fatalf("SeedTeam wrote %d entries, want 3", written)
	}

	// extract_from_team — the headline endpoint. NON-404, produces a real draft.
	st, body = postBearer(t, srv.URL, "/admin/agent-tools/extract_from_team", "acat_w1", map[string]any{
		"agent_id": atAgent1, "team_id": teamID,
	})
	if st == http.StatusNotFound {
		t.Fatalf("extract_from_team is a 404 — the domain is still unwired")
	}
	if st != http.StatusOK {
		t.Fatalf("extract_from_team status=%d body=%v", st, body)
	}

	draft, _ := body["draft"].(map[string]any)
	if draft == nil {
		t.Fatalf("extract_from_team returned no draft: %v", body)
	}
	// Roles carried over (2), portable experiences kept (2: team+global), project
	// scope dropped (1), draft NOT export-ready (curation still required).
	if roles, _ := draft["roles"].([]any); len(roles) != 2 {
		t.Fatalf("draft roles=%v want 2", draft["roles"])
	}
	if ec, _ := draft["experience_count"].(float64); ec != 2 {
		t.Fatalf("draft experience_count=%v want 2 (project scope dropped)", draft["experience_count"])
	}
	if dropped, _ := body["dropped_project"].(float64); dropped != 1 {
		t.Fatalf("dropped_project=%v want 1", body["dropped_project"])
	}
	if curated, _ := body["curated"].(bool); curated {
		t.Fatalf("extracted draft must be un-curated (manual curation still required)")
	}
	if name, _ := draft["name"].(string); name != "team-extract (extracted)" {
		t.Fatalf("draft name=%q want %q", name, "team-extract (extracted)")
	}
	// The scrub pass HIGHLIGHTED at least the proprietary token(s) in the kept
	// experiences (e.g. "T950" / "internal/team/foo.go") for manual curation.
	if findings, _ := body["scrub_findings"].([]any); len(findings) == 0 {
		t.Fatalf("scrub_findings empty — the curation-assist scrub did not run over kept experiences")
	}
}
