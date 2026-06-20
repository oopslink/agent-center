package api

import (
	"context"
	"database/sql"
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
// v2.7 D2-b2 — agent MCP write tools (post_message, request_input,
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

	db         *sql.DB
	pmSvc      *pmservice.Service
	convRepo   conversation.ConversationRepository
	msgRepo    conversation.MessageRepository
	workItems  *agentsql.WorkItemRepo
	outboxRepo *outboxsql.OutboxRepo
	relay      *outbox.Relay
	clk        *clock.FakeClock
}

// atAllAgentsDir maps every agent to the fixture's test org so an agent assignee
// resolves (exists + same org) in StartPlan's §9.6c assignee validation. Mirrors
// the webconsole plan test's allAgentsDir (returning the project's org so the
// cross-org guard passes).
type atAllAgentsDir struct{}

func (atAllAgentsDir) OrgOfAgent(_ context.Context, _ string) (string, error) {
	return atTestOrg, nil
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

	// v2.9 P3 Stage C: wire the Plan repo + a REAL PlanDispatcher (over the
	// MessageWriter) so the Plan passthrough tools' AppServices are AVAILABLE in
	// the fixture (mirrors the webconsole plan test). allAgentsDir resolves every
	// agent assignee into the test org so StartPlan's §9.6c assignee check passes.
	plans := pmsql.NewPlanRepo(db)
	pmSvc := pmservice.New(pmservice.Deps{
		DB:           db,
		Projects:     pmsql.NewProjectRepo(db),
		Members:      pmsql.NewProjectMemberRepo(db),
		Issues:       pmsql.NewIssueRepo(db),
		Tasks:        pmsql.NewTaskRepo(db),
		TaskSubs:     pmsql.NewTaskSubscriberRepo(db),
		IssueSubs:    pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db),
		Plans:        plans,
		Findings:     pmsql.NewPlanFindingRepo(db), // v2.10 ADR-0053: shared findings
		Outbox:       outboxsql.NewOutboxRepo(db),
		IDGen:        gen,
		Clock:        clk,
		AgentDir:     atAllAgentsDir{},
		PlanDispatcher: convservice.NewPlanDispatchAdapter(writer, func(_ context.Context, ref string) (string, bool) {
			if i := strings.IndexByte(ref, ':'); i >= 0 {
				ref = ref[i+1:]
			}
			if strings.TrimSpace(ref) == "" {
				return "", false
			}
			return ref, true
		}),
	})

	outboxRepo := outboxsql.NewOutboxRepo(db)
	applied := outboxsql.NewAppliedRepo(db)
	relay := outbox.NewRelay(outboxsql.NewOutboxRepo(db), applied, clk,
		pmservice.NewParticipantProjector(db, convRepo, applied, gen, clk),
		pmservice.NewPlanParticipantProjector(db, convRepo, plans, applied, gen, clk),
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
		OutboxRepo:        outboxRepo,
	}
	return &writeToolsFixture{
		deps: deps, verifier: verifier, db: db, pmSvc: pmSvc, convRepo: convRepo,
		msgRepo: msgRepo, workItems: workItems, outboxRepo: outboxRepo, relay: relay, clk: clk,
	}
}

// awaitingInputEvents returns the agent.awaiting_input outbox events currently in
// the table (queried directly so the assertion is independent of the test relay,
// which has no batch projector). Used to assert request_input's same-tx emit.
func (f *writeToolsFixture) awaitingInputEvents(t *testing.T) []outbox.Event {
	t.Helper()
	rows, err := f.db.QueryContext(context.Background(),
		`SELECT id, event_type, payload FROM outbox_events WHERE event_type = ? ORDER BY id ASC`,
		"agent.awaiting_input")
	if err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	defer rows.Close()
	var out []outbox.Event
	for rows.Next() {
		var e outbox.Event
		if err := rows.Scan(&e.ID, &e.EventType, &e.Payload); err != nil {
			t.Fatalf("scan outbox: %v", err)
		}
		out = append(out, e)
	}
	return out
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

// --- post_message (target type "task") — T200 WS4 ---------------------------

func TestPostTaskMessage_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/post_message", "acat_w1",
		map[string]any{"agent_id": atAgent1, "target": map[string]any{"type": "task", "id": tid}, "content": "working on it"})
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

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/post_message", "acat_w1",
		map[string]any{"agent_id": atAgent1, "target": map[string]any{"type": "task", "id": string(tid)}, "content": "hi"})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (not the agent's task); body = %v", status, body)
	}
	if body["error"] != "not_agents_task" {
		t.Fatalf("error = %v, want not_agents_task", body["error"])
	}
}

