package service

import (
	"errors"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// =============================================================================
// I103 §2/§6 — BlockedOn READ enrichment tests: the materialized 旁路 snapshots are
// re-read into the get_plan detail (per-node + frontier + pending-decision), on BOTH
// the FE (GetPlanDetail) and agent (GetPlanDetailForMember) paths. PURE READ — these
// assert the read only re-surfaces what reconcile materialized; it changes no gating.
// =============================================================================

// TestBlockedOnRead_UpstreamCompletion_SurfacedOnBothReadPaths builds an A→B plan,
// reconciles to materialize (B waits upstream_completion on A), then asserts BOTH
// get_plan read paths carry B's snapshot, the frontier groups it under
// upstream_completion, and the pending-decision queue is empty (no human ruling).
func TestBlockedOnRead_UpstreamCompletion_SurfacedOnBothReadPaths(t *testing.T) {
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
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	// Both read paths must carry the SAME frontier truth.
	feDetail, err := h.svc.GetPlanDetail(ctx, planID)
	if err != nil {
		t.Fatalf("GetPlanDetail: %v", err)
	}
	agentDetail, err := h.svc.GetPlanDetailForMember(ctx, planID, "user:a")
	if err != nil {
		t.Fatalf("GetPlanDetailForMember: %v", err)
	}
	for name, detail := range map[string]*PlanDetail{"GetPlanDetail": feDetail, "GetPlanDetailForMember": agentDetail} {
		byTask := map[pm.TaskID]pm.BlockedOn{}
		for _, bo := range detail.BlockedOn {
			byTask[bo.TaskID] = bo
		}
		// A is dispatched/runnable → NO snapshot; B waits upstream_completion on A.
		if _, ok := byTask[a]; ok {
			t.Errorf("%s: A (runnable) should carry no blocked_on, got %+v", name, byTask[a])
		}
		bBO, ok := byTask[b]
		if !ok {
			t.Fatalf("%s: B missing blocked_on snapshot; got %+v", name, detail.BlockedOn)
		}
		if bBO.WaitType != pm.WaitUpstreamCompletion {
			t.Errorf("%s: B wait_type = %q, want upstream_completion", name, bBO.WaitType)
		}
		if bBO.WaitedSince.IsZero() {
			t.Errorf("%s: B waited_since not surfaced", name)
		}

		// Frontier: one group (upstream_completion) holding B, total 1.
		f := FrontierOf(detail)
		if f.Total != 1 || len(f.Groups) != 1 {
			t.Fatalf("%s: frontier total=%d groups=%d, want 1/1", name, f.Total, len(f.Groups))
		}
		if f.Groups[0].WaitType != pm.WaitUpstreamCompletion || len(f.Groups[0].Nodes) != 1 || f.Groups[0].Nodes[0].TaskID != b {
			t.Errorf("%s: frontier group = %+v, want [upstream_completion:B]", name, f.Groups[0])
		}
		// No human ruling pending.
		if pend := PendingDecisionsOf(detail); len(pend) != 0 {
			t.Errorf("%s: pending_decisions = %+v, want empty", name, pend)
		}
	}
}

// TestBlockedOnRead_BuiltinPoolSkipped asserts the builtin assignment pool (FLAT, no
// graph → nothing materialized) surfaces no blocked_on frontier — the read skips it,
// leaving BlockedOn nil (zero-regression for the pool board).
func TestBlockedOnRead_BuiltinPoolSkipped(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	pool := findBuiltinPlan(t, h, pid)
	detail, err := h.svc.GetPlanDetail(ctx, pool.ID())
	if err != nil {
		t.Fatalf("GetPlanDetail(pool): %v", err)
	}
	if detail.BlockedOn != nil {
		t.Errorf("builtin pool BlockedOn = %+v, want nil (skipped)", detail.BlockedOn)
	}
	if f := FrontierOf(detail); f.Total != 0 {
		t.Errorf("builtin pool frontier total = %d, want 0", f.Total)
	}
}

// TestBlockedOnRead_NilDetailGuards pins the nil-safe accessors + the fillBlockedOn nil
// guard (a nil detail yields an empty frontier / nil queue and no error — no panic).
func TestBlockedOnRead_NilDetailGuards(t *testing.T) {
	if f := FrontierOf(nil); f.Total != 0 || len(f.Groups) != 0 {
		t.Errorf("FrontierOf(nil) = %+v, want empty", f)
	}
	if pend := PendingDecisionsOf(nil); pend != nil {
		t.Errorf("PendingDecisionsOf(nil) = %+v, want nil", pend)
	}
	h, _ := planGraphSetup(t)
	if err := h.svc.fillBlockedOn(h.ctx, nil); err != nil {
		t.Errorf("fillBlockedOn(nil) = %v, want nil (guard)", err)
	}
}

// TestBlockedOnRead_PropagatesListError asserts the read fails LOUDLY when the blocked_on
// store read errors — the frontier is never silently dropped.
func TestBlockedOnRead_PropagatesListError(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "err", CreatedBy: "user:a"})
	h.drain(t)
	_ = h.seedAssignedTask(t, pid, planID, "A", "user:x")
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	p, _ := h.plans.FindByID(ctx, planID)

	real := h.svc.plans
	defer func() { h.svc.plans = real }()
	h.svc.plans = &failingPlanRepo{PlanRepo: h.plans, failOn: "list"}
	if err := h.svc.fillBlockedOn(ctx, &PlanDetail{Plan: p}); !errors.Is(err, errBoom) {
		t.Fatalf("fillBlockedOn ListBlockedOn err = %v, want errBoom", err)
	}
}

