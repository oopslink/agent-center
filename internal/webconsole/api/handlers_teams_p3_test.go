package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/team"
)

// --- auth gate ---------------------------------------------------------------

// TestTeamFacadeP3_AuthGate locks that every P3 endpoint requires a valid
// web-session (no cookie → 401).
func TestTeamFacadeP3_AuthGate(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	tm := seedTeam(t, deps, sess.OrgID, "Agent Core", implRole)
	ts := newTestServer(t, deps)
	defer ts.Close()

	cases := []struct{ method, path string }{
		{http.MethodDelete, "/api/orgs/" + sess.OrgSlug + "/teams/" + string(tm.ID()) + "/projects/proj-1"},
		{http.MethodPost, "/api/orgs/" + sess.OrgSlug + "/team-templates/save"},
		{http.MethodPost, "/api/orgs/" + sess.OrgSlug + "/team-templates/import"},
		{http.MethodGet, "/api/orgs/" + sess.OrgSlug + "/team-templates/tmpl-1/instances"},
		{http.MethodGet, "/api/orgs/" + sess.OrgSlug + "/directory/agents"},
		{http.MethodGet, "/api/orgs/" + sess.OrgSlug + "/directory/humans"},
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

// --- disassociate_project ----------------------------------------------------

func TestDisassociateProject_HappyPath(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	tm := seedTeam(t, deps, sess.OrgID, "Agent Core", implRole)
	if err := deps.TeamService.AssociateProject(context.Background(), tm.ID(), "proj-1"); err != nil {
		t.Fatalf("seed associate: %v", err)
	}
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp := orgScopedDelete(t, ts.URL+"/api/teams/"+string(tm.ID())+"/projects/proj-1", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disassociate = %d, want 200", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	if body["ok"] != true || body["project_id"] != "proj-1" {
		t.Errorf("body = %v, want ok + project_id", body)
	}
	// the link is gone.
	projects, _ := deps.TeamService.ListProjects(context.Background(), tm.ID())
	if len(projects) != 0 {
		t.Errorf("projects after disassociate = %d, want 0", len(projects))
	}
}

func TestDisassociateProject_NotAssociated(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	tm := seedTeam(t, deps, sess.OrgID, "Agent Core", implRole)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp := orgScopedDelete(t, ts.URL+"/api/teams/"+string(tm.ID())+"/projects/ghost", sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("disassociate unlinked = %d, want 404", resp.StatusCode)
	}
}

func TestDisassociateProject_CrossOrgForbidden(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	other := seedTeam(t, deps, "org-someone-else", "Other Org Team", implRole)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp := orgScopedDelete(t, ts.URL+"/api/teams/"+string(other.ID())+"/projects/p", sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-org disassociate = %d, want 404", resp.StatusCode)
	}
}

// --- template save / import --------------------------------------------------

func TestSaveTemplate_CuratedThenListed(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	body := `{"name":"Curated Squad","description":"reviewed","source":"从 Agent Core 提取","source_kind":"extract","roles":[
		{"role":"coder","cli":"claude-code","model":"opus-4.8","capability_tags":["go"],"max_concurrency":2,"count":3}
	]}`
	resp := orgScopedPost(t, ts.URL+"/api/team-templates/save", body, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("save = %d, want 201", resp.StatusCode)
	}
	saved := decodeBody(t, resp)
	if saved["curated"] != true {
		t.Errorf("curated = %v, want true (save persists the curated draft)", saved["curated"])
	}
	if saved["source_kind"] != "extract" {
		t.Errorf("source_kind = %v, want extract", saved["source_kind"])
	}
	if !strings.Contains(saved["version_label"].(string), "curated") {
		t.Errorf("version_label = %v, want a curated label", saved["version_label"])
	}

	// list reflects it.
	listResp := orgScopedGet(t, ts.URL+"/api/team-templates", sess)
	if arr := decodeArray(t, listResp); len(arr) != 1 {
		t.Fatalf("list len = %d, want 1", len(arr))
	}
}

func TestImportTemplate_UncuratedWithDefaults(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	// a partial envelope — missing fields fall back to defaults.
	body := `{"name":"Imported","roles":[{"role":"reviewer"}],"workflow_template_ref":"plan-x"}`
	resp := orgScopedPost(t, ts.URL+"/api/team-templates/import", body, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("import = %d, want 201", resp.StatusCode)
	}
	imported := decodeBody(t, resp)
	if imported["curated"] != false {
		t.Errorf("curated = %v, want false (import must be re-curated)", imported["curated"])
	}
	if imported["source_kind"] != "import" {
		t.Errorf("source_kind = %v, want import", imported["source_kind"])
	}
	roles := imported["roles"].([]any)
	r0 := roles[0].(map[string]any)
	if r0["cli"] != "claude-code" || r0["model"] != "sonnet-5" || r0["count"].(float64) != 1 {
		t.Errorf("role defaults not applied: %v", r0)
	}
}

// --- template instances ------------------------------------------------------

func TestTemplateInstances_ReturnsInstantiatedTeams(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	// create a template.
	create := `{"name":"Backend Squad","description":"go","source":"manual","source_kind":"manual","roles":[
		{"role":"coder","cli":"claude-code","model":"opus-4.8","capability_tags":["go"],"max_concurrency":2,"count":1}
	]}`
	createResp := orgScopedPost(t, ts.URL+"/api/team-templates", create, sess)
	tid := decodeBody(t, createResp)["id"].(string)

	// no instances yet.
	empty := orgScopedGet(t, ts.URL+"/api/team-templates/"+tid+"/instances", sess)
	if empty.StatusCode != http.StatusOK {
		t.Fatalf("instances = %d, want 200", empty.StatusCode)
	}
	if arr := decodeArray(t, empty); len(arr) != 0 {
		t.Fatalf("instances = %d, want 0", len(arr))
	}

	// instantiate from the template.
	inst := `{"template_id":"` + tid + `","team_name":"Backend One","roles":[
		{"role":"coder","cli":"claude-code","model":"opus-4.8","max_concurrency":2,"count":1,"tags":"go"}
	]}`
	if r := orgScopedPost(t, ts.URL+"/api/teams/instantiate", inst, sess); r.StatusCode != http.StatusCreated {
		t.Fatalf("instantiate = %d, want 201", r.StatusCode)
	}

	// instances now lists the new team as a TeamView.
	listResp := orgScopedGet(t, ts.URL+"/api/team-templates/"+tid+"/instances", sess)
	arr := decodeArray(t, listResp)
	if len(arr) != 1 {
		t.Fatalf("instances = %d, want 1", len(arr))
	}
	view := arr[0].(map[string]any)
	if view["name"] != "Backend One" {
		t.Errorf("instance name = %v, want Backend One", view["name"])
	}
	if _, ok := view["glyph"]; !ok {
		t.Error("instance missing glyph (not a TeamView)")
	}

	// the template's instances_count now reflects the instantiation.
	getResp := orgScopedGet(t, ts.URL+"/api/team-templates/"+tid, sess)
	if c := decodeBody(t, getResp)["instances_count"].(float64); c != 1 {
		t.Errorf("instances_count = %v, want 1", c)
	}
}

func TestTemplateInstances_UnknownTemplate(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp := orgScopedGet(t, ts.URL+"/api/team-templates/nope/instances", sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown template instances = %d, want 404", resp.StatusCode)
	}
}

// --- directory ---------------------------------------------------------------

func TestDirectoryHumans_RealIdentityAndTeams(t *testing.T) {
	deps, db, sess := setupTeamsAPI(t)
	// give the session user an email so DirectoryHuman.email is a real value.
	idRepo := identity.NewSQLiteIdentityRepo(db)
	ident, err := idRepo.GetByID(context.Background(), sess.IdentityID)
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	if err := ident.SetEmail("owner@example.com"); err != nil {
		t.Fatalf("set email: %v", err)
	}
	if err := idRepo.Update(context.Background(), ident); err != nil {
		t.Fatalf("update identity: %v", err)
	}
	// put the user on a team so teams/role populate.
	tm := seedTeam(t, deps, sess.OrgID, "Agent Core", implRole)
	if _, err := deps.TeamService.AddMember(context.Background(), tm.ID(), team.MemberRef("user:"+sess.IdentityID), "impl"); err != nil {
		t.Fatalf("add member: %v", err)
	}
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp := orgScopedGet(t, ts.URL+"/api/directory/humans", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("humans = %d, want 200", resp.StatusCode)
	}
	arr := decodeArray(t, resp)
	if len(arr) != 1 {
		t.Fatalf("humans = %d, want 1", len(arr))
	}
	h := arr[0].(map[string]any)
	if h["name"] != "testuser" {
		t.Errorf("name = %v, want testuser", h["name"])
	}
	if h["email"] != "owner@example.com" {
		t.Errorf("email = %v, want owner@example.com", h["email"])
	}
	if h["status"] != "Joined" {
		t.Errorf("status = %v, want Joined", h["status"])
	}
	teams := h["teams"].([]any)
	if len(teams) != 1 || teams[0] != "Agent Core" {
		t.Errorf("teams = %v, want [Agent Core]", teams)
	}
	if h["role"] != "impl" {
		t.Errorf("role = %v, want impl", h["role"])
	}
	// created is always present; last is "—" (no session recorded).
	if h["created"] == "" || h["last"] != "—" {
		t.Errorf("created/last = %v/%v", h["created"], h["last"])
	}
}

func TestDirectoryAgents_MembershipAndPlaceholders(t *testing.T) {
	deps, db, sess := setupTeamsAPI(t)
	// provision an agent identity + org member directly (no execution agent → the
	// model/status resolve to the unwired defaults).
	ctx := context.Background()
	idRepo := identity.NewSQLiteIdentityRepo(db)
	memberRepo := identity.NewSQLiteMemberRepo(db)
	agentIdent, err := identity.IdentityFactory{}.NewAgent("Ada", "backend agent")
	if err != nil {
		t.Fatalf("new agent identity: %v", err)
	}
	if err := idRepo.Save(ctx, agentIdent); err != nil {
		t.Fatalf("save agent identity: %v", err)
	}
	agentMember, err := identity.MemberFactory{}.New(sess.OrgID, agentIdent.ID(), identity.RoleMember, nil)
	if err != nil {
		t.Fatalf("new agent member: %v", err)
	}
	if err := memberRepo.Save(ctx, agentMember); err != nil {
		t.Fatalf("save agent member: %v", err)
	}
	// put the agent on a team.
	tm := seedTeam(t, deps, sess.OrgID, "Agent Core", implRole)
	if _, err := deps.TeamService.AddMember(ctx, tm.ID(), team.MemberRef("agent:"+agentIdent.ID()), "impl"); err != nil {
		t.Fatalf("add agent member: %v", err)
	}
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp := orgScopedGet(t, ts.URL+"/api/directory/agents", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("agents = %d, want 200", resp.StatusCode)
	}
	arr := decodeArray(t, resp)
	if len(arr) != 1 {
		t.Fatalf("agents = %d, want 1 (the session user is human, excluded)", len(arr))
	}
	a := arr[0].(map[string]any)
	if a["name"] != "Ada" {
		t.Errorf("name = %v, want Ada", a["name"])
	}
	if a["status"] != "idle" {
		t.Errorf("status = %v, want idle (no execution agent)", a["status"])
	}
	if a["role"] != "impl" {
		t.Errorf("role = %v, want impl", a["role"])
	}
	teams := a["teams"].([]any)
	if len(teams) != 1 || teams[0] != "Agent Core" {
		t.Errorf("teams = %v, want [Agent Core]", teams)
	}
	// Phase-1 telemetry placeholders.
	if a["load"].(float64) != 0 || a["backlog"].(float64) != 0 || a["last"] != "—" {
		t.Errorf("placeholders = load %v backlog %v last %v, want 0/0/—", a["load"], a["backlog"], a["last"])
	}
}
