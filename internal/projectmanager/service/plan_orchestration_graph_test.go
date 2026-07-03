package service

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	convsql "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
	orchsql "github.com/oopslink/agent-center/internal/projectmanager/orchestration/sqlite"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// planGraphSetup mirrors planAdvanceSetup but ALSO wires the T768 orchestration
// engine (Deps.Orch), so StartPlan builds a graph and dispatch runs off it. It
// returns the harness plus the orch service for graph assertions.
func planGraphSetup(t *testing.T) (*planAdvanceHarness, *orch.Service) {
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
	actionLogs := pmsql.NewTaskActionLogRepo(db, gen)
	orchSvc := orch.NewService(orch.ServiceDeps{
		DB: db, Graphs: orchsql.NewGraphRepo(db), Nodes: orchsql.NewNodeRepo(db),
		Edges: orchsql.NewEdgeRepo(db), IDGen: gen, Clock: clk,
	})
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: tasks,
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Plans: plans, Outbox: ob, IDGen: gen, Clock: clk,
		TaskActionLogs: actionLogs,
		OrgSeq:         pmsql.NewOrgSequenceRepo(db),
		AgentDir:       allOrgDir("org-1"),
		PlanDispatcher: convservice.NewPlanDispatchAdapter(writer, planTestDisplayName),
		Orch:           orchSvc, // T768: graph-backed dispatch
	})
	taskProj := NewParticipantProjector(db, convRepo, applied, gen, clk)
	planProj := NewPlanParticipantProjector(db, convRepo, plans, applied, gen, clk)
	relay := outbox.NewRelay(ob, applied, clk, taskProj, planProj)
	h := &planAdvanceHarness{svc: svc, plans: plans, tasks: tasks, convRepo: convRepo, msgRepo: msgRepo, relay: relay, clk: clk, actionLogs: actionLogs, ctx: context.Background()}
	return h, orchSvc
}

// TestStartPlan_BuildsGraph_AndDispatchesOffIt is the T768 end-to-end for a pure
// DAG plan A→B: StartPlan builds a graph (plan.graph_id set, one business node per
// task bound by task_id, one edge), then dispatch runs off GetReadyNodes — A first
// (root), B only after A completes (node sync) — and the plan marks done via
// graph.IsAutoDone.
func TestStartPlan_BuildsGraph_AndDispatchesOffIt(t *testing.T) {
	h, orchSvc := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "graphdag", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:b1")
	// B depends_on A (B runs after A).
	if err := h.svc.AddPlanDependency(ctx, planID, b, a, "user:a"); err != nil {
		t.Fatalf("AddPlanDependency: %v", err)
	}
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}

	// --- graph was built + wired back onto the aggregates ---
	p, _ := h.plans.FindByID(ctx, planID)
	if p.GraphID() == "" {
		t.Fatal("StartPlan did not set plan.graph_id — graph was not built")
	}
	ta, _ := h.tasks.FindByID(ctx, a)
	tb, _ := h.tasks.FindByID(ctx, b)
	if ta.NodeID() == "" || tb.NodeID() == "" {
		t.Fatalf("tasks not bound to nodes: A.node=%q B.node=%q", ta.NodeID(), tb.NodeID())
	}
	nodes, err := orchSvc.ListNodes(ctx, orch.GraphID(p.GraphID()))
	if err != nil {
		t.Fatal(err)
	}
	business := 0
	for _, n := range nodes {
		if n.Category() == orch.NodeCategoryBusiness {
			business++
			if nodeTaskID(n) == "" {
				t.Fatalf("business node %s has no bound task_id", n.ID())
			}
		}
	}
	if business != 2 {
		t.Fatalf("business nodes = %d, want 2 (one per task)", business)
	}

	// --- dispatch #1: only A (root) is ready; B blocked on A ---
	d1, err := h.svc.AdvancePlan(ctx, planID, "user:a")
	if err != nil {
		t.Fatalf("AdvancePlan #1: %v", err)
	}
	if len(d1) != 1 || d1[0] != a {
		t.Fatalf("dispatch #1 = %v, want [A]=%v (GetReadyNodes root only)", d1, a)
	}

	// idempotent: re-advance with no state change dispatches nothing.
	if d, _ := h.svc.AdvancePlan(ctx, planID, "user:a"); len(d) != 0 {
		t.Fatalf("re-advance dispatched %v, want [] (dispatch-record idempotency)", d)
	}

	// --- A claimed → running: node open→running sync; B still blocked ---
	h.setTaskStatus(t, a, pm.TaskRunning)
	if d, _ := h.svc.AdvancePlan(ctx, planID, "user:a"); len(d) != 0 {
		t.Fatalf("A running dispatched %v, want [] (B still blocked)", d)
	}

	// --- complete A → node sync advances A→completed → B becomes ready ---
	h.setTaskStatus(t, a, pm.TaskCompleted)
	d2, err := h.svc.AdvancePlan(ctx, planID, "user:a")
	if err != nil {
		t.Fatalf("AdvancePlan #2: %v", err)
	}
	if len(d2) != 1 || d2[0] != b {
		t.Fatalf("dispatch #2 = %v, want [B]=%v (unblocked after A completed)", d2, b)
	}

	// --- complete B → graph.IsAutoDone → plan done ---
	h.setTaskStatus(t, b, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan #3: %v", err)
	}
	p, _ = h.plans.FindByID(ctx, planID)
	if p.Status() != pm.PlanDone {
		t.Fatalf("plan status = %s, want done (graph.IsAutoDone)", p.Status())
	}
}