// TestBlockedOnRead_PendingDecisionQueue drives a Dev→Review→Decision cycle to the
// decision-pending point and asserts the read face exposes the human_decision waits as
// the pending-decision queue (and groups them in the frontier).
func TestBlockedOnRead_PendingDecisionQueue(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "dec", CreatedBy: "user:a"})
	h.drain(t)
	dev, rev, dec, integ := buildGraphCycle(t, h, pid, planID)
	h.drain(t)

	// Drive Dev + Review to done so the Decision is the live node awaiting a ruling.
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, dev, pm.TaskCompleted)
	h.drain(t)
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, rev, pm.TaskCompleted)
	h.drain(t)
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatal(err)
	}

	detail, err := h.svc.GetPlanDetailForMember(ctx, planID, "user:a")
	if err != nil {
		t.Fatalf("GetPlanDetailForMember: %v", err)
	}
	pend := PendingDecisionsOf(detail)
	got := map[pm.TaskID]bool{}
	for _, b := range pend {
		if b.WaitType != pm.WaitHumanDecision {
			t.Errorf("pending queue has non-human_decision wait: %+v", b)
		}
		got[b.TaskID] = true
	}
	// The Decision node itself (pending ruling) + Integrate (blocked behind it).
	if !got[dec] {
		t.Errorf("pending_decisions missing the Decision node %s; got %+v", dec, pend)
	}
	if !got[integ] {
		t.Errorf("pending_decisions missing the Integrate node %s (blocked behind the decision); got %+v", integ, pend)
	}
	// The frontier must contain a human_decision group covering exactly the pending queue.
	f := FrontierOf(detail)
	var human *pm.FrontierGroup
	for i := range f.Groups {
		if f.Groups[i].WaitType == pm.WaitHumanDecision {
			human = &f.Groups[i]
		}
	}
	if human == nil {
		t.Fatalf("frontier has no human_decision group; groups=%+v", f.Groups)
	}
	if len(human.Nodes) != len(pend) {
		t.Errorf("human_decision group size = %d, want == pending queue size %d", len(human.Nodes), len(pend))
	}
}

// TestBlockedOnRead_EmptyBoundaries covers the boundary cases: a graphed plan with NO
// blocked nodes (never reconciled → no snapshots) and a plan whose only node is TERMINAL
// both surface an empty frontier / nil queue — the read adds nothing when there is
// nothing un-advanced.
func TestBlockedOnRead_EmptyBoundaries(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})

	// (a) A started single-node plan whose node completes → the node is terminal, the
	// sweep clears any snapshot → empty frontier.
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "term", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "solo", "user:x")
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	h.setTaskStatus(t, a, pm.TaskCompleted)
	h.drain(t)
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	detail, err := h.svc.GetPlanDetail(ctx, planID)
	if err != nil {
		t.Fatalf("GetPlanDetail: %v", err)
	}
	if len(detail.BlockedOn) != 0 {
		t.Errorf("all-terminal plan carries snapshots: %+v", detail.BlockedOn)
	}
	if f := FrontierOf(detail); f.Total != 0 || len(f.Groups) != 0 {
		t.Errorf("all-terminal frontier = %+v, want empty", f)
	}
	if pend := PendingDecisionsOf(detail); pend != nil {
		t.Errorf("all-terminal pending_decisions = %+v, want nil", pend)
	}

	// (b) A brand-new draft plan with no tasks and never started → nil BlockedOn.
	empty, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "empty", CreatedBy: "user:a"})
	h.drain(t)
	ed, err := h.svc.GetPlanDetail(ctx, empty)
	if err != nil {
		t.Fatalf("GetPlanDetail(empty): %v", err)
	}
	if ed.BlockedOn != nil {
		t.Errorf("empty plan BlockedOn = %+v, want nil", ed.BlockedOn)
	}
	if f := FrontierOf(ed); f.Total != 0 {
		t.Errorf("empty plan frontier total = %d, want 0", f.Total)
	}
}
