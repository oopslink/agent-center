package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/oopslink/agent-center/internal/identity"
)

// errorCodeOf reads the "error" code field from a JSON error response body.
func errorCodeOf(t *testing.T, resp *http.Response) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	resp.Body.Close()
	return body.Error
}

// addOrgMemberSession provisions a NEW user identity + membership (with `role`)
// in the SAME org as `base`, and returns a session (cookie) for it. Used to
// exercise the I41 (T470) non-owner login gate alongside the owner session.
func addOrgMemberSession(t *testing.T, db *sql.DB, base testSession, role identity.MemberRole, name string) testSession {
	t.Helper()
	ctx := context.Background()
	hash, _ := identity.HashPasscode("123456")
	ident, err := identity.IdentityFactory{}.NewUser(name, hash)
	if err != nil {
		t.Fatal(err)
	}
	idRepo := identity.NewSQLiteIdentityRepo(db)
	memberRepo := identity.NewSQLiteMemberRepo(db)
	if err := idRepo.Save(ctx, ident); err != nil {
		t.Fatal(err)
	}
	m, err := identity.MemberFactory{}.New(base.OrgID, ident.ID(), role, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := memberRepo.Save(ctx, m); err != nil {
		t.Fatal(err)
	}
	jwt, err := identity.MintJWT(ident.ID(), testSigningKey)
	if err != nil {
		t.Fatal(err)
	}
	return testSession{
		IdentityID: ident.ID(),
		OrgID:      base.OrgID,
		OrgSlug:    base.OrgSlug,
		Cookie:     &http.Cookie{Name: jwtCookieName, Value: jwt},
	}
}

// TestAPI_OrgDisable_LoginGate is the I41 (T470) acceptance test: once an org is
// disabled, a NON-owner member is blocked from every org-scoped API (the
// requireOrgMember gate), while the OWNER keeps full access — including the
// already-signed-in case (the gate runs per-request, so a non-owner who was
// already authenticated is rejected on their next call). Enable restores access.
func TestAPI_OrgDisable_LoginGate(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.OrgLifecycleSvc = identity.NewOrganizationLifecycleService(
		db, deps.OrgRepo, deps.MemberRepo, identity.NewOrganizationLockManager())
	owner := setupTestSession(t, db, deps)
	memberSess := addOrgMemberSession(t, db, owner, identity.RoleMember, "regular-member")
	s := newTestServer(t, deps)
	defer s.Close()

	// Baseline: both can list members while the org is enabled.
	if resp := orgScopedGet(t, s.URL+"/api/members", owner); resp.StatusCode != http.StatusOK {
		t.Fatalf("owner list (enabled): status=%d, want 200", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	if resp := orgScopedGet(t, s.URL+"/api/members", memberSess); resp.StatusCode != http.StatusOK {
		t.Fatalf("non-owner list (enabled): status=%d, want 200", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// Owner disables the org via the owner-only endpoint (bare /api/orgs/{id}/...).
	disableReq, _ := http.NewRequest(http.MethodPost, s.URL+"/api/orgs/"+owner.OrgID+"/disable", nil)
	disableReq.AddCookie(owner.Cookie)
	disableResp, err := http.DefaultClient.Do(disableReq)
	if err != nil {
		t.Fatal(err)
	}
	if disableResp.StatusCode != http.StatusNoContent {
		t.Fatalf("owner disable org: status=%d, want 204", disableResp.StatusCode)
	}
	disableResp.Body.Close()

	// Gate: the already-signed-in NON-owner is now blocked (403 org_disabled).
	blocked := orgScopedGet(t, s.URL+"/api/members", memberSess)
	if blocked.StatusCode != http.StatusForbidden {
		t.Fatalf("non-owner list (disabled): status=%d, want 403", blocked.StatusCode)
	}
	if got := errorCodeOf(t, blocked); got != "org_disabled" {
		t.Fatalf("non-owner block code = %q, want org_disabled", got)
	}

	// The OWNER is unaffected — full access to manage / re-enable.
	if resp := orgScopedGet(t, s.URL+"/api/members", owner); resp.StatusCode != http.StatusOK {
		t.Fatalf("owner list (disabled): status=%d, want 200 (owner must keep full access)", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// Owner re-enables → the non-owner regains access.
	enableReq, _ := http.NewRequest(http.MethodPost, s.URL+"/api/orgs/"+owner.OrgID+"/enable", nil)
	enableReq.AddCookie(owner.Cookie)
	enableResp, err := http.DefaultClient.Do(enableReq)
	if err != nil {
		t.Fatal(err)
	}
	if enableResp.StatusCode != http.StatusNoContent {
		t.Fatalf("owner enable org: status=%d, want 204", enableResp.StatusCode)
	}
	enableResp.Body.Close()
	if resp := orgScopedGet(t, s.URL+"/api/members", memberSess); resp.StatusCode != http.StatusOK {
		t.Fatalf("non-owner list (re-enabled): status=%d, want 200", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
}

// TestAPI_OrgDisable_NonOwnerForbidden verifies a non-owner cannot disable the org.
func TestAPI_OrgDisable_NonOwnerForbidden(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.OrgLifecycleSvc = identity.NewOrganizationLifecycleService(
		db, deps.OrgRepo, deps.MemberRepo, identity.NewOrganizationLockManager())
	owner := setupTestSession(t, db, deps)
	memberSess := addOrgMemberSession(t, db, owner, identity.RoleMember, "regular-member")
	s := newTestServer(t, deps)
	defer s.Close()

	req, _ := http.NewRequest(http.MethodPost, s.URL+"/api/orgs/"+owner.OrgID+"/disable", nil)
	req.AddCookie(memberSess.Cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-owner disable: status=%d, want 403", resp.StatusCode)
	}
	// And the org must remain enabled.
	if org, _ := deps.OrgRepo.GetByID(context.Background(), owner.OrgID); org == nil || org.IsDisabled() {
		t.Fatalf("org must stay enabled after a rejected non-owner disable")
	}
}
