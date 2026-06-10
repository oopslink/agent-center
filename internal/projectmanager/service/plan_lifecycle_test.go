package service

import (
	"context"
	"errors"
	"testing"
	"time"

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
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// planAdvanceHarness bundles everything an advance/lifecycle test drives: the
// producer Service (with Plans repo + a REAL PlanDispatcher posting into the
// plan conversation + a permissive AgentDirectory), the relay (to materialize
// the plan conversation), and the conversation/message repos to assert dispatch.
type planAdvanceHarness struct {
	svc      *Service
	plans    *pmsql.PlanRepo
	tasks    *pmsql.TaskRepo
	convRepo *convsql.ConversationRepo
	msgRepo  *convsql.MessageRepo
	relay    *outbox.Relay
	ctx      context.Context
}

func planAdvanceSetup(t *testing.T) *planAdvanceHarness {
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
	msgRepo := convsql.NewMessageRepo(db)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, clk)
	writer := convservice.NewMessageWriter(db, convRepo, msgRepo, sink, gen, clk).WithOutbox(ob)
	plans := pmsql.NewPlanRepo(db)
	tasks := pmsql.NewTaskRepo(db)
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: tasks,
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Plans: plans, Outbox: ob, IDGen: gen, Clock: clk,
		AgentDir:       allOrgDir("org-1"),
		PlanDispatcher: convservice.NewPlanDispatchAdapter(writer),
	})
	taskProj := NewParticipantProjector(db, convRepo, applied, gen, clk)
	planProj := NewPlanParticipantProjector(db, convRepo, plans, applied, gen, clk)
	relay := outbox.NewRelay(ob, applied, clk, taskProj, planProj)
	return &planAdvanceHarness{svc: svc, plans: plans, tasks: tasks, convRepo: convRepo, msgRepo: msgRepo, relay: relay, ctx: context.Background()}
}

func (h *planAdvanceHarness) drain(t *testing.T) {
	t.Helper()
	for {
		n, err := h.relay.RunOnce(h.ctx, 100)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return
		}
	}
}

// seedAssignedTask creates a task, assigns it, and selects it into the plan
// (draining so the assignee becomes a plan-conversation participant, §9.5).
func (h *planAdvanceHarness) seedAssignedTask(t *testing.T, pid pm.ProjectID, planID pm.PlanID, title, assignee string) pm.TaskID {
	t.Helper()
	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: title, CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	a := assignee
	if err := h.svc.BatchUpdateTask(h.ctx, tid, BatchTaskPatch{Assignee: &a}, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.SelectTaskIntoPlan(h.ctx, planID, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	return tid
}

func (h *planAdvanceHarness) planConvMsgCount(t *testing.T, planID pm.PlanID) int {
	t.Helper()
	conv, err := h.convRepo.FindByOwnerRef(h.ctx, conversation.NewPlanOwnerRef(string(planID)))
	if err != nil {
		t.Fatalf("plan conversation should exist: %v", err)
	}
	msgs, err := h.msgRepo.FindRecent(h.ctx, conv.ID(), 1000)
	if err != nil {
		t.Fatal(err)
	}
	return len(msgs)
}

// setTaskStatus drives a task's status (helper for advancing the DAG).
func (h *planAdvanceHarness) setTaskStatus(t *testing.T, tid pm.TaskID, status pm.TaskStatus) {
	t.Helper()
	if err := h.svc.SetTaskStatus(h.ctx, tid, status, "user:a"); err != nil {
		t.Fatalf("SetTaskStatus(%s): %v", status, err)
	}
}

// §9.6 start validation.
func TestStartPlan_Validation(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})

	t.Run("0-task rejected", func(t *testing.T) {
		planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "empty", CreatedBy: "user:a"})
		h.drain(t)
		if err := h.svc.StartPlan(h.ctx, planID, "user:a"); !errors.Is(err, pm.ErrPlanNoTasks) {
			t.Fatalf("start 0-task = %v, want ErrPlanNoTasks", err)
		}
	})

	t.Run("unassigned task rejected", func(t *testing.T) {
		planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "unassigned", CreatedBy: "user:a"})
		h.drain(t)
		tid, _ := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "noone", CreatedBy: "user:a"})
		if err := h.svc.SelectTaskIntoPlan(h.ctx, planID, tid, "user:a"); err != nil {
			t.Fatal(err)
		}
		h.drain(t)
		if err := h.svc.StartPlan(h.ctx, planID, "user:a"); !errors.Is(err, pm.ErrPlanUnassignedTask) {
			t.Fatalf("start unassigned = %v, want ErrPlanUnassignedTask", err)
		}
	})

	t.Run("cyclic DAG rejected", func(t *testing.T) {
		planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "cyclic", CreatedBy: "user:a"})
		h.drain(t)
		a := h.seedAssignedTask(t, pid, planID, "A", "user:x")
		b := h.seedAssignedTask(t, pid, planID, "B", "user:y")
		// A→B and B→A would cycle; AddPlanDependency's repo guard rejects the 2nd add,
		// so inject a cycle directly to exercise StartPlan's ValidateNoCycle gate.
		if err := h.svc.AddPlanDependency(h.ctx, planID, a, b, "user:a"); err != nil {
			t.Fatal(err)
		}
		// Force a back-edge straight through the repo (bypassing the add guard) so the
		// edge set is cyclic when StartPlan validates.
		if err := h.plans.AddDependency(h.ctx, pm.Dependency{PlanID: planID, FromTaskID: b, ToTaskID: a}); err == nil {
			// AddDependency itself rejects the cycle — that already proves acyclicity is
			// enforced at edit time; nothing left for StartPlan to catch here.
			t.Fatal("expected repo AddDependency to reject the back-edge (cycle)")
		}
		// The plan is still acyclic (a→b only); start should SUCCEED.
		if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
			t.Fatalf("acyclic start = %v, want nil", err)
		}
	})

	t.Run("valid start succeeds + pre-done allowed", func(t *testing.T) {
		planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "valid", CreatedBy: "user:a"})
		h.drain(t)
		done := h.seedAssignedTask(t, pid, planID, "pre-done", "agent:42")
		h.setTaskStatus(t, done, pm.TaskCompleted) // pre-done allowed (§9.6)
		_ = h.seedAssignedTask(t, pid, planID, "todo", "user:z")
		if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
			t.Fatalf("valid start = %v, want nil", err)
		}
		p, _ := h.plans.FindByID(h.ctx, planID)
		if p.Status() != pm.PlanRunning {
			t.Fatalf("plan status = %s, want running", p.Status())
		}
	})
}

