package service

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

func setup(t *testing.T) (*Service, *outboxsql.OutboxRepo, context.Context) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ob := outboxsql.NewOutboxRepo(db)
	svc := New(Deps{
		DB:           db,
		Projects:     pmsql.NewProjectRepo(db),
		Members:      pmsql.NewProjectMemberRepo(db),
		Issues:       pmsql.NewIssueRepo(db),
		Tasks:        pmsql.NewTaskRepo(db),
		TaskSubs:     pmsql.NewTaskSubscriberRepo(db),
		IssueSubs:    pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db),
		Outbox:       ob,
		IDGen:        idgen.NewGenerator(clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())),
		Clock:        clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC()),
	})
	return svc, ob, context.Background()
}

// unprocessedTypes returns the event types currently in the outbox.
func unprocessedTypes(t *testing.T, ob *outboxsql.OutboxRepo, ctx context.Context) []string {
	t.Helper()
	evs, err := ob.FetchUnprocessed(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.EventType
	}
	return out
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestCreateProject_WritesStateMemberAndEvent(t *testing.T) {
	svc, ob, ctx := setup(t)
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "Acme", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	// creator is an owner member (write-gate bootstrap)
	m, err := svc.members.FindByProjectAndIdentity(ctx, pid, "user:a")
	if err != nil || m.Role() != pm.RoleOwner {
		t.Fatalf("creator should be owner member: %+v %v", m, err)
	}
	if !contains(unprocessedTypes(t, ob, ctx), EvtProjectCreated) {
		t.Fatal("expected pm.project.created outbox event")
	}
}

func TestAddProjectMember_GatedByMembership(t *testing.T) {
	svc, _, ctx := setup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	// non-member actor rejected
	if _, err := svc.AddProjectMember(ctx, AddProjectMemberCommand{ProjectID: pid, IdentityID: "user:b", Actor: "user:stranger"}); err != ErrNotMember {
		t.Fatalf("non-member add should be ErrNotMember, got %v", err)
	}
	// member actor ok
	if _, err := svc.AddProjectMember(ctx, AddProjectMemberCommand{ProjectID: pid, IdentityID: "user:b", Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
}

func TestCreateTask_EmitsCreatedWithCreatorSubscriber(t *testing.T) {
	svc, ob, ctx := setup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, err := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := svc.tasks.FindByID(ctx, tid)
	if tk == nil || tk.Status() != pm.TaskOpen {
		t.Fatal("task should be saved open")
	}
	// non-member cannot create a task
	if _, err := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "x", CreatedBy: "user:stranger"}); err != ErrNotMember {
		t.Fatalf("non-member create task should be ErrNotMember, got %v", err)
	}
	if !contains(unprocessedTypes(t, ob, ctx), EvtTaskCreated) {
		t.Fatal("expected pm.task.created outbox event")
	}
}

func TestEffectiveSubscribers_DerivesCreatorAssigneeManual(t *testing.T) {
	tk, _ := pm.NewTask(pm.NewTaskInput{ID: "T1", ProjectID: "P1", Title: "x", CreatedBy: "user:creator", CreatedAt: time.Unix(1, 0)})
	// no assignee, no manual → {creator}
	if got := EffectiveTaskSubscribers(tk, nil); len(got) != 1 || got[0] != "user:creator" {
		t.Fatalf("want [creator], got %v", got)
	}
	_ = tk.Assign("agent:c", time.Unix(2, 0))
	man := []*pm.TaskSubscriber{mustSub(t, "T1", "user:watcher")}
	got := EffectiveTaskSubscribers(tk, man)
	// {creator, agent:c, user:watcher} sorted
	if len(got) != 3 || !contains(got, "user:creator") || !contains(got, "agent:c") || !contains(got, "user:watcher") {
		t.Fatalf("effective set wrong: %v", got)
	}
}

func mustSub(t *testing.T, taskID, id string) *pm.TaskSubscriber {
	t.Helper()
	s, err := pm.NewTaskSubscriber(pm.TaskID(taskID), pm.IdentityRef(id), "user:o", time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCreateIssueAndSubscribe(t *testing.T) {
	svc, ob, ctx := setup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	iid, err := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: pid, Title: "bug", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(unprocessedTypes(t, ob, ctx), EvtIssueCreated) {
		t.Fatal("expected pm.issue.created event")
	}
	if _, err := svc.AddProjectMember(ctx, AddProjectMemberCommand{ProjectID: pid, IdentityID: "user:w", Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SubscribeIssue(ctx, iid, "user:w", "user:a"); err != nil {
		t.Fatal(err)
	}
	manual, _ := svc.issueSubs.ListByIssue(ctx, iid)
	if len(manual) != 1 {
		t.Fatalf("want 1 issue subscriber, got %d", len(manual))
	}
	if !contains(unprocessedTypes(t, ob, ctx), EvtIssueSubsChanged) {
		t.Fatal("expected pm.issue.subscribers_changed event")
	}
}

// TestDomainIsolation_CrossProjectRejected verifies an actor who is a member of
// one project cannot write to a task in another project (OQ6 domain isolation).
func TestDomainIsolation_CrossProjectRejected(t *testing.T) {
	svc, _, ctx := setup(t)
	p1, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P1", CreatedBy: "user:a"})
	p2, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P2", CreatedBy: "user:b"})
	// task in P2
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: p2, Title: "t", CreatedBy: "user:b"})
	_ = p1
	// user:a (member of P1 only) cannot subscribe a P2 task
	if err := svc.SubscribeTask(ctx, tid, "user:x", "user:a"); err != ErrNotMember {
		t.Fatalf("cross-project write must be ErrNotMember, got %v", err)
	}
}

func TestSubscribeUnsubscribe_CreatorStaysEffective(t *testing.T) {
	svc, ob, ctx := setup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})

	// subscribe a manual watcher
	if err := svc.SubscribeTask(ctx, tid, "user:watcher", "user:a"); err != nil {
		t.Fatal(err)
	}
	manual, _ := svc.taskSubs.ListByTask(ctx, tid)
	if len(manual) != 1 {
		t.Fatalf("want 1 manual subscriber, got %d", len(manual))
	}

	// "unsubscribe" the creator — only deletes manual rows; creator has none,
	// so the manual set is unchanged and the creator stays effective.
	if err := svc.UnsubscribeTask(ctx, tid, "user:a", "user:a"); err != nil {
		t.Fatal(err)
	}
	manual2, _ := svc.taskSubs.ListByTask(ctx, tid)
	if len(manual2) != 1 {
		t.Fatalf("unsubscribing creator must not touch manual rows, got %d", len(manual2))
	}
	tk, _ := svc.tasks.FindByID(ctx, tid)
	if !contains(EffectiveTaskSubscribers(tk, manual2), "user:a") {
		t.Fatal("creator must remain an effective subscriber")
	}
	// a subscribers_changed event was emitted
	if !contains(unprocessedTypes(t, ob, ctx), EvtTaskSubsChanged) {
		t.Fatal("expected pm.task.subscribers_changed event")
	}
}
