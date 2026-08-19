package api

import (
	"encoding/json"
	"net/http"
	"testing"
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
