package service

import (
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
	if err := h.svc.ReconcilePlanProgressWithFence(h.ctx, planID, oldFence); err != nil {
		t.Fatalf("stale reconcile: %v", err)
	}
	incs, _ := h.plans.ListOpenProgressIncidents(h.ctx, planID)
	if incidentsOfKind(incs, pm.IncidentLeaseFenceConflict) != 1 {
		t.Fatalf("lease_fence_conflict incidents=%+v, want one durable incident", incs)
	}
	if _, ok, err := h.plans.LatestProgressObservation(h.ctx, planID, taskID); err != nil || ok {
		t.Fatalf("stale writer created observation = ok %v err %v, want no stale mutation", ok, err)
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

func withWakeKey(a ProgressWakeAttempt, sev pm.ProgressWakeSeverity, key string) ProgressWakeAttempt {
	a.Severity = sev
	a.IdempotencyKey = key
	return a
}
