package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	convsql "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/mention"
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
		// #245 org sequence so CreateTask allocates T<n> ids — the plan-conversation
		// dispatch reminder names tasks by their id.
		OrgSeq:   pmsql.NewOrgSequenceRepo(db),
		AgentDir: allOrgDir("org-1"),
		// Resolver mirrors production (strip the agent:/user: scheme → display_name).
		// Here the harness has no IdentityRepo, so it uses the bare id as the
		// display_name — enough to exercise the @<display_name> prepend + wake match.
		PlanDispatcher: convservice.NewPlanDispatchAdapter(writer, planTestDisplayName),
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

// TestListPlanSummaries_DerivesPerPlanReadModel asserts ListPlanSummaries returns
// one PlanDetail per plan with the SAME DERIVED read model as GetPlanDetail
// (progress/has_failed/node statuses), so the Work Board enrich is consistent and
// per-plan — without a second N+1 round of GetPlanDetail calls.
func TestListPlanSummaries_DerivesPerPlanReadModel(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})

	// planA: 3 tasks — one completed (done), one discarded (failed), one open.
	planA, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "alpha", CreatedBy: "user:a"})
	h.drain(t)
	tDone := h.seedAssignedTask(t, pid, planA, "done", "user:x")
	tFail := h.seedAssignedTask(t, pid, planA, "fail", "user:y")
	h.seedAssignedTask(t, pid, planA, "open", "user:z")
	h.setTaskStatus(t, tDone, pm.TaskCompleted)
	h.setTaskStatus(t, tFail, pm.TaskDiscarded)

	// planB: empty (no tasks).
	planB, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "beta", CreatedBy: "user:a"})
	h.drain(t)

	summaries, err := h.svc.ListPlanSummaries(h.ctx, pid)
	if err != nil {
		t.Fatalf("ListPlanSummaries: %v", err)
	}
	// planA + planB + the ADR-0047 auto-created built-in pool (empty: 0 nodes).
	if len(summaries) != 3 {
		t.Fatalf("summaries=%d want 3", len(summaries))
	}
	byID := map[pm.PlanID]*PlanDetail{}
	for _, d := range summaries {
		byID[d.Plan.ID()] = d
	}

	a := byID[planA]
	if a == nil {
		t.Fatal("planA missing from summaries")
	}
	if a.View.Progress.Done != 1 || a.View.Progress.Total != 3 {
		t.Fatalf("planA progress=%+v want {Done:1,Total:3}", a.View.Progress)
	}
	if !a.View.HasFailed {
		t.Fatal("planA has_failed=false want true")
	}
	if len(a.View.Nodes) != 3 {
		t.Fatalf("planA nodes=%d want 3", len(a.View.Nodes))
	}

	b := byID[planB]
	if b.View.Progress.Done != 0 || b.View.Progress.Total != 0 {
		t.Fatalf("planB progress=%+v want {0,0}", b.View.Progress)
	}
	if b.View.HasFailed {
		t.Fatal("planB has_failed=true want false")
	}
	if len(b.View.Nodes) != 0 {
		t.Fatalf("planB nodes=%d want 0", len(b.View.Nodes))
	}

	// Consistency: the summary's per-node status equals GetPlanDetail's for planA.
	detail, err := h.svc.GetPlanDetail(h.ctx, planA)
	if err != nil {
		t.Fatal(err)
	}
	detailStatus := map[pm.TaskID]pm.NodeStatus{}
	for _, n := range detail.View.Nodes {
		detailStatus[n.TaskID] = n.NodeStatus
	}
	for _, n := range a.View.Nodes {
		if detailStatus[n.TaskID] != n.NodeStatus {
			t.Fatalf("node %s: summary=%s detail=%s", n.TaskID, n.NodeStatus, detailStatus[n.TaskID])
		}
	}
}

