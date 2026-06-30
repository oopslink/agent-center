package api

import (
	"context"
	"errors"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// TestPMCreateTask_DispatchLandsRunnable covers issue-ca51e07c F2: the webconsole
// POST /tasks handler accepts a one-step `assignee` + `dispatch:true`, landing the
// new task in the project's built-in Assignment Pool as a DISPATCHED node — so it
// is immediately runnable (EnsureTaskRunnable == nil), no reconcile / manual
// pool-select. This is exactly what `install test-instance --with-agent` now does
// so the seeded task the agent is given actually runs (vs an assign-only backlog
// task that returns task_not_runnable and never executes).
func TestPMCreateTask_DispatchLandsRunnable(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanAPI(t, deps) // plan-capable PM → CreateProject auto-creates the built-in pool
	s := newTestServer(t, fx.deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, err := fx.deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "P", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	// Materialize the auto-created pool's conversation (the create+dispatch path's
	// assign step joins the assignee to it).
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
	// Dispatched tasks carry their owning plan (the built-in pool) — NOT the backlog.
	planID, _ := created["plan_id"].(string)
	if planID == "" {
		t.Fatal("dispatched task must carry plan_id (built-in pool), got backlog (empty)")
	}
	fx.drain(t)

	// The plan_id is the project's BUILT-IN Assignment Pool (is_builtin=true).
	presp := orgScopedGet(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+planID, sess)
	if presp.StatusCode != 200 {
		t.Fatalf("get plan status=%d", presp.StatusCode)
	}
	plan := decodeBody(t, presp)
	if plan["is_builtin"] != true {
		t.Fatalf("dispatched task's plan is_builtin=%v want true (built-in pool)", plan["is_builtin"])
	}

	// The acceptance bit: the task is RUNNABLE (a dispatched pool member) — start_task
	// would no longer return task_not_runnable.
	if err := fx.deps.PM.EnsureTaskRunnable(ctx, pm.TaskID(taskID)); err != nil {
		t.Fatalf("EnsureTaskRunnable on a dispatched task = %v, want nil (runnable)", err)
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
