package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/admintoken"
	"github.com/oopslink/agent-center/internal/agent"
	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	convsql "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
	"github.com/oopslink/agent-center/internal/workforce"
	wfsql "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

// =============================================================================
// v2.7 D2-b2 — agent MCP write tools (post_task_message, request_input,
// block_task, complete_task). These tests stand up the REAL admin server +
// AuthMiddleware over a full pm → outbox → projector pipeline so the task's
// Conversation + the agent's WorkItem exist exactly as they do in production
// (the participant + work-item projectors create them from pm.task.assigned).
// =============================================================================

// writeToolsFixture is a richer agent-tools fixture: it wires the pm Service,
// the conversation MessageWriter, the agent WorkItem repo, and the outbox relay
// so seeding a task → assigning the agent → running the relay creates the bound
// Conversation + WorkItem, and StartTask moves the Task to running (so block /
// complete are reachable).
type writeToolsFixture struct {
	deps     HandlerDeps
	verifier *fakeVerifier

	pmSvc     *pmservice.Service
	convRepo  conversation.ConversationRepository
	msgRepo   conversation.MessageRepository
	workItems *agentsql.WorkItemRepo
	relay     *outbox.Relay
	clk       *clock.FakeClock
}

func newWriteToolsFixture(t *testing.T) *writeToolsFixture {
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
	workItems := agentsql.NewWorkItemRepo(db)

	agentSvc := agentsvc.New(agentsvc.Deps{
		DB: db, Agents: agents, WorkItems: workItems,
		Activity: agentsql.NewActivityEventRepo(db), Workers: workers,
		Outbox: outboxsql.NewOutboxRepo(db), IDGen: gen, Clock: clk,
	})

	er, err := obsqlite.NewEventRepo(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	sink := observability.NewEventSink(er, er, gen, clk)
	convRepo := convsql.NewConversationRepo(db)
	msgRepo := convsql.NewMessageRepo(db)
	writer := convservice.NewMessageWriter(db, convRepo, msgRepo, sink, gen, clk)

	pmSvc := pmservice.New(pmservice.Deps{
		DB:           db,
		Projects:     pmsql.NewProjectRepo(db),
		Members:      pmsql.NewProjectMemberRepo(db),
		Issues:       pmsql.NewIssueRepo(db),
		Tasks:        pmsql.NewTaskRepo(db),
		TaskSubs:     pmsql.NewTaskSubscriberRepo(db),
		IssueSubs:    pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db),
		Outbox:       outboxsql.NewOutboxRepo(db),
		IDGen:        gen,
		Clock:        clk,
		AgentDir:     agent.NewOrgDirectory(agents),
	})

	applied := outboxsql.NewAppliedRepo(db)
	relay := outbox.NewRelay(outboxsql.NewOutboxRepo(db), applied, clk,
		pmservice.NewParticipantProjector(db, convRepo, applied, gen, clk),
		pmservice.NewWorkItemProjector(db, workItems, applied, gen, clk))

	// Seed the worker + agent (bound to W1, in the test org).
	w, werr := workforce.NewWorker(workforce.NewWorkerInput{
		ID: workforce.WorkerID(atWorker1), Capabilities: []string{"claude-code"},
		OrganizationID: atTestOrg, EnrolledAt: atNow,
	})
	if werr != nil {
		t.Fatal(werr)
	}
	if werr := workers.Save(ctx, w); werr != nil {
		t.Fatal(werr)
	}
	// A second worker for the cross-worker guardrail test.
	w2, _ := workforce.NewWorker(workforce.NewWorkerInput{
		ID: workforce.WorkerID(atWorker2), Capabilities: []string{"claude-code"},
		OrganizationID: atTestOrg, EnrolledAt: atNow,
	})
	_ = workers.Save(ctx, w2)

	seedAgent := func(id, workerID string) {
		a, aerr := agent.NewAgent(agent.NewAgentInput{
			ID: agent.AgentID(id), OrganizationID: atTestOrg,
			Profile: agent.Profile{Name: id}, WorkerID: workerID,
			CreatedBy: "system", CreatedAt: atNow,
		})
		if aerr != nil {
			t.Fatal(aerr)
		}
		if aerr := agents.Save(ctx, a); aerr != nil {
			t.Fatal(aerr)
		}
	}
	seedAgent(atAgent1, atWorker1)
	seedAgent(atAgent2, atWorker2)

	verifier := &fakeVerifier{tokens: map[string]*admintoken.AdminToken{}}
	deps := HandlerDeps{
		DB:                db,
		Actor:             observability.Actor("system"),
		AgentSvc:          agentSvc,
		AgentWorkItemRepo: workItems,
		WorkerRepo:        workers,
		ConvRepo:          convRepo,
		MsgRepo:           msgRepo,
		MessageWriter:     writer,
		PMService:         pmSvc,
	}
	return &writeToolsFixture{
		deps: deps, verifier: verifier, pmSvc: pmSvc, convRepo: convRepo,
		msgRepo: msgRepo, workItems: workItems, relay: relay, clk: clk,
	}
}

