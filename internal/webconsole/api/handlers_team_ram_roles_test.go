package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/team"
	teamservice "github.com/oopslink/agent-center/internal/team/service"
)

func TestTeamRAMRoleMappingHTTP_ReplacePreviewAndCAS(t *testing.T) {
	deps, db, sess := setupTeamsAPI(t)
	if _, err := db.Exec(`INSERT INTO authorization_roles (id,org_id,kind,name,description,created_by,created_at,updated_at,version) VALUES ('role-contributor',?,'custom','Contributor','','user:owner',datetime('now'),datetime('now'),1)`, sess.OrgID); err != nil {
		t.Fatal(err)
	}
	tm := seedTeam(t, deps, sess.OrgID, "Mapped", implRole)
	ts := newTestServer(t, deps)
	defer ts.Close()
	path := ts.URL + "/api/teams/" + tm.ID().String() + "/roles/impl/ram-roles"

	resp := orgScopedPut(t, path, []byte(`{"ram_role_ids":["role-contributor"],"expected_version":1}`), sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replace=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	body := decodeBody(t, resp)
	if body["version"] != float64(2) {
		t.Fatalf("replace body=%#v", body)
	}

	resp = orgScopedPost(t, path+"/preview", `{"ram_role_ids":[]}`, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	preview := decodeBody(t, resp)
	removed, _ := preview["removed_ram_role_ids"].([]any)
	if len(removed) != 1 || removed[0] != "role-contributor" {
		t.Fatalf("preview=%#v", preview)
	}

	resp = orgScopedPut(t, path, []byte(`{"ram_role_ids":[],"expected_version":1}`), sess)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("stale replace=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	resp = orgScopedGet(t, path, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get=%d", resp.StatusCode)
	}
	defer resp.Body.Close()
	var mapping map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&mapping); err != nil {
		t.Fatal(err)
	}
	ids := mapping["ram_role_ids"].([]any)
	if mapping["version"] != float64(2) || len(ids) != 1 {
		t.Fatalf("stale write changed mapping: %#v", mapping)
	}
}