// T183: an agent that is a MEMBER of the task's project — but holds NO WorkItem
// (e.g. it create_task'd the task) — can post to the task conversation WITHOUT
// first self-assigning. Previously requireOwnTask 403'd not_agents_task; the
// relaxed requireTaskAccess gate admits the project member.
func TestPostTaskMessage_ProjectMemberNoWorkItem_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	ctx := context.Background()
	owner := pm.IdentityRef("user:owner")
	pid, err := f.pmSvc.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: atTestOrg, Name: "P", CreatedBy: owner})
	if err != nil {
		t.Fatal(err)
	}
	// AG1 is a project member but the task is NOT assigned to it → no WorkItem.
	if _, err := f.pmSvc.AddProjectMember(ctx, pmservice.AddProjectMemberCommand{
		ProjectID: pid, IdentityID: pm.IdentityRef("agent:" + atAgent1), Actor: owner,
	}); err != nil {
		t.Fatal(err)
	}
	tid, err := f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "t", CreatedBy: owner})
	if err != nil {
		t.Fatal(err)
	}
	f.drain(t) // participant projector creates the task Conversation.
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/post_message", "acat_w1",
		map[string]any{"agent_id": atAgent1, "target": map[string]any{"type": "task", "id": string(tid)}, "content": "from a member"})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (project member may post); body = %v", status, body)
	}
	found := false
	for _, m := range f.taskMessages(t, string(tid)) {
		if string(m.SenderIdentityID()) == "agent:"+atAgent1 && m.Content() == "from a member" {
			found = true
		}
	}
	if !found {
		t.Fatalf("member's message not posted to task conv")
	}
}

// v2.7 #185 FINDING-L: an agent's reply via post_message is authorized by
// agentIsActiveParticipant. Conversation participants carry the agent's MEMBER
// ref (agent:<member-id>), but the execution-entity id differs — agentActor must
// yield the member ref so authz matches AND the message sender is the business id
// (not the entity ULID). The prior entity-id agentActor failed authz (the reply
// turn errored) and would have leaked the entity ULID as the sender.
func TestAgentActor_PostMessageAuthz_UseMemberID(t *testing.T) {
	a, err := agent.NewAgent(agent.NewAgentInput{
		ID: "01ENTITYULID", OrganizationID: atTestOrg,
		Profile: agent.Profile{Name: "Helper"}, WorkerID: "W1",
		CreatedBy: "system", CreatedAt: atNow, IdentityMemberID: "agent-mem1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := agentActor(a); got != "agent:agent-mem1" {
		t.Fatalf("agentActor = %q, want agent:agent-mem1 (member ref, not entity id)", got)
	}

	conv, err := conversation.NewConversation(conversation.NewConversationInput{
		ID: "c1", Kind: conversation.ConversationKindChannel, Name: "general",
		OrganizationID: atTestOrg, CreatedBy: "user:alice", OpenedAt: atNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Participant ref is the MEMBER id (the business ref the assign/picker stores).
	conv.SetParticipants([]conversation.ParticipantElement{
		{IdentityID: "user:alice", Role: "owner", JoinedAt: "t"},
		{IdentityID: "agent:agent-mem1", Role: "member", JoinedAt: "t"},
	}, atNow)
	if !agentIsActiveParticipant(conv, a) {
		t.Fatal("agent with member-ref participant must pass post_message authz (FINDING-L)")
	}

	other, _ := agent.NewAgent(agent.NewAgentInput{
		ID: "01OTHER", OrganizationID: atTestOrg, Profile: agent.Profile{Name: "Other"},
		WorkerID: "W1", CreatedBy: "system", CreatedAt: atNow, IdentityMemberID: "agent-mem2",
	})
	if agentIsActiveParticipant(conv, other) {
		t.Fatal("non-participant agent must be rejected")
	}
}

func TestPostTaskMessage_CrossWorker_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	srv := f.server(t)
	// W1 token operating AG2 (bound to W2) → guardrail 403.
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/post_message", "acat_w1",
		map[string]any{"agent_id": atAgent2, "target": map[string]any{"type": "task", "id": tid}, "content": "hi"})
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
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/post_message", "acat_w1",
		map[string]any{"agent_id": atAgent1, "target": map[string]any{"type": "task", "id": tid}})
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

	// D2-e-ii: request_input emitted agent.awaiting_input in the SAME tx as the
	// message + WaitInput (the batch-flush trigger). Payload carries the task conv.
	evs := f.awaitingInputEvents(t)
	if len(evs) != 1 {
		t.Fatalf("want 1 agent.awaiting_input event, got %d", len(evs))
	}
	conv, err := f.convRepo.FindByOwnerRef(context.Background(), conversation.NewTaskOwnerRef(tid))
	if err != nil {
		t.Fatal(err)
	}
	pay := evs[0].Payload
	for _, want := range []string{
		`"agent_id":"` + atAgent1 + `"`,
		`"task_ref":"pm://tasks/` + tid + `"`,
		`"conversation_id":"` + string(conv.ID()) + `"`,
		`"work_item_id":"`,
	} {
		if !contains(pay, want) {
			t.Fatalf("awaiting_input payload missing %q: %s", want, pay)
		}
	}
}

