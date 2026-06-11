package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/query"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
	"github.com/oopslink/agent-center/internal/secretmgmt"
	secretservice "github.com/oopslink/agent-center/internal/secretmgmt/service"
	secretsqlite "github.com/oopslink/agent-center/internal/secretmgmt/sqlite"
	wfsqlite "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

func setupAPI(t *testing.T) (HandlerDeps, *sql.DB) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	er, _ := obsqlite.NewEventRepo(ctx, db)
	sink := observability.NewEventSink(er, er, gen, clk)
	convRepo := convsqlite.NewConversationRepo(db)
	msgRepo := convsqlite.NewMessageRepo(db)
	refRepo := convsqlite.NewReferenceRepo(db)
	writer := convservice.NewMessageWriter(db, convRepo, msgRepo, sink, gen, clk)
	chSvc := convservice.NewChannelManagementService(db, convRepo, sink, gen, clk)
	pSvc := convservice.NewParticipantManagementService(db, convRepo, sink, clk)
	coSvc := convservice.NewCarryOverService(db, convRepo, msgRepo, refRepo, sink, gen, clk)
	rsRepo := convsqlite.NewReadStateRepo(db)
	rsSvc := convservice.NewReadStateService(db, rsRepo, msgRepo, sink, clk)
	fsRepo := convsqlite.NewFollowStateRepo(db)
	fsSvc := convservice.NewFollowStateService(fsRepo, convRepo, clk)
	fleetSvc := query.NewFleetSnapshotService(query.Deps{Events: er})
	aiRepo := wfsqlite.NewAgentInstanceRepo(db)
	// Wire UserSecret with a test master key.
	userSecretRepo := secretsqlite.NewUserSecretRepo(db)
	mk, err := secretmgmt.GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	userSecretSvc := secretservice.NewUserSecretService(db, userSecretRepo, gen, sink, clk, mk)
	deps := HandlerDeps{
		DB:                 db,
		Actor:              observability.Actor("user:hayang"),
		ConvRepo:           convRepo,
		MsgRepo:            msgRepo,
		MessageWriter:      writer,
		ChannelMgmtSvc:     chSvc,
		ParticipantMgmtSvc: pSvc,
		CarryOverSvc:       coSvc,
		FleetSvc:           fleetSvc,
		UserSecretRepo:     userSecretRepo,
		UserSecretSvc:      userSecretSvc,
		AgentInstanceRepo:  aiRepo,
		ReadStateRepo:      rsRepo,
		ReadStateSvc:       rsSvc,
		FollowStateSvc:     fsSvc,
	}
	return deps, db
}

// setupAPIWithAuth returns deps with identity (AuthSvc/OrgRepo/MemberRepo) wired
// using the fixed test signing key. Pair with setupTestSession for a valid
// cookie + org.
func setupAPIWithAuth(t *testing.T) (HandlerDeps, *sql.DB) {
	t.Helper()
	deps, db := setupAPI(t)
	deps.AuthSvc = identity.NewAuthService(identity.NewSQLiteIdentityRepo(db), testSigningKey)
	deps.IdentityRepo = identity.NewSQLiteIdentityRepo(db)
	deps.OrgRepo = identity.NewSQLiteOrganizationRepo(db)
	deps.MemberRepo = identity.NewSQLiteMemberRepo(db)
	// v2.7 B3: wire the ProjectManager service for the nested /api/projects/...
	// routes (the pm handlers in handlers_pm.go).
	deps.PM = pmservice.New(pmservice.Deps{
		DB:           db,
		Projects:     pmsql.NewProjectRepo(db),
		Members:      pmsql.NewProjectMemberRepo(db),
		Issues:       pmsql.NewIssueRepo(db),
		Tasks:        pmsql.NewTaskRepo(db),
		TaskSubs:     pmsql.NewTaskSubscriberRepo(db),
		IssueSubs:    pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db),
		Outbox:       outboxsql.NewOutboxRepo(db),
		IDGen:        idgen.NewGenerator(clock.SystemClock{}),
		Clock:        clock.SystemClock{},
		// #5a: assigning a Task to an agent grants it project membership, which is
		// cross-org-guarded via the AgentDirectory (and fail-closed when nil). Wire
		// the real directory over the test agent repo so agent assignment works.
		AgentDir: agentpkg.NewOrgDirectory(agentsql.NewAgentRepo(db)),
	})
	// v2.7 C3: wire the Agent BC AppService for the /api/agents... routes
	// (handlers_agent.go). Mirrors deps.PM: sqlite repos over the test DB + the
	// workforce WorkerRepo for the worker-in-org check & availability derivation.
	deps.AgentSvc = agentsvc.New(agentsvc.Deps{
		DB:        db,
		Agents:    agentsql.NewAgentRepo(db),
		WorkItems: agentsql.NewWorkItemRepo(db),
		Activity:  agentsql.NewActivityEventRepo(db),
		Workers:   wfsqlite.NewWorkerRepo(db),
		Outbox:    outboxsql.NewOutboxRepo(db),
		IDGen:     idgen.NewGenerator(clock.SystemClock{}),
		Clock:     clock.SystemClock{},
	})
	// v2.7 #157: agent identity-member provisioning (Members→Add Agent), incl. the
	// unified one-step create that also spins up the execution Agent.
	deps.AgentProvisionSvc = identity.NewAgentIdentityProvisionService(db, deps.IdentityRepo, deps.MemberRepo)
	return deps, db
}

