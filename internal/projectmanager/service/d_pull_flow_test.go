package service

// PR7 — #278 D pull-model end-to-end integration suite (mechanism layer).
//
// Owner: Tester (integration/e2e lane per conventions §19). Simulates the agent
// via DIRECT service calls (StartWork/CompleteTask/Tick/…) — NOT a real LLM; the
// real-claude behavioural layer is Tester2's consolidated run-real. This suite
// wires the cross-BC pull-flow (pm dispatch → agent pull-loop → task-sync) on one
// shared DB + relay and asserts the mechanism invariants.

import (
	"context"
	"testing"
	"time"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	agentservice "github.com/oopslink/agent-center/internal/agent/service"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	conversation "github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	convsql "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pmpkg "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
	wfsql "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

// pr7Harness wires the full D pull-flow across BCs on one shared in-mem DB:
// pm Service (dispatch + task-sync source) + agent Service (pull-loop) + a relay
// carrying ParticipantProjector + WorkItemProjector + TaskStatusSyncProjector.
type pr7Harness struct {
	pm       *Service
	agent    *agentservice.Service
	wiRepo   *agentsql.WorkItemRepo
	agents   *agentsql.AgentRepo
	tasks    *pmsql.TaskRepo
	activity *agentsql.ActivityEventRepo
	wiSink   *agentsql.WorkItemRepo // sink-wrapped: writes emit agent.work_item_transitioned
	relay    *outbox.Relay
	clk      *clock.FakeClock
	ctx      context.Context
}

// newReconciler builds a WorkItemReconciler over the harness's repos with a short
// staleAge for fast-test (production default is 30 min; the env override is
// AGENT_CENTER_WORKITEM_STALE_MINUTES).
func (h *pr7Harness) newReconciler(staleAge time.Duration) *agentservice.WorkItemReconciler {
	return agentservice.NewWorkItemReconciler(h.wiSink, h.activity, h.clk, staleAge, time.Minute, func(string, ...any) {})
}

func pr7Setup(t *testing.T) *pr7Harness {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	clk := clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	gen := idgen.NewGenerator(clk)
	ob := outboxsql.NewOutboxRepo(db)
	applied := outboxsql.NewAppliedRepo(db)
	wiRepo := agentsql.NewWorkItemRepo(db)
	agents := agentsql.NewAgentRepo(db)
	tasks := pmsql.NewTaskRepo(db)

	pmSvc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: tasks,
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Outbox: ob, AgentDir: allOrgDir("org-1"), IDGen: gen, Clock: clk,
	})
	// The agent service writes WorkItem transitions through a sink-wrapped repo so
	// StartWork/Complete/Fail emit agent.work_item_transitioned → relay →
	// TaskStatusSyncProjector (the production wiring, webconsole_wiring.go).
	sink := agentservice.NewOutboxWorkItemTransitionSink(ob, gen)
	activity := agentsql.NewActivityEventRepo(db)
	wiSink := agentsql.NewWorkItemRepoWithSink(db, sink)
	agentSvc := agentservice.New(agentservice.Deps{
		DB: db, Agents: agents, WorkItems: wiSink,
		Activity: activity,
		Workers:  wfsql.NewWorkerRepo(db), Outbox: ob, IDGen: gen, Clock: clk,
	})
	partProj := NewParticipantProjector(db, convsql.NewConversationRepo(db), applied, gen, clk)
	wiProj := NewWorkItemProjector(db, wiRepo, applied, gen, clk)
	taskSync := NewTaskStatusSyncProjector(db, pmSvc, applied, clk)
	relay := outbox.NewRelay(ob, applied, clk, partProj, wiProj, taskSync)

	return &pr7Harness{pm: pmSvc, agent: agentSvc, wiRepo: wiRepo, agents: agents, tasks: tasks, activity: activity, wiSink: wiSink, relay: relay, clk: clk, ctx: context.Background()}
}

func (h *pr7Harness) drain(t *testing.T) {
	t.Helper()
	if _, err := h.relay.RunOnce(h.ctx, 100); err != nil {
		t.Fatal(err)
	}
}