// buildGraphCycle seeds a Dev→Review→Decision cycle with a conditional (pass→
// Integrate) branch and a bounded loopback (reject→Dev), then StartPlan (which
// builds the graph incl. the decision's condition node). Returns the task ids.
func buildGraphCycle(t *testing.T, h *planAdvanceHarness, pid pm.ProjectID, planID pm.PlanID) (dev, rev, dec, integ pm.TaskID) {
	t.Helper()
	dev = h.seedAssignedTask(t, pid, planID, "Dev", "user:dev")
	rev = h.seedAssignedTask(t, pid, planID, "Review", "user:rev")
	dec = h.seedAssignedTask(t, pid, planID, "Decision", "user:pd")
	integ = h.seedAssignedTask(t, pid, planID, "Integrate", "user:int")
	// Forward seq chain Dev → Review → Decision.
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: rev, ToTaskID: dev, Kind: pm.EdgeSeq})
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: dec, ToTaskID: rev, Kind: pm.EdgeSeq})
	// Conditional Integrate --(pass)--> gated behind Decision.
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: integ, ToTaskID: dec, Kind: pm.EdgeConditional, When: "pass"})
	// Bounded loopback Decision --loopback(reject,max=2)--> Dev.
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: dec, ToTaskID: dev, Kind: pm.EdgeLoopback, When: "reject", MaxRounds: 2})
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	return dev, rev, dec, integ
}

func mustAddDep(t *testing.T, h *planAdvanceHarness, planID pm.PlanID, d pm.Dependency) {
	t.Helper()
	if err := h.plans.AddDependency(h.ctx, d); err != nil {
		t.Fatalf("AddDependency %+v: %v", d, err)
	}
}

