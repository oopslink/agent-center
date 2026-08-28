package api

import (
	"context"
	"errors"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// TestPMCreateTask_DispatchLandsRunnable covers issue-ca51e07c F2: the webconsole
// POST /tasks handler accepts a one-step `assignee` + `dispatch:true`, landing the
// new task in the project's first-class AssignmentPool — so it
// is immediately runnable (EnsureTaskRunnable == nil), no reconcile / manual
// pool-select. This is exactly what `install test-instance --with-agent` now does
// so the seeded task the agent is given actually runs (vs an assign-only backlog
// task that returns task_not_runnable and never executes).
func TestPMCreateTask_DispatchLandsRunnable(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, err := fx.deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "P", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	// Drain unrelated project/participant events.
	fx.drain(t)

	// One-step create→assign→dispatch: assignee = the owner (a same-org project
	// member) + dispatch:true.
	body := `{"title":"List the files","description":"run ls","assignee":"user:` + sess.IdentityID + `","dispatch":true}`
	resp := orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/tasks", body, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("create+dispatch status=%d", resp.StatusCode)
	}
	created := decodeBody(t, resp)
	taskID, _ := created["id"].(string)
	if taskID == "" {
		t.Fatal("create response missing task id")
	}
	if created["assignee"] != "user:"+sess.IdentityID {
		t.Fatalf("assignee=%v want user:%s", created["assignee"], sess.IdentityID)
	}
	// Pool membership is independent from Plan ownership.
	if planID, _ := created["plan_id"].(string); planID != "" {
		t.Fatalf("assignment-pool task plan_id=%q, want empty", planID)
	}
	fx.drain(t)

	poolResp := orgScopedGet(t, s.URL+"/api/projects/"+string(pid)+"/assignment-pool", sess)
	if poolResp.StatusCode != 200 {
		t.Fatalf("get assignment pool status=%d", poolResp.StatusCode)
	}
	pool := decodeBody(t, poolResp)
	tasks, _ := pool["tasks"].([]any)
	if len(tasks) != 1 || tasks[0].(map[string]any)["id"] != taskID {
		t.Fatalf("assignment pool tasks=%v, want [%s]", tasks, taskID)
	}
	plansResp := orgScopedGet(t, s.URL+"/api/projects/"+string(pid)+"/plans", sess)
	plansBody := decodeBody(t, plansResp)
	if got := len(plansBody["plans"].([]any)); got != 0 {
		t.Fatalf("assignment pool leaked into plans list: %d rows", got)
	}

	// The acceptance bit: the task is RUNNABLE (a dispatched pool member) — start_task
	// would no longer return task_not_runnable.
	if err := fx.deps.PM.EnsureTaskRunnable(ctx, pm.TaskID(taskID)); err != nil {
		t.Fatalf("EnsureTaskRunnable on a dispatched task = %v, want nil (runnable)", err)
	}
}

// TestPMTaskContainerExclusivity_AuthoritativeLists verifies the production HTTP
// reads, including compatibility with a duplicate pool row left by an older
// binary. Plan wins over Pool and Backlog; pending removal restores Backlog; only
// a later explicit pool dispatch moves it to AssignmentPool.
func TestPMTaskContainerExclusivity_AuthoritativeLists(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, err := fx.deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: sess.OrgID, Name: "exclusive", CreatedBy: caller,
	})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := fx.deps.PM.CreatePlan(ctx, pmservice.CreatePlanCommand{ProjectID: pid, Name: "pending", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := fx.deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{
		ProjectID: pid, Title: "one container", CreatedBy: caller, Dispatch: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fx.deps.PM.SelectTaskIntoPlan(ctx, planID, taskID, caller); err != nil {
		t.Fatal(err)
	}

	// Recreate a legacy duplicate after the fixed write has removed the real pool
	// row. The API must still exclude it from the Pool projection.
	pools := pmsql.NewAssignmentPoolRepo(db)
	pool, err := pools.FindByProject(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	member, err := pm.NewAssignmentPoolTask(pool.ID(), taskID, 0, caller, pool.CreatedAt())
	if err != nil {
		t.Fatal(err)
	}
	if err := pools.AddTask(ctx, member); err != nil {
		t.Fatal(err)
	}

	base := s.URL + "/api/projects/" + string(pid)
	poolBody := decodeBody(t, orgScopedGet(t, base+"/assignment-pool", sess))
	if got := len(poolBody["tasks"].([]any)); got != 0 {
		t.Fatalf("planned task visible in assignment pool: %v", poolBody["tasks"])
	}
	backlogBody := decodeBody(t, orgScopedGet(t, base+"/tasks?unplanned=1", sess))
	if got := len(backlogBody["tasks"].([]any)); got != 0 {
		t.Fatalf("planned task visible in backlog: %v", backlogBody["tasks"])
	}

	if err := fx.deps.PM.RemoveTaskFromPlan(ctx, planID, taskID, caller); err != nil {
		t.Fatal(err)
	}
	backlogBody = decodeBody(t, orgScopedGet(t, base+"/tasks?unplanned=1", sess))
	if rows := backlogBody["tasks"].([]any); len(rows) != 1 || rows[0].(map[string]any)["id"] != string(taskID) {
		t.Fatalf("pending-plan removal did not restore backlog: %v", rows)
	}

	// Pool dispatch remains a separate command after the task has returned to
	// Backlog; it must then disappear from Backlog and appear once in Pool.
	resp := orgScopedPost(t, base+"/assignment-pool/tasks", `{"task_id":"`+string(taskID)+`","priority":9}`, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("pool dispatch status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	backlogBody = decodeBody(t, orgScopedGet(t, base+"/tasks?unplanned=1", sess))
	if got := len(backlogBody["tasks"].([]any)); got != 0 {
		t.Fatalf("pool task remained in backlog: %v", backlogBody["tasks"])
	}
	poolBody = decodeBody(t, orgScopedGet(t, base+"/assignment-pool", sess))
	if rows := poolBody["tasks"].([]any); len(rows) != 1 || rows[0].(map[string]any)["id"] != string(taskID) {
		t.Fatalf("pool dispatch projection=%v", rows)
	}
}

// TestPMCreateTask_AssignOnlyStaysBacklog is the negative control: assign WITHOUT
// dispatch keeps the task in the backlog (no plan), so it is NOT runnable. This is
// the pre-fix behavior the --with-agent seed used to hit (assign-only →
// task_not_runnable → agent never ran).
func TestPMCreateTask_AssignOnlyStaysBacklog(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, err := fx.deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "P", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	fx.drain(t)

	body := `{"title":"backlog task","assignee":"user:` + sess.IdentityID + `"}` // no dispatch
	resp := orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/tasks", body, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("create status=%d", resp.StatusCode)
	}
	created := decodeBody(t, resp)
	taskID, _ := created["id"].(string)
	if _, ok := created["plan_id"]; ok {
		t.Fatalf("assign-only task must stay in backlog (no plan_id), got %v", created["plan_id"])
	}
	if err := fx.deps.PM.EnsureTaskRunnable(ctx, pm.TaskID(taskID)); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("EnsureTaskRunnable on a backlog task = %v, want ErrTaskNotRunnable", err)
	}
}