func (h *pr7Harness) taskStatus(t *testing.T, tid pmpkg.TaskID) pmpkg.TaskStatus {
	t.Helper()
	tk, err := h.tasks.FindByID(h.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	return tk.Status()
}

// activeWaiting counts an agent's single-active slot occupancy (active+waiting_input).
func (h *pr7Harness) activeWaiting(t *testing.T, agentID string) int {
	t.Helper()
	items, err := h.wiRepo.ListByAgent(h.ctx, agentpkg.AgentID(agentID))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, it := range items {
		if it.Status() == agentpkg.WorkItemActive || it.Status() == agentpkg.WorkItemWaitingInput {
			n++
		}
	}
	return n
}

// TestPR7_HappyPullFlow — the D 灵魂 happy path end-to-end:
// assign → WorkItem queued (task stays OPEN, assignee=metadata per v2.8.1 model fix)
// → agent StartWork → WI active + task running → CompleteTask → task completed + WI done.
// Single-active slot (active+waiting) never exceeds 1.
func TestPR7_HappyPullFlow(t *testing.T) {
	h := pr7Setup(t)
	seedAgentWithWorker(t, h.agents, h.ctx, "AG1", "W1")

	pid, _ := h.pm.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := h.pm.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "do the thing", CreatedBy: "user:a"})
	taskRef := "pm://tasks/" + string(tid)

	// 1. Assign — assignee is METADATA; task stays open; a queued WorkItem is dispatched.
	if err := h.pm.AssignTask(h.ctx, tid, "agent:AG1", "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	if got := h.taskStatus(t, tid); got != pmpkg.TaskOpen {
		t.Fatalf("after assign: task must stay OPEN (assignee=metadata, not a state), got %s", got)
	}
	wis, _ := h.wiRepo.ListByTask(h.ctx, taskRef)
	if len(wis) != 1 || wis[0].Status() != agentpkg.WorkItemQueued {
		t.Fatalf("after assign: want 1 queued WorkItem, got %+v", wis)
	}
	wiID := string(wis[0].ID())

	// 2. Agent pulls + starts — WI active; task open→running; single-active==1.
	if err := h.agent.StartWork(h.ctx, "AG1", wiID); err != nil {
		t.Fatalf("StartWork: %v", err)
	}
	h.drain(t)
	if n := h.activeWaiting(t, "AG1"); n != 1 {
		t.Fatalf("after StartWork: single-active slot must be 1, got %d", n)
	}
	if got := h.taskStatus(t, tid); got != pmpkg.TaskRunning {
		t.Fatalf("after StartWork: task must be RUNNING (WI active → task-sync), got %s", got)
	}

	// 3. Agent completes — task completed; live WorkItem finished (done); slot freed.
	if err := h.pm.CompleteTask(h.ctx, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	if got := h.taskStatus(t, tid); got != pmpkg.TaskCompleted {
		t.Fatalf("after CompleteTask: task must be COMPLETED, got %s", got)
	}
	if n := h.activeWaiting(t, "AG1"); n != 0 {
		t.Fatalf("after complete: single-active slot must be freed (0), got %d", n)
	}
	final, _ := h.wiRepo.ListByTask(h.ctx, taskRef)
	if len(final) != 1 || final[0].Status() != agentpkg.WorkItemDone {
		t.Fatalf("after complete: WorkItem must be done, got %+v", final)
	}
}

