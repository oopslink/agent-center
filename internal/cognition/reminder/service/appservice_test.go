package service

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition/reminder"
	remindersqlite "github.com/oopslink/agent-center/internal/cognition/reminder/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

type fakeDir struct {
	rc     ReminderContext
	rcErr  error
	owners map[string]bool
}

func (d *fakeDir) ResolveReminderContext(_ context.Context, _, _ string) (ReminderContext, error) {
	return d.rc, d.rcErr
}
func (d *fakeDir) IsOwner(_ context.Context, ref string) bool { return d.owners[ref] }

type fakeIDGen struct{ n int }

func (g *fakeIDGen) NewULID() string { g.n++; return "rmd-" + strconv.Itoa(g.n) }

func appSetup(t *testing.T, dir *fakeDir) (context.Context, *ReminderAppService, *remindersqlite.ReminderRepo, *fakeEmitter) {
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
	emitter := &fakeEmitter{}
	svc := NewReminderAppService(d, repo, dir, emitter, &fakeIDGen{}, clock.NewFakeClock(t0))
	return context.Background(), svc, repo, emitter
}

func sameProjectDir() *fakeDir {
	return &fakeDir{rc: ReminderContext{OrganizationID: "org-1", ProjectID: "proj-1", CreatorIsOwner: false, CreatorProjectID: "proj-1"}}
}

func createCmd() CreateReminderCommand {
	return CreateReminderCommand{
		CreatorRef: "agent:AG1", RemindeeAgentID: "AG2",
		Schedule: reminder.OnceScheduleAt(t0.Add(time.Hour)), Content: "ping",
		EndCondition: reminder.NeverEnd(),
	}
}

func TestApp_Create_OK_EmitsCreated(t *testing.T) {
	ctx, svc, repo, emitter := appSetup(t, sameProjectDir())
	r, err := svc.CreateReminder(ctx, createCmd())
	if err != nil {
		t.Fatalf("CreateReminder: %v", err)
	}
	if r.Status() != reminder.StatusActive || r.ProjectID() != "proj-1" {
		t.Errorf("created: status=%s project=%s", r.Status(), r.ProjectID())
	}
	got, _ := repo.Get(ctx, r.ID())
	if got == nil {
		t.Fatal("not persisted")
	}
	if emitter.count(EventReminderCreated) != 1 {
		t.Errorf("created event=%d, want 1", emitter.count(EventReminderCreated))
	}
}

func TestApp_Create_CrossProjectGuard(t *testing.T) {
	dir := &fakeDir{rc: ReminderContext{OrganizationID: "org-1", ProjectID: "proj-2", CreatorIsOwner: false, CreatorProjectID: "proj-1"}}
	ctx, svc, _, _ := appSetup(t, dir)
	if _, err := svc.CreateReminder(ctx, createCmd()); !errors.Is(err, reminder.ErrCrossProjectReminder) {
		t.Fatalf("cross-project: err=%v, want ErrCrossProjectReminder", err)
	}
	// owner may cross projects.
	dir.rc.CreatorIsOwner = true
	if _, err := svc.CreateReminder(ctx, createCmd()); err != nil {
		t.Fatalf("owner cross-project should pass: %v", err)
	}
}

func TestApp_Update_Authz(t *testing.T) {
	dir := sameProjectDir()
	dir.owners = map[string]bool{"user:owner": true}
	ctx, svc, _, emitter := appSetup(t, dir)
	r, _ := svc.CreateReminder(ctx, createCmd())

	// Stranger (not creator, not owner) → forbidden.
	if _, err := svc.UpdateReminder(ctx, UpdateReminderCommand{ID: r.ID(), RequesterRef: "agent:AGX", Action: ActionPause}); !errors.Is(err, ErrReminderForbidden) {
		t.Errorf("stranger pause: err=%v, want forbidden", err)
	}
	// Creator → pause OK + event.
	if _, err := svc.UpdateReminder(ctx, UpdateReminderCommand{ID: r.ID(), RequesterRef: "agent:AG1", Action: ActionPause}); err != nil {
		t.Fatalf("creator pause: %v", err)
	}
	if emitter.count(EventReminderPaused) != 1 {
		t.Errorf("paused event=%d, want 1", emitter.count(EventReminderPaused))
	}
	// Owner → resume OK.
	if _, err := svc.UpdateReminder(ctx, UpdateReminderCommand{ID: r.ID(), RequesterRef: "user:owner", Action: ActionResume}); err != nil {
		t.Fatalf("owner resume: %v", err)
	}
	// Creator → cancel OK (terminal).
	if _, err := svc.UpdateReminder(ctx, UpdateReminderCommand{ID: r.ID(), RequesterRef: "agent:AG1", Action: ActionCancel}); err != nil {
		t.Fatalf("creator cancel: %v", err)
	}
	got, _ := svc.GetReminder(ctx, r.ID(), "user:owner")
	if got.Status() != reminder.StatusCanceled {
		t.Errorf("status=%s, want canceled", got.Status())
	}
}

func TestApp_List_Filter(t *testing.T) {
	ctx, svc, _, _ := appSetup(t, sameProjectDir())
	a, _ := svc.CreateReminder(ctx, createCmd())
	_, _ = svc.CreateReminder(ctx, createCmd())
	_, _ = svc.UpdateReminder(ctx, UpdateReminderCommand{ID: a.ID(), RequesterRef: "agent:AG1", Action: ActionCancel})

	all, _ := svc.ListReminders(ctx, ListRemindersQuery{CreatorRef: "agent:AG1"})
	if len(all) != 2 {
		t.Errorf("list all by creator: got %d, want 2", len(all))
	}
	active, _ := svc.ListReminders(ctx, ListRemindersQuery{RemindeeAgentID: "AG2", Statuses: []reminder.ReminderStatus{reminder.StatusActive}})
	if len(active) != 1 {
		t.Errorf("list active by remindee: got %d, want 1", len(active))
	}
}
