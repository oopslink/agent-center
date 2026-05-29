package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/secretmgmt"
	secretsvcCreate "github.com/oopslink/agent-center/internal/secretmgmt/service"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
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
	// Seed a channel in the caller's org and one in a different org.
	_, _ = deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "mine", OrganizationID: sess.OrgID, CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	_, _ = deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "theirs", OrganizationID: "organization-other", CreatedBy: "user:other", Actor: "user:other",
	})
	s := newTestServer(t, deps)
	defer s.Close()

	// (1) no cookie → 401
	resp, _ := http.Get(s.URL + "/api/conversations?org_slug=" + sess.OrgSlug)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no cookie: got %d want 401", resp.StatusCode)
	}

	// (3) cookie but no org scope → 400 (must NOT return all data)
	req, _ := http.NewRequest(http.MethodGet, s.URL+"/api/conversations", nil)
	req.AddCookie(sess.Cookie)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("no org scope: got %d want 400", resp.StatusCode)
	}

	// (2) member of a different org → 403
	req, _ = http.NewRequest(http.MethodGet, s.URL+"/api/conversations?org_id=organization-other", nil)
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
	cid := seedOrgChannel(t, deps, sess.OrgID, "remove-test")
	_, _ = deps.ParticipantMgmtSvc.Invite(ctx, convservice.InviteCommand{
		ConversationName: "remove-test", IdentityID: "user:bob",
		InvitedBy: "user:hayang", Actor: observability.Actor("user:hayang"),
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
// Derivation — minimal stubs
// ============================================================================

type fakeIssueOpener struct {
	called bool
	deps   HandlerDeps
}

func (f *fakeIssueOpener) OpenFromConversation(ctx context.Context, in convservice.OpenFromConversationInput) (convservice.OpenFromConversationResult, error) {
	f.called = true
	// Create a real child conv so subsequent CarryOver.Materialise can
	// validate the child exists.
	id := conversation.ConversationID("CHILD-ISSUE")
	conv, _ := conversation.NewConversation(conversation.NewConversationInput{
		ID: id, Kind: conversation.ConversationKindIssue,
		Name: in.Title, CreatedBy: in.OpenedBy,
		OpenedAt: time.Now().UTC(),
	})
	_ = f.deps.ConvRepo.Save(ctx, conv)
	return convservice.OpenFromConversationResult{IssueID: "I-1", ConversationID: id, EventID: "E-X"}, nil
}

type fakeTaskCreator struct {
	called bool
	deps   HandlerDeps
}

func (f *fakeTaskCreator) CreateFromConversation(ctx context.Context, in convservice.CreateFromConversationInput) (convservice.CreateFromConversationResult, error) {
	f.called = true
	id := conversation.ConversationID("CHILD-TASK")
	conv, _ := conversation.NewConversation(conversation.NewConversationInput{
		ID: id, Kind: conversation.ConversationKindTask,
		Name: in.Title, CreatedBy: in.CreatedBy,
		OpenedAt: time.Now().UTC(),
	})
	_ = f.deps.ConvRepo.Save(ctx, conv)
	return convservice.CreateFromConversationResult{TaskID: "T-1", ConversationID: id}, nil
}

func setupDerive(t *testing.T) (HandlerDeps, *fakeIssueOpener, *fakeTaskCreator, testSession) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	seedOrgProject(t, db, sess.OrgID, "p-1", "P1")
	// Seed a source channel (in the org) + 1 message so derive validates.
	ctx := context.Background()
	res, _ := deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "src", OrganizationID: sess.OrgID, CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	mw, _ := deps.MessageWriter.AddMessage(ctx, convservice.AddMessageCommand{
		ConversationID:   res.ConversationID,
		SenderIdentityID: "user:hayang",
		ContentKind:      conversation.MessageContentText,
		Direction:        conversation.DirectionInbound,
		Content:          "x",
		Actor:            observability.Actor("user:hayang"),
	})
	io := &fakeIssueOpener{deps: deps}
	tc := &fakeTaskCreator{deps: deps}
	refRepo := convsqlite.NewReferenceRepo(db)
	carryOver := convservice.NewCarryOverService(db, deps.ConvRepo, deps.MsgRepo, refRepo,
		nil, nil, nil)
	_ = carryOver
	deps.DerivationSvc = convservice.NewMessageDerivationService(db,
		deps.ConvRepo, deps.MsgRepo, deps.CarryOverSvc, io, tc, nil, nil)
	_ = mw
	_ = res
	return deps, io, tc, sess
}

func TestAPI_DeriveIssue_Happy(t *testing.T) {
	deps, io, _, sess := setupDerive(t)
	s := newTestServer(t, deps)
	defer s.Close()
	// fetch the seeded conv + msg.
	src, _ := deps.ConvRepo.FindByName(context.Background(), "src")
	msgs, _ := deps.MsgRepo.FindByConversationID(context.Background(), src.ID(),
		conversation.MessageFilter{Limit: 10})
	body, _ := json.Marshal(map[string]any{
		"source_conversation_id": string(src.ID()),
		"source_message_ids":     []string{string(msgs[0].ID())},
		"project_id":             "p-1",
		"title":                  "X",
		"description":            "",
	})
	resp := orgScopedPost(t, s.URL+"/api/issues", string(body), sess)
	if resp.StatusCode != 201 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	if !io.called {
		t.Fatal("opener not invoked")
	}
}

func TestAPI_DeriveIssue_DerivationNotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	// DerivationSvc nil; payload carries source fields so the post handler
	// routes into the derive branch (v2.5.x #61: empty source now means
	// open-from-scratch instead of derive-not-wired).
	deps.DerivationSvc = nil
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"source_conversation_id":"C-1","source_message_ids":["M-1"]}`
	resp, _ := http.Post(s.URL+"/api/issues", "application/json", strings.NewReader(body))
	if resp.StatusCode != 501 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_DeriveIssue_BadJSON(t *testing.T) {
	deps, _, _, sess := setupDerive(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/issues", `{not`, sess)
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_DeriveTask_Happy(t *testing.T) {
	deps, _, tc, sess := setupDerive(t)
	s := newTestServer(t, deps)
	defer s.Close()
	src, _ := deps.ConvRepo.FindByName(context.Background(), "src")
	msgs, _ := deps.MsgRepo.FindByConversationID(context.Background(), src.ID(),
		conversation.MessageFilter{Limit: 10})
	body, _ := json.Marshal(map[string]any{
		"source_conversation_id": string(src.ID()),
		"source_message_ids":     []string{string(msgs[0].ID())},
		"project_id":             "p-1",
		"title":                  "T",
		"agent_instance_id":      "ai-1",
	})
	resp := orgScopedPost(t, s.URL+"/api/tasks", string(body), sess)
	if resp.StatusCode != 201 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	if !tc.called {
		t.Fatal()
	}
}

func TestAPI_DeriveTask_NotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	// DerivationSvc nil; payload carries source fields so the post handler
	// routes into the derive branch (v2.5.x #62: empty source now means
	// create-from-scratch instead of derive-not-wired).
	deps.DerivationSvc = nil
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"source_conversation_id":"C-1","source_message_ids":["M-1"]}`
	resp, _ := http.Post(s.URL+"/api/tasks", "application/json", strings.NewReader(body))
	if resp.StatusCode != 501 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_DeriveTask_BadJSON(t *testing.T) {
	deps, _, _, sess := setupDerive(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/tasks", `x{`, sess)
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

// ============================================================================
// Input Requests
// ============================================================================

func TestAPI_ListInputRequests_Empty(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/input_requests", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_RespondInputRequest_BadJSON(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	// Bad JSON is a 400 before the org guard (body parse runs first).
	resp := orgScopedPost(t, s.URL+"/api/input_requests/x/respond", `{xx`, sess)
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

// ============================================================================
// Agents
// ============================================================================

func TestAPI_ListAgents_Empty(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	deps.AgentInstanceRepo = &fakeAgentRepo{}
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/agents", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_ShowAgent_NotFound(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	deps.AgentInstanceRepo = &fakeAgentRepo{}
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/agents/nope", sess)
	if resp.StatusCode != 404 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

// ============================================================================
// Projects (v2.1-A — powers DeriveModal project picker)
// ============================================================================

func TestAPI_ListProjects_Empty(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	deps.ProjectRepo = &fakeProjectRepo{}
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/projects", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("expected [] got %q", body)
	}
}

func TestAPI_ListProjects_SingleProject(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	p, err := workforce.NewProject(workforce.NewProjectInput{
		ID: "p-1", Name: "Demo", Tags: []string{"coding"},
		CreatedByIdentityID: "user:hayang",
		CreatedAt:           time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	deps.ProjectRepo = &fakeProjectRepo{projects: []*workforce.Project{p}}
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/projects", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var arr []map[string]any
	if err := json.Unmarshal(body, &arr); err != nil {
		t.Fatal(err)
	}
	if len(arr) != 1 {
		t.Fatalf("expected 1 project got %d", len(arr))
	}
	if arr[0]["id"] != "p-1" || arr[0]["name"] != "Demo" {
		t.Fatalf("bad row: %v", arr[0])
	}
	tagsAny, _ := arr[0]["tags"].([]any)
	if len(tagsAny) != 1 || tagsAny[0] != "coding" {
		t.Fatalf("bad tags: %v", arr[0]["tags"])
	}
}

func TestAPI_ListProjects_RepoNotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	// deps.ProjectRepo intentionally not set
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/projects")
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status=%d want 501", resp.StatusCode)
	}
}

func TestAPI_ListProjects_RepoError(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	deps.ProjectRepo = &fakeProjectRepo{findAllErr: errors.New("db boom")}
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/projects", sess)
	if resp.StatusCode != 500 {
		t.Fatalf("status=%d want 500", resp.StatusCode)
	}
}

// v2.5.5: list projection emits {id, name, description, tags, version,
// created_at, updated_at}. kind / default_agent_cli were removed.
func TestAPI_ListProjects_FullProjection(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	p, err := workforce.NewProject(workforce.NewProjectInput{
		ID: "p-2", Name: "Beta", Tags: []string{"coding", "ops"},
		Description:         "the beta project",
		CreatedByIdentityID: "user:hayang",
		CreatedAt:           time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	deps.ProjectRepo = &fakeProjectRepo{projects: []*workforce.Project{p}}
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/projects", sess)
	body, _ := io.ReadAll(resp.Body)
	var arr []map[string]any
	if err := json.Unmarshal(body, &arr); err != nil {
		t.Fatal(err)
	}
	if len(arr) != 1 {
		t.Fatalf("len=%d", len(arr))
	}
	row := arr[0]
	if row["description"] != "the beta project" {
		t.Fatalf("missing description: %v", row)
	}
	if row["updated_at"] == nil {
		t.Fatalf("missing updated_at: %v", row)
	}
	tagsAny, _ := row["tags"].([]any)
	if len(tagsAny) != 2 {
		t.Fatalf("expected 2 tags, got %v", row["tags"])
	}
}

// v2.5.5: GET /api/projects/{id} happy path emits the simplified shape.
func TestAPI_ShowProject_Happy(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	p, err := workforce.NewProject(workforce.NewProjectInput{
		ID: "p-1", Name: "Demo", Tags: []string{"coding"},
		Description:         "demo project",
		OrganizationID:      sess.OrgID,
		CreatedByIdentityID: "user:hayang",
		CreatedAt:           time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	deps.ProjectRepo = &fakeProjectRepo{projects: []*workforce.Project{p}}
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/projects/p-1", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got["id"] != "p-1" || got["name"] != "Demo" {
		t.Fatalf("bad: %v", got)
	}
	tagsAny, _ := got["tags"].([]any)
	if len(tagsAny) != 1 || tagsAny[0] != "coding" {
		t.Fatalf("bad tags: %v", got["tags"])
	}
}

// v2.3-4: GET /api/projects/{id} 404.
func TestAPI_ShowProject_NotFound(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	deps.ProjectRepo = &fakeProjectRepo{}
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/projects/ghost", sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d want 404", resp.StatusCode)
	}
}

// v2.3-4: GET /api/projects/{id} 501 when repo unwired.
func TestAPI_ShowProject_RepoNotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	// deps.ProjectRepo intentionally not set
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/projects/p-1")
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status=%d want 501", resp.StatusCode)
	}
}

// v2.3-4: GET /api/projects/{id} surfaces non-404 errors as 500.
func TestAPI_ShowProject_RepoError(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	deps.ProjectRepo = &fakeProjectRepo{findByIDErr: errors.New("db down")}
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/projects/p-1", sess)
	// repo error during org-scope check surfaces as non-200 (404/500).
	if resp.StatusCode == 200 {
		t.Fatalf("expected non-200 on repo error, got %d", resp.StatusCode)
	}
}

type fakeProjectRepo struct {
	projects    []*workforce.Project
	findAllErr  error
	findByIDErr error
}

func (f *fakeProjectRepo) FindByID(ctx context.Context, id workforce.ProjectID) (*workforce.Project, error) {
	if f.findByIDErr != nil {
		return nil, f.findByIDErr
	}
	for _, p := range f.projects {
		if p.ID() == id {
			return p, nil
		}
	}
	return nil, workforce.ErrProjectNotFound
}
func (f *fakeProjectRepo) FindAll(ctx context.Context, filter workforce.ProjectFilter) ([]*workforce.Project, error) {
	if f.findAllErr != nil {
		return nil, f.findAllErr
	}
	return f.projects, nil
}
func (f *fakeProjectRepo) Save(ctx context.Context, p *workforce.Project) error { return nil }
func (f *fakeProjectRepo) Update(ctx context.Context, id workforce.ProjectID, fields workforce.ProjectUpdateFields, version int, at time.Time) (*workforce.Project, error) {
	return nil, nil
}
func (f *fakeProjectRepo) Delete(ctx context.Context, id workforce.ProjectID) error { return nil }

type fakeAgentRepo struct{}

func (fakeAgentRepo) FindByID(ctx context.Context, id workforce.AgentInstanceID) (*workforce.AgentInstance, error) {
	return nil, workforce.ErrAgentInstanceNotFound
}
func (fakeAgentRepo) FindByName(ctx context.Context, name string) (*workforce.AgentInstance, error) {
	return nil, workforce.ErrAgentInstanceNotFound
}
func (fakeAgentRepo) FindAll(ctx context.Context, filter workforce.AgentInstanceFilter) ([]*workforce.AgentInstance, error) {
	return nil, nil
}
func (fakeAgentRepo) Save(ctx context.Context, a *workforce.AgentInstance) error {
	return nil
}
func (fakeAgentRepo) UpdateState(ctx context.Context, id workforce.AgentInstanceID, from, to workforce.AgentInstanceState, version int) error {
	return nil
}
func (fakeAgentRepo) UpdateConfig(ctx context.Context, id workforce.AgentInstanceID, config string, maxConcurrent *int, version int) error {
	return nil
}
func (fakeAgentRepo) Archive(ctx context.Context, id workforce.AgentInstanceID, at time.Time, reason workforce.AgentInstanceArchivedReason, message string, version int) error {
	return nil
}
func (fakeAgentRepo) CountActiveExecutions(ctx context.Context, id workforce.AgentInstanceID) (int, error) {
	return 0, nil
}
func (fakeAgentRepo) BulkUpdateStateByWorker(ctx context.Context, workerID workforce.WorkerID, from, to workforce.AgentInstanceState) (int, error) {
	return 0, nil
}

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
	resp, _ := http.Get(s.URL + "/api/secrets")
	if resp.StatusCode != 501 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_CreateSecret_NotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.UserSecretSvc = nil
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/secrets", "application/json",
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
	req, _ := http.NewRequest("DELETE", s.URL+"/api/secrets/x", nil)
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
	resp, _ := http.Post(s.URL+"/api/secrets", "application/json", strings.NewReader(body))
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
	resp, _ := http.Post(s.URL+"/api/secrets", "application/json",
		strings.NewReader(`{"name":"x"}`))
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_CreateSecret_BadJSON(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/secrets", "application/json", strings.NewReader(`{x`))
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
		{inputrequest.ErrInputRequestNotFound, 404},
		{conversation.ErrConversationVersionConflict, 409},
		{secretmgmt.ErrUserSecretVersionConflict, 409},
		{conversation.ErrConversationArchived, 403},
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

func TestIRPublicMap_Responded(t *testing.T) {
	now := time.Now().UTC()
	ir, err := inputrequest.New(inputrequest.NewInput{
		ID: "IR-1", TaskExecutionID: "E-1",
		Question: "q?", Options: []string{"yes", "no"},
		Urgency: inputrequest.UrgencyNormal, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = ir.Respond(inputrequest.InputResponse{
		Answer: "yes", DecidedBy: "user:hayang", DecidedAt: now,
	})
	m := irPublicMap(ir)
	if m["answer"] != "yes" {
		t.Fatalf("answer: %v", m["answer"])
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
		ID: "C-1", Kind: conversation.ConversationKindProjectChannel,
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

func TestAPI_ListInputRequests_DBError(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	db.Close()
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/input_requests", sess)
	if resp.StatusCode == 200 {
		t.Fatalf("expected non-200 on closed db, got %d", resp.StatusCode)
	}
}

func TestAPI_ListAgents_DBError(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	db.Close()
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/agents", sess)
	if resp.StatusCode == 200 {
		t.Fatalf("expected non-200, got %d", resp.StatusCode)
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

func TestAPI_TaskTrace_DBError(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	db.Close()
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/tasks/t-1/trace", sess)
	if resp.StatusCode == 200 {
		t.Fatalf("expected non-200 on closed db, got %d", resp.StatusCode)
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

func TestAPI_DeriveIssue_SourceNotFound(t *testing.T) {
	deps, _, _, sess := setupDerive(t)
	s := newTestServer(t, deps)
	defer s.Close()
	// Source conversation "nope" is not in the org → guard returns 404.
	body := `{"source_conversation_id":"nope","project_id":"p-1","title":"X"}`
	resp := orgScopedPost(t, s.URL+"/api/issues", body, sess)
	if resp.StatusCode != 500 && resp.StatusCode != 404 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_DeriveTask_SourceNotFound(t *testing.T) {
	deps, _, _, sess := setupDerive(t)
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"source_conversation_id":"nope","project_id":"p-1","title":"X","agent_instance_id":"a"}`
	resp := orgScopedPost(t, s.URL+"/api/tasks", body, sess)
	if resp.StatusCode != 500 && resp.StatusCode != 404 {
		t.Fatalf("got %d", resp.StatusCode)
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

func TestAPI_CancelInputRequest_BadJSON(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/input_requests/anything/cancel", `{not json`, sess)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d want 400", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "invalid_json" {
		t.Fatalf("error=%v want invalid_json", body["error"])
	}
}

func TestAPI_CancelInputRequest_NotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.IRSvc = nil
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/input_requests/anything/cancel",
		"application/json", strings.NewReader(`{"message":"nm"}`))
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_CancelInputRequest_NotFound(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/input_requests/nope/cancel", `{"message":"nm"}`, sess)
	if resp.StatusCode != 404 && resp.StatusCode != 500 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_RespondInputRequest_NotFound(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedPost(t, s.URL+"/api/input_requests/nope/respond", `{"answer":"yes"}`, sess)
	if resp.StatusCode != 404 && resp.StatusCode != 500 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}
