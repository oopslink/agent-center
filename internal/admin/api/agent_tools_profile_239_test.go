package api

import (
	"net/http"
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/identity"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// v2.7.1 #239: get_my_profile + find_org_agent — agent self/org discovery. The
// operating agent is fixed by the token-bound worker (the shared guardrail), so
// these are inherently self/own-org scoped and cannot be spoofed via the body.

// --- find_org_agent ----------------------------------------------------------

func TestFindOrgAgent_NameFilter_OK(t *testing.T) {
	f := newAgentToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	s := f.server(t)

	// AG1 (org-1) searches for "AG2" — same org, substring match → one hit.
	status, body := postBearer(t, s.URL, "/admin/agent-tools/find_org_agent", "acat_w1",
		map[string]any{"agent_id": atAgent1, "name": "AG2"})
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%v", status, body)
	}
	agents, _ := body["agents"].([]any)
	if len(agents) != 1 {
		t.Fatalf("want 1 agent named AG2, got %v", body["agents"])
	}
	row := agents[0].(map[string]any)
	if row["id"] != atAgent2 || row["name"] != atAgent2 {
		t.Fatalf("row=%v, want id/name AG2", row)
	}
	// v2.7.1 #241: assignee_ref is the ready-to-use "agent:<id>" form for assign_task.
	if row["assignee_ref"] != "agent:"+atAgent2 {
		t.Fatalf("assignee_ref=%v, want agent:%s", row["assignee_ref"], atAgent2)
	}
	// It must pass the SAME validation assign_task applies to its assignee, so the
	// agent can feed it straight in (no bare-id-vs-prefixed-ref footgun).
	if err := pm.IdentityRef(row["assignee_ref"].(string)).Validate(); err != nil {
		t.Fatalf("assignee_ref %q must be a valid assign_task assignee: %v", row["assignee_ref"], err)
	}
}

func TestFindOrgAgent_EmptyName_ListsAll(t *testing.T) {
	f := newAgentToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	s := f.server(t)

	// Empty name → all org agents (AG1 + AG2, both in org-1).
	status, body := postBearer(t, s.URL, "/admin/agent-tools/find_org_agent", "acat_w1",
		map[string]any{"agent_id": atAgent1, "name": ""})
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%v", status, body)
	}
	if agents, _ := body["agents"].([]any); len(agents) != 2 {
		t.Fatalf("want 2 org agents, got %v", body["agents"])
	}
}

// The guardrail applies: a W1 token may not operate AG2 (bound to W2) → 403,
// so find_org_agent's org scope can't be borrowed via another worker's agent.
func TestFindOrgAgent_CrossWorker_403(t *testing.T) {
	f := newAgentToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	s := f.server(t)

	status, body := postBearer(t, s.URL, "/admin/agent-tools/find_org_agent", "acat_w1",
		map[string]any{"agent_id": atAgent2, "name": ""})
	if status != http.StatusForbidden {
		t.Fatalf("status=%d, want 403; body=%v", status, body)
	}
}

// --- get_my_profile ----------------------------------------------------------

// Degraded wiring (no PMService / IdentityOrgRepo): the handler still returns a
// well-formed self profile — own org_id, empty org_name, my_projects as an
// empty array (never null), and the org-scoped capability list.
func TestGetMyProfile_Shape_Degraded(t *testing.T) {
	f := newAgentToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	s := f.server(t)

	status, body := postBearer(t, s.URL, "/admin/agent-tools/get_my_profile", "acat_w1",
		map[string]any{"agent_id": atAgent1})
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%v", status, body)
	}
	if body["org_id"] != atTestOrg {
		t.Fatalf("org_id=%v, want %s (own org)", body["org_id"], atTestOrg)
	}
	if body["org_name"] != "" {
		t.Fatalf("org_name=%v, want empty (no IdentityOrgRepo)", body["org_name"])
	}
	mp, ok := body["my_projects"].([]any)
	if !ok || len(mp) != 0 {
		t.Fatalf("my_projects=%v, want empty array (never null)", body["my_projects"])
	}
	caps, _ := body["my_capabilities"].([]any)
	if len(caps) == 0 {
		t.Fatalf("my_capabilities must be non-empty, got %v", body["my_capabilities"])
	}
	found := false
	for _, c := range caps {
		if c == "get_my_profile" {
			found = true
		}
	}
	if !found {
		t.Fatalf("my_capabilities should list get_my_profile, got %v", caps)
	}
}

