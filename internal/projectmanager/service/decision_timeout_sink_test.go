package service

import (
	"context"
	"errors"
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// captureDecisionPort records every ArmDecisionReminder the sink makes (and can inject an
// error to exercise the best-effort swallow path).
type captureDecisionPort struct {
	reqs []DecisionReminderRequest
	err  error
}

func (c *captureDecisionPort) ArmDecisionReminder(_ context.Context, req DecisionReminderRequest) error {
	c.reqs = append(c.reqs, req)
	return c.err
}

// TestHumanDecisionSink_OnlyHumanDecision: every non-human_decision wait_type is a no-op
// (the engine already recorded the probe; the sink must NOT re-dispatch/take over).
func TestHumanDecisionSink_OnlyHumanDecision(t *testing.T) {
	port := &captureDecisionPort{}
	sink := NewHumanDecisionTimeoutSink(port, 3)
	for _, wt := range []pm.WaitType{
		pm.WaitUpstreamCompletion, pm.WaitAcceptanceVerdict, pm.WaitStageBarrier,
		pm.WaitExternalEvent, pm.WaitExecutorLiveness, pm.WaitTimeoutOnly,
	} {
		if err := sink.OnTimeout(context.Background(), pm.TimeoutEvent{WaitType: wt, ProbeCount: 1}); err != nil {
			t.Fatalf("OnTimeout(%s) err = %v", wt, err)
		}
	}
	if len(port.reqs) != 0 {
		t.Fatalf("non-human_decision wait types armed reminders: %+v", port.reqs)
	}
}

// TestHumanDecisionSink_ArmAndEscalateIdempotent: a human_decision timeout arms EXACTLY
// once at the first probe and escalates EXACTLY once at escalateAfter; every other
// probe_count is a no-op (the durable reminder already stands — never re-armed per sweep).
func TestHumanDecisionSink_ArmAndEscalateIdempotent(t *testing.T) {
	port := &captureDecisionPort{}
	const n = 3
	sink := NewHumanDecisionTimeoutSink(port, n)
	// Simulate the engine calling the sink once per routed probe: 1,2,3,4,5.
	for pc := 1; pc <= 5; pc++ {
		ev := pm.TimeoutEvent{
			WaitType:   pm.WaitHumanDecision,
			TaskID:     "dec-1",
			WaitKeys:   []string{"dec-1"},
			ProbeCount: pc,
		}
		if err := sink.OnTimeout(context.Background(), ev); err != nil {
			t.Fatalf("OnTimeout probe#%d err = %v", pc, err)
		}
	}
	if len(port.reqs) != 2 {
		t.Fatalf("armed %d times, want 2 (initial + escalate); reqs=%+v", len(port.reqs), port.reqs)
	}
	if port.reqs[0].ProbeCount != 1 || port.reqs[0].Escalate {
		t.Fatalf("first arm = %+v, want probe#1 non-escalate", port.reqs[0])
	}
	if port.reqs[1].ProbeCount != n || !port.reqs[1].Escalate {
		t.Fatalf("second arm = %+v, want probe#%d escalate", port.reqs[1], n)
	}
	if port.reqs[0].TaskID != "dec-1" || len(port.reqs[0].DecisionKeys) != 1 {
		t.Fatalf("arm carried wrong routing keys: %+v", port.reqs[0])
	}
}

// TestHumanDecisionSink_NilPortAndErrors: a nil port is a no-op (nil-safe), and a port
// error is propagated (the engine swallows it best-effort — see routeTimeouts).
func TestHumanDecisionSink_NilPortAndErrors(t *testing.T) {
	nilSink := NewHumanDecisionTimeoutSink(nil, 3)
	if err := nilSink.OnTimeout(context.Background(), pm.TimeoutEvent{WaitType: pm.WaitHumanDecision, ProbeCount: 1}); err != nil {
		t.Fatalf("nil-port sink err = %v, want nil", err)
	}
	boom := errors.New("reminder down")
	sink := NewHumanDecisionTimeoutSink(&captureDecisionPort{err: boom}, 3)
	if err := sink.OnTimeout(context.Background(), pm.TimeoutEvent{WaitType: pm.WaitHumanDecision, ProbeCount: 1}); !errors.Is(err, boom) {
		t.Fatalf("sink err = %v, want boom", err)
	}
}

// TestHumanDecisionSink_EscalateAfterFloor: escalateAfter <= 1 falls back to the default
// so an "initial arm" and "escalate" never collapse onto the same first probe.
func TestHumanDecisionSink_EscalateAfterFloor(t *testing.T) {
	for _, in := range []int{0, 1} {
		sink := NewHumanDecisionTimeoutSink(&captureDecisionPort{}, in)
		if sink.escalateAfter != DefaultDecisionEscalateAfter {
			t.Fatalf("escalateAfter(%d) = %d, want default %d", in, sink.escalateAfter, DefaultDecisionEscalateAfter)
		}
	}
}

// seedRootDecisionPlan builds a running plan whose ROOT node is a pending human_decision
// (Decision) gating a downstream merge via a conditional "pass" edge, assigns the
// decision to `decisionOwner`, runs one sweep so the decision is classified
// human_decision, and returns the decision + merge task ids.
func seedRootDecisionPlan(t *testing.T, h *planAdvanceHarness, name, decisionOwner string) (pm.ProjectID, pm.PlanID, pm.TaskID, pm.TaskID) {
	t.Helper()
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: name, CreatedBy: "user:a"})
	h.drain(t)
	dec := h.seedAssignedTask(t, pid, planID, "Decision", decisionOwner)
	merge := h.seedAssignedTask(t, pid, planID, "merge to main", "user:int")
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: merge, ToTaskID: dec, Kind: pm.EdgeConditional, When: "pass"})
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatalf("seed sweep: %v", err)
	}
	return pid, planID, dec, merge
}

