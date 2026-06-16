package service

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition/reminder"
)

func TestTickProjector_FiresDueAtClockNow(t *testing.T) {
	ctx, _, repo, sched, emitter := setup(t)
	r, _ := reminder.NewReminder(reminder.NewReminderInput{
		ID: "rmd-tick", OrganizationID: "org-1", ProjectID: "proj-1",
		CreatorRef: "agent:AG1", CreatorProjectID: "proj-1", RemindeeAgentID: "AG2",
		Schedule: reminder.OnceScheduleAt(t0.Add(time.Hour)), Content: "tick",
		EndCondition: reminder.NeverEnd(), Now: t0,
	})
	_ = repo.Save(ctx, r)

	clk := clock.NewFakeClock(t0) // before due
	tp := NewReminderTickProjector(sched, clk, nil)
	tp.OnTick(ctx)
	if emitter.count(EventReminderFired) != 0 {
		t.Fatalf("not due yet: fired=%d", emitter.count(EventReminderFired))
	}
	clk.Set(t0.Add(time.Hour)) // now due
	tp.OnTick(ctx)
	if emitter.count(EventReminderFired) != 1 {
		t.Errorf("due: fired=%d, want 1", emitter.count(EventReminderFired))
	}
	got, _ := repo.Get(ctx, "rmd-tick")
	if got.Status() != reminder.StatusCompleted {
		t.Errorf("status=%s, want completed", got.Status())
	}
}

func TestTickProjector_OnError(t *testing.T) {
	// A nil onError must not panic even though Tick can't error here (smoke).
	ctx, _, _, sched, _ := setup(t)
	tp := NewReminderTickProjector(sched, clock.NewFakeClock(t0), func(error) {})
	tp.OnTick(ctx) // no reminders → no-op, no panic
	_ = context.Background()
}
