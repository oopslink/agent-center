package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/admintoken"
	"github.com/oopslink/agent-center/internal/agent"
	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/conversation/replyguard"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	convsql "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
	wfsql "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

// =============================================================================
// v2.7 D2-c-i — controller→center feedback admin endpoints.
//
// These tests exercise the REAL admin server + AuthMiddleware. They reuse the
// per-agent guardrail (requireAgentOnWorker): worker from the TOKEN OWNER, target
// agent must be bound to it. The lifecycle-feedback tests assert the CRITICAL
// loop-avoidance invariant: a feedback call emits NO outbox event (so the
// AgentControlProjector never re-enqueues a reconcile).
// =============================================================================

type fbFixture struct {
	deps      HandlerDeps
	verifier  *fakeVerifier
	agents    *agentsql.AgentRepo
	activity  *agentsql.ActivityEventRepo
	outbox    *outboxsql.OutboxRepo
	convs     *convsql.ConversationRepo
	msgs      *convsql.MessageRepo
	readState *convsql.ReadStateRepo
	gen       idgen.Generator
	clk       *clock.FakeClock
	ctx       context.Context
}

// newFBFixture seeds two workers (W1, W2) and builds HandlerDeps wired with the
// AgentService + the raw agent repos so tests can seed agents in arbitrary
// lifecycle states and assert persisted effects.
func newFBFixture(t *testing.T) *fbFixture {
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
	clk := clock.NewFakeClock(atNow)
	gen := idgen.NewGenerator(clk)

	workers := wfsql.NewWorkerRepo(db)
	agents := agentsql.NewAgentRepo(db)
	activity := agentsql.NewActivityEventRepo(db)
	ob := outboxsql.NewOutboxRepo(db)
	svc := agentsvc.New(agentsvc.Deps{
		DB: db, Agents: agents, Activity: activity,
		Workers: workers, Outbox: ob, IDGen: gen, Clock: clk,
	})

	// Conversation read-state plumbing for the mark-seen endpoint (D2-e-ii).
	er, err := obsqlite.NewEventRepo(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	sink := observability.NewEventSink(er, er, gen, clk)
	convs := convsql.NewConversationRepo(db)
	msgs := convsql.NewMessageRepo(db)
	readState := convsql.NewReadStateRepo(db)
	readStateSvc := convservice.NewReadStateService(db, readState, msgs, sink, clk)
	msgWriter := convservice.NewMessageWriter(db, convs, msgs, sink, gen, clk)

	for _, id := range []string{atWorker1, atWorker2} {
		w, werr := workforce.NewWorker(workforce.NewWorkerInput{
			ID: workforce.WorkerID(id), Capabilities: []string{"claude-code"},
			OrganizationID: atTestOrg, EnrolledAt: atNow,
		})
		if werr != nil {
			t.Fatal(werr)
		}
		if werr := workers.Save(ctx, w); werr != nil {
			t.Fatal(werr)
		}
	}

	verifier := &fakeVerifier{tokens: map[string]*admintoken.AdminToken{}}
	return &fbFixture{
		deps: HandlerDeps{
			DB: db, AgentSvc: svc, WorkerRepo: workers,
			AgentRepo:         agents,
			AgentActivityRepo: activity,
			ReadStateSvc:      readStateSvc,
			MessageWriter:     msgWriter,
			ConvRepo:          convs,
		},
		verifier: verifier, agents: agents, activity: activity, outbox: ob,
		convs: convs, msgs: msgs, readState: readState, gen: gen, clk: clk, ctx: ctx,
	}
}

// seedTaskConvWithMessage creates a task-owned conversation and appends one
// message, returning (convID, messageID). Used by the mark-seen endpoint tests
// (the message must be in the conversation for ReadStateService.MarkSeen).
func (f *fbFixture) seedTaskConvWithMessage(t *testing.T, convID, taskID, sender, content string) (string, string) {
	t.Helper()
	c, err := conversation.NewConversation(conversation.NewConversationInput{
		ID:             conversation.ConversationID(convID),
		Kind:           conversation.ConversationKindTask,
		OwnerRef:       conversation.NewTaskOwnerRef(taskID),
		Name:           "task " + taskID,
		OrganizationID: atTestOrg,
		CreatedBy:      conversation.IdentityRef("user:alice"),
		OpenedAt:       f.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.convs.Save(f.ctx, c); err != nil {
		t.Fatal(err)
	}
	return convID, f.appendMsg(t, convID, sender, content)
}

func (f *fbFixture) appendMsg(t *testing.T, convID, sender, content string) string {
	t.Helper()
	f.clk.Advance(1)
	id := f.gen.NewULID()
	m, err := conversation.NewMessage(conversation.NewMessageInput{
		ID:               conversation.MessageID(id),
		ConversationID:   conversation.ConversationID(convID),
		SenderIdentityID: conversation.IdentityRef(sender),
		ContentKind:      conversation.MessageContentText,
		Content:          content,
		Direction:        conversation.DirectionInbound,
		PostedAt:         f.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.msgs.Append(f.ctx, m); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *fbFixture) cursor(t *testing.T, agentID, convID string) string {
	t.Helper()
	rs, err := f.readState.FindByUserAndConv(f.ctx,
		conversation.IdentityRef("agent:"+agentID), conversation.ConversationID(convID))
	if err != nil {
		return "" // absent → never seen
	}
	return string(rs.LastSeenMessageID)
}

func (f *fbFixture) addWorkerToken(t *testing.T, plaintext, workerID string) {
	t.Helper()
	tok, err := admintoken.New(admintoken.NewAdminTokenInput{
		ID: admintoken.TokenID("T-" + plaintext), Owner: admintoken.Owner("worker:" + workerID),
		Scopes: []admintoken.Scope{"*"}, ValueHash: admintoken.HashPlaintext(plaintext),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.verifier.tokens[plaintext] = tok
}

// seedAgentLifecycle inserts an Agent bound to workerID in the given lifecycle.
func (f *fbFixture) seedAgentLifecycle(t *testing.T, id, workerID string, lc agent.AgentLifecycle) {
	t.Helper()
	a, err := agent.RehydrateAgent(agent.RehydrateAgentInput{
		ID: agent.AgentID(id), OrganizationID: atTestOrg,
		Profile: agent.Profile{Name: id}, WorkerID: workerID,
		Lifecycle: lc, CreatedBy: "system",
		CreatedAt: atNow, UpdatedAt: atNow, Version: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.agents.Save(f.ctx, a); err != nil {
		t.Fatal(err)
	}
}

func (f *fbFixture) outboxCount(t *testing.T) int {
	t.Helper()
	evts, err := f.outbox.FetchUnprocessed(f.ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	return len(evts)
}

func (f *fbFixture) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := NewServerWithDeps("", ServerDeps{})
	h := AuthMiddleware(f.verifier)(WithDeps(f.deps)(srv.Handler()))
	httpsrv := httptest.NewServer(h)
	t.Cleanup(httpsrv.Close)
	return httpsrv
}

// --- activity sink ----------------------------------------------------------

func TestEnvAgentActivity_Appends(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent1, atWorker1, agent.LifecycleRunning)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/environment/agent/activity", "acat_fb_w1", map[string]any{
		"agent_id": atAgent1, "event_type": "assistant_text",
		"payload": `{"text":"hi"}`, "task_ref": "wi-1",
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	if body["ok"] != true || body["id"] == "" || body["id"] == nil {
		t.Fatalf("want ok + id, got %v", body)
	}
	evts, err := f.activity.ListByAgent(f.ctx, agent.AgentID(atAgent1), 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(evts) != 1 || evts[0].EventType() != "assistant_text" || evts[0].TaskRef() != "wi-1" {
		t.Fatalf("activity event not appended as expected: %+v", evts)
	}
}

func TestEnvAgentActivity_CrossWorker_403(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent2, atWorker2, agent.LifecycleRunning)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/environment/agent/activity", "acat_fb_w1", map[string]any{
		"agent_id": atAgent2, "event_type": "status",
	})
	if status != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("want 403 agent_not_bound_to_worker, got %d %v", status, body)
	}
}

// --- lifecycle feedback (loop-avoidance) ------------------------------------

func TestEnvAgentLifecycleFeedback_Stopped_NoReconcileEvent(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent1, atWorker1, agent.LifecycleStopping)
	srv := f.server(t)

	before := f.outboxCount(t)
	status, body := postBearer(t, srv.URL, "/admin/environment/agent/lifecycle-feedback", "acat_fb_w1", map[string]any{
		"agent_id": atAgent1, "state": "stopped",
	})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d, want 200 ok; body = %v", status, body)
	}
	// Agent settled to stopped.
	a, _ := f.agents.FindByID(f.ctx, agent.AgentID(atAgent1))
	if a.Lifecycle() != agent.LifecycleStopped {
		t.Fatalf("agent lifecycle = %s, want stopped", a.Lifecycle())
	}
	// CRITICAL: NO outbox event emitted (no agent.lifecycle_changed → no reconcile
	// loop). The outbox count is unchanged.
	if after := f.outboxCount(t); after != before {
		evts, _ := f.outbox.FetchUnprocessed(f.ctx, 1000)
		t.Fatalf("lifecycle-feedback emitted %d new outbox events (loop risk!): %+v", after-before, evts)
	}
}

func TestEnvAgentLifecycleFeedback_Error_SetsMessage(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent1, atWorker1, agent.LifecycleRunning)
	srv := f.server(t)

	before := f.outboxCount(t)
	status, body := postBearer(t, srv.URL, "/admin/environment/agent/lifecycle-feedback", "acat_fb_w1", map[string]any{
		"agent_id": atAgent1, "state": "error", "error": "boom",
	})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d, want 200 ok; body = %v", status, body)
	}
	a, _ := f.agents.FindByID(f.ctx, agent.AgentID(atAgent1))
	if a.Lifecycle() != agent.LifecycleError || a.LifecycleError() != "boom" {
		t.Fatalf("agent = %s/%q, want error/boom", a.Lifecycle(), a.LifecycleError())
	}
	if after := f.outboxCount(t); after != before {
		t.Fatalf("error feedback emitted %d new outbox events (loop risk!)", after-before)
	}
}

// TestEnvAgentLifecycleFeedback_IllegalStopped_409: MarkStopped on a RUNNING
// agent violates the AR precondition (must be stopping/resetting) → 409.
func TestEnvAgentLifecycleFeedback_IllegalStopped_409(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent1, atWorker1, agent.LifecycleRunning)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/environment/agent/lifecycle-feedback", "acat_fb_w1", map[string]any{
		"agent_id": atAgent1, "state": "stopped",
	})
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (illegal MarkStopped on running); body = %v", status, body)
	}
}