// TestHumanDecisionSink_Integration_LiveEngine drives the whole engine with the REAL
// production sink over a fake port: the DefaultDeadlinePolicy classifies the root
// Decision as human_decision + assigns a deadline; past the deadline the router arms a
// reminder (probe#1), stays idempotent across sweeps until escalateAfter, then escalates
// — and the gated merge node is NEVER released (I103 §5 P2 red line).
func TestHumanDecisionSink_Integration_LiveEngine(t *testing.T) {
	h, _ := planGraphSetup(t)
	port := &captureDecisionPort{}
	const escalateAfter = 3
	h.svc.timeoutSink = NewHumanDecisionTimeoutSink(port, escalateAfter)
	h.svc.deadlinePolicy = pm.DeadlinePolicy{
		ByWaitType: map[pm.WaitType]pm.WaitDeadline{
			pm.WaitHumanDecision: {Timeout: time.Hour, OnTimeout: pm.TimeoutEscalate},
		},
		ProbeBackoff: 10 * time.Minute,
	}
	_, planID, dec, merge := seedRootDecisionPlan(t, h, "live-decision", "agent:reviewer")

	// The Decision is a pending human_decision with an assigned deadline.
	bo := blockedOnByTask(t, h, planID)[dec]
	if bo.WaitType != pm.WaitHumanDecision || bo.Deadline.IsZero() {
		t.Fatalf("decision not human_decision with a deadline: %+v", bo)
	}
	// merge is gated behind the unresolved decision.
	mustGated := func(when string) {
		if err := h.svc.EnsureTaskRunnable(h.ctx, merge); !errors.Is(err, pm.ErrTaskNotRunnable) {
			t.Fatalf("merge runnable %s — decision auto-ruled, gate regressed: %v", when, err)
		}
	}
	mustGated("before timeout")

	// First overdue sweep → probe#1 → initial arm.
	h.clk.Advance(2 * time.Hour)
	sweep := func() {
		if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
			t.Fatalf("sweep: %v", err)
		}
	}
	sweep()
	if len(port.reqs) != 1 || port.reqs[0].Escalate {
		t.Fatalf("after first overdue sweep, arms=%+v, want 1 non-escalate", port.reqs)
	}
	mustGated("after probe#1")

	// A sweep within ProbeBackoff → no new probe, no new arm (idempotent).
	h.clk.Advance(5 * time.Minute)
	sweep()
	if len(port.reqs) != 1 {
		t.Fatalf("re-armed within backoff: %+v", port.reqs)
	}

	// Advance through probes #2 and #3. Probe #2 is a no-op (between initial & escalate);
	// probe #3 == escalateAfter → escalate.
	for i := 0; i < 2; i++ {
		h.clk.Advance(11 * time.Minute)
		sweep()
	}
	if bo := blockedOnByTask(t, h, planID)[dec]; bo.ProbeCount != escalateAfter {
		t.Fatalf("probe_count = %d, want %d", bo.ProbeCount, escalateAfter)
	}
	if len(port.reqs) != 2 {
		t.Fatalf("arms=%d, want 2 (initial + escalate); reqs=%+v", len(port.reqs), port.reqs)
	}
	if !port.reqs[1].Escalate || port.reqs[1].ProbeCount != escalateAfter {
		t.Fatalf("escalation arm = %+v, want escalate at probe#%d", port.reqs[1], escalateAfter)
	}
	// A further overdue sweep past escalation → still no re-arm.
	h.clk.Advance(11 * time.Minute)
	sweep()
	if len(port.reqs) != 2 {
		t.Fatalf("re-armed after escalation: %+v", port.reqs)
	}
	// RED LINE: after every timeout + escalation, the decision is still unresolved and the
	// gated merge is still not runnable — the sink never released it.
	mustGated("after escalation")
}

// TestDefaultDeadlinePolicy_Wired confirms the SHIPPED policy (the one the composition
// root wires) is live-capable: it assigns the human_decision node its 4h escalate
// deadline through the normal materialize path.
func TestDefaultDeadlinePolicy_Wired(t *testing.T) {
	h, _ := planGraphSetup(t)
	h.svc.deadlinePolicy = pm.DefaultDeadlinePolicy()
	_, planID, dec, _ := seedRootDecisionPlan(t, h, "default-policy", "agent:reviewer")

	bo := blockedOnByTask(t, h, planID)[dec]
	if bo.WaitType != pm.WaitHumanDecision {
		t.Fatalf("decision wait_type = %q, want human_decision", bo.WaitType)
	}
	if bo.OnTimeout != string(pm.TimeoutEscalate) {
		t.Fatalf("human_decision on_timeout = %q, want escalate", bo.OnTimeout)
	}
	if want := bo.WaitedSince.Add(4 * time.Hour); !bo.Deadline.Equal(want) {
		t.Fatalf("human_decision deadline = %v, want waited_since+4h = %v", bo.Deadline, want)
	}
}
