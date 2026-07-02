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

// addFailureFinding injects a kind=failure finding ON the given task (B3's interim
// open-review-comment signal), bypassing admission rules via the repo directly.
func (f *autoFixture) addFailureFinding(t *testing.T, planID pm.PlanID, taskID pm.TaskID, pid pm.ProjectID) {
	t.Helper()
	fnd, err := pm.NewPlanFinding(pm.NewPlanFindingInput{
		ID: pm.PlanFindingID("f-" + string(taskID)), PlanID: planID, TaskID: taskID, ProjectID: pid,
		AuthorRef: "user:rev", Kind: pm.FindingFailure, Content: "review objection", CreatedAt: f.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.findings.Save(f.ctx, fnd); err != nil {
		t.Fatal(err)
	}
}

func (f *autoFixture) projectOf(t *testing.T, tid pm.TaskID) pm.ProjectID {
	t.Helper()
	tk, err := f.tasks.FindByID(f.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	return tk.ProjectID()
}

// With branch/base removed from Task, evaluateGate always returns GateUnknown,
// so all gate-dependent auto-decisions now defer to a human.

func TestComputeAutoDecision_AlwaysDefersWithoutBranchBase(t *testing.T) {
	f := newAutoFixture(t)
	_, dec, _ := f.decisionNode(t)
	ad, err := f.svc.ComputeAutoDecision(f.ctx, dec)
	if err != nil {
		t.Fatal(err)
	}
	if !ad.IsDecision {
		t.Fatalf("got %+v; want IsDecision", ad)
	}
	// Gate always returns unknown (no branch/base on Task) → defers.
	if ad.Decided {
		t.Fatalf("got %+v; want !Decided (gate always unknown without branch/base)", ad)
	}
	if ad.Gate != pm.GateUnknown {
		t.Fatalf("got gate=%v; want unknown", ad.Gate)
	}
}

func TestComputeAutoDecision_GateUnknown_Defers(t *testing.T) {
	f := newAutoFixture(t)
	_, dec, _ := f.decisionNode(t)
	ad, err := f.svc.ComputeAutoDecision(f.ctx, dec)
	if err != nil {
		t.Fatal(err)
	}
	if !ad.IsDecision || ad.Decided {
		t.Fatalf("got %+v; want IsDecision && !Decided (unknown gate → human)", ad)
	}
}

func TestComputeAutoDecision_NoGateWired_Defers(t *testing.T) {
	f := newAutoFixture(t) // gate always GateUnknown
	_, dec, _ := f.decisionNode(t)
	ad, err := f.svc.ComputeAutoDecision(f.ctx, dec)
	if err != nil {
		t.Fatal(err)
	}
	if !ad.IsDecision || ad.Decided || ad.Gate != pm.GateUnknown {
		t.Fatalf("got %+v; want IsDecision && !Decided && gate unknown (nil gate)", ad)
	}
}

func TestComputeAutoDecision_OrdinaryNode_NoOp(t *testing.T) {
	f := newAutoFixture(t)
	_, _, down := f.decisionNode(t) // downstream node has no routing OUT-edge
	ad, err := f.svc.ComputeAutoDecision(f.ctx, down)
	if err != nil {
		t.Fatal(err)
	}
	if ad.IsDecision {
		t.Fatalf("downstream/ordinary node must not be a decision: %+v", ad)
	}
}

func TestComputeAutoDecision_TaskNotInPlan_NoOp(t *testing.T) {
	f := newAutoFixture(t)
	pid, err := f.svc.CreateProject(f.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P2", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := f.svc.CreateTask(f.ctx, CreateTaskCommand{ProjectID: pid, Title: "loose", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	ad, err := f.svc.ComputeAutoDecision(f.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if ad.IsDecision {
		t.Fatalf("a task with no plan cannot be a decision node: %+v", ad)
	}
}

func TestNotifyDecisionDeferred_PingsHuman(t *testing.T) {
	f := newAutoFixture(t)
	_, dec, _ := f.decisionNode(t)
	ad, err := f.svc.ComputeAutoDecision(f.ctx, dec)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.NotifyDecisionDeferred(f.ctx, dec, ad); err != nil {
		t.Fatal(err)
	}
	if f.disp.posts != 1 {
		t.Fatalf("expected 1 deferral @mention, got %d", f.disp.posts)
	}
	if f.disp.lastTarget != "user:pd" { // unassigned decision → plan creator
		t.Fatalf("deferral target = %q, want user:pd (plan creator fallback)", f.disp.lastTarget)
	}
}

// TestAutoDecision_ManualRecordRoundTrip exercises the handler path where a human
// manually sets a decision outcome (since auto-decisions now always defer without
// branch/base on the Task).
func TestAutoDecision_ManualRecordRoundTrip(t *testing.T) {
	f := newAutoFixture(t)
	planID, dec, _ := f.decisionNode(t)

	// The auto-decision defers (no branch/base → GateUnknown), so record manually.
	if err := f.svc.SetDecisionOutcome(f.ctx, dec, pm.OutcomePass, "user:pd"); err != nil {
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

// An UNDECIDED (deferred) decision triggers a deferral @mention.
func TestNotifyDecisionDeferred_PostsWhenUndecided(t *testing.T) {
	f := newAutoFixture(t)
	_, dec, _ := f.decisionNode(t)
	ad, err := f.svc.ComputeAutoDecision(f.ctx, dec)
	if err != nil {
		t.Fatal(err)
	}
	if ad.Decided {
		t.Fatalf("precondition: expected an undecided auto-decision (gate=unknown), got %+v", ad)
	}
	if err := f.svc.NotifyDecisionDeferred(f.ctx, dec, ad); err != nil {
		t.Fatal(err)
	}
	if f.disp.posts != 1 {
		t.Fatalf("an undecided decision must @mention a human, got %d posts", f.disp.posts)
	}
}
