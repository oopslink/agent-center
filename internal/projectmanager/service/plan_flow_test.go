package service

import (
	"context"
	"errors"
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

// planSetup wires the producer Service (with the Plans repo) + BOTH the task/issue
// ParticipantProjector and the v2.9 PlanParticipantProjector + a relay over one
// shared DB, so a test can drive the end-to-end Plan AppService→outbox→projector→
// Conversation path (§9.5).
func planSetup(t *testing.T) (*Service, *convsql.ConversationRepo, *pmsql.PlanRepo, *pmsql.TaskRepo, *outbox.Relay, context.Context) {
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
	convRepo := convsql.NewConversationRepo(db)
	plans := pmsql.NewPlanRepo(db)
	tasks := pmsql.NewTaskRepo(db)
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: tasks,
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Plans: plans, Outbox: ob, IDGen: gen, Clock: clk,
	})
	taskProj := NewParticipantProjector(db, convRepo, applied, gen, clk)
	planProj := NewPlanParticipantProjector(db, convRepo, plans, applied, gen, clk)
	relay := outbox.NewRelay(ob, applied, clk, taskProj, planProj)
	return svc, convRepo, plans, tasks, relay, context.Background()
}

// drain runs the relay until no more events are processed.
func drain(t *testing.T, relay *outbox.Relay, ctx context.Context) {
	t.Helper()
	for {
		n, err := relay.RunOnce(ctx, 100)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return
		}
	}
}

func TestCreatePlan_CreatesConversationAndBinds(t *testing.T) {
	svc, convRepo, plans, _, relay, ctx := planSetup(t)
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "Sprint 1", CreatedBy: "user:a"})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	// Plan persisted in draft.
	p, err := plans.FindByID(ctx, planID)
	if err != nil {
		t.Fatalf("plan should be persisted: %v", err)
	}
	if p.Status() != pm.PlanDraft {
		t.Fatalf("plan status = %s, want draft", p.Status())
	}

	drain(t, relay, ctx)

	// The Plan's 1:1 Conversation exists by owner_ref pm://plans/{id}.
	ownerRef := conversation.NewPlanOwnerRef(string(planID))
	conv, err := convRepo.FindByOwnerRef(ctx, ownerRef)
	if err != nil {
		t.Fatalf("plan Conversation should exist by owner_ref: %v", err)
	}
	if conv.Kind() != conversation.ConversationKindPlan {
		t.Fatalf("conv kind = %s, want plan", conv.Kind())
	}
	if conv.OrganizationID() != "org-1" {
		t.Fatalf("plan Conversation org = %q, want org-1 (org-scoped @mention/wake would 404)", conv.OrganizationID())
	}
	if ids := participantIDs(conv); len(ids) != 1 || ids[0] != "user:a" {
		t.Fatalf("participants should be {creator}, got %v", ids)
	}

	// The Plan's conversationID is bound back (1:1).
	p2, _ := plans.FindByID(ctx, planID)
	if p2.ConversationID() != string(conv.ID()) {
		t.Fatalf("plan.conversationID = %q, want %q", p2.ConversationID(), string(conv.ID()))
	}
}