// TestPR7_SingleActiveByConstruction — @oopslink's original "multiple agents
// running simultaneously" bug, structurally eradicated: an agent with two queued
// WorkItems can only hold ONE active at a time. The second StartWork is rejected
// (ErrAgentHasActiveWork) — single-active is enforced on the mark-running step.
func TestPR7_SingleActiveByConstruction(t *testing.T) {
	h := pr7Setup(t)
	seedAgentWithWorker(t, h.agents, h.ctx, "AG1", "W1")

	pid, _ := h.pm.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	t1, _ := h.pm.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "t1", CreatedBy: "user:a"})
	t2, _ := h.pm.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "t2", CreatedBy: "user:a"})
	_ = h.pm.AssignTask(h.ctx, t1, "agent:AG1", "user:a")
	_ = h.pm.AssignTask(h.ctx, t2, "agent:AG1", "user:a")
	h.drain(t)

	wi1, _ := h.wiRepo.ListByTask(h.ctx, "pm://tasks/"+string(t1))
	wi2, _ := h.wiRepo.ListByTask(h.ctx, "pm://tasks/"+string(t2))
	if len(wi1) != 1 || len(wi2) != 1 {
		t.Fatalf("want 1 queued WI per task, got %d/%d", len(wi1), len(wi2))
	}
	// First start → active.
	if err := h.agent.StartWork(h.ctx, "AG1", string(wi1[0].ID())); err != nil {
		t.Fatalf("first StartWork: %v", err)
	}
	// Second start while one is active → rejected (single-active).
	err := h.agent.StartWork(h.ctx, "AG1", string(wi2[0].ID()))
	if err == nil {
		t.Fatal("second StartWork while one active MUST be rejected (single-active by construction)")
	}
	if n := h.activeWaiting(t, "AG1"); n != 1 {
		t.Fatalf("single-active slot must remain 1 (never 2), got %d", n)
	}
}

// TestPR7_TaskWISync_FailedToBlock — task↔WI convergence (the (b) fix) end-to-end:
// an active agent's work FAILS → WorkItem failed → TaskStatusSyncProjector blocks
// the running Task (no "running" limbo). Slot freed; task is re-dispatchable.
func TestPR7_TaskWISync_FailedToBlock(t *testing.T) {
	h := pr7Setup(t)
	seedAgentWithWorker(t, h.agents, h.ctx, "AG1", "W1")

	pid, _ := h.pm.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := h.pm.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})
	_ = h.pm.AssignTask(h.ctx, tid, "agent:AG1", "user:a")
	h.drain(t)
	wis, _ := h.wiRepo.ListByTask(h.ctx, "pm://tasks/"+string(tid))
	if err := h.agent.StartWork(h.ctx, "AG1", string(wis[0].ID())); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	if got := h.taskStatus(t, tid); got != pmpkg.TaskRunning {
		t.Fatalf("precondition: task should be running, got %s", got)
	}
	// Agent's work fails (e.g. unrecoverable error) → WI failed → task blocked.
	if err := h.agent.FailWork(h.ctx, "AG1", string(wis[0].ID())); err != nil {
		t.Fatalf("FailWork: %v", err)
	}
	h.drain(t)
	if got := h.taskStatus(t, tid); got != pmpkg.TaskBlocked {
		t.Fatalf("after FailWork: task must be BLOCKED (no running-limbo, per (b) fix), got %s", got)
	}
	if n := h.activeWaiting(t, "AG1"); n != 0 {
		t.Fatalf("after fail: single-active slot must be freed, got %d", n)
	}
}

// TestPR7_ReconcilerReleasesStuck — the self-healing watchdog (PR5): an active
// WorkItem whose agent goes silent past staleAge is released by the reconciler
// (FailFromAgentDeath under CAS) → WI failed → task Blocked (re-dispatchable via
// the (b) path). NO-FALSE-KILL: within the window it is NOT released. Uses a short
// staleAge for fast-test (prod default 30 min, env-configurable).
func TestPR7_ReconcilerReleasesStuck(t *testing.T) {
	h := pr7Setup(t)
	seedAgentWithWorker(t, h.agents, h.ctx, "AG1", "W1")
	const staleAge = 10 * time.Minute
	rec := h.newReconciler(staleAge)

	pid, _ := h.pm.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := h.pm.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})
	_ = h.pm.AssignTask(h.ctx, tid, "agent:AG1", "user:a")
	h.drain(t)
	wis, _ := h.wiRepo.ListByTask(h.ctx, "pm://tasks/"+string(tid))
	if err := h.agent.StartWork(h.ctx, "AG1", string(wis[0].ID())); err != nil {
		t.Fatal(err)
	}
	h.drain(t)

	// NO-FALSE-KILL: still within the window → not released.
	h.clk.Advance(staleAge - time.Minute)
	if n, err := rec.Tick(h.ctx); err != nil || n != 0 {
		t.Fatalf("within window: released=%d err=%v, want 0 (no false kill)", n, err)
	}
	if got := h.activeWaiting(t, "AG1"); got != 1 {
		t.Fatalf("within window: WI must stay active, slot=%d", got)
	}

	// Past staleAge with no activity → stuck → released (failed) → task blocked.
	h.clk.Advance(2 * time.Minute)
	n, err := rec.Tick(h.ctx)
	if err != nil || n != 1 {
		t.Fatalf("stale: released=%d err=%v, want 1", n, err)
	}
	h.drain(t)
	if got := h.activeWaiting(t, "AG1"); got != 0 {
		t.Fatalf("after reconciler release: single-active slot must be freed, got %d", got)
	}
	if got := h.taskStatus(t, tid); got != pmpkg.TaskBlocked {
		t.Fatalf("after reconciler release: task must be BLOCKED (stuck recovery → (b) path), got %s", got)
	}
}

