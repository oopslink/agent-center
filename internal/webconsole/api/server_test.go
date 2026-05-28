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
	disservice "github.com/oopslink/agent-center/internal/discussion/service"
	disqlite "github.com/oopslink/agent-center/internal/discussion/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/query"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/secretmgmt"
	secretservice "github.com/oopslink/agent-center/internal/secretmgmt/service"
	secretsqlite "github.com/oopslink/agent-center/internal/secretmgmt/sqlite"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
	trsqlite "github.com/oopslink/agent-center/internal/taskruntime/sqlite"
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
	// v2.5.x #61 — IssueLifecycleSvc backs the Web Console "Open Issue"
	// + "Conclude" handlers. Spawner is left nil here (covers no_action
	// + withdrawn paths); tests that need closed_with_tasks must wire it.
	convOpener := disservice.NewIssueConversationOpener(convRepo, sink, gen, clk)
	issueLifecycle := disservice.NewIssueLifecycleService(db, issueRepo, convOpener, writer, sink, gen, clk)
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
		IRSvc:              irSvc,
		UserSecretRepo:     userSecretRepo,
		UserSecretSvc:      userSecretSvc,
		AgentInstanceRepo:  aiRepo,
		ReadStateRepo:      rsRepo,
		ReadStateSvc:       rsSvc,
		IssueRepo:          issueRepo,
		TaskRepo:           taskRepo,
		IssueLifecycleSvc:  issueLifecycle,
		TaskSvc:            trservice.NewTaskService(db, taskRepo, convRepo, execRepo, msgRepo, sink, gen, clk),
	}
	return deps, db
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
	deps, _ := setupAPI(t)
	ctx := context.Background()
	// Seed a channel via service.
	res, err := deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "platform", CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, deps)
	defer s.Close()

	// GET /api/conversations?kind=channel
	resp, err := http.Get(s.URL + "/api/conversations?kind=channel")
	if err != nil {
		t.Fatal(err)
	}
	var arr []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		t.Fatal(err)
	}
	if len(arr) != 1 || arr[0]["name"] != "platform" {
		t.Fatalf("list: %v", arr)
	}

	// GET /api/conversations/{id}
	resp, err = http.Get(s.URL + "/api/conversations/" + string(res.ConversationID))
	if err != nil {
		t.Fatal(err)
	}
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
	deps, _ := setupAPI(t)
	ctx := context.Background()
	res, _ := deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "alpha", CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	s := newTestServer(t, deps)
	defer s.Close()

	body := `{"content":"hello world"}`
	resp, err := http.Post(s.URL+"/api/conversations/"+string(res.ConversationID)+"/messages",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 201 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out["message_id"] == nil {
		t.Fatalf("missing message_id: %v", out)
	}
	// List messages.
	resp, _ = http.Get(s.URL + "/api/conversations/" + string(res.ConversationID) + "/messages")
	var msgs []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&msgs)
	if len(msgs) != 1 || msgs[0]["content"] != "hello world" {
		t.Fatalf("msgs: %v", msgs)
	}
}

func TestAPI_ConversationNotFound(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/conversations/nope")
	if resp.StatusCode != 404 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_InviteParticipant(t *testing.T) {
	deps, _ := setupAPI(t)
	ctx := context.Background()
	res, _ := deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "beta", CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"identity_id":"agent:fixer","role":"member"}`
	resp, _ := http.Post(s.URL+"/api/conversations/"+string(res.ConversationID)+"/participants",
		"application/json", strings.NewReader(body))
	if resp.StatusCode != 201 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_InvitePartOnDM_Rejected(t *testing.T) {
	deps, _ := setupAPI(t)
	ctx := context.Background()
	openRes, _ := deps.MessageWriter.OpenConversation(ctx, convservice.OpenCommand{
		Kind: conversation.ConversationKindDM, CreatedBy: "user:hayang",
		Actor: observability.Actor("user:hayang"),
	})
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"identity_id":"agent:x"}`
	resp, _ := http.Post(s.URL+"/api/conversations/"+string(openRes.ConversationID)+"/participants",
		"application/json", strings.NewReader(body))
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_ArchiveConversation(t *testing.T) {
	deps, _ := setupAPI(t)
	ctx := context.Background()
	res, _ := deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "gamma", CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/conversations/"+string(res.ConversationID)+"/archive",
		"application/json", strings.NewReader(`{}`))
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
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/fleet")
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
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/tasks/t-1/trace")
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
