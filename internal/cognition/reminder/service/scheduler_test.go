package service

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/reminder"
	remindersqlite "github.com/oopslink/agent-center/internal/cognition/reminder/sqlite"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

var t0 = time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)

// fakeEmitter records emitted events without the observability stack.
type fakeEmitter struct {
	mu     sync.Mutex
	events []observability.EventType
}

func (f *fakeEmitter) Emit(_ context.Context, cmd observability.EmitCommand) (observability.EventID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, cmd.EventType)
	return observability.EventID("ev-" + strconv.Itoa(len(f.events))), nil
}

func (f *fakeEmitter) count(et observability.EventType) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.events {
		if e == et {
			n++
		}
	}
	return n
}

func setup(t *testing.T) (context.Context, *reminder.Reminder, *remindersqlite.ReminderRepo, *ReminderScheduler, *fakeEmitter) {
	_, _, repo, sched, emitter, _ := setupWithOutbox(t)
	return context.Background(), nil, repo, sched, emitter
}

// setupWithOutbox is setup plus the real outbox repo, so a test can assert the
// fired event actually lands in outbox_events (the channel the delivery
// projector drains — F1: without this the reminder is never delivered/woken).
func setupWithOutbox(t *testing.T) (context.Context, *reminder.Reminder, *remindersqlite.ReminderRepo, *ReminderScheduler, *fakeEmitter, *outboxsql.OutboxRepo) {
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
	emitter := &fakeEmitter{}
	var seq int
	sched := NewReminderScheduler(d, repo, emitter, outboxRepo, IDGenFunc(func() string {
		seq++
		return "fire-" + strconv.Itoa(seq)
	}))
	return context.Background(), nil, repo, sched, emitter, outboxRepo
}

