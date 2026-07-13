package service

import (
	"context"
	"errors"
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// errBoom is the injected failure for the materialize error-propagation test.
var errBoom = errors.New("boom")

// failingPlanRepo wraps the real PlanRepo and fails ONE method (failOn) so a test can
// assert materializeBlockedOn propagates each repo error rather than swallowing it.
type failingPlanRepo struct {
	*pmsql.PlanRepo
	failOn string
}

func (r *failingPlanRepo) ListDependencies(ctx context.Context, planID pm.PlanID) ([]pm.Dependency, error) {
	if r.failOn == "deps" {
		return nil, errBoom
	}
	return r.PlanRepo.ListDependencies(ctx, planID)
}

func (r *failingPlanRepo) ListDispatchRecords(ctx context.Context, planID pm.PlanID) ([]pm.DispatchRecord, error) {
	if r.failOn == "records" {
		return nil, errBoom
	}
	return r.PlanRepo.ListDispatchRecords(ctx, planID)
}

func (r *failingPlanRepo) ListDecisionOutcomes(ctx context.Context, planID pm.PlanID) ([]pm.DecisionOutcome, error) {
	if r.failOn == "outcomes" {
		return nil, errBoom
	}
	return r.PlanRepo.ListDecisionOutcomes(ctx, planID)
}

func (r *failingPlanRepo) GetBlockedOn(ctx context.Context, planID pm.PlanID, taskID pm.TaskID) (pm.BlockedOn, bool, error) {
	if r.failOn == "get" {
		return pm.BlockedOn{}, false, errBoom
	}
	return r.PlanRepo.GetBlockedOn(ctx, planID, taskID)
}

func (r *failingPlanRepo) ClearBlockedOn(ctx context.Context, planID pm.PlanID, taskID pm.TaskID) error {
	if r.failOn == "clear" {
		return errBoom
	}
	return r.PlanRepo.ClearBlockedOn(ctx, planID, taskID)
}

func (r *failingPlanRepo) UpsertBlockedOn(ctx context.Context, b pm.BlockedOn) error {
	if r.failOn == "upsert" {
		return errBoom
	}
	return r.PlanRepo.UpsertBlockedOn(ctx, b)
}

// t0svc is a fixed instant for the direct classifyBlockedOn unit test (no clock).
var t0svc = time.Unix(1_700_000_000, 0).UTC()

// blockedOnByTask indexes a plan's BlockedOn snapshots by task id (test helper).
func blockedOnByTask(t *testing.T, h *planAdvanceHarness, planID pm.PlanID) map[pm.TaskID]pm.BlockedOn {
	t.Helper()
	list, err := h.plans.ListBlockedOn(h.ctx, planID)
	if err != nil {
		t.Fatalf("ListBlockedOn: %v", err)
	}
	out := make(map[pm.TaskID]pm.BlockedOn, len(list))
	for _, b := range list {
		out[b.TaskID] = b
	}
	return out
}

func wantWaitKeys(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("wait_keys = %v, want %v", got, want)
	}
	seen := map[string]bool{}
	for _, g := range got {
		seen[g] = true
	}
	for _, w := range want {
		if !seen[w] {
			t.Fatalf("wait_keys = %v, missing %q", got, w)
		}
	}
}