func (f *writeToolsFixture) addWorkerToken(t *testing.T, plaintext, workerID string) {
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

func (f *writeToolsFixture) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := NewServerWithDeps("", ServerDeps{})
	h := AuthMiddleware(f.verifier)(WithDeps(f.deps)(srv.Handler()))
	httpsrv := httptest.NewServer(h)
	t.Cleanup(httpsrv.Close)
	return httpsrv
}

func (f *writeToolsFixture) drain(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		n, err := f.relay.RunOnce(ctx, 100)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
}

// seedRunningTask creates a project + task, assigns it to AG1 (granting it
// project membership + a WorkItem via the relay + creating the bound
// Conversation), then Starts it (→ running). Returns the task id. The WorkItem
// is then activated so request_input's WaitInput (active→waiting_input) is legal.
func (f *writeToolsFixture) seedRunningTask(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	owner := pm.IdentityRef("user:owner")
	pid, err := f.pmSvc.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: atTestOrg, Name: "Acme", CreatedBy: owner,
	})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{
		ProjectID: pid, Title: "do the thing", CreatedBy: owner,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.drain(t) // participant projector creates the task Conversation.
	if err := f.pmSvc.AssignTask(ctx, tid, pm.IdentityRef("agent:"+atAgent1), owner); err != nil {
		t.Fatal(err)
	}
	f.drain(t) // work-item projector creates the queued WorkItem for AG1.
	// Start the task so block/complete are reachable (running state). The agent
	// is a project member now (#5a), so it can be the actor.
	if err := f.pmSvc.StartTask(ctx, tid, pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatal(err)
	}
	return string(tid)
}

// activateWorkItem moves the agent's WorkItem for the task queued→active so
// request_input's WaitInput is a legal transition (active→waiting_input).
func (f *writeToolsFixture) activateWorkItem(t *testing.T, taskID string) {
	t.Helper()
	ctx := context.Background()
	items, err := f.workItems.ListByTask(ctx, "pm://tasks/"+taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 work item for task, got %d", len(items))
	}
	wi := items[0]
	if err := wi.Activate(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := f.workItems.Update(ctx, wi); err != nil {
		t.Fatal(err)
	}
}

func (f *writeToolsFixture) taskMessages(t *testing.T, taskID string) []*conversation.Message {
	t.Helper()
	ctx := context.Background()
	conv, err := f.convRepo.FindByOwnerRef(ctx, conversation.NewTaskOwnerRef(taskID))
	if err != nil {
		t.Fatalf("task conv not found: %v", err)
	}
	msgs, err := f.msgRepo.FindByConversationID(ctx, conv.ID(), conversation.MessageFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	return msgs
}

func (f *writeToolsFixture) taskStatus(t *testing.T, taskID string) pm.TaskStatus {
	t.Helper()
	tk, err := f.pmSvc.GetTask(context.Background(), pm.TaskID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	return tk.Status()
}

func (f *writeToolsFixture) workItemStatus(t *testing.T, taskID string) agent.WorkItemStatus {
	t.Helper()
	items, err := f.workItems.ListByTask(context.Background(), "pm://tasks/"+taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatalf("no work item for task %s", taskID)
	}
	return items[0].Status()
}

// --- post_task_message -------------------------------------------------------

func TestPostTaskMessage_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/post_task_message", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid, "content": "working on it"})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	if body["message_id"] == nil || body["message_id"] == "" {
		t.Fatalf("no message_id in body: %v", body)
	}

	msgs := f.taskMessages(t, tid)
	found := false
	for _, m := range msgs {
		if string(m.SenderIdentityID()) == "agent:"+atAgent1 && m.Content() == "working on it" {
			found = true
		}
	}
	if !found {
		t.Fatalf("agent message not posted to task conv; got %d msgs", len(msgs))
	}
}

func TestPostTaskMessage_NoWorkItem_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	// A task the agent does NOT have a WorkItem for: create one but don't assign
	// it to AG1.
	ctx := context.Background()
	owner := pm.IdentityRef("user:owner")
	pid, _ := f.pmSvc.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: atTestOrg, Name: "P", CreatedBy: owner})
	tid, _ := f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "t", CreatedBy: owner})
	f.drain(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/post_task_message", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": string(tid), "content": "hi"})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (not the agent's task); body = %v", status, body)
	}
	if body["error"] != "not_agents_task" {
		t.Fatalf("error = %v, want not_agents_task", body["error"])
	}
}

func TestPostTaskMessage_CrossWorker_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	srv := f.server(t)
	// W1 token operating AG2 (bound to W2) → guardrail 403.
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/post_task_message", "acat_w1",
		map[string]any{"agent_id": atAgent2, "task_id": tid, "content": "hi"})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %v", status, body)
	}
	if body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("error = %v, want agent_not_bound_to_worker", body["error"])
	}
}

