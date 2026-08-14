package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authz "github.com/oopslink/agent-center/internal/authorization"
)

func TestPermissionsHTTP_CheckAndEffectiveUseServerAuthorizer(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.Authorizer = authz.New(authz.Deps{DB: db})
	sess := setupTestSession(t, db, deps)
	srv := NewServer("127.0.0.1:0", Deps{})
	ts := httptest.NewServer(WithDeps(deps)(srv.Handler()))
	defer ts.Close()

	body := `{"permission":"org.read","resource":{"kind":"org","id":"` + sess.OrgID + `"}}`
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/orgs/"+sess.OrgSlug+"/permissions/check", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(sess.Cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("check status=%d", resp.StatusCode)
	}
	var decision authz.AccessDecision
	if err := json.NewDecoder(resp.Body).Decode(&decision); err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed || decision.Source != authz.SourceOrgRole {
		t.Fatalf("decision=%#v", decision)
	}

	req, err = http.NewRequest(http.MethodGet, ts.URL+"/api/orgs/"+sess.OrgSlug+"/permissions/effective?resource_kind=org&resource_id="+sess.OrgID, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(sess.Cookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("effective status=%d", resp.StatusCode)
	}
	var effective authz.EffectivePermissions
	if err := json.NewDecoder(resp.Body).Decode(&effective); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, p := range effective.Permissions {
		if p.Key == "org.read" {
			found = true
		}
	}
	if !found {
		t.Fatalf("effective permissions missing org.read: %#v", effective.Permissions)
	}
}