func TestEnvAgentLifecycleFeedback_CrossWorker_403(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent2, atWorker2, agent.LifecycleStopping)
	srv := f.server(t)

	status, _ := postBearer(t, srv.URL, "/admin/environment/agent/lifecycle-feedback", "acat_fb_w1", map[string]any{
		"agent_id": atAgent2, "state": "stopped",
	})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (cross-worker)", status)
	}
}

// v2.14.0 F7 (issue I14): the work-item-state feedback tests were removed — the
// /admin/environment/agent/work-item-state route + AgentWorkItem were retired.

// --- mark-seen (D2-e-ii read-state cursor) ----------------------------------

func TestEnvAgentMarkSeen_InsertsWhenAbsent(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent1, atWorker1, agent.LifecycleRunning)
	convID, msgID := f.seedTaskConvWithMessage(t, "conv-1", "T1", "user:bob", "hello")
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/environment/agent/mark-seen", "acat_fb_w1", map[string]any{
		"agent_id": atAgent1, "conversation_id": convID, "message_id": msgID,
	})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d body = %v, want 200 ok", status, body)
	}
	if got := f.cursor(t, atAgent1, convID); got != msgID {
		t.Fatalf("cursor = %q, want %q (inserted when absent)", got, msgID)
	}
}

func TestEnvAgentMarkSeen_AdvancesWhenNewer(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent1, atWorker1, agent.LifecycleRunning)
	convID, m1 := f.seedTaskConvWithMessage(t, "conv-1", "T1", "user:bob", "first")
	m2 := f.appendMsg(t, convID, "user:bob", "second")
	srv := f.server(t)

	for _, mid := range []string{m1, m2} {
		status, _ := postBearer(t, srv.URL, "/admin/environment/agent/mark-seen", "acat_fb_w1", map[string]any{
			"agent_id": atAgent1, "conversation_id": convID, "message_id": mid,
		})
		if status != http.StatusOK {
			t.Fatalf("mark-seen %s: status %d", mid, status)
		}
	}
	if got := f.cursor(t, atAgent1, convID); got != m2 {
		t.Fatalf("cursor = %q, want %q (advanced to newer)", got, m2)
	}
}