func graphHasTaskID(ids []pm.TaskID, want pm.TaskID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// TestGraphCycle_ConditionGate_PassReleasesDownstream proves the T768 decision
// routing on the graph: the conditional (Integrate) branch stays GATED behind the
// decision's condition node until the decision records a pass-outcome, at which
// point driveGraphConditions resolves the condition and Integrate is released +
// dispatched, and the plan completes.
func TestGraphCycle_ConditionGate_PassReleasesDownstream(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "cyclepass", CreatedBy: "user:a"})
	h.drain(t)
	dev, rev, dec, integ := buildGraphCycle(t, h, pid, planID)

	// Dev first; Integrate GATED behind the decision's condition node.
	d1, err := h.svc.AdvancePlan(ctx, planID, "user:a")
	if err != nil {
		t.Fatalf("AdvancePlan #1: %v", err)
	}
	if !graphHasTaskID(d1, dev) || graphHasTaskID(d1, integ) {
		t.Fatalf("dispatch #1 = %v, want [Dev] only (Integrate gated behind condition)", d1)
	}

	// Walk Dev → Review → Decision.
	h.setTaskStatus(t, dev, pm.TaskCompleted)
	if d, _ := h.svc.AdvancePlan(ctx, planID, "user:a"); !graphHasTaskID(d, rev) {
		t.Fatalf("after Dev done, dispatch = %v, want Review", d)
	}
	h.setTaskStatus(t, rev, pm.TaskCompleted)
	if d, _ := h.svc.AdvancePlan(ctx, planID, "user:a"); !graphHasTaskID(d, dec) {
		t.Fatalf("after Review done, dispatch = %v, want Decision", d)
	}

	// Decision still open → Integrate MUST NOT be dispatched yet.
	if d, _ := h.svc.AdvancePlan(ctx, planID, "user:a"); graphHasTaskID(d, integ) {
		t.Fatalf("Integrate dispatched before decision resolved: %v", d)
	}

	// Decision passes → condition resolves → Integrate released.
	if err := h.svc.SetDecisionOutcome(ctx, dec, "pass", "user:a"); err != nil {
		t.Fatalf("SetDecisionOutcome: %v", err)
	}
	h.setTaskStatus(t, dec, pm.TaskCompleted)
	dRelease, err := h.svc.AdvancePlan(ctx, planID, "user:a")
	if err != nil {
		t.Fatalf("AdvancePlan release: %v", err)
	}
	if !graphHasTaskID(dRelease, integ) {
		t.Fatalf("after decision pass, dispatch = %v, want Integrate released", dRelease)
	}

	// Finish Integrate → plan done.
	h.setTaskStatus(t, integ, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan final: %v", err)
	}
	p, _ := h.plans.FindByID(ctx, planID)
	if p.Status() != pm.PlanDone {
		t.Fatalf("plan status = %s, want done", p.Status())
	}
}

// TestGraphCycle_Loopback_RejectReopensDev proves the reject/loopback round on the
// graph: a reject outcome does NOT release Integrate; the task-level applyLoopbacks
// reopens the Dev→Review→Decision subgraph, and syncGraphToTasks propagates the
// reopen onto the graph nodes so Dev is re-dispatched for another round.
func TestGraphCycle_Loopback_RejectReopensDev(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "cyclereject", CreatedBy: "user:a"})
	h.drain(t)
	dev, rev, dec, integ := buildGraphCycle(t, h, pid, planID)

	// Walk the first round Dev → Review → Decision.
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, dev, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, rev, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	// Decision rejects.
	if err := h.svc.SetDecisionOutcome(ctx, dec, "reject", "user:a"); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, dec, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	// Integrate must NOT be released on reject.
	p, _ := h.plans.FindByID(ctx, planID)
	if p.Status() == pm.PlanDone {
		t.Fatal("plan marked done on reject — Integrate should stay gated")
	}

	// Drive the loopback (the projector's advance step). It reopens the loop subgraph.
	if err := h.svc.applyLoopbacks(ctx, p, dec); err != nil {
		t.Fatalf("applyLoopbacks: %v", err)
	}
	// Next advance re-dispatches Dev for round 2 (node reopened + dispatch cleared).
	dLoop, err := h.svc.AdvancePlan(ctx, planID, "user:a")
	if err != nil {
		t.Fatalf("AdvancePlan loopback: %v", err)
	}
	if !graphHasTaskID(dLoop, dev) {
		t.Fatalf("after reject loopback, dispatch = %v, want Dev re-dispatched", dLoop)
	}
	// The decision task was reopened by the loopback (Completed→Reopened).
	dt, _ := h.tasks.FindByID(ctx, dec)
	if dt.Status() != pm.TaskReopened && dt.Status() != pm.TaskOpen {
		t.Fatalf("decision status after loopback = %s, want reopened/open", dt.Status())
	}
	_ = rev
	_ = integ
}