func TestPlanParticipantsChanged_LegacyBuiltinPlanDerivesOrgWhenConversationMissing(t *testing.T) {
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	clk := clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	gen := idgen.NewGenerator(clk)
	ob := outboxsql.NewOutboxRepo(db)
	applied := outboxsql.NewAppliedRepo(db)
	convRepo := convsql.NewConversationRepo(db)
	plans := pmsql.NewPlanRepo(db)
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: pmsql.NewTaskRepo(db),
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Plans: plans, Outbox: ob, IDGen: gen, Clock: clk,
	})
	taskProj := NewParticipantProjector(db, convRepo, applied, gen, clk)
	planProj := NewPlanParticipantProjector(db, convRepo, plans, applied, gen, clk)
	relay := outbox.NewRelay(ob, applied, clk, taskProj, planProj)

	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-legacy", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	ps, err := plans.ListByProject(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	var builtin *pm.Plan
	for _, p := range ps {
		if p.IsBuiltin() {
			builtin = p
			break
		}
	}
	if builtin == nil {
		t.Fatal("builtin plan not found")
	}
	// Simulate the production legacy shape: the built-in plan exists, but its
	// plan.created event has already been swallowed/processed without creating
	// the plan conversation; the later participants_changed event also carries
	// an empty organization_id.
	if _, err := db.ExecContext(ctx, `UPDATE outbox_events SET processed_at = ? WHERE event_type = ?`,
		clk.Now().Format(time.RFC3339Nano), EvtPlanCreated); err != nil {
		t.Fatal(err)
	}
	legacyPayload := `{"plan_id":"` + string(builtin.ID()) + `","project_id":"` + string(pid) + `","organization_id":"","owner_ref":"pm://plans/` + string(builtin.ID()) + `","participants":["agent:agent-1"]}`
	if err := ob.Append(ctx, outbox.Event{
		ID:        "01LEGACYPLANPARTICIPANT000",
		EventType: EvtPlanParticipantsChanged,
		Payload:   legacyPayload,
		CreatedAt: clk.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	drain(t, relay, ctx)

	conv, err := convRepo.FindByOwnerRef(ctx, conversation.NewPlanOwnerRef(string(builtin.ID())))
	if err != nil {
		t.Fatalf("legacy participants_changed should self-heal missing plan conversation: %v", err)
	}
	if conv.OrganizationID() != "org-legacy" {
		t.Fatalf("plan Conversation org = %q, want derived org-legacy", conv.OrganizationID())
	}
	if ids := participantIDs(conv); !hasID(ids, "agent:agent-1") {
		t.Fatalf("participants should include legacy assignee, got %v", ids)
	}
	p2, _ := plans.FindByID(ctx, builtin.ID())
	if p2.ConversationID() != string(conv.ID()) {
		t.Fatalf("builtin plan conversationID = %q, want %q", p2.ConversationID(), string(conv.ID()))
	}
}

func TestCreatePlan_Unavailable_WhenNoPlanRepo(t *testing.T) {
	// A Service with NO Plans repo wired.
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
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Tasks: pmsql.NewTaskRepo(db), Outbox: outboxsql.NewOutboxRepo(db), IDGen: gen, Clock: clk,
	})
	if _, err := svc.CreatePlan(context.Background(), CreatePlanCommand{ProjectID: "p", Name: "x", CreatedBy: "user:a"}); !errors.Is(err, ErrPlansUnavailable) {
		t.Fatalf("CreatePlan with nil plans = %v, want ErrPlansUnavailable", err)
	}
}

func TestSelectTaskIntoPlan_HumanAssignee_BecomesParticipant(t *testing.T) {
	svc, convRepo, _, _, relay, ctx := planSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "Sprint", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})
	// Assign a HUMAN to the task (BatchUpdateTask assigns without the agent-membership
	// grant, so no AgentDirectory is needed for a human).
	human := "user:bob"
	if err := svc.BatchUpdateTask(ctx, tid, BatchTaskPatch{Assignee: &human}, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SelectTaskIntoPlan(ctx, planID, tid, "user:a"); err != nil {
		t.Fatalf("SelectTaskIntoPlan: %v", err)
	}
	drain(t, relay, ctx)

	conv, _ := convRepo.FindByOwnerRef(ctx, conversation.NewPlanOwnerRef(string(planID)))
	ids := participantIDs(conv)
	if !hasID(ids, "user:a") || !hasID(ids, "user:bob") {
		t.Fatalf("plan participants should include creator + human assignee, got %v", ids)
	}
}