// TestRequestInput_AwaitingInput_AtomicWithRollback proves the agent.awaiting_input
// emit shares the request_input tx: when WaitInput fails (terminal WorkItem) the
// whole tx rolls back, so NEITHER the message NOR the awaiting_input event persist.
func TestRequestInput_AwaitingInput_AtomicWithRollback(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	ctx := context.Background()
	items, _ := f.workItems.ListByTask(ctx, "pm://tasks/"+tid)
	wi := items[0]
	_ = wi.Activate(time.Now())
	_ = wi.Done(time.Now())
	if err := f.workItems.Update(ctx, wi); err != nil {
		t.Fatal(err)
	}
	srv := f.server(t)
	status, _ := postBearer(t, srv.URL, "/admin/agent-tools/request_input", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid, "question": "nope"})
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want 409", status)
	}
	if evs := f.awaitingInputEvents(t); len(evs) != 0 {
		t.Fatalf("ROLLBACK FAILED: awaiting_input event leaked (%d) despite WaitInput failing", len(evs))
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
	// ADR-0046: block sets a reason annotation; status stays running (never a state).
	if got := f.taskStatus(t, tid); got != pm.TaskRunning {
		t.Fatalf("task status = %s, want running (blocked is an annotation)", got)
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

// TestCompleteTask_PoolClaimed_NoWorkItem is the regression for the pool-claim
// completion bug: a task acquired via claim_task is ASSIGNED to the agent and
// running but has NO WorkItem (ClaimPoolTask mints none). Before the fix,
// complete_task's requireOwnTask gate (work-item-only) 403'd not_agents_task, so a
// claimed pool task could never be completed. The own-task gate now also accepts
// the task's assignee, so the claimer can complete it.
func TestCompleteTask_PoolClaimed_NoWorkItem(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	ctx := context.Background()
	pid, _ := f.seedMemberProject(t) // AG1 is now a member of pid.

	// Find the project's built-in claimable pool.
	plans, err := f.pmSvc.ListPlans(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	var pool pm.PlanID
	for _, p := range plans {
		if p.IsBuiltin() {
			pool = p.ID()
		}
	}
	if pool == "" {
		t.Fatal("no built-in pool found for project")
	}

	// Create a SEPARATE, UNASSIGNED task and place it in the running pool so it is
	// claimable (the claim — not a prior assign — is what makes AG1 the assignee).
	owner := pm.IdentityRef("user:owner")
	tid, err := f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{
		ProjectID: pid, Title: "pool task", CreatedBy: owner,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.drain(t) // participant projector creates the task Conversation.
	if err := f.pmSvc.SelectTaskIntoPlan(ctx, pool, tid, owner); err != nil {
		t.Fatalf("SelectTaskIntoPlan into pool: %v", err)
	}
	f.drain(t)
	if err := f.pmSvc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatalf("ReconcileRunningPlans: %v", err)
	}

	srv := f.server(t)
	// AG1 claims the pool task → assignee=AG1, running, NO WorkItem.
	if status, body := postBearer(t, srv.URL, "/admin/agent-tools/claim_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": string(tid)}); status != http.StatusOK {
		t.Fatalf("claim_task status = %d, want 200; body = %v", status, body)
	}
	// Precondition for the regression: the claimed pool task really has no WorkItem.
	if items, _ := f.workItems.ListByTask(ctx, "pm://tasks/"+string(tid)); len(items) != 0 {
		t.Fatalf("expected NO work item for a pool-claimed task, got %d", len(items))
	}

	// complete_task must now succeed (was 403 not_agents_task before the fix).
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/complete_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": string(tid)})
	if status != http.StatusOK {
		t.Fatalf("complete_task on pool-claimed task: status = %d, want 200; body = %v", status, body)
	}
	if got := f.taskStatus(t, string(tid)); got != pm.TaskCompleted {
		t.Fatalf("task status = %s, want completed", got)
	}
}

// --- discard_task (T119) -----------------------------------------------------

func TestDiscardTask_RunningToDiscarded_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/discard_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid, "reason": "superseded by T120"})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	if got := f.taskStatus(t, tid); got != pm.TaskDiscarded {
		t.Fatalf("task status = %s, want discarded", got)
	}
	// Optional reason is posted to the task conversation.
	found := false
	for _, m := range f.taskMessages(t, tid) {
		if m.Content() == "superseded by T120" {
			found = true
		}
	}
	if !found {
		t.Fatalf("discard reason not posted to task conv")
	}
}