// TestGraphDispatch_FailedTaskBlocksDownstream is the §9.7 parity guard: a FAILED
// (discarded) task must leave the graph node non-terminal so downstream stays
// blocked and the plan stays running (NOT auto-complete via a satisfied-terminal
// discard). Covers advanceNodeTo's failed branch.
func TestGraphDispatch_FailedTaskBlocksDownstream(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "failblock", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:b1")
	if err := h.svc.AddPlanDependency(ctx, planID, b, a, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	// A fails.
	h.setTaskStatus(t, a, pm.TaskDiscarded)
	d, err := h.svc.AdvancePlan(ctx, planID, "user:a")
	if err != nil {
		t.Fatalf("AdvancePlan: %v", err)
	}
	if graphHasTaskID(d, b) {
		t.Fatalf("B dispatched after upstream A failed: %v (should stay blocked, §9.7)", d)
	}
	p, _ := h.plans.FindByID(ctx, planID)
	if p.Status() == pm.PlanDone {
		t.Fatal("plan marked done with a failed node — must stay running (§9.1)")
	}
}

// TestGraphHelpers covers the pure metadata helpers' edge branches.
func TestGraphHelpers(t *testing.T) {
	// nodeTaskID: bound, missing, and non-string metadata.
	nBound, _ := orch.NewNode(orch.NewNodeInput{ID: "n1", GraphID: "g", Category: orch.NodeCategoryBusiness, Title: "t", Metadata: map[string]any{"task_id": "T7"}})
	if got := nodeTaskID(nBound); got != "T7" {
		t.Fatalf("nodeTaskID bound = %q, want T7", got)
	}
	nBad, _ := orch.NewNode(orch.NewNodeInput{ID: "n2", GraphID: "g", Category: orch.NodeCategoryBusiness, Title: "t", Metadata: map[string]any{"task_id": 42}})
	if got := nodeTaskID(nBad); got != "" {
		t.Fatalf("nodeTaskID non-string = %q, want empty", got)
	}
	nNone, _ := orch.NewNode(orch.NewNodeInput{ID: "n3", GraphID: "g", Category: orch.NodeCategoryBusiness, Title: "t"})
	if got := nodeTaskID(nNone); got != "" {
		t.Fatalf("nodeTaskID missing = %q, want empty", got)
	}
	// metaHasWhen: hit, miss, non-list, non-string items.
	if !metaHasWhen([]any{"pass", "ok"}, "pass") {
		t.Fatal("metaHasWhen should find 'pass'")
	}
	if metaHasWhen([]any{"pass"}, "reject") {
		t.Fatal("metaHasWhen should not find 'reject'")
	}
	if metaHasWhen("not-a-list", "pass") {
		t.Fatal("metaHasWhen on non-list should be false")
	}
	if metaHasWhen([]any{1, 2}, "pass") {
		t.Fatal("metaHasWhen on non-string items should be false")
	}
}

// TestStartPlan_NoOrch_UsesLegacyPath is the zero-regression guard: with the engine
// UNWIRED (Deps.Orch nil, every pre-T768 construction + control-flow plan), StartPlan
// builds NO graph (plan.graph_id stays empty) and dispatch runs off the legacy
// ComputePlanView path — unchanged behavior.
func TestStartPlan_NoOrch_UsesLegacyPath(t *testing.T) {
	h := planAdvanceSetup(t) // no Orch wired
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "legacy", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	p, _ := h.plans.FindByID(ctx, planID)
	if p.GraphID() != "" {
		t.Fatalf("legacy plan got a graph_id %q — graph must not be built without the engine", p.GraphID())
	}
	// Dispatch still works off the legacy path.
	d, err := h.svc.AdvancePlan(ctx, planID, "user:a")
	if err != nil {
		t.Fatalf("AdvancePlan: %v", err)
	}
	if len(d) != 1 || d[0] != a {
		t.Fatalf("legacy dispatch = %v, want [A]=%v", d, a)
	}
}