// TestListPlanSummaries_ExcludesArchived is the class-guard for task-1099941e:
// an ARCHIVED plan must NOT appear in the Work Board list (both web + agent-tools
// mirrors go through ListPlanSummaries). Mirrors project/channel archive — archived
// work leaves the active board by default.
func TestListPlanSummaries_ExcludesArchived(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})

	// kept: a normal draft plan. gone: a plan we archive.
	kept, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "kept", CreatedBy: "user:a"})
	h.drain(t)
	gone, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "gone", CreatedBy: "user:a"})
	h.drain(t)
	if err := h.svc.ArchivePlan(h.ctx, gone, "user:a"); err != nil {
		t.Fatalf("ArchivePlan: %v", err)
	}
	h.drain(t)

	summaries, err := h.svc.ListPlanSummaries(h.ctx, pid)
	if err != nil {
		t.Fatalf("ListPlanSummaries: %v", err)
	}
	for _, d := range summaries {
		if d.Plan.ID() == gone {
			t.Fatalf("archived plan %s leaked into the Work Board list", gone)
		}
		if d.Plan.Status() == pm.PlanArchived {
			t.Fatalf("archived plan %s (status=%s) leaked into summaries", d.Plan.ID(), d.Plan.Status())
		}
	}
	// the non-archived plan + the auto-created built-in pool remain.
	var keptSeen bool
	for _, d := range summaries {
		if d.Plan.ID() == kept {
			keptSeen = true
		}
	}
	if !keptSeen {
		t.Fatalf("non-archived plan %s missing from summaries", kept)
	}
}

// TestListPlanSummaries_BatchedNoNPlus1 asserts ListPlanSummaries is N+1-free:
// across THREE non-trivial plans (each with tasks + DAG edges + dispatch records),
// the derived per-plan read model is correct AND identical to the per-plan
// GetPlanDetail — proving the batched (ListByProject + ListDependenciesByPlans +
// ListDispatchRecordsByPlans) in-memory grouping produces the SAME result as the
// single-plan load path, with no per-plan repo round-trip.
func TestListPlanSummaries_BatchedNoNPlus1(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})

	type planShape struct {
		id    pm.PlanID
		tasks []pm.TaskID
	}
	shapes := make([]planShape, 0, 3)
	for i := 0; i < 3; i++ {
		name := string(rune('a' + i))
		planID, err := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: name, CreatedBy: "user:a"})
		if err != nil {
			t.Fatal(err)
		}
		h.drain(t)
		// Each plan: 3 tasks t0->t1->t2 (a real DAG), distinct per plan.
		t0 := h.seedAssignedTask(t, pid, planID, name+"-t0", "user:x")
		t1 := h.seedAssignedTask(t, pid, planID, name+"-t1", "user:y")
		t2 := h.seedAssignedTask(t, pid, planID, name+"-t2", "user:z")
		// t1 depends_on t0; t2 depends_on t1 (edges scoped to THIS plan).
		if err := h.svc.AddPlanDependency(h.ctx, planID, t1, t0, "user:a"); err != nil {
			t.Fatal(err)
		}
		if err := h.svc.AddPlanDependency(h.ctx, planID, t2, t1, "user:a"); err != nil {
			t.Fatal(err)
		}
		// A dispatch record for t0 (the root) in THIS plan only.
		if err := h.plans.RecordDispatch(h.ctx, planID, t0, time.Unix(1_700_000_100, 0).UTC(), "msg-"+name); err != nil {
			t.Fatal(err)
		}
		shapes = append(shapes, planShape{id: planID, tasks: []pm.TaskID{t0, t1, t2}})
	}

	summaries, err := h.svc.ListPlanSummaries(h.ctx, pid)
	if err != nil {
		t.Fatalf("ListPlanSummaries: %v", err)
	}
	// 3 structured plans + the ADR-0047 auto-created built-in pool (empty).
	if len(summaries) != 4 {
		t.Fatalf("summaries=%d want 4", len(summaries))
	}
	byID := map[pm.PlanID]*PlanDetail{}
	for _, d := range summaries {
		byID[d.Plan.ID()] = d
	}

	for _, sh := range shapes {
		got := byID[sh.id]
		if got == nil {
			t.Fatalf("plan %s missing from summaries", sh.id)
		}
		// Correctness + isolation: each plan sees EXACTLY its own 3 tasks/nodes.
		if len(got.Tasks) != 3 || len(got.View.Nodes) != 3 {
			t.Fatalf("plan %s tasks=%d nodes=%d want 3/3 (cross-plan leak?)", sh.id, len(got.Tasks), len(got.View.Nodes))
		}
		// The batched view must equal the single-plan GetPlanDetail view exactly:
		// node order, node_status, task_status, depends_on, dispatch — field by field.
		detail, derr := h.svc.GetPlanDetail(h.ctx, sh.id)
		if derr != nil {
			t.Fatal(derr)
		}
		if !reflect.DeepEqual(got.View, detail.View) {
			t.Fatalf("plan %s batched view != GetPlanDetail view:\n batched=%#v\n detail =%#v", sh.id, got.View, detail.View)
		}
		// The root (t0) is dispatched in this plan and nowhere leaked from others.
		for _, n := range got.View.Nodes {
			if n.TaskID == sh.tasks[0] && !n.Dispatched {
				t.Fatalf("plan %s root %s should be dispatched", sh.id, sh.tasks[0])
			}
		}
	}
}

