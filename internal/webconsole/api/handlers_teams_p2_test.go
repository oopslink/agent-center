package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition/memory/centergit"
	"github.com/oopslink/agent-center/internal/cognition/memory/teammemory"
	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/team"
	teamservice "github.com/oopslink/agent-center/internal/team/service"
	teamsql "github.com/oopslink/agent-center/internal/team/sqlite"
)

// setupTeamsAPI wires the Team WebUI facade over an authed test harness: the real
// team.Service on the in-memory DB + a signed-in owner session. TeamGitHost stays
// nil (test/client mode) so memory/extract exercise the degrade path.
func setupTeamsAPI(t *testing.T) (HandlerDeps, *sql.DB, testSession) {
	t.Helper()
	deps, db := setupAPIWithAuth(t)
	deps.TeamService = teamservice.New(teamsql.NewRepo(db), db, idgen.NewGenerator(clock.SystemClock{}), clock.SystemClock{})
	sess := setupTestSession(t, db, deps)
	return deps, db, sess
}

func setupTeamsMemoryAPI(t *testing.T) (HandlerDeps, *sql.DB, testSession) {
	t.Helper()
	deps, db, sess := setupTeamsAPI(t)
	host := centergit.NewHost(t.TempDir(), nil)
	deps.TeamGitHost = host
	deps.TeamMemory = centergit.NewTeamMemoryService(host, nil)
	repo := centergit.NewTeamMemoryRepository(host, nil)
	deps.TeamMemoryWrite = teammemory.NewService(repo, teammemory.NewTeamPolicyAuthorizationFromService(deps.TeamService, deps.MemberRepo))
	deps.TeamMemoryProjector = teammemory.NewProjector(nil, nil, nil, nil)
	return deps, db, sess
}

func seedTeam(t *testing.T, deps HandlerDeps, orgID, name string, roles []team.RoleConfig) *team.Team {
	t.Helper()
	tm, err := deps.TeamService.CreateTeam(context.Background(), teamservice.CreateTeamInput{
		OrgID: orgID, Name: name, Description: "seed", Roles: roles,
	})
	if err != nil {
		t.Fatalf("seed CreateTeam: %v", err)
	}
	return tm
}

func decodeArray(t *testing.T, resp *http.Response) []any {
	t.Helper()
	defer resp.Body.Close()
	var a []any
	if err := json.NewDecoder(resp.Body).Decode(&a); err != nil {
		t.Fatalf("decode array: %v", err)
	}
	return a
}

var implRole = []team.RoleConfig{{Role: "impl", CLI: "claude-code", Model: "claude-opus-4-8", CapabilityTags: []string{"go"}, MaxConcurrency: 2}}

func TestCreateTeam_AllowsNoRoleForEmptyTeam(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp := orgScopedPost(t, ts.URL+"/api/teams", `{"name":"Empty Squad","description":"start blank","visibility":"org-private","roles":[{"role":"","cli":"claude-code","model":"sonnet-5","max_concurrency":1,"count":1,"tags":""}]}`, sess)
	if resp.StatusCode != http.StatusCreated {
		body := decodeBody(t, resp)
		t.Fatalf("create empty team = %d (%v), want 201", resp.StatusCode, body)
	}
	body := decodeBody(t, resp)
	if body["name"] != "Empty Squad" {
		t.Fatalf("name = %v, want Empty Squad", body["name"])
	}
	roles, ok := body["roles"].([]any)
	if !ok {
		t.Fatalf("roles = %T %v, want array", body["roles"], body["roles"])
	}
	if len(roles) != 0 {
		t.Fatalf("blank role rows must be ignored for empty teams, got roles=%v", roles)
	}
	if body["status"] != "draft" {
		t.Fatalf("status = %v, want draft", body["status"])
	}
}

