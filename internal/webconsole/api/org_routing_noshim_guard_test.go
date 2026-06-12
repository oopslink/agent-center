package api

import (
	"net/http"
	"testing"
)

// TestOrgRouting_NoShim_QueryOrgSlugIgnored is the org-populate no-shim guard
// for the v2.9 org-routing explicit cut (#304): the org is resolved ONLY from
// the {slug} path segment, never the legacy ?org_slug= query. A request whose
// PATH slug is unknown but whose ?org_slug= query names the caller's valid org
// must NOT fall back to the query — the path is authoritative (→ 400, not 200).
//
// Inverse-mutation: re-add a not-found→query fallback to resolveOrgIDFromRequest
// → the valid query resolves → this FAILS (got != 400).
func TestOrgRouting_NoShim_QueryOrgSlugIgnored(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	// path slug unknown, but ?org_slug= names the caller's valid org →
	// no-shim: the query is dead → path "nonexistent-slug" → 400.
	req, _ := http.NewRequest(http.MethodGet, s.URL+"/api/orgs/nonexistent-slug/conversations?org_slug="+sess.OrgSlug, nil)
	req.AddCookie(sess.Cookie)
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("no-shim violated: ?org_slug=%s resolved despite unknown path slug (got %d, want 400) — the legacy query is being read", sess.OrgSlug, resp.StatusCode)
	}
}
