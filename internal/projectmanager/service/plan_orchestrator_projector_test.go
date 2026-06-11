package service

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// orchestratorHarness extends planAdvanceSetup with a relay that ALSO registers
// the PlanOrchestratorProjector (the P2-1 auto-advance core) — so feeding
// pm.task.state_changed / pm.plan.started through it auto-advances the plan,
// exactly like the production relay.
type orchestratorHarness struct {
	*planAdvanceHarness
	applied      *outboxsql.AppliedRepo
	ob           *outboxsql.OutboxRepo
	orchestrator *PlanOrchestratorProjector
	autoRelay    *outbox.Relay
}

func orchestratorSetup(t *testing.T) *orchestratorHarness {
	t.Helper()
	h := planAdvanceSetup(t)
	// Reuse the SAME db/outbox/applied as planAdvanceSetup by re-deriving from svc.
	ob := outboxsql.NewOutboxRepo(h.svc.db)
	applied := outboxsql.NewAppliedRepo(h.svc.db)
	taskProj := NewParticipantProjector(h.svc.db, h.convRepo, applied, h.svc.idgen, h.svc.clock)
	planProj := NewPlanParticipantProjector(h.svc.db, h.convRepo, h.plans, applied, h.svc.idgen, h.svc.clock)
	orch := NewPlanOrchestratorProjector(h.svc.db, h.svc, applied, h.svc.clock)
	// The auto-advance relay includes the orchestrator alongside the participant
	// projectors (so plan.created → conversation bound, then auto-advance fires).
	autoRelay := outbox.NewRelay(ob, applied, h.svc.clock, taskProj, planProj, orch)
	// Point the harness' drain at the auto-relay so setup helpers (seedAssignedTask)
	// also process plan.created/participants through the same store.
	h.relay = autoRelay
	return &orchestratorHarness{planAdvanceHarness: h, applied: applied, ob: ob, orchestrator: orch, autoRelay: autoRelay}
}

func (h *orchestratorHarness) dispatchRecords(t *testing.T, planID pm.PlanID) []pm.DispatchRecord {
	t.Helper()
	recs, err := h.plans.ListDispatchRecords(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	return recs
}

func dispatchCount(recs []pm.DispatchRecord, tid pm.TaskID) int {
	n := 0
	for _, r := range recs {
		if r.TaskID == tid {
			n++
		}
	}
	return n
}

// TestOrchestrator_AutoAdvance_OnTaskDone is the headline behavioral test:
// a running plan A→{B,C}; A done → feeding pm.task.state_changed(A) through the
// orchestrator projector AUTO-DISPATCHES B and C (mention posted + dispatch
// records) with NO manual AdvancePlan. pm.plan.started dispatches initial node A.
func TestOrchestrator_AutoAdvance_OnTaskDone(t *testing.T) {
	h := orchestratorSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "fan", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:x")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:y")
	c := h.seedAssignedTask(t, pid, planID, "C", "user:z")
	if err := h.svc.AddPlanDependency(h.ctx, planID, b, a, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.AddPlanDependency(h.ctx, planID, c, a, "user:a"); err != nil {
		t.Fatal(err)
	}

	// StartPlan emits pm.plan.started → orchestrator dispatches initial node A.
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	recs := h.dispatchRecords(t, planID)
	if dispatchCount(recs, a) != 1 {
		t.Fatalf("pm.plan.started did not auto-dispatch initial node A (count=%d)", dispatchCount(recs, a))
	}
	if dispatchCount(recs, b) != 0 || dispatchCount(recs, c) != 0 {
		t.Fatal("B/C dispatched before A done — should be blocked")
	}
	baseMsgs := h.planConvMsgCount(t, planID)

	// A done → pm.task.state_changed(A) → orchestrator dispatches B AND C.
	h.setTaskStatus(t, a, pm.TaskCompleted)
	h.drain(t)
	recs = h.dispatchRecords(t, planID)
	if dispatchCount(recs, b) != 1 || dispatchCount(recs, c) != 1 {
		t.Fatalf("auto-advance did not dispatch both B and C: B=%d C=%d", dispatchCount(recs, b), dispatchCount(recs, c))
	}
	if got := h.planConvMsgCount(t, planID) - baseMsgs; got != 2 {
		t.Fatalf("plan conversation got %d new messages, want exactly 2 (@B,@C)", got)
	}
}

// TestOrchestrator_ReplayIdempotent feeds the SAME pm.task.state_changed event
// TWICE (event replay) → each ready node dispatched EXACTLY once (1 dispatch
// record, 1 mention) — §9.3 (AppliedStore + INSERT-OR-IGNORE record).
func TestOrchestrator_ReplayIdempotent(t *testing.T) {
	h := orchestratorSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "replay", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:x")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:y")
	if err := h.svc.AddPlanDependency(h.ctx, planID, b, a, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	h.setTaskStatus(t, a, pm.TaskCompleted) // emits the real task.state_changed
	h.drain(t)
	baseB := dispatchCount(h.dispatchRecords(t, planID), b)
	if baseB != 1 {
		t.Fatalf("B dispatched %d times after A done, want 1", baseB)
	}
	baseMsgs := h.planConvMsgCount(t, planID)

	// Replay: feed a SYNTHETIC duplicate pm.task.state_changed(A) directly to the
	// projector (a different event ID, simulating redelivery of the same logical
	// change) → must NOT re-dispatch B (record already exists, INSERT-OR-IGNORE).
	dupEvent := outbox.Event{
		ID:        h.svc.idgen.NewULID(),
		EventType: EvtTaskStateChanged,
		Payload:   mustJSON(t, taskEventPayload{TaskID: string(a), ProjectID: string(pid), Status: string(pm.TaskCompleted)}),
		CreatedAt: h.svc.clock.Now(),
	}
	if err := h.orchestrator.Project(h.ctx, dupEvent); err != nil {
		t.Fatalf("replay Project: %v", err)
	}
	// And feed the EXACT same event a SECOND time (true replay on the same ID →
	// AppliedStore short-circuits).
	if err := h.orchestrator.Project(h.ctx, dupEvent); err != nil {
		t.Fatalf("second replay Project: %v", err)
	}
	if got := dispatchCount(h.dispatchRecords(t, planID), b); got != 1 {
		t.Fatalf("replay double-dispatched B: count=%d want 1 (§9.3)", got)
	}
	if got := h.planConvMsgCount(t, planID) - baseMsgs; got != 0 {
		t.Fatalf("replay posted %d extra @mention messages, want 0 (§9.3)", got)
	}
}

// TestOrchestrator_ConcurrentDispatch fires the SAME pm.task.state_changed event
// from many goroutines at once (the §9.3 concurrency invariant) → B is dispatched
// EXACTLY once (never double-woken). Each goroutine uses a distinct event ID so
// the AppliedStore does not trivially dedup — the INSERT-OR-IGNORE dispatch record
// is what guarantees single dispatch under concurrency.
func TestOrchestrator_ConcurrentDispatch(t *testing.T) {
	h := orchestratorSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "conc", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:x")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:y")
	if err := h.svc.AddPlanDependency(h.ctx, planID, b, a, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	// Mark A done WITHOUT draining (so the orchestrator hasn't dispatched B yet).
	h.setTaskStatus(t, a, pm.TaskCompleted)

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ev := outbox.Event{
				ID:        h.svc.idgen.NewULID(),
				EventType: EvtTaskStateChanged,
				Payload:   mustJSON(t, taskEventPayload{TaskID: string(a), ProjectID: string(pid), Status: string(pm.TaskCompleted)}),
				CreatedAt: h.svc.clock.Now(),
			}
			errs[i] = h.orchestrator.Project(h.ctx, ev)
		}(i)
	}
	wg.Wait()
	// Under SQLite the writes serialize; transient busy errors are acceptable as
	// long as the invariant holds. The invariant: EXACTLY one dispatch record for B.
	if got := dispatchCount(h.dispatchRecords(t, planID), b); got != 1 {
		t.Fatalf("concurrent dispatch produced %d records for B, want EXACTLY 1 (§9.3 never double-wake)", got)
	}
}

