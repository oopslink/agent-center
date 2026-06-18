package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/reminder"
	"github.com/oopslink/agent-center/internal/persistence"
)

var t0 = time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)

func setup(t *testing.T) (context.Context, *ReminderRepo) {
	t.Helper()
	d, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := persistence.NewMigrator(d).Up(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return context.Background(), NewReminderRepo(d)
}

func newOnce(t *testing.T, id string) *reminder.Reminder {
	t.Helper()
	r, err := reminder.NewReminder(reminder.NewReminderInput{
		ID: id, OrganizationID: "org-1", ProjectID: "proj-1",
		CreatorRef: "agent:AG1", CreatorProjectID: "proj-1", RemindeeAgentID: "AG2",
		Schedule: reminder.OnceScheduleAt(t0.Add(time.Hour)), Content: "standup",
		EndCondition: reminder.NeverEnd(), Now: t0,
	})
	if err != nil {
		t.Fatalf("NewReminder: %v", err)
	}
	return r
}

func TestRepo_SaveGet_RoundTrip(t *testing.T) {
	ctx, repo := setup(t)
	r := newOnce(t, "rmd-1")
	if err := repo.Save(ctx, r); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.Get(ctx, "rmd-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status() != reminder.StatusActive || got.Content() != "standup" || got.Version() != 1 {
		t.Errorf("round-trip mismatch: status=%s content=%q v=%d", got.Status(), got.Content(), got.Version())
	}
	if got.Schedule().Kind != reminder.ScheduleOnce || !got.Schedule().OnceAt.Equal(t0.Add(time.Hour)) {
		t.Errorf("schedule round-trip mismatch: %+v", got.Schedule())
	}
	if got.NextRunAt() == nil || !got.NextRunAt().Equal(t0.Add(time.Hour)) {
		t.Errorf("next_run_at round-trip: %v", got.NextRunAt())
	}
}

func TestRepo_Get_NotFound(t *testing.T) {
	ctx, repo := setup(t)
	if _, err := repo.Get(ctx, "missing"); err != reminder.ErrReminderNotFound {
		t.Fatalf("Get missing: err=%v, want ErrReminderNotFound", err)
	}
}

func TestRepo_Update_CAS(t *testing.T) {
	ctx, repo := setup(t)
	r := newOnce(t, "rmd-cas")
	_ = repo.Save(ctx, r)

	// Mutate + Update (version 1 → 2).
	if err := r.Pause(t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(ctx, r); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ := repo.Get(ctx, "rmd-cas")
	if got.Status() != reminder.StatusPaused || got.Version() != 2 || got.NextRunAt() != nil {
		t.Errorf("after update: status=%s v=%d next=%v", got.Status(), got.Version(), got.NextRunAt())
	}

	// Stale write: r still has version 1 → CAS miss → ErrReminderNotFound.
	_ = r.Resume(t0.Add(2 * time.Minute))
	if err := repo.Update(ctx, r); err != reminder.ErrReminderNotFound {
		t.Errorf("stale CAS Update: err=%v, want ErrReminderNotFound", err)
	}
}

func TestRepo_FindDue(t *testing.T) {
	ctx, repo := setup(t)
	r := newOnce(t, "rmd-due") // next_run_at = t0+1h, active
	_ = repo.Save(ctx, r)

	// Not due before next_run_at.
	if due, _ := repo.FindDue(ctx, t0); len(due) != 0 {
		t.Errorf("FindDue(before): got %d, want 0", len(due))
	}
	// Due at/after next_run_at.
	due, err := repo.FindDue(ctx, t0.Add(time.Hour))
	if err != nil {
		t.Fatalf("FindDue: %v", err)
	}
	if len(due) != 1 || due[0].ID() != "rmd-due" {
		t.Errorf("FindDue(after): got %+v", due)
	}
	// Paused reminders are not due.
	_ = r.Pause(t0.Add(30 * time.Minute))
	_ = repo.Update(ctx, r)
	if due, _ := repo.FindDue(ctx, t0.Add(2*time.Hour)); len(due) != 0 {
		t.Errorf("FindDue(paused): got %d, want 0", len(due))
	}
}

func TestRepo_ListFilters(t *testing.T) {
	ctx, repo := setup(t)
	a := newOnce(t, "rmd-a")
	_ = repo.Save(ctx, a)
	b := newOnce(t, "rmd-b")
	_ = b.Cancel(t0.Add(time.Minute))
	_ = repo.Save(ctx, b)

	all, _ := repo.ListByCreator(ctx, "agent:AG1", reminder.ListFilter{})
	if len(all) != 2 {
		t.Errorf("ListByCreator all: got %d, want 2", len(all))
	}
	active, _ := repo.ListByRemindee(ctx, "AG2", reminder.ListFilter{Statuses: []reminder.ReminderStatus{reminder.StatusActive}})
	if len(active) != 1 || active[0].ID() != "rmd-a" {
		t.Errorf("ListByRemindee active: got %+v", active)
	}
}

func TestRepo_AppendFiring(t *testing.T) {
	ctx, repo := setup(t)
	r := newOnce(t, "rmd-f")
	_ = repo.Save(ctx, r)
	if err := repo.AppendFiring(ctx, reminder.Firing{
		ID: "fire-1", ReminderID: "rmd-f", FiredAt: t0.Add(time.Hour),
		Outcome: reminder.OutcomeDelivered, Detail: "",
	}); err != nil {
		t.Fatalf("AppendFiring: %v", err)
	}
	if err := repo.AppendFiring(ctx, reminder.Firing{
		ID: "fire-2", ReminderID: "rmd-f", FiredAt: t0.Add(2 * time.Hour),
		Outcome: reminder.OutcomeSkippedOverlap, Detail: "still running",
	}); err != nil {
		t.Fatalf("AppendFiring 2: %v", err)
	}
}

func TestRepo_HasPendingFiring(t *testing.T) {
	ctx, repo := setup(t)
	r := newOnce(t, "rmd-p")
	_ = repo.Save(ctx, r)

	// No firings yet → not in flight.
	if pending, err := repo.HasPendingFiring(ctx, "rmd-p"); err != nil || pending {
		t.Fatalf("no firings: pending=%v err=%v, want false", pending, err)
	}
	// A delivered firing is processed → not in flight.
	_ = repo.AppendFiring(ctx, reminder.Firing{
		ID: "f-delivered", ReminderID: "rmd-p", FiredAt: t0.Add(time.Hour),
		Outcome: reminder.OutcomeDelivered,
	})
	// A skipped_overlap row is not a dispatch → not in flight.
	_ = repo.AppendFiring(ctx, reminder.Firing{
		ID: "f-skipped", ReminderID: "rmd-p", FiredAt: t0.Add(90 * time.Minute),
		Outcome: reminder.OutcomeSkippedOverlap,
	})
	if pending, err := repo.HasPendingFiring(ctx, "rmd-p"); err != nil || pending {
		t.Fatalf("delivered+skipped only: pending=%v err=%v, want false", pending, err)
	}
	// A pending firing = dispatched-but-undelivered → in flight.
	_ = repo.AppendFiring(ctx, reminder.Firing{
		ID: "f-pending", ReminderID: "rmd-p", FiredAt: t0.Add(2 * time.Hour),
		Outcome: reminder.OutcomePending,
	})
	if pending, err := repo.HasPendingFiring(ctx, "rmd-p"); err != nil || !pending {
		t.Fatalf("with pending firing: pending=%v err=%v, want true", pending, err)
	}
	// Scoped per reminder: another reminder's pending must not leak.
	if pending, err := repo.HasPendingFiring(ctx, "rmd-other"); err != nil || pending {
		t.Fatalf("other reminder: pending=%v err=%v, want false", pending, err)
	}
}
