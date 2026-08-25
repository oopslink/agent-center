package api

import (
	"net/http"
	"testing"

	authz "github.com/oopslink/agent-center/internal/authorization"
)

func seedT1413AgentOrgMember(t *testing.T, f *writeToolsFixture) {
	t.Helper()
	now := atNow.Format("2006-01-02T15:04:05.999999999Z07:00")
	if _, err := f.db.Exec(`
		INSERT OR IGNORE INTO organizations (id, slug, name, created_by_identity_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`, atTestOrg, atTestOrg, "T1413 Org", "system", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`
		INSERT OR IGNORE INTO members (id, organization_id, identity_id, role, status, joined_at)
		VALUES (?, ?, ?, 'member', 'joined', ?)`, atAgent1, atTestOrg, "agent:"+atAgent1, now); err != nil {
		t.Fatal(err)
	}
}

func TestT1413AgentCreateTaskEnforceUsesSharedAuthorization(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	projectID, _ := f.seedMemberProject(t)
	f.deps.Authorizer = authz.New(authz.Deps{DB: f.db, Mode: authz.EnforcementEnforce})
	_, err := f.db.Exec(`DELETE FROM authorization_role_permissions WHERE role_id='sys-project-member' AND permission_key='project.write'`)
	if err != nil {
		t.Fatal(err)
	}
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_task", "acat_w1", map[string]any{
		"agent_id":   atAgent1,
		"project_id": string(projectID),
		"title":      "blocked by RAM",
	})
	if status != http.StatusForbidden || body["error"] != "permission_denied" {
		t.Fatalf("create_task status=%d body=%v, want shared authz 403", status, body)
	}
}

func TestT1413AgentPlanWriteEnforceRevokesPlanTools(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	projectID, _ := f.seedMemberProject(t)
	f.deps.Authorizer = authz.New(authz.Deps{DB: f.db, Mode: authz.EnforcementEnforce})
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_plan", "acat_w1", map[string]any{
		"agent_id": atAgent1, "project_id": string(projectID), "name": "RAM plan", "owner_ref": "agent:" + atAgent1,
	})
	if status != http.StatusOK {
		t.Fatalf("create_plan status=%d body=%v", status, body)
	}
	planID, _ := body["plan_id"].(string)
	if planID == "" {
		t.Fatalf("create_plan returned no plan_id: %v", body)
	}
	if _, err := f.db.Exec(`DELETE FROM authorization_role_permissions WHERE role_id='sys-project-member' AND permission_key='project.write'`); err != nil {
		t.Fatal(err)
	}

	status, body = postBearer(t, srv.URL, "/admin/agent-tools/create_stage", "acat_w1", map[string]any{
		"agent_id": atAgent1, "plan_id": planID, "name": "blocked stage", "acceptance_contract": "must pass",
	})
	if status != http.StatusForbidden || body["error"] != "permission_denied" {
		t.Fatalf("create_stage status=%d body=%v, want shared authz 403", status, body)
	}
}

func TestT1413AgentTeamCreateEnforceUsesOrgScope(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	seedT1413AgentOrgMember(t, f)
	f.deps.Authorizer = authz.New(authz.Deps{DB: f.db, Mode: authz.EnforcementEnforce})
	srv, _ := wireTeam(t, f)
	if _, err := f.db.Exec(`DELETE FROM authorization_role_permissions WHERE role_id='sys-org-member' AND permission_key='team.create'`); err != nil {
		t.Fatal(err)
	}

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_team", "acat_w1", map[string]any{
		"agent_id": atAgent1, "name": "blocked-team",
	})
	if status != http.StatusForbidden || body["error"] != "permission_denied" {
		t.Fatalf("create_team status=%d body=%v, want org-scoped shared authz 403", status, body)
	}
}

func TestT1413AgentTeamMemberManageEnforceRevokesTeamTools(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	seedT1413AgentOrgMember(t, f)
	f.deps.Authorizer = authz.New(authz.Deps{DB: f.db, Mode: authz.EnforcementEnforce})
	srv, _ := wireTeam(t, f)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_team", "acat_w1", map[string]any{
		"agent_id": atAgent1, "name": "team-members", "roles": []map[string]any{{"role": "dev"}},
	})
	if status != http.StatusCreated {
		t.Fatalf("create_team status=%d body=%v", status, body)
	}
	teamID, _ := body["id"].(string)
	if teamID == "" {
		t.Fatalf("create_team returned no id: %v", body)
	}
	if _, err := f.db.Exec(`DELETE FROM authorization_role_permissions WHERE permission_key='team.member.manage'`); err != nil {
		t.Fatal(err)
	}

	status, body = postBearer(t, srv.URL, "/admin/agent-tools/add_member", "acat_w1", map[string]any{
		"agent_id": atAgent1, "team_id": teamID, "member_ref": "agent:blocked", "role": "dev",
	})
	if status != http.StatusForbidden || body["error"] != "permission_denied" {
		t.Fatalf("add_member status=%d body=%v, want team-scoped shared authz 403", status, body)
	}
}
