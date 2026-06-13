package service

import (
	"context"
	"testing"
	"time"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// --- test doubles -----------------------------------------------------------

// stubGate is a deterministic RedispatchEligibilityPort: it returns a fixed
// verdict and counts calls (to prove the reason/assignee FILTER runs before the
// gate, i.e. ineligible-by-filter tasks never reach the gate).
type stubGate struct {
	eligible bool
	err      error
	calls    int
}

func (g *stubGate) Eligible(ctx context.Context, agentRef, taskRef string) (bool, error) {
	g.calls++
	return g.eligible, g.err
}

// eventRecorder is a projector that tallies every event type the relay delivers,
// so a test can assert which domain/audit events were emitted.
type eventRecorder struct{ seen map[string]int }

func newEventRecorder() *eventRecorder { return &eventRecorder{seen: map[string]int{}} }
func (r *eventRecorder) Name() string  { return "test-auto-redispatch-recorder" }
func (r *eventRecorder) Project(ctx context.Context, e outbox.Event) error {
	r.seen[e.EventType]++
	return nil
}

// --- fixture ----------------------------------------------------------------

type redispatchFixture struct {
	svc      *Service
	wiRepo   *agentsql.WorkItemRepo
	tasks    pm.TaskRepository
	relay    *outbox.Relay
	recorder *eventRecorder
	clk      clock.Clock
	ctx      context.Context
}

func newRedispatchFixture(t *testing.T) *redispatchFixture {
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
	ob := outboxsql.NewOutboxRepo(db)
	applied := outboxsql.NewAppliedRepo(db)
	wiRepo := agentsql.NewWorkItemRepo(db)
	tasks := pmsql.NewTaskRepo(db)
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: tasks,
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Outbox: ob, IDGen: gen, Clock: clk,
	})
	wiProj := NewWorkItemProjector(db, wiRepo, applied, gen, clk)
	recorder := newEventRecorder()
	relay := outbox.NewRelay(ob, applied, clk, wiProj, recorder)
	return &redispatchFixture{svc: svc, wiRepo: wiRepo, tasks: tasks, relay: relay, recorder: recorder, clk: clk, ctx: context.Background()}
}