// TestBlockedOn_UpstreamCompletion_ExecutorLiveness_Lifecycle drives the full
// materialize→refresh→clear lifecycle over a two-node plan A→B (B depends_on A):
//   - upstream_completion: B is blocked on A (wait_keys=[A]).
//   - executor_liveness: A running holds a lease (wait_keys=[assignee]).
//   - clear: a node entering ready/running/terminal drops its snapshot.
func TestBlockedOn_UpstreamCompletion_ExecutorLiveness_Lifecycle(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "up", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:x")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:y")
	if err := h.svc.AddPlanDependency(ctx, planID, b, a, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)

	// Sweep #1: A dispatched (runnable → no snapshot); B blocked → upstream_completion.
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatalf("sweep #1: %v", err)
	}
	bo := blockedOnByTask(t, h, planID)
	if _, ok := bo[a]; ok {
		t.Fatalf("A (dispatched/runnable) should have NO snapshot, got %+v", bo[a])
	}
	if bo[b].WaitType != pm.WaitUpstreamCompletion {
		t.Fatalf("B wait_type = %q, want upstream_completion", bo[b].WaitType)
	}
	wantWaitKeys(t, bo[b].WaitKeys, string(a))
	if bo[b].NodeID == "" {
		t.Fatal("B snapshot missing node_id")
	}

	// A running → executor_liveness (lease held by its assignee); B unchanged.
	h.setTaskStatus(t, a, pm.TaskRunning)
	h.drain(t)
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatalf("sweep #2: %v", err)
	}
	bo = blockedOnByTask(t, h, planID)
	if bo[a].WaitType != pm.WaitExecutorLiveness {
		t.Fatalf("A wait_type = %q, want executor_liveness", bo[a].WaitType)
	}
	wantWaitKeys(t, bo[a].WaitKeys, "user:x")
	if bo[b].WaitType != pm.WaitUpstreamCompletion {
		t.Fatalf("B wait_type = %q, want upstream_completion (A not done)", bo[b].WaitType)
	}

	// A completed → A cleared (terminal); B becomes ready → dispatched → cleared.
	h.setTaskStatus(t, a, pm.TaskCompleted)
	h.drain(t)
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatalf("sweep #3: %v", err)
	}
	if bo := blockedOnByTask(t, h, planID); len(bo) != 0 {
		t.Fatalf("after A done + B dispatched, want NO snapshots, got %+v", bo)
	}
}

// TestBlockedOn_Idempotent asserts a re-run of the sweep produces NO churn: the
// snapshot is single-slot (never duplicated) and waited_since is PRESERVED across
// refreshes while the wait_type is unchanged — even as the clock advances.
func TestBlockedOn_Idempotent(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "idem", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:x")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:y")
	if err := h.svc.AddPlanDependency(ctx, planID, b, a, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)

	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatalf("sweep #1: %v", err)
	}
	first := blockedOnByTask(t, h, planID)[b]
	if first.WaitedSince.IsZero() {
		t.Fatal("B waited_since not stamped")
	}

	// Advance the clock and re-sweep with NO state change — the wait_type is unchanged,
	// so waited_since must be PRESERVED (the ongoing-wait基准), and there is still
	// exactly one row for B.
	h.clk.Advance(time.Hour)
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatalf("sweep #2: %v", err)
	}
	list, _ := h.plans.ListBlockedOn(ctx, planID)
	count := 0
	for _, x := range list {
		if x.TaskID == b {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("B snapshot count = %d, want exactly 1 (single-slot)", count)
	}
	second := blockedOnByTask(t, h, planID)[b]
	if !second.WaitedSince.Equal(first.WaitedSince) {
		t.Fatalf("waited_since churned on refresh: %v → %v (must be preserved)", first.WaitedSince, second.WaitedSince)
	}
}

