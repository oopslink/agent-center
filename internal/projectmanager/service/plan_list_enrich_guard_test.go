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
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// countingPlanRepo wraps a PlanRepository and counts the BATCH vs per-plan read
// methods. ListPlanSummaries (#272 Work Board enrich) is N+1-free precisely
// because it reads all plans' dependencies + dispatch records via ONE batched
// IN-query each (ListDependenciesByPlans / ListDispatchRecordsByPlans), then
// derives each plan's view in-memory — never the per-plan ListDependencies /
// ListDispatchRecords in a loop. This guard locks that: a future refactor that
// regresses ListPlanSummaries to a per-plan loop (the silent N+1) makes the batch
// counts != 1 and/or the per-plan counts != 0, and this test FAILS.
// Same production-completeness family as the #266 outboxProjectors class-guard.
type countingPlanRepo struct {
	pm.PlanRepository
	depsByPlans     int
	dispatchByPlans int
	perPlanDeps     int
	perPlanDispatch int
}

func (c *countingPlanRepo) ListDependenciesByPlans(ctx context.Context, ids []pm.PlanID) ([]pm.Dependency, error) {
	c.depsByPlans++
	return c.PlanRepository.ListDependenciesByPlans(ctx, ids)
}

func (c *countingPlanRepo) ListDispatchRecordsByPlans(ctx context.Context, ids []pm.PlanID) ([]pm.DispatchRecord, error) {
	c.dispatchByPlans++
	return c.PlanRepository.ListDispatchRecordsByPlans(ctx, ids)
}

func (c *countingPlanRepo) ListDependencies(ctx context.Context, id pm.PlanID) ([]pm.Dependency, error) {
	c.perPlanDeps++
	return c.PlanRepository.ListDependencies(ctx, id)
}

func (c *countingPlanRepo) ListDispatchRecords(ctx context.Context, id pm.PlanID) ([]pm.DispatchRecord, error) {
	c.perPlanDispatch++
	return c.PlanRepository.ListDispatchRecords(ctx, id)
}

// TestListPlanSummaries_NoNPlus1_QueryCountGuard is the deterministic standing
// guard for the #272 list-enrich N+1-free invariant (PD-assigned fast-follow,
// the counting-guard). It builds the service with a counting PlanRepository spy,
// seeds N plans, and asserts ListPlanSummaries issues the batch reads exactly
// once each (constant, not N) and never the per-plan variants.
func TestListPlanSummaries_NoNPlus1_QueryCountGuard(t *testing.T) {
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
	realPlans := pmsql.NewPlanRepo(db)
	spy := &countingPlanRepo{PlanRepository: realPlans}
	tasks := pmsql.NewTaskRepo(db)
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: tasks,
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Plans: spy, Outbox: ob, IDGen: gen, Clock: clk,
		AgentDir:       allOrgDir("org-1"),
		PlanDispatcher: convservice.NewPlanDispatchAdapter(writer, planTestDisplayName),
	})
	taskProj := NewParticipantProjector(db, convRepo, applied, gen, clk)
	planProj := NewPlanParticipantProjector(db, convRepo, realPlans, applied, gen, clk)
	relay := outbox.NewRelay(ob, applied, clk, taskProj, planProj)
	h := &planAdvanceHarness{svc: svc, plans: realPlans, tasks: tasks, convRepo: convRepo, msgRepo: msgRepo, relay: relay, ctx: context.Background()}

	pid, perr := svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if perr != nil {
		t.Fatal(perr)
	}

	// Seed N plans, each a real DAG (3 tasks t0->t1->t2 + 1 dispatch record).
	const N = 5
	for i := 0; i < N; i++ {
		name := "p" + string(rune('a'+i))
		planID, cerr := svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: name, CreatedBy: "user:a"})
		if cerr != nil {
			t.Fatal(cerr)
		}
		h.drain(t)
		t0 := h.seedAssignedTask(t, pid, planID, name+"-t0", "user:x")
		t1 := h.seedAssignedTask(t, pid, planID, name+"-t1", "user:y")
		t2 := h.seedAssignedTask(t, pid, planID, name+"-t2", "user:z")
		if aerr := svc.AddPlanDependency(h.ctx, planID, t1, t0, "user:a"); aerr != nil {
			t.Fatal(aerr)
		}
		if aerr := svc.AddPlanDependency(h.ctx, planID, t2, t1, "user:a"); aerr != nil {
			t.Fatal(aerr)
		}
		if rerr := realPlans.RecordDispatch(h.ctx, planID, t0, clk.Now(), "msg-"+name); rerr != nil {
			t.Fatal(rerr)
		}
	}

	// Reset AFTER seeding (AddPlanDependency's cycle-check uses per-plan reads);
	// the guard is about the single ListPlanSummaries call below.
	spy.depsByPlans, spy.dispatchByPlans, spy.perPlanDeps, spy.perPlanDispatch = 0, 0, 0, 0

	sums, lerr := svc.ListPlanSummaries(h.ctx, pid)
	if lerr != nil {
		t.Fatal(lerr)
	}
	// N seeded plans + the ADR-0047 auto-created built-in pool (from CreateProject).
	if len(sums) != N+1 {
		t.Fatalf("ListPlanSummaries returned %d plans, want %d", len(sums), N+1)
	}

	if spy.depsByPlans != 1 {
		t.Errorf("ListDependenciesByPlans called %d times over %d plans — want exactly 1 (single batched IN-query). >1 = ListPlanSummaries regressed to a per-plan loop = N+1 (#272 命门).", spy.depsByPlans, N)
	}
	if spy.dispatchByPlans != 1 {
		t.Errorf("ListDispatchRecordsByPlans called %d times over %d plans — want exactly 1 (single batched IN-query). >1 = N+1 regression.", spy.dispatchByPlans, N)
	}
	if spy.perPlanDeps != 0 {
		t.Errorf("per-plan ListDependencies called %d times during ListPlanSummaries — must be 0 (that per-plan read IS the N+1 the batch avoids).", spy.perPlanDeps)
	}
	if spy.perPlanDispatch != 0 {
		t.Errorf("per-plan ListDispatchRecords called %d times during ListPlanSummaries — must be 0.", spy.perPlanDispatch)
	}
}