func TestSelectTaskIntoPlan_AgentAssignee_BecomesParticipant(t *testing.T) {
	// §9.5: an AGENT assignee MUST become a plan-conversation participant, else the
	// orchestrator's @mention can never wake it.
	svc, convRepo, _, _, relay, ctx := planSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "Sprint", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})
	agent := "agent:007"
	if err := svc.BatchUpdateTask(ctx, tid, BatchTaskPatch{Assignee: &agent}, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SelectTaskIntoPlan(ctx, planID, tid, "user:a"); err != nil {
		t.Fatalf("SelectTaskIntoPlan: %v", err)
	}
	drain(t, relay, ctx)

	conv, _ := convRepo.FindByOwnerRef(ctx, conversation.NewPlanOwnerRef(string(planID)))
	ids := participantIDs(conv)
	if !hasID(ids, "agent:007") {
		t.Fatalf("plan participants MUST include the agent assignee (§9.5 — mention only reaches members), got %v", ids)
	}
}

func TestSelectTaskIntoPlan_Additive_RemoveDoesNotDropParticipant(t *testing.T) {
	svc, convRepo, _, _, relay, ctx := planSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "Sprint", CreatedBy: "user:a"})

	taskA, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "A", CreatedBy: "user:a"})
	taskB, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "B", CreatedBy: "user:a"})
	x, y := "user:x", "user:y"
	if err := svc.BatchUpdateTask(ctx, taskA, BatchTaskPatch{Assignee: &x}, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.BatchUpdateTask(ctx, taskB, BatchTaskPatch{Assignee: &y}, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SelectTaskIntoPlan(ctx, planID, taskA, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SelectTaskIntoPlan(ctx, planID, taskB, "user:a"); err != nil {
		t.Fatal(err)
	}
	drain(t, relay, ctx)

	conv, _ := convRepo.FindByOwnerRef(ctx, conversation.NewPlanOwnerRef(string(planID)))
	ids := participantIDs(conv)
	if !hasID(ids, "user:x") || !hasID(ids, "user:y") {
		t.Fatalf("both assignees should be participants, got %v", ids)
	}

	// Removing task A does NOT drop X (additive §9.5 — participants are monotonic).
	if err := svc.RemoveTaskFromPlan(ctx, planID, taskA, "user:a"); err != nil {
		t.Fatalf("RemoveTaskFromPlan: %v", err)
	}
	drain(t, relay, ctx)
	conv2, _ := convRepo.FindByOwnerRef(ctx, conversation.NewPlanOwnerRef(string(planID)))
	ids2 := participantIDs(conv2)
	if !hasID(ids2, "user:x") {
		t.Fatalf("RemoveTaskFromPlan must NOT drop participant X (additive §9.5), got %v", ids2)
	}
	if !hasID(ids2, "user:y") {
		t.Fatalf("Y should remain too, got %v", ids2)
	}
}

func TestSelectTaskIntoPlan_Rejects(t *testing.T) {
	svc, _, plans, _, relay, ctx := planSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	otherPid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "Other", CreatedBy: "user:a"})

	planID, _ := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "Sprint", CreatedBy: "user:a"})
	drain(t, relay, ctx)

	// Different project → ErrPlanProjectMismatch.
	otherTask, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: otherPid, Title: "x", CreatedBy: "user:a"})
	if err := svc.SelectTaskIntoPlan(ctx, planID, otherTask, "user:a"); !errors.Is(err, pm.ErrPlanProjectMismatch) {
		t.Fatalf("cross-project select = %v, want ErrPlanProjectMismatch", err)
	}

	// Task already in ANOTHER plan → ErrTaskInOtherPlan.
	otherPlan, _ := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "Other Plan", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "t", CreatedBy: "user:a"})
	if err := svc.SelectTaskIntoPlan(ctx, otherPlan, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SelectTaskIntoPlan(ctx, planID, tid, "user:a"); !errors.Is(err, pm.ErrTaskInOtherPlan) {
		t.Fatalf("select task in other plan = %v, want ErrTaskInOtherPlan", err)
	}

	// Plan not draft → ErrPlanNotDraft. Start the plan first (needs a task selected
	// so... Start has no validation here; just transition via the AR through repo).
	p, _ := plans.FindByID(ctx, planID)
	if err := p.Start(svc.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := plans.Update(ctx, p); err != nil {
		t.Fatal(err)
	}
	freshTask, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "fresh", CreatedBy: "user:a"})
	if err := svc.SelectTaskIntoPlan(ctx, planID, freshTask, "user:a"); !errors.Is(err, pm.ErrPlanNotDraft) {
		t.Fatalf("select into running plan = %v, want ErrPlanNotDraft", err)
	}
}