// --- PR7 ⑥ dual-stream (#278 PR4b) — the agent's SECOND inbound stream: besides
// pulling WorkItems, an agent reads conversation messages addressed to it via
// get_my_unread (DM-all + channel-@mention only), and mark_seen dedups. Wired on
// the conversation BC (AgentInboxService + ReadStateService), mirroring the A6
// unit-test scope at the integration layer alongside the pull-flow suite.

func pr7InboxSetup(t *testing.T) (*convservice.AgentInboxService, *convservice.ReadStateService, *convsql.ConversationRepo, *convsql.MessageRepo, *clock.FakeClock, context.Context) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	clk := clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	gen := idgen.NewGenerator(clk)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, clk)
	convRepo := convsql.NewConversationRepo(db)
	msgRepo := convsql.NewMessageRepo(db)
	rsRepo := convsql.NewReadStateRepo(db)
	inbox := convservice.NewAgentInboxService(db, convRepo, rsRepo)
	rs := convservice.NewReadStateService(db, rsRepo, msgRepo, sink, clk)
	return inbox, rs, convRepo, msgRepo, clk, context.Background()
}

func pr7SeedConv(t *testing.T, repo *convsql.ConversationRepo, clk *clock.FakeClock, id conversation.ConversationID, kind conversation.ConversationKind, org string, participants ...conversation.IdentityRef) {
	t.Helper()
	parts := make([]conversation.ParticipantElement, 0, len(participants))
	for _, p := range participants {
		parts = append(parts, conversation.ParticipantElement{
			IdentityID: p, Role: "member", JoinedAt: clk.Now().Format(time.RFC3339Nano), JoinedBy: "user:alice",
		})
	}
	name := ""
	if kind == conversation.ConversationKindChannel {
		name = "ch-" + string(id)
	}
	c, err := conversation.NewConversation(conversation.NewConversationInput{
		ID: id, Kind: kind, Name: name, CreatedBy: "user:alice",
		OpenedAt: clk.Now(), Participants: parts, OrganizationID: org,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(context.Background(), c); err != nil {
		t.Fatal(err)
	}
}

func pr7SeedMsg(t *testing.T, repo *convsql.MessageRepo, clk *clock.FakeClock, id conversation.MessageID, conv conversation.ConversationID, sender conversation.IdentityRef, content string) {
	t.Helper()
	clk.Advance(time.Millisecond)
	m, err := conversation.NewMessage(conversation.NewMessageInput{
		ID: id, ConversationID: conv, SenderIdentityID: sender,
		ContentKind: conversation.MessageContentText, Content: content,
		Direction: conversation.DirectionInbound, PostedAt: clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Append(context.Background(), m); err != nil {
		t.Fatal(err)
	}
}

func TestPR7_DualStream(t *testing.T) {
	inbox, rs, convRepo, msgRepo, clk, ctx := pr7InboxSetup(t)
	const agent = conversation.IdentityRef("agent:bot-1")
	refs := []conversation.IdentityRef{agent}

	// DM(agent+user): every DM message is unread for the agent.
	pr7SeedConv(t, convRepo, clk, "dm-1", conversation.ConversationKindDM, "org-1", agent, "user:alice")
	pr7SeedMsg(t, msgRepo, clk, "dm-1-msg-1", "dm-1", "user:alice", "hey bot, ping")
	// Channel(agent participates): @mention is unread; plain chatter is NOT.
	pr7SeedConv(t, convRepo, clk, "ch-1", conversation.ConversationKindChannel, "org-1", agent, "user:alice")
	pr7SeedMsg(t, msgRepo, clk, "ch-1-msg-1", "ch-1", "user:alice", "morning everyone")
	pr7SeedMsg(t, msgRepo, clk, "ch-1-msg-2", "ch-1", "user:alice", "@Bot please review")

	idset := func(items []convservice.UnreadItem) map[conversation.MessageID]bool {
		m := map[conversation.MessageID]bool{}
		for _, it := range items {
			m[it.MessageID] = true
		}
		return m
	}

	got, err := inbox.ListUnreadForIdentity(ctx, refs, "org-1", "Bot")
	if err != nil {
		t.Fatal(err)
	}
	ids := idset(got)
	if !ids["dm-1-msg-1"] {
		t.Fatal("dual-stream: DM message must be unread for the agent")
	}
	if !ids["ch-1-msg-2"] {
		t.Fatal("dual-stream: channel @mention must be unread for the agent")
	}
	if ids["ch-1-msg-1"] {
		t.Fatal("dual-stream: channel non-mention chatter must NOT surface in get_my_unread")
	}

	// mark_seen the DM → it dedups out of the next get_my_unread.
	if _, err := rs.MarkSeen(ctx, convservice.MarkSeenCommand{
		UserID: agent, ConversationID: "dm-1", LastSeenMessageID: "dm-1-msg-1", Actor: observability.Actor(agent),
	}); err != nil {
		t.Fatalf("MarkSeen: %v", err)
	}
	if idset(mustUnread(t, inbox, ctx, refs))["dm-1-msg-1"] {
		t.Fatal("dual-stream: after mark_seen the DM message must no longer be unread (dedup)")
	}
}

func mustUnread(t *testing.T, inbox *convservice.AgentInboxService, ctx context.Context, refs []conversation.IdentityRef) []convservice.UnreadItem {
	t.Helper()
	got, err := inbox.ListUnreadForIdentity(ctx, refs, "org-1", "Bot")
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestPR7_CompleteVsReleaseRace — restart/liveness race (7a): a WorkItem is stale
// (agent went quiet past staleAge, so the reconciler WOULD release it) but the
// agent completes it first. The reconciler must NOT double-process: its
// ListByStatus(active) no longer sees the finished WI, so it releases 0 and the
// task stays COMPLETED (no spurious reconciler-block of a just-finished task).
// Mirror of the A7 complete-vs-release CAS unit tests at the integration layer.
func TestPR7_CompleteVsReleaseRace(t *testing.T) {
	h := pr7Setup(t)
	seedAgentWithWorker(t, h.agents, h.ctx, "AG1", "W1")
	const staleAge = 10 * time.Minute
	rec := h.newReconciler(staleAge)

	pid, _ := h.pm.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := h.pm.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})
	_ = h.pm.AssignTask(h.ctx, tid, "agent:AG1", "user:a")
	h.drain(t)
	wis, _ := h.wiRepo.ListByTask(h.ctx, "pm://tasks/"+string(tid))
	if err := h.agent.StartWork(h.ctx, "AG1", string(wis[0].ID())); err != nil {
		t.Fatal(err)
	}
	h.drain(t)

	// WI is now stale (would be reconciler-eligible)...
	h.clk.Advance(staleAge + time.Minute)
	// ...but the agent wins the race by completing first.
	if err := h.pm.CompleteTask(h.ctx, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	// Reconciler ticks on the (formerly stale) WI — it is done, not active → 0 released.
	n, err := rec.Tick(h.ctx)
	if err != nil || n != 0 {
		t.Fatalf("agent won the race: reconciler must release 0 (WI already done), got %d err=%v", n, err)
	}
	h.drain(t)
	if got := h.taskStatus(t, tid); got != pmpkg.TaskCompleted {
		t.Fatalf("agent-wins race: task must stay COMPLETED (no spurious reconciler-block of a finished WI), got %s", got)
	}
	if got := h.activeWaiting(t, "AG1"); got != 0 {
		t.Fatalf("slot must be freed after completion, got %d", got)
	}
}
