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

// ============================================================================
// issue-77cda494 — make list_plans (ListPlanSummaries) STAGE-AWARE, matching the
// get_plan detail path (issue-77d9beff ②), WITHOUT the per-plan graph read that
// would be an N+1. The batched barrier read (stageBarrierHeldSetByPlans) pays a
// CONSTANT number of graph reads for the whole page.
// ============================================================================

// countingNodeRepo / countingEdgeRepo wrap the sqlite orch repos and count the
// per-graph vs batched graph reads. The N+1 guard asserts the STAGE-aware list_plans
// read issues the BATCHED ListByGraphs exactly once each (constant) and NEVER the
// per-plan ListByGraph in a loop (that per-plan read IS the N+1 the batch avoids).
type countingNodeRepo struct {
	orch.NodeRepository
	perGraph  int
	byGraphs  int
	graphsArg int // max #graphs passed to a single ListByGraphs (proves it batches)
}

func (c *countingNodeRepo) ListByGraph(ctx context.Context, id orch.GraphID) ([]*orch.Node, error) {
	c.perGraph++
	return c.NodeRepository.ListByGraph(ctx, id)
}

func (c *countingNodeRepo) ListByGraphs(ctx context.Context, ids []orch.GraphID) ([]*orch.Node, error) {
	c.byGraphs++
	if len(ids) > c.graphsArg {
		c.graphsArg = len(ids)
	}
	return c.NodeRepository.ListByGraphs(ctx, ids)
}

type countingEdgeRepo struct {
	orch.EdgeRepository
	perGraph int
	byGraphs int
}

func (c *countingEdgeRepo) ListByGraph(ctx context.Context, id orch.GraphID) ([]orch.Edge, error) {
	c.perGraph++
	return c.EdgeRepository.ListByGraph(ctx, id)
}

func (c *countingEdgeRepo) ListByGraphs(ctx context.Context, ids []orch.GraphID) ([]orch.Edge, error) {
	c.byGraphs++
	return c.EdgeRepository.ListByGraphs(ctx, ids)
}

// planGraphSetupWithOrchSpies mirrors planGraphSetup but wires COUNTING node/edge
// repos into the orchestration engine so a test can assert the graph read counts.
func planGraphSetupWithOrchSpies(t *testing.T) (*planAdvanceHarness, *countingNodeRepo, *countingEdgeRepo) {
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
	nodeSpy := &countingNodeRepo{NodeRepository: orchsql.NewNodeRepo(db)}
	edgeSpy := &countingEdgeRepo{EdgeRepository: orchsql.NewEdgeRepo(db)}
	orchSvc := orch.NewService(orch.ServiceDeps{
		DB: db, Graphs: orchsql.NewGraphRepo(db), Nodes: nodeSpy,
		Edges: edgeSpy, IDGen: gen, Clock: clk,
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
		Orch:           orchSvc,
		Stages:         pmsql.NewStageRepo(db),
		Audit:          pmsql.NewAuditLogRepo(db, gen),
	})
	taskProj := NewParticipantProjector(db, convRepo, applied, gen, clk)
	planProj := NewPlanParticipantProjector(db, convRepo, plans, applied, gen, clk)
	relay := outbox.NewRelay(ob, applied, clk, taskProj, planProj)
	h := &planAdvanceHarness{svc: svc, plans: plans, tasks: tasks, convRepo: convRepo, msgRepo: msgRepo, relay: relay, clk: clk, actionLogs: actionLogs, ctx: context.Background()}
	return h, nodeSpy, edgeSpy
}

// startedTwoStagePlan seeds + starts a two-stage plan (Stage A={a1→a2}, Stage B={b1},
// B depends_on A) and advances once so a1 dispatches while stage A's gate is UNRESOLVED
// (so b1 is barrier-held). Returns the plan id + b1.
func startedTwoStagePlan(t *testing.T, h *planAdvanceHarness, pid pm.ProjectID, name string) (pm.PlanID, pm.TaskID) {
	t.Helper()
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: name, CreatedBy: "user:a"})
	h.drain(t)
	_, _, b1, _, _ := seedTwoStagePlan(t, h, pid, planID, 3)
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	if _, err := h.svc.AdvancePlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan: %v", err)
	}
	return planID, b1
}

