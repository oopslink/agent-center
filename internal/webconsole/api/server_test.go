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

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	disqlite "github.com/oopslink/agent-center/internal/discussion/sqlite"
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
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
	trsqlite "github.com/oopslink/agent-center/internal/taskruntime/sqlite"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
	"github.com/oopslink/agent-center/internal/workforce"
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
	// Query svc with minimal deps; covers /api/tasks/{id}/trace endpoint.
	querySvc := query.NewService(query.Deps{Events: er})
	fleetSvc := query.NewFleetSnapshotService(query.Deps{Events: er})
	irRepo := trsqlite.NewInputRequestRepo(db)
	taskRepo := trsqlite.NewTaskRepo(db)
	execRepo := trsqlite.NewTaskExecutionRepo(db)
	irSvc := trservice.NewInputRequestService(db, irRepo, execRepo, taskRepo, convRepo, msgRepo,
		sink, gen, clk, "")
	aiRepo := wfsqlite.NewAgentInstanceRepo(db)
	issueRepo := disqlite.NewIssueRepo(db)
	// Wire UserSecret with a test master key.
	userSecretRepo := secretsqlite.NewUserSecretRepo(db)
	mk, err := secretmgmt.GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	userSecretSvc := secretservice.NewUserSecretService(db, userSecretRepo, gen, sink, clk, mk)
	deps := HandlerDeps{
		Actor:              observability.Actor("user:hayang"),
		ConvRepo:           convRepo,
		MsgRepo:            msgRepo,
		MessageWriter:      writer,
		ChannelMgmtSvc:     chSvc,
		ParticipantMgmtSvc: pSvc,
		CarryOverSvc:       coSvc,
		QuerySvc:           querySvc,
		FleetSvc:           fleetSvc,
		IRRepo:             irRepo,
		ExecRepo:           execRepo,
		IRSvc:              irSvc,
		UserSecretRepo:     userSecretRepo,
		UserSecretSvc:      userSecretSvc,
		AgentInstanceRepo:  aiRepo,
		ReadStateRepo:      rsRepo,
		ReadStateSvc:       rsSvc,
		IssueRepo:          issueRepo,
		TaskRepo:           taskRepo,
	}
	return deps, db
}

// setupAPIWithAuth returns deps with identity (AuthSvc/OrgRepo/MemberRepo) wired
// using the fixed test signing key, plus a real ProjectRepo so org-scoped
// issue/task/IR/fleet endpoints can resolve the org's project set. Pair with
// setupTestSession for a valid cookie + org, and seedOrgProject to put a
// project in that org.
func setupAPIWithAuth(t *testing.T) (HandlerDeps, *sql.DB) {
	t.Helper()
	deps, db := setupAPI(t)
	deps.AuthSvc = identity.NewAuthService(identity.NewSQLiteIdentityRepo(db), testSigningKey)
	deps.OrgRepo = identity.NewSQLiteOrganizationRepo(db)
	deps.MemberRepo = identity.NewSQLiteMemberRepo(db)
	deps.ProjectRepo = wfsqlite.NewProjectRepo(db)
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
	})
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

// seedOrgProject creates a project belonging to orgID with the given id so
// org-scoped issue/task/IR/fleet reads (which resolve via ProjectRepo) pass.
func seedOrgProject(t *testing.T, db *sql.DB, orgID, projectID, name string) {
	t.Helper()
	p, err := workforce.NewProject(workforce.NewProjectInput{
		ID:                  workforce.ProjectID(projectID),
		Name:                name,
		OrganizationID:      orgID,
		CreatedByIdentityID: "user:hayang",
		CreatedAt:           time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := wfsqlite.NewProjectRepo(db).Save(context.Background(), p); err != nil {
		t.Fatal(err)
	}
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

// orgScopedPost executes a POST with the session cookie + ?org_slug attached.
// Needed because once AuthSvc is wired the global authMiddleware gates every
// /api/* request (not just the org-scoped reads).
func orgScopedPost(t *testing.T, url, body string, sess testSession) *http.Response {
	t.Helper()
	if !strings.Contains(url, "?") {
		url += "?org_slug=" + sess.OrgSlug
	} else {
		url += "&org_slug=" + sess.OrgSlug
	}
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

// orgScopedPatch executes a PATCH with the session cookie + ?org_slug attached.
func orgScopedPatch(t *testing.T, url, body string, sess testSession) *http.Response {
	t.Helper()
	if !strings.Contains(url, "?") {
		url += "?org_slug=" + sess.OrgSlug
	} else {
		url += "&org_slug=" + sess.OrgSlug
	}
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

// orgScopedDelete executes a DELETE with the session cookie + ?org_slug attached.
func orgScopedDelete(t *testing.T, url string, sess testSession) *http.Response {
	t.Helper()
	if !strings.Contains(url, "?") {
		url += "?org_slug=" + sess.OrgSlug
	} else {
		url += "&org_slug=" + sess.OrgSlug
	}
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

// orgScopedGet executes a GET against url with the session cookie attached
// and ?org_slug=<sess.OrgSlug> appended (merging with existing query params).
// Used by list endpoint tests now that requireOrgMember enforces strict
// org membership at the API boundary.
func orgScopedGet(t *testing.T, url string, sess testSession) *http.Response {
	t.Helper()
	if !strings.Contains(url, "?") {
		url += "?org_slug=" + sess.OrgSlug
	} else {
		url += "&org_slug=" + sess.OrgSlug
	}
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
	resp, _ := http.Get(s.URL + "/api/fleet")
	if resp.StatusCode != 501 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_TaskTraceWithoutSvc(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.QuerySvc = nil
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/tasks/t-1/trace")
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

func TestAPI_TaskTrace(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	seedOrgProject(t, db, sess.OrgID, "p-1", "P1")
	// Seed task t-1 in the org's project so the trace gate (task→project→org) passes.
	tk, _ := task.New(task.NewInput{ID: "t-1", ProjectID: "p-1", Title: "x", CreatedBy: "user:hayang", Now: time.Now()})
	if err := deps.TaskRepo.Save(context.Background(), tk); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/tasks/t-1/trace", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var res map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if res["resource"] != "events" {
		t.Fatalf("expected resource=events: %v", res)
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
