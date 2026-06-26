package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// v2.7.1 #214: GET /api/users/{user_id} returns the user profile (member-id path)
// + org memberships with role, NO activity stream. The test user has no email →
// validates the explicit-null shape (Tester seam contract).
func TestUserDetailHandler(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	// /api/users/{user_id} is org-agnostic (exempt): authenticated only, bare path.
	req, _ := http.NewRequest(http.MethodGet, s.URL+"/api/users/"+sess.IdentityID, nil)
	req.AddCookie(sess.Cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["user_id"] != sess.IdentityID {
		t.Errorf("user_id=%v want %s", body["user_id"], sess.IdentityID)
	}
	if body["display_name"] != "testuser" {
		t.Errorf("display_name=%v want testuser", body["display_name"])
	}
	// no email / no signin yet → explicit JSON null (present key, nil value).
	if v, ok := body["email"]; !ok || v != nil {
		t.Errorf("email = %v (present=%v), want explicit null", v, ok)
	}
	if v, ok := body["last_session_at"]; !ok || v != nil {
		t.Errorf("last_session_at = %v (present=%v), want explicit null", v, ok)
	}
	if _, ok := body["created_at"].(string); !ok {
		t.Errorf("created_at missing/!string: %v", body["created_at"])
	}
	// orgs: [{org_id, role}] — the session org with owner role.
	orgs, ok := body["orgs"].([]any)
	if !ok || len(orgs) != 1 {
		t.Fatalf("orgs = %v, want 1 entry", body["orgs"])
	}
	o0 := orgs[0].(map[string]any)
	if o0["org_id"] != sess.OrgID || o0["role"] != "owner" {
		t.Errorf("org entry = %v, want {org_id:%s, role:owner}", o0, sess.OrgID)
	}
	// T478 #1: org_name + org_slug are emitted so the UI shows a stable "name + id"
	// for any membership (not just orgs the viewer happens to share).
	if name, ok := o0["org_name"].(string); !ok || name == "" {
		t.Errorf("org_name = %v, want non-empty string", o0["org_name"])
	}
	if slug, ok := o0["org_slug"].(string); !ok || slug == "" {
		t.Errorf("org_slug = %v, want non-empty string", o0["org_slug"])
	}
}

// v2.7.1 #214: unknown / non-user id → 404.
func TestUserDetailHandler_NotFound(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedGet(t, s.URL+"/api/users/user-deadbeef", sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d want 404", resp.StatusCode)
	}
}
