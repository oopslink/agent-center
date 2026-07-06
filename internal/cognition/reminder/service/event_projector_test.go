package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition/reminder"
	remindersqlite "github.com/oopslink/agent-center/internal/cognition/reminder/sqlite"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

// fakeAudit records the reminder audit entries the projector/scheduler write.
type fakeAudit struct {
	mu      sync.Mutex
	entries []ReminderAuditEntry
}

func (f *fakeAudit) AppendReminderAudit(_ context.Context, e ReminderAuditEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, e)
	return nil
}

func (f *fakeAudit) count(changeType string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.entries {
		if e.ChangeType == changeType {
			n++
		}
	}
	return n
}

type eventEnv struct {
	ctx     context.Context
	db      *sql.DB
	repo    *remindersqlite.ReminderRepo
	outbox  *outboxsql.OutboxRepo
	applied *outboxsql.AppliedRepo
	sched   *ReminderScheduler
	proj    *ReminderEventProjector
	emitter *fakeEmitter
	audit   *fakeAudit
	clk     *clock.FakeClock
}

func newEventEnv(t *testing.T) *eventEnv {
	t.Helper()
	d, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := persistence.NewMigrator(d).Up(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := remindersqlite.NewReminderRepo(d)
	outboxRepo := outboxsql.NewOutboxRepo(d)
	applied := outboxsql.NewAppliedRepo(d)
	emitter := &fakeEmitter{}
	audit := &fakeAudit{}
	clk := clock.NewFakeClock(oe0)
	var seq int
	sched := NewReminderScheduler(d, repo, emitter, outboxRepo, IDGenFunc(func() string {
		seq++
		return "fire-" + strconv.Itoa(seq)
	}))
	sched.SetAudit(audit)
	proj := NewReminderEventProjector(d, repo, applied, clk, audit)
	return &eventEnv{
		ctx: context.Background(), db: d, repo: repo, outbox: outboxRepo, applied: applied,
		sched: sched, proj: proj, emitter: emitter, audit: audit, clk: clk,
	}
}

var oe0 = time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)

// createOnEvent saves a dormant on_event reminder watching (entityType,id,event) in
// projectID, notifying target after delay.
func (e *eventEnv) createOnEvent(t *testing.T, id, projectID, target string, oe reminder.OnEvent, content string) *reminder.Reminder {
	t.Helper()
	r, err := reminder.NewReminder(reminder.NewReminderInput{
		ID: id, OrganizationID: "org-1", ProjectID: projectID,
		CreatorRef: "agent:AG1", CreatorProjectID: projectID, RemindeeAgentID: target,
		Schedule: reminder.EventScheduleFor(), OnEvent: &oe, Content: content, Now: oe0,
	})
	if err != nil {
		t.Fatalf("NewReminder: %v", err)
	}
	if err := e.repo.Save(e.ctx, r); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return r
}

// taskEvent builds a pm.task.state_changed outbox event.
func taskStateEvent(id, taskID, projectID, status, reason string, at time.Time) outbox.Event {
	pl, _ := json.Marshal(map[string]any{
		"task_id": taskID, "project_id": projectID, "status": status, "reason": reason,
	})
	return outbox.Event{ID: id, EventType: evtTaskStateChanged, Payload: string(pl), CreatedAt: at}
}

func issueStateEvent(id, issueID, projectID, status string, at time.Time) outbox.Event {
	pl, _ := json.Marshal(map[string]any{"issue_id": issueID, "project_id": projectID, "status": status})
	return outbox.Event{ID: id, EventType: evtIssueStateChanged, Payload: string(pl), CreatedAt: at}
}

func planLifecycleEvent(id, eventType, planID, projectID string, at time.Time) outbox.Event {
	pl, _ := json.Marshal(map[string]any{"plan_id": planID, "project_id": projectID})
	return outbox.Event{ID: id, EventType: eventType, Payload: string(pl), CreatedAt: at}
}

