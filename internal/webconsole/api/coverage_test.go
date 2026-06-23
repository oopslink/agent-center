package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/secretmgmt"
	secretsvcCreate "github.com/oopslink/agent-center/internal/secretmgmt/service"
	"github.com/oopslink/agent-center/internal/webconsole/sse"
	"github.com/oopslink/agent-center/internal/workforce"
)

// ============================================================================
// Conversations — error / edge branches
// ============================================================================

func TestAPI_ListConversations_KindAndStatusFilter(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	ctx := context.Background()
	_, _ = deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "filterable", OrganizationID: sess.OrgID,
		CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	s := newTestServer(t, deps)
	defer s.Close()
	// Both kind + status filters set.
	resp := orgScopedGet(t, s.URL+"/api/conversations?kind=channel&status=active", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var arr []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&arr)
	if len(arr) != 1 {
		t.Fatalf("got %d", len(arr))
	}
}

// TestAPI_OrgScope_Isolation_And_Membership proves the v2.6 X1 §1 ship-block
// fix: org-scoped list endpoints (1) require a valid session, (2) require the
// caller to be a member of the requested org, (3) never fall back to returning
// all orgs' data when scope is missing, and (4) only return the requested
// org's rows.
func TestAPI_OrgScope_Isolation_And_Membership(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps) // org "testorg", caller is owner
	ctx := context.Background()
	// Seed a second org the caller is NOT a member of (real org with a slug so
	// the {slug} path resolves) + a channel in each org.
	otherOrg, err := identity.OrganizationFactory{}.New("otherorg", "Other Org", "user:other")
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.NewSQLiteOrganizationRepo(db).Save(ctx, otherOrg); err != nil {
		t.Fatal(err)
	}
	_, _ = deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "mine", OrganizationID: sess.OrgID, CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	_, _ = deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "theirs", OrganizationID: otherOrg.ID(), CreatedBy: "user:other", Actor: "user:other",
	})
	s := newTestServer(t, deps)
	defer s.Close()

	// (1) no cookie → 401 (path carries the org slug now)
	resp, _ := http.Get(s.URL + "/api/orgs/" + sess.OrgSlug + "/conversations")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no cookie: got %d want 401", resp.StatusCode)
	}

	// (3) cookie but unknown org slug → 400 (must NOT return all data)
	req, _ := http.NewRequest(http.MethodGet, s.URL+"/api/orgs/nonexistent-slug/conversations", nil)
	req.AddCookie(sess.Cookie)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown org slug: got %d want 400", resp.StatusCode)
	}

	// (2) member of a different org → 403 (valid slug, caller not a member)
	req, _ = http.NewRequest(http.MethodGet, s.URL+"/api/orgs/"+otherOrg.Slug()+"/conversations", nil)
	req.AddCookie(sess.Cookie)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-member org: got %d want 403", resp.StatusCode)
	}

	// (4) own org → 200 + only "mine"
	resp = orgScopedGet(t, s.URL+"/api/conversations?kind=channel", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("own org: got %d", resp.StatusCode)
	}
	var arr []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&arr)
	if len(arr) != 1 || arr[0]["name"] != "mine" {
		t.Fatalf("expected only own-org channel, got %v", arr)
	}
}

// TestAPI_OrgScope_DetailMutation_CrossOrgBlocked proves v2.6 X1 §1/§5: a
// detail/mutation request for a resource that belongs to another org returns
// 404 (no cross-org read or write), even though the caller is authenticated
// and a member of *their own* org.
func TestAPI_OrgScope_DetailMutation_CrossOrgBlocked(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps) // caller's org = "testorg"
	ctx := context.Background()
	// A conversation that lives in a DIFFERENT org.
	other, _ := deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "other-org-ch", OrganizationID: "organization-other",
		CreatedBy: "user:other", Actor: "user:other",
	})
	s := newTestServer(t, deps)
	defer s.Close()

	// Detail read of another org's conversation → 404.
	resp := orgScopedGet(t, s.URL+"/api/conversations/"+string(other.ConversationID), sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-org conversation detail: got %d want 404", resp.StatusCode)
	}
	// Message write into another org's conversation → 404.
	resp = orgScopedPost(t, s.URL+"/api/conversations/"+string(other.ConversationID)+"/messages", `{"content":"x"}`, sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-org message write: got %d want 404", resp.StatusCode)
	}
	// Archive another org's conversation → 404.
	resp = orgScopedPost(t, s.URL+"/api/conversations/"+string(other.ConversationID)+"/archive", `{"version":1}`, sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-org archive: got %d want 404", resp.StatusCode)
	}
}