// TestBlockedOn_HumanDecision covers BOTH human_decision shapes on a Dev→Review→
// Decision cycle with a conditional (pass→Integrate) branch: the Decision node
// itself (pending, no outcome) and the Integrate node blocked behind the unresolved
// decision condition.
func TestBlockedOn_HumanDecision(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "dec", CreatedBy: "user:a"})
	h.drain(t)
	dev, rev, dec, integ := buildGraphCycle(t, h, pid, planID)
	h.drain(t)

	// Drive Dev + Review to done so the Decision becomes the live node awaiting a ruling.
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil { // dispatch Dev
		t.Fatal(err)
	}
	h.setTaskStatus(t, dev, pm.TaskCompleted)
	h.drain(t)
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil { // dispatch Review
		t.Fatal(err)
	}
	h.setTaskStatus(t, rev, pm.TaskCompleted)
	h.drain(t)
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil { // dispatch Decision; classify
		t.Fatal(err)
	}

	bo := blockedOnByTask(t, h, planID)
	// The Decision node itself — pending, no recorded outcome → human_decision.
	if bo[dec].WaitType != pm.WaitHumanDecision {
		t.Fatalf("Decision wait_type = %q, want human_decision", bo[dec].WaitType)
	}
	wantWaitKeys(t, bo[dec].WaitKeys, string(dec))
	// Integrate — blocked behind the unresolved decision condition → human_decision.
	if bo[integ].WaitType != pm.WaitHumanDecision {
		t.Fatalf("Integrate wait_type = %q, want human_decision", bo[integ].WaitType)
	}
	wantWaitKeys(t, bo[integ].WaitKeys, string(dec))

	// Record a pass ruling + complete the Decision → the branch releases. The Decision's
	// snapshot clears (terminal) and Integrate is no longer waiting on a human decision
	// (this also exercises the outcome-indexing path in the materialize).
	if err := h.svc.RecordDecisionOutcome(ctx, dec, "pass", "user:a"); err != nil {
		t.Fatalf("RecordDecisionOutcome: %v", err)
	}
	h.setTaskStatus(t, dec, pm.TaskCompleted)
	h.drain(t)
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatal(err)
	}
	bo = blockedOnByTask(t, h, planID)
	if _, ok := bo[dec]; ok {
		t.Fatalf("Decision snapshot not cleared after completion: %+v", bo[dec])
	}
	if b, ok := bo[integ]; ok && b.WaitType == pm.WaitHumanDecision {
		t.Fatalf("Integrate still human_decision after the ruling: %+v", b)
	}
}

// TestBlockedOn_AcceptanceVerdict builds a merge-to-main node gated behind a decision
// acceptance condition: while the decision is unresolved the merge node is held by the
// T1041 acceptance HARD gate → acceptance_verdict (checked BEFORE the plain
// blocked-behind-decision path, since the acceptance gate is the more specific reason).
func TestBlockedOn_AcceptanceVerdict(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "acc", CreatedBy: "user:a"})
	h.drain(t)
	dev := h.seedAssignedTask(t, pid, planID, "Dev", "user:dev")
	dec := h.seedAssignedTask(t, pid, planID, "Decision", "user:pd")
	// The gated node is a MERGE-to-main ship node — the title triggers RequiresAcceptance,
	// so buildPlanGraph auto-stamps TagMergeToMain and the run-gate hard-gates it.
	merge := h.seedAssignedTask(t, pid, planID, "merge to main", "user:int")
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: dec, ToTaskID: dev, Kind: pm.EdgeSeq})
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: merge, ToTaskID: dec, Kind: pm.EdgeConditional, When: "pass"})
	// A plain business (seq) upstream too, so upstreamConditionNodeIDs must SKIP a
	// non-condition upstream and return only the acceptance gate node.
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: merge, ToTaskID: dev, Kind: pm.EdgeSeq})
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)

	// Confirm the run-gate actually hard-gates the merge node (the acceptance gate we
	// are classifying really holds it) — this is the observed-reason source of truth.
	if err := h.svc.EnsureTaskRunnable(ctx, merge); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("merge node EnsureTaskRunnable = %v, want ErrTaskNotRunnable (acceptance gate)", err)
	}

	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatal(err)
	}
	bo := blockedOnByTask(t, h, planID)
	if bo[merge].WaitType != pm.WaitAcceptanceVerdict {
		t.Fatalf("merge wait_type = %q, want acceptance_verdict", bo[merge].WaitType)
	}
	if len(bo[merge].WaitKeys) == 0 {
		t.Fatal("acceptance_verdict wait_keys empty — want the upstream gate node id")
	}
}

