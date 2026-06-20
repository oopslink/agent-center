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

// seedCronSkip builds & saves an active daily-09:00 cron reminder whose
// skip_if_overlap flag is `skip`, and returns it.
func seedCronSkip(t *testing.T, ctx context.Context, repo *remindersqlite.ReminderRepo, id string, skip bool) *reminder.Reminder {
	t.Helper()
	r, err := reminder.NewReminder(reminder.NewReminderInput{
		ID: id, OrganizationID: "org-1", ProjectID: "proj-1",
		CreatorRef: "agent:AG1", CreatorProjectID: "proj-1", RemindeeAgentID: "AG2",
		Schedule: reminder.CronScheduleAt("0 9 * * *", "UTC"), Content: "daily",
		SkipIfOverlap: skip, EndCondition: reminder.NeverEnd(), Now: t0,
	})
	if err != nil {
		t.Fatalf("NewReminder: %v", err)
	}
	if err := repo.Save(ctx, r); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return r
}

func latestOutcome(t *testing.T, ctx context.Context, repo *remindersqlite.ReminderRepo, id string) reminder.FiringOutcome {
	t.Helper()
	fs, err := repo.ListFirings(ctx, id)
	if err != nil {
		t.Fatalf("ListFirings: %v", err)
	}
	if len(fs) == 0 {
		t.Fatalf("no firings for %s", id)
	}
	return fs[0].Outcome // newest-first
}

// When skip_if_overlap=true and the previous fire is still in flight (a pending
// firing exists), the due occurrence is SKIPPED: no fired event, a skipped_overlap
// firing is recorded, next_run_at advances and fired_count is NOT bumped.
func TestScheduler_SkipIfOverlap_PrevPending_Skips(t *testing.T) {
	ctx, _, repo, sched, emitter := setup(t)
	seedCronSkip(t, ctx, repo, "rmd-skip", true)
	// Previous fire dispatched but not yet delivered → in flight.
	if err := repo.AppendFiring(ctx, reminder.Firing{
		ID: "prev", ReminderID: "rmd-skip", FiredAt: t0, Outcome: reminder.OutcomePending,
	}); err != nil {
		t.Fatal(err)
	}

	due := time.Date(2026, 6, 17, 9, 0, 0, 0, time.UTC)
	n, err := sched.Tick(ctx, due)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if n != 0 {
		t.Errorf("Tick reported n=%d fired, want 0 (skipped, not fired)", n)
	}
	if emitter.count(EventReminderFired) != 0 {
		t.Errorf("fired events=%d, want 0 (overlap must not deliver)", emitter.count(EventReminderFired))
	}
	if got := latestOutcome(t, ctx, repo, "rmd-skip"); got != reminder.OutcomeSkippedOverlap {
		t.Errorf("latest firing outcome=%s, want skipped_overlap", got)
	}
	got, _ := repo.Get(ctx, "rmd-skip")
	if got.Status() != reminder.StatusActive || got.FiredCount() != 0 {
		t.Errorf("after skip: status=%s firedCount=%d, want active/0", got.Status(), got.FiredCount())
	}
	wantNext := time.Date(2026, 6, 18, 9, 0, 0, 0, time.UTC)
	if got.NextRunAt() == nil || !got.NextRunAt().Equal(wantNext) {
		t.Errorf("next_run_at=%v, want %v (advanced)", got.NextRunAt(), wantNext)
	}
}