// The headline flow: event triggers → delay → fire → @target + message.
func TestEventProjector_ArmsThenFires_Delayed_DeliversToTarget(t *testing.T) {
	e := newEventEnv(t)
	e.createOnEvent(t, "rmd-1", "proj-1", "AG2",
		reminder.OnEvent{EntityType: reminder.EntityTask, EntityID: "T1", Event: "completed", Delay: 10 * time.Minute},
		"@AG2 please review T1")

	eventAt := oe0.Add(time.Hour)
	if err := e.proj.Project(e.ctx, taskStateEvent("ev-1", "T1", "proj-1", "completed", "", eventAt)); err != nil {
		t.Fatalf("Project: %v", err)
	}

	// Armed: next_run_at = eventAt + delay, still active.
	got, _ := e.repo.Get(e.ctx, "rmd-1")
	wantNext := eventAt.Add(10 * time.Minute)
	if got.Status() != reminder.StatusActive || got.NextRunAt() == nil || !got.NextRunAt().Equal(wantNext) {
		t.Fatalf("after arm: status=%s next_run_at=%v, want active/%v", got.Status(), got.NextRunAt(), wantNext)
	}
	if e.audit.count("reminder_armed") != 1 {
		t.Errorf("armed audit=%d, want 1", e.audit.count("reminder_armed"))
	}

	// Not due before the delay elapses.
	if n, err := e.sched.Tick(e.ctx, wantNext.Add(-time.Second)); err != nil || n != 0 {
		t.Fatalf("Tick(before delay): n=%d err=%v, want 0", n, err)
	}
	// Due after the delay → fires once, completes.
	if n, err := e.sched.Tick(e.ctx, wantNext); err != nil || n != 1 {
		t.Fatalf("Tick(due): n=%d err=%v, want 1", n, err)
	}
	got, _ = e.repo.Get(e.ctx, "rmd-1")
	if got.Status() != reminder.StatusCompleted || got.FiredCount() != 1 {
		t.Errorf("after fire: status=%s firedCount=%d, want completed/1", got.Status(), got.FiredCount())
	}
	if e.audit.count("reminder_fired") != 1 {
		t.Errorf("fired audit=%d, want 1", e.audit.count("reminder_fired"))
	}

	// Delivery: drive the delivery projector off the fired outbox event and assert
	// the message reaches the @target (remindee == target).
	del := &fakeDeliverer{}
	dp := NewReminderDeliveryProjector(del, e.repo)
	evts, _ := e.outbox.FetchUnprocessed(e.ctx, 100)
	for _, ev := range evts {
		if ev.EventType == string(EventReminderFired) {
			if err := dp.Project(e.ctx, ev); err != nil {
				t.Fatalf("delivery Project: %v", err)
			}
		}
	}
	if del.calls != 1 {
		t.Fatalf("delivery calls=%d, want 1", del.calls)
	}
	if del.lastTo != "AG2" || del.lastMsg != "@AG2 please review T1" {
		t.Errorf("delivered to %q with %q, want AG2 / the message", del.lastTo, del.lastMsg)
	}
}

// One-shot consumption: a repeated / redelivered event must not re-arm or re-fire.
func TestEventProjector_OneShot(t *testing.T) {
	e := newEventEnv(t)
	e.createOnEvent(t, "rmd-os", "proj-1", "AG2",
		reminder.OnEvent{EntityType: reminder.EntityPlan, EntityID: "P1", Event: "completed"}, "done")

	at := oe0.Add(time.Hour)
	_ = e.proj.Project(e.ctx, planLifecycleEvent("ev-1", evtPlanCompleted, "P1", "proj-1", at))
	// Second, DISTINCT completion event while still armed → already armed → no shift.
	first := *mustGet(t, e.repo, "rmd-os").NextRunAt()
	_ = e.proj.Project(e.ctx, planLifecycleEvent("ev-2", evtPlanCompleted, "P1", "proj-1", at.Add(time.Hour)))
	if n := mustGet(t, e.repo, "rmd-os").NextRunAt(); !n.Equal(first) {
		t.Errorf("re-armed on second event: %v != %v", n, first)
	}

	// Fire it, then a THIRD event must not resurrect the completed reminder.
	if _, err := e.sched.Tick(e.ctx, first); err != nil {
		t.Fatal(err)
	}
	if mustGet(t, e.repo, "rmd-os").Status() != reminder.StatusCompleted {
		t.Fatalf("want completed after fire")
	}
	_ = e.proj.Project(e.ctx, planLifecycleEvent("ev-3", evtPlanCompleted, "P1", "proj-1", at.Add(2*time.Hour)))
	if got := mustGet(t, e.repo, "rmd-os"); got.Status() != reminder.StatusCompleted || got.FiredCount() != 1 {
		t.Errorf("resurrected: status=%s firedCount=%d, want completed/1", got.Status(), got.FiredCount())
	}
}