func TestEnvAgentMarkSeen_NoOpWhenOlderOrEqual(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent1, atWorker1, agent.LifecycleRunning)
	convID, m1 := f.seedTaskConvWithMessage(t, "conv-1", "T1", "user:bob", "first")
	m2 := f.appendMsg(t, convID, "user:bob", "second")
	srv := f.server(t)

	// Advance to m2, then try to regress to m1 (older) → must stay at m2 (monotonic).
	for _, mid := range []string{m2, m1, m2} {
		status, _ := postBearer(t, srv.URL, "/admin/environment/agent/mark-seen", "acat_fb_w1", map[string]any{
			"agent_id": atAgent1, "conversation_id": convID, "message_id": mid,
		})
		if status != http.StatusOK {
			t.Fatalf("mark-seen %s: status %d", mid, status)
		}
	}
	if got := f.cursor(t, atAgent1, convID); got != m2 {
		t.Fatalf("cursor = %q, want %q (never regress to older/equal)", got, m2)
	}
}

func TestEnvAgentMarkSeen_CrossWorker_403(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	// Agent bound to W2, but the token owner is W1 → requireAgentOnWorker rejects.
	f.seedAgentLifecycle(t, atAgent2, atWorker2, agent.LifecycleRunning)
	convID, msgID := f.seedTaskConvWithMessage(t, "conv-1", "T1", "user:bob", "hi")
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/environment/agent/mark-seen", "acat_fb_w1", map[string]any{
		"agent_id": atAgent2, "conversation_id": convID, "message_id": msgID,
	})
	if status != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("want 403 agent_not_bound_to_worker, got %d %v", status, body)
	}
}