func TestDiscardTask_NoReason_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	before := len(f.taskMessages(t, tid))
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/discard_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	// No reason → no new message.
	if after := len(f.taskMessages(t, tid)); after != before {
		t.Fatalf("message count changed %d → %d with no reason", before, after)
	}
	if got := f.taskStatus(t, tid); got != pm.TaskDiscarded {
		t.Fatalf("task status = %s, want discarded", got)
	}
}

// A terminal task (completed/discarded) cannot be discarded — illegal transition.
func TestDiscardTask_TerminalRejected(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	srv := f.server(t)

	// Complete it first (terminal).
	if status, body := postBearer(t, srv.URL, "/admin/agent-tools/complete_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid}); status != http.StatusOK {
		t.Fatalf("precondition complete status = %d body=%v", status, body)
	}
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/discard_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body = %v", status, body)
	}
	if body["error"] != "invalid_transition" {
		t.Fatalf("error = %v, want invalid_transition", body["error"])
	}
	if got := f.taskStatus(t, tid); got != pm.TaskCompleted {
		t.Fatalf("task status = %s, want completed (no write on reject)", got)
	}
}

// Domain guardrail: a worker token operating an agent bound to ANOTHER worker → 403.
func TestDiscardTask_CrossWorker_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/discard_task", "acat_w1",
		map[string]any{"agent_id": atAgent2, "task_id": tid, "reason": "x"})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %v", status, body)
	}
	if body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("error = %v, want agent_not_bound_to_worker", body["error"])
	}
	if got := f.taskStatus(t, tid); got != pm.TaskRunning {
		t.Fatalf("task status = %s, want running (no write on 403)", got)
	}
}

// T183: a project member with NO WorkItem can discard a task it can see (the
// create-then-discard flow). Previously requireOwnTask blocked it before
// pm.DiscardTask's own requireProjectMember check could admit the member.
func TestDiscardTask_ProjectMemberNoWorkItem_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	ctx := context.Background()
	owner := pm.IdentityRef("user:owner")
	pid, err := f.pmSvc.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: atTestOrg, Name: "P", CreatedBy: owner})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pmSvc.AddProjectMember(ctx, pmservice.AddProjectMemberCommand{
		ProjectID: pid, IdentityID: pm.IdentityRef("agent:" + atAgent1), Actor: owner,
	}); err != nil {
		t.Fatal(err)
	}
	tid, err := f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "t", CreatedBy: owner})
	if err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/discard_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": string(tid), "reason": "mis-created"})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (project member may discard); body = %v", status, body)
	}
	if got := f.taskStatus(t, string(tid)); got != pm.TaskDiscarded {
		t.Fatalf("task status = %s, want discarded", got)
	}
}

// T183: a NON-member with no WorkItem is still fail-closed on discard_task
// (mirrors the task-post 403) — the relaxed gate does not leak access.
func TestDiscardTask_NonMemberNoWorkItem_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	ctx := context.Background()
	owner := pm.IdentityRef("user:owner")
	pid, _ := f.pmSvc.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: atTestOrg, Name: "P", CreatedBy: owner})
	tid, _ := f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "t", CreatedBy: owner})
	f.drain(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/discard_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": string(tid), "reason": "x"})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (non-member); body = %v", status, body)
	}
	if body["error"] != "not_agents_task" {
		t.Fatalf("error = %v, want not_agents_task", body["error"])
	}
	if got := f.taskStatus(t, string(tid)); got != pm.TaskOpen {
		t.Fatalf("task status = %s, want open (no write on 403)", got)
	}
}

