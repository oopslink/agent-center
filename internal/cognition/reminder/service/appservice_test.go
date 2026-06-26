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

func (d *fakeDir) ResolveReminderContext(_ context.Context, _, _, _ string) (ReminderContext, error) {
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

// T207: the web console "全部" view — an org OWNER sees every reminder in the org
// (any creator); a non-owner is fail-closed to its OWN created reminders.
func TestApp_ListOrgReminders_OwnerSeesAll_NonOwnerOwnOnly(t *testing.T) {
	dir := sameProjectDir()
	dir.owners = map[string]bool{"user:owner": true}
	ctx, svc, _, _ := appSetup(t, dir)

	c1 := createCmd() // creator agent:AG1
	c2 := createCmd()
	c2.CreatorRef = "agent:AG2"
	if _, err := svc.CreateReminder(ctx, c1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateReminder(ctx, c2); err != nil {
		t.Fatal(err)
	}

	all, err := svc.ListOrgReminders(ctx, "org-1", "user:owner", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("owner org view = %d, want 2 (all creators)", len(all))
	}

	mine, err := svc.ListOrgReminders(ctx, "org-1", "agent:AG1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 1 || mine[0].CreatorRef() != "agent:AG1" {
		t.Fatalf("non-owner org view = %d (creator scope), want exactly 1 own", len(mine))
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

// T477: hard-delete removes the reminder row AND its firing history, under the
// same creator/owner authz gate as the other lifecycle ops, emitting deleted.
func TestApp_Delete_Authz_RemovesRowAndFirings(t *testing.T) {
	dir := sameProjectDir()
	dir.owners = map[string]bool{"user:owner": true}
	ctx, svc, repo, emitter := appSetup(t, dir)
	r, _ := svc.CreateReminder(ctx, createCmd())
	// Seed a firing so we can assert the history is cascaded on delete.
	if err := repo.AppendFiring(ctx, reminder.Firing{
		ID: "F1", ReminderID: r.ID().String(), FiredAt: t0, Outcome: reminder.OutcomeDelivered,
	}); err != nil {
		t.Fatalf("seed firing: %v", err)
	}

	// Stranger (not creator, not owner) → forbidden; nothing removed.
	if err := svc.DeleteReminder(ctx, DeleteReminderCommand{ID: r.ID(), RequesterRef: "agent:AGX"}); !errors.Is(err, ErrReminderForbidden) {
		t.Errorf("stranger delete: err=%v, want forbidden", err)
	}
	if got, _ := repo.Get(ctx, r.ID()); got == nil {
		t.Fatal("forbidden delete must NOT remove the row")
	}

	// Creator → delete OK; reminder + firings gone; deleted event emitted.
	if err := svc.DeleteReminder(ctx, DeleteReminderCommand{ID: r.ID(), RequesterRef: "agent:AG1"}); err != nil {
		t.Fatalf("creator delete: %v", err)
	}
	if _, err := repo.Get(ctx, r.ID()); !errors.Is(err, reminder.ErrReminderNotFound) {
		t.Errorf("after delete Get err=%v, want ErrReminderNotFound", err)
	}
	if fs, _ := repo.ListFirings(ctx, r.ID().String()); len(fs) != 0 {
		t.Errorf("firings after delete=%d, want 0 (cascaded)", len(fs))
	}
	if emitter.count(EventReminderDeleted) != 1 {
		t.Errorf("deleted event=%d, want 1", emitter.count(EventReminderDeleted))
	}

	// Re-deleting a now-absent reminder → ErrReminderNotFound.
	if err := svc.DeleteReminder(ctx, DeleteReminderCommand{ID: r.ID(), RequesterRef: "agent:AG1"}); !errors.Is(err, reminder.ErrReminderNotFound) {
		t.Errorf("re-delete: err=%v, want ErrReminderNotFound", err)
	}

	// Owner may delete another creator's reminder (owner authz).
	r2, _ := svc.CreateReminder(ctx, createCmd())
	if err := svc.DeleteReminder(ctx, DeleteReminderCommand{ID: r2.ID(), RequesterRef: "user:owner"}); err != nil {
		t.Fatalf("owner delete: %v", err)
	}
	if _, err := repo.Get(ctx, r2.ID()); !errors.Is(err, reminder.ErrReminderNotFound) {
		t.Errorf("owner delete left row: err=%v", err)
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
