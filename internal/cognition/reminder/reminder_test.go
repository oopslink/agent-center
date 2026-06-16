package reminder

import (
	"errors"
	"testing"
	"time"
)

// base creation instant used across tests (UTC, deterministic — no time.Now()).
var t0 = time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)

func validOnceInput() NewReminderInput {
	return NewReminderInput{
		ID:               "rmd-1",
		OrganizationID:   "org-1",
		ProjectID:        "proj-1",
		CreatorRef:       "agent:AG1",
		CreatorIsOwner:   false,
		CreatorProjectID: "proj-1",
		RemindeeAgentID:  "AG2",
		Schedule:         OnceScheduleAt(t0.Add(time.Hour)),
		Content:          "standup",
		EndCondition:     NeverEnd(),
		Now:              t0,
	}
}

func validCronInput() NewReminderInput {
	in := validOnceInput()
	in.ID = "rmd-cron"
	in.Schedule = CronScheduleAt("0 9 * * *", "UTC") // daily 09:00 UTC
	return in
}

func TestNewReminder_Once_OK(t *testing.T) {
	r, err := NewReminder(validOnceInput())
	if err != nil {
		t.Fatalf("NewReminder: %v", err)
	}
	if r.Status() != StatusActive {
		t.Errorf("status=%s, want active", r.Status())
	}
	if r.Version() != 1 {
		t.Errorf("version=%d, want 1", r.Version())
	}
	if r.NextRunAt() == nil || !r.NextRunAt().Equal(t0.Add(time.Hour)) {
		t.Errorf("next_run_at=%v, want %v", r.NextRunAt(), t0.Add(time.Hour))
	}
}

func TestNewReminder_Cron_NextRunAt_Timezone(t *testing.T) {
	in := validCronInput()
	// Daily 09:00 in Shanghai (UTC+8) → first run at 09:00 CST == 01:00 UTC next day
	// relative to a 12:00 UTC creation.
	in.Schedule = CronScheduleAt("0 9 * * *", "Asia/Shanghai")
	r, err := NewReminder(in)
	if err != nil {
		t.Fatalf("NewReminder cron: %v", err)
	}
	want := time.Date(2026, 6, 17, 1, 0, 0, 0, time.UTC) // 09:00 CST = 01:00 UTC
	if r.NextRunAt() == nil || !r.NextRunAt().Equal(want) {
		t.Errorf("next_run_at=%v, want %v (09:00 Asia/Shanghai)", r.NextRunAt(), want)
	}
}

func TestNewReminder_CrossProjectGuard(t *testing.T) {
	// agent creator targeting a different project → rejected.
	in := validOnceInput()
	in.ProjectID = "proj-2"
	in.CreatorProjectID = "proj-1"
	if _, err := NewReminder(in); !errors.Is(err, ErrCrossProjectReminder) {
		t.Fatalf("cross-project agent: err=%v, want ErrCrossProjectReminder", err)
	}
	// owner may cross projects.
	in.CreatorIsOwner = true
	in.CreatorRef = "user:owner"
	if _, err := NewReminder(in); err != nil {
		t.Fatalf("owner cross-project should be allowed: %v", err)
	}
}

func TestNewReminder_Validations(t *testing.T) {
	// once time in the past → ErrInvalidSchedule.
	past := validOnceInput()
	past.Schedule = OnceScheduleAt(t0.Add(-time.Hour))
	if _, err := NewReminder(past); !errors.Is(err, ErrInvalidSchedule) {
		t.Errorf("past once: err=%v, want ErrInvalidSchedule", err)
	}
	// bad cron expr.
	badCron := validCronInput()
	badCron.Schedule = CronScheduleAt("not a cron", "UTC")
	if _, err := NewReminder(badCron); !errors.Is(err, ErrInvalidSchedule) {
		t.Errorf("bad cron: err=%v, want ErrInvalidSchedule", err)
	}
	// bad timezone.
	badTz := validCronInput()
	badTz.Schedule = CronScheduleAt("0 9 * * *", "Mars/Phobos")
	if _, err := NewReminder(badTz); !errors.Is(err, ErrInvalidSchedule) {
		t.Errorf("bad tz: err=%v, want ErrInvalidSchedule", err)
	}
	// empty content / remindee.
	noContent := validOnceInput()
	noContent.Content = "  "
	if _, err := NewReminder(noContent); !errors.Is(err, ErrReminderContentEmpty) {
		t.Errorf("empty content: err=%v", err)
	}
	noRemindee := validOnceInput()
	noRemindee.RemindeeAgentID = ""
	if _, err := NewReminder(noRemindee); !errors.Is(err, ErrReminderRemindeeEmpty) {
		t.Errorf("empty remindee: err=%v", err)
	}
	// bad end condition.
	badEnd := validCronInput()
	badEnd.EndCondition = EndCondition{Kind: EndMaxCount, MaxCount: 0}
	if _, err := NewReminder(badEnd); !errors.Is(err, ErrInvalidEndCondition) {
		t.Errorf("bad end condition: err=%v", err)
	}
}