func TestUpdateTeam_RoleDefinitions(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	tm := seedTeam(t, deps, sess.OrgID, "Agent Core", implRole)
	ts := newTestServer(t, deps)
	defer ts.Close()
	resp := orgScopedPatch(t, ts.URL+"/api/teams/"+string(tm.ID()), `{"roles":[{"role":"reviewer","cli":"codex","model":"gpt-5","max_concurrency":2,"tags":"go, review"}]}`, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update roles = %d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	body := decodeBody(t, resp)
	roles := body["roles"].([]any)
	if len(roles) != 1 || roles[0].(map[string]any)["role"] != "reviewer" {
		t.Fatalf("roles = %#v", roles)
	}
	resp = orgScopedPatch(t, ts.URL+"/api/teams/"+string(tm.ID()), `{"roles":[]}`, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear roles = %d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
}

// --- auth gate ---------------------------------------------------------------

// TestTeamFacade_AuthGate locks that every new endpoint requires a valid session
// (no cookie → 401), i.e. the web-session gate, not the worker-token surface.
func TestTeamFacade_AuthGate(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	tm := seedTeam(t, deps, sess.OrgID, "Agent Core", implRole)
	ts := newTestServer(t, deps)
	defer ts.Close()

	cases := []struct{ method, path string }{
		{http.MethodPatch, "/api/orgs/" + sess.OrgSlug + "/teams/" + string(tm.ID())},
		{http.MethodPost, "/api/orgs/" + sess.OrgSlug + "/teams/instantiate"},
		{http.MethodGet, "/api/orgs/" + sess.OrgSlug + "/teams/" + string(tm.ID()) + "/extract"},
		{http.MethodGet, "/api/orgs/" + sess.OrgSlug + "/teams/" + string(tm.ID()) + "/memory"},
		{http.MethodPost, "/api/orgs/" + sess.OrgSlug + "/teams/" + string(tm.ID()) + "/memory/proposals"},
		{http.MethodPost, "/api/orgs/" + sess.OrgSlug + "/teams/" + string(tm.ID()) + "/memory/proposals/proposal-1/promote"},
		{http.MethodPost, "/api/orgs/" + sess.OrgSlug + "/teams/" + string(tm.ID()) + "/memory/proposals/proposal-1/reject"},
		{http.MethodPut, "/api/orgs/" + sess.OrgSlug + "/teams/" + string(tm.ID()) + "/memory/settings"},
		{http.MethodGet, "/api/orgs/" + sess.OrgSlug + "/team-templates"},
	}
	for _, c := range cases {
		req, _ := http.NewRequest(c.method, ts.URL+c.path, strings.NewReader("{}"))
		resp, err := http.DefaultClient.Do(req) // no cookie
		if err != nil {
			t.Fatalf("%s %s: %v", c.method, c.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s without session = %d, want 401", c.method, c.path, resp.StatusCode)
		}
	}
}

// --- update_team -------------------------------------------------------------

func TestUpdateTeam_HappyPath(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	tm := seedTeam(t, deps, sess.OrgID, "Agent Core", implRole)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp := orgScopedPatch(t, ts.URL+"/api/teams/"+string(tm.ID()), `{"name":"Renamed Core","description":"new desc"}`, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH = %d, want 200", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	if body["name"] != "Renamed Core" {
		t.Errorf("name = %v, want Renamed Core", body["name"])
	}
	if body["description"] != "new desc" {
		t.Errorf("description = %v, want new desc", body["description"])
	}
	// TeamView shape carries the display extras.
	if _, ok := body["glyph"]; !ok {
		t.Error("response missing glyph (not a TeamView)")
	}
}

func TestUpdateTeam_NotFound(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp := orgScopedPatch(t, ts.URL+"/api/teams/team-does-not-exist", `{"name":"x"}`, sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("PATCH unknown = %d, want 404", resp.StatusCode)
	}
}

// TestUpdateTeam_CrossOrgForbidden locks that a team owned by another org is
// treated as not-found (no cross-org write).
func TestUpdateTeam_CrossOrgForbidden(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	other := seedTeam(t, deps, "org-someone-else", "Other Org Team", implRole)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp := orgScopedPatch(t, ts.URL+"/api/teams/"+string(other.ID()), `{"name":"hijack"}`, sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-org PATCH = %d, want 404", resp.StatusCode)
	}
}

// --- instantiate_team --------------------------------------------------------

func TestInstantiateTeam_HappyPath(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	body := `{"team_name":"Squad Run","roles":[
		{"role":"planner","cli":"claude-code","model":"opus-4.8","max_concurrency":1,"count":1,"tags":"plan"},
		{"role":"coder","cli":"codex","model":"gpt-5","max_concurrency":3,"count":2,"tags":"go, backend"}
	]}`
	resp := orgScopedPost(t, ts.URL+"/api/teams/instantiate", body, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("instantiate = %d, want 201", resp.StatusCode)
	}
	view := decodeBody(t, resp)
	if view["name"] != "Squad Run" {
		t.Errorf("name = %v, want Squad Run", view["name"])
	}
	if view["status"] != "active" {
		t.Errorf("status = %v, want active", view["status"])
	}
	roles, ok := view["roles"].([]any)
	if !ok || len(roles) != 2 {
		t.Fatalf("roles = %v, want 2", view["roles"])
	}
	// per-role count echoes the requested composition (coder count=2).
	for _, r := range roles {
		rm := r.(map[string]any)
		if rm["role"] == "coder" && rm["count"].(float64) != 2 {
			t.Errorf("coder count = %v, want 2", rm["count"])
		}
	}
	// project-decoupled: no project bound on instantiate.
	if view["projects_count"].(float64) != 0 {
		t.Errorf("projects_count = %v, want 0 (project-decoupled)", view["projects_count"])
	}
}

// --- team templates ----------------------------------------------------------

func TestTeamTemplates_CreateListGet(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	create := `{"name":"Backend Squad","description":"go team","source":"manual","source_kind":"manual","roles":[
		{"role":"coder","cli":"claude-code","model":"opus-4.8","capability_tags":["go"],"max_concurrency":2,"count":3}
	]}`
	resp := orgScopedPost(t, ts.URL+"/api/team-templates", create, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create template = %d, want 201", resp.StatusCode)
	}
	created := decodeBody(t, resp)
	tid, _ := created["id"].(string)
	if tid == "" {
		t.Fatal("created template has no id")
	}
	if created["source_kind"] != "manual" || created["instances_count"].(float64) != 0 {
		t.Errorf("template extras = %v", created)
	}
	if created["curated"] != false {
		t.Errorf("curated = %v, want false", created["curated"])
	}

	// list returns the created template.
	listResp := orgScopedGet(t, ts.URL+"/api/team-templates", sess)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list = %d, want 200", listResp.StatusCode)
	}
	if arr := decodeArray(t, listResp); len(arr) != 1 {
		t.Fatalf("list len = %d, want 1", len(arr))
	}

	// get by id round-trips.
	getResp := orgScopedGet(t, ts.URL+"/api/team-templates/"+tid, sess)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get = %d, want 200", getResp.StatusCode)
	}
	got := decodeBody(t, getResp)
	if got["name"] != "Backend Squad" {
		t.Errorf("get name = %v", got["name"])
	}
	roles := got["roles"].([]any)
	if roles[0].(map[string]any)["count"].(float64) != 3 {
		t.Errorf("role count = %v, want 3", roles[0])
	}
}

func TestTeamTemplate_GetNotFound(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp := orgScopedGet(t, ts.URL+"/api/team-templates/nope", sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get unknown = %d, want 404", resp.StatusCode)
	}
}

// --- extract_from_team -------------------------------------------------------

// TestExtractFromTeam_RolesOnlyDraft locks the git-unwired degrade: a roles-only
// draft (no experiences to scrub), curated=false, TeamTemplate-shaped draft.
func TestExtractFromTeam_RolesOnlyDraft(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	tm := seedTeam(t, deps, sess.OrgID, "Agent Core", implRole)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp := orgScopedGet(t, ts.URL+"/api/teams/"+string(tm.ID())+"/extract", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("extract = %d, want 200", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	if body["curated"] != false {
		t.Errorf("curated = %v, want false (draft never export-ready)", body["curated"])
	}
	findings, ok := body["scrub_findings"].([]any)
	if !ok || len(findings) != 0 {
		t.Errorf("scrub_findings = %v, want [] (no memory wired)", body["scrub_findings"])
	}
	draft, ok := body["draft"].(map[string]any)
	if !ok || draft["name"] == "" {
		t.Fatalf("draft = %v, want a TeamTemplate-shaped draft", body["draft"])
	}
	if len(draft["roles"].([]any)) != 1 {
		t.Errorf("draft roles = %v, want 1 (copied from team)", draft["roles"])
	}
}

func TestExtractFromTeam_NotFound(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp := orgScopedGet(t, ts.URL+"/api/teams/ghost/extract", sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("extract unknown team = %d, want 404", resp.StatusCode)
	}
}

// --- team memory (read-only, git-unwired degrade) ----------------------------

func TestTeamMemory_IndexEmptyWhenUnwired(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	tm := seedTeam(t, deps, sess.OrgID, "Agent Core", implRole)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp := orgScopedGet(t, ts.URL+"/api/teams/"+string(tm.ID())+"/memory", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("memory index = %d, want 200", resp.StatusCode)
	}
	if arr := decodeArray(t, resp); len(arr) != 0 {
		t.Errorf("index = %v, want [] (git unwired)", arr)
	}
}

func TestTeamMemory_DocNotFound(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	tm := seedTeam(t, deps, sess.OrgID, "Agent Core", implRole)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp := orgScopedGet(t, ts.URL+"/api/teams/"+string(tm.ID())+"/memory/some-slug", sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("memory doc = %d, want 404 (git unwired)", resp.StatusCode)
	}
}

func TestTeamMemory_ProposalLifecycleSettingsAndPermissions(t *testing.T) {
	deps, db, owner := setupTeamsMemoryAPI(t)
	tm := seedTeam(t, deps, owner.OrgID, "Agent Core", implRole)
	memberSess := addOrgMemberSession(t, db, owner, identity.RoleMember, "regular-member")
	ts := newTestServer(t, deps)
	defer ts.Close()

	detail := orgScopedGet(t, ts.URL+"/api/teams/"+string(tm.ID()), owner)
	if detail.StatusCode != http.StatusOK {
		t.Fatalf("detail = %d body=%v", detail.StatusCode, decodeBody(t, detail))
	}
	view := decodeBody(t, detail)
	perms := view["memory_permissions"].(map[string]any)
	if perms["web_edit"] != true || perms["can_manage"] != true {
		t.Fatalf("owner memory permissions = %#v", perms)
	}

	noAck := orgScopedPost(t, ts.URL+"/api/teams/"+string(tm.ID())+"/memory/proposals", `{
		"target_kind":"entry","slug":"ci-runbook","description":"CI notes","body":"Use CI"
	}`, owner)
	if noAck.StatusCode != http.StatusBadRequest {
		t.Fatalf("create without ack = %d, want 400", noAck.StatusCode)
	}
	if code := errorCodeOf(t, noAck); code != "invalid_candidate" {
		t.Fatalf("error code = %q", code)
	}

	memberWrite := orgScopedPost(t, ts.URL+"/api/teams/"+string(tm.ID())+"/memory/proposals", `{
		"target_kind":"entry","slug":"member-note","description":"no","body":"no","warning_acknowledged":true
	}`, memberSess)
	if memberWrite.StatusCode != http.StatusForbidden {
		t.Fatalf("member create proposal = %d, want 403", memberWrite.StatusCode)
	}
	memberWrite.Body.Close()

	create := orgScopedPost(t, ts.URL+"/api/teams/"+string(tm.ID())+"/memory/proposals", `{
		"idempotency_key":"web-test-create-1",
		"operation":"add",
		"target_kind":"entry",
		"slug":"ci-runbook",
		"title":"CI Runbook",
		"description":"CI notes",
		"body":"Use CI and keep rollback notes.",
		"warning_acknowledged":true,
		"rationale":"record the CI runbook"
	}`, owner)
	if create.StatusCode != http.StatusCreated {
		t.Fatalf("create proposal = %d body=%v", create.StatusCode, decodeBody(t, create))
	}
	created := decodeBody(t, create)
	proposalID := created["id"].(string)
	if proposalID == "" || created["status"] != "pending" || created["source_path"] == "" || created["commit"] == "" {
		t.Fatalf("created proposal metadata = %#v", created)
	}
	if created["diff"] == "" {
		t.Fatalf("created proposal missing diff: %#v", created)
	}

	index := orgScopedGet(t, ts.URL+"/api/teams/"+string(tm.ID())+"/memory", owner)
	if index.StatusCode != http.StatusOK {
		t.Fatalf("memory index = %d body=%v", index.StatusCode, decodeBody(t, index))
	}
	arr := decodeArray(t, index)
	var proposalCount int
	for _, item := range arr {
		m, _ := item.(map[string]any)
		if m["slug"] == proposalID {
			proposalCount++
			if m["kind"] != "proposal" || m["status"] != "pending" || m["source_path"] == "" {
				t.Fatalf("proposal index metadata = %#v", m)
			}
		}
	}
	if proposalCount != 1 {
		t.Fatalf("proposal index count = %d, want exactly 1: %#v", proposalCount, arr)
	}

	docResp := orgScopedGet(t, ts.URL+"/api/teams/"+string(tm.ID())+"/memory/proposals/"+proposalID, owner)
	if docResp.StatusCode != http.StatusOK {
		t.Fatalf("proposal doc = %d body=%v", docResp.StatusCode, decodeBody(t, docResp))
	}
	doc := decodeBody(t, docResp)
	if doc["kind"] != "proposal" || doc["diff"] == "" || doc["commit"] == "" || doc["proposal"] == nil {
		t.Fatalf("proposal doc metadata = %#v", doc)
	}

	promote := orgScopedPost(t, ts.URL+"/api/teams/"+string(tm.ID())+"/memory/proposals/"+proposalID+"/promote", `{"expected_repo_commit":"`+created["commit"].(string)+`","expected_proposal_status":"pending","comment":"approved"}`, owner)
	if promote.StatusCode != http.StatusOK {
		t.Fatalf("promote = %d body=%v", promote.StatusCode, decodeBody(t, promote))
	}
	promoted := decodeBody(t, promote)
	if promoted["status"] != "promoted" || promoted["new_commit"] == "" || promoted["effective_for"] != "new_sessions_and_forks" {
		t.Fatalf("promoted = %#v", promoted)
	}

	entryDoc := orgScopedGet(t, ts.URL+"/api/teams/"+string(tm.ID())+"/memory/ci-runbook?kind=entry", owner)
	if entryDoc.StatusCode != http.StatusOK {
		t.Fatalf("entry doc = %d body=%v", entryDoc.StatusCode, decodeBody(t, entryDoc))
	}
	entry := decodeBody(t, entryDoc)
	if entry["source_path"] == "" || entry["uuid"] == "" || entry["commit"] == "" {
		t.Fatalf("entry metadata = %#v", entry)
	}

	req, _ := http.NewRequest(http.MethodPut, orgScopedURL(ts.URL+"/api/teams/"+string(tm.ID())+"/memory/settings", owner.OrgSlug), strings.NewReader(`{"policy":"proposal_only","curator_agents":[]}`))
	req.AddCookie(owner.Cookie)
	putResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT settings: %v", err)
	}
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT settings = %d body=%v", putResp.StatusCode, decodeBody(t, putResp))
	}
	settings := decodeBody(t, putResp)
	if settings["policy"] != "proposal_only" || settings["effect_hint"] == "" {
		t.Fatalf("settings = %#v", settings)
	}
	agents := settings["curator_agents"].([]any)
	if len(agents) != 0 {
		t.Fatalf("curator_agents = %#v", agents)
	}
}

func TestTeamMemory_AgentCannotSelfGrantSettings(t *testing.T) {
	deps, _, owner := setupTeamsMemoryAPI(t)
	tm := seedTeam(t, deps, owner.OrgID, "Agent Core", implRole)

	ctx := context.Background()
	agentIdent, err := identity.IdentityFactory{}.NewAgent("curator-agent", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := deps.IdentityRepo.Save(ctx, agentIdent); err != nil {
		t.Fatal(err)
	}
	member, err := identity.MemberFactory{}.New(owner.OrgID, agentIdent.ID(), identity.RoleAdmin, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := deps.MemberRepo.Save(ctx, member); err != nil {
		t.Fatal(err)
	}
	jwt, err := identity.MintJWT(agentIdent.ID(), testSigningKey)
	if err != nil {
		t.Fatal(err)
	}
	agentSess := testSession{
		IdentityID: agentIdent.ID(),
		OrgID:      owner.OrgID,
		OrgSlug:    owner.OrgSlug,
		Cookie:     &http.Cookie{Name: jwtCookieName, Value: jwt},
	}
	ts := newTestServer(t, deps)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPut, orgScopedURL(ts.URL+"/api/teams/"+string(tm.ID())+"/memory/settings", agentSess.OrgSlug), strings.NewReader(`{"policy":"curator_review","curator_agents":["agent:`+agentIdent.ID()+`"]}`))
	req.AddCookie(agentSess.Cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT settings as agent: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("agent self-grant = %d, want 403", resp.StatusCode)
	}
	if code := errorCodeOf(t, resp); code != "agent_self_grant_forbidden" {
		t.Fatalf("code = %q", code)
	}
}

// TestTeamNotWired locks the 501 degrade when the team service is absent.
func TestTeamFacade_NotWired(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps) // TeamService intentionally nil
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp := orgScopedPost(t, ts.URL+"/api/teams/instantiate", `{"team_name":"x"}`, sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("instantiate without team service = %d, want 501", resp.StatusCode)
	}
}
