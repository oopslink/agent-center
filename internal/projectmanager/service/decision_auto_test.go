package service

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// recordingDispatcher captures NotifyDecisionDeferred @mentions.
type recordingDispatcher struct {
	posts       int
	lastTarget  string
	lastContent string
}

func (d *recordingDispatcher) PostMention(_ context.Context, _, assigneeRef, content string) (string, error) {
	d.posts++
	d.lastTarget, d.lastContent = assigneeRef, content
	return "msg-1", nil
}

type autoFixture struct {
	svc      *Service
	tasks    *pmsql.TaskRepo
	findings *pmsql.PlanFindingRepo
	disp     *recordingDispatcher
	ctx      context.Context
	clk      *clock.FakeClock
}

func newAutoFixture(t *testing.T) *autoFixture {
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
	tasks := pmsql.NewTaskRepo(db)
	findings := pmsql.NewPlanFindingRepo(db)
	disp := &recordingDispatcher{}
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: tasks,
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Plans: pmsql.NewPlanRepo(db),
		Findings: findings, Outbox: outboxsql.NewOutboxRepo(db),
		IDGen: gen, Clock: clk, PlanDispatcher: disp,
	})
	return &autoFixture{svc: svc, tasks: tasks, findings: findings, disp: disp, ctx: context.Background(), clk: clk}
}

// decisionNode builds a draft plan with a decision node (branch/base set) and a
// downstream node wired by a conditional(when=pass) edge (From=downstream → To=decision,
// the canonical direction). Returns the plan + decision + downstream task ids.
func (f *autoFixture) decisionNode(t *testing.T) (pm.PlanID, pm.TaskID, pm.TaskID) {
	t.Helper()
	pid, err := f.svc.CreateProject(f.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := pm.NewCodeRepoRef(pm.NewCodeRepoRefInput{
		ID: "repo-1", ProjectID: pid, URL: "https://example.com/repo.git", AddedBy: "user:pd", CreatedAt: f.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.codeRepoRefs.Save(f.ctx, ref); err != nil {
		t.Fatal(err)
	}
	planID, err := f.svc.CreatePlan(f.ctx, CreatePlanCommand{ProjectID: pid, Name: "cycle", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	dec, err := f.svc.CreateTask(f.ctx, CreateTaskCommand{
		ProjectID: pid, Title: "Review/Decision", CreatedBy: "user:pd",
	})
	if err != nil {
		t.Fatal(err)
	}
	down, err := f.svc.CreateTask(f.ctx, CreateTaskCommand{ProjectID: pid, Title: "Integrate", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tid := range []pm.TaskID{dec, down} {
		if err := f.svc.SelectTaskIntoPlan(f.ctx, planID, tid, "user:pd"); err != nil {
			t.Fatal(err)
		}
	}
	// Conditional edge: downstream depends_on decision when pass ⇒ decision = To.
	if err := f.svc.plans.AddDependency(f.ctx, pm.Dependency{
		PlanID: planID, FromTaskID: down, ToTaskID: dec, Kind: pm.EdgeConditional, When: "pass",
	}); err != nil {
		t.Fatal(err)
	}
	return planID, dec, down
}

// T810 ⑤: the B3 auto-decision (ComputeAutoDecision) was DELETED — the gate was removed
// in v2.28.0 so it always deferred, and the orchestration engine now owns routing. The
// tests below cover what remains: RecordDecisionOutcome (the engine's decision input)
// and NotifyDecisionDeferred (the deferral @mention for a decision completed without a
// manual outcome — now self-determining "is a decision node" via pm.IsDecisionNode).

// TestRecordDecisionOutcome_RoundTrip: a human records a decision's outcome; it is
// persisted as the decision-routing input driveGraphDecisions consumes.
func TestRecordDecisionOutcome_RoundTrip(t *testing.T) {
	f := newAutoFixture(t)
	planID, dec, _ := f.decisionNode(t)
	if err := f.svc.RecordDecisionOutcome(f.ctx, dec, pm.OutcomePass, "user:pd"); err != nil {
		t.Fatal(err)
	}
	outs, err := f.svc.plans.ListDecisionOutcomes(f.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	found := ""
	for _, o := range outs {
		if o.TaskID == dec {
			found = o.Outcome
		}
	}
	if found != pm.OutcomePass {
		t.Fatalf("recorded outcome for decision = %q, want pass", found)
	}
}

// TestNotifyDecisionDeferred_PingsHuman: a DECISION node completed without a manual
// outcome @mentions a human (the plan creator, absent an assignee).
func TestNotifyDecisionDeferred_PingsHuman(t *testing.T) {
	f := newAutoFixture(t)
	_, dec, _ := f.decisionNode(t)
	if err := f.svc.NotifyDecisionDeferred(f.ctx, dec); err != nil {
		t.Fatal(err)
	}
	if f.disp.posts != 1 {
		t.Fatalf("expected 1 deferral @mention, got %d", f.disp.posts)
	}
	if f.disp.lastTarget != "user:pd" { // unassigned decision → plan creator
		t.Fatalf("deferral target = %q, want user:pd (plan creator fallback)", f.disp.lastTarget)
	}
}

// TestNotifyDecisionDeferred_OrdinaryNode_NoOp: a NON-decision node does not @mention
// (NotifyDecisionDeferred now self-determines the decision-ness via pm.IsDecisionNode).
func TestNotifyDecisionDeferred_OrdinaryNode_NoOp(t *testing.T) {
	f := newAutoFixture(t)
	_, _, down := f.decisionNode(t) // downstream node has no routing OUT-edge → not a decision
	if err := f.svc.NotifyDecisionDeferred(f.ctx, down); err != nil {
		t.Fatal(err)
	}
	if f.disp.posts != 0 {
		t.Fatalf("an ordinary node must NOT @mention, got %d posts", f.disp.posts)
	}
}
