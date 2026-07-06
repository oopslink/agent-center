package reminder

import (
	"errors"
	"testing"
	"time"
)

var oe0 = time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)

func newOnEventReminder(t *testing.T, oe OnEvent) *Reminder {
	t.Helper()
	r, err := NewReminder(NewReminderInput{
		ID: "rmd-ev", OrganizationID: "org-1", ProjectID: "proj-1",
		CreatorRef: "agent:AG1", CreatorProjectID: "proj-1", RemindeeAgentID: "AG2",
		Schedule: EventScheduleFor(), OnEvent: &oe, Content: "review it", Now: oe0,
	})
	if err != nil {
		t.Fatalf("NewReminder(on_event): %v", err)
	}
	return r
}

// The factory produces a DORMANT on_event reminder: active, no next_run_at, with the
// OnEvent trigger attached (it does NOT reject on "schedule yields no future run").
func TestNewReminder_OnEvent_Dormant(t *testing.T) {
	r := newOnEventReminder(t, OnEvent{EntityType: EntityTask, EntityID: "T1", Event: "completed", Delay: 10 * time.Minute})
	if r.Status() != StatusActive {
		t.Errorf("status=%s, want active (armed/awaiting)", r.Status())
	}
	if r.NextRunAt() != nil {
		t.Errorf("next_run_at=%v, want nil (dormant until armed)", r.NextRunAt())
	}
	if !r.IsOnEvent() || r.OnEvent() == nil {
		t.Errorf("IsOnEvent=%v OnEvent=%v, want true/non-nil", r.IsOnEvent(), r.OnEvent())
	}
	if r.IsArmed() {
		t.Errorf("IsArmed=true before any event; want false")
	}
	if r.IsDue(oe0.Add(time.Hour)) {
		t.Errorf("a dormant on_event reminder is never due")
	}
}

func TestOnEvent_Validate_Vocabulary(t *testing.T) {
	cases := []struct {
		name    string
		oe      OnEvent
		wantErr bool
	}{
		{"task completed", OnEvent{EntityType: EntityTask, EntityID: "T1", Event: "completed"}, false},
		{"task blocked", OnEvent{EntityType: EntityTask, EntityID: "T1", Event: "blocked"}, false},
		{"task discarded", OnEvent{EntityType: EntityTask, EntityID: "T1", Event: "discarded"}, false},
		{"task reopened", OnEvent{EntityType: EntityTask, EntityID: "T1", Event: "reopened"}, false},
		{"plan completed", OnEvent{EntityType: EntityPlan, EntityID: "P1", Event: "completed"}, false},
		{"plan failed", OnEvent{EntityType: EntityPlan, EntityID: "P1", Event: "failed"}, false},
		{"plan stopped", OnEvent{EntityType: EntityPlan, EntityID: "P1", Event: "stopped"}, false},
		{"issue closed", OnEvent{EntityType: EntityIssue, EntityID: "I1", Event: "closed"}, false},
		{"issue reopened", OnEvent{EntityType: EntityIssue, EntityID: "I1", Event: "reopened"}, false},
		{"unknown entity", OnEvent{EntityType: "milestone", EntityID: "M1", Event: "done"}, true},
		{"empty entity id", OnEvent{EntityType: EntityTask, EntityID: "", Event: "completed"}, true},
		{"issue with task event", OnEvent{EntityType: EntityIssue, EntityID: "I1", Event: "completed"}, true},
		{"plan with issue event", OnEvent{EntityType: EntityPlan, EntityID: "P1", Event: "closed"}, true},
		{"negative delay", OnEvent{EntityType: EntityTask, EntityID: "T1", Event: "completed", Delay: -time.Second}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.oe.validate()
			if c.wantErr && err == nil {
				t.Errorf("validate()=nil, want error")
			}
			if !c.wantErr && err != nil {
				t.Errorf("validate()=%v, want nil", err)
			}
			if c.wantErr && err != nil && !errors.Is(err, ErrInvalidOnEvent) {
				t.Errorf("validate() err=%v, want ErrInvalidOnEvent", err)
			}
		})
	}
}

// The factory rejects an on_event reminder whose trigger is invalid.
func TestNewReminder_OnEvent_RejectsInvalidTrigger(t *testing.T) {
	_, err := NewReminder(NewReminderInput{
		ID: "rmd-bad", OrganizationID: "org-1", ProjectID: "proj-1",
		CreatorRef: "agent:AG1", CreatorProjectID: "proj-1", RemindeeAgentID: "AG2",
		Schedule: EventScheduleFor(), OnEvent: &OnEvent{EntityType: EntityIssue, EntityID: "I1", Event: "completed"},
		Content: "x", Now: oe0,
	})
	if !errors.Is(err, ErrInvalidOnEvent) {
		t.Errorf("err=%v, want ErrInvalidOnEvent", err)
	}
}

// The cross-project guard (Invariant #2) applies to on_event reminders too: an
// agent creator may not target a remindee in a different project.
func TestNewReminder_OnEvent_CrossProjectGuard(t *testing.T) {
	_, err := NewReminder(NewReminderInput{
		ID: "rmd-xp", OrganizationID: "org-1", ProjectID: "proj-A",
		CreatorRef: "agent:AG1", CreatorProjectID: "proj-B", RemindeeAgentID: "AG2",
		Schedule: EventScheduleFor(),
		OnEvent:  &OnEvent{EntityType: EntityTask, EntityID: "T1", Event: "completed"},
		Content:  "x", Now: oe0,
	})
	if !errors.Is(err, ErrCrossProjectReminder) {
		t.Errorf("err=%v, want ErrCrossProjectReminder", err)
	}
	// An owner creator may cross projects.
	if _, err := NewReminder(NewReminderInput{
		ID: "rmd-xp-ok", OrganizationID: "org-1", ProjectID: "proj-A",
		CreatorRef: "user:owner", CreatorIsOwner: true, RemindeeAgentID: "AG2",
		Schedule: EventScheduleFor(),
		OnEvent:  &OnEvent{EntityType: EntityTask, EntityID: "T1", Event: "completed"},
		Content:  "x", Now: oe0,
	}); err != nil {
		t.Errorf("owner cross-project on_event create: %v", err)
	}
}