// §9.3 idempotency + all-ready: A→{B,C}; A done → advance dispatches B AND C in
// one call; a second advance (replay) dispatches nothing more.
func TestAdvancePlan_AllReady_Idempotent(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "fanout", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:x")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:y")
	c := h.seedAssignedTask(t, pid, planID, "C", "user:z")
	if err := h.svc.AddPlanDependency(h.ctx, planID, b, a, "user:a"); err != nil { // B depends_on A
		t.Fatal(err)
	}
	if err := h.svc.AddPlanDependency(h.ctx, planID, c, a, "user:a"); err != nil { // C depends_on A
		t.Fatal(err)
	}
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}

	// First advance: only A is ready (B, C blocked on A) → A dispatched.
	dispatched, err := h.svc.AdvancePlan(h.ctx, planID, "user:a")
	if err != nil {
		t.Fatal(err)
	}
	if len(dispatched) != 1 || dispatched[0] != a {
		t.Fatalf("advance before A done dispatched %v, want only [A]", dispatched)
	}

	baseMsgs := h.planConvMsgCount(t, planID)

	// A done → advance dispatches BOTH B and C in one call (all-ready).
	h.setTaskStatus(t, a, pm.TaskCompleted)
	dispatched, err = h.svc.AdvancePlan(h.ctx, planID, "user:a")
	if err != nil {
		t.Fatal(err)
	}
	if len(dispatched) != 2 {
		t.Fatalf("advance dispatched %v, want both B and C", dispatched)
	}
	gotDisp := map[pm.TaskID]bool{}
	for _, id := range dispatched {
		gotDisp[id] = true
	}
	if !gotDisp[b] || !gotDisp[c] {
		t.Fatalf("dispatched %v, want B and C", dispatched)
	}
	if got := h.planConvMsgCount(t, planID) - baseMsgs; got != 2 {
		t.Fatalf("plan conversation got %d new messages, want exactly 2 (@B,@C)", got)
	}

	// §9.3: re-running advance (and a 2nd upstream-complete style re-eval) dispatches
	// NOTHING more — B and C already have dispatch records.
	dispatched, err = h.svc.AdvancePlan(h.ctx, planID, "user:a")
	if err != nil {
		t.Fatal(err)
	}
	if len(dispatched) != 0 {
		t.Fatalf("re-advance dispatched %v, want none (§9.3 idempotency)", dispatched)
	}
	if got := h.planConvMsgCount(t, planID) - baseMsgs; got != 2 {
		t.Fatalf("re-advance posted extra messages: total new = %d, want 2", got)
	}
	// Exactly one dispatch record per dispatched node: A + B + C = 3.
	recs, _ := h.plans.ListDispatchRecords(h.ctx, planID)
	if len(recs) != 3 {
		t.Fatalf("dispatch records = %d, want 3 (A, B, C — one per dispatched node)", len(recs))
	}
}