// TestBlockedOn_StageBarrier: a downstream stage's ENTRY node, held behind the
// upstream stage's unresolved gate barrier, classifies as stage_barrier even though
// the stage-UNAWARE derived view calls it `ready`.
func TestBlockedOn_StageBarrier(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)
	a1, a2, b1, _, _, _ := seedTwoMultiNodeStagePlan(t, h, pid, planID, 3)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)

	// Drive stage A's members to done — its gate is still unresolved, so b1 (stage B
	// entry) is held by the barrier.
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, a1, pm.TaskCompleted)
	h.drain(t)
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, a2, pm.TaskCompleted)
	h.drain(t)
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatal(err)
	}

	// Sanity: the run-gate holds b1 behind the barrier (the reason we classify).
	if err := h.svc.EnsureTaskRunnable(ctx, b1); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("b1 EnsureTaskRunnable = %v, want ErrTaskNotRunnable (stage barrier)", err)
	}
	bo := blockedOnByTask(t, h, planID)
	if bo[b1].WaitType != pm.WaitStageBarrier {
		t.Fatalf("b1 wait_type = %q, want stage_barrier", bo[b1].WaitType)
	}
	if len(bo[b1].WaitKeys) == 0 {
		t.Fatal("stage_barrier wait_keys empty — want the upstream stage gate node id")
	}
}

// TestBlockedOn_NoGateRegression proves materialization is PURE OBSERVATION: running
// the sweep (which materializes BlockedOn) does NOT change any run-gate outcome. The
// gated node stays not-runnable and the runnable node stays runnable — before AND
// after the sweep — and the ready-set / dispatch is unaffected.
func TestBlockedOn_NoGateRegression(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "reg", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:x")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:y")
	if err := h.svc.AddPlanDependency(ctx, planID, b, a, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)

	// A dispatched → runnable; B blocked → not runnable (before any materialize).
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.EnsureTaskRunnable(ctx, b); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("B runnable before, want not-runnable: %v", err)
	}

	// Re-sweep (more materialize) must NOT change the gate outcome for either node.
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.EnsureTaskRunnable(ctx, b); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("B runnable after materialize — gate regressed: %v", err)
	}
	// A is dispatched (deps satisfied) → runnable; materialize must not have flipped it.
	if err := h.svc.EnsureTaskRunnable(ctx, a); err != nil {
		t.Fatalf("A not runnable after materialize — gate regressed: %v", err)
	}
}

// TestBlockedOn_SkipsBuiltinPool asserts a builtin pool plan materializes NO
// snapshots (it is flat — no upstream / no gates to classify).
func TestBlockedOn_SkipsBuiltinPool(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	pool := findBuiltinPlan(t, h, pid) // the per-project builtin assignment pool (always running)
	_ = h.seedAssignedTask(t, pid, pool.ID(), "pool-task", "user:x")
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if list, _ := h.plans.ListBlockedOn(ctx, pool.ID()); len(list) != 0 {
		t.Fatalf("builtin pool materialized %d snapshots, want 0 (flat, no gates)", len(list))
	}
}

// TestBlockedOn_Materialize_PropagatesRepoErrors asserts materializeBlockedOn returns
// (never swallows) a repo error at each of its persistence touch-points — so the
// best-effort wrapper in ReconcileRunningPlans can log it rather than silently drop a
// classification. An A→B plan post-dispatch exercises BOTH the clear path (A,
// dispatched→runnable) and the materialize path (B, blocked→get+upsert).
func TestBlockedOn_Materialize_PropagatesRepoErrors(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "err", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:x")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:y")
	if err := h.svc.AddPlanDependency(ctx, planID, b, a, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil { // A dispatched, B blocked
		t.Fatal(err)
	}
	p, _ := h.plans.FindByID(ctx, planID)

	real := h.svc.plans
	defer func() { h.svc.plans = real }()
	for _, failOn := range []string{"deps", "records", "outcomes", "clear", "get", "upsert"} {
		h.svc.plans = &failingPlanRepo{PlanRepo: h.plans, failOn: failOn}
		if err := h.svc.materializeBlockedOn(ctx, p); !errors.Is(err, errBoom) {
			t.Errorf("materialize failOn=%q err = %v, want errBoom", failOn, err)
		}
	}
	h.svc.plans = real

	// The task-list load also propagates (the first repo touch in materialize).
	realTasks := h.svc.tasks
	defer func() { h.svc.tasks = realTasks }()
	h.svc.tasks = &failingTaskRepo{TaskRepo: h.tasks}
	if err := h.svc.materializeBlockedOn(ctx, p); !errors.Is(err, errBoom) {
		t.Errorf("materialize task-list err = %v, want errBoom", err)
	}
}