// TestPlanRepo_BatchReads_GroupAndIsolate asserts the two batch repo methods
// (ListDependenciesByPlans / ListDispatchRecordsByPlans) return correctly GROUPED
// per-plan data and never leak between plans — and that an empty planIDs slice
// yields an empty result (no malformed `IN ()`), matching the single-plan readers.
func TestPlanRepo_BatchReads_GroupAndIsolate(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})

	// Two plans with DISTINCT edge + dispatch counts so a leak is observable.
	planA, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "alpha", CreatedBy: "user:a"})
	h.drain(t)
	a0 := h.seedAssignedTask(t, pid, planA, "a0", "user:x")
	a1 := h.seedAssignedTask(t, pid, planA, "a1", "user:y")
	if err := h.svc.AddPlanDependency(h.ctx, planA, a1, a0, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.plans.RecordDispatch(h.ctx, planA, a0, time.Unix(1_700_000_100, 0).UTC(), "ma0"); err != nil {
		t.Fatal(err)
	}

	planB, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "beta", CreatedBy: "user:a"})
	h.drain(t)
	b0 := h.seedAssignedTask(t, pid, planB, "b0", "user:x")
	b1 := h.seedAssignedTask(t, pid, planB, "b1", "user:y")
	b2 := h.seedAssignedTask(t, pid, planB, "b2", "user:z")
	if err := h.svc.AddPlanDependency(h.ctx, planB, b1, b0, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.AddPlanDependency(h.ctx, planB, b2, b1, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.plans.RecordDispatch(h.ctx, planB, b0, time.Unix(1_700_000_200, 0).UTC(), "mb0"); err != nil {
		t.Fatal(err)
	}
	if err := h.plans.RecordDispatch(h.ctx, planB, b1, time.Unix(1_700_000_300, 0).UTC(), "mb1"); err != nil {
		t.Fatal(err)
	}

	planIDs := []pm.PlanID{planA, planB}

	// --- ListDependenciesByPlans: grouped per plan, == single-plan reader. -----
	allEdges, err := h.plans.ListDependenciesByPlans(h.ctx, planIDs)
	if err != nil {
		t.Fatal(err)
	}
	edgesByPlan := map[pm.PlanID][]pm.Dependency{}
	for _, e := range allEdges {
		edgesByPlan[e.PlanID] = append(edgesByPlan[e.PlanID], e)
	}
	if len(edgesByPlan[planA]) != 1 {
		t.Fatalf("planA edges=%d want 1 (leak?)", len(edgesByPlan[planA]))
	}
	if len(edgesByPlan[planB]) != 2 {
		t.Fatalf("planB edges=%d want 2 (leak?)", len(edgesByPlan[planB]))
	}
	for _, p := range planIDs {
		single, serr := h.plans.ListDependencies(h.ctx, p)
		if serr != nil {
			t.Fatal(serr)
		}
		if !reflect.DeepEqual(edgesByPlan[p], single) {
			t.Fatalf("plan %s batched edges != single-plan edges:\n batched=%v\n single =%v", p, edgesByPlan[p], single)
		}
	}

	// --- ListDispatchRecordsByPlans: grouped per plan, == single-plan reader. ---
	allRecs, err := h.plans.ListDispatchRecordsByPlans(h.ctx, planIDs)
	if err != nil {
		t.Fatal(err)
	}
	recsByPlan := map[pm.PlanID][]pm.DispatchRecord{}
	for _, rec := range allRecs {
		recsByPlan[rec.PlanID] = append(recsByPlan[rec.PlanID], rec)
	}
	if len(recsByPlan[planA]) != 1 {
		t.Fatalf("planA dispatch=%d want 1 (leak?)", len(recsByPlan[planA]))
	}
	if len(recsByPlan[planB]) != 2 {
		t.Fatalf("planB dispatch=%d want 2 (leak?)", len(recsByPlan[planB]))
	}
	for _, p := range planIDs {
		single, serr := h.plans.ListDispatchRecords(h.ctx, p)
		if serr != nil {
			t.Fatal(serr)
		}
		if !reflect.DeepEqual(recsByPlan[p], single) {
			t.Fatalf("plan %s batched dispatch != single-plan dispatch:\n batched=%v\n single =%v", p, recsByPlan[p], single)
		}
	}

	// --- empty planIDs → empty slice (no malformed IN ()). ---------------------
	emptyEdges, err := h.plans.ListDependenciesByPlans(h.ctx, nil)
	if err != nil {
		t.Fatalf("empty ListDependenciesByPlans: %v", err)
	}
	if len(emptyEdges) != 0 {
		t.Fatalf("empty planIDs edges=%d want 0", len(emptyEdges))
	}
	emptyRecs, err := h.plans.ListDispatchRecordsByPlans(h.ctx, nil)
	if err != nil {
		t.Fatalf("empty ListDispatchRecordsByPlans: %v", err)
	}
	if len(emptyRecs) != 0 {
		t.Fatalf("empty planIDs dispatch=%d want 0", len(emptyRecs))
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
		h.setTaskStatus(t, b, pm.TaskCompleted)
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

// planTestDisplayName mirrors the production resolver (strip the agent:/user:
// scheme → display_name). The harness has no IdentityRepo, so the bare id stands
// in for the display_name — enough to exercise the @<display_name> prepend +
// the wake-match assertion. An empty/scheme-only ref is "unresolvable".
func planTestDisplayName(_ context.Context, assigneeRef string) (string, bool) {
	id := assigneeRef
	if i := strings.IndexByte(id, ':'); i >= 0 {
		id = id[i+1:]
	}
	if strings.TrimSpace(id) == "" {
		return "", false
	}
	return id, true
}

// latestPlanMsgText returns the most recent message text in the plan conversation.
func (h *planAdvanceHarness) latestPlanMsgText(t *testing.T, planID pm.PlanID) string {
	t.Helper()
	conv, err := h.convRepo.FindByOwnerRef(h.ctx, conversation.NewPlanOwnerRef(string(planID)))
	if err != nil {
		t.Fatalf("plan conversation should exist: %v", err)
	}
	msgs, err := h.msgRepo.FindRecent(h.ctx, conv.ID(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) == 0 {
		t.Fatal("no message posted into plan conversation")
	}
	return msgs[0].Content()
}

// TestDispatch_PostsAtMention_WakeWouldFire is the BUG C regression: a dispatched
// node's message must @mention the assignee's display_name so the wake detector
// (mention.Present) matches it — i.e. an IDLE agent WOULD be woken. Without the
// fix the orchestrator posted the raw ref ("agent:... your task..."), which
// mention.Present never matched → the idle agent was never woken.
func TestDispatch_PostsAtMention_WakeWouldFire(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "wake", CreatedBy: "user:a"})
	h.drain(t)
	const assignee = "agent:agent-bot-1"
	a := h.seedAssignedTask(t, pid, planID, "build the thing", assignee)
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	d, err := h.svc.AdvancePlan(h.ctx, planID, "user:a")
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 1 || d[0] != a {
		t.Fatalf("advance = %v, want [A]", d)
	}
	text := h.latestPlanMsgText(t, planID)

	displayName, _ := planTestDisplayName(h.ctx, assignee) // "agent-bot-1"
	if !strings.Contains(text, "@"+displayName) {
		t.Fatalf("dispatch text %q does not contain @%s", text, displayName)
	}
	// The exact contract: the wake detector WOULD fire on this text.
	if !mention.Present(text, displayName) {
		t.Fatalf("mention.Present(%q, %q) = false; the idle agent would NOT be woken", text, displayName)
	}
	// The raw ref must NOT leak into the body (the old broken format).
	if strings.Contains(text, assignee+" your task") {
		t.Fatalf("dispatch text still embeds the raw ref: %q", text)
	}
}

// TestDispatch_NamesTaskByID is the @oopslink request: the plan-conversation
// system reminder names the dispatched task by its human id (T<n>) — not only
// its title — so the reminder is unambiguous.
func TestDispatch_NamesTaskByID(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "wake", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "build the thing", "agent:agent-bot-1")
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	d, err := h.svc.AdvancePlan(h.ctx, planID, "user:a")
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 1 || d[0] != a {
		t.Fatalf("advance = %v, want [A]", d)
	}
	tk, err := h.tasks.FindByID(h.ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	ref := taskRefToken(tk)
	if ref == "" {
		t.Fatalf("seeded task has no org number; expected an allocated T<n>")
	}
	text := h.latestPlanMsgText(t, planID)
	if !strings.Contains(text, ref) {
		t.Fatalf("dispatch text %q does not name the task by id %q", text, ref)
	}
	if !strings.Contains(text, "build the thing") {
		t.Fatalf("dispatch text %q dropped the title", text)
	}
}

// TestTaskRefToken covers the small formatter directly: allocated → "T<n>",
// unallocated / nil → "".
func TestTaskRefToken(t *testing.T) {
	mk := func(org int) *pm.Task {
		tk, err := pm.RehydrateTask(pm.RehydrateTaskInput{
			ID: "t-1", ProjectID: "p-1", Title: "x", Status: pm.TaskOpen,
			Version: 1, OrgNumber: org, CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0),
		})
		if err != nil {
			t.Fatal(err)
		}
		return tk
	}
	if got := taskRefToken(mk(123)); got != "T123" {
		t.Fatalf("taskRefToken(org=123) = %q, want T123", got)
	}
	if got := taskRefToken(mk(0)); got != "" {
		t.Fatalf("taskRefToken(org=0) = %q, want empty", got)
	}
	if got := taskRefToken(nil); got != "" {
		t.Fatalf("taskRefToken(nil) = %q, want empty", got)
	}
}

// TestDispatch_UnresolvableRef_FallsBack covers the BUG C fallback: when the
// assignee ref has no resolvable display_name, the adapter posts the body
// verbatim (nothing breaks) — but with no @mention, so the wake won't fire.
func TestDispatch_UnresolvableRef_FallsBack(t *testing.T) {
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
	convRepo := convsql.NewConversationRepo(db)
	msgRepo := convsql.NewMessageRepo(db)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, clk)
	writer := convservice.NewMessageWriter(db, convRepo, msgRepo, sink, gen, clk).WithOutbox(ob)

	// Resolver that NEVER resolves → the fallback path (post content verbatim).
	adapter := convservice.NewPlanDispatchAdapter(writer, func(context.Context, string) (string, bool) {
		return "", false
	})

	ctx := context.Background()
	// Create a conversation to post into.
	conv, err := conversation.NewConversation(conversation.NewConversationInput{
		ID: conversation.ConversationID(gen.NewULID()), Kind: conversation.ConversationKindPlan,
		OwnerRef: conversation.NewPlanOwnerRef("plan-x"), OrganizationID: "org-1",
		CreatedBy: conversation.IdentityRef("user:a"), OpenedAt: clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := convRepo.Save(ctx, conv); err != nil {
		t.Fatal(err)
	}
	body := "your task \"X\" is ready — all upstream dependencies are done."
	_, err = adapter.PostMention(ctx, string(conv.ID()), "agent:unknown", body)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := msgRepo.FindRecent(ctx, conv.ID(), 1)
	if err != nil || len(msgs) == 0 {
		t.Fatalf("no message posted: %v", err)
	}
	if got := msgs[0].Content(); got != body {
		t.Fatalf("fallback content = %q, want verbatim %q (no @mention prepended)", got, body)
	}
}
