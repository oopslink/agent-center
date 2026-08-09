package service

import (
	"sync"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func seedReviewRejectSeqPlan(t *testing.T, h *planAdvanceHarness) (pm.PlanID, pm.TaskID, pm.TaskID, pm.TaskID) {
	t.Helper()
	ctx := h.ctx
	pid, err := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "review-reject", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	dev := h.seedAssignedTask(t, pid, planID, "Dev", "user:dev")
	review := h.seedAssignedTask(t, pid, planID, "Review / final acceptance", "user:review")
	ship := h.seedAssignedTask(t, pid, planID, "Ship", "user:ship")
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: review, ToTaskID: dev, Kind: pm.EdgeSeq})
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: ship, ToTaskID: review, Kind: pm.EdgeSeq})
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	return planID, dev, review, ship
}

func completeBlockingReviewReject(t *testing.T, h *planAdvanceHarness, planID pm.PlanID, dev, review pm.TaskID) {
	t.Helper()
	ctx := h.ctx
	if d, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil || len(d) != 1 || d[0] != dev {
		t.Fatalf("initial advance = %v, %v; want Dev only", d, err)
	}
	h.setTaskStatus(t, dev, pm.TaskCompleted)
	if d, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil || len(d) != 1 || d[0] != review {
		t.Fatalf("after Dev completed advance = %v, %v; want Review only", d, err)
	}
	if err := h.svc.RecordReviewVerdict(ctx, review, pm.ReviewReject, true, "blocking defect", "sha-review", "user:a"); err != nil {
		t.Fatalf("RecordReviewVerdict(reject/blocking): %v", err)
	}
	h.setTaskStatus(t, review, pm.TaskCompleted)
}

func assertShipNotDispatchedAfterReject(t *testing.T, h *planAdvanceHarness, planID pm.PlanID, review, ship pm.TaskID) {
	t.Helper()
	recs, err := h.plans.ListDispatchRecords(h.ctx, planID)
	if err != nil {
		t.Fatalf("ListDispatchRecords: %v", err)
	}
	for _, rec := range recs {
		if rec.TaskID == ship {
			t.Fatalf("Ship was dispatched despite blocking Review REJECT; records=%+v", recs)
		}
	}
	reviewTask, err := h.tasks.FindByID(h.ctx, review)
	if err != nil {
		t.Fatalf("FindByID(review): %v", err)
	}
	if reviewTask.Status() != pm.TaskCompleted {
		t.Fatalf("Review task status = %s, want completed immutable execution record", reviewTask.Status())
	}
	p, err := h.plans.FindByID(h.ctx, planID)
	if err != nil {
		t.Fatalf("FindByID(plan): %v", err)
	}
	if p.Status() != pm.PlanRunning {
		t.Fatalf("plan status = %s, want running while blocking reject is unresolved", p.Status())
	}
}

func TestAdvancePlan_BlockingReviewRejectDoesNotSatisfySeqDependency(t *testing.T) {
	h, _ := planGraphSetup(t)
	planID, dev, review, ship := seedReviewRejectSeqPlan(t, h)
	completeBlockingReviewReject(t, h, planID, dev, review)

	dispatched, err := h.svc.AdvancePlan(h.ctx, planID, "user:a")
	if err != nil {
		t.Fatalf("AdvancePlan after blocking reject: %v", err)
	}
	if len(dispatched) != 0 {
		t.Fatalf("AdvancePlan dispatched %v after blocking Review REJECT, want none", dispatched)
	}
	assertShipNotDispatchedAfterReject(t, h, planID, review, ship)
}

func TestReconcileRunningPlans_BlockingReviewRejectDoesNotDispatchSeqDownstreamAfterRestart(t *testing.T) {
	h, _ := planGraphSetup(t)
	planID, dev, review, ship := seedReviewRejectSeqPlan(t, h)
	completeBlockingReviewReject(t, h, planID, dev, review)

	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatalf("ReconcileRunningPlans after blocking reject: %v", err)
	}
	assertShipNotDispatchedAfterReject(t, h, planID, review, ship)
	bo := blockedOnByTask(t, h, planID)
	if bo[ship].WaitType != pm.WaitUpstreamCompletion {
		t.Fatalf("Ship wait_type = %q, want upstream_completion", bo[ship].WaitType)
	}
	wantWaitKeys(t, bo[ship].WaitKeys, string(review))
}

func TestAdvancePlan_BlockingReviewRejectConcurrentReplayDoesNotDispatchSeqDownstream(t *testing.T) {
	h, _ := planGraphSetup(t)
	planID, dev, review, ship := seedReviewRejectSeqPlan(t, h)
	completeBlockingReviewReject(t, h, planID, dev, review)

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := h.svc.AdvancePlan(h.ctx, planID, "user:a")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent AdvancePlan: %v", err)
		}
	}
	assertShipNotDispatchedAfterReject(t, h, planID, review, ship)
}

func TestAdvancePlan_ReviewPassSatisfiesSeqDependency(t *testing.T) {
	h, _ := planGraphSetup(t)
	planID, dev, review, ship := seedReviewRejectSeqPlan(t, h)
	ctx := h.ctx
	if d, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil || len(d) != 1 || d[0] != dev {
		t.Fatalf("initial advance = %v, %v; want Dev only", d, err)
	}
	h.setTaskStatus(t, dev, pm.TaskCompleted)
	if d, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil || len(d) != 1 || d[0] != review {
		t.Fatalf("after Dev completed advance = %v, %v; want Review only", d, err)
	}
	if err := h.svc.RecordReviewVerdict(ctx, review, pm.ReviewPass, false, "approved", "sha-review", "user:a"); err != nil {
		t.Fatalf("RecordReviewVerdict(pass): %v", err)
	}
	h.setTaskStatus(t, review, pm.TaskCompleted)
	dispatched, err := h.svc.AdvancePlan(ctx, planID, "user:a")
	if err != nil {
		t.Fatalf("AdvancePlan after pass verdict: %v", err)
	}
	if len(dispatched) != 1 || dispatched[0] != ship {
		t.Fatalf("AdvancePlan after Review PASS dispatched %v, want Ship", dispatched)
	}
}
