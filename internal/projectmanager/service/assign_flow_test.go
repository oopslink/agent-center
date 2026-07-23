package service

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convsql "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// flowSetup wires the Service + the participant projector + a relay, so a test
// can drive assign→outbox→participant end to end. (The work-item projector was
// removed with the AgentWorkItem domain; these tests now exercise the surviving
// Task-model behavior only.)
func flowSetup(t *testing.T) (*Service, *outbox.Relay, context.Context) {
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
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: pmsql.NewTaskRepo(db),
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Outbox: ob, AgentDir: allOrgDir("org-1"), IDGen: gen, Clock: clk,
	})
	partProj := NewParticipantProjector(db, convsql.NewConversationRepo(db), applied, gen, clk)
	relay := outbox.NewRelay(ob, applied, clk, partProj)
	return svc, relay, context.Background()
}

// TestParticipantProjector_BranchHandling exercises the participant projector
// dispatch branches: non-matching event types are no-ops; matching events with a
// bad/empty payload surface errors (leaving the event for retry).
func TestParticipantProjector_BranchHandling(t *testing.T) {
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
	ctx := context.Background()

	// ignored event types → nil, no side effect
	if err := pp.Project(ctx, outbox.Event{ID: "e1", EventType: EvtProjectCreated, Payload: "{}"}); err != nil {
		t.Fatalf("%s should ignore project.created: %v", pp.Name(), err)
	}
	// participant projector: task event with empty owner_ref → error
	if err := pp.Project(ctx, outbox.Event{ID: "e2", EventType: EvtTaskCreated, Payload: `{"owner_ref":""}`}); err == nil {
		t.Fatal("participant projector should error on missing owner_ref")
	}
	// bad JSON → error
	if err := pp.Project(ctx, outbox.Event{ID: "e3", EventType: EvtTaskCreated, Payload: `not-json`}); err == nil {
		t.Fatal("participant projector should error on bad payload")
	}
}

// TestTaskStateFlow_CompleteThenNonSelfVerify covers the §2.2 acceptance:
// running→completed→verified, with self-verification rejected.
func TestTaskStateFlow_CompleteThenNonSelfVerify(t *testing.T) {
	svc, _, ctx := flowSetup(t)
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
	if err := svc.BlockTask(ctx, tid, "", pm.BlockReasonObstacle, "user:b"); err != pm.ErrBlockReasonRequired {
		t.Fatalf("block without reason should fail, got %v", err)
	}
	if err := svc.CompleteTask(ctx, tid, "user:b"); err != nil {
		t.Fatal(err)
	}
	// ADR-0046: verification removed — completed is the terminal done state.
	tk, _ := svc.tasks.FindByID(ctx, tid)
	if tk.Status() != pm.TaskCompleted {
		t.Fatalf("task should be completed, got %s", tk.Status())
	}
}

