package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestPermissionsHTTP_ShadowMetricsEndpoint(t *testing.T) {
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
	if resp, err := http.DefaultClient.Do(req); err != nil {
		t.Fatal(err)
	} else {
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("check status=%d", resp.StatusCode)
		}
	}

	req, err = http.NewRequest(http.MethodGet, ts.URL+"/api/orgs/"+sess.OrgSlug+"/permissions/shadow", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(sess.Cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("shadow status=%d", resp.StatusCode)
	}
	var bodyOut struct {
		Mode    string              `json:"mode"`
		Metrics authz.ShadowMetrics `json:"metrics"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&bodyOut); err != nil {
		t.Fatal(err)
	}
	if bodyOut.Mode != string(authz.EnforcementShadow) || bodyOut.Metrics.Checks == 0 {
		t.Fatalf("shadow payload = %+v", bodyOut)
	}
}

func TestPermissionsHTTP_CrossOrgRevokeMustFailClosed(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	seedHTTPAuthzAssignment(t, db, "org-http-other", "role-http-cross", "asgn-http-cross")
	deps.Authorizer = authz.New(authz.Deps{DB: db})
	srv := NewServer("127.0.0.1:0", Deps{})
	ts := httptest.NewServer(WithDeps(deps)(srv.Handler()))
	defer ts.Close()

	body := `{"idempotency_key":"idem-http-cross","operations":[{"id":"cross","revoke":{"assignment_id":"asgn-http-cross","reason":"cross-org"}}]}`
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/orgs/"+sess.OrgSlug+"/permissions/batch/revoke", strings.NewReader(body))
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
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-org revoke status=%d, want opaque 404", resp.StatusCode)
	}
	var errBody map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&errBody); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(errBody)
	if strings.Contains(string(raw), "org-http-other") {
		t.Fatalf("cross-org revoke response leaked target org: %s", raw)
	}
	var revokedAt sql.NullString
	if err := db.QueryRowContext(context.Background(),
		`SELECT revoked_at FROM authorization_role_assignments WHERE org_id = 'org-http-other' AND id = 'asgn-http-cross'`,
	).Scan(&revokedAt); err != nil {
		t.Fatal(err)
	}
	if revokedAt.Valid && revokedAt.String != "" {
		t.Fatalf("cross-org assignment was revoked via HTTP: %q", revokedAt.String)
	}
}

func TestPermissionsHTTP_SubjectAuditIsOrgScoped(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	deps.Authorizer = authz.New(authz.Deps{DB: db})
	srv := NewServer("127.0.0.1:0", Deps{})
	ts := httptest.NewServer(WithDeps(deps)(srv.Handler()))
	defer ts.Close()

	subject := string(authz.UserSubject(sess.IdentityID))
	insertHTTPAuthzAudit(t, db, "audit-current", subject, "org", sess.OrgID)
	insertHTTPAuthzAudit(t, db, "audit-other", subject, "org", "org-http-other")

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/orgs/"+sess.OrgSlug+"/permissions/audit?subject_ref="+subject+"&limit=10", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(sess.Cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("audit status=%d", resp.StatusCode)
	}
	var body struct {
		Events []authz.AuditEvent `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Events) != 1 || body.Events[0].ID != "audit-current" {
		t.Fatalf("audit events = %#v, want only current org event", body.Events)
	}
}

func TestAccessApplyCompatibilityMutatesUnifiedAuthorizationAndOverview(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	deps.Authorizer = authz.New(authz.Deps{DB: db})
	srv := NewServer("127.0.0.1:0", Deps{})
	ts := httptest.NewServer(WithDeps(deps)(srv.Handler()))
	defer ts.Close()

	assertAccessApplyRoundTrip(t, ts.URL+"/api/access/apply", "org.analytics.read", sess)
	assertAccessApplyRoundTrip(t, ts.URL+"/api/permissions/batch/apply", "ai_runtime.catalog.export", sess)
}

