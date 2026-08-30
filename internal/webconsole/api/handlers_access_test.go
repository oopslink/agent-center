package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/team"
)

func TestAccessFilterDecisionsSupportsCanonicalSubjectProjectPermissionFilters(t *testing.T) {
	decisions := []accessDecisionDTO{
		{SubjectRef: "user:alice", Permission: "project.write", Resource: accessResourceScopeDTO{Kind: "project", ID: "project-atlas", ProjectID: "project-atlas"}, Risk: "medium", Status: "allowed"},
		{SubjectRef: "user:bob", Permission: "org.read", Resource: accessResourceScopeDTO{Kind: "org", ID: "org-1"}, Risk: "low", Status: "allowed"},
	}
	subjects := map[string]accessSubjectDTO{
		"user:alice": {Ref: "user:alice", Kind: "human", Name: "Alice", Email: "alice@example.com"},
		"user:bob":   {Ref: "user:bob", Kind: "human", Name: "Bob"},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/access/overview?q=alice%40example.com&subject_kind=human&project_id=project-atlas&permission=project.write&risk=medium&status=allowed", nil)
	filtered := accessFilterDecisions(decisions, subjects, map[string]accessPermissionDefinitionDTO{}, request)
	if len(filtered) != 1 || filtered[0].SubjectRef != "user:alice" || filtered[0].Permission != "project.write" {
		t.Fatalf("canonical filters returned %+v", filtered)
	}
}

func TestAccessEffectiveBatchAndRevokeContract(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	server := newTestServer(t, deps)
	defer server.Close()

	for _, url := range []string{
		server.URL + "/api/permissions/effective?view=access&status=not_applicable",
		server.URL + "/api/access/overview?status=not_applicable",
	} {
		resp := orgScopedGet(t, url, sess)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("effective status=%d for %s", resp.StatusCode, url)
		}
		var effective struct {
			Decisions []struct {
				Permission string `json:"permission"`
				Status     string `json:"status"`
				Reason     string `json:"reason"`
			} `json:"decisions"`
			Summary map[string]int `json:"summary"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&effective); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if effective.Summary["not_applicable"] == 0 {
			t.Fatalf("summary missing not_applicable for %s: %+v", url, effective.Summary)
		}
		if len(effective.Decisions) == 0 || effective.Decisions[0].Status != "not_applicable" {
			t.Fatalf("effective decisions did not expose not_applicable for %s: %+v", url, effective.Decisions)
		}
	}

	body := `{
		"subject_refs":["user:` + sess.IdentityID + `","agent:missing"],
		"permission_keys":["org.member.role.manage","file.download"],
		"resources":[{"kind":"org","id":"` + sess.OrgID + `","org_id":"` + sess.OrgID + `","label":"Test Org"}],
		"expires_at":"` + time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano) + `",
		"reason":"temporary release support"
	}`
	resp := orgScopedPost(t, server.URL+"/api/access/batch/preview", body, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview status=%d", resp.StatusCode)
	}
	var preview struct {
		ExpiresAt string `json:"expires_at"`
		Summary   struct {
			Total         int `json:"total"`
			Grantable     int `json:"grantable"`
			HighRisk      int `json:"high_risk"`
			Unauthorized  int `json:"unauthorized"`
			NotApplicable int `json:"not_applicable"`
		} `json:"summary"`
		Items []struct {
			Status string `json:"status"`
			Risk   string `json:"risk"`
			Reason string `json:"reason"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&preview); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if preview.ExpiresAt == "" || preview.Summary.HighRisk == 0 || preview.Summary.Unauthorized == 0 || preview.Summary.NotApplicable == 0 {
		t.Fatalf("preview did not expose expiry/high-risk/unauthorized/not-applicable: %+v", preview)
	}

	resp = orgScopedPost(t, server.URL+"/api/access/batch/apply", body, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply status=%d", resp.StatusCode)
	}
	var applied struct {
		Summary struct {
			Total          int  `json:"total"`
			Succeeded      int  `json:"succeeded"`
			PartialFailure bool `json:"partial_failure"`
			Failed         int  `json:"failed"`
			Unauthorized   int  `json:"unauthorized"`
			NotApplicable  int  `json:"not_applicable"`
		} `json:"summary"`
		Items []struct {
			ID         string `json:"id"`
			SubjectRef string `json:"subject_ref"`
			Permission string `json:"permission"`
			Resource   struct {
				Kind string `json:"kind"`
				ID   string `json:"id"`
			} `json:"resource"`
			Status string `json:"status"`
			Reason string `json:"reason"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&applied); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !applied.Summary.PartialFailure || applied.Summary.Failed == 0 || applied.Summary.Unauthorized == 0 || applied.Summary.NotApplicable == 0 || applied.Summary.Total != len(applied.Items) {
		t.Fatalf("apply did not expose partial failure details/items: summary=%+v items=%+v", applied.Summary, applied.Items)
	}
	for _, item := range applied.Items {
		if item.SubjectRef == "unknown" || item.Permission == "unknown" || item.Resource.ID == "unknown" {
			t.Fatalf("apply returned phantom unknown item: %+v", item)
		}
	}

	mixedBody := `{
		"subject_refs":["user:` + sess.IdentityID + `"],
		"permission_keys":["org.analytics.read"],
		"resources":[
			{"kind":"org","id":"` + sess.OrgID + `","org_id":"` + sess.OrgID + `","label":"Test Org"},
			{"kind":"project","id":"proj-alpha","org_id":"` + sess.OrgID + `","label":"Project Alpha"}
		],
		"reason":"temporary analytics audit"
	}`
	resp = orgScopedPost(t, server.URL+"/api/access/batch/apply", mixedBody, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mixed org/project apply status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	var mixed struct {
		Summary struct {
			Total          int  `json:"total"`
			Succeeded      int  `json:"succeeded"`
			Failed         int  `json:"failed"`
			NotApplicable  int  `json:"not_applicable"`
			PartialFailure bool `json:"partial_failure"`
		} `json:"summary"`
		Items []struct {
			Permission string `json:"permission"`
			Resource   struct {
				Kind string `json:"kind"`
			} `json:"resource"`
			Status string `json:"status"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&mixed); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(mixed.Items) != 2 || mixed.Summary.Total != 2 || mixed.Summary.Succeeded != 1 || mixed.Summary.Failed != 1 || mixed.Summary.NotApplicable != 1 || !mixed.Summary.PartialFailure {
		t.Fatalf("mixed org/project apply summary/items mismatch: summary=%+v items=%+v", mixed.Summary, mixed.Items)
	}
	statusByKind := map[string]string{}
	for _, item := range mixed.Items {
		statusByKind[item.Resource.Kind] = item.Status
	}
	if statusByKind["org"] != "allowed" || statusByKind["project"] != "not_applicable" {
		t.Fatalf("mixed org/project item mapping mismatch: %+v", mixed.Items)
	}

	roleBody := `{
		"subject_refs":["user:` + sess.IdentityID + `"],
		"role_ids":["role-web-batch-member"],
		"permission_keys":[],
		"resources":[{"kind":"org","id":"` + sess.OrgID + `","org_id":"` + sess.OrgID + `","label":"Test Org"}],
		"reason":"temporary role assignment"
	}`
	if _, err := db.Exec(`
		INSERT INTO authorization_roles (id, org_id, kind, visibility, stable_key, name, description, scope_kind, created_by, created_at, updated_at, version)
		VALUES ('role-web-batch-member', ?, 'custom', 'reusable', 'web-batch-member', 'Web batch member', '', 'org', 'system', datetime('now'), datetime('now'), 1)`, sess.OrgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO authorization_role_permissions (role_id, permission_key, resource_kind, delegatable, created_at)
		VALUES ('role-web-batch-member', 'org.read', 'org', 0, datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	resp = orgScopedPost(t, server.URL+"/api/access/batch/preview", roleBody, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("role preview status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	var rolePreview struct {
		Summary struct {
			Total     int `json:"total"`
			Grantable int `json:"grantable"`
		} `json:"summary"`
		Items []struct {
			Permission string `json:"permission"`
			RoleID     string `json:"role_id"`
			RoleName   string `json:"role_name"`
			Status     string `json:"status"`
			Reason     string `json:"reason"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rolePreview); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if rolePreview.Summary.Total != 1 || rolePreview.Summary.Grantable != 1 || len(rolePreview.Items) != 1 || rolePreview.Items[0].RoleID != "role-web-batch-member" || rolePreview.Items[0].RoleName != "Web batch member" || rolePreview.Items[0].Permission != "role:role-web-batch-member" {
		t.Fatalf("role preview did not expose RAM Role item: summary=%+v items=%+v", rolePreview.Summary, rolePreview.Items)
	}
	resp = orgScopedPost(t, server.URL+"/api/access/batch/apply", roleBody, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("role apply status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	var roleApply struct {
		Summary struct {
			Succeeded int `json:"succeeded"`
		} `json:"summary"`
		Items []struct {
			RoleID  string `json:"role_id"`
			GrantID string `json:"grant_id"`
			Status  string `json:"status"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&roleApply); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if roleApply.Summary.Succeeded != 1 || len(roleApply.Items) != 1 || roleApply.Items[0].RoleID != "role-web-batch-member" || roleApply.Items[0].GrantID == "" || roleApply.Items[0].Status != "allowed" {
		t.Fatalf("role apply did not assign RAM Role: summary=%+v items=%+v", roleApply.Summary, roleApply.Items)
	}

	grantID := "grant:org_role:user:" + sess.IdentityID + ":org.member.role.manage:org:" + sess.OrgID
	for _, url := range []string{
		server.URL + "/api/permissions/batch/revoke",
		server.URL + "/api/access/grants/revoke",
	} {
		resp = orgScopedPost(t, url, `{"grant_ids":["`+grantID+`"],"reason":"cleanup"}`, sess)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("direct revoke status=%d for %s, want 400", resp.StatusCode, url)
		}
		resp.Body.Close()
	}
	resp = orgScopedPost(t, server.URL+"/api/access/grants/revoke/preview", `{"grant_ids":["`+grantID+`"],"reason":"cleanup"}`, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke preview status=%d", resp.StatusCode)
	}
	var revoked struct {
		PreviewID string `json:"preview_id"`
		ExpiresAt string `json:"expires_at"`
		Summary   struct {
			NotApplicable int `json:"not_applicable"`
		} `json:"summary"`
		Items []struct {
			Status string `json:"status"`
			Reason string `json:"reason"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&revoked); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if revoked.PreviewID == "" || revoked.ExpiresAt == "" || revoked.Summary.NotApplicable != 1 || revoked.Items[0].Status != "not_applicable" {
		t.Fatalf("revoke preview did not expose derived-grant not_applicable: %+v", revoked)
	}
	resp = orgScopedPost(t, server.URL+"/api/access/grants/revoke/confirm", `{"grant_ids":["`+grantID+`"],"reason":"cleanup","preview_id":"`+revoked.PreviewID+`","token":"wrong"}`, sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("revoke confirm without persisted preview status=%d want 404", resp.StatusCode)
	}
	resp.Body.Close()

	directBody := `{
		"subject_refs":["user:` + sess.IdentityID + `"],
		"permission_keys":["org.analytics.read"],
		"resources":[{"kind":"org","id":"` + sess.OrgID + `","org_id":"` + sess.OrgID + `","label":"Test Org"}],
		"reason":"temporary analytics audit"
	}`
	resp = orgScopedPost(t, server.URL+"/api/access/batch/apply", directBody, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("direct access apply status=%d", resp.StatusCode)
	}
	var direct struct {
		Items []struct {
			Status  string `json:"status"`
			GrantID string `json:"grant_id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&direct); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(direct.Items) != 1 || direct.Items[0].Status != "allowed" || direct.Items[0].GrantID == "" {
		t.Fatalf("direct grant apply = %+v", direct.Items)
	}
	var directKind, directVisibility, directName string
	directRoleID := accessRoleIDForPermission("org.analytics.read", "org")
	if err := db.QueryRow(`SELECT kind, visibility, name FROM authorization_roles WHERE id = ?`, directRoleID).Scan(&directKind, &directVisibility, &directName); err != nil {
		t.Fatalf("direct grant managed role missing: %v", err)
	}
	if directKind != "managed" || directVisibility != "internal" || strings.HasPrefix(directName, "Access grant") {
		t.Fatalf("direct grant role classification=(%q,%q,%q), want managed/internal without reserved prefix", directKind, directVisibility, directName)
	}
	resp = orgScopedGet(t, server.URL+"/api/access/ram-roles/"+directRoleID, sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("managed internal role detail status=%d want 404", resp.StatusCode)
	}
	resp.Body.Close()
	directGrantID := direct.Items[0].GrantID
	revokeReason := "quarterly least privilege review"
	resp = orgScopedPost(t, server.URL+"/api/access/grants/revoke/preview", `{"grant_ids":["`+directGrantID+`"],"reason":"`+revokeReason+`","message":"`+revokeReason+`"}`, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("direct revoke preview status=%d", resp.StatusCode)
	}
	var directPreview struct {
		PreviewID string `json:"preview_id"`
		Token     string `json:"token"`
		Items     []struct {
			Status  string `json:"status"`
			GrantID string `json:"grant_id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&directPreview); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if directPreview.PreviewID == "" || directPreview.Token == "" || len(directPreview.Items) != 1 || directPreview.Items[0].Status != "allowed" || directPreview.Items[0].GrantID != directGrantID {
		t.Fatalf("direct revoke preview = %+v", directPreview)
	}
	stableRevokeKey := "access-revoke-" + directPreview.PreviewID
	resp = orgScopedPost(t, server.URL+"/api/access/grants/revoke/confirm", `{"grant_ids":["`+directGrantID+`"],"reason":"`+revokeReason+`","message":"`+revokeReason+`","preview_id":"`+directPreview.PreviewID+`","token":"`+directPreview.Token+`","idempotency_key":"`+stableRevokeKey+`"}`, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("direct revoke confirm status=%d", resp.StatusCode)
	}
	var directConfirm struct {
		Summary struct {
			Succeeded int `json:"succeeded"`
			Failed    int `json:"failed"`
		} `json:"summary"`
		Items []struct {
			Status  string `json:"status"`
			GrantID string `json:"grant_id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&directConfirm); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if directConfirm.Summary.Succeeded != 1 || directConfirm.Summary.Failed != 0 || len(directConfirm.Items) != 1 || directConfirm.Items[0].Status != "allowed" || directConfirm.Items[0].GrantID != directGrantID {
		t.Fatalf("direct revoke confirm = %+v", directConfirm)
	}
	resp = orgScopedPost(t, server.URL+"/api/access/grants/revoke/confirm", `{"grant_ids":["`+directGrantID+`"],"reason":"`+revokeReason+`","message":"`+revokeReason+`","preview_id":"`+directPreview.PreviewID+`","token":"`+directPreview.Token+`","idempotency_key":"`+stableRevokeKey+`"}`, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("direct revoke confirm replay status=%d", resp.StatusCode)
	}
	var directReplay struct {
		Summary struct {
			Succeeded int `json:"succeeded"`
			Failed    int `json:"failed"`
		} `json:"summary"`
		Items []struct {
			Status  string `json:"status"`
			GrantID string `json:"grant_id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&directReplay); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if directReplay.Summary.Succeeded != directConfirm.Summary.Succeeded || directReplay.Summary.Failed != directConfirm.Summary.Failed || len(directReplay.Items) != len(directConfirm.Items) || directReplay.Items[0].Status != directConfirm.Items[0].Status || directReplay.Items[0].GrantID != directConfirm.Items[0].GrantID {
		t.Fatalf("direct revoke confirm replay = %+v, first = %+v", directReplay, directConfirm)
	}
	var previewConsumed int
	if err := db.QueryRow(`SELECT COUNT(*) FROM authorization_revoke_previews WHERE preview_id = ? AND status = 'confirmed'`, directPreview.PreviewID).Scan(&previewConsumed); err != nil || previewConsumed != 1 {
		t.Fatalf("confirmed preview count=%d err=%v", previewConsumed, err)
	}
	var auditCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM authorization_audit_events WHERE event_type = 'authorization.assignment.revoked' AND assignment_id = ?`, directGrantID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("revoke audit count=%d err=%v", auditCount, err)
	}
	var payloadRaw string
	var requestID string
	if err := db.QueryRow(`SELECT request_id, payload_json FROM authorization_audit_events WHERE event_type = 'authorization.assignment.revoked' AND assignment_id = ? ORDER BY created_at DESC LIMIT 1`, directGrantID).Scan(&requestID, &payloadRaw); err != nil {
		t.Fatal(err)
	}
	if requestID != stableRevokeKey {
		t.Fatalf("revoke audit request_id=%q want %q", requestID, stableRevokeKey)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadRaw), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["reason"] != revokeReason || payload["message"] != revokeReason {
		t.Fatalf("revoke audit payload reason/message = %s", payloadRaw)
	}

	resp = orgScopedPatch(t, server.URL+"/api/access/roles/org:admin", `{"permissions":["org.read"],"reason":"test"}`, sess)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("role update status=%d want 409", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAccessOverviewShowsTeamRAMAndDirectBindingUnion(t *testing.T) {
	deps, db, sess := setupTeamsAPI(t)
	deps.Authorizer = authz.New(authz.Deps{DB: db, Mode: authz.EnforcementEnforce})
	now := time.Now().UTC().Format(time.RFC3339Nano)
	expiresAt := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	tm := seedTeam(t, deps, sess.OrgID, "Access Union Team", []team.RoleConfig{{Role: "reviewer", CLI: "codex", Model: "gpt-5", MaxConcurrency: 1}})
	subject := "user:" + sess.IdentityID
	if _, err := deps.TeamService.AddMember(context.Background(), tm.ID(), team.MemberRef(subject), "reviewer"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO authorization_roles (id, org_id, kind, name, description, created_by, created_at, updated_at, version)
		VALUES ('role-union-reviewer', ?, 'custom', 'Access union reviewer', '', 'system', ?, ?, 1)`, sess.OrgID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO authorization_role_permissions (role_id, permission_key, resource_kind, delegatable, created_at)
		VALUES ('role-union-reviewer', 'team.memory.review', 'team', 0, ?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO team_role_ram_role_mappings (team_id, team_role, ram_role_id, created_at, created_by) VALUES (?, 'reviewer', 'role-union-reviewer', ?, 'system')`, tm.ID().String(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO team_role_ram_role_versions (team_id, team_role, version, updated_at, updated_by) VALUES (?, 'reviewer', 1, ?, 'system')`, tm.ID().String(), now); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, deps)
	defer server.Close()

	applyBody := `{
		"subject_refs":["` + subject + `"],
		"permission_keys":["team.memory.review"],
		"resources":[{"kind":"team","id":"` + tm.ID().String() + `","org_id":"` + sess.OrgID + `","label":"Access Union Team"}],
		"expires_at":"` + expiresAt + `",
		"reason":"temporary direct binding"
	}`
	resp := orgScopedPost(t, server.URL+"/api/access/batch/apply", applyBody, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("direct binding apply status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	var applied struct {
		Items []struct {
			Status  string `json:"status"`
			GrantID string `json:"grant_id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&applied); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(applied.Items) != 1 || applied.Items[0].Status != "allowed" || applied.Items[0].GrantID == "" {
		t.Fatalf("direct binding apply items=%+v", applied.Items)
	}

	resp = orgScopedGet(t, server.URL+"/api/access/overview?q=Access%20Union%20Team", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("overview status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	var overview struct {
		Decisions []struct {
			SubjectRef  string `json:"subject_ref"`
			Permission  string `json:"permission"`
			Source      string `json:"source"`
			EvidenceRef string `json:"evidence_ref"`
			GrantID     string `json:"grant_id"`
			RoleID      string `json:"role_id"`
			ExpiresAt   string `json:"expires_at"`
		} `json:"decisions"`
		Grants []struct {
			ID     string `json:"id"`
			Source string `json:"source"`
			RoleID string `json:"role_id"`
		} `json:"grants"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&overview); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	seen := map[string]bool{}
	for _, d := range overview.Decisions {
		if d.SubjectRef == subject && d.Permission == "team.memory.review" {
			seen[d.Source] = true
			if d.Source == string(authz.SourceTeamRoleRAM) && d.RoleID != "role-union-reviewer" {
				t.Fatalf("team RAM role_id=%q evidence=%q", d.RoleID, d.EvidenceRef)
			}
			if d.Source == string(authz.SourceCustomRole) && (d.GrantID != applied.Items[0].GrantID || d.ExpiresAt == "" || d.RoleID != "") {
				t.Fatalf("direct decision grant/expiry not read back: %+v", d)
			}
		}
	}
	for _, source := range []string{string(authz.SourceOrgRole), string(authz.SourceTeamRoleRAM), string(authz.SourceCustomRole)} {
		if !seen[source] {
			t.Fatalf("overview missing %s union source; decisions=%+v", source, overview.Decisions)
		}
	}
	var directGrant bool
	for _, grant := range overview.Grants {
		if grant.ID == applied.Items[0].GrantID && grant.Source == string(authz.SourceCustomRole) && grant.RoleID == "" {
			directGrant = true
		}
	}
	if !directGrant {
		t.Fatalf("overview grants missing direct binding grant id=%s grants=%+v", applied.Items[0].GrantID, overview.Grants)
	}

	revokeBody := `{"grant_ids":["` + applied.Items[0].GrantID + `"],"reason":"union test cleanup","message":"union test cleanup"}`
	resp = orgScopedPost(t, server.URL+"/api/access/grants/revoke/preview", revokeBody, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("overview-selected direct revoke preview status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	var preview struct {
		PreviewID string `json:"preview_id"`
		Token     string `json:"token"`
		Items     []struct {
			Status  string `json:"status"`
			GrantID string `json:"grant_id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&preview); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if preview.PreviewID == "" || preview.Token == "" || len(preview.Items) != 1 || preview.Items[0].Status != "allowed" || preview.Items[0].GrantID != applied.Items[0].GrantID {
		t.Fatalf("overview-selected direct revoke preview=%+v", preview)
	}
	confirmBody := `{"grant_ids":["` + applied.Items[0].GrantID + `"],"reason":"union test cleanup","message":"union test cleanup","preview_id":"` + preview.PreviewID + `","token":"` + preview.Token + `","idempotency_key":"overview-union-revoke-` + preview.PreviewID + `"}`
	resp = orgScopedPost(t, server.URL+"/api/access/grants/revoke/confirm", confirmBody, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("overview-selected direct revoke confirm status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	var confirmed struct {
		Summary struct {
			Succeeded int `json:"succeeded"`
			Failed    int `json:"failed"`
		} `json:"summary"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&confirmed); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if confirmed.Summary.Succeeded != 1 || confirmed.Summary.Failed != 0 {
		t.Fatalf("overview-selected direct revoke confirm=%+v", confirmed)
	}

	resp = orgScopedGet(t, server.URL+"/api/access/overview?q=Access%20Union%20Team", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("overview after direct revoke status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	var afterRevoke struct {
		Grants []struct {
			ID string `json:"id"`
		} `json:"grants"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&afterRevoke); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	for _, grant := range afterRevoke.Grants {
		if grant.ID == applied.Items[0].GrantID {
			t.Fatalf("revoked direct binding still selectable in overview: %+v", afterRevoke.Grants)
		}
	}
}

func TestAccessGrantsFromDecisionsIncludesDirectOrgRead(t *testing.T) {
	now := time.Now().UTC()
	decisions := []accessDecisionDTO{{
		Allowed: true, SubjectRef: "user:reader", Permission: "org.read",
		Resource: accessResourceScopeDTO{Kind: "org", ID: "org-1", OrgID: "org-1", Label: "Organization"},
		Source:   string(authz.SourceCustomRole), Status: "allowed", GrantID: "asgn-direct-org-read", RoleID: "role-org-read", Risk: "low",
	}}
	grants := accessGrantsFromDecisions(now, decisions, map[string]accessSubjectDTO{"user:reader": {Ref: "user:reader", Name: "Reader"}})
	if len(grants) != 1 || grants[0].ID != "asgn-direct-org-read" || grants[0].Permission != "org.read" {
		t.Fatalf("direct org.read must be revocable from overview grants: %+v", grants)
	}
}

func TestAccessBatchCodeForErrorExposesHTTPMeaning(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("duplicate assignment: %w", authz.ErrConflict), "409 conflict"},
		{fmt.Errorf("actor rejected: %w", authz.ErrDenied), "403 forbidden"},
		{fmt.Errorf("bad request: %w", authz.ErrInvalid), "422 invalid_request"},
	} {
		if got := accessBatchCodeForError(tc.err); got != tc.want {
			t.Fatalf("code(%v)=%q want %q", tc.err, got, tc.want)
		}
	}
}

func TestAccessOverviewDirectBindingUsesResolverExpiryFailClosed(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	deps.Authorizer = authz.New(authz.Deps{DB: db, Mode: authz.EnforcementEnforce})
	subject := "user:" + sess.IdentityID
	now := time.Now().UTC()
	expired := now.Add(-time.Hour).Format(time.RFC3339Nano)
	created := now.Add(-2 * time.Hour).Format(time.RFC3339Nano)
	if _, err := db.Exec(`
		INSERT INTO authorization_roles (id, org_id, kind, name, description, created_by, created_at, updated_at, version)
		VALUES ('role-expired-overview', ?, 'custom', 'Expired overview role', '', 'system', ?, ?, 1)`, sess.OrgID, created, created); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO authorization_role_permissions (role_id, permission_key, resource_kind, delegatable, created_at)
		VALUES ('role-expired-overview', 'org.settings.manage', 'org', 0, ?)`, created); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO authorization_role_assignments (id, org_id, subject_ref, role_id, resource_kind, resource_id, created_by, created_at, expires_at)
		VALUES ('asgn-expired-overview', ?, ?, 'role-expired-overview', 'org', ?, 'system', ?, ?)`, sess.OrgID, subject, sess.OrgID, created, expired); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, deps)
	defer server.Close()

	resp := orgScopedGet(t, server.URL+"/api/access/overview?q="+sess.IdentityID, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("overview status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	var overview struct {
		Decisions []struct {
			SubjectRef  string `json:"subject_ref"`
			Permission  string `json:"permission"`
			Source      string `json:"source"`
			EvidenceRef string `json:"evidence_ref"`
			Status      string `json:"status"`
			GrantID     string `json:"grant_id"`
		} `json:"decisions"`
		Grants []struct {
			ID string `json:"id"`
		} `json:"grants"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&overview); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	for _, decision := range overview.Decisions {
		if decision.EvidenceRef == "authorization_role_assignments:asgn-expired-overview" || decision.GrantID == "asgn-expired-overview" {
			t.Fatalf("expired direct assignment surfaced outside resolver expiry semantics: %+v", decision)
		}
	}
	for _, grant := range overview.Grants {
		if grant.ID == "asgn-expired-overview" {
			t.Fatalf("expired direct assignment surfaced as grant: %+v", overview.Grants)
		}
	}
}

func TestAccessOverviewExplicitDenyPrecedesDirectBinding(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	deps.Authorizer = authz.New(authz.Deps{DB: db, Mode: authz.EnforcementEnforce})
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.ExecContext(context.Background(), `INSERT INTO identities (id, kind, display_name, passcode_hash, created_at, updated_at)
		VALUES ('denied-user', 'user', 'Denied User', 'x', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO members (id, organization_id, identity_id, role, status, joined_at, disabled_at, disabled_reason)
		VALUES ('member-denied-user', ?, 'denied-user', 'member', 'disabled', ?, ?, 'explicit test disable')`, sess.OrgID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO authorization_roles (id, org_id, kind, name, description, created_by, created_at, updated_at, version)
		VALUES ('role-denied-org-reader', ?, 'custom', 'Denied direct reader', '', 'system', ?, ?, 1)`, sess.OrgID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO authorization_role_permissions (role_id, permission_key, resource_kind, delegatable, created_at)
		VALUES ('role-denied-org-reader', 'org.read', 'org', 0, ?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO authorization_role_assignments (id, org_id, subject_ref, role_id, resource_kind, resource_id, created_by, created_at)
		VALUES ('asgn-denied-org-reader', ?, 'user:denied-user', 'role-denied-org-reader', 'org', ?, 'system', ?)`, sess.OrgID, sess.OrgID, now); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, deps)
	defer server.Close()

	resp := orgScopedGet(t, server.URL+"/api/access/overview?q=Denied%20User", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("overview status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	var overview struct {
		Decisions []struct {
			SubjectRef  string `json:"subject_ref"`
			Permission  string `json:"permission"`
			Status      string `json:"status"`
			Reason      string `json:"reason"`
			GrantID     string `json:"grant_id"`
			EvidenceRef string `json:"evidence_ref"`
		} `json:"decisions"`
		Grants []struct {
			ID string `json:"id"`
		} `json:"grants"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&overview); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var found bool
	for _, decision := range overview.Decisions {
		if decision.SubjectRef == "user:denied-user" && decision.Permission == "org.read" {
			found = true
			if decision.Status != "denied" || decision.GrantID != "" || !strings.Contains(decision.Reason, "explicit deny precedence") {
				t.Fatalf("org.read did not remain explicitly denied: %+v", decision)
			}
			if strings.Contains(decision.EvidenceRef, "asgn-denied-org-reader") {
				t.Fatalf("direct allow assignment displaced explicit deny evidence: %+v", decision)
			}
		}
	}
	if !found {
		t.Fatalf("missing denied org.read decision: %+v", overview.Decisions)
	}
	for _, grant := range overview.Grants {
		if grant.ID == "asgn-denied-org-reader" {
			t.Fatalf("explicitly denied direct assignment surfaced as grant: %+v", overview.Grants)
		}
	}
}

func TestAccessOverviewResolverErrorsSurfaceFailClosedRows(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	deps.Authorizer = authz.New(authz.Deps{DB: db, Mode: authz.EnforcementEnforce})
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.ExecContext(context.Background(), `INSERT INTO authorization_roles (id, org_id, kind, name, description, created_by, created_at, updated_at, version)
		VALUES ('role-invalid-resource', ?, 'custom', 'Invalid resource', '', 'system', ?, ?, 1)`, sess.OrgID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO authorization_role_permissions (role_id, permission_key, resource_kind, delegatable, created_at)
		VALUES ('role-invalid-resource', 'org.read', 'org', 0, ?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO authorization_role_assignments (id, org_id, subject_ref, role_id, resource_kind, resource_id, created_by, created_at)
		VALUES ('asgn-invalid-resource-overview', ?, ?, 'role-invalid-resource', 'invalid', 'bad-resource', 'system', ?)`, sess.OrgID, authz.UserSubject(sess.IdentityID), now); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, deps)
	defer server.Close()

	resp := orgScopedGet(t, server.URL+"/api/access/overview?q=resolver%20failed%20closed", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("overview status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	var overview struct {
		Decisions []struct {
			Permission  string `json:"permission"`
			Status      string `json:"status"`
			Reason      string `json:"reason"`
			EvidenceRef string `json:"evidence_ref"`
			Resource    struct {
				Kind string `json:"kind"`
				ID   string `json:"id"`
			} `json:"resource"`
		} `json:"decisions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&overview); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	for _, decision := range overview.Decisions {
		if decision.Permission == "access.resolve" && decision.Status == "denied" && strings.Contains(decision.Reason, "resolver failed closed") && decision.Resource.Kind == "invalid" {
			return
		}
	}
	t.Fatalf("missing fail-closed resolver row: %+v", overview.Decisions)
}

func TestAccessRAMRolesPersistVersionsCASRevokeAndReferences(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	server := newTestServer(t, deps)
	defer server.Close()

	resp := orgScopedGet(t, server.URL+"/api/access/ram-roles", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list RAM roles status=%d", resp.StatusCode)
	}
	var listed struct {
		Roles []struct {
			ID        string   `json:"id"`
			Version   int      `json:"version"`
			Perms     []string `json:"permissions"`
			UpdatedAt string   `json:"updated_at"`
		} `json:"roles"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(listed.Roles) == 0 || listed.Roles[0].Version == 0 || len(listed.Roles[0].Perms) == 0 || listed.Roles[0].UpdatedAt == "" {
		t.Fatalf("seeded persistent RAM roles missing: %+v", listed.Roles)
	}

	createBody := `{"name":"Release operator","description":"ship access","permissions":["team.read","team.write"]}`
	resp = orgScopedPost(t, server.URL+"/api/access/ram-roles", createBody, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create RAM role status=%d", resp.StatusCode)
	}
	var created struct {
		ID     string `json:"id"`
		Latest struct {
			Version int      `json:"version"`
			Perms   []string `json:"permissions"`
		} `json:"latest"`
		Versions []struct {
			Version int      `json:"version"`
			Perms   []string `json:"permissions"`
		} `json:"versions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if created.ID == "" || created.Latest.Version != 1 || len(created.Versions) != 1 {
		t.Fatalf("created RAM role shape wrong: %+v", created)
	}
	resp = orgScopedPost(t, server.URL+"/api/access/ram-roles", `{"name":"Access grant org.read on org","permissions":["org.read"]}`, sess)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("reserved Access grant create status=%d want 422", resp.StatusCode)
	}
	resp.Body.Close()

	stale := orgScopedPost(t, server.URL+"/api/access/ram-roles/"+created.ID+"/versions", `{"expected_latest_version":0,"permissions":["team.read"]}`, sess)
	if stale.StatusCode != http.StatusConflict {
		t.Fatalf("stale new-version status=%d want 409", stale.StatusCode)
	}
	stale.Body.Close()

	resp = orgScopedPost(t, server.URL+"/api/access/ram-roles/"+created.ID+"/versions", `{"expected_latest_version":1,"permissions":["team.read","team.memory.review"]}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("new-version status=%d", resp.StatusCode)
	}
	var updated struct {
		Latest struct {
			Version int      `json:"version"`
			Risk    string   `json:"risk"`
			Perms   []string `json:"permissions"`
		} `json:"latest"`
		Versions []struct {
			Version int      `json:"version"`
			Perms   []string `json:"permissions"`
		} `json:"versions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if updated.Latest.Version != 2 || updated.Latest.Risk != "high" || len(updated.Versions) != 2 {
		t.Fatalf("new version shape wrong: %+v", updated)
	}
	if len(updated.Versions[1].Perms) != 2 {
		t.Fatalf("v1 mutated; versions must be immutable: %+v", updated.Versions)
	}
	resp = orgScopedPost(t, server.URL+"/api/access/ram-roles/"+created.ID+"/versions", `{"name":"Access grant team.read on team","expected_latest_version":2,"permissions":["team.read"]}`, sess)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("reserved Access grant update status=%d want 422", resp.StatusCode)
	}
	resp.Body.Close()
	resp = orgScopedPost(t, server.URL+"/api/access/ram-roles/team-basic/versions", `{"expected_latest_version":1,"permissions":["team.read"]}`, sess)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("system role update status=%d want 422", resp.StatusCode)
	}
	resp.Body.Close()
	resp = orgScopedPost(t, server.URL+"/api/access/ram-roles/"+created.ID+"/revoke", `{"expected_latest_version":1,"reason":"stale"}`, sess)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("stale revoke status=%d want 409", resp.StatusCode)
	}
	resp.Body.Close()
	resp = orgScopedPost(t, server.URL+"/api/access/ram-roles/"+created.ID+"/revoke", `{"expected_latest_version":2,"reason":"done"}`, sess)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	resp.Body.Close()
	var auditCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM authorization_audit_events WHERE event_type = 'authorization.ram_role.revoked' AND role_id = ?`, created.ID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("RAM role revoke audit count=%d err=%v", auditCount, err)
	}

	if _, err := db.Exec(`UPDATE members SET role='member' WHERE identity_id=?`, sess.IdentityID); err != nil {
		t.Fatal(err)
	}
	resp = orgScopedPost(t, server.URL+"/api/access/ram-roles", `{"name":"Blocked","permissions":["team.read"]}`, sess)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member create RAM role status=%d want 403", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAccessRAMRoleV4EditDeleteAndReferenceBlocking(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	server := newTestServer(t, deps)
	defer server.Close()

	resp := orgScopedPost(t, server.URL+"/api/access/ram-roles", `{"name":"Deploy operator","stable_key":"deploy-operator","description":"deploy work","scope":"project","permissions":["project.read"]}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	var created struct {
		ID        string `json:"id"`
		StableKey string `json:"stable_key"`
		Latest    struct {
			Version int    `json:"version"`
			Scope   string `json:"scope"`
		} `json:"latest"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if created.ID == "" || created.ID == created.StableKey || created.StableKey != "deploy-operator" || created.Latest.Scope != "project" {
		t.Fatalf("created stable key/scope wrong: %+v", created)
	}

	resp = orgScopedPatch(t, server.URL+"/api/access/ram-roles/"+created.ID, `{"name":"Deploy admin","stable_key":"deploy-admin","description":"deploy high risk","scope":"team","permissions":["team.read","team.memory.review"],"expected_latest_version":1}`, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	var edited struct {
		StableKey   string `json:"stable_key"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Scope       string `json:"scope"`
		Latest      struct {
			StableKey string `json:"stable_key"`
			Version   int    `json:"version"`
			Risk      string `json:"risk"`
			Scope     string `json:"scope"`
		} `json:"latest"`
		Versions []struct {
			Version     int    `json:"version"`
			StableKey   string `json:"stable_key"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Scope       string `json:"scope"`
		} `json:"versions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&edited); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if edited.StableKey != "deploy-admin" || edited.Name != "Deploy admin" || edited.Description != "deploy high risk" || edited.Scope != "team" || edited.Latest.StableKey != "deploy-admin" || edited.Latest.Version != 2 || edited.Latest.Risk != "high" {
		t.Fatalf("edited role wrong: %+v", edited)
	}
	if len(edited.Versions) != 2 || edited.Versions[0].Version != 2 || edited.Versions[0].StableKey != "deploy-admin" || edited.Versions[0].Name != "Deploy admin" || edited.Versions[0].Description != "deploy high risk" || edited.Versions[0].Scope != "team" || edited.Versions[1].Version != 1 || edited.Versions[1].StableKey != "deploy-operator" || edited.Versions[1].Name != "Deploy operator" || edited.Versions[1].Description != "deploy work" || edited.Versions[1].Scope != "project" {
		t.Fatalf("version metadata is not immutable: %+v", edited.Versions)
	}

	resp = orgScopedGet(t, server.URL+"/api/access/ram-roles/"+created.ID, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("readback status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	var readback struct {
		StableKey string `json:"stable_key"`
		Versions  []struct {
			Version     int    `json:"version"`
			StableKey   string `json:"stable_key"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Scope       string `json:"scope"`
		} `json:"versions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&readback); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if readback.StableKey != "deploy-admin" || len(readback.Versions) != 2 || readback.Versions[0].StableKey != "deploy-admin" || readback.Versions[0].Name != "Deploy admin" || readback.Versions[1].StableKey != "deploy-operator" || readback.Versions[1].Name != "Deploy operator" {
		t.Fatalf("HTTP readback did not preserve renamed key and distinct history: %+v", readback)
	}

	resp = orgScopedPost(t, server.URL+"/api/access/ram-roles", `{"name":"Duplicate key","stable_key":"deploy-admin","scope":"team","permissions":["team.read"]}`, sess)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate stable key status=%d want 409 body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	var conflict struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&conflict); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if conflict.Error != "stable_key_conflict" {
		t.Fatalf("duplicate stable key error=%q want stable_key_conflict", conflict.Error)
	}
	var persistedKey string
	if err := db.QueryRow(`SELECT stable_key FROM authorization_roles WHERE id = ?`, created.ID).Scan(&persistedKey); err != nil || persistedKey != "deploy-admin" {
		t.Fatalf("persisted stable key=%q err=%v", persistedKey, err)
	}
	rows, err := db.Query(`SELECT version, stable_key, name, description, scope_kind FROM authorization_role_versions WHERE role_id = ? ORDER BY version`, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var snapshots [][]any
	for rows.Next() {
		var version int
		var stableKey, name, description, scope string
		if err := rows.Scan(&version, &stableKey, &name, &description, &scope); err != nil {
			t.Fatal(err)
		}
		snapshots = append(snapshots, []any{version, stableKey, name, description, scope})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if got, want := fmt.Sprint(snapshots), "[[1 deploy-operator Deploy operator deploy work project] [2 deploy-admin Deploy admin deploy high risk team]]"; got != want {
		t.Fatalf("persisted version snapshots=%s want=%s", got, want)
	}

	resp = orgScopedDelete(t, server.URL+"/api/access/ram-roles/"+created.ID, sess)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("delete without confirm status=%d want 409", resp.StatusCode)
	}
	resp.Body.Close()
	resp = orgScopedDeleteJSON(t, server.URL+"/api/access/ram-roles/"+created.ID, `{"expected_latest_version":2,"confirm_unreferenced":true,"reason":"retire"}`, sess)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("confirmed delete status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	resp.Body.Close()

	resp = orgScopedPost(t, server.URL+"/api/access/ram-roles", `{"name":"Mapped operator","stable_key":"mapped-operator","scope":"team","permissions":["team.read"]}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create mapped status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	var mapped struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&mapped); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	now := "2026-08-21T00:00:00Z"
	if _, err := db.Exec(`INSERT INTO teams (id, org_id, name, created_at, updated_at) VALUES ('team-v4', ?, 'Core team', ?, ?)`, sess.OrgID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO team_roles (team_id, role, created_at) VALUES ('team-v4', 'planner', ?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO team_role_ram_role_mappings (team_id, team_role, ram_role_id, created_at, created_by) VALUES ('team-v4', 'planner', ?, ?, 'test')`, mapped.ID, now); err != nil {
		t.Fatal(err)
	}
	resp = orgScopedGet(t, server.URL+"/api/access/ram-roles/"+mapped.ID, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail mapped status=%d", resp.StatusCode)
	}
	var detail struct {
		References []struct {
			TeamName string `json:"team_name"`
			TeamRole string `json:"team_role"`
		} `json:"references"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(detail.References) != 1 || detail.References[0].TeamName != "Core team" || detail.References[0].TeamRole != "planner" {
		t.Fatalf("references not exposed: %+v", detail.References)
	}
	resp = orgScopedDeleteJSON(t, server.URL+"/api/access/ram-roles/"+mapped.ID, `{"confirm_unreferenced":true,"reason":"retire"}`, sess)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("mapped delete status=%d want 409", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	resp.Body.Close()
	if body["error"] != "ram_role_referenced" {
		t.Fatalf("mapped delete error=%v", body)
	}
}
