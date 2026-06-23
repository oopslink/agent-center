package service

import (
	"context"
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

// planRemovalHarness wires the producer Service + a relay whose plan projector has
// the conversation cascade (so EvtPlanDeleted hard-deletes / EvtPlanArchived
// archives the plan conversation) — the surface DeletePlan/ArchivePlan exercise.
type planRemovalHarness struct {
	svc      *Service
	plans    *pmsql.PlanRepo
	tasks    *pmsql.TaskRepo
	convRepo *convsql.ConversationRepo
	relay    *outbox.Relay
	ctx      context.Context
}

func planRemovalSetup(t *testing.T) *planRemovalHarness {
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
	readState := convsql.NewReadStateRepo(db)
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
		PlanDispatcher: convservice.NewPlanDispatchAdapter(writer, planTestDisplayName),
	})
	taskProj := NewParticipantProjector(db, convRepo, applied, gen, clk)
	planProj := NewPlanParticipantProjector(db, convRepo, plans, applied, gen, clk).
		WithConversationCascade(msgRepo, readState)
	relay := outbox.NewRelay(ob, applied, clk, taskProj, planProj)
	return &planRemovalHarness{svc: svc, plans: plans, tasks: tasks, convRepo: convRepo, relay: relay, ctx: context.Background()}
}