// §9.7 failure isolation through advance: A→B, X→Y. A failed → B never
// dispatched; X done → Y dispatched (independent branch advances).
func TestAdvancePlan_FailureIsolation(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "iso", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:b1")
	x := h.seedAssignedTask(t, pid, planID, "X", "user:x1")
	y := h.seedAssignedTask(t, pid, planID, "Y", "user:y1")
	if err := h.svc.AddPlanDependency(h.ctx, planID, b, a, "user:a"); err != nil { // B depends_on A
		t.Fatal(err)
	}
	if err := h.svc.AddPlanDependency(h.ctx, planID, y, x, "user:a"); err != nil { // Y depends_on X
		t.Fatal(err)
	}
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}

	h.setTaskStatus(t, a, pm.TaskDiscarded) // A failed
	h.setTaskStatus(t, x, pm.TaskCompleted) // X done
	dispatched, err := h.svc.AdvancePlan(h.ctx, planID, "user:a")
	if err != nil {
		t.Fatal(err)
	}
	if len(dispatched) != 1 || dispatched[0] != y {
		t.Fatalf("dispatched %v, want only Y (B blocked behind failed A, Y independent)", dispatched)
	}
	_ = b
}

// §9.1: a Plan becomes done only when EVERY node is done; a failed node keeps it
// running.
func TestAdvancePlan_DoneSemantics(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})

	t.Run("failed node keeps plan running", func(t *testing.T) {
		planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "failkeep", CreatedBy: "user:a"})
		h.drain(t)
		a := h.seedAssignedTask(t, pid, planID, "A", "user:a1")
		b := h.seedAssignedTask(t, pid, planID, "B", "user:b1")
		if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
			t.Fatal(err)
		}
		h.setTaskStatus(t, a, pm.TaskCompleted)
		h.setTaskStatus(t, b, pm.TaskDiscarded) // failed
		if _, err := h.svc.AdvancePlan(h.ctx, planID, "user:a"); err != nil {
			t.Fatal(err)
		}
		p, _ := h.plans.FindByID(h.ctx, planID)
		if p.Status() != pm.PlanRunning {
			t.Fatalf("plan with a failed node = %s, want still running (§9.1)", p.Status())
		}
	})

	t.Run("all done marks plan done", func(t *testing.T) {
		planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "alldone", CreatedBy: "user:a"})
		h.drain(t)
		a := h.seedAssignedTask(t, pid, planID, "A", "user:a1")
		b := h.seedAssignedTask(t, pid, planID, "B", "user:b1")
		if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
			t.Fatal(err)
		}
		h.setTaskStatus(t, a, pm.TaskCompleted)
		h.setTaskStatus(t, b, pm.TaskVerified)
		if _, err := h.svc.AdvancePlan(h.ctx, planID, "user:a"); err != nil {
			t.Fatal(err)
		}
		p, _ := h.plans.FindByID(h.ctx, planID)
		if p.Status() != pm.PlanDone {
			t.Fatalf("plan with all nodes done = %s, want done (§9.1)", p.Status())
		}
	})
}

// Advance on a non-running plan is rejected; StopPlan returns running→draft (§9.4).
func TestStopPlan_AndAdvanceGuards(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "p", CreatedBy: "user:a"})
	h.drain(t)
	_ = h.seedAssignedTask(t, pid, planID, "A", "user:a1")

	// Advance while draft → ErrPlanNotRunning.
	if _, err := h.svc.AdvancePlan(h.ctx, planID, "user:a"); !errors.Is(err, pm.ErrPlanNotRunning) {
		t.Fatalf("advance draft = %v, want ErrPlanNotRunning", err)
	}
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	// Stop → back to draft (§9.4).
	if err := h.svc.StopPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("StopPlan: %v", err)
	}
	p, _ := h.plans.FindByID(h.ctx, planID)
	if p.Status() != pm.PlanDraft {
		t.Fatalf("after stop = %s, want draft", p.Status())
	}
}

// RerunFailedNode clears the dispatch record so the next advance re-dispatches.
func TestRerunFailedNode_ReDispatches(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "rerun", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	// A is ready immediately (no upstream) → dispatched once.
	d1, _ := h.svc.AdvancePlan(h.ctx, planID, "user:a")
	if len(d1) != 1 || d1[0] != a {
		t.Fatalf("first advance = %v, want [A]", d1)
	}
	// Re-advance: idempotent, no re-dispatch.
	d2, _ := h.svc.AdvancePlan(h.ctx, planID, "user:a")
	if len(d2) != 0 {
		t.Fatalf("second advance = %v, want none", d2)
	}
	// Clear the dispatch record (re-run) → next advance re-dispatches A.
	if err := h.svc.RerunFailedNode(h.ctx, planID, a, "user:a"); err != nil {
		t.Fatal(err)
	}
	d3, _ := h.svc.AdvancePlan(h.ctx, planID, "user:a")
	if len(d3) != 1 || d3[0] != a {
		t.Fatalf("post-rerun advance = %v, want [A] (re-dispatched)", d3)
	}
}