// Self-scope can't be spoofed: a W1 token operating AG2 (W2's agent) → 403.
func TestGetMyProfile_CrossWorker_403(t *testing.T) {
	f := newAgentToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	s := f.server(t)

	status, _ := postBearer(t, s.URL, "/admin/agent-tools/get_my_profile", "acat_w1",
		map[string]any{"agent_id": atAgent2})
	if status != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", status)
	}
}

// Full path: with PMService + IdentityOrgRepo wired and the operating agent
// seeded as a member of a project in its org, get_my_profile resolves org_name +
// my_projects (with role + per-project capabilities). The project-member ref is
// "agent:" + IdentityMemberID(), so we seed a fresh agent AG3 (bound to W1, with
// a member id — the fixture's AG1/AG2 carry none) and register it as a member.
func TestGetMyProfile_Populated(t *testing.T) {
	f := newAgentToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)

	// Seed AG3 on W1 with an identity-member id so its project-member ref is valid.
	const ag3 = "AG3"
	const ag3Member = "AG3-mem"
	a3, aerr := agent.NewAgent(agent.NewAgentInput{
		ID: ag3, OrganizationID: atTestOrg, Profile: agent.Profile{Name: ag3},
		WorkerID: atWorker1, CreatedBy: "system", IdentityMemberID: ag3Member, CreatedAt: atNow,
	})
	if aerr != nil {
		t.Fatal(aerr)
	}
	if aerr := f.agents.Save(t.Context(), a3); aerr != nil {
		t.Fatal(aerr)
	}

	// Seed org-1 name.
	orgRepo := identity.NewSQLiteOrganizationRepo(f.db)
	org := identity.RehydrateOrganization(atTestOrg, "org-1-slug", "Acme Org", "", "system", atNow, atNow, nil, nil)
	if err := orgRepo.Save(t.Context(), org); err != nil {
		t.Fatal(err)
	}
	f.deps.IdentityOrgRepo = orgRepo

	// Seed a project in org-1 + AG1 as a member.
	projRepo := pmsql.NewProjectRepo(f.db)
	memRepo := pmsql.NewProjectMemberRepo(f.db)
	proj, err := pm.NewProject(pm.NewProjectInput{
		ID: "proj-1", OrganizationID: atTestOrg, Name: "Alpha", CreatedBy: "system", CreatedAt: atNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := projRepo.Save(t.Context(), proj); err != nil {
		t.Fatal(err)
	}
	mem, err := pm.NewProjectMember(pm.NewProjectMemberInput{
		ID: "pm-1", ProjectID: "proj-1", IdentityID: pm.IdentityRef("agent:" + ag3Member), Role: pm.RoleMember, AddedBy: "system", CreatedAt: atNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := memRepo.Save(t.Context(), mem); err != nil {
		t.Fatal(err)
	}
	f.deps.PMService = pmservice.New(pmservice.Deps{Projects: projRepo, Members: memRepo})

	s := f.server(t)
	status, body := postBearer(t, s.URL, "/admin/agent-tools/get_my_profile", "acat_w1",
		map[string]any{"agent_id": ag3})
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%v", status, body)
	}
	if body["org_name"] != "Acme Org" {
		t.Fatalf("org_name=%v, want Acme Org", body["org_name"])
	}
	mp, _ := body["my_projects"].([]any)
	if len(mp) != 1 {
		t.Fatalf("my_projects=%v, want 1 (AG1 is a member of proj-1)", body["my_projects"])
	}
	p := mp[0].(map[string]any)
	if p["id"] != "proj-1" || p["name"] != "Alpha" || p["role"] != "member" {
		t.Fatalf("project row=%v, want proj-1/Alpha/member", p)
	}
	if caps, _ := p["my_capabilities"].([]any); len(caps) == 0 {
		t.Fatalf("per-project my_capabilities must be non-empty, got %v", p["my_capabilities"])
	}
	// E2E finding F-3 class-guard: get_my_profile MUST surface the agent's OWN
	// identity (display_name + agent_ref) so it can tell which @mentions are for it
	// and never impersonate another agent in a shared conversation.
	if body["display_name"] != ag3 {
		t.Fatalf("display_name=%v, want %q", body["display_name"], ag3)
	}
	if body["agent_ref"] != "agent:"+ag3Member {
		t.Fatalf("agent_ref=%v, want agent:%s", body["agent_ref"], ag3Member)
	}
}
