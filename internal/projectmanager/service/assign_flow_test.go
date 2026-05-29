package service

import (
	"context"
	"testing"
	"time"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	convsql "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// flowSetup wires the Service + BOTH projectors (participant + work item) + a
// relay, so a test can drive assign→outbox→WorkItem/participant end to end.
func flowSetup(t *testing.T) (*Service, *agentsql.WorkItemRepo, *outbox.Relay, context.Context) {
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
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: pmsql.NewTaskRepo(db),
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Outbox: ob, IDGen: gen, Clock: clk,
	})
	partProj := NewParticipantProjector(db, convsql.NewConversationRepo(db), applied, gen, clk)
	wiProj := NewWorkItemProjector(db, wiRepo, applied, gen, clk)
	relay := outbox.NewRelay(ob, applied, clk, partProj, wiProj)
	return svc, wiRepo, relay, context.Background()
}

// TestProjectors_BranchHandling exercises the projector dispatch branches:
// non-matching event types are no-ops; matching events with a bad/empty payload
// surface errors (leaving the event for retry).
func TestProjectors_BranchHandling(t *testing.T) {
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	clk := clock.NewFakeClock(time.Unix(1, 0).UTC())
	gen := idgen.NewGenerator(clk)
	applied := outboxsql.NewAppliedRepo(db)
	pp := NewParticipantProjector(db, convsql.NewConversationRepo(db), applied, gen, clk)
	wp := NewWorkItemProjector(db, agentsql.NewWorkItemRepo(db), applied, gen, clk)
	ctx := context.Background()

	// ignored event types → nil, no side effect
	for _, p := range []outbox.Projector{pp, wp} {
		if err := p.Project(ctx, outbox.Event{ID: "e1", EventType: EvtProjectCreated, Payload: "{}"}); err != nil {
			t.Fatalf("%s should ignore project.created: %v", p.Name(), err)
		}
	}
	// participant projector: task event with empty owner_ref → error
	if err := pp.Project(ctx, outbox.Event{ID: "e2", EventType: EvtTaskCreated, Payload: `{"owner_ref":""}`}); err == nil {
		t.Fatal("participant projector should error on missing owner_ref")
	}
	// bad JSON → error
	if err := pp.Project(ctx, outbox.Event{ID: "e3", EventType: EvtTaskCreated, Payload: `not-json`}); err == nil {
		t.Fatal("participant projector should error on bad payload")
	}
	if err := wp.Project(ctx, outbox.Event{ID: "e4", EventType: EvtTaskAssigned, Payload: `not-json`}); err == nil {
		t.Fatal("work item projector should error on bad payload")
	}
	// work item projector: assigned to a human (non-agent) → supersede only, no new WorkItem
	if err := wp.Project(ctx, outbox.Event{ID: "e5", EventType: EvtTaskAssigned, Payload: `{"owner_ref":"pm://tasks/T9","assignee":"user:x"}`}); err != nil {
		t.Fatalf("human assignee should be a no-op create: %v", err)
	}
}

func liveAndSuperseded(items []*agentpkg.AgentWorkItem) (live, superseded int) {
	for _, w := range items {
		switch w.Status() {
		case agentpkg.WorkItemSuperseded:
			superseded++
		default:
			if !w.Status().IsTerminal() {
				live++
			}
		}
	}
	return
}