func TestCompleteTaskRejectsReportedZeroDelivery(t *testing.T) {
	svc, _, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if _, err := svc.AddProjectMember(ctx, AddProjectMemberCommand{ProjectID: pid, IdentityID: "user:b", Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "code", CreatedBy: "user:a"})
	if err := svc.AssignTask(ctx, tid, "user:b", "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartTask(ctx, tid, "user:b"); err != nil {
		t.Fatal(err)
	}
	task, err := svc.tasks.FindByID(ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	task.SetDelivery(&pm.Delivery{Probed: true, Pushed: false, Branch: "ac-exec/task/e", HeadSHA: "base", BaseKnown: true, AheadOfBase: 0})
	if err := svc.tasks.Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := svc.CompleteTask(ctx, tid, "user:b"); err == nil {
		t.Fatal("reported zero-delivery must not be completable")
	}
}

// TestReassign_DropsOldAssigneeFromSubscribers is the §4.2/ADR-0052 acceptance:
// on reassignment the previous assignee leaves the effective subscriber set
// (unless creator/manual), and the new assignee joins it.
func TestReassign_DropsOldAssigneeFromSubscribers(t *testing.T) {
	svc, _, ctx := flowSetup(t)
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

func TestDiscardTask(t *testing.T) {
	svc, _, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})
	if err := svc.DiscardTask(ctx, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	tk, _ := svc.tasks.FindByID(ctx, tid)
	if tk.Status() != pm.TaskDiscarded {
		t.Fatalf("want canceled, got %s", tk.Status())
	}
	// non-member can't cancel another project's task
	if err := svc.DiscardTask(ctx, tid, "user:stranger"); err != ErrNotMember {
		t.Fatalf("non-member cancel want ErrNotMember, got %v", err)
	}
}

func TestCreateIssue_GatingAndUnsubscribe(t *testing.T) {
	svc, _, ctx := flowSetup(t)
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

// TestBlockAnnotates_Unblock is the block/unblock round-trip acceptance. ADR-0054
// amends the ADR-0046 contract it originally pinned: Block now PARKS the task (status →
// blocked, which is what actually stops dispatch) while STILL writing the reason
// annotation; Unblock un-parks it back to running and clears the reason.
func TestBlockAnnotates_Unblock(t *testing.T) {
	svc, _, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})

	_ = svc.AssignTask(ctx, tid, "agent:AG1", "user:a")
	_ = svc.StartTask(ctx, tid, "user:a")

	// Block the Task → ADR-0054 PARK: status really moves to blocked (that is what stops
	// dispatch) AND the reason annotation is still written.
	if err := svc.BlockTask(ctx, tid, "needs key", pm.BlockReasonObstacle, "user:a"); err != nil {
		t.Fatal(err)
	}
	tk, _ := svc.tasks.FindByID(ctx, tid)
	if tk.Status() != pm.TaskBlocked || tk.BlockedReason() != "needs key" {
		t.Fatalf("after block: want blocked + reason set, got %s / %q", tk.Status(), tk.BlockedReason())
	}

	// Unblock → un-parked back to running, reason cleared.
	if err := svc.UnblockTask(ctx, UnblockTaskCommand{TaskID: tid, Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	tk, _ = svc.tasks.FindByID(ctx, tid)
	if tk.Status() != pm.TaskRunning || tk.BlockedReason() != "" {
		t.Fatalf("after unblock: want running + reason cleared, got %s / %q", tk.Status(), tk.BlockedReason())
	}
}

// TestUnassignTask_ClearsAssignee: Unassign (assigned→open) clears the assignee
// metadata and returns the task to open.
func TestUnassignTask_ClearsAssignee(t *testing.T) {
	svc, _, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})

	_ = svc.AssignTask(ctx, tid, "agent:AG1", "user:a")

	if err := svc.UnassignTask(ctx, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	tk, _ := svc.tasks.FindByID(ctx, tid)
	if tk.Status() != pm.TaskOpen || tk.Assignee() != "" {
		t.Fatalf("after unassign: want open + no assignee, got %s / %q", tk.Status(), tk.Assignee())
	}
}

func TestReopenTask_ClearsAssignmentAndCompletion(t *testing.T) {
	svc, _, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})
	_ = svc.AssignTask(ctx, tid, "user:a", "user:a")
	_ = svc.StartTask(ctx, tid, "user:a")
	if err := svc.CompleteTask(ctx, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	// Reopen a completed Task → straight back to open, assignment + completion cleared.
	if err := svc.ReopenTask(ctx, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	tk, _ := svc.tasks.FindByID(ctx, tid)
	if tk.Status() != pm.TaskOpen || tk.Assignee() != "" || tk.CompletedBy() != "" {
		t.Fatalf("after reopen: want open + cleared assignee/completer, got %s / %q / %q",
			tk.Status(), tk.Assignee(), tk.CompletedBy())
	}
	// Reopen from a non-completed/verified state is illegal.
	if err := svc.ReopenTask(ctx, tid, "user:a"); err != pm.ErrIllegalTransition {
		t.Fatalf("reopen from open should be ErrIllegalTransition, got %v", err)
	}
}

// TestOffboardedAssignee_RetainedAsSubscriber is the #98 OQ13 semantics: anyone
// taken off a Task (unassign / reassign-away / reopen) is RETAINED as a sticky
// manual subscriber — they stay in the task Conversation, downgraded to
// subscriber, until an explicit Unsubscribe removes them. Conversation
// membership is monotonic; task state reset does not evict.
func TestOffboardedAssignee_RetainedAsSubscriber(t *testing.T) {
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
	convRepo := convsql.NewConversationRepo(db)
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: pmsql.NewTaskRepo(db),
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Outbox: ob, AgentDir: allOrgDir("org-1"), IDGen: gen, Clock: clk,
	})
	relay := outbox.NewRelay(ob, applied, clk,
		NewParticipantProjector(db, convRepo, applied, gen, clk))
	ctx := context.Background()
	drain := func() {
		for i := 0; i < 8; i++ {
			n, err := relay.RunOnce(ctx, 100)
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				return
			}
		}
	}
	hasParticipant := func(tid pm.TaskID, who string) bool {
		conv, err := convRepo.FindByOwnerRef(ctx, conversation.NewTaskOwnerRef(string(tid)))
		if err != nil {
			t.Fatalf("task conversation not found: %v", err)
		}
		for _, p := range conv.Participants() {
			if string(p.IdentityID) == who {
				return true
			}
		}
		return false
	}

	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})

	// Assign agent:AG1 → it joins the task Conversation.
	_ = svc.AssignTask(ctx, tid, "agent:AG1", "user:a")
	drain()
	if !hasParticipant(tid, "agent:AG1") {
		t.Fatal("after assign: AG1 should be a participant")
	}

	// Reassign AG1→AG2: AG1 is retained as a sticky subscriber; AG2 is the new
	// assignee. BOTH stay in the Conversation.
	_ = svc.AssignTask(ctx, tid, "agent:AG2", "user:a")
	drain()
	if !hasParticipant(tid, "agent:AG1") {
		t.Fatal("after reassign: outgoing AG1 should be retained as subscriber")
	}
	if !hasParticipant(tid, "agent:AG2") {
		t.Fatal("after reassign: new assignee AG2 should be a participant")
	}

	// Unassign AG2 → AG2 retained as subscriber; AG1 still there; creator stays.
	if err := svc.UnassignTask(ctx, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	drain()
	if !hasParticipant(tid, "agent:AG2") {
		t.Fatal("after unassign: ex-assignee AG2 should be retained as subscriber")
	}
	if !hasParticipant(tid, "agent:AG1") {
		t.Fatal("after unassign: AG1 should still be a subscriber")
	}

	// Explicit Unsubscribe is the ONLY way out: AG1 leaves, AG2 stays.
	if err := svc.UnsubscribeTask(ctx, tid, "agent:AG1", "user:a"); err != nil {
		t.Fatal(err)
	}
	drain()
	if hasParticipant(tid, "agent:AG1") {
		t.Fatal("after unsubscribe: AG1 should be removed")
	}
	if !hasParticipant(tid, "agent:AG2") {
		t.Fatal("after unsubscribe: AG2 should remain a subscriber")
	}

	// Reopen path: assign AG3, run, complete via a NON-creator member, reopen →
	// BOTH the prior assignee (AG3) and the completer (user:b) are retained.
	if _, err := svc.AddProjectMember(ctx, AddProjectMemberCommand{ProjectID: pid, IdentityID: "user:b", Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	_ = svc.AssignTask(ctx, tid, "agent:AG3", "user:a")
	_ = svc.StartTask(ctx, tid, "user:a")
	if err := svc.CompleteTask(ctx, tid, "user:b"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReopenTask(ctx, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	drain()
	if !hasParticipant(tid, "agent:AG3") {
		t.Fatal("after reopen: prior assignee AG3 should be retained as subscriber")
	}
	if !hasParticipant(tid, "user:b") {
		t.Fatal("after reopen: completer user:b should be retained as subscriber")
	}
}

func TestUnassignReopen_Gating(t *testing.T) {
	svc, _, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})
	_ = svc.AssignTask(ctx, tid, "agent:AG1", "user:a")

	// non-member cannot unassign / reopen.
	if err := svc.UnassignTask(ctx, tid, "user:stranger"); err != ErrNotMember {
		t.Fatalf("unassign by non-member: want ErrNotMember, got %v", err)
	}
	if err := svc.ReopenTask(ctx, tid, "user:stranger"); err != ErrNotMember {
		t.Fatalf("reopen by non-member: want ErrNotMember, got %v", err)
	}
	// v2.8.1: unassign is a metadata clear (no "assigned" state) — allowed in any
	// non-terminal state and idempotent (a repeat unassign is a no-op, not illegal).
	if err := svc.UnassignTask(ctx, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.UnassignTask(ctx, tid, "user:a"); err != nil {
		t.Fatalf("repeat unassign (metadata, idempotent): want nil, got %v", err)
	}
	// unsubscribe by non-member is gated too.
	if err := svc.UnsubscribeTask(ctx, tid, "agent:AG1", "user:stranger"); err != ErrNotMember {
		t.Fatalf("unsubscribe by non-member: want ErrNotMember, got %v", err)
	}
}
