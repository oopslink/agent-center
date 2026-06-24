package api

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestOrgRouting_NoBareOrgScopedRoute is the durable "可证无漏" guard for the v2.9
// org-routing explicit cut (#304): every registered /api route must be EITHER
// org-scoped (/api/orgs/{slug}/...) OR one of the small set of legitimately-global
// exempt routes (auth, orgs CRUD, users/{id}, sse, health, system, admintoken/
// revoke). A future PR that registers a bare org-scoped route — e.g. GET
// /api/projects instead of GET /api/orgs/{slug}/projects — would silently
// re-introduce the no-shim/cross-org-leak this migration removed; this test fails
// CI on it. Derived from the 2026-06-11 confirmatory enumeration of server.go
// (92 org-scoped + 17 bare-exempt + 1 /api/ catch-all). A new genuinely-global
// route must be added to exemptBare WITH justification (a deliberate review gate).
func TestOrgRouting_NoBareOrgScopedRoute(t *testing.T) {
	exemptBare := map[string]bool{
		"/api/auth/bootstrap": true, "/api/auth/me": true, "/api/auth/me/passcode": true,
		"/api/auth/signin": true, "/api/auth/signout": true, "/api/auth/signup": true,
		"/api/orgs": true, "/api/orgs/{id}": true,
		"/api/users/{user_id}": true,
		"/api/sse":             true, "/api/sse/subscribe": true, "/api/sse/unsubscribe": true,
		"/api/health": true, "/api/system/version": true,
		// I7-D1 (T216): wake-guardrail thresholds are CENTER-WIDE (one process-global
		// WakeGuard config, not per-org) — like /api/system/version. Authed (session)
		// but intentionally global; no org scope to leak.
		"/api/system/wake-guardrail": true,
		"/api/admintoken/revoke":     true, // #296: bare but authenticated (org-gate in handler, not path)
	}
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("read server.go: %v", err)
	}
	re := regexp.MustCompile(`HandleFunc\("(?:GET|POST|PATCH|PUT|DELETE) (/api/[^"]*)"`)
	matches := re.FindAllStringSubmatch(string(src), -1)
	if len(matches) < 80 {
		t.Fatalf("only %d /api routes matched — regex likely stale vs server.go format", len(matches))
	}
	for _, m := range matches {
		path := m[1]
		if strings.HasPrefix(path, "/api/orgs/{slug}/") {
			continue // org-scoped — correct
		}
		if exemptBare[path] {
			continue // legitimately global
		}
		t.Errorf("bare route %q is neither org-scoped (/api/orgs/{slug}/...) nor exempt — "+
			"this re-introduces the no-shim/cross-org leak removed in #304. If genuinely "+
			"global, add it to exemptBare with justification.", path)
	}
}