// seedOrgChannel creates a channel conversation in orgID and returns its id.
// Used by conversation detail/message/participant/read-state tests now that
// those endpoints enforce requireConversationInOrg.
func seedOrgChannel(t *testing.T, deps HandlerDeps, orgID, name string) string {
	t.Helper()
	res, err := deps.ChannelMgmtSvc.CreateChannel(context.Background(), convservice.CreateChannelCommand{
		Name: name, OrganizationID: orgID, CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(res.ConversationID)
}

// testSession holds a signed-in identity + org + cookie for v2.6 org-scoped tests.
type testSession struct {
	IdentityID string
	OrgID      string
	OrgSlug    string
	Cookie     *http.Cookie
}

// setupTestSession provisions a test user identity + organization + member
// and returns a valid JWT cookie. Test requests can attach this cookie and
// pass ?org_slug=<slug> to satisfy the v2.6 org-scoped + membership checks.
func setupTestSession(t *testing.T, db *sql.DB, deps HandlerDeps) testSession {
	t.Helper()
	ctx := context.Background()
	hash, _ := identity.HashPasscode("123456")
	ident, err := identity.IdentityFactory{}.NewUser("testuser", hash)
	if err != nil {
		t.Fatal(err)
	}
	idRepo := identity.NewSQLiteIdentityRepo(db)
	orgRepo := identity.NewSQLiteOrganizationRepo(db)
	memberRepo := identity.NewSQLiteMemberRepo(db)
	if err := idRepo.Save(ctx, ident); err != nil {
		t.Fatal(err)
	}
	org, err := identity.OrganizationFactory{}.New("testorg", "Test Org", ident.ID())
	if err != nil {
		t.Fatal(err)
	}
	if err := orgRepo.Save(ctx, org); err != nil {
		t.Fatal(err)
	}
	member, err := identity.MemberFactory{}.New(org.ID(), ident.ID(), identity.RoleOwner, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := memberRepo.Save(ctx, member); err != nil {
		t.Fatal(err)
	}
	jwt, err := identity.MintJWT(ident.ID(), testSigningKey)
	if err != nil {
		t.Fatal(err)
	}
	return testSession{
		IdentityID: ident.ID(),
		OrgID:      org.ID(),
		OrgSlug:    org.Slug(),
		Cookie: &http.Cookie{
			Name:  "ac_session",
			Value: jwt,
		},
	}
}

// testSigningKey is the fixed JWT signing key used by setupAPIWithAuth and
// setupTestSession so cookies verify under test.
var testSigningKey = func() []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(i + 1)
	}
	return b
}()

// orgScopedURL rewrites a bare org-scoped URL into the v2.9 path form.
// `url` is a relative path like "/api/conversations" or "/api/projects/p1/tasks"
// (optionally with a query string). It is rewritten to
// "/api/orgs/<slug>/conversations" — the {slug} path segment requireOrgMember
// now reads (the legacy ?org_slug= query was deleted backend-side). A full URL
// (scheme://host/api/...) is rewritten in place so callers can pass ts.URL+path.
// Already-prefixed (/api/orgs/...) URLs and non-/api URLs are left untouched.
func orgScopedURL(url, slug string) string {
	const apiPrefix = "/api/"
	idx := strings.Index(url, apiPrefix)
	if idx < 0 {
		return url
	}
	head := url[:idx]            // "" or "scheme://host"
	rest := url[idx+len(apiPrefix):] // e.g. "conversations" or "orgs/s/x"
	if strings.HasPrefix(rest, "orgs/") || rest == "orgs" {
		return url // already path-scoped (or the org CRUD endpoint itself)
	}
	return head + "/api/orgs/" + slug + "/" + rest
}

// orgScopedPost executes a POST with the session cookie against the v2.9
// path-scoped org URL (/api/orgs/<slug>/...). Needed because once AuthSvc is
// wired the global authMiddleware gates every /api/* request and requireOrgMember
// reads the {slug} path segment.
func orgScopedPost(t *testing.T, url, body string, sess testSession) *http.Response {
	t.Helper()
	url = orgScopedURL(url, sess.OrgSlug)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(sess.Cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// orgScopedPatch executes a PATCH with the session cookie against the v2.9
// path-scoped org URL (/api/orgs/<slug>/...).
func orgScopedPatch(t *testing.T, url, body string, sess testSession) *http.Response {
	t.Helper()
	url = orgScopedURL(url, sess.OrgSlug)
	req, err := http.NewRequest(http.MethodPatch, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(sess.Cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// orgScopedDelete executes a DELETE with the session cookie against the v2.9
// path-scoped org URL (/api/orgs/<slug>/...).
func orgScopedDelete(t *testing.T, url string, sess testSession) *http.Response {
	t.Helper()
	url = orgScopedURL(url, sess.OrgSlug)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(sess.Cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// orgScopedGet executes a GET against the v2.9 path-scoped org URL
// (/api/orgs/<slug>/...) with the session cookie attached. Used by list
// endpoint tests now that requireOrgMember reads the {slug} path segment.
func orgScopedGet(t *testing.T, url string, sess testSession) *http.Response {
	t.Helper()
	url = orgScopedURL(url, sess.OrgSlug)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(sess.Cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func newTestServer(t *testing.T, deps HandlerDeps) *httptest.Server {
	srv := NewServer("127.0.0.1:0", Deps{})
	handler := WithDeps(deps)(srv.Handler())
	return httptest.NewServer(handler)
}

func TestAPI_Health(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, err := http.Get(s.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_ConversationsRoundTrip(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	ctx := context.Background()
	// Seed a channel via service.
	res, err := deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "platform", OrganizationID: sess.OrgID,
		CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, deps)
	defer s.Close()

	// GET /api/conversations?kind=channel
	resp := orgScopedGet(t, s.URL+"/api/conversations?kind=channel", sess)
	var arr []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		t.Fatal(err)
	}
	if len(arr) != 1 || arr[0]["name"] != "platform" {
		t.Fatalf("list: %v", arr)
	}

	// GET /api/conversations/{id}
	resp = orgScopedGet(t, s.URL+"/api/conversations/"+string(res.ConversationID), sess)
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["name"] != "platform" {
		t.Fatalf("show: %v", got)
	}
	parts, _ := got["participants"].([]any)
	if len(parts) != 1 {
		t.Fatalf("participants: %v", parts)
	}
}

func TestAPI_SendMessage(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "alpha")
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedPost(t, s.URL+"/api/conversations/"+cid+"/messages", `{"content":"hello world"}`, sess)
	if resp.StatusCode != 201 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out["message_id"] == nil {
		t.Fatalf("missing message_id: %v", out)
	}
	// List messages.
	resp = orgScopedGet(t, s.URL+"/api/conversations/"+cid+"/messages", sess)
	var msgs []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&msgs)
	if len(msgs) != 1 || msgs[0]["content"] != "hello world" {
		t.Fatalf("msgs: %v", msgs)
	}
}

func TestAPI_ConversationNotFound(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/conversations/nope", sess)
	if resp.StatusCode != 404 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_InviteParticipant(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "beta")
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/conversations/"+cid+"/participants", `{"identity_id":"agent:fixer","role":"member"}`, sess)
	if resp.StatusCode != 201 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_InvitePartOnDM_Rejected(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	ctx := context.Background()
	openRes, _ := deps.MessageWriter.OpenConversation(ctx, convservice.OpenCommand{
		Kind: conversation.ConversationKindDM, OrganizationID: sess.OrgID, CreatedBy: "user:hayang",
		Actor: observability.Actor("user:hayang"),
	})
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/conversations/"+string(openRes.ConversationID)+"/participants", `{"identity_id":"agent:x"}`, sess)
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_ArchiveConversation(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "gamma")
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/conversations/"+cid+"/archive", `{}`, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

// FleetSvc / QuerySvc unwired → 501 (graceful degrade until App wires).
func TestAPI_FleetWithoutSvc(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.FleetSvc = nil
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/orgs/_/fleet")
	if resp.StatusCode != 501 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_FleetSnapshot(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/fleet", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var snap map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&snap)
	if snap["generated_at"] == nil {
		t.Fatalf("expected generated_at: %v", snap)
	}
}

func TestServer_RefusesNonLoopbackBind(t *testing.T) {
	srv := NewServer("0.0.0.0:7100", Deps{})
	err := srv.ListenAndServe()
	if err == nil {
		_ = srv.Shutdown(context.Background())
		t.Fatal("expected error binding 0.0.0.0")
	}
	if !strings.Contains(err.Error(), "non-loopback") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestServer_DecodeJSON_EmptyBodyOK(t *testing.T) {
	// Empty body is allowed (handler decides whether fields are required).
	req := httptest.NewRequest("POST", "/x", http.NoBody)
	var got map[string]any
	if err := decodeJSON(req, &got); err != nil {
		t.Fatalf("empty body should not error: %v", err)
	}
}

func TestServer_DecodeJSON_BadJSON(t *testing.T) {
	req := httptest.NewRequest("POST", "/x", strings.NewReader("{not json"))
	var got map[string]any
	if err := decodeJSON(req, &got); err == nil {
		t.Fatal("expected parse error")
	}
}

// stubSPA is a minimal embedded-SPA stand-in for routing tests: it 200s every
// path so we can assert the auth middleware lets non-/api requests through.
func stubSPA() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("SPA:" + r.URL.Path))
	})
}

// v2.7 #145: the auth middleware must guard ONLY /api/* — the embedded SPA
// (/, /signin, /signup, /assets/*) must be served to a fresh/unauth visitor,
// not answered with a JSON 401. /api/* (non-public) stays gated; health +
// auth/* (incl. bootstrap) stay public.
func TestAPI_AuthMiddleware_OnlyGuardsAPI(t *testing.T) {
	deps, _ := setupAPIWithAuth(t)
	srv := NewServer("127.0.0.1:0", Deps{SPA: stubSPA()})
	s := httptest.NewServer(WithDeps(deps)(srv.Handler()))
	defer s.Close()

	for _, p := range []string{"/", "/signin", "/signup", "/assets/app.js"} {
		resp, err := http.Get(s.URL + p)
		if err != nil {
			t.Fatalf("GET %s: %v", p, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized {
			t.Errorf("GET %s (no cookie): got 401, want SPA passthrough", p)
		}
	}
	resp, err := http.Get(s.URL + "/api/orgs")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /api/orgs (no cookie): got %d, want 401", resp.StatusCode)
	}
	for _, p := range []string{"/api/health", "/api/auth/bootstrap"} {
		resp, err := http.Get(s.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized {
			t.Errorf("GET %s: got 401, want public", p)
		}
	}
}

// v2.7 #196 / FINDING-M: an unmatched or wrong-method /api/* path must return a
// JSON 4xx, NOT fall through to the SPA HTML catch-all. Before the fix, POST
// /api/agents (removed in #185) and any unknown /api path returned 200 + the SPA
// index.html, misleading programmatic clients. A non-/api path must still serve
// the SPA.
func TestAPI_UnmatchedAPIPath_JSON404NotSPA(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	srv := NewServer("127.0.0.1:0", Deps{SPA: stubSPA()})
	s := httptest.NewServer(WithDeps(deps)(srv.Handler()))
	defer s.Close()

	// Authenticated so the request passes the /api/* auth guard and reaches routing.
	for _, resp := range []*http.Response{
		orgScopedPost(t, s.URL+"/api/agents", `{}`, sess),      // route removed in #185
		orgScopedGet(t, s.URL+"/api/does-not-exist-xyz", sess), // never registered
	} {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Fatalf("unmatched /api path: got 200 (fell to SPA), want 4xx; body=%s", body)
		}
		if strings.HasPrefix(string(body), "SPA:") {
			t.Fatalf("unmatched /api path served SPA body %q, want JSON API error", body)
		}
	}

	// A non-/api path still serves the SPA (unaffected).
	resp, err := http.Get(s.URL + "/some/client/route")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(string(body), "SPA:") {
		t.Fatalf("non-/api path must still serve SPA, got %d %q", resp.StatusCode, body)
	}
}

// v2.7 #145: GET /api/auth/bootstrap reports initialized=false on a fresh
// install (no users) and initialized=true once any user exists — letting the
// SPA route to /signup vs /signin without an authenticated /api/orgs bounce.
func TestAPI_Bootstrap_ReflectsUserExistence(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	srv := NewServer("127.0.0.1:0", Deps{SPA: stubSPA()})
	s := httptest.NewServer(WithDeps(deps)(srv.Handler()))
	defer s.Close()

	if got := bootstrapInitialized(t, s.URL); got {
		t.Errorf("fresh install: initialized=true, want false")
	}
	setupTestSession(t, db, deps) // provisions a user identity
	if got := bootstrapInitialized(t, s.URL); !got {
		t.Errorf("after user provisioned: initialized=false, want true")
	}
}

func bootstrapInitialized(t *testing.T, base string) bool {
	t.Helper()
	resp, err := http.Get(base + "/api/auth/bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bootstrap: status %d", resp.StatusCode)
	}
	var body struct {
		Initialized bool `json:"initialized"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.Initialized
}

// v2.8.1: GET /api/system/version returns the full build identity (version /
// branch / commit / built_at) for the Settings version panel; public (auth-exempt
// like /api/health), with sentinels for unversioned builds.
func TestAPI_SystemVersion(t *testing.T) {
	srv := NewServer("127.0.0.1:0", Deps{
		Version: "v2.8.1-9908825", Branch: "v2.8.1", Commit: "9908825", BuiltAt: "2026-06-08T02:20:16Z",
	})
	s := httptest.NewServer(srv.Handler())
	defer s.Close()
	resp, err := http.Get(s.URL + "/api/system/version")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("got %d, want 200 (public, auth-exempt)", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"version": "v2.8.1-9908825", "branch": "v2.8.1", "commit": "9908825", "built_at": "2026-06-08T02:20:16Z",
	} {
		if body[k] != want {
			t.Errorf("%s = %v, want %q", k, body[k], want)
		}
	}
}

func TestAPI_SystemVersion_Fallbacks(t *testing.T) {
	srv := NewServer("127.0.0.1:0", Deps{}) // unversioned go-run path
	s := httptest.NewServer(srv.Handler())
	defer s.Close()
	resp, err := http.Get(s.URL + "/api/system/version")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["version"] != "dev" || body["branch"] != "unknown" || body["commit"] != "unknown" || body["built_at"] != "unknown" {
		t.Fatalf("unversioned fallbacks mismatch: %+v", body)
	}
}
