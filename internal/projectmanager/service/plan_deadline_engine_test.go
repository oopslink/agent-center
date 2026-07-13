package service

import (
	"context"
	"errors"
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// captureSink records every TimeoutEvent the router hands it (and can inject an error
// to exercise the best-effort swallow path).
type captureSink struct {
	events []pm.TimeoutEvent
	err    error
}

func (c *captureSink) OnTimeout(_ context.Context, ev pm.TimeoutEvent) error {
	c.events = append(c.events, ev)
	return c.err
}

func (c *captureSink) byTask(id pm.TaskID) (pm.TimeoutEvent, bool) {
	for _, e := range c.events {
		if e.TaskID == id {
			return e, true
		}
	}
	return pm.TimeoutEvent{}, false
}

// ListBlockedOn override for the shared failingPlanRepo (defined in
// plan_blocked_on_test.go) — lets the router error-propagation test fail the list read.
func (r *failingPlanRepo) ListBlockedOn(ctx context.Context, planID pm.PlanID) ([]pm.BlockedOn, error) {
	if r.failOn == "list" {
		return nil, errBoom
	}
	return r.PlanRepo.ListBlockedOn(ctx, planID)
}

// seedBlockedPlanAB builds a running A→B plan (B depends_on A), runs one sweep so A is
// dispatched (runnable, no snapshot) and B is blocked (upstream_completion), and returns
// the two task ids. A stays un-completed so B's wait persists across sweeps.
func seedBlockedPlanAB(t *testing.T, h *planAdvanceHarness, name string) (pm.ProjectID, pm.PlanID, pm.TaskID, pm.TaskID) {
	t.Helper()
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: name, CreatedBy: "user:a"})
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
		t.Fatalf("seed sweep: %v", err)
	}
	return pid, planID, a, b
}

// TestDeadline_Assign_And_NoDrift: the materialize ASSIGNS B's deadline (waited_since +
// the per-type timeout) and on_timeout action, and a re-sweep BEFORE the deadline (clock
// advanced but still short of it) leaves the deadline UNCHANGED — no drift, no probe.
func TestDeadline_Assign_And_NoDrift(t *testing.T) {
	h, _ := planGraphSetup(t)
	h.svc.deadlinePolicy = pm.DeadlinePolicy{
		Default: pm.WaitDeadline{Timeout: time.Hour, OnTimeout: pm.TimeoutReprobe},
	}
	_, planID, _, b := seedBlockedPlanAB(t, h, "assign")

	bo := blockedOnByTask(t, h, planID)[b]
	if bo.WaitType != pm.WaitUpstreamCompletion {
		t.Fatalf("B wait_type = %q, want upstream_completion", bo.WaitType)
	}
	wantDeadline := bo.WaitedSince.Add(time.Hour)
	if !bo.Deadline.Equal(wantDeadline) {
		t.Fatalf("B deadline = %v, want waited_since+1h = %v", bo.Deadline, wantDeadline)
	}
	if bo.OnTimeout != string(pm.TimeoutReprobe) {
		t.Fatalf("B on_timeout = %q, want reprobe", bo.OnTimeout)
	}
	if bo.ProbeCount != 0 || !bo.LastProbeAt.IsZero() {
		t.Fatalf("B probed before deadline: count=%d last=%v", bo.ProbeCount, bo.LastProbeAt)
	}

	// Advance 30m (< 1h) and re-sweep: still an ongoing upstream_completion wait, so
	// waited_since is preserved and the deadline recomputes to the SAME instant.
	h.clk.Advance(30 * time.Minute)
	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatalf("resweep: %v", err)
	}
	bo2 := blockedOnByTask(t, h, planID)[b]
	if !bo2.Deadline.Equal(wantDeadline) {
		t.Fatalf("deadline drifted: %v → %v", wantDeadline, bo2.Deadline)
	}
	if bo2.ProbeCount != 0 {
		t.Fatalf("probed before deadline elapsed: count=%d", bo2.ProbeCount)
	}
}