// TestRemoveTaskFromPlan_RejectsNonDraft locks §9.4 add/remove symmetry: a task
// can be removed from the plan only in draft. Without the gate, the Stage C MCP
// remove_task tool (and the API) could pull a node out of a RUNNING plan, breaking
// the executing DAG / orphaning its dispatch record — an enforcement hole the FE
// gate can't cover (MCP/API bypass it).
func TestRemoveTaskFromPlan_RejectsNonDraft(t *testing.T) {
	svc, _, plans, _, _, ctx := planSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "Sprint", CreatedBy: "user:a"})
	taskA, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "A", CreatedBy: "user:a"})
	if err := svc.SelectTaskIntoPlan(ctx, planID, taskA, "user:a"); err != nil {
		t.Fatal(err)
	}
	// Transition the plan to running, then attempt the remove.
	p, _ := plans.FindByID(ctx, planID)
	if err := p.Start(svc.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := plans.Update(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := svc.RemoveTaskFromPlan(ctx, planID, taskA, "user:a"); !errors.Is(err, pm.ErrPlanNotDraft) {
		t.Fatalf("remove from running plan = %v, want ErrPlanNotDraft", err)
	}
}

// TestRemoveTaskFromPlan_BuiltinPool_OK locks T121: the built-in assignment pool
// is ALWAYS running, yet its task-set is freely editable (it is a flat claimable
// bucket, not an executing DAG). SelectTaskIntoPlan already exempts the pool from
// the draft-only gate; RemoveTaskFromPlan MUST mirror that exemption so a task can
// leave the pool (back to backlog, or as the remove-half of a Work Board move).
// Without it, dragging a pool task anywhere reports plan_conflict (the reported bug).
func TestRemoveTaskFromPlan_BuiltinPool_OK(t *testing.T) {
	svc, _, _, tasks, _, ctx := planSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	pool := svc.builtinPoolOf(ctx, pid)
	if pool == nil {
		t.Fatal("project should have a built-in pool")
	}
	if pool.Status() == pm.PlanDraft {
		t.Fatalf("built-in pool should be always-running, got status %v", pool.Status())
	}
	taskA, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "A", CreatedBy: "user:a"})
	// Selecting INTO the always-running pool is already allowed (the exemption).
	if err := svc.SelectTaskIntoPlan(ctx, pool.ID(), taskA, "user:a"); err != nil {
		t.Fatalf("SelectTaskIntoPlan(pool) = %v, want nil", err)
	}
	// Removing FROM the always-running pool must also succeed (the T121 symmetry).
	if err := svc.RemoveTaskFromPlan(ctx, pool.ID(), taskA, "user:a"); err != nil {
		t.Fatalf("RemoveTaskFromPlan(pool) = %v, want nil (pool task-set is editable)", err)
	}
	tk, _ := tasks.FindByID(ctx, taskA)
	if tk.PlanID() != "" {
		t.Fatalf("task plan after remove = %q, want empty (back to backlog)", tk.PlanID())
	}
}

func TestSelectTaskIntoPlan_SamePlan_NoOp(t *testing.T) {
	svc, _, _, tasks, relay, ctx := planSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "Sprint", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "t", CreatedBy: "user:a"})
	if err := svc.SelectTaskIntoPlan(ctx, planID, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	// Re-selecting into the SAME plan is a no-op (no error).
	if err := svc.SelectTaskIntoPlan(ctx, planID, tid, "user:a"); err != nil {
		t.Fatalf("re-select same plan = %v, want nil (no-op)", err)
	}
	drain(t, relay, ctx)
	tk, _ := tasks.FindByID(ctx, tid)
	if tk.PlanID() != planID {
		t.Fatalf("task plan = %q, want %q", tk.PlanID(), planID)
	}
}