// --- converse-error (#185 follow-up / UX Rule 9) ----------------------------

// seedConvWithAgentParticipant creates a DM conversation with the agent as an
// active participant. The seeded agent has no identity-member, so its participant
// ref is the entity ref agent:<id> (matching agentActor's fallback).
func (f *fbFixture) seedConvWithAgentParticipant(t *testing.T, convID, agentID string) string {
	t.Helper()
	c, err := conversation.NewConversation(conversation.NewConversationInput{
		ID:             conversation.ConversationID(convID),
		Kind:           conversation.ConversationKindDM,
		OrganizationID: atTestOrg,
		CreatedBy:      conversation.IdentityRef("user:alice"),
		OpenedAt:       f.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	c.SetParticipants([]conversation.ParticipantElement{
		{IdentityID: "user:alice", Role: "owner", JoinedAt: "t"},
		{IdentityID: conversation.IdentityRef("agent:" + agentID), Role: "member", JoinedAt: "t"},
	}, f.clk.Now())
	if err := f.convs.Save(f.ctx, c); err != nil {
		t.Fatal(err)
	}
	return convID
}

func TestEnvAgentConverseError_PostsSystemMessage(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent1, atWorker1, agent.LifecycleRunning)
	convID := f.seedConvWithAgentParticipant(t, "conv-1", atAgent1)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/environment/agent/converse-error", "acat_fb_w1", map[string]any{
		"agent_id": atAgent1, "conversation_id": convID, "error": "error_during_execution: model x not found",
	})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d body = %v, want 200 ok", status, body)
	}
	msgs, err := f.msgs.FindByConversationID(f.ctx, conversation.ConversationID(convID), conversation.MessageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range msgs {
		if m.ContentKind() == conversation.MessageContentSystem &&
			string(m.SenderIdentityID()) == "system" &&
			strings.Contains(m.Content(), "couldn't process") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a system 'couldn't process' message in the conversation; got %d msgs", len(msgs))
	}
}