// skip_if_overlap=true but the previous fire is already processed (no pending
// firing) → fires normally.
func TestScheduler_SkipIfOverlap_NoPending_Fires(t *testing.T) {
	ctx, _, repo, sched, emitter := setup(t)
	seedCronSkip(t, ctx, repo, "rmd-skip2", true)
	// A delivered firing is NOT in flight; must not block the next fire.
	_ = repo.AppendFiring(ctx, reminder.Firing{
		ID: "prev", ReminderID: "rmd-skip2", FiredAt: t0, Outcome: reminder.OutcomeDelivered,
	})

	n, err := sched.Tick(ctx, time.Date(2026, 6, 17, 9, 0, 0, 0, time.UTC))
	if err != nil || n != 1 {
		t.Fatalf("Tick: n=%d err=%v, want 1", n, err)
	}
	if emitter.count(EventReminderFired) != 1 {
		t.Errorf("fired events=%d, want 1", emitter.count(EventReminderFired))
	}
	got, _ := repo.Get(ctx, "rmd-skip2")
	if got.FiredCount() != 1 {
		t.Errorf("firedCount=%d, want 1 (fired normally)", got.FiredCount())
	}
	// The fresh fire is recorded IN FLIGHT (pending), not delivered — delivery is
	// async and the projector resolves it later.
	if got := latestOutcome(t, ctx, repo, "rmd-skip2"); got != reminder.OutcomePending {
		t.Errorf("fresh fire outcome=%s, want pending (delivery is async)", got)
	}
}

// Full overlap lifecycle: a fire is recorded pending → while pending the next
// occurrence skips → once delivery resolves it to delivered, the following
// occurrence fires again (overlap cleared).
func TestScheduler_OverlapLifecycle_PendingThenDelivered(t *testing.T) {
	ctx, _, repo, sched, emitter := setup(t)
	seedCronSkip(t, ctx, repo, "rmd-life", true)

	// Day 1: fires → records pending.
	if n, err := sched.Tick(ctx, time.Date(2026, 6, 17, 9, 0, 0, 0, time.UTC)); err != nil || n != 1 {
		t.Fatalf("day1 Tick: n=%d err=%v, want 1", n, err)
	}
	if latestOutcome(t, ctx, repo, "rmd-life") != reminder.OutcomePending {
		t.Fatalf("day1 outcome != pending")
	}
	// Day 2: previous still pending → skips.
	if n, err := sched.Tick(ctx, time.Date(2026, 6, 18, 9, 0, 0, 0, time.UTC)); err != nil || n != 0 {
		t.Fatalf("day2 Tick: n=%d err=%v, want 0 (skip)", n, err)
	}
	if latestOutcome(t, ctx, repo, "rmd-life") != reminder.OutcomeSkippedOverlap {
		t.Fatalf("day2 outcome != skipped_overlap")
	}
	firedAfterSkip := emitter.count(EventReminderFired)

	// Delivery resolves the pending firing → delivered (what the projector does).
	fs, _ := repo.ListFirings(ctx, "rmd-life")
	var pendingID string
	for _, f := range fs {
		if f.Outcome == reminder.OutcomePending {
			pendingID = f.ID
		}
	}
	if pendingID == "" {
		t.Fatal("no pending firing to resolve")
	}
	if err := repo.UpdateFiringOutcome(ctx, pendingID, reminder.OutcomeDelivered); err != nil {
		t.Fatal(err)
	}

	// Day 3: overlap cleared → fires again.
	if n, err := sched.Tick(ctx, time.Date(2026, 6, 19, 9, 0, 0, 0, time.UTC)); err != nil || n != 1 {
		t.Fatalf("day3 Tick: n=%d err=%v, want 1 (overlap cleared)", n, err)
	}
	if emitter.count(EventReminderFired) != firedAfterSkip+1 {
		t.Errorf("day3 should emit one more fired event")
	}
}

// skip_if_overlap=false ignores overlap entirely: fires even with a pending firing.
func TestScheduler_SkipFalse_FiresDespitePending(t *testing.T) {
	ctx, _, repo, sched, emitter := setup(t)
	seedCronSkip(t, ctx, repo, "rmd-noskip", false)
	_ = repo.AppendFiring(ctx, reminder.Firing{
		ID: "prev", ReminderID: "rmd-noskip", FiredAt: t0, Outcome: reminder.OutcomePending,
	})

	n, err := sched.Tick(ctx, time.Date(2026, 6, 17, 9, 0, 0, 0, time.UTC))
	if err != nil || n != 1 {
		t.Fatalf("Tick: n=%d err=%v, want 1", n, err)
	}
	if emitter.count(EventReminderFired) != 1 {
		t.Errorf("fired events=%d, want 1 (skip=false ignores overlap)", emitter.count(EventReminderFired))
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
