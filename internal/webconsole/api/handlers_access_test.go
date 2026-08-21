package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

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
		"expires_at":"2026-08-20T12:30:00Z",
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
			PartialFailure bool `json:"partial_failure"`
			Failed         int  `json:"failed"`
			Unauthorized   int  `json:"unauthorized"`
			NotApplicable  int  `json:"not_applicable"`
		} `json:"summary"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&applied); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !applied.Summary.PartialFailure || applied.Summary.Failed == 0 || applied.Summary.Unauthorized == 0 || applied.Summary.NotApplicable == 0 {
		t.Fatalf("apply did not expose partial failure details: %+v", applied.Summary)
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