// F4: post_message (task target) with parent_message_id threads the agent's reply IN the
// thread (parent=root) instead of at conversation top-level. This is the center
// half of the @agent-in-thread fix: the agent passes the thread root the wake brief
// gave it, and the reply lands in the thread.
func TestPostTaskMessage_ParentMessageID_ThreadsReply(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/post_message", "acat_w1",
		map[string]any{"agent_id": atAgent1, "target": map[string]any{"type": "task", "id": tid}, "content": "thread root"})
	if status != http.StatusOK {
		t.Fatalf("post root status=%d body=%v", status, body)
	}
	rootID, _ := body["message_id"].(string)
	if rootID == "" {
		t.Fatalf("missing root message_id: %v", body)
	}

	status, body = postBearer(t, srv.URL, "/admin/agent-tools/post_message", "acat_w1",
		map[string]any{"agent_id": atAgent1, "target": map[string]any{"type": "task", "id": tid}, "content": "in-thread reply", "parent_message_id": rootID})
	if status != http.StatusOK {
		t.Fatalf("post reply status=%d body=%v", status, body)
	}
	replyID, _ := body["message_id"].(string)

	for _, m := range f.taskMessages(t, tid) {
		if string(m.ID()) == replyID {
			if string(m.ParentMessageID()) != rootID || string(m.RootMessageID()) != rootID {
				t.Fatalf("reply must be in-thread (parent=root=%s), got parent=%q root=%q", rootID, m.ParentMessageID(), m.RootMessageID())
			}
			return
		}
	}
	t.Fatalf("reply message %s not found", replyID)
}

// --- unblock_task (v2.9.1 P0 recovery) ---------------------------------------

func TestUnblockTask_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	srv := f.server(t)

	// Block it first (simulating the failure path).
	if status, body := postBearer(t, srv.URL, "/admin/agent-tools/block_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid, "reason": "agent execution failed"}); status != http.StatusOK {
		t.Fatalf("block status = %d body=%v", status, body)
	}
	// ADR-0046: blocked is a running-task annotation, not a state.
	if got := f.taskStatus(t, tid); got != pm.TaskRunning {
		t.Fatalf("precondition: task should be running with a block annotation, got %s", got)
	}

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/unblock_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid})
	if status != http.StatusOK {
		t.Fatalf("unblock status = %d, want 200; body = %v", status, body)
	}
	if got := f.taskStatus(t, tid); got != pm.TaskRunning {
		t.Fatalf("task status = %s, want running after unblock", got)
	}
}

// CLASS-GUARD: the exact deadlock this P0 fixes — a task blocked with reason
// "agent execution failed" (the restart/stale-release path) must be RECOVERABLE
// back to executable and able to complete. Guards against the whole
// "restart → deadlocked blocked" defect class (no legal path out of blocked).
func TestUnblockTask_RecoversAgentExecutionFailedDeadlock(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	srv := f.server(t)

	// 1. The failure path blocks the running task with the generic reason.
	if status, _ := postBearer(t, srv.URL, "/admin/agent-tools/block_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid, "reason": "agent execution failed"}); status != http.StatusOK {
		t.Fatalf("block status = %d", status)
	}
	// 2. Recovery: unblock → running.
	if status, body := postBearer(t, srv.URL, "/admin/agent-tools/unblock_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid}); status != http.StatusOK {
		t.Fatalf("unblock status = %d body=%v", status, body)
	}
	if got := f.taskStatus(t, tid); got != pm.TaskRunning {
		t.Fatalf("after unblock: status = %s, want running", got)
	}
	// 3. The recovered task can now complete normally (the deadlock is gone).
	if status, body := postBearer(t, srv.URL, "/admin/agent-tools/complete_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid, "summary": "recovered and finished"}); status != http.StatusOK {
		t.Fatalf("complete status = %d body=%v", status, body)
	}
	if got := f.taskStatus(t, tid); got != pm.TaskCompleted {
		t.Fatalf("recovered task should complete, got %s", got)
	}
}

// ADR-0046: unblock_task on a task that carries NO blocked_reason is an idempotent
// NO-OP (returns 200) — not an error. "blocked" is no longer a state, so there is
// no illegal transition; clearing a non-existent annotation is harmless and avoids
// a double-dispatch on an already-active task.
func TestUnblockTask_NotBlocked_NoOp200(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t) // running, no blocked_reason
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/unblock_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid})
	if status != http.StatusOK {
		t.Fatalf("unblock of a non-stuck task should be a 200 no-op, got %d body=%v", status, body)
	}
	// Status unchanged — still running.
	if got := f.taskStatus(t, tid); got != pm.TaskRunning {
		t.Fatalf("task status = %s, want running (unchanged by no-op unblock)", got)
	}
}