func TestScheduler_FiresOnce_Completes(t *testing.T) {
	ctx, _, repo, sched, emitter := setup(t)
	r, _ := reminder.NewReminder(reminder.NewReminderInput{
		ID: "rmd-once", OrganizationID: "org-1", ProjectID: "proj-1",
		CreatorRef: "agent:AG1", CreatorProjectID: "proj-1", RemindeeAgentID: "AG2",
		Schedule: reminder.OnceScheduleAt(t0.Add(time.Hour)), Content: "ping",
		EndCondition: reminder.NeverEnd(), Now: t0,
	})
	if err := repo.Save(ctx, r); err != nil {
		t.Fatal(err)
	}

	// Not due yet.
	if n, err := sched.Tick(ctx, t0); err != nil || n != 0 {
		t.Fatalf("Tick(before): n=%d err=%v, want 0", n, err)
	}
	// Due → fire once → completed.
	n, err := sched.Tick(ctx, t0.Add(time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("Tick(due): n=%d err=%v, want 1", n, err)
	}
	got, _ := repo.Get(ctx, "rmd-once")
	if got.Status() != reminder.StatusCompleted || got.FiredCount() != 1 {
		t.Errorf("after fire: status=%s firedCount=%d", got.Status(), got.FiredCount())
	}
	if emitter.count(EventReminderFired) != 1 || emitter.count(EventReminderCompleted) != 1 {
		t.Errorf("events: fired=%d completed=%d, want 1/1", emitter.count(EventReminderFired), emitter.count(EventReminderCompleted))
	}
	// A second Tick fires nothing (no longer active/due).
	if n, _ := sched.Tick(ctx, t0.Add(2*time.Hour)); n != 0 {
		t.Errorf("Tick after completion: n=%d, want 0", n)
	}
}

// TestScheduler_FiredEvent_LandsInOutbox is the F1 ship-blocker regression: the
// fired event MUST be appended to the outbox (the channel the delivery projector
// drains), not only to the observability events table. Before the fix the fire
// path had no outbox.Append, so the remindee was never delivered/woken.
func TestScheduler_FiredEvent_LandsInOutbox(t *testing.T) {
	ctx, _, repo, sched, _, outboxRepo := setupWithOutbox(t)
	r, _ := reminder.NewReminder(reminder.NewReminderInput{
		ID: "rmd-outbox", OrganizationID: "org-7", ProjectID: "proj-7",
		CreatorRef: "agent:AG1", CreatorProjectID: "proj-7", RemindeeAgentID: "AG2",
		Schedule: reminder.OnceScheduleAt(t0.Add(time.Hour)), Content: "wake up",
		EndCondition: reminder.NeverEnd(), Now: t0,
	})
	if err := repo.Save(ctx, r); err != nil {
		t.Fatal(err)
	}

	if n, err := sched.Tick(ctx, t0.Add(time.Hour)); err != nil || n != 1 {
		t.Fatalf("Tick(due): n=%d err=%v, want 1", n, err)
	}

	evts, err := outboxRepo.FetchUnprocessed(ctx, 100)
	if err != nil {
		t.Fatalf("FetchUnprocessed: %v", err)
	}
	var fired *outbox.Event
	for i := range evts {
		if evts[i].EventType == string(EventReminderFired) {
			fired = &evts[i]
			break
		}
	}
	if fired == nil {
		t.Fatalf("no %s event in outbox; got %d events %+v", EventReminderFired, len(evts), evts)
	}
	// Payload must carry exactly the fields the delivery projector decodes.
	var pl struct {
		ReminderID      string `json:"reminder_id"`
		RemindeeAgentID string `json:"remindee_agent_id"`
		Content         string `json:"content"`
		OrganizationID  string `json:"organization_id"`
	}
	if err := json.Unmarshal([]byte(fired.Payload), &pl); err != nil {
		t.Fatalf("decode payload: %v (%s)", err, fired.Payload)
	}
	if pl.ReminderID != "rmd-outbox" || pl.RemindeeAgentID != "AG2" ||
		pl.Content != "wake up" || pl.OrganizationID != "org-7" {
		t.Errorf("payload mismatch: %+v", pl)
	}
}

func TestScheduler_Cron_Recurs_NoCompleteEvent(t *testing.T) {
	ctx, _, repo, sched, emitter := setup(t)
	r, _ := reminder.NewReminder(reminder.NewReminderInput{
		ID: "rmd-cron", OrganizationID: "org-1", ProjectID: "proj-1",
		CreatorRef: "agent:AG1", CreatorProjectID: "proj-1", RemindeeAgentID: "AG2",
		Schedule: reminder.CronScheduleAt("0 9 * * *", "UTC"), Content: "daily",
		EndCondition: reminder.NeverEnd(), Now: t0,
	})
	_ = repo.Save(ctx, r)

	// First run is 09:00 on the 17th; fire then.
	n, err := sched.Tick(ctx, time.Date(2026, 6, 17, 9, 0, 0, 0, time.UTC))
	if err != nil || n != 1 {
		t.Fatalf("Tick: n=%d err=%v", n, err)
	}
	got, _ := repo.Get(ctx, "rmd-cron")
	if got.Status() != reminder.StatusActive {
		t.Errorf("recurring should stay active, got %s", got.Status())
	}
	wantNext := time.Date(2026, 6, 18, 9, 0, 0, 0, time.UTC)
	if got.NextRunAt() == nil || !got.NextRunAt().Equal(wantNext) {
		t.Errorf("next_run_at=%v, want %v", got.NextRunAt(), wantNext)
	}
	if emitter.count(EventReminderFired) != 1 || emitter.count(EventReminderCompleted) != 0 {
		t.Errorf("events: fired=%d completed=%d, want 1/0", emitter.count(EventReminderFired), emitter.count(EventReminderCompleted))
	}
}

func TestScheduler_SkipsPaused(t *testing.T) {
	ctx, _, repo, sched, _ := setup(t)
	r, _ := reminder.NewReminder(reminder.NewReminderInput{
		ID: "rmd-paused", OrganizationID: "org-1", ProjectID: "proj-1",
		CreatorRef: "agent:AG1", CreatorProjectID: "proj-1", RemindeeAgentID: "AG2",
		Schedule: reminder.OnceScheduleAt(t0.Add(time.Hour)), Content: "x",
		EndCondition: reminder.NeverEnd(), Now: t0,
	})
	_ = r.Pause(t0.Add(time.Minute))
	_ = repo.Save(ctx, r)
	if n, _ := sched.Tick(ctx, t0.Add(2*time.Hour)); n != 0 {
		t.Errorf("paused reminder must not fire: n=%d", n)
	}
}