func TestAPI_SendMessage_DefaultsContentKindAndDirection(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "smdefaults")
	s := newTestServer(t, deps)
	defer s.Close()
	// Body has only content; handler defaults.
	resp := orgScopedPost(t, s.URL+"/api/conversations/"+cid+"/messages", `{"content":"x"}`, sess)
	if resp.StatusCode != 201 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_SendMessage_BadJSON(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "smbadjson")
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/conversations/"+cid+"/messages", `{not json`, sess)
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_ArchiveConversation_NotFound(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/conversations/nope/archive", `{"version":1}`, sess)
	if resp.StatusCode != 404 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_RemoveParticipant_Happy(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	ctx := context.Background()
	// v2.7 #146: the kicker is the logged-in session user, and Kick requires the
	// caller to be the channel owner — so the session user must own the channel
	// (mirrors production: the creator opens it, then kicks).
	sref := conversation.IdentityRef("user:" + sess.IdentityID)
	cres, err := deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "remove-test", OrganizationID: sess.OrgID, CreatedBy: sref, Actor: observability.Actor(sref),
	})
	if err != nil {
		t.Fatal(err)
	}
	cid := string(cres.ConversationID)
	_, _ = deps.ParticipantMgmtSvc.Invite(ctx, convservice.InviteCommand{
		ConversationName: "remove-test", IdentityID: "user:bob",
		InvitedBy: sref, Actor: observability.Actor(sref),
	})
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedDelete(t, s.URL+"/api/conversations/"+cid+"/participants/user:bob", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_RemoveParticipant_OnDM_Rejected(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	ctx := context.Background()
	openRes, _ := deps.MessageWriter.OpenConversation(ctx, convservice.OpenCommand{
		Kind: conversation.ConversationKindDM, OrganizationID: sess.OrgID, CreatedBy: "user:hayang",
		Participants: []conversation.ParticipantElement{
			{IdentityID: "user:hayang", Role: "owner", JoinedAt: "t", JoinedBy: "user:hayang"},
			{IdentityID: "agent:agent-peer", Role: "member", JoinedAt: "t", JoinedBy: "user:hayang"},
		},
		Actor: observability.Actor("user:hayang"),
	})
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedDelete(t, s.URL+"/api/conversations/"+string(openRes.ConversationID)+"/participants/user:bob", sess)
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_InviteParticipant_NotFound(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/conversations/nope/participants", `{"identity_id":"user:bob"}`, sess)
	if resp.StatusCode != 404 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_InviteParticipant_MissingIdentityID(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "inv-missing")
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/conversations/"+cid+"/participants", `{"role":"member"}`, sess)
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_InviteParticipant_BadJSON(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "inv-badjson")
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/conversations/"+cid+"/participants", `{nope`, sess)
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

// ============================================================================
// Agents
// ============================================================================
//
// The legacy workforce.AgentInstance /api/agents tests (ListAgents_Empty,
// ShowAgent_NotFound, ListAgents_DBError) + their fakeAgentRepo were removed
// when v2.7 C3 replaced that surface with the new Agent BC. The new surface is
// covered by handlers_agent_test.go (+ internal/agent/service tests).

// ============================================================================
// Secrets
// ============================================================================

// Not-wired branches — explicitly nil-out svc / repo even though
// setupAPI now wires them.
func TestAPI_ListSecrets_NotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.UserSecretRepo = nil
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/orgs/_/secrets")
	if resp.StatusCode != 501 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_CreateSecret_NotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.UserSecretSvc = nil
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/orgs/_/secrets", "application/json",
		strings.NewReader(`{"name":"x","value":"v"}`))
	if resp.StatusCode != 501 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_RevokeSecret_NotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.UserSecretSvc = nil
	s := newTestServer(t, deps)
	defer s.Close()
	req, _ := http.NewRequest("DELETE", s.URL+"/api/orgs/_/secrets/x", nil)
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 501 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

// Happy paths — svc wired.
func TestAPI_CreateSecret_Happy(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"name":"api-key","kind":"cloud_credential","value":"deadbeef"}`
	resp, _ := http.Post(s.URL+"/api/orgs/_/secrets", "application/json", strings.NewReader(body))
	if resp.StatusCode != 201 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	// Plaintext MUST NOT be echoed back.
	if _, ok := out["value"]; ok {
		t.Fatalf("response leaked plaintext: %v", out)
	}
}

func TestAPI_CreateSecret_MissingValue(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/orgs/_/secrets", "application/json",
		strings.NewReader(`{"name":"x"}`))
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_CreateSecret_BadJSON(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/orgs/_/secrets", "application/json", strings.NewReader(`{x`))
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_ListSecrets_AfterCreate(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	_, err := deps.UserSecretSvc.Create(context.Background(), secretsvcCreate.CreateSecretCommand{
		Name: "k", Kind: secretmgmt.UserSecretKindOther,
		Plaintext: []byte("v"), OrganizationID: sess.OrgID,
		ActorIdentity: observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/secrets", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var arr []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&arr)
	if len(arr) != 1 {
		t.Fatalf("got %d", len(arr))
	}
}

func TestAPI_RevokeSecret_Happy(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	res, err := deps.UserSecretSvc.Create(context.Background(), secretsvcCreate.CreateSecretCommand{
		Name: "k", Kind: secretmgmt.UserSecretKindOther,
		Plaintext: []byte("v"), OrganizationID: sess.OrgID,
		ActorIdentity: observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedDelete(t, s.URL+"/api/secrets/"+string(res.ID), sess)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_RevokeSecret_NotFound(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedDelete(t, s.URL+"/api/secrets/nope", sess)
	if resp.StatusCode != 404 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

type fakeSecretRepo struct{}

func (fakeSecretRepo) FindByID(ctx context.Context, id secretmgmt.UserSecretID) (*secretmgmt.UserSecret, error) {
	return nil, secretmgmt.ErrUserSecretNotFound
}
func (fakeSecretRepo) FindByName(ctx context.Context, name string) (*secretmgmt.UserSecret, error) {
	return nil, secretmgmt.ErrUserSecretNotFound
}
func (fakeSecretRepo) FindAll(ctx context.Context, filter secretmgmt.UserSecretFilter) ([]*secretmgmt.UserSecret, error) {
	return nil, nil
}
func (fakeSecretRepo) Save(ctx context.Context, s *secretmgmt.UserSecret) error { return nil }
func (fakeSecretRepo) UpdateValue(ctx context.Context, id secretmgmt.UserSecretID, ciphertext, nonce []byte, rotatedAt time.Time, version int) error {
	return nil
}
func (fakeSecretRepo) UpdateState(ctx context.Context, id secretmgmt.UserSecretID, from, to secretmgmt.UserSecretState, at time.Time, by string, reason secretmgmt.UserSecretRevokedReason, message string, version int) error {
	return nil
}
func (fakeSecretRepo) UpdateLastUsedAt(ctx context.Context, id secretmgmt.UserSecretID, at time.Time) error {
	return nil
}

// ============================================================================
// SSE handler integration
// ============================================================================

func TestAPI_SSE_NotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	srv := NewServer("127.0.0.1:0", Deps{}) // no SSE bus
	handler := WithDeps(deps)(srv.Handler())
	s := httptest.NewServer(handler)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/sse?user_id=u1")
	if resp.StatusCode != 501 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	resp, _ = http.Post(s.URL+"/api/sse/subscribe", "application/json",
		strings.NewReader(`{"user_id":"u1","conversation_id":"c"}`))
	if resp.StatusCode != 501 {
		t.Fatalf("subscribe got %d", resp.StatusCode)
	}
	resp, _ = http.Post(s.URL+"/api/sse/unsubscribe", "application/json",
		strings.NewReader(`{"user_id":"u1","conversation_id":"c"}`))
	if resp.StatusCode != 501 {
		t.Fatalf("unsubscribe got %d", resp.StatusCode)
	}
}

func TestAPI_SSE_SubscribeUnsubscribe(t *testing.T) {
	deps, _ := setupAPI(t)
	bus := sse.NewBus()
	srv := NewServer("127.0.0.1:0", Deps{SSE: bus})
	handler := WithDeps(deps)(srv.Handler())
	s := httptest.NewServer(handler)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/sse/subscribe", "application/json",
		strings.NewReader(`{"user_id":"u1","conversation_id":"c-1"}`))
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	if !bus.IsSubscribed("u1", "c-1") {
		t.Fatal()
	}
	resp, _ = http.Post(s.URL+"/api/sse/unsubscribe", "application/json",
		strings.NewReader(`{"user_id":"u1","conversation_id":"c-1"}`))
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	if bus.IsSubscribed("u1", "c-1") {
		t.Fatal()
	}
}

func TestAPI_SSE_Subscribe_BadJSON(t *testing.T) {
	deps, _ := setupAPI(t)
	srv := NewServer("127.0.0.1:0", Deps{SSE: sse.NewBus()})
	handler := WithDeps(deps)(srv.Handler())
	s := httptest.NewServer(handler)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/sse/subscribe", "application/json",
		strings.NewReader(`{nope`))
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	resp, _ = http.Post(s.URL+"/api/sse/unsubscribe", "application/json",
		strings.NewReader(`{nope`))
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_SSE_SubscribeRejectsEmptyArgs(t *testing.T) {
	deps, _ := setupAPI(t)
	srv := NewServer("127.0.0.1:0", Deps{SSE: sse.NewBus()})
	handler := WithDeps(deps)(srv.Handler())
	s := httptest.NewServer(handler)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/sse/subscribe", "application/json",
		strings.NewReader(`{}`))
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

// ============================================================================
// mapDomainError matrix
// ============================================================================

func TestMapDomainError_Matrix(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{conversation.ErrConversationNotFound, 404},
		{conversation.ErrMessageNotFound, 404},
		{workforce.ErrAgentInstanceNotFound, 404},
		{secretmgmt.ErrUserSecretNotFound, 404},
		{conversation.ErrConversationVersionConflict, 409},
		{secretmgmt.ErrUserSecretVersionConflict, 409},
		{conversation.ErrConversationArchived, 409}, // v2.9.1 task-169c598d: archived = read-only → 409 (project-archive parity)
		{conversation.ErrConversationClosed, 403},
		{conversation.ErrConversationAlreadyExists, 409},
		{convservice.ErrParticipantAlreadyActive, 409},
		{errors.New("unmapped"), 500},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		mapDomainError(rec, c.err)
		if rec.Code != c.want {
			t.Errorf("err %v: got %d want %d", c.err, rec.Code, c.want)
		}
	}
}

// ============================================================================
// Public projection helpers — exercise via real ARs
// ============================================================================

func TestSecretPublicMap_RevokedFieldsPresent(t *testing.T) {
	// Build a revoked UserSecret via the AR direct API and project.
	now := time.Now().UTC()
	sec, err := secretmgmt.NewUserSecret(secretmgmt.NewUserSecretInput{
		ID: "S-1", Name: "x", Kind: secretmgmt.UserSecretKindOther,
		Ciphertext: []byte{1, 2, 3}, Nonce: []byte{4, 5, 6},
		CreatedAt: now, CreatedBy: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sec.Revoke(now, "user:hayang", secretmgmt.UserSecretRevokedReasonManual, "done"); err != nil {
		t.Fatal(err)
	}
	m := secretPublicMap(sec)
	if m["state"] != "revoked" {
		t.Fatalf("state: %v", m["state"])
	}
	if m["revoked_by"] != "user:hayang" {
		t.Fatalf("revoked_by: %v", m["revoked_by"])
	}
}

func TestAgentPublicMap_NilWorkerID(t *testing.T) {
	now := time.Now().UTC()
	ai, err := workforce.NewAgentInstance(workforce.NewAgentInstanceInput{
		ID: "AI-1", Name: "builtin", AgentCLI: "claudecode",
		IsBuiltin: true, Config: "{}", CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := agentPublicMap(ai)
	if m["worker_id"] != "" {
		t.Fatalf("worker_id: %v", m["worker_id"])
	}
	if m["is_builtin"] != true {
		t.Fatalf("is_builtin: %v", m["is_builtin"])
	}
}

func TestConvPublicMap_ArchivedFields(t *testing.T) {
	now := time.Now().UTC()
	c, err := conversation.NewConversation(conversation.NewConversationInput{
		ID: "C-1", Kind: conversation.ConversationKindChannel,
		Name: "alpha", CreatedBy: "user:hayang", OpenedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Archive(now, "user:hayang")
	m := convPublicMap(c)
	if m["archived_by"] != "user:hayang" {
		t.Fatalf("got %v", m["archived_by"])
	}
}

// ============================================================================
// Server lifecycle
// ============================================================================

func TestServer_SetHandlerThenServe(t *testing.T) {
	srv := NewServer("127.0.0.1:0", Deps{})
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	})
	srv.SetHandler(wrapped)
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("idempotent shutdown: %v", err)
	}
}

// ============================================================================
// Error path coverage (force repo errors via closed DB)
// ============================================================================

func TestAPI_ListConversations_DBError(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	// After session is provisioned, close the DB to force every subsequent
	// query (auth lookup, list query, etc.) to fail. The auth-cookie path
	// will hit Identity.GetByID first; that error path also returns 500/401
	// depending on the layer. We accept either as "broke as expected".
	db.Close()
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/conversations", sess)
	if resp.StatusCode == 200 {
		t.Fatalf("expected non-200, got %d", resp.StatusCode)
	}
}

func TestAPI_ListMessages_DBError(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	db.Close()
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/conversations/x/messages", sess)
	if resp.StatusCode == 200 {
		t.Fatalf("expected non-200 on closed db, got %d", resp.StatusCode)
	}
}

func TestAPI_ShowConversation_DBError(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	db.Close()
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/conversations/x", sess)
	if resp.StatusCode == 200 {
		t.Fatalf("expected non-200 on closed db, got %d", resp.StatusCode)
	}
}

func TestAPI_ListSecrets_DBError(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	db.Close()
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/secrets", sess)
	if resp.StatusCode == 200 {
		t.Fatalf("expected non-200, got %d", resp.StatusCode)
	}
}

func TestAPI_RemoveParticipant_NotFound(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedDelete(t, s.URL+"/api/conversations/nope/participants/user:x", sess)
	if resp.StatusCode != 404 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_Archive_DBError(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	db.Close()
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/conversations/x/archive", `{"version":1}`, sess)
	if resp.StatusCode == 200 {
		t.Fatalf("expected non-200 on closed db, got %d", resp.StatusCode)
	}
}

func TestAPI_ArchiveConversation_BadJSON(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "arch-badjson")
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/conversations/"+cid+"/archive", `{not json`, sess)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d want 400", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "invalid_json" {
		t.Fatalf("error=%v want invalid_json", body["error"])
	}
}