// TestAssignAndReassign_WorkItemSupersedeChain is the #100/#97 acceptance:
// assign → queued WorkItem; reassign → old superseded + new queued, both under
// the same Task (one stable Conversation).
func TestAssignAndReassign_WorkItemSupersedeChain(t *testing.T) {
	svc, wiRepo, relay, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})
	taskRef := "pm://tasks/" + string(tid)

	if err := svc.AssignTask(ctx, tid, "agent:AG1", "user:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := relay.RunOnce(ctx, 100); err != nil {
		t.Fatal(err)
	}
	items, _ := wiRepo.ListByTask(ctx, taskRef)
	if live, sup := liveAndSuperseded(items); live != 1 || sup != 0 || len(items) != 1 {
		t.Fatalf("after assign: want 1 live 0 superseded, got %d items (live=%d sup=%d)", len(items), live, sup)
	}
	if items[0].AgentID() != "AG1" {
		t.Fatalf("work item agent = %s, want AG1", items[0].AgentID())
	}

	// Reassign to a different agent → old superseded + new queued.
	if err := svc.AssignTask(ctx, tid, "agent:AG2", "user:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := relay.RunOnce(ctx, 100); err != nil {
		t.Fatal(err)
	}
	items2, _ := wiRepo.ListByTask(ctx, taskRef)
	if live, sup := liveAndSuperseded(items2); live != 1 || sup != 1 || len(items2) != 2 {
		t.Fatalf("after reassign: want 1 live + 1 superseded, got %d (live=%d sup=%d)", len(items2), live, sup)
	}

	// Replay is idempotent — no extra WorkItems.
	if n, _ := relay.RunOnce(ctx, 100); n != 0 {
		t.Fatalf("replay processed %d, want 0", n)
	}
	items3, _ := wiRepo.ListByTask(ctx, taskRef)
	if len(items3) != 2 {
		t.Fatalf("replay created extra work items: %d", len(items3))
	}
}

// TestTaskStateFlow_CompleteThenNonSelfVerify covers the §2.2 acceptance:
// running→completed→verified, with self-verification rejected.
func TestTaskStateFlow_CompleteThenNonSelfVerify(t *testing.T) {
	svc, _, _, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if _, err := svc.AddProjectMember(ctx, AddProjectMemberCommand{ProjectID: pid, IdentityID: "user:b", Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})

	if err := svc.AssignTask(ctx, tid, "user:b", "user:a"); err != nil {
		t.Fatal(err)
	}
	// can't complete before running
	if err := svc.CompleteTask(ctx, tid, "user:b"); err != pm.ErrIllegalTransition {
		t.Fatalf("complete before running should be illegal, got %v", err)
	}
	if err := svc.StartTask(ctx, tid, "user:b"); err != nil {
		t.Fatal(err)
	}
	// block requires a reason
	if err := svc.BlockTask(ctx, tid, "", "user:b"); err != pm.ErrBlockReasonRequired {
		t.Fatalf("block without reason should fail, got %v", err)
	}
	if err := svc.CompleteTask(ctx, tid, "user:b"); err != nil {
		t.Fatal(err)
	}
	// self-verify rejected
	if err := svc.VerifyTask(ctx, tid, "user:b"); err != pm.ErrSelfVerify {
		t.Fatalf("self-verify should be ErrSelfVerify, got %v", err)
	}
	// a different member verifies
	if err := svc.VerifyTask(ctx, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	tk, _ := svc.tasks.FindByID(ctx, tid)
	if tk.Status() != pm.TaskVerified {
		t.Fatalf("task should be verified, got %s", tk.Status())
	}
}

// TestReassign_DropsOldAssigneeFromSubscribers is the §4.2/ADR-0052 acceptance:
// on reassignment the previous assignee leaves the effective subscriber set
// (unless creator/manual), and the new assignee joins it.
func TestReassign_DropsOldAssigneeFromSubscribers(t *testing.T) {
	svc, _, _, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})

	_ = svc.AssignTask(ctx, tid, "user:b", "user:a")
	tk, _ := svc.tasks.FindByID(ctx, tid)
	eff := EffectiveTaskSubscribers(tk, nil)
	if !hasID(eff, "user:b") || !hasID(eff, "user:a") {
		t.Fatalf("after assign want {creator,user:b}, got %v", eff)
	}
	// reassign to user:c — user:b is neither creator nor manual → drops out.
	_ = svc.AssignTask(ctx, tid, "user:c", "user:a")
	tk2, _ := svc.tasks.FindByID(ctx, tid)
	eff2 := EffectiveTaskSubscribers(tk2, nil)
	if hasID(eff2, "user:b") {
		t.Fatalf("old assignee user:b must drop from effective subs, got %v", eff2)
	}
	if !hasID(eff2, "user:c") || !hasID(eff2, "user:a") {
		t.Fatalf("want {creator,user:c}, got %v", eff2)
	}
}

