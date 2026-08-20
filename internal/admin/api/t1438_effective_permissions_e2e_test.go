package api

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	authz "github.com/oopslink/agent-center/internal/authorization"
)

func seedT1438TeamRAMProjectWrite(t *testing.T, f *writeToolsFixture, projectID string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := f.db.Exec(`
		INSERT INTO authorization_roles (id, org_id, kind, name, description, created_by, created_at, updated_at, version)
		VALUES ('role-t1438-project-writer', ?, 'custom', 'T1438 project writer', '', 'system', ?, ?, 1)`,
		atTestOrg, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`
		INSERT INTO authorization_role_permissions (role_id, permission_key, resource_kind, delegatable, created_at)
		VALUES ('role-t1438-project-writer', 'project.write', 'project', 0, ?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`
		INSERT INTO teams (id, org_id, name, description, created_at, updated_at, version)
		VALUES ('team-t1438-mcp', ?, 'T1438 MCP', '', ?, ?, 1)`, atTestOrg, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`
		INSERT INTO team_roles (team_id, role, cli, model, capability_tags, max_concurrency, created_at, access_requirements)
		VALUES ('team-t1438-mcp', 'writer', '', '', '[]', 1, ?, '["project.write"]')`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`
		INSERT INTO team_members (team_id, member_ref, member_kind, role, created_at)
		VALUES ('team-t1438-mcp', ?, 'agent', 'writer', ?)`, "agent:"+atAgent1, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO team_projects (team_id, project_id, created_at) VALUES ('team-t1438-mcp', ?, ?)`, projectID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`
		INSERT INTO team_role_ram_role_mappings (team_id, team_role, ram_role_id, created_at, created_by)
		VALUES ('team-t1438-mcp', 'writer', 'role-t1438-project-writer', ?, 'system')`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`
		INSERT INTO team_role_ram_role_versions (team_id, team_role, version, updated_at, updated_by)
		VALUES ('team-t1438-mcp', 'writer', 1, ?, 'system')`, now); err != nil {
		t.Fatal(err)
	}
}

func TestT1438MCPCreateTaskRoleMappingAllowThenRevokeDeny(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	seedT1413AgentOrgMember(t, f)
	projectID, _ := f.seedMemberProject(t)
	if _, err := f.db.Exec(`DELETE FROM authorization_role_permissions WHERE role_id='sys-project-member' AND permission_key='project.write'`); err != nil {
		t.Fatal(err)
	}
	seedT1438TeamRAMProjectWrite(t, f, string(projectID))
	f.deps.Authorizer = authz.New(authz.Deps{DB: f.db, Mode: authz.EnforcementEnforce})
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_task", "acat_w1", map[string]any{
		"agent_id": atAgent1, "project_id": string(projectID), "title": "allowed by team RAM",
	})
	if status != http.StatusOK {
		t.Fatalf("create_task before mapping revoke status=%d body=%v", status, body)
	}

	if _, err := f.db.Exec(`DELETE FROM team_role_ram_role_mappings WHERE team_id='team-t1438-mcp' AND team_role='writer'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE team_role_ram_role_versions SET version=version+1, updated_at=? WHERE team_id='team-t1438-mcp' AND team_role='writer'`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	status, body = postBearer(t, srv.URL, "/admin/agent-tools/create_task", "acat_w1", map[string]any{
		"agent_id": atAgent1, "project_id": string(projectID), "title": "denied after mapping revoke",
	})
	if status != http.StatusForbidden || body["error"] != "permission_denied" {
		t.Fatalf("create_task after mapping revoke status=%d body=%v, want 403", status, body)
	}
}

func TestT1438MCPCreateTaskDirectBindingAllowThenAuditedRevokeDeny(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	seedT1413AgentOrgMember(t, f)
	projectID, _ := f.seedMemberProject(t)
	if _, err := f.db.Exec(`DELETE FROM authorization_role_permissions WHERE role_id='sys-project-member' AND permission_key='project.write'`); err != nil {
		t.Fatal(err)
	}
	svc := authz.New(authz.Deps{DB: f.db, Mode: authz.EnforcementEnforce})
	f.deps.Authorizer = svc
	if _, err := svc.ApplyBatch(context.Background(), authz.BatchRequest{
		IdempotencyKey: "t1438-direct-grant",
		ActorRef:       "system",
		OrgID:          atTestOrg,
		Operations: []authz.BatchOperation{
			{Type: "upsert_role", Role: authz.RoleInput{ID: "role-t1438-direct-writer", Name: "T1438 direct writer"}},
			{Type: "set_role_permissions", Role: authz.RoleInput{ID: "role-t1438-direct-writer"}, Permissions: []authz.RolePermissionInput{{PermissionKey: "project.write", ResourceKind: "project"}}},
			{Type: "assign_role", Assignment: authz.AssignmentInput{ID: "asgn-t1438-direct-writer", SubjectRef: authz.SubjectRef("agent:" + atAgent1), RoleID: "role-t1438-direct-writer", Resource: authz.ResourceScope{Kind: "project", ID: string(projectID)}}},
		},
	}); err != nil {
		t.Fatalf("direct grant: %v", err)
	}
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_task", "acat_w1", map[string]any{
		"agent_id": atAgent1, "project_id": string(projectID), "title": "allowed by direct binding",
	})
	if status != http.StatusOK {
		t.Fatalf("create_task before direct revoke status=%d body=%v", status, body)
	}

	if _, err := svc.RevokeBatch(context.Background(), authz.BatchRequest{
		IdempotencyKey: "t1438-direct-revoke",
		ActorRef:       "system",
		OrgID:          atTestOrg,
		Operations:     []authz.BatchOperation{{ID: "revoke-direct", Revoke: authz.RevokeInput{AssignmentID: "asgn-t1438-direct-writer", Reason: "t1438 e2e revoke"}}},
	}); err != nil {
		t.Fatalf("direct revoke: %v", err)
	}
	if _, err := svc.Check(context.Background(), authz.CheckRequest{SubjectRef: authz.SubjectRef("agent:" + atAgent1), Transport: authz.TransportMCP, Permission: "project.write", Resource: authz.ResourceScope{Kind: "project", ID: string(projectID), OrgID: atTestOrg}}); !errors.Is(err, authz.ErrDenied) {
		t.Fatalf("direct revoke did not invalidate resolver cache immediately: %v", err)
	}
	status, body = postBearer(t, srv.URL, "/admin/agent-tools/create_task", "acat_w1", map[string]any{
		"agent_id": atAgent1, "project_id": string(projectID), "title": "denied after direct revoke",
	})
	if status != http.StatusForbidden || body["error"] != "permission_denied" {
		t.Fatalf("create_task after direct revoke status=%d body=%v, want 403", status, body)
	}
	var auditCount int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM authorization_audit_events WHERE event_type='authorization.assignment.revoked' AND assignment_id='asgn-t1438-direct-writer'`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("revoke audit count=%d want 1", auditCount)
	}
}
