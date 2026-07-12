package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/cognition/memory/centergit"
	"github.com/oopslink/agent-center/internal/idgen"
	teamservice "github.com/oopslink/agent-center/internal/team/service"
	teamsql "github.com/oopslink/agent-center/internal/team/sqlite"
)

// wireTeamLive is wireTeam's sibling, but it backs the git Authorizer with the
// PRODUCTION membership adapter (api.NewTeamMembership over the LIVE team + agent
// tables) instead of the in-memory MapMembership. This is deliberate: the ref-vs-id
// bug (T1019 ②) lived ONLY in that adapter — the ULID→identity-member-ref bridge —
// so a test on MapMembership can never exercise it. Here the exact production code
// path decides read/write.
func wireTeamLive(t *testing.T, f *writeToolsFixture) (*httptest.Server, *centergit.Host) {
	t.Helper()
	gen := idgen.NewGenerator(f.clk)
	f.deps.TeamSvc = teamservice.New(teamsql.NewRepo(f.db), f.db, gen, f.clk)
	f.deps.TeamIDGen = gen
	gitHost := centergit.NewHost(t.TempDir(), nil)
	f.deps.TeamGitHost = gitHost

	membership := NewTeamMembership(teamsql.NewRepo(f.db), f.agents)
	gitHandler, err := NewGitHandler(gitHost, membership)
	if err != nil {
		t.Skipf("git smart-HTTP unavailable in this environment: %v", err)
	}
	srv := NewServerWithDeps("", ServerDeps{GitHandler: gitHandler})
	h := AuthMiddleware(f.verifier)(WithDeps(f.deps)(srv.Handler()))
	httpsrv := httptest.NewServer(h)
	t.Cleanup(httpsrv.Close)
	return httpsrv, gitHost
}