func TestPlanParticipantSync_Idempotent(t *testing.T) {
	svc, convRepo, _, _, relay, ctx := planSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "Sprint", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "t", CreatedBy: "user:a"})
	human := "user:bob"
	if err := svc.BatchUpdateTask(ctx, tid, BatchTaskPatch{Assignee: &human}, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SelectTaskIntoPlan(ctx, planID, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	drain(t, relay, ctx)
	conv, _ := convRepo.FindByOwnerRef(ctx, conversation.NewPlanOwnerRef(string(planID)))
	before := len(participantIDs(conv))

	// Re-running the relay (replay) must change nothing: applied-store idempotency +
	// union no-op.
	if n, err := relay.RunOnce(ctx, 100); err != nil || n != 0 {
		t.Fatalf("replay RunOnce = %d, %v; want 0 (all processed)", n, err)
	}
	conv2, _ := convRepo.FindByOwnerRef(ctx, conversation.NewPlanOwnerRef(string(planID)))
	if got := len(participantIDs(conv2)); got != before {
		t.Fatalf("replay changed participant count: before %d, after %d", before, got)
	}
	if before != 2 { // {creator user:a, assignee user:bob}
		t.Fatalf("want 2 participants (creator + assignee), got %d", before)
	}
}

// TestAssignAfterSelect_SyncsPlanParticipant is the BUG B regression: a task is
// selected into a plan WITHOUT an assignee first, THEN AssignTask is called. The
// assign path must emit pm.plan.participants_changed so the new assignee joins the
// plan conversation — otherwise the dispatch @mention can never reach it. Before
// the fix only SelectTaskIntoPlan emitted it (and only when the task already had an
// assignee at select time), so the assign-after-select agent was left out.
func TestAssignAfterSelect_SyncsPlanParticipant(t *testing.T) {
	svc, convRepo, _, _, relay, ctx := planSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "Sprint", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})

	// Select FIRST, with NO assignee → no participant delta for the task yet.
	if err := svc.SelectTaskIntoPlan(ctx, planID, tid, "user:a"); err != nil {
		t.Fatalf("SelectTaskIntoPlan: %v", err)
	}
	drain(t, relay, ctx)
	conv, _ := convRepo.FindByOwnerRef(ctx, conversation.NewPlanOwnerRef(string(planID)))
	if ids := participantIDs(conv); hasID(ids, "user:bob") {
		t.Fatalf("assignee must not be a participant before assignment, got %v", ids)
	}

	// Assign AFTER select → must emit participants_changed → assignee joins the plan conv.
	if err := svc.AssignTask(ctx, tid, "user:bob", "user:a"); err != nil {
		t.Fatalf("AssignTask: %v", err)
	}
	drain(t, relay, ctx)

	conv, _ = convRepo.FindByOwnerRef(ctx, conversation.NewPlanOwnerRef(string(planID)))
	ids := participantIDs(conv)
	if !hasID(ids, "user:bob") {
		t.Fatalf("assign-after-select must add the assignee to the plan conversation, got %v", ids)
	}
}

// TestAssignTask_NoPlan_DoesNotEmitParticipantSync confirms the BUG B emit is
// scoped to plan tasks: assigning a task NOT in any plan must not error / must be
// a no-op for the plan participant sync (PlanID == "" → nothing emitted).
func TestAssignTask_NoPlan_DoesNotEmitParticipantSync(t *testing.T) {
	svc, _, _, _, _, ctx := planSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "loose", CreatedBy: "user:a"})
	if err := svc.AssignTask(ctx, tid, "user:bob", "user:a"); err != nil {
		t.Fatalf("AssignTask on a plan-less task should succeed: %v", err)
	}
}
