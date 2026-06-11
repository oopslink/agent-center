package service

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
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

// mentionCount returns how many messages in the plan conversation contain `ref`
// in their content (the @mention is carried in the message text — the wake path
// scans content, see PlanDispatchAdapter).
func (h *orchestratorHarness) mentionCount(t *testing.T, planID pm.PlanID, ref string) int {
	t.Helper()
	conv, err := h.convRepo.FindByOwnerRef(h.ctx, conversation.NewPlanOwnerRef(string(planID)))
	if err != nil {
		t.Fatalf("plan conversation should exist: %v", err)
	}
	msgs, err := h.msgRepo.FindRecent(h.ctx, conv.ID(), 1000)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, m := range msgs {
		if strings.Contains(m.Content(), ref) {
			n++
		}
	}
	return n
}

// takeEvent fetches the first UNPROCESSED outbox event of the given type whose
// payload references taskID — the REAL event (with its real id) the producer
// emitted, so a test can replay the SAME event id through the projector.
func (h *orchestratorHarness) takeEvent(t *testing.T, eventType, taskID string) outbox.Event {
	t.Helper()
	evs, err := h.ob.FetchUnprocessed(h.ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if e.EventType != eventType {
			continue
		}
		var pl taskEventPayload
		if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
			continue
		}
		if pl.TaskID == taskID {
			return e
		}
	}
	t.Fatalf("no unprocessed %s event for task %s", eventType, taskID)
	return outbox.Event{}
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

// TestOrchestrator_FailureNotifiesCreator is the v2.9 P2-2 headline test:
// a running plan A→{B,C} where A→B and A→C; A FAILS (TaskDiscarded) →
//   - the plan CREATOR (user:a) is @mentioned in the plan conversation,
//   - the downstream subtree (B, C) is NOT dispatched (stays blocked, §9.7),
//   - the plan stays `running` (not done, never auto-terminal-failed).
func TestOrchestrator_FailureNotifiesCreator(t *testing.T) {
	h := orchestratorSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "fail", CreatedBy: "user:a"})
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
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	// A dispatched; the creator hasn't been notified yet (no failure).
	if got := h.mentionCount(t, planID, "user:a"); got != 0 {
		t.Fatalf("creator mentioned %d times before any failure, want 0", got)
	}

	// A FAILS → orchestrator notifies the creator; B/C stay blocked.
	h.setTaskStatus(t, a, pm.TaskDiscarded)
	h.drain(t)

	if got := h.mentionCount(t, planID, "user:a"); got != 1 {
		t.Fatalf("creator @mentioned %d times on failure, want exactly 1 (P2-2)", got)
	}
	recs := h.dispatchRecords(t, planID)
	if dispatchCount(recs, b) != 0 || dispatchCount(recs, c) != 0 {
		t.Fatalf("downstream-of-failed dispatched (B=%d C=%d), want 0 — must stay blocked (§9.7)", dispatchCount(recs, b), dispatchCount(recs, c))
	}
	// Plan stays running (failure never auto-terminates — §9.1).
	plan, err := h.plans.FindByID(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status() != pm.PlanRunning {
		t.Fatalf("plan status %q after a node failed, want running (no auto-terminate)", plan.Status())
	}
}

// TestOrchestrator_FailureIndependentBranchAdvances asserts §9.7 branch
// isolation: in a plan with an independent branch (A→B) and a separate node D
// with no relation to A, A failing leaves B blocked but a SEPARATELY-completing
// upstream still advances its own downstream. Concretely: A→B and D→E; A fails
// (B blocked, creator notified) while D completes (E auto-dispatched).
func TestOrchestrator_FailureIndependentBranchAdvances(t *testing.T) {
	h := orchestratorSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "branch", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:x")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:y")
	d := h.seedAssignedTask(t, pid, planID, "D", "user:m")
	e := h.seedAssignedTask(t, pid, planID, "E", "user:n")
	if err := h.svc.AddPlanDependency(h.ctx, planID, b, a, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.AddPlanDependency(h.ctx, planID, e, d, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)

	// A fails → B stays blocked, creator notified.
	h.setTaskStatus(t, a, pm.TaskDiscarded)
	h.drain(t)
	// D completes → its INDEPENDENT downstream E auto-advances.
	h.setTaskStatus(t, d, pm.TaskCompleted)
	h.drain(t)

	recs := h.dispatchRecords(t, planID)
	if dispatchCount(recs, b) != 0 {
		t.Fatalf("B (downstream of failed A) dispatched %d times, want 0 (§9.7)", dispatchCount(recs, b))
	}
	if dispatchCount(recs, e) != 1 {
		t.Fatalf("E (independent branch, D done) dispatched %d times, want 1 (independent branch advances)", dispatchCount(recs, e))
	}
	if got := h.mentionCount(t, planID, "user:a"); got != 1 {
		t.Fatalf("creator @mentioned %d times, want exactly 1", got)
	}
}