func TestPauseResume(t *testing.T) {
	r, _ := NewReminder(validCronInput())
	pauseAt := t0.Add(2 * time.Hour)
	if err := r.Pause(pauseAt); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if r.Status() != StatusPaused {
		t.Errorf("status=%s, want paused", r.Status())
	}
	if r.NextRunAt() != nil {
		t.Errorf("paused must clear next_run_at, got %v (Invariant #3)", r.NextRunAt())
	}
	// Pause is idempotent.
	if err := r.Pause(pauseAt); err != nil {
		t.Errorf("idempotent Pause: %v", err)
	}
	// Resume recomputes next_run_at strictly after `at`.
	resumeAt := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC) // after 09:00
	if err := r.Resume(resumeAt); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if r.Status() != StatusActive {
		t.Errorf("status=%s, want active", r.Status())
	}
	wantNext := time.Date(2026, 6, 18, 9, 0, 0, 0, time.UTC) // next 09:00 after resume
	if r.NextRunAt() == nil || !r.NextRunAt().Equal(wantNext) {
		t.Errorf("resumed next_run_at=%v, want %v", r.NextRunAt(), wantNext)
	}
}

func TestResume_OncePassed_Completes(t *testing.T) {
	r, _ := NewReminder(validOnceInput()) // once at t0+1h
	_ = r.Pause(t0.Add(30 * time.Minute))
	// Resume AFTER the once time → no future run → completed.
	if err := r.Resume(t0.Add(2 * time.Hour)); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if r.Status() != StatusCompleted {
		t.Errorf("status=%s, want completed (once time passed while paused)", r.Status())
	}
}

func TestRecordFire_Once_Completes(t *testing.T) {
	r, _ := NewReminder(validOnceInput())
	if err := r.RecordFire(t0.Add(time.Hour)); err != nil {
		t.Fatalf("RecordFire: %v", err)
	}
	if r.Status() != StatusCompleted {
		t.Errorf("status=%s, want completed", r.Status())
	}
	if r.FiredCount() != 1 || r.LastFiredAt() == nil {
		t.Errorf("firedCount=%d lastFired=%v, want 1 + set", r.FiredCount(), r.LastFiredAt())
	}
	if r.NextRunAt() != nil {
		t.Errorf("completed once must have nil next_run_at")
	}
}

func TestRecordFire_Cron_Recurs(t *testing.T) {
	r, _ := NewReminder(validCronInput()) // daily 09:00 UTC, never-end
	fireAt := time.Date(2026, 6, 17, 9, 0, 0, 0, time.UTC)
	if err := r.RecordFire(fireAt); err != nil {
		t.Fatalf("RecordFire: %v", err)
	}
	if r.Status() != StatusActive {
		t.Errorf("status=%s, want active (recurring)", r.Status())
	}
	wantNext := time.Date(2026, 6, 18, 9, 0, 0, 0, time.UTC)
	if r.NextRunAt() == nil || !r.NextRunAt().Equal(wantNext) {
		t.Errorf("recurring next_run_at=%v, want %v", r.NextRunAt(), wantNext)
	}
}

func TestRecordFire_MaxCount_Completes(t *testing.T) {
	in := validCronInput()
	in.EndCondition = EndCondition{Kind: EndMaxCount, MaxCount: 2}
	r, _ := NewReminder(in)
	_ = r.RecordFire(time.Date(2026, 6, 17, 9, 0, 0, 0, time.UTC)) // fire 1 → active
	if r.Status() != StatusActive {
		t.Fatalf("after fire 1 status=%s, want active", r.Status())
	}
	_ = r.RecordFire(time.Date(2026, 6, 18, 9, 0, 0, 0, time.UTC)) // fire 2 → completed
	if r.Status() != StatusCompleted {
		t.Errorf("after fire 2 status=%s, want completed (max_count=2)", r.Status())
	}
}