// TestDeadline_Routes_OnTimeout_PerAction drives every on_timeout branch: with the clock
// advanced PAST the deadline, a sweep records the probe (probe_count=1, last_probe_at=now)
// and routes the configured action to the sink.
func TestDeadline_Routes_OnTimeout_PerAction(t *testing.T) {
	for _, action := range []pm.TimeoutAction{pm.TimeoutReprobe, pm.TimeoutEscalate, pm.TimeoutRouteToHandler} {
		action := action
		t.Run(string(action), func(t *testing.T) {
			h, _ := planGraphSetup(t)
			sink := &captureSink{}
			h.svc.timeoutSink = sink
			h.svc.deadlinePolicy = pm.DeadlinePolicy{
				Default: pm.WaitDeadline{Timeout: time.Hour, OnTimeout: action},
			}
			_, planID, _, b := seedBlockedPlanAB(t, h, "route")

			// Past the deadline → route.
			h.clk.Advance(2 * time.Hour)
			now := h.clk.Now()
			if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
				t.Fatalf("route sweep: %v", err)
			}
			ev, ok := sink.byTask(b)
			if !ok {
				t.Fatalf("sink got no event for B; events=%+v", sink.events)
			}
			if ev.Action != action {
				t.Fatalf("routed action = %q, want %q", ev.Action, action)
			}
			if ev.WaitType != pm.WaitUpstreamCompletion || ev.ProbeCount != 1 {
				t.Fatalf("event = %+v, want upstream_completion probe#1", ev)
			}
			if !ev.At.Equal(now) {
				t.Fatalf("event.At = %v, want now %v", ev.At, now)
			}
			bo := blockedOnByTask(t, h, planID)[b]
			if bo.ProbeCount != 1 || !bo.LastProbeAt.Equal(now) {
				t.Fatalf("B probe not recorded: count=%d last=%v now=%v", bo.ProbeCount, bo.LastProbeAt, now)
			}
		})
	}
}

// TestDeadline_GatedNode_NotReleased_OnTimeout is the CRITICAL regression (I103 §5 P2):
// a node held by the T1041 acceptance HARD gate that times out is re-probed / escalated
// but is NEVER released — EnsureTaskRunnable still rejects it after routing. Runs BOTH a
// reprobe policy (the resume-PROPOSING action) and an escalate policy to prove even the
// resume path cannot bypass the gate.
func TestDeadline_GatedNode_NotReleased_OnTimeout(t *testing.T) {
	for _, action := range []pm.TimeoutAction{pm.TimeoutReprobe, pm.TimeoutEscalate} {
		action := action
		t.Run(string(action), func(t *testing.T) {
			h, _ := planGraphSetup(t)
			ctx := h.ctx
			sink := &captureSink{}
			h.svc.timeoutSink = sink
			h.svc.deadlinePolicy = pm.DeadlinePolicy{
				ByWaitType: map[pm.WaitType]pm.WaitDeadline{
					pm.WaitAcceptanceVerdict: {Timeout: time.Hour, OnTimeout: action},
				},
			}
			pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
			planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "gated", CreatedBy: "user:a"})
			h.drain(t)
			dev := h.seedAssignedTask(t, pid, planID, "Dev", "user:dev")
			dec := h.seedAssignedTask(t, pid, planID, "Decision", "user:pd")
			merge := h.seedAssignedTask(t, pid, planID, "merge to main", "user:int")
			mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: dec, ToTaskID: dev, Kind: pm.EdgeSeq})
			mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: merge, ToTaskID: dec, Kind: pm.EdgeConditional, When: "pass"})
			mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: merge, ToTaskID: dev, Kind: pm.EdgeSeq})
			if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
				t.Fatal(err)
			}
			h.drain(t)

			// Assign the deadline, confirm the gate holds the merge node.
			if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
				t.Fatal(err)
			}
			if err := h.svc.EnsureTaskRunnable(ctx, merge); !errors.Is(err, pm.ErrTaskNotRunnable) {
				t.Fatalf("merge runnable before timeout, want gated: %v", err)
			}
			if bo := blockedOnByTask(t, h, planID)[merge]; bo.WaitType != pm.WaitAcceptanceVerdict || bo.Deadline.IsZero() {
				t.Fatalf("merge not acceptance_verdict with a deadline: %+v", bo)
			}

			// Past the deadline → the router fires the action…
			h.clk.Advance(2 * time.Hour)
			if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
				t.Fatal(err)
			}
			ev, ok := sink.byTask(merge)
			if !ok || ev.Action != action {
				t.Fatalf("merge timeout not routed with %q: ok=%v ev=%+v", action, ok, ev)
			}
			if bo := blockedOnByTask(t, h, planID)[merge]; bo.ProbeCount != 1 {
				t.Fatalf("merge probe not recorded: %+v", bo)
			}
			// …but the acceptance gate is UNTOUCHED — the node is still not runnable.
			if err := h.svc.EnsureTaskRunnable(ctx, merge); !errors.Is(err, pm.ErrTaskNotRunnable) {
				t.Fatalf("merge RELEASED by %q timeout — gate regressed: %v", action, err)
			}
		})
	}
}