func TestEnvAgentConverseError_NotParticipant_403(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent1, atWorker1, agent.LifecycleRunning)
	// A conversation that does NOT include atAgent1 as a participant.
	convID, _ := f.seedTaskConvWithMessage(t, "conv-1", "T1", "user:bob", "hi")
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/environment/agent/converse-error", "acat_fb_w1", map[string]any{
		"agent_id": atAgent1, "conversation_id": convID, "error": "boom",
	})
	if status != http.StatusForbidden || body["error"] != "not_a_participant" {
		t.Fatalf("status = %d body = %v, want 403 not_a_participant", status, body)
	}
}

// --- reply-guardrail (T341) -------------------------------------------------

// wireReplyGuardrail attaches a real ReplyNudgeService (human-only: nil
// wake-guard) backed by the fixture's conversation repos.
func (f *fbFixture) wireReplyGuardrail() {
	obl := convservice.NewReplyObligationService(f.deps.DB, f.convs, f.readState)
	f.deps.ReplyNudgeSvc = convservice.NewReplyNudgeService(obl, nil, replyguard.DefaultConfig, f.clk)
}

// TestEnvAgentReplyNudges_ReturnsPrompts is the happy path: an agent that
// perceived a human DM but never replied gets back exactly one re-inject prompt
// naming the source conversation (方案 A).
func TestEnvAgentReplyNudges_ReturnsPrompts(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent1, atWorker1, agent.LifecycleRunning)
	f.wireReplyGuardrail()
	convID := f.seedConvWithAgentParticipant(t, "dm-1", atAgent1)
	f.appendMsg(t, convID, "user:alice", "please look at this")
	f.clk.Advance(31 * time.Second) // exceed idle_grace(30s) → perceived w/o mark_seen
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/environment/agent/reply-nudges", "acat_fb_w1", map[string]any{
		"agent_id": atAgent1,
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d body = %v, want 200", status, body)
	}
	prompts, ok := body["prompts"].([]any)
	if !ok || len(prompts) != 1 {
		t.Fatalf("want 1 prompt, got %v", body["prompts"])
	}
	if !strings.Contains(prompts[0].(string), convID) {
		t.Fatalf("prompt should name the source conversation %q, got %q", convID, prompts[0])
	}
}

// TestEnvAgentReplyNudges_NotWired_501 pins the feature-off contract: a nil
// ReplyNudgeSvc returns 501 (after the per-agent guardrail passes).
func TestEnvAgentReplyNudges_NotWired_501(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent1, atWorker1, agent.LifecycleRunning)
	// deps.ReplyNudgeSvc deliberately left nil.
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/environment/agent/reply-nudges", "acat_fb_w1", map[string]any{
		"agent_id": atAgent1,
	})
	if status != http.StatusNotImplemented || body["error"] != "reply_guardrail_not_wired" {
		t.Fatalf("want 501 reply_guardrail_not_wired, got %d %v", status, body)
	}
}

// TestEnvAgentReplyNudges_CrossWorker_403 pins the per-agent guardrail: a worker
// may only fetch reply-nudges for an agent bound to ITSELF.
func TestEnvAgentReplyNudges_CrossWorker_403(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_fb_w1", atWorker1)
	f.seedAgentLifecycle(t, atAgent2, atWorker2, agent.LifecycleRunning)
	f.wireReplyGuardrail()
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/environment/agent/reply-nudges", "acat_fb_w1", map[string]any{
		"agent_id": atAgent2,
	})
	if status != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("want 403 agent_not_bound_to_worker, got %d %v", status, body)
	}
}