func TestRecordFire_Until_Completes(t *testing.T) {
	in := validCronInput()
	in.EndCondition = EndCondition{Kind: EndUntil, Until: time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)}
	r, _ := NewReminder(in)
	// Fire on the 17th: next would be the 18th 09:00, which is after Until → complete.
	_ = r.RecordFire(time.Date(2026, 6, 17, 9, 0, 0, 0, time.UTC))
	if r.Status() != StatusCompleted {
		t.Errorf("status=%s, want completed (until passed)", r.Status())
	}
}

func TestCancel_And_TerminalImmutability(t *testing.T) {
	r, _ := NewReminder(validCronInput())
	if err := r.Cancel(t0.Add(time.Hour)); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if r.Status() != StatusCanceled {
		t.Errorf("status=%s, want canceled", r.Status())
	}
	// Invariant #6: terminal is immutable — every mutator rejects.
	at := t0.Add(2 * time.Hour)
	if err := r.Pause(at); !errors.Is(err, ErrReminderTerminal) {
		t.Errorf("Pause on terminal: %v", err)
	}
	if err := r.Resume(at); !errors.Is(err, ErrReminderTerminal) {
		t.Errorf("Resume on terminal: %v", err)
	}
	if err := r.Cancel(at); !errors.Is(err, ErrReminderTerminal) {
		t.Errorf("Cancel on terminal: %v", err)
	}
	if err := r.Update(nil, "x", at); !errors.Is(err, ErrReminderTerminal) {
		t.Errorf("Update on terminal: %v", err)
	}
	if err := r.RecordFire(at); !errors.Is(err, ErrReminderTerminal) {
		t.Errorf("RecordFire on terminal: %v", err)
	}
}

func TestUpdate_ScheduleRecomputesNext(t *testing.T) {
	r, _ := NewReminder(validOnceInput())
	newSched := OnceScheduleAt(t0.Add(3 * time.Hour))
	if err := r.Update(&newSched, "new text", t0.Add(time.Minute)); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if r.Content() != "new text" {
		t.Errorf("content=%q, want updated", r.Content())
	}
	if r.NextRunAt() == nil || !r.NextRunAt().Equal(t0.Add(3*time.Hour)) {
		t.Errorf("next_run_at=%v, want recomputed to %v", r.NextRunAt(), t0.Add(3*time.Hour))
	}
}

func TestIsDue(t *testing.T) {
	r, _ := NewReminder(validOnceInput()) // next at t0+1h
	if r.IsDue(t0) {
		t.Errorf("not due before next_run_at")
	}
	if !r.IsDue(t0.Add(time.Hour)) {
		t.Errorf("due at next_run_at")
	}
	if !r.IsDue(t0.Add(2 * time.Hour)) {
		t.Errorf("due after next_run_at")
	}
	_ = r.Pause(t0.Add(30 * time.Minute))
	if r.IsDue(t0.Add(2 * time.Hour)) {
		t.Errorf("paused reminder is never due (Invariant #3)")
	}
}

func TestRehydrate_RoundTrips(t *testing.T) {
	next := t0.Add(time.Hour)
	r, err := Rehydrate(RehydrateInput{
		ID: "rmd-9", OrganizationID: "org-1", ProjectID: "proj-1",
		CreatorRef: "agent:AG1", RemindeeAgentID: "AG2",
		Schedule: OnceScheduleAt(next), Content: "hi",
		Status: StatusActive, NextRunAt: &next, SkipIfOverlap: true,
		EndCondition: NeverEnd(), FiredCount: 0, Version: 3,
		CreatedAt: t0, UpdatedAt: t0,
	})
	if err != nil {
		t.Fatalf("Rehydrate: %v", err)
	}
	if r.Version() != 3 || r.Status() != StatusActive || !r.SkipIfOverlap() {
		t.Errorf("rehydrated fields mismatch: v=%d status=%s skip=%v", r.Version(), r.Status(), r.SkipIfOverlap())
	}
	if _, err := Rehydrate(RehydrateInput{ID: "x", Status: "bogus", Version: 1}); err == nil {
		t.Errorf("invalid status should error")
	}
}