// Durable across restart: arming persists to the DB, so a FRESH scheduler (new
// process) built over the same DB still fires the armed reminder.
func TestEventProjector_RestartDurable(t *testing.T) {
	e := newEventEnv(t)
	e.createOnEvent(t, "rmd-dur", "proj-1", "AG2",
		reminder.OnEvent{EntityType: reminder.EntityIssue, EntityID: "I1", Event: "closed", Delay: 5 * time.Minute}, "closed")

	eventAt := oe0.Add(time.Hour)
	if err := e.proj.Project(e.ctx, issueStateEvent("ev-1", "I1", "proj-1", "closed", eventAt)); err != nil {
		t.Fatalf("Project: %v", err)
	}

	// Simulate a restart: brand-new repo + scheduler over the SAME db handle.
	repo2 := remindersqlite.NewReminderRepo(e.db)
	var seq int
	sched2 := NewReminderScheduler(e.db, repo2, &fakeEmitter{}, e.outbox, IDGenFunc(func() string {
		seq++
		return "f2-" + strconv.Itoa(seq)
	}))
	// The armed reminder survives; it fires from the persisted next_run_at.
	wantNext := eventAt.Add(5 * time.Minute)
	if n, err := sched2.Tick(e.ctx, wantNext); err != nil || n != 1 {
		t.Fatalf("post-restart Tick: n=%d err=%v, want 1", n, err)
	}
	if mustGet(t, repo2, "rmd-dur").Status() != reminder.StatusCompleted {
		t.Errorf("post-restart reminder did not fire/complete")
	}
}

// Permission: a reminder is only armed for events in its OWN project — an event in a
// different project must not arm it (no cross-project timing leak).
func TestEventProjector_Permission_SameProjectOnly(t *testing.T) {
	e := newEventEnv(t)
	// Reminder lives in proj-A; it watches task T9.
	e.createOnEvent(t, "rmd-perm", "proj-A", "AG2",
		reminder.OnEvent{EntityType: reminder.EntityTask, EntityID: "T9", Event: "completed"}, "x")

	// The SAME task id completes, but the event is scoped to a DIFFERENT project.
	if err := e.proj.Project(e.ctx, taskStateEvent("ev-x", "T9", "proj-B", "completed", "", oe0.Add(time.Hour))); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if got := mustGet(t, e.repo, "rmd-perm"); got.NextRunAt() != nil {
		t.Errorf("cross-project event armed the reminder (next_run_at=%v)", got.NextRunAt())
	}
	// Same project → arms.
	if err := e.proj.Project(e.ctx, taskStateEvent("ev-y", "T9", "proj-A", "completed", "", oe0.Add(time.Hour))); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if got := mustGet(t, e.repo, "rmd-perm"); got.NextRunAt() == nil {
		t.Errorf("same-project event did not arm the reminder")
	}
}