// TestDeadline_Sweep_BestEffort_SinkError: a sink that ERRORS must not abort the sweep,
// must not roll back the dispatch, and must not lose the recorded probe (record-timeout
// commits independent of the sink).
func TestDeadline_Sweep_BestEffort_SinkError(t *testing.T) {
	h, _ := planGraphSetup(t)
	sink := &captureSink{err: errors.New("sink down")}
	h.svc.timeoutSink = sink
	h.svc.deadlinePolicy = pm.DeadlinePolicy{
		Default: pm.WaitDeadline{Timeout: time.Hour, OnTimeout: pm.TimeoutReprobe},
	}
	_, planID, a, b := seedBlockedPlanAB(t, h, "besteffort")

	h.clk.Advance(2 * time.Hour)
	// The sweep returns nil despite the sink error (best-effort router never aborts it).
	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatalf("sweep aborted on sink error: %v", err)
	}
	if len(sink.events) == 0 {
		t.Fatal("sink was not called")
	}
	// The probe was recorded even though the sink failed (record-timeout is the primary,
	// committed write).
	if bo := blockedOnByTask(t, h, planID)[b]; bo.ProbeCount != 1 {
		t.Fatalf("probe not recorded despite sink error: %+v", bo)
	}
	// Dispatch is intact — A is still dispatched/runnable (router never rolled it back).
	if err := h.svc.EnsureTaskRunnable(h.ctx, a); err != nil {
		t.Fatalf("A not runnable after sink error — dispatch regressed: %v", err)
	}
}

// TestDeadline_ProbeBackoff: a node that stays overdue is re-probed only once per
// ProbeBackoff window, not every sweep.
func TestDeadline_ProbeBackoff(t *testing.T) {
	h, _ := planGraphSetup(t)
	sink := &captureSink{}
	h.svc.timeoutSink = sink
	h.svc.deadlinePolicy = pm.DeadlinePolicy{
		Default:      pm.WaitDeadline{Timeout: time.Hour, OnTimeout: pm.TimeoutReprobe},
		ProbeBackoff: 10 * time.Minute,
	}
	_, planID, _, b := seedBlockedPlanAB(t, h, "backoff")

	// First overdue sweep → probe #1.
	h.clk.Advance(2 * time.Hour)
	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatal(err)
	}
	if bo := blockedOnByTask(t, h, planID)[b]; bo.ProbeCount != 1 {
		t.Fatalf("after first overdue sweep, probe_count = %d, want 1", bo.ProbeCount)
	}
	// Advance < backoff and sweep again → still probe #1 (throttled).
	h.clk.Advance(5 * time.Minute)
	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatal(err)
	}
	if bo := blockedOnByTask(t, h, planID)[b]; bo.ProbeCount != 1 {
		t.Fatalf("within backoff, probe_count = %d, want still 1", bo.ProbeCount)
	}
	// Advance past the backoff window → probe #2.
	h.clk.Advance(6 * time.Minute)
	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatal(err)
	}
	if bo := blockedOnByTask(t, h, planID)[b]; bo.ProbeCount != 2 {
		t.Fatalf("after backoff, probe_count = %d, want 2", bo.ProbeCount)
	}
}