// --- worker boot-resume (D2-f s4) -------------------------------------------

// addOwnerToken registers a token with an arbitrary (non-worker) owner — used by
// the resume-state non-worker-token 403 test.
func (f *fbFixture) addOwnerToken(t *testing.T, plaintext string, owner admintoken.Owner) {
	t.Helper()
	tok, err := admintoken.New(admintoken.NewAdminTokenInput{
		ID: admintoken.TokenID("T-" + plaintext), Owner: owner,
		Scopes: []admintoken.Scope{"*"}, ValueHash: admintoken.HashPlaintext(plaintext),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.verifier.tokens[plaintext] = tok
}

// resumeAgentsByID parses a resume-state response into agent_id → entry.
func resumeAgentsByID(t *testing.T, body map[string]any) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	raw, ok := body["agents"].([]any)
	if !ok {
		t.Fatalf("body has no agents array: %v", body)
	}
	for _, a := range raw {
		m := a.(map[string]any)
		out[m["agent_id"].(string)] = m
	}
	return out
}

// TestEnvWorkerResumeState_SelfOK is the happy path + the COMPUTED SET assertion.
// v2.14.0 F7 (issue I14): the resumable set is now EXACTLY the running-lifecycle
// agents (the "OR has in-flight WorkItem" condition + the per-agent in-flight WI
// list were retired with AgentWorkItem). W1 asks for its own resume-state and
// gets only its running agents; a stopped agent is excluded; each agent's
// "work_items" array is ALWAYS empty.
func TestEnvWorkerResumeState_SelfOK(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_rs_w1", atWorker1)

	// running agent → included (running lifecycle is the resumable signal).
	f.seedAgentLifecycle(t, "AG-run", atWorker1, agent.LifecycleRunning)

	// stopped agent → EXCLUDED (nothing to resume), even though pre-F7 a stopped
	// agent with an in-flight WorkItem would have been included.
	f.seedAgentLifecycle(t, "AG-stopped", atWorker1, agent.LifecycleStopped)

	// an agent on W2 must never leak into W1's resume-state.
	f.seedAgentLifecycle(t, "AG-w2", atWorker2, agent.LifecycleRunning)

	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/environment/worker/resume-state", "acat_rs_w1",
		map[string]any{"worker_id": atWorker1})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	byID := resumeAgentsByID(t, body)
	if len(byID) != 1 {
		t.Fatalf("computed set size = %d, want 1 (only the running agent); got %v", len(byID), byID)
	}
	if _, ok := byID["AG-stopped"]; ok {
		t.Fatalf("stopped agent leaked into resume set: %v", byID)
	}
	if _, ok := byID["AG-w2"]; ok {
		t.Fatalf("cross-worker agent leaked into resume set: %v", byID)
	}

	// AG-run: desired running; the tasks array is ALWAYS empty (F7).
	run := byID["AG-run"]
	if run["desired_lifecycle"] != "running" {
		t.Fatalf("AG-run desired = %v, want running", run["desired_lifecycle"])
	}
	runWIs, ok := run["tasks"].([]any)
	if !ok || len(runWIs) != 0 {
		t.Fatalf("AG-run tasks = %v, want an empty array (F7: AgentWorkItem retired)", run["tasks"])
	}
}