func TestTeamRAMRoleMappingHTTP_RejectsCrossOrgRole(t *testing.T) {
	deps, db, sess := setupTeamsAPI(t)
	if _, err := db.Exec(`INSERT INTO authorization_roles (id,org_id,kind,name,description,created_by,created_at,updated_at,version) VALUES ('role-other','org-other','custom','Other','','user:owner',datetime('now'),datetime('now'),1)`); err != nil {
		t.Fatal(err)
	}
	tm := seedTeam(t, deps, sess.OrgID, "Mapped", implRole)
	ts := newTestServer(t, deps)
	defer ts.Close()
	resp := orgScopedPut(t, ts.URL+"/api/teams/"+tm.ID().String()+"/roles/impl/ram-roles", []byte(`{"ram_role_ids":["role-other"],"expected_version":1}`), sess)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("cross-org=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
}

func TestTeamBasicRAMRoleHTTP_CreatePreviewReplaceAndImmediateAuthz(t *testing.T) {
	deps, db, sess := setupTeamsAPI(t)
	deps.Authorizer = authz.New(authz.Deps{DB: db, Mode: authz.EnforcementEnforce})
	ts := newTestServer(t, deps)
	defer ts.Close()

	rolesResp := orgScopedGet(t, ts.URL+"/api/access/ram-roles", sess)
	if rolesResp.StatusCode != http.StatusOK {
		t.Fatalf("ram roles=%d body=%v", rolesResp.StatusCode, decodeBody(t, rolesResp))
	}
	rolesBody := decodeBody(t, rolesResp)
	foundTeamBasic := false
	for _, raw := range rolesBody["roles"].([]any) {
		p := raw.(map[string]any)
		if p["id"] == "team-basic" && p["name"] == "Team basic" && p["version"] == float64(1) {
			foundTeamBasic = true
		}
	}
	if !foundTeamBasic {
		t.Fatalf("/access/ram-roles did not expose Team basic v1: %#v", rolesBody)
	}

	create := orgScopedPost(t, ts.URL+"/api/teams", `{"name":"Team Basic Contract","description":"ram","visibility":"org-private","roles":[{"role":"dev","cli":"claude-code","model":"claude-sonnet-5","max_concurrency":1,"count":1,"tags":"","ram_role_keys":["Team basic"],"access_requirements":["team.read","team.memory.read"]}]}`, sess)
	if create.StatusCode != http.StatusCreated {
		t.Fatalf("create=%d body=%v", create.StatusCode, decodeBody(t, create))
	}
	created := decodeBody(t, create)
	teamID := created["id"].(string)
	roles := created["roles"].([]any)
	keys := roles[0].(map[string]any)["ram_role_keys"].([]any)
	if len(keys) != 1 || keys[0] != "Team basic" {
		t.Fatalf("created ram_role_keys=%#v", keys)
	}

	path := ts.URL + "/api/teams/" + teamID + "/roles/dev/ram-roles"
	preview := orgScopedPost(t, path+"/preview", `{"ram_role_ids":["team-basic"]}`, sess)
	if preview.StatusCode != http.StatusOK {
		t.Fatalf("preview=%d body=%v", preview.StatusCode, decodeBody(t, preview))
	}
	previewBody := decodeBody(t, preview)
	if got := previewBody["next_ram_role_ids"].([]any); len(got) != 1 || got[0] != "team-basic" {
		t.Fatalf("preview next ids=%#v", got)
	}
	replace := orgScopedPut(t, path, []byte(`{"ram_role_ids":["team-basic"],"expected_version":1}`), sess)
	if replace.StatusCode != http.StatusOK {
		t.Fatalf("replace=%d body=%v", replace.StatusCode, decodeBody(t, replace))
	}
	replaced := decodeBody(t, replace)
	if replaced["version"] != float64(2) {
		t.Fatalf("replace version body=%#v", replaced)
	}

	ramSubject := "user:ram-contract-subject"
	if _, err := deps.TeamService.AddMember(context.Background(), team.TeamID(teamID), team.MemberRef(ramSubject), "dev"); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if got, err := deps.Authorizer.Check(context.Background(), authz.CheckRequest{
		SubjectRef: authz.SubjectRef(ramSubject),
		Transport:  authz.TransportWeb,
		Permission: "team.read",
		Resource:   authz.ResourceScope{Kind: "team", ID: teamID, OrgID: sess.OrgID},
	}); err != nil || !got.Allowed || got.Source != authz.SourceTeamRoleRAM {
		t.Fatalf("team-basic should grant team.read immediately, decision=%#v err=%v", got, err)
	}
	otherTeam, err := deps.TeamService.CreateTeam(context.Background(), teamservice.CreateTeamInput{
		OrgID: sess.OrgID,
		Name:  "Other Team",
		Roles: []team.RoleConfig{{Role: "dev", CLI: "claude-code", Model: "claude-sonnet-5", MaxConcurrency: 1}},
	})
	if err != nil {
		t.Fatalf("create other team: %v", err)
	}
	if got, err := deps.Authorizer.Check(context.Background(), authz.CheckRequest{
		SubjectRef: authz.SubjectRef(ramSubject),
		Transport:  authz.TransportWeb,
		Permission: "team.read",
		Resource:   authz.ResourceScope{Kind: "team", ID: otherTeam.ID().String(), OrgID: sess.OrgID},
	}); !errors.Is(err, authz.ErrDenied) || got.Allowed {
		t.Fatalf("cross-team scope must fail closed, decision=%#v err=%v", got, err)
	}

	revoke := orgScopedPut(t, path, []byte(`{"ram_role_ids":[],"expected_version":2}`), sess)
	if revoke.StatusCode != http.StatusOK {
		t.Fatalf("revoke=%d body=%v", revoke.StatusCode, decodeBody(t, revoke))
	}
	if got, err := deps.Authorizer.Check(context.Background(), authz.CheckRequest{
		SubjectRef: authz.SubjectRef(ramSubject),
		Transport:  authz.TransportWeb,
		Permission: "team.read",
		Resource:   authz.ResourceScope{Kind: "team", ID: teamID, OrgID: sess.OrgID},
	}); !errors.Is(err, authz.ErrDenied) || got.Allowed {
		t.Fatalf("revoked mapping must fail closed immediately, decision=%#v err=%v", got, err)
	}
}