// failingTaskRepo wraps the real TaskRepo and fails ListByPlan (materialize's first
// repo touch) so the propagation test covers that branch too.
type failingTaskRepo struct {
	*pmsql.TaskRepo
}

func (r *failingTaskRepo) ListByPlan(ctx context.Context, planID pm.PlanID) ([]*pm.Task, error) {
	return nil, errBoom
}

// TestBlockedOn_Classify_Fallbacks unit-tests classifyBlockedOn's terminal-clear,
// ready-clear, and timeout_only fallback branches directly (the fallback is a
// defensive path not reachable through the normal node-status enum).
func TestBlockedOn_Classify_Fallbacks(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	// A bare (non-merge, unbound) task + empty plan/edges is enough for these branches:
	// none of them touch the graph gates (a non-merge task short-circuits
	// acceptanceVerdictBlocks, and an unbound node short-circuits stageGateBlocks).
	plan, _ := pm.NewPlan(pm.NewPlanInput{ID: "PL-x", ProjectID: "P1", Name: "n", CreatorRef: "user:a", CreatedAt: t0svc})
	task, _ := pm.NewTask(pm.NewTaskInput{ID: "T1", ProjectID: "P1", Title: "t", CreatedBy: "user:a", CreatedAt: t0svc})

	// terminal → clear.
	if _, clear, err := h.svc.classifyBlockedOn(ctx, plan, task, pm.PlanNodeView{TaskID: "T1", NodeStatus: pm.NodeDone}, nil, nil, nil); err != nil || !clear {
		t.Fatalf("NodeDone classify = (clear=%v, err=%v), want clear", clear, err)
	}
	// ready → clear (runnable, awaiting pickup).
	if _, clear, err := h.svc.classifyBlockedOn(ctx, plan, task, pm.PlanNodeView{TaskID: "T1", NodeStatus: pm.NodeReady}, nil, nil, nil); err != nil || !clear {
		t.Fatalf("NodeReady classify = (clear=%v, err=%v), want clear", clear, err)
	}
	// unknown non-terminal state → timeout_only fallback (defensive).
	cls, clear, err := h.svc.classifyBlockedOn(ctx, plan, task, pm.PlanNodeView{TaskID: "T1", NodeStatus: pm.NodeStatus("weird")}, nil, nil, nil)
	if err != nil || clear {
		t.Fatalf("unknown status classify = (clear=%v, err=%v), want a snapshot", clear, err)
	}
	if cls.waitType != pm.WaitTimeoutOnly {
		t.Fatalf("unknown status wait_type = %q, want timeout_only", cls.waitType)
	}

	// paused (a running task holds a lease too) → executor_liveness; with no assignee
	// the wait_keys are empty (the empty-assignee branch).
	cls, clear, err = h.svc.classifyBlockedOn(ctx, plan, task, pm.PlanNodeView{TaskID: "T1", NodeStatus: pm.NodePaused}, nil, nil, nil)
	if err != nil || clear {
		t.Fatalf("NodePaused classify = (clear=%v, err=%v), want a snapshot", clear, err)
	}
	if cls.waitType != pm.WaitExecutorLiveness || len(cls.waitKeys) != 0 {
		t.Fatalf("NodePaused (no assignee) = %+v, want executor_liveness with empty keys", cls)
	}

	// upstreamConditionNodeIDs nil-guard: an unbound node (no graph_id / node_id) returns
	// no keys without touching the engine.
	if keys, err := h.svc.upstreamConditionNodeIDs(ctx, plan, task, false); err != nil || keys != nil {
		t.Fatalf("upstreamConditionNodeIDs(unbound) = (%v, %v), want (nil, nil)", keys, err)
	}
}
