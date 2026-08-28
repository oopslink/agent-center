package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

func assignmentPoolSetup(t *testing.T) (*Service, *pmsql.PlanRepo, *pmsql.TaskRepo, context.Context) {
	t.Helper()
	db := openMigratedTestDB(t)
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

func taskIDs(tasks []*pm.Task) map[pm.TaskID]bool {
	out := make(map[pm.TaskID]bool, len(tasks))
	for _, task := range tasks {
		out[task.ID()] = true
	}
	return out
}

// TestTaskContainerExclusivity_PlanLifecycleAndExplicitReturn locks the Owner
// hotfix invariant at the authoritative AppService boundary. It covers ready and
// future/blocked DAG nodes, legacy duplicate pool rows, pending-only removal back
// to Backlog, and a later independent dispatch into AssignmentPool.
func TestTaskContainerExclusivity_PlanLifecycleAndExplicitReturn(t *testing.T) {
	svc, _, tasks, ctx := assignmentPoolSetup(t)
	projectID, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: projectID, Name: "delivery", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}

	var ids []pm.TaskID
	for _, title := range []string{"ready", "future", "blocked"} {
		id, err := svc.CreateTask(ctx, CreateTaskCommand{
			ProjectID: projectID, Title: title, CreatedBy: "user:a", Assignee: "user:a", Dispatch: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		if err := svc.SelectTaskIntoPlan(ctx, planID, id, "user:a"); err != nil {
			t.Fatalf("select %s from pool into plan: %v", title, err)
		}
		if inPool, err := svc.TaskInAssignmentPool(ctx, id); err != nil || inPool {
			t.Fatalf("%s retained pool membership: inPool=%v err=%v", title, inPool, err)
		}
	}
	if err := svc.AddPlanDependency(ctx, planID, ids[1], ids[0], "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddPlanDependency(ctx, planID, ids[2], ids[1], "user:a"); err != nil {
		t.Fatal(err)
	}
	detail, err := svc.GetPlanDetail(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[pm.TaskID]pm.NodeStatus{}
	for _, node := range detail.View.Nodes {
		statuses[node.TaskID] = node.NodeStatus
	}
	if statuses[ids[0]] != pm.NodeReady || statuses[ids[1]] != pm.NodeBlocked || statuses[ids[2]] != pm.NodeBlocked {
		t.Fatalf("pending node statuses=%v, want ready/future-blocked/blocked", statuses)
	}
	if backlog, err := svc.ListUnplannedTasks(ctx, projectID); err != nil || len(backlog) != 0 {
		t.Fatalf("planned nodes leaked to backlog: ids=%v err=%v", taskIDs(backlog), err)
	}

	// Simulate rows written by a pre-fix binary. The authoritative pool read must
	// exclude every planned task even before migration 0147 physically removes it.
	pool, err := svc.pools.FindByProject(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		member, err := pm.NewAssignmentPoolTask(pool.ID(), id, i, "user:a", svc.clock.Now())
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.pools.AddTask(ctx, member); err != nil {
			t.Fatal(err)
		}
	}
	poolDetail, err := svc.GetAssignmentPool(ctx, projectID, "user:a")
	if err != nil {
		t.Fatal(err)
	}
	if len(poolDetail.Tasks) != 0 {
		t.Fatalf("legacy duplicate projections visible in pool: %+v", poolDetail.Tasks)
	}
	for _, id := range ids {
		if inPool, err := svc.TaskInAssignmentPool(ctx, id); err != nil || inPool {
			t.Fatalf("authoritative membership exposed planned duplicate %s: inPool=%v err=%v", id, inPool, err)
		}
		if err := svc.ClaimPoolTask(ctx, id, "user:a"); !errors.Is(err, pm.ErrTaskNotClaimable) {
			t.Fatalf("planned duplicate %s claim=%v, want ErrTaskNotClaimable", id, err)
		}
	}

	if err := svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	running, err := svc.GetPlanDetail(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range running.View.Nodes {
		if node.TaskID == ids[0] && node.NodeStatus != pm.NodeReady {
			t.Fatalf("ready node in running plan=%s, want ready", node.NodeStatus)
		}
	}
	if backlog, err := svc.ListUnplannedTasks(ctx, projectID); err != nil || len(backlog) != 0 {
		t.Fatalf("running plan nodes leaked to backlog: ids=%v err=%v", taskIDs(backlog), err)
	}
	if poolDetail, err := svc.GetAssignmentPool(ctx, projectID, "user:a"); err != nil || len(poolDetail.Tasks) != 0 {
		t.Fatalf("running plan nodes leaked to pool: tasks=%v err=%v", poolDetail.Tasks, err)
	}

	// Only explicit removal from a pending Plan returns a live task to Backlog.
	pendingID, err := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: projectID, Name: "authoring", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	returnID, err := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: projectID, Title: "return", CreatedBy: "user:a", Dispatch: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SelectTaskIntoPlan(ctx, pendingID, returnID, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RemoveTaskFromPlan(ctx, pendingID, returnID, "user:a"); err != nil {
		t.Fatal(err)
	}
	backlog, err := svc.ListUnplannedTasks(ctx, projectID)
	if err != nil || !taskIDs(backlog)[returnID] {
		t.Fatalf("explicit pending-plan removal did not restore backlog: ids=%v err=%v", taskIDs(backlog), err)
	}
	if inPool, err := svc.TaskInAssignmentPool(ctx, returnID); err != nil || inPool {
		t.Fatalf("removed task implicitly entered pool: inPool=%v err=%v", inPool, err)
	}
	if err := svc.AddTaskToAssignmentPool(ctx, projectID, returnID, 7, "user:a"); err != nil {
		t.Fatalf("independent pool dispatch: %v", err)
	}
	backlog, err = svc.ListUnplannedTasks(ctx, projectID)
	if err != nil || taskIDs(backlog)[returnID] {
		t.Fatalf("pool-dispatched task still in backlog: ids=%v err=%v", taskIDs(backlog), err)
	}
	poolDetail, err = svc.GetAssignmentPool(ctx, projectID, "user:a")
	if err != nil || len(poolDetail.Tasks) != 1 || poolDetail.Tasks[0].Task.ID() != returnID {
		t.Fatalf("independent pool dispatch projection=%v err=%v", fmt.Sprint(poolDetail.Tasks), err)
	}

	// Storage truth agrees: the returned task has no Plan, exactly one pool row.
	stored, err := tasks.FindByID(ctx, returnID)
	if err != nil || stored.PlanID() != "" {
		t.Fatalf("returned task plan=%q err=%v", stored.PlanID(), err)
	}
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
	planID, _ := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: projectID, Name: "delivery", CreatedBy: "user:a"})
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