// TestDeadline_InertPolicy_NoDeadlines: with NO policy wired the engine is a no-op — no
// deadline is assigned and no timeout is ever routed, however far the clock advances.
func TestDeadline_InertPolicy_NoDeadlines(t *testing.T) {
	h, _ := planGraphSetup(t)
	sink := &captureSink{}
	h.svc.timeoutSink = sink // wired, but the inert policy assigns no deadlines to route.
	_, planID, _, b := seedBlockedPlanAB(t, h, "inert")

	if bo := blockedOnByTask(t, h, planID)[b]; !bo.Deadline.IsZero() || bo.OnTimeout != "" {
		t.Fatalf("inert policy assigned a deadline: %+v", bo)
	}
	h.clk.Advance(1000 * time.Hour)
	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 0 {
		t.Fatalf("inert policy routed timeouts: %+v", sink.events)
	}
	if bo := blockedOnByTask(t, h, planID)[b]; bo.ProbeCount != 0 {
		t.Fatalf("inert policy probed: %+v", bo)
	}
}

// TestDeadline_NoSink_RecordsTimeout: with NO sink wired the router still RECORDS the
// timeout on the BlockedOn row (probe_count / last_probe_at) — the always-on
// record-timeout, independent of any external action.
func TestDeadline_NoSink_RecordsTimeout(t *testing.T) {
	h, _ := planGraphSetup(t)
	h.svc.deadlinePolicy = pm.DeadlinePolicy{
		Default: pm.WaitDeadline{Timeout: time.Hour, OnTimeout: pm.TimeoutEscalate},
	}
	_, planID, _, b := seedBlockedPlanAB(t, h, "nosink")
	h.clk.Advance(2 * time.Hour)
	now := h.clk.Now()
	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatal(err)
	}
	if bo := blockedOnByTask(t, h, planID)[b]; bo.ProbeCount != 1 || !bo.LastProbeAt.Equal(now) {
		t.Fatalf("no-sink timeout not recorded: count=%d last=%v now=%v", bo.ProbeCount, bo.LastProbeAt, now)
	}
}

// TestDeadline_RouteTimeouts_PropagatesRepoErrors asserts routeTimeouts returns (never
// swallows) the ListBlockedOn and UpsertBlockedOn errors — so the best-effort wrapper in
// ReconcileRunningPlans can log them. Uses a node already past its deadline.
func TestDeadline_RouteTimeouts_PropagatesRepoErrors(t *testing.T) {
	h, _ := planGraphSetup(t)
	h.svc.deadlinePolicy = pm.DeadlinePolicy{
		Default: pm.WaitDeadline{Timeout: time.Hour, OnTimeout: pm.TimeoutReprobe},
	}
	_, planID, _, _ := seedBlockedPlanAB(t, h, "roerr")
	h.clk.Advance(2 * time.Hour)
	p, _ := h.plans.FindByID(h.ctx, planID)

	real := h.svc.plans
	defer func() { h.svc.plans = real }()
	for _, failOn := range []string{"list", "upsert"} {
		h.svc.plans = &failingPlanRepo{PlanRepo: h.plans, failOn: failOn}
		if err := h.svc.routeTimeouts(h.ctx, p); !errors.Is(err, errBoom) {
			t.Errorf("routeTimeouts failOn=%q err = %v, want errBoom", failOn, err)
		}
	}
}
