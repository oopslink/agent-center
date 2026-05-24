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
	"github.com/oopslink/agent-center/internal/conversation/identity"
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
	deps, _ := setupAPI(t)
	ctx := context.Background()
	_, _ = deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "filterable", CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	s := newTestServer(t, deps)
	defer s.Close()
	// Both kind + status filters set.
	resp, _ := http.Get(s.URL + "/api/conversations?kind=channel&status=active")
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var arr []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&arr)
	if len(arr) != 1 {
		t.Fatalf("got %d", len(arr))
	}
}

func TestAPI_SendMessage_DefaultsContentKindAndDirection(t *testing.T) {
	deps, _ := setupAPI(t)
	ctx := context.Background()
	res, _ := deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "smdefaults", CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	s := newTestServer(t, deps)
	defer s.Close()
	// Body has only content; handler defaults.
	resp, _ := http.Post(s.URL+"/api/conversations/"+string(res.ConversationID)+"/messages",
		"application/json", strings.NewReader(`{"content":"x"}`))
	if resp.StatusCode != 201 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_SendMessage_BadJSON(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/conversations/foo/messages",
		"application/json", strings.NewReader(`{not json`))
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_ArchiveConversation_NotFound(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/conversations/nope/archive",
		"application/json", strings.NewReader(`{"version":1}`))
	if resp.StatusCode != 404 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_RemoveParticipant_Happy(t *testing.T) {
	deps, _ := setupAPI(t)
	ctx := context.Background()
	res, _ := deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "remove-test", CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	_, _ = deps.ParticipantMgmtSvc.Invite(ctx, convservice.InviteCommand{
		ConversationName: "remove-test", IdentityID: "user:bob",
		InvitedBy: "user:hayang", Actor: observability.Actor("user:hayang"),
	})
	s := newTestServer(t, deps)
	defer s.Close()
	req, _ := http.NewRequest("DELETE",
		s.URL+"/api/conversations/"+string(res.ConversationID)+"/participants/user:bob", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_RemoveParticipant_OnDM_Rejected(t *testing.T) {
	deps, _ := setupAPI(t)
	ctx := context.Background()
	openRes, _ := deps.MessageWriter.OpenConversation(ctx, convservice.OpenCommand{
		Kind: conversation.ConversationKindDM, CreatedBy: "user:hayang",
		Actor: observability.Actor("user:hayang"),
	})
	s := newTestServer(t, deps)
	defer s.Close()
	req, _ := http.NewRequest("DELETE",
		s.URL+"/api/conversations/"+string(openRes.ConversationID)+"/participants/user:bob", nil)
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_InviteParticipant_NotFound(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/conversations/nope/participants",
		"application/json", strings.NewReader(`{"identity_id":"user:bob"}`))
	if resp.StatusCode != 404 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_InviteParticipant_MissingIdentityID(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/conversations/c-1/participants",
		"application/json", strings.NewReader(`{"role":"member"}`))
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_InviteParticipant_BadJSON(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/conversations/c-1/participants",
		"application/json", strings.NewReader(`{nope`))
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

func setupDerive(t *testing.T) (HandlerDeps, *fakeIssueOpener, *fakeTaskCreator) {
	deps, db := setupAPI(t)
	// Seed a source channel + 1 message so derive validates.
	ctx := context.Background()
	res, _ := deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "src", CreatedBy: "user:hayang", Actor: "user:hayang",
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
	// Build a derivation service using the existing wiring.
	refRepo := convsqlite.NewReferenceRepo(db)
	carryOver := convservice.NewCarryOverService(db, deps.ConvRepo, deps.MsgRepo, refRepo,
		nil, nil, nil)
	_ = carryOver // unused if we don't materialise refs in tests
	deps.DerivationSvc = convservice.NewMessageDerivationService(db,
		deps.ConvRepo, deps.MsgRepo, deps.CarryOverSvc, io, tc, nil, nil)
	// stash msg id on the opener for later assertions via test context.
	_ = mw
	_ = res
	return deps, io, tc
}

func TestAPI_DeriveIssue_Happy(t *testing.T) {
	deps, io, _ := setupDerive(t)
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
	resp, _ := http.Post(s.URL+"/api/issues", "application/json", strings.NewReader(string(body)))
	if resp.StatusCode != 201 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	if !io.called {
		t.Fatal("opener not invoked")
	}
}

func TestAPI_DeriveIssue_DerivationNotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	// DerivationSvc nil
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/issues", "application/json", strings.NewReader(`{}`))
	if resp.StatusCode != 501 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_DeriveIssue_BadJSON(t *testing.T) {
	deps, _, _ := setupDerive(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/issues", "application/json", strings.NewReader(`{not`))
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_DeriveTask_Happy(t *testing.T) {
	deps, _, tc := setupDerive(t)
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
	resp, _ := http.Post(s.URL+"/api/tasks", "application/json", strings.NewReader(string(body)))
	if resp.StatusCode != 201 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	if !tc.called {
		t.Fatal()
	}
}

func TestAPI_DeriveTask_NotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/tasks", "application/json", strings.NewReader(`{}`))
	if resp.StatusCode != 501 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_DeriveTask_BadJSON(t *testing.T) {
	deps, _, _ := setupDerive(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/tasks", "application/json", strings.NewReader(`x{`))
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

// ============================================================================
// Input Requests
// ============================================================================

func TestAPI_ListInputRequests_Empty(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/input_requests")
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_RespondInputRequest_BadJSON(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/input_requests/x/respond",
		"application/json", strings.NewReader(`{xx`))
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

// ============================================================================
// Agents
// ============================================================================

func TestAPI_ListAgents_Empty(t *testing.T) {
	deps, _ := setupAPI(t)
	// Wire the AgentInstance repo so the handler works.
	// setupAPI doesn't wire it; install one over the existing DB.
	// We'll just install a fakeAgentRepo.
	deps.AgentInstanceRepo = &fakeAgentRepo{}
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/agents")
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_ShowAgent_NotFound(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.AgentInstanceRepo = &fakeAgentRepo{}
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/agents/nope")
	if resp.StatusCode != 404 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

// ============================================================================
// Projects (v2.1-A — powers DeriveModal project picker)
// ============================================================================

func TestAPI_ListProjects_Empty(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.ProjectRepo = &fakeProjectRepo{}
	s := newTestServer(t, deps)
	defer s.Close()
	resp, err := http.Get(s.URL + "/api/projects")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("expected [] got %q", body)
	}
}

func TestAPI_ListProjects_SingleProject(t *testing.T) {
	deps, _ := setupAPI(t)
	p, err := workforce.NewProject(workforce.NewProjectInput{
		ID: "p-1", Name: "Demo", Kind: workforce.ProjectKindCoding,
		CreatedByIdentityID: "user:hayang",
		CreatedAt:           time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	deps.ProjectRepo = &fakeProjectRepo{projects: []*workforce.Project{p}}
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/projects")
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
	if arr[0]["id"] != "p-1" || arr[0]["name"] != "Demo" || arr[0]["kind"] != "coding" {
		t.Fatalf("bad row: %v", arr[0])
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
	deps, _ := setupAPI(t)
	deps.ProjectRepo = &fakeProjectRepo{findAllErr: errors.New("db boom")}
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/projects")
	if resp.StatusCode != 500 {
		t.Fatalf("status=%d want 500", resp.StatusCode)
	}
}

type fakeProjectRepo struct {
	projects   []*workforce.Project
	findAllErr error
}

func (f *fakeProjectRepo) FindByID(ctx context.Context, id workforce.ProjectID) (*workforce.Project, error) {
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
	deps, _ := setupAPI(t)
	_, err := deps.UserSecretSvc.Create(context.Background(), secretsvcCreate.CreateSecretCommand{
		Name: "k", Kind: secretmgmt.UserSecretKindOther,
		Plaintext: []byte("v"), ActorIdentity: observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/secrets")
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
	deps, _ := setupAPI(t)
	res, err := deps.UserSecretSvc.Create(context.Background(), secretsvcCreate.CreateSecretCommand{
		Name: "k", Kind: secretmgmt.UserSecretKindOther,
		Plaintext: []byte("v"), ActorIdentity: observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, deps)
	defer s.Close()
	req, _ := http.NewRequest("DELETE", s.URL+"/api/secrets/"+string(res.ID), nil)
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_RevokeSecret_NotFound(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	req, _ := http.NewRequest("DELETE", s.URL+"/api/secrets/nope", nil)
	resp, _ := http.DefaultClient.Do(req)
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
		{identity.ErrIdentityNotFound, 404},
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
	deps, db := setupAPI(t)
	db.Close() // force every query to fail
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/conversations")
	if resp.StatusCode != 500 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_ListMessages_DBError(t *testing.T) {
	deps, db := setupAPI(t)
	db.Close()
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/conversations/x/messages")
	if resp.StatusCode != 500 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_ShowConversation_DBError(t *testing.T) {
	deps, db := setupAPI(t)
	db.Close()
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/conversations/x")
	// closed-DB returns 500 (DB-level error, not ErrConversationNotFound).
	if resp.StatusCode != 500 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_ListInputRequests_DBError(t *testing.T) {
	deps, db := setupAPI(t)
	db.Close()
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/input_requests")
	if resp.StatusCode != 500 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_ListAgents_DBError(t *testing.T) {
	deps, db := setupAPI(t)
	db.Close()
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/agents")
	if resp.StatusCode != 500 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_ListSecrets_DBError(t *testing.T) {
	deps, db := setupAPI(t)
	db.Close()
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/secrets")
	if resp.StatusCode != 500 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_TaskTrace_DBError(t *testing.T) {
	deps, db := setupAPI(t)
	db.Close()
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/tasks/t-1/trace")
	if resp.StatusCode != 500 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_RemoveParticipant_NotFound(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	req, _ := http.NewRequest("DELETE", s.URL+"/api/conversations/nope/participants/user:x", nil)
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 404 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_Archive_DBError(t *testing.T) {
	deps, db := setupAPI(t)
	db.Close()
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/conversations/x/archive",
		"application/json", strings.NewReader(`{"version":1}`))
	// closed-DB path through FindByID returns 500.
	if resp.StatusCode != 500 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_DeriveIssue_SourceNotFound(t *testing.T) {
	deps, _, _ := setupDerive(t)
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"source_conversation_id":"nope","project_id":"p","title":"X"}`
	resp, _ := http.Post(s.URL+"/api/issues", "application/json", strings.NewReader(body))
	// validation chain rejects with 500 (mapDomainError falls through).
	if resp.StatusCode != 500 && resp.StatusCode != 404 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_DeriveTask_SourceNotFound(t *testing.T) {
	deps, _, _ := setupDerive(t)
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"source_conversation_id":"nope","project_id":"p","title":"X","agent_instance_id":"a"}`
	resp, _ := http.Post(s.URL+"/api/tasks", "application/json", strings.NewReader(body))
	if resp.StatusCode != 500 && resp.StatusCode != 404 {
		t.Fatalf("got %d", resp.StatusCode)
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
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/input_requests/nope/cancel",
		"application/json", strings.NewReader(`{"message":"nm"}`))
	if resp.StatusCode != 404 && resp.StatusCode != 500 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_RespondInputRequest_NotFound(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/input_requests/nope/respond",
		"application/json", strings.NewReader(`{"answer":"yes"}`))
	if resp.StatusCode != 404 && resp.StatusCode != 500 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}
