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

func TestPermissionsHTTP_AccessGraphRoutePaginationRedactionAndRollbackSwitch(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	deps.Authorizer = authz.New(authz.Deps{DB: db})
	srv := NewServer("127.0.0.1:0", Deps{})
	ts := httptest.NewServer(WithDeps(deps)(srv.Handler()))
	defer ts.Close()

	resp := orgScopedGet(t, ts.URL+"/api/permissions/access-graph?layer=permissions&limit=1", sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("access graph status=%d", resp.StatusCode)
	}
	var graph authz.AccessGraphPage
	if err := json.NewDecoder(resp.Body).Decode(&graph); err != nil {
		t.Fatal(err)
	}
	if graph.Complete || !graph.Completeness.HasMore || graph.NextCursor == "" {
		t.Fatalf("graph completeness = %#v complete=%v next=%q", graph.Completeness, graph.Complete, graph.NextCursor)
	}
	if len(graph.Permissions) != 1 {
		t.Fatalf("graph permissions len=%d", len(graph.Permissions))
	}
	if !graph.Permissions[0].Evidence.Redacted || graph.Permissions[0].Evidence.Ref != "members:redacted" {
		t.Fatalf("graph evidence not redacted: %#v", graph.Permissions[0].Evidence)
	}
	if graph.ParityShadow.Checked == 0 || graph.ParityShadow.Mismatches != 0 {
		t.Fatalf("graph parity = %#v", graph.ParityShadow)
	}

	disabledDeps := deps
	disabledDeps.AccessGovernanceReadModelDisabled = true
	disabledSrv := NewServer("127.0.0.1:0", Deps{})
	disabledTS := httptest.NewServer(WithDeps(disabledDeps)(disabledSrv.Handler()))
	defer disabledTS.Close()
	disabledResp := orgScopedGet(t, disabledTS.URL+"/api/permissions/access-graph", sess)
	defer disabledResp.Body.Close()
	if disabledResp.StatusCode != http.StatusNotFound {
		t.Fatalf("disabled access graph status=%d, want 404", disabledResp.StatusCode)
	}
}

func TestPermissionsHTTP_ExplainVisibilityGateCrossOrg404AndRedactsEvidence(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	deps.Authorizer = authz.New(authz.Deps{DB: db})
	insertOtherHTTPOrg(t, db, "org-http-visibility-other")
	srv := NewServer("127.0.0.1:0", Deps{})
	ts := httptest.NewServer(WithDeps(deps)(srv.Handler()))
	defer ts.Close()

	cross := orgScopedPost(t, ts.URL+"/api/permissions/explain",
		`{"permission":"org.read","resource":{"kind":"org","id":"org-http-visibility-other"}}`, sess)
	defer cross.Body.Close()
	if cross.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-org explain status=%d, want 404", cross.StatusCode)
	}
	var crossBody map[string]any
	if err := json.NewDecoder(cross.Body).Decode(&crossBody); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(crossBody)
	if strings.Contains(string(raw), "org-http-visibility-other") {
		t.Fatalf("cross-org explain leaked target org id: %s", raw)
	}

	own := orgScopedPost(t, ts.URL+"/api/permissions/explain",
		`{"permission":"org.read","resource":{"kind":"org","id":"`+sess.OrgID+`"}}`, sess)
	defer own.Body.Close()
	if own.StatusCode != http.StatusOK {
		t.Fatalf("own explain status=%d", own.StatusCode)
	}
	var explain authz.ExplainResult
	if err := json.NewDecoder(own.Body).Decode(&explain); err != nil {
		t.Fatal(err)
	}
	if !explain.Decision.Allowed || explain.Decision.EvidenceRef != "members:redacted" {
		t.Fatalf("own explain decision not redacted/allowed: %#v", explain.Decision)
	}
	for _, eff := range explain.Effective {
		if strings.HasPrefix(eff.EvidenceRef, "members:mem-") {
			t.Fatalf("raw evidence leaked in effective: %#v", eff)
		}
	}
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

func insertOtherHTTPOrg(t *testing.T, db *sql.DB, orgID string) {
	t.Helper()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	execHTTPAuthz(t, db,
		`INSERT INTO organizations (id, slug, name, created_by_identity_id, created_at, updated_at)
		 VALUES (?, ?, 'Other Org', 'testuser', ?, ?)`,
		orgID, orgID, now, now,
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