func TestCancelTask(t *testing.T) {
	svc, _, _, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})
	if err := svc.CancelTask(ctx, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	tk, _ := svc.tasks.FindByID(ctx, tid)
	if tk.Status() != pm.TaskCanceled {
		t.Fatalf("want canceled, got %s", tk.Status())
	}
	// non-member can't cancel another project's task
	if err := svc.CancelTask(ctx, tid, "user:stranger"); err != ErrNotMember {
		t.Fatalf("non-member cancel want ErrNotMember, got %v", err)
	}
}

func TestCreateIssue_GatingAndUnsubscribe(t *testing.T) {
	svc, _, _, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	// non-member cannot create an issue
	if _, err := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: pid, Title: "x", CreatedBy: "user:stranger"}); err != ErrNotMember {
		t.Fatalf("non-member create issue want ErrNotMember, got %v", err)
	}
	iid, _ := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: pid, Title: "bug", CreatedBy: "user:a"})
	_ = svc.SubscribeIssue(ctx, iid, "user:w", "user:a")
	if err := svc.UnsubscribeIssue(ctx, iid, "user:w", "user:a"); err != nil {
		t.Fatal(err)
	}
	manual, _ := svc.issueSubs.ListByIssue(ctx, iid)
	if len(manual) != 0 {
		t.Fatalf("unsubscribe should remove manual issue sub, got %d", len(manual))
	}
}

// TestBlockCancelsWorkItem_UnblockCreatesNew is the §10 OQ11 acceptance: a
// blocked Task CANCELS its live WorkItem (no WorkItem `blocked`), and unblocking
// creates a fresh WorkItem (nothing to supersede — the old one is canceled).
func TestBlockCancelsWorkItem_UnblockCreatesNew(t *testing.T) {
	svc, wiRepo, relay, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})
	taskRef := "pm://tasks/" + string(tid)
	list := func() []*agentpkg.AgentWorkItem {
		items, err := wiRepo.ListByTask(ctx, taskRef)
		if err != nil {
			t.Fatal(err)
		}
		return items
	}

	_ = svc.AssignTask(ctx, tid, "agent:AG1", "user:a")
	_ = svc.StartTask(ctx, tid, "user:a")
	_, _ = relay.RunOnce(ctx, 100)
	if sc := statusCount(list()); sc[agentpkg.WorkItemSuperseded] != 0 || liveCount(list()) != 1 {
		t.Fatalf("after assign: want 1 live WorkItem, got %v", sc)
	}

	// Block the Task → the live WorkItem is CANCELED (not blocked).
	if err := svc.BlockTask(ctx, tid, "needs key", "user:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := relay.RunOnce(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if liveCount(list()) != 0 || statusCount(list())[agentpkg.WorkItemCanceled] != 1 {
		t.Fatalf("after block: want 0 live + 1 canceled, got %v", statusCount(list()))
	}

	// Unblock → a brand-new WorkItem is created (no supersede).
	if err := svc.UnblockTask(ctx, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := relay.RunOnce(ctx, 100); err != nil {
		t.Fatal(err)
	}
	sc := statusCount(list())
	if liveCount(list()) != 1 || sc[agentpkg.WorkItemCanceled] != 1 || sc[agentpkg.WorkItemSuperseded] != 0 {
		t.Fatalf("after unblock: want 1 live + 1 canceled + 0 superseded, got %v", sc)
	}
}

func statusCount(items []*agentpkg.AgentWorkItem) map[agentpkg.WorkItemStatus]int {
	m := map[agentpkg.WorkItemStatus]int{}
	for _, w := range items {
		m[w.Status()]++
	}
	return m
}

func liveCount(items []*agentpkg.AgentWorkItem) (n int) {
	for _, w := range items {
		if !w.Status().IsTerminal() {
			n++
		}
	}
	return
}
