package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	"github.com/oopslink/agent-center/internal/identity"
)

// createAgentViaAPI creates a stopped agent (POST /api/members/agent) and returns
// its business id (identity_member id, "agent-<ulid>").
func createAgentViaAPI(t *testing.T, s *httptest.Server, sess testSession, workerID string) string {
	t.Helper()
	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"coder","model":"claude","cli":"claude-code","worker_id":"`+workerID+`"}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create agent: got %d", resp.StatusCode)
	}
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	id, _ := created["identity_id"].(string)
	if id == "" {
		t.Fatalf("missing agent business id: %v", created)
	}
	return id
}

// memberSessionInOrg provisions a RoleMember (non-admin) identity in an existing
// org and returns its session — for admin-gate tests.
func memberSessionInOrg(t *testing.T, db *sql.DB, orgID, orgSlug string) testSession {
	t.Helper()
	ctx := context.Background()
	hash, _ := identity.HashPasscode("123456")
	ident, err := identity.IdentityFactory{}.NewUser("plainmember", hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.NewSQLiteIdentityRepo(db).Save(ctx, ident); err != nil {
		t.Fatal(err)
	}
	member, err := identity.MemberFactory{}.New(orgID, ident.ID(), identity.RoleMember, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.NewSQLiteMemberRepo(db).Save(ctx, member); err != nil {
		t.Fatal(err)
	}
	jwt, err := identity.MintJWT(ident.ID(), testSigningKey)
	if err != nil {
		t.Fatal(err)
	}
	return testSession{
		IdentityID: ident.ID(), OrgID: orgID, OrgSlug: orgSlug,
		Cookie: &http.Cookie{Name: "ac_session", Value: jwt},
	}
}

func TestAPI_AgentArchive_HappyPath_ListExcludes(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	s := newTestServer(t, deps)
	defer s.Close()
	id := createAgentViaAPI(t, s, sess, "w-1") // stopped

	// Archive → 200 + lifecycle=archived.
	resp := orgScopedPost(t, s.URL+"/api/agents/"+id+"/archive", `{}`, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("archive: got %d", resp.StatusCode)
	}
	var arch map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&arch)
	if arch["lifecycle"] != "archived" {
		t.Fatalf("lifecycle=%v want archived", arch["lifecycle"])
	}

	// GET by id still resolves (history).
	resp = orgScopedGet(t, s.URL+"/api/agents/"+id, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("archived GET-by-id: got %d want 200", resp.StatusCode)
	}

	// List excludes archived by default.
	if ids := listAgentIDs(t, s, sess, false); contains(ids, id) {
		t.Fatalf("default list must exclude archived agent %s", id)
	}
	// ?include_archived=true includes it.
	if ids := listAgentIDs(t, s, sess, true); !contains(ids, id) {
		t.Fatalf("include_archived list must contain %s", id)
	}
}

func listAgentIDs(t *testing.T, s *httptest.Server, sess testSession, includeArchived bool) []string {
	t.Helper()
	url := s.URL + "/api/agents"
	if includeArchived {
		url += "?include_archived=true" // orgScopedGet rewrites the path to /api/orgs/<slug>/agents, query preserved
	}
	resp := orgScopedGet(t, url, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("list agents: got %d", resp.StatusCode)
	}
	var body struct {
		Agents []map[string]any `json:"agents"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	out := make([]string, 0, len(body.Agents))
	for _, a := range body.Agents {
		if v, ok := a["id"].(string); ok {
			out = append(out, v)
		}
	}
	return out
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestAPI_AgentArchive_RunningRejected_409(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	s := newTestServer(t, deps)
	defer s.Close()
	id := createAgentViaAPI(t, s, sess, "w-1")

	// Move it to running via the wired service.
	a, err := deps.AgentSvc.ResolveAgent(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if err := deps.AgentSvc.StartAgent(context.Background(), a.ID()); err != nil {
		t.Fatal(err)
	}
	resp := orgScopedPost(t, s.URL+"/api/agents/"+id+"/archive", `{}`, sess)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("archive running: got %d want 409", resp.StatusCode)
	}
	var e map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&e)
	if e["error"] != "invalid_state" {
		t.Fatalf("error=%v want invalid_state", e["error"])
	}
}

func TestAPI_AgentArchive_Idempotent(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	s := newTestServer(t, deps)
	defer s.Close()
	id := createAgentViaAPI(t, s, sess, "w-1")

	if resp := orgScopedPost(t, s.URL+"/api/agents/"+id+"/archive", `{}`, sess); resp.StatusCode != 200 {
		t.Fatalf("first archive: %d", resp.StatusCode)
	}
	if resp := orgScopedPost(t, s.URL+"/api/agents/"+id+"/archive", `{}`, sess); resp.StatusCode != 200 {
		t.Fatalf("re-archive must be 200 no-op, got %d", resp.StatusCode)
	}
}

func TestAPI_AgentArchive_CrossOrg404(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	saveWorkerInOrg(t, db, "org-other", "w-other")
	foreignID, err := deps.AgentSvc.CreateAgent(context.Background(), agentsvc.CreateAgentCommand{
		OrganizationID: "org-other", Name: "foreign", Model: "claude", CLI: "claude-code",
		WorkerID: "w-other", CreatedBy: "user:someone",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := orgScopedPost(t, s.URL+"/api/agents/"+string(foreignID)+"/archive", `{}`, sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-org archive: got %d want 404", resp.StatusCode)
	}
}

// v2.8 #272: hard DELETE is admin-only — a non-admin member is rejected (403),
// so there is no user-reachable hard-delete path (archive is the user path).
func TestAPI_AgentDelete_AdminOnly_NonAdmin403(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	owner := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, owner.OrgID, "w-1")
	s := newTestServer(t, deps)
	defer s.Close()
	id := createAgentViaAPI(t, s, owner, "w-1")

	// A non-admin member of the SAME org cannot hard-delete.
	memberSess := memberSessionInOrg(t, db, owner.OrgID, owner.OrgSlug)
	resp := orgScopedDelete(t, s.URL+"/api/agents/"+id, memberSess)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin hard delete: got %d want 403", resp.StatusCode)
	}
}