func TestPostTaskMessage_MissingContent_400(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/post_task_message", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid})
	if status != http.StatusBadRequest || body["error"] != "missing_content" {
		t.Fatalf("status = %d err=%v, want 400 missing_content", status, body["error"])
	}
}

// --- request_input -----------------------------------------------------------

func TestRequestInput_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	f.activateWorkItem(t, tid) // WaitInput needs active→waiting_input
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/request_input", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid, "question": "which branch?"})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	if body["status"] != "waiting_input" {
		t.Fatalf("status field = %v, want waiting_input", body["status"])
	}
	// The question is posted.
	msgs := f.taskMessages(t, tid)
	found := false
	for _, m := range msgs {
		if m.Content() == "which branch?" {
			found = true
		}
	}
	if !found {
		t.Fatalf("question not posted; got %d msgs", len(msgs))
	}
	// The WorkItem is parked waiting_input.
	if got := f.workItemStatus(t, tid); got != agent.WorkItemWaitingInput {
		t.Fatalf("work item status = %s, want waiting_input", got)
	}
}

// TestRequestInput_Atomicity_RollbackOnNoLiveWorkItem is the KEY atomicity test:
// the agent OWNS a WorkItem for the task (passes scope) but it's terminal (done),
// so the WaitInput step fails. The whole tx must roll back — NO message is left
// in the task Conversation.
func TestRequestInput_Atomicity_RollbackOnNoLiveWorkItem(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	// Drive the WorkItem to a terminal state so there is no LIVE item to park,
	// but the agent still OWNS an item for the task (so the scope check passes
	// and we reach the second, failing, step).
	ctx := context.Background()
	items, _ := f.workItems.ListByTask(ctx, "pm://tasks/"+tid)
	if len(items) != 1 {
		t.Fatalf("expected 1 work item, got %d", len(items))
	}
	wi := items[0]
	if err := wi.Activate(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := wi.Done(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := f.workItems.Update(ctx, wi); err != nil {
		t.Fatal(err)
	}

	msgsBefore := len(f.taskMessages(t, tid))
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/request_input", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid, "question": "should not persist"})
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (no live work item); body = %v", status, body)
	}
	if body["error"] != "no_live_work_item" {
		t.Fatalf("error = %v, want no_live_work_item", body["error"])
	}
	// ATOMICITY: no message was written (the whole tx rolled back).
	msgsAfter := f.taskMessages(t, tid)
	if len(msgsAfter) != msgsBefore {
		t.Fatalf("ROLLBACK FAILED: message count %d → %d; the question was persisted "+
			"despite the WaitInput step failing", msgsBefore, len(msgsAfter))
	}
	for _, m := range msgsAfter {
		if m.Content() == "should not persist" {
			t.Fatalf("ROLLBACK FAILED: the question leaked into the conversation")
		}
	}
}

func TestRequestInput_MissingQuestion_400(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/request_input", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid})
	if status != http.StatusBadRequest || body["error"] != "missing_question" {
		t.Fatalf("status = %d err=%v, want 400 missing_question", status, body["error"])
	}
}

// --- block_task --------------------------------------------------------------

func TestBlockTask_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/block_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid, "reason": "waiting on creds"})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	// Reason message posted.
	found := false
	for _, m := range f.taskMessages(t, tid) {
		if m.Content() == "waiting on creds" {
			found = true
		}
	}
	if !found {
		t.Fatalf("block reason not posted to task conv")
	}
	// Task is blocked.
	if got := f.taskStatus(t, tid); got != pm.TaskBlocked {
		t.Fatalf("task status = %s, want blocked", got)
	}
}

func TestBlockTask_MissingReason_400(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/block_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid})
	if status != http.StatusBadRequest || body["error"] != "missing_reason" {
		t.Fatalf("status = %d err=%v, want 400 missing_reason", status, body["error"])
	}
	// Task untouched (still running) — the 400 short-circuits before any write.
	if got := f.taskStatus(t, tid); got != pm.TaskRunning {
		t.Fatalf("task status = %s, want running (no write on 400)", got)
	}
}

// --- complete_task -----------------------------------------------------------

func TestCompleteTask_WithSummary_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/complete_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid, "summary": "shipped it"})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	found := false
	for _, m := range f.taskMessages(t, tid) {
		if m.Content() == "shipped it" {
			found = true
		}
	}
	if !found {
		t.Fatalf("summary not posted to task conv")
	}
	if got := f.taskStatus(t, tid); got != pm.TaskCompleted {
		t.Fatalf("task status = %s, want completed", got)
	}
}

func TestCompleteTask_NoSummary_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	before := len(f.taskMessages(t, tid))
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/complete_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	// No summary → no new message.
	if after := len(f.taskMessages(t, tid)); after != before {
		t.Fatalf("message count changed %d → %d with no summary", before, after)
	}
	if got := f.taskStatus(t, tid); got != pm.TaskCompleted {
		t.Fatalf("task status = %s, want completed", got)
	}
}