func TestOnEvent_Matches(t *testing.T) {
	oe := OnEvent{EntityType: EntityPlan, EntityID: "P1", Event: "completed"}
	if !oe.Matches(EntityPlan, "P1", "completed") {
		t.Errorf("Matches should be true for the exact triple")
	}
	if oe.Matches(EntityPlan, "P2", "completed") || oe.Matches(EntityTask, "P1", "completed") || oe.Matches(EntityPlan, "P1", "stopped") {
		t.Errorf("Matches should be false on any differing field")
	}
}

// Arm schedules the reminder at eventAt + delay and makes it due at that instant.
func TestReminder_Arm(t *testing.T) {
	r := newOnEventReminder(t, OnEvent{EntityType: EntityTask, EntityID: "T1", Event: "completed", Delay: 10 * time.Minute})
	eventAt := oe0.Add(time.Hour)
	if err := r.Arm(eventAt, eventAt); err != nil {
		t.Fatalf("Arm: %v", err)
	}
	wantNext := eventAt.Add(10 * time.Minute)
	if r.NextRunAt() == nil || !r.NextRunAt().Equal(wantNext) {
		t.Errorf("next_run_at=%v, want %v", r.NextRunAt(), wantNext)
	}
	if !r.IsArmed() {
		t.Errorf("IsArmed=false after Arm; want true")
	}
	if r.IsDue(wantNext.Add(-time.Second)) {
		t.Errorf("must not be due before next_run_at")
	}
	if !r.IsDue(wantNext) {
		t.Errorf("must be due at next_run_at")
	}
}

// Arming twice is idempotent — the second event does not shift the fire time (one-shot).
func TestReminder_Arm_Idempotent(t *testing.T) {
	r := newOnEventReminder(t, OnEvent{EntityType: EntityTask, EntityID: "T1", Event: "completed", Delay: time.Minute})
	first := oe0.Add(time.Hour)
	_ = r.Arm(first, first)
	firstNext := *r.NextRunAt()
	// A second (later) matching event must not re-arm/shift.
	if err := r.Arm(first.Add(time.Hour), first.Add(time.Hour)); err != nil {
		t.Fatalf("second Arm: %v", err)
	}
	if !r.NextRunAt().Equal(firstNext) {
		t.Errorf("next_run_at moved on re-arm: got %v want %v", r.NextRunAt(), firstNext)
	}
}

func TestReminder_Arm_Errors(t *testing.T) {
	// Paused reminder does not arm.
	paused := newOnEventReminder(t, OnEvent{EntityType: EntityTask, EntityID: "T1", Event: "completed"})
	_ = paused.Pause(oe0)
	if err := paused.Arm(oe0.Add(time.Hour), oe0.Add(time.Hour)); err == nil {
		t.Errorf("arming a paused reminder should error")
	}
	// Canceled (terminal) reminder does not arm.
	canceled := newOnEventReminder(t, OnEvent{EntityType: EntityTask, EntityID: "T1", Event: "completed"})
	_ = canceled.Cancel(oe0)
	if err := canceled.Arm(oe0.Add(time.Hour), oe0.Add(time.Hour)); !errors.Is(err, ErrReminderTerminal) {
		t.Errorf("arming a canceled reminder err=%v, want ErrReminderTerminal", err)
	}
	// A non-on_event reminder cannot be armed.
	once, _ := NewReminder(NewReminderInput{
		ID: "rmd-once", OrganizationID: "org-1", ProjectID: "proj-1",
		CreatorRef: "agent:AG1", CreatorProjectID: "proj-1", RemindeeAgentID: "AG2",
		Schedule: OnceScheduleAt(oe0.Add(time.Hour)), Content: "x", EndCondition: NeverEnd(), Now: oe0,
	})
	if err := once.Arm(oe0.Add(time.Hour), oe0.Add(time.Hour)); !errors.Is(err, ErrInvalidOnEvent) {
		t.Errorf("arming a once reminder err=%v, want ErrInvalidOnEvent", err)
	}
}

// An armed on_event reminder fires exactly once, then completes (one-shot consumption).
func TestReminder_OnEvent_RecordFire_OneShot(t *testing.T) {
	r := newOnEventReminder(t, OnEvent{EntityType: EntityPlan, EntityID: "P1", Event: "completed"})
	at := oe0.Add(time.Hour)
	_ = r.Arm(at, at)
	if err := r.RecordFire(at); err != nil {
		t.Fatalf("RecordFire: %v", err)
	}
	if r.Status() != StatusCompleted {
		t.Errorf("status=%s, want completed after one fire", r.Status())
	}
	if r.FiredCount() != 1 || r.NextRunAt() != nil {
		t.Errorf("firedCount=%d next_run_at=%v, want 1/nil", r.FiredCount(), r.NextRunAt())
	}
	// A completed reminder cannot fire again.
	if err := r.RecordFire(at.Add(time.Hour)); !errors.Is(err, ErrReminderTerminal) {
		t.Errorf("second RecordFire err=%v, want ErrReminderTerminal", err)
	}
}
