package sqlite

import (
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/reminder"
)

func newOnEvent(t *testing.T, id, entityID, event string, delay time.Duration) *reminder.Reminder {
	t.Helper()
	r, err := reminder.NewReminder(reminder.NewReminderInput{
		ID: id, OrganizationID: "org-1", ProjectID: "proj-1",
		CreatorRef: "agent:AG1", CreatorProjectID: "proj-1", RemindeeAgentID: "AG2",
		Schedule: reminder.EventScheduleFor(),
		OnEvent:  &reminder.OnEvent{EntityType: reminder.EntityTask, EntityID: entityID, Event: event, Delay: delay},
		Content:  "x", Now: t0,
	})
	if err != nil {
		t.Fatalf("NewReminder: %v", err)
	}
	return r
}

// The on_event trigger (all 4 columns) survives a Save/Get round-trip, and a dormant
// reminder persists with a NULL next_run_at.
func TestRepo_OnEvent_RoundTrip(t *testing.T) {
	ctx, repo := setup(t)
	r := newOnEvent(t, "rmd-oe", "T1", "completed", 90*time.Second)
	if err := repo.Save(ctx, r); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.Get(ctx, "rmd-oe")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Schedule().Kind != reminder.ScheduleEvent {
		t.Errorf("schedule kind=%s, want on_event", got.Schedule().Kind)
	}
	if got.NextRunAt() != nil {
		t.Errorf("dormant reminder next_run_at=%v, want nil", got.NextRunAt())
	}
	oe := got.OnEvent()
	if oe == nil {
		t.Fatalf("OnEvent lost on round-trip")
	}
	if oe.EntityType != reminder.EntityTask || oe.EntityID != "T1" || oe.Event != "completed" || oe.Delay != 90*time.Second {
		t.Errorf("on_event round-trip mismatch: %+v", oe)
	}
}

// FindArmedByEvent returns only DORMANT reminders matching the exact (type,id,event)
// triple — excluding already-armed, terminal, and non-matching reminders.
func TestRepo_FindArmedByEvent_Filters(t *testing.T) {
	ctx, repo := setup(t)

	// Dormant match — should be returned.
	dormant := newOnEvent(t, "rmd-dormant", "T1", "completed", 0)
	// Already armed (next_run_at set) — must be excluded (one-shot).
	armed := newOnEvent(t, "rmd-armed", "T1", "completed", 0)
	if err := armed.Arm(t0.Add(time.Hour), t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// Wrong event — excluded.
	wrongEvent := newOnEvent(t, "rmd-wrongevent", "T1", "reopened", 0)
	// Wrong entity id — excluded.
	wrongID := newOnEvent(t, "rmd-wrongid", "T2", "completed", 0)
	// Canceled (terminal) — excluded.
	canceled := newOnEvent(t, "rmd-canceled", "T1", "completed", 0)
	if err := canceled.Cancel(t0); err != nil {
		t.Fatal(err)
	}

	for _, r := range []*reminder.Reminder{dormant, armed, wrongEvent, wrongID, canceled} {
		if err := repo.Save(ctx, r); err != nil {
			t.Fatalf("Save %s: %v", r.ID(), err)
		}
	}

	got, err := repo.FindArmedByEvent(ctx, reminder.EntityTask, "T1", "completed")
	if err != nil {
		t.Fatalf("FindArmedByEvent: %v", err)
	}
	if len(got) != 1 || got[0].ID() != "rmd-dormant" {
		ids := make([]string, len(got))
		for i, r := range got {
			ids[i] = string(r.ID())
		}
		t.Errorf("FindArmedByEvent returned %v, want [rmd-dormant]", ids)
	}
}
