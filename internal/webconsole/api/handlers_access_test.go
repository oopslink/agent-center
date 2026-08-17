package api

import (
	"encoding/json"
	"net/http"
	"strings"
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
		RequestID string `json:"request_id"`
		ExpiresAt string `json:"expires_at"`
		Summary   struct {
			Total         int `json:"total"`
			Grantable     int `json:"grantable"`
			HighRisk      int `json:"high_risk"`
			Unauthorized  int `json:"unauthorized"`
			NotApplicable int `json:"not_applicable"`
		} `json:"summary"`
		Items []struct {
			ID       string `json:"id"`
			Status   string `json:"status"`
			Risk     string `json:"risk"`
			HighRisk bool   `json:"high_risk"`
			Reason   string `json:"reason"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&preview); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if preview.ExpiresAt == "" || preview.Summary.HighRisk == 0 || preview.Summary.Unauthorized == 0 || preview.Summary.NotApplicable == 0 {
		t.Fatalf("preview did not expose expiry/high-risk/unauthorized/not-applicable: %+v", preview)
	}
	var highRisk []string
	for _, item := range preview.Items {
		if item.HighRisk && item.Status == "allowed" {
			highRisk = append(highRisk, `"`+item.ID+`"`)
		}
	}
	applyBody := body[:strings.LastIndex(body, "}")] + `,
		"preview_request_id":"` + preview.RequestID + `",
		"idempotency_key":"access-test-apply",
		"high_risk_confirmed_item_ids":[` + strings.Join(highRisk, ",") + `]
	}`

	resp = orgScopedPost(t, server.URL+"/api/access/batch/apply", applyBody, sess)
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
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("revoke status=%d for %s", resp.StatusCode, url)
		}
		var revoked struct {
			Summary struct {
				PartialFailure bool `json:"partial_failure"`
				NotApplicable  int  `json:"not_applicable"`
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
		if !revoked.Summary.PartialFailure || revoked.Summary.NotApplicable != 1 || revoked.Items[0].Status != "not_applicable" {
			t.Fatalf("revoke did not expose derived-grant not_applicable for %s: %+v", url, revoked)
		}
	}

	resp = orgScopedPatch(t, server.URL+"/api/access/roles/org:admin", `{"permissions":["org.read"],"reason":"test"}`, sess)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("role update status=%d want 409", resp.StatusCode)
	}
	resp.Body.Close()
}
