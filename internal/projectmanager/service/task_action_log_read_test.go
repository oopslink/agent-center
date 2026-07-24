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

func taskActionLogReadSetup(t *testing.T) (*Service, context.Context) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	clk := clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	gen := idgen.NewGenerator(clk)
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Tasks: pmsql.NewTaskRepo(db), TaskSubs: pmsql.NewTaskSubscriberRepo(db), Outbox: outboxsql.NewOutboxRepo(db),
		TaskActionLogs: pmsql.NewTaskActionLogRepo(db, gen),
		AgentDir:       allOrgDir("org-1"),
		IDGen:          gen, Clock: clk,
	})
	return svc, context.Background()
}

func TestListTaskActionLogs_ReadsPersistedLifecycleWithPagination(t *testing.T) {
	svc, ctx := taskActionLogReadSetup(t)
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AssignTask(ctx, tid, "agent:c", "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartTask(ctx, tid, "agent:c"); err != nil {
		t.Fatal(err)
	}
	if err := svc.BlockTask(ctx, tid, "need secret", pm.BlockReasonObstacle, "agent:c"); err != nil {
		t.Fatal(err)
	}
	if err := svc.UnblockTask(ctx, UnblockTaskCommand{TaskID: tid, Actor: "agent:c", Comment: "approved"}); err != nil {
		t.Fatal(err)
	}

	rehydrated, err := svc.GetTask(ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if len(rehydrated.ActionLogs()) != 0 {
		t.Fatalf("GetTask should not hydrate action logs, got %d", len(rehydrated.ActionLogs()))
	}

	all, total, err := svc.ListTaskActionLogs(ctx, tid, 0, 10)
	if err != nil {
		t.Fatalf("ListTaskActionLogs: %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	seen := map[pm.TaskAction]bool{}
	for _, lg := range all {
		seen[lg.Action] = true
	}
	for _, want := range []pm.TaskAction{pm.TaskActionAgentStarted, pm.TaskActionBlocked, pm.TaskActionUnblocked} {
		if !seen[want] {
			t.Fatalf("all logs = %+v, missing %s", all, want)
		}
	}
	page, total, err := svc.ListTaskActionLogs(ctx, tid, 1, 2)
	if err != nil {
		t.Fatalf("ListTaskActionLogs page: %v", err)
	}
	if total != 3 || len(page) != 2 {
		t.Fatalf("page len=%d total=%d, want len=2 total=3", len(page), total)
	}
}