// TestListPlanSummaries_StageBarrierHeldEntryNotReady — the list_plans口径一致 guard.
// A downstream-stage entry held behind an UNRESOLVED upstream stage gate must show
// `blocked` (not `ready`) in the summary's nodes_preview, IDENTICAL to what
// GetPlanDetailForMember (get_plan) reports (issue-77d9beff ②). PRE-FIX the summary
// path derived it `ready` (stage-unaware DerivePlanView); this test is RED then.
func TestListPlanSummaries_StageBarrierHeldEntryNotReady(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, b1 := startedTwoStagePlan(t, h, pid, "stages")

	sums, err := h.svc.ListPlanSummaries(ctx, pid)
	if err != nil {
		t.Fatalf("ListPlanSummaries: %v", err)
	}
	var got *PlanDetail
	for _, d := range sums {
		if d.Plan.ID() == planID {
			got = d
			break
		}
	}
	if got == nil {
		t.Fatalf("ListPlanSummaries did not return plan %s", planID)
	}
	// b1 (stage B entry) is behind the unresolved stage A gate → must NOT be ready.
	sawB1 := false
	for _, n := range got.View.Nodes {
		if n.TaskID == b1 {
			sawB1 = true
			if n.NodeStatus == pm.NodeReady {
				t.Fatalf("list_plans: b1 (stage-barrier held) NodeStatus=ready, want blocked — summary view must be stage-aware, matching get_plan")
			}
		}
	}
	if !sawB1 {
		t.Fatalf("b1 not present in the summary's nodes preview")
	}

	// Cross-check口径一致: get_plan detail reports the SAME status for b1.
	detail, err := h.svc.GetPlanDetailForMember(ctx, planID, "user:a")
	if err != nil {
		t.Fatalf("GetPlanDetailForMember: %v", err)
	}
	detailB1 := statusOfTask(detail.View.Nodes, b1)
	summaryB1 := statusOfTask(got.View.Nodes, b1)
	if detailB1 != summaryB1 {
		t.Fatalf("b1 status mismatch: list_plans=%q vs get_plan=%q —口径 must be identical", summaryB1, detailB1)
	}
}

func statusOfTask(nodes []pm.PlanNodeView, id pm.TaskID) pm.NodeStatus {
	for _, n := range nodes {
		if n.TaskID == id {
			return n.NodeStatus
		}
	}
	return ""
}

// TestListPlanSummaries_StageAware_NoNPlus1_QueryCountGuard is the N+1 GUARD
// (issue-77cda494命门): with N stage-gated plans, the stage-aware list_plans read
// must issue the BATCHED graph reads a CONSTANT number of times (ListByGraphs once
// for nodes + once for edges), and NEVER the per-plan ListByGraph in a loop. If a
// future refactor regresses to a per-plan graph load, perGraph != 0 and this FAILS.
func TestListPlanSummaries_StageAware_NoNPlus1_QueryCountGuard(t *testing.T) {
	h, nodeSpy, edgeSpy := planGraphSetupWithOrchSpies(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})

	const N = 4
	for i := 0; i < N; i++ {
		startedTwoStagePlan(t, h, pid, "stages"+string(rune('a'+i)))
	}

	// Reset counters AFTER all seeding (StartPlan/AdvancePlan do many per-graph reads);
	// the guard is about the single ListPlanSummaries call below.
	nodeSpy.perGraph, nodeSpy.byGraphs, nodeSpy.graphsArg = 0, 0, 0
	edgeSpy.perGraph, edgeSpy.byGraphs = 0, 0

	sums, err := h.svc.ListPlanSummaries(ctx, pid)
	if err != nil {
		t.Fatalf("ListPlanSummaries: %v", err)
	}
	if len(sums) != N+1 { // N stage plans + the builtin pool
		t.Fatalf("ListPlanSummaries returned %d plans, want %d", len(sums), N+1)
	}

	if nodeSpy.perGraph != 0 {
		t.Errorf("per-plan node ListByGraph called %d times over %d plans — must be 0 (that per-plan graph read IS the N+1 the batch avoids).", nodeSpy.perGraph, N)
	}
	if edgeSpy.perGraph != 0 {
		t.Errorf("per-plan edge ListByGraph called %d times over %d plans — must be 0.", edgeSpy.perGraph, N)
	}
	if nodeSpy.byGraphs != 1 {
		t.Errorf("batched ListNodesByGraphs called %d times over %d plans — want exactly 1 (single batched IN-query). >1 = N+1 regression.", nodeSpy.byGraphs, N)
	}
	if edgeSpy.byGraphs != 1 {
		t.Errorf("batched ListEdgesByGraphs called %d times over %d plans — want exactly 1.", edgeSpy.byGraphs, N)
	}
	// The single node batch must actually carry ALL N plans' graphs (proves batching,
	// not a lucky 1-graph call): with N stage plans, graphsArg must be >= N.
	if nodeSpy.graphsArg < N {
		t.Errorf("batched ListNodesByGraphs saw at most %d graphs in one call — want >= %d (all plans batched in ONE query).", nodeSpy.graphsArg, N)
	}
}