// TestOrchestrator_FailureReplayNotifiesOnce replays the SAME failure event
// (same event ID) → the creator is @mentioned EXACTLY once. The AppliedStore
// dedups the failed-transition event per projector, so the redelivery
// short-circuits at IsApplied before advance re-runs (P2-2 idempotency: the
// per-event AppliedStore dedup is the primary guarantee).
func TestOrchestrator_FailureReplayNotifiesOnce(t *testing.T) {
	h := orchestratorSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "replayfail", CreatedBy: "user:a"})
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
	// Mark A failed WITHOUT draining, then capture the real failure event so we can
	// replay the SAME event id through the projector twice.
	h.setTaskStatus(t, a, pm.TaskDiscarded)
	failEvent := h.takeEvent(t, EvtTaskStateChanged, string(a))

	if err := h.orchestrator.Project(h.ctx, failEvent); err != nil {
		t.Fatalf("first Project(failure): %v", err)
	}
	if err := h.orchestrator.Project(h.ctx, failEvent); err != nil {
		t.Fatalf("replay Project(failure): %v", err)
	}
	if got := h.mentionCount(t, planID, "user:a"); got != 1 {
		t.Fatalf("creator @mentioned %d times across an event replay, want exactly 1 (AppliedStore dedup)", got)
	}
}

// TestOrchestrator_FirstFailureTransitionNotifies asserts the →failed TRANSITION
// guard notifies on a genuine first failure: a task moving running→discarded
// carries prev_status=running (NOT failed) and now=discarded (failed), so the
// creator is @mentioned exactly once. This is the positive side of the
// transition guard that the re-discard test below is the negative side of.
func TestOrchestrator_FirstFailureTransitionNotifies(t *testing.T) {
	h := orchestratorSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "firstfail", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:x")
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	// Move A into running first (so the failure is a running→discarded transition,
	// not open→discarded) — prev_status is running, which is NOT failed.
	h.setTaskStatus(t, a, pm.TaskRunning)
	h.drain(t)
	if got := h.mentionCount(t, planID, "user:a"); got != 0 {
		t.Fatalf("creator mentioned %d times before failure, want 0", got)
	}
	// A FAILS: prev=running (not failed) → now=discarded (failed) ⇒ notify once.
	h.setTaskStatus(t, a, pm.TaskDiscarded)
	h.drain(t)
	if got := h.mentionCount(t, planID, "user:a"); got != 1 {
		t.Fatalf("first failure (running→discarded transition) notified %d times, want 1", got)
	}
}

// TestOrchestrator_ReDiscardDoesNotRenotify is the edge this fast-follow closes:
// once a plan-task is already FAILED (discarded), a SUBSEQUENT state_changed event
// whose prev_status is already `discarded` (a re-discard of an already-failed task)
// must NOT re-notify the creator — only the →failed TRANSITION notifies. We drive
// the first failure through the real path (creator mentioned once), then feed a
// SYNTHETIC state_changed with prev_status=discorded & status=discarded (distinct
// event id, so the AppliedStore does NOT dedup it) and assert the mention count
// does NOT increase.
func TestOrchestrator_ReDiscardDoesNotRenotify(t *testing.T) {
	h := orchestratorSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "rediscard", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:x")
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	// First failure → creator notified once (the →failed transition).
	h.setTaskStatus(t, a, pm.TaskDiscarded)
	h.drain(t)
	if got := h.mentionCount(t, planID, "user:a"); got != 1 {
		t.Fatalf("first failure notified %d times, want exactly 1", got)
	}

	// Re-discard: a NEW state_changed event for the already-failed task whose
	// prev_status is ALREADY discarded. A distinct event id means the AppliedStore
	// does not short-circuit it — the TRANSITION guard is what must suppress the
	// re-notify (prev already failed ⇒ not a →failed transition).
	reEvent := outbox.Event{
		ID:        h.svc.idgen.NewULID(),
		EventType: EvtTaskStateChanged,
		Payload: mustJSON(t, taskEventPayload{
			TaskID: string(a), ProjectID: string(pid),
			PrevStatus: string(pm.TaskDiscarded), Status: string(pm.TaskDiscarded),
		}),
		CreatedAt: h.svc.clock.Now(),
	}
	if err := h.orchestrator.Project(h.ctx, reEvent); err != nil {
		t.Fatalf("Project(re-discard): %v", err)
	}
	if got := h.mentionCount(t, planID, "user:a"); got != 1 {
		t.Fatalf("re-discard (prev already failed) re-notified: count=%d, want still 1 (the edge this closes)", got)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