func (h *planRemovalHarness) drain(t *testing.T) {
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

// seedTaskInPlan creates a task, optionally assigns it, and selects it into plan.
func (h *planRemovalHarness) seedTaskInPlan(t *testing.T, pid pm.ProjectID, planID pm.PlanID, title, assignee string) pm.TaskID {
	t.Helper()
	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: title, CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if assignee != "" {
		a := assignee
		if err := h.svc.BatchUpdateTask(h.ctx, tid, BatchTaskPatch{Assignee: &a}, "user:a"); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.svc.SelectTaskIntoPlan(h.ctx, planID, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	return tid
}

// --- DeletePlan -------------------------------------------------------------

// TestDeletePlan_NonRunning_UnloadsTasksAndDeletesPlanAndDeps: a draft plan with
// tasks + a dependency edge → DeletePlan unloads the tasks to backlog (plan_id="")
// and removes the plan + deps. The plan's conversation is hard-deleted.
func TestDeletePlan_NonRunning_UnloadsTasksAndDeletesPlanAndDeps(t *testing.T) {
	h := planRemovalSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "alpha", CreatedBy: "user:a"})
	h.drain(t)

	a := h.seedTaskInPlan(t, pid, planID, "a", "user:x")
	b := h.seedTaskInPlan(t, pid, planID, "b", "user:y")
	if err := h.svc.AddPlanDependency(h.ctx, planID, b, a, "user:a"); err != nil {
		t.Fatal(err)
	}

	// Plan conversation exists before delete.
	if _, err := h.convRepo.FindByOwnerRef(h.ctx, conversation.NewPlanOwnerRef(string(planID))); err != nil {
		t.Fatalf("plan conversation should exist pre-delete: %v", err)
	}

	if err := h.svc.DeletePlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("DeletePlan: %v", err)
	}
	h.drain(t)

	// Plan row gone.
	if _, err := h.plans.FindByID(h.ctx, planID); err != pm.ErrPlanNotFound {
		t.Fatalf("plan FindByID = %v want ErrPlanNotFound", err)
	}
	// Tasks back in the backlog (plan_id="").
	for _, id := range []pm.TaskID{a, b} {
		tk, err := h.tasks.FindByID(h.ctx, id)
		if err != nil {
			t.Fatalf("task %s should still exist (unloaded, not deleted): %v", id, err)
		}
		if tk.PlanID() != "" {
			t.Fatalf("task %s plan_id=%q want \"\" (backlog)", id, tk.PlanID())
		}
	}
	// Dependencies gone.
	edges, err := h.plans.ListDependencies(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 0 {
		t.Fatalf("deps after delete = %d want 0", len(edges))
	}
	// Conversation hard-deleted.
	if _, err := h.convRepo.FindByOwnerRef(h.ctx, conversation.NewPlanOwnerRef(string(planID))); err != conversation.ErrConversationNotFound {
		t.Fatalf("plan conversation FindByOwnerRef = %v want ErrConversationNotFound", err)
	}
}

// TestDeletePlan_Running_Rejected: a running plan cannot be deleted (ErrPlanRunning),
// and nothing is mutated.
func TestDeletePlan_Running_Rejected(t *testing.T) {
	h := planRemovalSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "alpha", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedTaskInPlan(t, pid, planID, "a", "agent:bot")
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	h.drain(t)

	if err := h.svc.DeletePlan(h.ctx, planID, "user:a"); err != pm.ErrPlanRunning {
		t.Fatalf("DeletePlan(running) = %v want ErrPlanRunning", err)
	}
	// Plan still present + task still in plan.
	if _, err := h.plans.FindByID(h.ctx, planID); err != nil {
		t.Fatalf("plan must survive a rejected delete: %v", err)
	}
	tk, _ := h.tasks.FindByID(h.ctx, a)
	if tk.PlanID() != planID {
		t.Fatalf("task plan_id=%q want %q (unchanged)", tk.PlanID(), planID)
	}
}

// TestDiscardTask_ArchivedOpen_EscapeHatch (T339-D): discard_task is the operator
// tool to conclude a LEAKED open+archived dead task. Such a task rejects every normal
// mutator (incl. Discard) with ErrTaskArchived; DiscardTask routes it through
// FinalizeArchived so it can finally be concluded to discarded (staying archived).
func TestDiscardTask_ArchivedOpen_EscapeHatch(t *testing.T) {
	h := planRemovalSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "alpha", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedTaskInPlan(t, pid, planID, "a", "user:x")

	// Forge the dead state directly: archive the task while it is still OPEN (the
	// pre-T339 leak that ArchivePlan no longer produces, but legacy rows / other paths
	// might). Persist it as archived+open.
	tk, err := h.tasks.FindByID(h.ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1_700_000_500, 0).UTC()
	if err := tk.Archive(at, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.tasks.Update(h.ctx, tk); err != nil {
		t.Fatal(err)
	}

	// The escape hatch: discard_task concludes it (Discard() alone would reject it).
	if err := h.svc.DiscardTask(h.ctx, a, "user:a"); err != nil {
		t.Fatalf("DiscardTask(archived open) escape hatch: %v", err)
	}
	got, err := h.tasks.FindByID(h.ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status() != pm.TaskDiscarded {
		t.Fatalf("status = %q want discarded (escape hatch concluded it)", got.Status())
	}
	if !got.IsArchived() {
		t.Fatal("task must stay archived (concluded, not un-archived)")
	}
}

// --- ArchivePlan ------------------------------------------------------------

// TestArchivePlan_NonRunning_CascadeArchivesTasks: archiving a plan archives the
// plan AND all its tasks. A TERMINAL task keeps its real outcome (a=completed stays
// completed); a NON-TERMINAL task is FINALIZED to discarded (T339 — b=open→discarded)
// so the cascade never leaves an open+archived task leaking into the board /
// list_tasks(open).
func TestArchivePlan_NonRunning_CascadeArchivesTasks(t *testing.T) {
	h := planRemovalSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "alpha", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedTaskInPlan(t, pid, planID, "a", "user:x")
	b := h.seedTaskInPlan(t, pid, planID, "b", "user:y")
	// Drive task a to a TERMINAL status (completed) so we can assert archive preserves
	// a finished node's real outcome. Task b is left OPEN — the escape/skipped-node
	// case T339 must finalize to discarded. (v2.9 #299: a running task would block
	// archive — see TestArchivePlan_RunningTask_Rejected.)
	if err := h.svc.SetTaskStatus(h.ctx, a, pm.TaskCompleted, "user:a"); err != nil {
		t.Fatal(err)
	}

	if err := h.svc.ArchivePlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("ArchivePlan: %v", err)
	}
	h.drain(t)

	p, err := h.plans.FindByID(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status() != pm.PlanArchived {
		t.Fatalf("plan status = %q want archived", p.Status())
	}
	// Both archived; terminal preserved (a=completed), non-terminal finalized (b→discarded).
	wantStatus := map[pm.TaskID]pm.TaskStatus{a: pm.TaskCompleted, b: pm.TaskDiscarded}
	for id, want := range wantStatus {
		tk, err := h.tasks.FindByID(h.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if !tk.IsArchived() {
			t.Fatalf("task %s must be archived (cascade)", id)
		}
		if tk.ArchivedBy() != "user:a" {
			t.Fatalf("task %s archived_by=%q want user:a", id, tk.ArchivedBy())
		}
		if tk.Status() != want {
			t.Fatalf("task %s status=%q want %q (terminal preserved / non-terminal finalized)", id, tk.Status(), want)
		}
	}
	// Plan conversation archived.
	conv, err := h.convRepo.FindByOwnerRef(h.ctx, conversation.NewPlanOwnerRef(string(planID)))
	if err != nil {
		t.Fatalf("plan conversation should still exist (archived, not deleted): %v", err)
	}
	if conv.Status() != conversation.ConversationArchived {
		t.Fatalf("plan conversation status = %q want archived", conv.Status())
	}
}

// TestArchivePlan_Running_Rejected: a running plan cannot be archived.
func TestArchivePlan_Running_Rejected(t *testing.T) {
	h := planRemovalSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "alpha", CreatedBy: "user:a"})
	h.drain(t)
	h.seedTaskInPlan(t, pid, planID, "a", "agent:bot")
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)

	if err := h.svc.ArchivePlan(h.ctx, planID, "user:a"); err != pm.ErrPlanRunning {
		t.Fatalf("ArchivePlan(running) = %v want ErrPlanRunning", err)
	}
}

// TestArchivePlan_RunningTask_Rejected: v2.9 #299 (@oopslink) — a (draft/stopped)
// plan with ANY member task in the running state can't be archived (archiving would
// orphan the in-flight task); completing/stopping the task unblocks archive.
func TestArchivePlan_RunningTask_Rejected(t *testing.T) {
	h := planRemovalSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "alpha", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedTaskInPlan(t, pid, planID, "a", "user:x")
	// Drive the member task to running while the plan stays draft (the in-flight case).
	if err := h.svc.SetTaskStatus(h.ctx, a, pm.TaskRunning, "user:a"); err != nil {
		t.Fatal(err)
	}

	// Archive rejected while the member task is running.
	if err := h.svc.ArchivePlan(h.ctx, planID, "user:a"); err != pm.ErrPlanHasRunningTasks {
		t.Fatalf("ArchivePlan(running task) = %v want ErrPlanHasRunningTasks", err)
	}

	// Discard the running task → no longer running → archive now succeeds.
	if err := h.svc.SetTaskStatus(h.ctx, a, pm.TaskDiscarded, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.ArchivePlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("ArchivePlan after discarding the running task: %v", err)
	}
}

// TestArchivePlan_DoubleRejected: re-archiving an archived plan → ErrPlanArchived.
func TestArchivePlan_DoubleRejected(t *testing.T) {
	h := planRemovalSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "alpha", CreatedBy: "user:a"})
	h.drain(t)
	if err := h.svc.ArchivePlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	if err := h.svc.ArchivePlan(h.ctx, planID, "user:a"); err != pm.ErrPlanArchived {
		t.Fatalf("re-archive = %v want ErrPlanArchived", err)
	}
}
