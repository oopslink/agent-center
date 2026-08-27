package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func TestProgressControl_StaleFenceConflictPersistsWithoutMutation(t *testing.T) {
	h, planID, taskID := progressPlanWithOneTask(t)
	p, err := h.plans.FindByID(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	h.svc.progressControllerID = "controller-a"
	oldFence, ok, err := h.svc.acquireProgressFence(h.ctx, p, time.Minute)
	if err != nil || !ok {
		t.Fatalf("acquire old fence = (%+v,%v,%v)", oldFence, ok, err)
	}
	h.clk.Advance(2 * time.Minute)
	h.svc.progressControllerID = "controller-b"
	newFence, ok, err := h.svc.acquireProgressFence(h.ctx, p, time.Minute)
	if err != nil || !ok {
		t.Fatalf("takeover fence = (%+v,%v,%v)", newFence, ok, err)
	}
	if newFence.FencingToken <= oldFence.FencingToken {
		t.Fatalf("takeover fencing token=%d old=%d, want monotonic increase", newFence.FencingToken, oldFence.FencingToken)
	}
	if err := h.svc.ReconcilePlanProgressWithFence(h.ctx, planID, oldFence); !errors.Is(err, pm.ErrProgressFenceStale) {
		t.Fatalf("stale reconcile err=%v, want ErrProgressFenceStale", err)
	}
	incs, _ := h.plans.ListOpenProgressIncidents(h.ctx, planID)
	if incidentsOfKind(incs, pm.IncidentLeaseFenceConflict) != 0 {
		t.Fatalf("stale writer created lease_fence_conflict incident: %+v", incs)
	}
	if _, ok, err := h.plans.LatestProgressObservation(h.ctx, planID, taskID); err != nil || ok {
		t.Fatalf("stale writer created observation = ok %v err %v, want no stale mutation", ok, err)
	}
}

func TestProgressControl_ConcurrentFenceTakeoverRejectsStaleWriter(t *testing.T) {
	h, planID, taskID := progressPlanWithOneTask(t)
	p, err := h.plans.FindByID(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	h.svc.progressControllerID = "controller-a"
	staleFence, ok, err := h.svc.acquireProgressFence(h.ctx, p, time.Minute)
	if err != nil || !ok {
		t.Fatalf("acquire stale fence = (%+v,%v,%v)", staleFence, ok, err)
	}
	h.clk.Advance(2 * time.Minute)
	h.svc.progressControllerID = "controller-b"

	start := make(chan struct{})
	taken := make(chan struct{})
	var wg sync.WaitGroup
	var staleErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		<-taken
		staleErr = h.svc.ReconcilePlanProgressWithFence(h.ctx, planID, staleFence)
	}()
	go func() {
		defer wg.Done()
		<-start
		if _, ok, err := h.svc.acquireProgressFence(h.ctx, p, time.Minute); err != nil || !ok {
			t.Errorf("takeover fence = ok %v err %v", ok, err)
		}
		close(taken)
	}()
	close(start)
	wg.Wait()

	if !errors.Is(staleErr, pm.ErrProgressFenceStale) {
		t.Fatalf("stale concurrent reconcile err=%v, want ErrProgressFenceStale", staleErr)
	}
	if _, ok, err := h.plans.LatestProgressObservation(h.ctx, planID, taskID); err != nil || ok {
		t.Fatalf("stale concurrent writer created observation ok=%v err=%v", ok, err)
	}
	if incs, _ := h.plans.ListOpenProgressIncidents(h.ctx, planID); incidentsOfKind(incs, pm.IncidentLeaseFenceConflict) != 0 {
		t.Fatalf("stale concurrent writer created incident: %+v", incs)
	}
}

func TestProgressControl_WatchdogIndependentFromPlanReconcile(t *testing.T) {
	h, planID, _ := progressPlanWithOneTask(t)
	old := h.clk.Now().Add(-10 * time.Minute)
	if err := h.plans.RecordProgressWatchdogHeartbeat(h.ctx, planID, "dead_component", old); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatal(err)
	}
	incs, _ := h.plans.ListOpenProgressIncidents(h.ctx, planID)
	if incidentsOfKind(incs, pm.IncidentWatchdogSilent) != 0 {
		t.Fatal("watchdog still hangs off ReconcileRunningPlans")
	}
	if err := h.svc.ProgressWatchdogTick(h.ctx, time.Minute); err != nil {
		t.Fatal(err)
	}
	incs, _ = h.plans.ListOpenProgressIncidents(h.ctx, planID)
	if incidentsOfKind(incs, pm.IncidentWatchdogSilent) != 1 {
		t.Fatalf("independent watchdog incidents=%+v, want one", incs)
	}
}

func TestProgressControl_WatchdogSilentCreatesServiceIncident(t *testing.T) {
	h, planID, _ := progressPlanWithOneTask(t)
	old := h.clk.Now().Add(-10 * time.Minute)
	if err := h.plans.RecordProgressWatchdogHeartbeat(h.ctx, planID, "progress_reconciler", old); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if err := h.svc.ProgressWatchdogTick(h.ctx, time.Minute); err != nil {
		t.Fatalf("watchdog tick: %v", err)
	}
	if err := h.svc.ProgressWatchdogTick(h.ctx, time.Minute); err != nil {
		t.Fatalf("watchdog tick replay: %v", err)
	}
	incs, _ := h.plans.ListOpenProgressIncidents(h.ctx, planID)
	if incidentsOfKind(incs, pm.IncidentWatchdogSilent) != 1 {
		t.Fatalf("watchdog_silent incidents=%+v, want one service-owned incident", incs)
	}
}