// seedStuckTask inserts a running task carrying the given blocked_reason + assignee
// (the stuck-released shape). reason="" means a healthy running task.
func (f *redispatchFixture) seedStuckTask(t *testing.T, id, assignee, reason string) {
	t.Helper()
	now := f.clk.Now()
	task, err := pm.RehydrateTask(pm.RehydrateTaskInput{
		ID: pm.TaskID(id), ProjectID: "proj-1", Title: "stuck task",
		Status: pm.TaskRunning, Assignee: pm.IdentityRef(assignee), BlockedReason: reason,
		CreatedBy: "user:a", CreatedAt: now, UpdatedAt: now, Version: 1, StatusChangedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.tasks.Save(f.ctx, task); err != nil {
		t.Fatal(err)
	}
}

// clearReason simulates recovery: the task re-activated and the failure
// annotation was cleared (TaskStatusSyncProjector A①). Bumps version like a real
// update would.
func (f *redispatchFixture) setReason(t *testing.T, id, reason string, version int) {
	t.Helper()
	now := f.clk.Now()
	task, err := pm.RehydrateTask(pm.RehydrateTaskInput{
		ID: pm.TaskID(id), ProjectID: "proj-1", Title: "stuck task",
		Status: pm.TaskRunning, Assignee: "agent:ag-1", BlockedReason: reason,
		CreatedBy: "user:a", CreatedAt: now, UpdatedAt: now, Version: version, StatusChangedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.tasks.Update(f.ctx, task); err != nil {
		t.Fatal(err)
	}
}

func (f *redispatchFixture) drain(t *testing.T) {
	t.Helper()
	if _, err := f.relay.RunOnce(f.ctx, 100); err != nil {
		t.Fatal(err)
	}
}

// --- tests ------------------------------------------------------------------

// TestAutoRedispatch_RedispatchesEligibleStuckTask: the happy path — a stuck-
// released task whose assignee is eligible gets re-dispatched (fresh queued
// WorkItem minted by the WorkItemProjector) + an audit event emitted, WITHOUT
// clearing the blocked_reason (the stuck signal survives until real activation).
func TestAutoRedispatch_RedispatchesEligibleStuckTask(t *testing.T) {
	f := newRedispatchFixture(t)
	f.seedStuckTask(t, "T1", "agent:ag-1", taskBlockedOnFailureReason)
	gate := &stubGate{eligible: true}
	r := NewAutoRedispatchReconciler(f.svc, gate, f.clk, 0, 0, nil)

	n, err := r.Tick(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("redispatched = %d, want 1", n)
	}
	f.drain(t)

	items, err := f.wiRepo.ListByTask(f.ctx, "pm://tasks/T1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status() != agentpkg.WorkItemQueued {
		t.Fatalf("want one queued work item, got %+v", items)
	}
	if f.recorder.seen[EvtTaskAutoRedispatched] != 1 {
		t.Fatalf("want 1 %s event, got %d", EvtTaskAutoRedispatched, f.recorder.seen[EvtTaskAutoRedispatched])
	}
	if f.recorder.seen[EvtTaskAssigned] != 1 {
		t.Fatalf("want 1 %s event (functional re-dispatch), got %d", EvtTaskAssigned, f.recorder.seen[EvtTaskAssigned])
	}
	// blocked_reason MUST remain until a real activation clears it.
	got, _ := f.tasks.FindByID(f.ctx, "T1")
	if got.BlockedReason() != taskBlockedOnFailureReason {
		t.Fatalf("blocked_reason cleared prematurely: %q", got.BlockedReason())
	}
}

// TestAutoRedispatch_SkipsIneligible: an ineligible assignee (agent still down) is
// not re-dispatched — no work item, no audit event.
func TestAutoRedispatch_SkipsIneligible(t *testing.T) {
	f := newRedispatchFixture(t)
	f.seedStuckTask(t, "T1", "agent:ag-1", taskBlockedOnFailureReason)
	gate := &stubGate{eligible: false}
	r := NewAutoRedispatchReconciler(f.svc, gate, f.clk, 0, 0, nil)

	n, err := r.Tick(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("redispatched = %d, want 0", n)
	}
	f.drain(t)
	if f.recorder.seen[EvtTaskAutoRedispatched] != 0 {
		t.Fatal("ineligible task must not emit an auto-redispatch event")
	}
}

// TestAutoRedispatch_RetryCapThenExhausted is the CLASS-GUARD against an infinite
// re-dispatch loop: with the assignee perpetually eligible but never picking the
// work up, the reconciler re-dispatches at most maxAttempts times, then backs off
// and emits EXACTLY ONE exhausted event (manual recovery from there). Inverse
// mutation (drop the `attempts >= maxAttempts` guard) → re-dispatch every tick,
// exhausted never fires → this test fails.
func TestAutoRedispatch_RetryCapThenExhausted(t *testing.T) {
	f := newRedispatchFixture(t)
	f.seedStuckTask(t, "T1", "agent:ag-1", taskBlockedOnFailureReason)
	gate := &stubGate{eligible: true}
	const cap = 2
	r := NewAutoRedispatchReconciler(f.svc, gate, f.clk, cap, 0, nil)

	total := 0
	for i := 0; i < cap+3; i++ { // tick well past the cap
		n, err := r.Tick(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		total += n
		f.drain(t)
	}
	if total != cap {
		t.Fatalf("total re-dispatches = %d, want exactly cap=%d", total, cap)
	}
	if f.recorder.seen[EvtTaskAutoRedispatched] != cap {
		t.Fatalf("auto-redispatched events = %d, want %d", f.recorder.seen[EvtTaskAutoRedispatched], cap)
	}
	if f.recorder.seen[EvtTaskAutoRedispatchExhausted] != 1 {
		t.Fatalf("exhausted events = %d, want exactly 1 (one-shot latch)", f.recorder.seen[EvtTaskAutoRedispatchExhausted])
	}
}

// TestAutoRedispatch_ResetsAttemptsOnRecovery guards the episode-scoping: when a
// task recovers (no longer stuck), its attempt budget is pruned, so a LATER stuck
// episode gets a fresh budget. Inverse mutation (drop the prune block) → the second
// episode would be over-cap immediately and never re-dispatch → this test fails.
func TestAutoRedispatch_ResetsAttemptsOnRecovery(t *testing.T) {
	f := newRedispatchFixture(t)
	f.seedStuckTask(t, "T1", "agent:ag-1", taskBlockedOnFailureReason)
	gate := &stubGate{eligible: true}
	r := NewAutoRedispatchReconciler(f.svc, gate, f.clk, 1, 0, nil) // cap = 1

	// Episode 1: one re-dispatch, then exhausted.
	if n, _ := r.Tick(f.ctx); n != 1 {
		t.Fatalf("episode1 tick1 = %d, want 1", n)
	}
	if n, _ := r.Tick(f.ctx); n != 0 {
		t.Fatalf("episode1 tick2 (over cap) = %d, want 0", n)
	}

	// Recovery: clear the failure annotation (task re-activated).
	f.setReason(t, "T1", "", 2)
	if n, _ := r.Tick(f.ctx); n != 0 {
		t.Fatalf("recovered tick = %d, want 0 (not stuck)", n)
	}

	// Episode 2: re-stuck → budget must have reset → one fresh re-dispatch.
	f.setReason(t, "T1", taskBlockedOnFailureReason, 3)
	if n, _ := r.Tick(f.ctx); n != 1 {
		t.Fatalf("episode2 tick = %d, want 1 (budget reset on recovery)", n)
	}
}

// TestAutoRedispatch_FilterExcludesNonStaleReasonAndHumans: only the system stale-
// release reason on an AGENT assignee is auto-redispatched. A human-authored block
// note, a healthy running task, and a human assignee are all excluded by the
// FILTER — they never even reach the eligibility gate.
func TestAutoRedispatch_FilterExcludesNonStaleReasonAndHumans(t *testing.T) {
	f := newRedispatchFixture(t)
	f.seedStuckTask(t, "T-humannote", "agent:ag-1", "waiting on design review") // not the stale-release reason
	f.seedStuckTask(t, "T-healthy", "agent:ag-1", "")                           // running, not stuck
	f.seedStuckTask(t, "T-human", "user:alice", taskBlockedOnFailureReason)     // stale reason but human assignee
	gate := &stubGate{eligible: true}
	r := NewAutoRedispatchReconciler(f.svc, gate, f.clk, 0, 0, nil)

	n, err := r.Tick(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("redispatched = %d, want 0 (all excluded by filter)", n)
	}
	if gate.calls != 0 {
		t.Fatalf("gate called %d times — filter must exclude before the gate", gate.calls)
	}
}

// TestAutoRedispatch_FilterExcludesArchived: an archived (read-only) task is never
// auto-redispatched even if it carries the stale-release reason on an agent.
func TestAutoRedispatch_FilterExcludesArchived(t *testing.T) {
	f := newRedispatchFixture(t)
	now := f.clk.Now()
	task, err := pm.RehydrateTask(pm.RehydrateTaskInput{
		ID: "T-arch", ProjectID: "proj-1", Title: "archived", Status: pm.TaskRunning,
		Assignee: "agent:ag-1", BlockedReason: taskBlockedOnFailureReason, CreatedBy: "user:a",
		CreatedAt: now, UpdatedAt: now, Version: 1, StatusChangedAt: now,
		ArchivedAt: &now, ArchivedBy: "user:a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.tasks.Save(f.ctx, task); err != nil {
		t.Fatal(err)
	}
	gate := &stubGate{eligible: true}
	r := NewAutoRedispatchReconciler(f.svc, gate, f.clk, 0, 0, nil)
	if n, _ := r.Tick(f.ctx); n != 0 {
		t.Fatalf("redispatched = %d, want 0 (archived excluded)", n)
	}
}

// TestAutoRedispatch_RedispatchNoOpWhenNotStuck: the system re-dispatch method
// re-verifies under its tx and is a no-op (idempotent) if the task is no longer a
// stuck-released running task — e.g. it recovered concurrently between the gate
// decision and the tx.
func TestAutoRedispatch_RedispatchNoOpWhenNotStuck(t *testing.T) {
	f := newRedispatchFixture(t)
	now := f.clk.Now()
	open, err := pm.RehydrateTask(pm.RehydrateTaskInput{
		ID: "T-open", ProjectID: "proj-1", Title: "open", Status: pm.TaskOpen,
		Assignee: "agent:ag-1", CreatedBy: "user:a", CreatedAt: now, UpdatedAt: now, Version: 1, StatusChangedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.tasks.Save(f.ctx, open); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.autoRedispatchStuckTask(f.ctx, "T-open", 1); err != nil {
		t.Fatalf("no-op re-dispatch errored: %v", err)
	}
	f.drain(t)
	if f.recorder.seen[EvtTaskAutoRedispatched] != 0 || f.recorder.seen[EvtTaskAssigned] != 0 {
		t.Fatal("re-dispatch of a non-stuck task must be a no-op (no events)")
	}
}

// TestAutoRedispatch_RunStopsOnContextCancel exercises the Run loop: it ticks on
// the configured cadence (re-dispatching the eligible stuck task) and returns when
// the context is canceled.
func TestAutoRedispatch_RunStopsOnContextCancel(t *testing.T) {
	f := newRedispatchFixture(t)
	f.seedStuckTask(t, "T1", "agent:ag-1", taskBlockedOnFailureReason)
	gate := &stubGate{eligible: true}
	r := NewAutoRedispatchReconciler(f.svc, gate, f.clk, 100, 5*time.Millisecond, nil)

	ctx, cancel := context.WithCancel(f.ctx)
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	time.Sleep(40 * time.Millisecond) // let a few ticks fire
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if gate.calls == 0 {
		t.Fatal("expected Run to have ticked at least once")
	}
}