// Redelivery of the exact same event (same ID) is idempotent via the AppliedStore.
func TestEventProjector_Idempotent_Redelivery(t *testing.T) {
	e := newEventEnv(t)
	e.createOnEvent(t, "rmd-idem", "proj-1", "AG2",
		reminder.OnEvent{EntityType: reminder.EntityTask, EntityID: "T1", Event: "reopened"}, "x")
	ev := taskStateEvent("ev-dup", "T1", "proj-1", "reopened", "", oe0.Add(time.Hour))
	for i := 0; i < 3; i++ {
		if err := e.proj.Project(e.ctx, ev); err != nil {
			t.Fatalf("Project #%d: %v", i, err)
		}
	}
	if e.audit.count("reminder_armed") != 1 {
		t.Errorf("armed audit=%d after 3 redeliveries, want 1 (idempotent)", e.audit.count("reminder_armed"))
	}
}

// The transition→event vocabulary: each supported transition arms its watcher, and a
// non-vocabulary transition (running task with no block reason) arms nothing.
func TestEventProjector_Vocabulary(t *testing.T) {
	e := newEventEnv(t)
	// task blocked = running + a block reason (ADR-0046 — no blocked status).
	e.createOnEvent(t, "rmd-blocked", "proj-1", "AG2",
		reminder.OnEvent{EntityType: reminder.EntityTask, EntityID: "T1", Event: "blocked"}, "x")
	e.createOnEvent(t, "rmd-discarded", "proj-1", "AG2",
		reminder.OnEvent{EntityType: reminder.EntityTask, EntityID: "T2", Event: "discarded"}, "x")
	e.createOnEvent(t, "rmd-planstopped", "proj-1", "AG2",
		reminder.OnEvent{EntityType: reminder.EntityPlan, EntityID: "P1", Event: "stopped"}, "x")
	e.createOnEvent(t, "rmd-planfailed", "proj-1", "AG2",
		reminder.OnEvent{EntityType: reminder.EntityPlan, EntityID: "P2", Event: "failed"}, "x")
	e.createOnEvent(t, "rmd-issuereopened", "proj-1", "AG2",
		reminder.OnEvent{EntityType: reminder.EntityIssue, EntityID: "I1", Event: "reopened"}, "x")

	at := oe0.Add(time.Hour)
	_ = e.proj.Project(e.ctx, taskStateEvent("e1", "T1", "proj-1", "running", "waiting on input", at))
	_ = e.proj.Project(e.ctx, taskStateEvent("e2", "T2", "proj-1", "discarded", "", at))
	_ = e.proj.Project(e.ctx, planLifecycleEvent("e3", evtPlanStopped, "P1", "proj-1", at))
	_ = e.proj.Project(e.ctx, planLifecycleEvent("e4", evtPlanFailed, "P2", "proj-1", at))
	_ = e.proj.Project(e.ctx, issueStateEvent("e5", "I1", "proj-1", "reopened", at))
	// A plain running transition (no reason) must NOT arm the blocked watcher — build
	// a second blocked watcher on T3 and feed it a reasonless running event.
	e.createOnEvent(t, "rmd-noarm", "proj-1", "AG2",
		reminder.OnEvent{EntityType: reminder.EntityTask, EntityID: "T3", Event: "blocked"}, "x")
	_ = e.proj.Project(e.ctx, taskStateEvent("e6", "T3", "proj-1", "running", "", at))

	for _, id := range []string{"rmd-blocked", "rmd-discarded", "rmd-planstopped", "rmd-planfailed", "rmd-issuereopened"} {
		if got := mustGet(t, e.repo, id); got.NextRunAt() == nil {
			t.Errorf("%s was not armed", id)
		}
	}
	if got := mustGet(t, e.repo, "rmd-noarm"); got.NextRunAt() != nil {
		t.Errorf("reasonless running transition armed a blocked watcher")
	}
}

func mustGet(t *testing.T, repo *remindersqlite.ReminderRepo, id string) *reminder.Reminder {
	t.Helper()
	r, err := repo.Get(context.Background(), reminder.ReminderID(id))
	if err != nil {
		t.Fatalf("Get(%s): %v", id, err)
	}
	return r
}