// TestEnvWorkerResumeState_ErrorAgentRecoverable is the auto-recovery fix for issue
// I13 ([BUG] agent 挂了,不能自动恢复). An agent the daemon reported as CRASHED
// (lifecycle=error) is NOT desired-stopped — the operator never asked to stop it, so
// its intent is still running; `error` is the TRANSIENT crash state (vs the TERMINAL
// `failed` circuit-breaker, which is manual-recovery only). It MUST appear in resume
// -state as desired_lifecycle=running so a daemon (re)boot or a new-work wake re-
// attaches the surviving supervisor / relaunches a dead one. If it were omitted (the
// pre-fix behaviour), boot-reconcile would see a LIVE supervisor with no center record
// and stop+reap it as an orphan — killing the surviving claude and never relaunching:
// the "挂了不能自动恢复" trap. `failed` + stopped/stopping/resetting stay EXCLUDED.
func TestEnvWorkerResumeState_ErrorAgentRecoverable(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_rs_w1", atWorker1)

	f.seedAgentLifecycle(t, "AG-run", atWorker1, agent.LifecycleRunning)
	f.seedAgentLifecycle(t, "AG-error", atWorker1, agent.LifecycleError)
	f.seedAgentLifecycle(t, "AG-failed", atWorker1, agent.LifecycleFailed)
	f.seedAgentLifecycle(t, "AG-stopping", atWorker1, agent.LifecycleStopping)

	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/environment/worker/resume-state", "acat_rs_w1",
		map[string]any{"worker_id": atWorker1})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	byID := resumeAgentsByID(t, body)

	// the crashed (error) agent is included and reported as desired-running so boot
	// -reconcile reattaches the survivor / relaunches a dead one (auto-recovery).
	errAgent, ok := byID["AG-error"]
	if !ok {
		t.Fatalf("error-lifecycle agent missing from resume set (auto-recovery trap): %v", byID)
	}
	if errAgent["desired_lifecycle"] != "running" {
		t.Fatalf("AG-error desired = %v, want running (error → recover as running)", errAgent["desired_lifecycle"])
	}
	// the normal running agent is still desired-running.
	if run := byID["AG-run"]; run == nil || run["desired_lifecycle"] != "running" {
		t.Fatalf("AG-run desired = %v, want running", byID["AG-run"])
	}
	// terminal / settling states stay excluded.
	if _, ok := byID["AG-failed"]; ok {
		t.Fatalf("terminal failed agent leaked into resume set (must be manual-only): %v", byID)
	}
	if _, ok := byID["AG-stopping"]; ok {
		t.Fatalf("stopping agent leaked into resume set: %v", byID)
	}
}

// TestEnvWorkerResumeState_BodyMismatch_403 is the 🔴 AUTHZ only-ask-self check:
// a W1 token asking about W2 in the body → 403 worker_mismatch (no cross-worker
// leak), even though the token is a valid worker token.
func TestEnvWorkerResumeState_BodyMismatch_403(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_rs_w1", atWorker1)
	f.seedAgentLifecycle(t, "AG-w2", atWorker2, agent.LifecycleRunning)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/environment/worker/resume-state", "acat_rs_w1",
		map[string]any{"worker_id": atWorker2})
	if status != http.StatusForbidden || body["error"] != "worker_mismatch" {
		t.Fatalf("status = %d error = %v, want 403 worker_mismatch", status, body["error"])
	}
}

// TestEnvWorkerResumeState_NonWorkerToken_403: a non-worker owner is rejected —
// the worker is taken from the token owner, not the body.
func TestEnvWorkerResumeState_NonWorkerToken_403(t *testing.T) {
	f := newFBFixture(t)
	f.addOwnerToken(t, "acat_rs_cli", admintoken.Owner("cli:admin"))
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/environment/worker/resume-state", "acat_rs_cli",
		map[string]any{"worker_id": atWorker1})
	if status != http.StatusForbidden || body["error"] != "not_a_worker_token" {
		t.Fatalf("status = %d error = %v, want 403 not_a_worker_token", status, body["error"])
	}
}

// TestEnvWorkerResumeState_EmptyOK: a worker with no resumable agents returns an
// empty (non-nil) agents array, not an error.
func TestEnvWorkerResumeState_EmptyOK(t *testing.T) {
	f := newFBFixture(t)
	f.addWorkerToken(t, "acat_rs_w1", atWorker1)
	f.seedAgentLifecycle(t, "AG-stopped", atWorker1, agent.LifecycleStopped)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/environment/worker/resume-state", "acat_rs_w1",
		map[string]any{"worker_id": atWorker1})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	agents, ok := body["agents"].([]any)
	if !ok || len(agents) != 0 {
		t.Fatalf("want empty agents array, got %v", body["agents"])
	}
}