// gitHome is a hermetic git env isolated from the dev machine's ~/.gitconfig, so
// the client behaves identically in CI and locally.
func gitHome(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// runAgentGit runs the git client against the center-hosted smart-HTTP endpoint
// AS an agent: the worker bearer + the X-Agent-Id header are injected via
// http.extraHeader, exactly as the worker-side memory sync client sends them.
// Returns combined output + error (nil error == the git op got a 2xx).
func runAgentGit(home, dir, bearer, agentID string, args ...string) (string, error) {
	full := []string{
		"-c", "http.extraHeader=Authorization: Bearer " + bearer,
		"-c", "http.extraHeader=X-Agent-Id: " + agentID,
		"-c", "commit.gpgsign=false",
		"-c", "protocol.version=2",
	}
	full = append(full, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=Agent", "GIT_COMMITTER_NAME=Agent",
		"GIT_AUTHOR_EMAIL=agent@agent-center.local", "GIT_COMMITTER_EMAIL=agent@agent-center.local",
		"HOME=" + home, "XDG_CONFIG_HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// seedRuntimeAgent seeds an execution Agent whose runtime id (a ULID — the value
// X-Agent-Id carries) is DISTINCT from its identity-member id ("agent-<...>", the
// namespace team member refs live in). This split is the whole point: it forces
// the git authz path to cross the two id namespaces, reproducing T1019 ②.
func seedRuntimeAgent(t *testing.T, f *writeToolsFixture, runtimeID, memberID string) {
	t.Helper()
	a, err := agent.NewAgent(agent.NewAgentInput{
		ID: agent.AgentID(runtimeID), OrganizationID: atTestOrg,
		Profile: agent.Profile{Name: runtimeID}, WorkerID: atWorker1,
		CreatedBy: "system", IdentityMemberID: memberID, CreatedAt: atNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.agents.Save(t.Context(), a); err != nil {
		t.Fatal(err)
	}
}

// cloneCommitPush drives a full clone → commit → push round-trip against the team
// repo as the given agent, asserting each git op reaches a 2xx. This is the real
// acceptance shape (T1019 ②): a genuine member cloning + pushing its team repo
// with its OWN token/agent-id must succeed, not 403.
func cloneCommitPush(t *testing.T, srv *httptest.Server, teamID, bearer, runtimeID, tag string) {
	t.Helper()
	home := gitHome(t)
	work := t.TempDir()
	url := srv.URL + "/admin/git/team/" + teamID + ".git"

	if out, err := runAgentGit(home, work, bearer, runtimeID, "clone", url, "wc"); err != nil {
		t.Fatalf("[%s] git clone team repo failed (want 200, got err): %v\n%s", tag, err, out)
	}
	wc := filepath.Join(work, "wc")
	fname := "note-" + tag + ".md"
	if err := os.WriteFile(filepath.Join(wc, fname), []byte("# "+tag+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := runAgentGit(home, wc, bearer, runtimeID, "add", "-A"); err != nil {
		t.Fatalf("[%s] git add failed: %v\n%s", tag, err, out)
	}
	if out, err := runAgentGit(home, wc, bearer, runtimeID, "commit", "-m", "member push "+tag); err != nil {
		t.Fatalf("[%s] git commit failed: %v\n%s", tag, err, out)
	}
	if out, err := runAgentGit(home, wc, bearer, runtimeID, "push", "origin", "HEAD:main"); err != nil {
		t.Fatalf("[%s] git push team repo failed (want 200, got err): %v\n%s", tag, err, out)
	}
}

// TestCenterGit_TeamMember_RealRefClonePush is the T1019 ② regression lock. It
// exercises the git rw path with the LIVE membership adapter and members added by
// the SYSTEM'S OWN member-id refs (NOT a hand-built ULID ref):
//
//   - add_member path: a member joined via its identity-member ref
//     ("agent:agent-<...>"), operating under its DISTINCT runtime ULID.
//   - instantiate_team path: an agent freshly MINTED by instantiation, likewise
//     operating under a distinct runtime ULID.
//
// Both must clone AND push their team repo with a 200. Under the pre-fix wiring the
// adapter looked the member up by the raw runtime ULID ("agent:<ULID>"), which can
// never equal the stored "agent:agent-<...>" ref → 403 for every real member. A
// non-member (same worker token) must still get 403, proving it is real authz.
func TestCenterGit_TeamMember_RealRefClonePush(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv, gitHost := wireTeamLive(t, f)
	ctx := t.Context()

	// ---- add_member path: real system-produced member-id ref -----------------
	// Runtime id (ULID) != member id ("agent-*") — the namespace split under test.
	const rtMember = "AGRT-01JMEMBERULID000000000000"
	const midMember = "agent-a1b2c3d4"
	seedRuntimeAgent(t, f, rtMember, midMember)

	st, body := postBearer(t, srv.URL, "/admin/agent-tools/create_team", "acat_w1", map[string]any{
		"agent_id": atAgent1, "name": "team-git-rw", "description": "rw regression",
		"roles": []map[string]any{{"role": "dev", "cli": "claude-code", "max_concurrency": 2}},
	})
	if st != http.StatusCreated {
		t.Fatalf("create_team status=%d body=%v", st, body)
	}
	teamID, _ := body["id"].(string)
	if teamID == "" {
		t.Fatalf("create_team returned no id: %v", body)
	}

	// Add the member by the SYSTEM member-id ref — the ref add_member persists.
	memberRef := "agent:" + midMember
	st, body = postBearer(t, srv.URL, "/admin/agent-tools/add_member", "acat_w1", map[string]any{
		"agent_id": atAgent1, "team_id": teamID, "member_ref": memberRef, "role": "dev",
	})
	if st != http.StatusCreated {
		t.Fatalf("add_member status=%d body=%v", st, body)
	}

	// Provision the team's shared repo (create_team does not; instantiate does).
	if err := gitHost.EnsureRepo(ctx, centergit.TeamRepo(teamID)); err != nil {
		t.Fatalf("EnsureRepo team: %v", err)
	}

	// The member clones + pushes its OWN team repo under its runtime ULID → 200.
	cloneCommitPush(t, srv, teamID, "acat_w1", rtMember, "addmember")

	// A non-member bound to the same worker must still be refused (real authz, not
	// a blanket allow): resolver OK (agent→worker), but no team → 403.
	const rtOutsider = "AGRT-01JOUTSIDERULID0000000000"
	seedRuntimeAgent(t, f, rtOutsider, "agent-outsider99")
	home := gitHome(t)
	work := t.TempDir()
	url := srv.URL + "/admin/git/team/" + teamID + ".git"
	out, err := runAgentGit(home, work, "acat_w1", rtOutsider, "clone", url, "wc")
	if err == nil {
		t.Fatalf("non-member clone unexpectedly succeeded (want 403):\n%s", out)
	}
	if !strings.Contains(out, "403") {
		t.Fatalf("non-member clone want 403, got:\n%s", out)
	}

	// ---- instantiate_team path: a MINTED agent's real member ref -------------
	st, body = postBearer(t, srv.URL, "/admin/agent-tools/instantiate_team", "acat_w1", map[string]any{
		"agent_id": atAgent1, "project_id": "proj-git-rw", "team_name": "inst-git-rw",
		"template": map[string]any{
			"name": "tmpl-git-rw", "description": "inst rw",
			"roles": []map[string]any{{"role": "dev", "cli": "claude-code", "count": 1, "max_concurrency": 1}},
		},
	})
	if st != http.StatusCreated {
		t.Fatalf("instantiate_team status=%d body=%v", st, body)
	}
	instTeam, _ := body["team"].(map[string]any)
	instTeamID, _ := instTeam["id"].(string)
	if instTeamID == "" {
		t.Fatalf("instantiate_team returned no team id: %v", body)
	}
	// Pull the minted member's identity-member id off the response and enroll a
	// runtime agent for it (the SEPARATE runtime step design §9 describes), again
	// with a runtime ULID distinct from the member id.
	agents, _ := body["agents"].([]any)
	if len(agents) != 1 {
		t.Fatalf("instantiate_team want 1 minted agent, got %v", body["agents"])
	}
	minted, _ := agents[0].(map[string]any)
	mintedMemberID, _ := minted["agent_id"].(string) // "agent-<hex>", the member id
	if !strings.HasPrefix(mintedMemberID, "agent-") {
		t.Fatalf("minted agent_id not an identity-member id: %q", mintedMemberID)
	}
	const rtInst = "AGRT-01JINSTANCEULID0000000000"
	seedRuntimeAgent(t, f, rtInst, mintedMemberID)

	// instantiate already provisioned + seeded the team repo; the minted agent
	// clones + pushes it under its runtime ULID → 200.
	cloneCommitPush(t, srv, instTeamID, "acat_w1", rtInst, "instantiate")

	// Sanity: the two ids really are different namespaces (guards against a future
	// refactor that accidentally makes runtime id == member id and hides the bug).
	if rtMember == midMember || rtInst == mintedMemberID {
		t.Fatal("runtime id must differ from member id for this regression to be meaningful")
	}
}
