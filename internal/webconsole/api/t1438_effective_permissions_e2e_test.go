package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/team"
)

func TestT1438WebPermissionCheckTeamMembershipAllowDenyRevoke(t *testing.T) {
	deps, db, sess := setupTeamsAPI(t)
	deps.Authorizer = authz.New(authz.Deps{DB: db, Mode: authz.EnforcementEnforce})
	now := time.Now().UTC().Format(time.RFC3339Nano)
	projectID := "project-t1438-web"
	if _, err := db.Exec(`
		INSERT INTO pm_projects (id, organization_id, name, description, status, created_by, created_at, updated_at, version)
		VALUES (?, ?, 'T1438 Web', '', 'active', 'system', ?, ?, 1)`, projectID, sess.OrgID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO authorization_roles (id, org_id, kind, name, description, created_by, created_at, updated_at, version)
		VALUES ('role-t1438-web-reader', ?, 'custom', 'T1438 Web Reader', '', 'system', ?, ?, 1)`, sess.OrgID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO authorization_role_permissions (role_id, permission_key, resource_kind, delegatable, created_at)
		VALUES ('role-t1438-web-reader', 'project.read', 'project', 0, ?)`, now); err != nil {
		t.Fatal(err)
	}
	tm := seedTeam(t, deps, sess.OrgID, "T1438 Web Team", []team.RoleConfig{{Role: "reader", CLI: "codex", Model: "gpt-5", MaxConcurrency: 1}})
	if _, err := db.Exec(`INSERT INTO team_projects (team_id, project_id, created_at) VALUES (?, ?, ?)`, tm.ID().String(), projectID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO team_members (team_id, member_ref, member_kind, role, created_at) VALUES (?, ?, 'human', 'reader', ?)`, tm.ID().String(), authz.UserSubject(sess.IdentityID), now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO team_role_ram_role_mappings (team_id, team_role, ram_role_id, created_at, created_by) VALUES (?, 'reader', 'role-t1438-web-reader', ?, 'system')`, tm.ID().String(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO team_role_ram_role_versions (team_id, team_role, version, updated_at, updated_by) VALUES (?, 'reader', 1, ?, 'system')`, tm.ID().String(), now); err != nil {
		t.Fatal(err)
	}
	ts := newTestServer(t, deps)
	defer ts.Close()

	body := `{"permission":"project.read","resource":{"kind":"project","id":"` + projectID + `"}}`
	resp := orgScopedPost(t, ts.URL+"/api/permissions/check", body, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("permission check allow=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	_ = decodeBody(t, resp)

	if _, err := db.Exec(`DELETE FROM team_members WHERE team_id = ? AND member_ref = ?`, tm.ID().String(), authz.UserSubject(sess.IdentityID)); err != nil {
		t.Fatal(err)
	}
	resp = orgScopedPost(t, ts.URL+"/api/permissions/check", body, sess)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("permission check after membership revoke=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	denied := decodeBody(t, resp)
	if !strings.Contains(denied["error"].(string), "permission_denied") {
		t.Fatalf("denied body=%v", denied)
	}
}
