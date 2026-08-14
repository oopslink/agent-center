package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	authz "github.com/oopslink/agent-center/internal/authorization"
)

func TestT1344AccessApplyEntrypointMustMutateUnifiedStateOrBeRemoved(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	deps.Authorizer = authz.New(authz.Deps{DB: db})
	seedT1344JoinedHuman(t, db, sess.OrgID, "grantuser", "Grant User")
	server := newTestServer(t, deps)
	defer server.Close()

	body := `{
		"subject_refs":["user:grantuser"],
		"permission_keys":["org.settings.manage"],
		"resources":[{"kind":"org","id":"` + sess.OrgID + `","org_id":"` + sess.OrgID + `","label":"Test Org"}],
		"reason":"t1344 consistency"
	}`
	resp := orgScopedPost(t, server.URL+"/api/access/batch/apply", body, sess)
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		applyUnifiedBatch(t, server.URL, sess, sess.OrgID, "grantuser", "t1344-new-entrypoint")
		assertEffectiveHas(t, server.URL, sess, "user:grantuser", sess.OrgID, "org.settings.manage")
		return
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("legacy access apply status=%d", resp.StatusCode)
	}
	var applied struct {
		Summary struct {
			Succeeded int `json:"succeeded"`
			Failed    int `json:"failed"`
		} `json:"summary"`
		Items []struct {
			Status  string `json:"status"`
			GrantID string `json:"grant_id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&applied); err != nil {
		t.Fatal(err)
	}
	if applied.Summary.Succeeded != 1 || applied.Summary.Failed != 0 || applied.Items[0].Status != "allowed" {
		t.Fatalf("legacy access apply did not report success: %#v", applied)
	}

	assertEffectiveHas(t, server.URL, sess, "user:grantuser", sess.OrgID, "org.settings.manage")
}

func TestT1344AccessOverviewMustIncludeCustomRolePermissionsOrBeRemoved(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	svc := authz.New(authz.Deps{DB: db})
	deps.Authorizer = svc
	seedT1344JoinedHuman(t, db, sess.OrgID, "customuser", "Custom User")
	server := newTestServer(t, deps)
	defer server.Close()

	if _, err := svc.ApplyBatch(context.Background(), authz.BatchRequest{
		IdempotencyKey: "t1344-web-custom-role",
		ActorRef:       authz.UserSubject(sess.IdentityID),
		OrgID:          sess.OrgID,
		Operations: []authz.BatchOperation{
			{Type: "upsert_role", Role: authz.RoleInput{ID: "role-t1344-settings", Name: "settings-manager"}},
			{Type: "set_role_permissions", Role: authz.RoleInput{ID: "role-t1344-settings"}, Permissions: []authz.RolePermissionInput{{PermissionKey: "org.settings.manage", ResourceKind: "org"}}},
			{Type: "assign_role", Assignment: authz.AssignmentInput{ID: "asgn-t1344-settings", SubjectRef: "user:customuser", RoleID: "role-t1344-settings", Resource: authz.ResourceScope{Kind: "org", ID: sess.OrgID}}},
		},
	}); err != nil {
		t.Fatalf("custom role setup: %v", err)
	}
	resp := orgScopedGet(t, server.URL+"/api/permissions/effective?subject_ref=user:customuser&resource_kind=org&resource_id="+sess.OrgID, sess)
	defer resp.Body.Close()
	var effective authz.EffectivePermissions
	if err := json.NewDecoder(resp.Body).Decode(&effective); err != nil {
		t.Fatal(err)
	}
	if !effectiveHas(effective, "org.settings.manage") {
		t.Fatalf("setup did not grant custom permission: %#v", effective.Permissions)
	}

	resp = orgScopedGet(t, server.URL+"/api/access/overview?q=customuser", sess)
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		resp = orgScopedGet(t, server.URL+"/api/permissions/effective?view=access&q=customuser", sess)
		defer resp.Body.Close()
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("access overview status=%d", resp.StatusCode)
	}
	var overview struct {
		Decisions []struct {
			SubjectRef string `json:"subject_ref"`
			Permission string `json:"permission"`
			Source     string `json:"source"`
			Status     string `json:"status"`
		} `json:"decisions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&overview); err != nil {
		t.Fatal(err)
	}
	for _, decision := range overview.Decisions {
		if decision.SubjectRef == "user:customuser" && decision.Permission == "org.settings.manage" && decision.Source == "custom_role" && decision.Status == "allowed" {
			return
		}
	}
	t.Fatalf("access overview omitted custom_role permission that /permissions effective exposes: %#v", overview.Decisions)
}

func applyUnifiedBatch(t *testing.T, serverURL string, sess testSession, orgID, identityID, idem string) {
	t.Helper()
	body := `{
		"idempotency_key":"` + idem + `",
		"operations":[
			{"id":"role","type":"upsert_role","role":{"id":"role-t1344-settings","name":"settings-manager"}},
			{"id":"perms","type":"set_role_permissions","role":{"id":"role-t1344-settings"},"permissions":[{"permission_key":"org.settings.manage","resource_kind":"org"}]},
			{"id":"assign","type":"assign_role","assignment":{"id":"asgn-t1344-settings-` + identityID + `","subject_ref":"user:` + identityID + `","role_id":"role-t1344-settings","resource":{"kind":"org","id":"` + orgID + `"}}}
		]
	}`
	resp := orgScopedPost(t, serverURL+"/api/permissions/batch/apply", body, sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unified permissions apply status=%d", resp.StatusCode)
	}
	var applied authz.BatchResult
	if err := json.NewDecoder(resp.Body).Decode(&applied); err != nil {
		t.Fatal(err)
	}
	if len(applied.Operations) != 3 || applied.Operations[2].Status != "created" {
		t.Fatalf("unified permissions apply result = %#v", applied)
	}
}

func assertEffectiveHas(t *testing.T, serverURL string, sess testSession, subjectRef, orgID string, key authz.PermissionKey) {
	t.Helper()
	resp := orgScopedGet(t, serverURL+"/api/permissions/effective?subject_ref="+subjectRef+"&resource_kind=org&resource_id="+orgID, sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("permissions effective status=%d", resp.StatusCode)
	}
	var effective authz.EffectivePermissions
	if err := json.NewDecoder(resp.Body).Decode(&effective); err != nil {
		t.Fatal(err)
	}
	if !effectiveHas(effective, key) {
		t.Fatalf("access apply reported success but /permissions effective did not include %s: %#v", key, effective.Permissions)
	}
}

func effectiveHas(effective authz.EffectivePermissions, key authz.PermissionKey) bool {
	for _, permission := range effective.Permissions {
		if permission.Key == key {
			return true
		}
	}
	return false
}

func seedT1344JoinedHuman(t *testing.T, db *sql.DB, orgID, identityID, name string) {
	t.Helper()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	if _, err := db.ExecContext(context.Background(), `INSERT INTO identities (id, kind, display_name, passcode_hash, created_at, updated_at)
		VALUES (?, 'user', ?, 'x', ?, ?)`, identityID, name, now, now); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO members (id, organization_id, identity_id, role, status, joined_at)
		VALUES (?, ?, ?, 'member', 'joined', ?)`, "member-"+identityID, orgID, identityID, now); err != nil {
		t.Fatalf("seed member: %v", err)
	}
}