func TestProgressControl_WatchdogTreatsMissingHeartbeatAsSilent(t *testing.T) {
	h, planID, _ := progressPlanWithOneTask(t)
	h.clk.Advance(10 * time.Minute)
	if err := h.svc.ProgressWatchdogTick(h.ctx, time.Minute); err != nil {
		t.Fatal(err)
	}
	incs, _ := h.plans.ListOpenProgressIncidents(h.ctx, planID)
	if incidentsOfKind(incs, pm.IncidentWatchdogSilent) != 1 {
		t.Fatalf("missing-heartbeat incidents=%+v, want fail-closed watchdog_silent", incs)
	}
}

func TestProgressControl_WakeBucketP0ReserveAndAutoResume(t *testing.T) {
	h, planID, _ := progressPlanWithOneTask(t)
	base := ProgressWakeAttempt{
		PlanID: planID, OrganizationID: "org-progress", OwnerRef: "agent:owner",
		Capacity: 2, ReservedP0: 1, RefillPerMinute: 2,
	}
	first, err := h.svc.RecordProgressWakeAttempt(h.ctx, withWakeKey(base, pm.ProgressWakeSeverityDefault, "default-1"))
	if err != nil || !first.Allowed {
		t.Fatalf("first default wake = %+v err=%v, want allowed", first, err)
	}
	suppressed, err := h.svc.RecordProgressWakeAttempt(h.ctx, withWakeKey(base, pm.ProgressWakeSeverityDefault, "default-2"))
	if err != nil || suppressed.Allowed {
		t.Fatalf("second default wake = %+v err=%v, want suppressed for P0 reserve", suppressed, err)
	}
	p0, err := h.svc.RecordProgressWakeAttempt(h.ctx, withWakeKey(base, pm.ProgressWakeSeverityP0, "p0-1"))
	if err != nil || !p0.Allowed {
		t.Fatalf("P0 wake = %+v err=%v, want allowed from reserve", p0, err)
	}
	obs, _ := h.plans.ListOpenProgressObligations(h.ctx, planID)
	if obligationsOfKind(obs, pm.ObligationAckWake) != 1 {
		t.Fatalf("suppressed wake obligations=%+v, want one ack_wake obligation", obs)
	}
	h.clk.Advance(61 * time.Second)
	recovered, err := h.svc.RecordProgressWakeAttempt(h.ctx, withWakeKey(base, pm.ProgressWakeSeverityDefault, "default-3"))
	if err != nil || !recovered.Allowed {
		t.Fatalf("refilled default wake = %+v err=%v, want auto-resumed delivery", recovered, err)
	}
	diags, _ := h.plans.ListProgressWakeBucketDiagnostics(h.ctx, planID)
	if len(diags) != 4 {
		t.Fatalf("wake diagnostics rows=%d %+v, want all attempts recorded", len(diags), diags)
	}
}

func TestProgressControl_WakeStormAggregates1000PlansAndDurablyDrains(t *testing.T) {
	h, planID, _ := progressPlanWithOneTask(t)
	base := ProgressWakeAttempt{PlanID: planID, OrganizationID: "org-progress", OwnerRef: "agent:owner",
		Severity: pm.ProgressWakeSeverityDefault, Channel: "agent-control", Capacity: 1, RefillPerMinute: 1}
	if d, err := h.svc.RecordProgressWakeAttempt(h.ctx, withWakeKey(base, base.Severity, "prime")); err != nil || !d.Allowed {
		t.Fatalf("prime=%+v err=%v", d, err)
	}
	for i := 0; i < 1000; i++ {
		a := base
		a.PlanID = pm.PlanID(fmt.Sprintf("storm-plan-%04d", i))
		a.IdempotencyKey = fmt.Sprintf("storm-%04d", i)
		d, err := h.svc.RecordProgressWakeAttempt(h.ctx, a)
		if err != nil || d.Allowed {
			t.Fatalf("attempt %d=%+v err=%v, want suppressed", i, d, err)
		}
	}
	h.clk.Advance(61 * time.Second)
	due, err := h.plans.ListDueProgressSuppressedWakes(h.ctx, h.clk.Now(), 10)
	if err != nil || len(due) != 1 {
		t.Fatalf("due lanes=%d err=%v, want one", len(due), err)
	}
	if len(due[0].PlanIDs) != 1000 {
		t.Fatalf("aggregated plans=%d, want 1000", len(due[0].PlanIDs))
	}
	delivered := 0
	if err := h.svc.DrainProgressSuppressedWakes(h.ctx, 10, func(_ context.Context, wake pm.ProgressSuppressedWake) error {
		delivered++
		if len(wake.PlanIDs) != 1000 {
			t.Fatalf("drain plans=%d", len(wake.PlanIDs))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if delivered != 1 {
		t.Fatalf("deliveries=%d, want one aggregated delivery", delivered)
	}
	due, _ = h.plans.ListDueProgressSuppressedWakes(h.ctx, h.clk.Now().Add(time.Hour), 10)
	if len(due) != 0 {
		t.Fatalf("delivered intent survived: %+v", due)
	}
}

func withWakeKey(a ProgressWakeAttempt, sev pm.ProgressWakeSeverity, key string) ProgressWakeAttempt {
	a.Severity = sev
	a.IdempotencyKey = key
	return a
}
