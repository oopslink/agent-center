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

func assignmentPoolSetup(t *testing.T) (*Service, *pmsql.PlanRepo, *pmsql.TaskRepo, context.Context) {
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
	plans, tasks := pmsql.NewPlanRepo(db), pmsql.NewTaskRepo(db)
	svc := New(Deps{DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: tasks, TaskSubs: pmsql.NewTaskSubscriberRepo(db),
		IssueSubs: pmsql.NewIssueSubscriberRepo(db), CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db),
		Plans: plans, AssignmentPools: pmsql.NewAssignmentPoolRepo(db), Outbox: outboxsql.NewOutboxRepo(db),
		IDGen: gen, Clock: clk})
	return svc, plans, tasks, context.Background()
}

func TestAssignmentPool_IsNotPlanAndCanBeClaimedAnytime(t *testing.T) {
	svc, plans, tasks, ctx := assignmentPoolSetup(t)
	projectID, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := plans.ListByProject(ctx, projectID); err != nil || len(got) != 0 {
		t.Fatalf("project creation leaked pool into plans: plans=%v err=%v", got, err)
	}
	if _, err := svc.AddProjectMember(ctx, AddProjectMemberCommand{ProjectID: projectID, IdentityID: "agent:a", Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	poolTask, err := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: projectID, Title: "background", CreatedBy: "user:a", Dispatch: true})
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := tasks.FindByID(ctx, poolTask)
	if stored.PlanID() != "" {
		t.Fatalf("pool membership must not set task.plan_id, got %q", stored.PlanID())
	}

	// A structured Plan may exist and be running; explicit pool claim is still
	// allowed because background priority affects auto scheduling, not pull access.
	planID, _ := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: projectID, Name: "delivery", CreatedBy: "user:a", OwnerRef: "user:a"})
	planTask, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: projectID, Title: "foreground", CreatedBy: "user:a", Assignee: "user:a"})
	if err := svc.SelectTaskIntoPlan(ctx, planID, planTask, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}

	if err := svc.ClaimPoolTask(ctx, poolTask, "agent:a"); err != nil {
		t.Fatal(err)
	}
	claimed, _ := tasks.FindByID(ctx, poolTask)
	if claimed.Status() != pm.TaskOpen || claimed.Assignee() != "agent:a" {
		t.Fatalf("claim status/assignee = %s/%s", claimed.Status(), claimed.Assignee())
	}
	if err := svc.ReleasePoolTask(ctx, poolTask, "agent:a"); err != nil {
		t.Fatal(err)
	}
	released, _ := tasks.FindByID(ctx, poolTask)
	if released.Assignee() != "" || released.Status() != pm.TaskOpen {
		t.Fatalf("release status/assignee = %s/%s", released.Status(), released.Assignee())
	}
}