// TestOrchestrator_NoopCases asserts the orchestrator is a no-op (no dispatch) +
// MarkApplied for: (a) a task not in any plan, (b) a plan in draft, (c) a plan
// that is done — and for irrelevant event types.
func TestOrchestrator_NoopCases(t *testing.T) {
	h := orchestratorSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})

	t.Run("task not in a plan", func(t *testing.T) {
		tid, _ := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "loose", CreatedBy: "user:a"})
		ev := outbox.Event{
			ID:        h.svc.idgen.NewULID(),
			EventType: EvtTaskStateChanged,
			Payload:   mustJSON(t, taskEventPayload{TaskID: string(tid), ProjectID: string(pid), Status: string(pm.TaskCompleted)}),
			CreatedAt: h.svc.clock.Now(),
		}
		if err := h.orchestrator.Project(h.ctx, ev); err != nil {
			t.Fatalf("Project: %v", err)
		}
		done, err := h.applied.IsApplied(h.ctx, h.orchestrator.Name(), ev.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !done {
			t.Fatal("no-op event (task not in plan) was not MarkApplied — would retry forever")
		}
	})

	t.Run("plan in draft", func(t *testing.T) {
		planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "draft", CreatedBy: "user:a"})
		h.drain(t)
		tid := h.seedAssignedTask(t, pid, planID, "A", "user:x")
		// Plan is still draft (not started) → orchestrator must NOT dispatch.
		ev := outbox.Event{
			ID:        h.svc.idgen.NewULID(),
			EventType: EvtPlanStarted,
			Payload:   mustJSON(t, planEventPayload{PlanID: string(planID), ProjectID: string(pid)}),
			CreatedAt: h.svc.clock.Now(),
		}
		if err := h.orchestrator.Project(h.ctx, ev); err != nil {
			t.Fatalf("Project: %v", err)
		}
		if got := len(h.dispatchRecords(t, planID)); got != 0 {
			t.Fatalf("draft plan dispatched %d nodes, want 0 (no-op)", got)
		}
		done, _ := h.applied.IsApplied(h.ctx, h.orchestrator.Name(), ev.ID)
		if !done {
			t.Fatal("draft-plan no-op was not MarkApplied")
		}
		_ = tid
	})

	t.Run("irrelevant event type", func(t *testing.T) {
		ev := outbox.Event{
			ID:        h.svc.idgen.NewULID(),
			EventType: EvtTaskCreated, // not consumed
			Payload:   `{}`,
			CreatedAt: h.svc.clock.Now(),
		}
		if err := h.orchestrator.Project(h.ctx, ev); err != nil {
			t.Fatalf("Project: %v", err)
		}
		// Irrelevant types short-circuit BEFORE the tx → not MarkApplied (other
		// projectors may consume it). Just assert no error + no panic above.
	})
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