func assertAccessApplyRoundTrip(t *testing.T, endpoint, permission string, sess testSession) {
	t.Helper()
	body := `{
		"subject_refs":["user:` + sess.IdentityID + `"],
		"permission_keys":["` + permission + `"],
		"resources":[{"kind":"org","id":"` + sess.OrgID + `","org_id":"` + sess.OrgID + `","label":"Test Org"}],
		"reason":"regression coverage"
	}`
	resp := orgScopedPost(t, endpoint, body, sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("access apply %s status=%d", endpoint, resp.StatusCode)
	}
	var applied struct {
		Summary struct {
			Succeeded      int  `json:"succeeded"`
			Failed         int  `json:"failed"`
			PartialFailure bool `json:"partial_failure"`
		} `json:"summary"`
		Items []struct {
			Permission string `json:"permission"`
			Status     string `json:"status"`
			GrantID    string `json:"grant_id"`
			Reason     string `json:"reason"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&applied); err != nil {
		t.Fatal(err)
	}
	if applied.Summary.Succeeded != 1 || applied.Summary.Failed != 0 || applied.Summary.PartialFailure {
		t.Fatalf("access apply summary=%+v items=%+v", applied.Summary, applied.Items)
	}
	if len(applied.Items) != 1 || applied.Items[0].Status != "allowed" || applied.Items[0].GrantID == "" {
		t.Fatalf("access apply item=%+v", applied.Items)
	}
	grantID := applied.Items[0].GrantID

	effResp := orgScopedGet(t, strings.Split(endpoint, "/api/")[0]+"/api/permissions/effective?subject_ref=user:"+sess.IdentityID+"&resource_kind=org&resource_id="+sess.OrgID, sess)
	defer effResp.Body.Close()
	if effResp.StatusCode != http.StatusOK {
		t.Fatalf("effective status=%d", effResp.StatusCode)
	}
	var effective authz.EffectivePermissions
	if err := json.NewDecoder(effResp.Body).Decode(&effective); err != nil {
		t.Fatal(err)
	}
	if !hasDirectEffectivePermission(effective.Permissions, permission, grantID) {
		t.Fatalf("effective permissions missing direct custom_role %s/%s: %+v", permission, grantID, effective.Permissions)
	}

	overviewResp := orgScopedGet(t, strings.Split(endpoint, "/api/")[0]+"/api/permissions/effective?view=access&status=allowed&q="+permission, sess)
	defer overviewResp.Body.Close()
	if overviewResp.StatusCode != http.StatusOK {
		t.Fatalf("access overview status=%d", overviewResp.StatusCode)
	}
	var overview struct {
		Decisions []struct {
			Permission  string `json:"permission"`
			Source      string `json:"source"`
			EvidenceRef string `json:"evidence_ref"`
			GrantID     string `json:"grant_id"`
			Status      string `json:"status"`
		} `json:"decisions"`
		Grants []struct {
			ID         string `json:"id"`
			Permission string `json:"permission"`
			Source     string `json:"source"`
		} `json:"grants"`
	}
	if err := json.NewDecoder(overviewResp.Body).Decode(&overview); err != nil {
		t.Fatal(err)
	}
	if !hasDirectAccessDecision(overview.Decisions, permission, grantID) {
		t.Fatalf("access overview missing direct custom_role %s/%s: %+v", permission, grantID, overview.Decisions)
	}
	if !hasDirectAccessGrant(overview.Grants, permission, grantID) {
		t.Fatalf("access overview grants missing direct custom_role %s/%s: %+v", permission, grantID, overview.Grants)
	}
}

func hasDirectEffectivePermission(perms []authz.EffectivePermission, permission, assignmentID string) bool {
	for _, p := range perms {
		if string(p.Key) == permission && p.Source == authz.SourceCustomRole && p.AssignmentID == assignmentID {
			return true
		}
	}
	return false
}

func hasDirectAccessDecision(decisions []struct {
	Permission  string `json:"permission"`
	Source      string `json:"source"`
	EvidenceRef string `json:"evidence_ref"`
	GrantID     string `json:"grant_id"`
	Status      string `json:"status"`
}, permission, grantID string) bool {
	for _, d := range decisions {
		if d.Permission == permission && d.Source == string(authz.SourceCustomRole) && d.GrantID == grantID && d.Status == "allowed" && d.EvidenceRef == "authorization_role_assignments:"+grantID {
			return true
		}
	}
	return false
}

func hasDirectAccessGrant(grants []struct {
	ID         string `json:"id"`
	Permission string `json:"permission"`
	Source     string `json:"source"`
}, permission, grantID string) bool {
	for _, g := range grants {
		if g.ID == grantID && g.Permission == permission && g.Source == string(authz.SourceCustomRole) {
			return true
		}
	}
	return false
}

func seedHTTPAuthzAssignment(t *testing.T, db *sql.DB, orgID, roleID, assignmentID string) {
	t.Helper()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	execHTTPAuthz(t, db,
		`INSERT INTO organizations (id, slug, name, created_by_identity_id, created_at, updated_at)
		 VALUES (?, ?, 'Other Org', 'testuser', ?, ?)`,
		orgID, orgID, now, now,
	)
	execHTTPAuthz(t, db,
		`INSERT INTO authorization_roles (id, org_id, kind, name, description, created_by, created_at, updated_at, version)
		 VALUES (?, ?, 'custom', ?, '', 'system', ?, ?, 1)`,
		roleID, orgID, roleID, now, now,
	)
	execHTTPAuthz(t, db,
		`INSERT INTO authorization_role_permissions (role_id, permission_key, resource_kind, delegatable, created_at)
		 VALUES (?, 'org.read', 'org', 1, ?)`,
		roleID, now,
	)
	execHTTPAuthz(t, db,
		`INSERT INTO authorization_role_assignments
		 (id, org_id, subject_ref, role_id, resource_kind, resource_id, created_by, created_at, version)
		 VALUES (?, ?, 'user:testuser', ?, 'org', ?, 'system', ?, 1)`,
		assignmentID, orgID, roleID, orgID, now,
	)
}

func insertHTTPAuthzAudit(t *testing.T, db *sql.DB, id, subject, resourceKind, resourceID string) {
	t.Helper()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	execHTTPAuthz(t, db,
		`INSERT INTO authorization_audit_events
		 (id, event_type, actor_ref, subject_ref, permission_key, resource_kind, resource_id,
		  role_id, assignment_id, request_id, payload_json, created_at)
		 VALUES (?, 'authorization.assignment.created', 'user:testuser', ?, 'org.read', ?, ?,
		  'role-audit', ?, '', '{}', ?)`,
		id, subject, resourceKind, resourceID, id, now,
	)
}

func execHTTPAuthz(t *testing.T, db *sql.DB, stmt string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), stmt, args...); err != nil {
		t.Fatalf("exec %s: %v", stmt, err)
	}
}
