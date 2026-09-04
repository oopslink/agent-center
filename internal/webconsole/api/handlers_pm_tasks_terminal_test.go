package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// TestPM_ListTasks_ExcludesTerminalByDefault is the v2.9.1 task-c91805fe guard: the
// project task/backlog list excludes terminal tasks (completed/discarded) by
// default, but surfaces them under an explicit ?status= filter — the same
// default-exclude / filter-to-see contract as archived projects (#298/#310). It
// also pins the ADR-0046 terminal set so a future state-machine drift is caught.
func TestPM_ListTasks_ExcludesTerminalByDefault(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	presp := orgScopedPost(t, s.URL+"/api/projects", `{"name":"P"}`, sess)
	if presp.StatusCode != http.StatusOK {
		t.Fatalf("create project status=%d", presp.StatusCode)
	}
	var pc map[string]any
	json.NewDecoder(presp.Body).Decode(&pc)
	pid := pc["id"].(string)

	mkTask := func(title string) string {
		resp := orgScopedPost(t, s.URL+"/api/projects/"+pid+"/tasks", `{"title":"`+title+`"}`, sess)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("create task %q status=%d", title, resp.StatusCode)
		}
		var c map[string]any
		json.NewDecoder(resp.Body).Decode(&c)
		return c["id"].(string)
	}
	setStatus := func(tid, status string) {
		resp := orgScopedPost(t, s.URL+"/api/projects/"+pid+"/tasks/"+tid+"/status", `{"status":"`+status+`","reason":"test transition"}`, sess)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("set %s=%s status=%d", tid, status, resp.StatusCode)
		}
	}

	openT := mkTask("open one")
	runningT := mkTask("running one")
	setStatus(runningT, "running")
	completedT := mkTask("done one")
	setStatus(completedT, "completed")
	discardedT := mkTask("dropped one")
	setStatus(discardedT, "discarded")

	list := func(query string) map[string]bool {
		resp := orgScopedGet(t, s.URL+"/api/projects/"+pid+"/tasks"+query, sess)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list %q status=%d", query, resp.StatusCode)
		}
		var l struct {
			Tasks []map[string]any `json:"tasks"`
		}
		json.NewDecoder(resp.Body).Decode(&l)
		ids := map[string]bool{}
		for _, tk := range l.Tasks {
			ids[tk["id"].(string)] = true
		}
		return ids
	}

	// Default → non-terminal only (open + running), terminal excluded.
	def := list("")
	if !def[openT] || !def[runningT] {
		t.Fatalf("default list must include non-terminal tasks, got %+v", def)
	}
	if def[completedT] || def[discardedT] {
		t.Fatalf("default list must EXCLUDE terminal (completed/discarded), got %+v", def)
	}

	// ?status=completed → only the completed task is reachable.
	if comp := list("?status=completed"); len(comp) != 1 || !comp[completedT] {
		t.Fatalf("?status=completed should be [completed], got %+v", comp)
	}

	// ?status=completed,discarded → both terminal, and ONLY those.
	term := list("?status=completed,discarded")
	if !term[completedT] || !term[discardedT] || term[openT] || term[runningT] {
		t.Fatalf("?status=completed,discarded should be exactly the terminal set, got %+v", term)
	}

	// ?status=all (T62/task-336335c5, reused by T76) → the escape hatch surfaces
	// EVERY status, terminal included, so the message ref resolver can resolve a
	// completed task. Must be the full set.
	if a := list("?status=all"); !a[openT] || !a[runningT] || !a[completedT] || !a[discardedT] {
		t.Fatalf("?status=all must include every status (terminal included), got %+v", a)
	}

	// The Backlog view (?unplanned=1) composes with the default terminal-exclude.
	unp := list("?unplanned=1")
	if !unp[openT] || !unp[runningT] || unp[completedT] || unp[discardedT] {
		t.Fatalf("unplanned backlog must exclude terminal by default, got %+v", unp)
	}
}

// TestListOrgTasks_StatusAll_IncludesTerminal_ForRefResolution (T62/task-336335c5,
// reused by T76/task-c780999a) is the faithful reproduction: the cross-project ORG
// task list (GET /api/tasks — the data source behind the message ref/T-number
// linkify resolver) excludes terminal tasks by default, so a reference to a
// COMPLETED task silently stayed plain text. ?status=all lets the resolver get it.
func TestListOrgTasks_StatusAll_IncludesTerminal_ForRefResolution(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	presp := orgScopedPost(t, s.URL+"/api/projects", `{"name":"P"}`, sess)
	if presp.StatusCode != http.StatusOK {
		t.Fatalf("create project status=%d", presp.StatusCode)
	}
	var pc map[string]any
	json.NewDecoder(presp.Body).Decode(&pc)
	pid := pc["id"].(string)

	mkTask := func(title string) string {
		resp := orgScopedPost(t, s.URL+"/api/projects/"+pid+"/tasks", `{"title":"`+title+`"}`, sess)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("create task %q status=%d", title, resp.StatusCode)
		}
		var c map[string]any
		json.NewDecoder(resp.Body).Decode(&c)
		return c["id"].(string)
	}
	openT := mkTask("open one")
	doneT := mkTask("done one")
	resp := orgScopedPost(t, s.URL+"/api/projects/"+pid+"/tasks/"+doneT+"/status", `{"status":"completed","reason":"test transition"}`, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("complete task status=%d", resp.StatusCode)
	}

	orgList := func(query string) map[string]bool {
		r := orgScopedGet(t, s.URL+"/api/tasks"+query, sess)
		ids := map[string]bool{}
		for _, tk := range decodeItems(t, r) {
			ids[tk["id"].(string)] = true
		}
		return ids
	}

	// Default ORG list: the completed task is INVISIBLE (the bug source).
	def := orgList("")
	if !def[openT] {
		t.Fatalf("default org list must include the open task, got %+v", def)
	}
	if def[doneT] {
		t.Fatalf("default org list must EXCLUDE the completed task (the bug source), got %+v", def)
	}

	// ?status=all: the resolver can now retrieve the completed task → ref linkifies.
	all := orgList("?status=all")
	if !all[openT] || !all[doneT] {
		t.Fatalf("?status=all org list must include BOTH open and completed, got %+v", all)
	}
}

func TestListTasks_HidesFailedTasksFromCompletedPlansByDefault(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()

	presp := orgScopedPost(t, s.URL+"/api/projects", `{"name":"P"}`, sess)
	if presp.StatusCode != http.StatusOK {
		t.Fatalf("create project status=%d", presp.StatusCode)
	}
	var pc map[string]any
	json.NewDecoder(presp.Body).Decode(&pc)
	pid := pc["id"].(string)

	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)
	activePlan, err := fx.deps.PM.CreatePlan(ctx, pmservice.CreatePlanCommand{ProjectID: pm.ProjectID(pid), Name: "active", CreatedBy: caller})
	if err != nil {
		t.Fatalf("CreatePlan active: %v", err)
	}
	donePlan, err := fx.deps.PM.CreatePlan(ctx, pmservice.CreatePlanCommand{ProjectID: pm.ProjectID(pid), Name: "done", CreatedBy: caller})
	if err != nil {
		t.Fatalf("CreatePlan done: %v", err)
	}
	mkFailedTaskInPlan := func(title string, planID pm.PlanID) string {
		tid, err := fx.deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pm.ProjectID(pid), Title: title, CreatedBy: caller})
		if err != nil {
			t.Fatalf("CreateTask %s: %v", title, err)
		}
		if err := fx.deps.PM.SelectTaskIntoPlan(ctx, planID, tid, caller); err != nil {
			t.Fatalf("SelectTaskIntoPlan %s: %v", title, err)
		}
		assignee := string(caller)
		if err := fx.deps.PM.BatchUpdateTask(ctx, tid, pmservice.BatchTaskPatch{Assignee: &assignee}, caller); err != nil {
			t.Fatalf("BatchUpdateTask assignee %s: %v", title, err)
		}
		if err := fx.deps.PM.StartPlan(ctx, planID, caller); err != nil {
			t.Fatalf("StartPlan %s: %v", planID, err)
		}
		if err := fx.deps.PM.FailTask(ctx, tid, "test failure", caller); err != nil {
			t.Fatalf("FailTask %s: %v", title, err)
		}
		return string(tid)
	}
	activeFailed := mkFailedTaskInPlan("active failed", activePlan)
	doneFailed := mkFailedTaskInPlan("done failed", donePlan)
	if err := fx.deps.PM.CompletePlanWithOptions(ctx, donePlan, caller, pmservice.CompletePlanOptions{
		Force:  true,
		Reason: "historical failure accepted",
	}); err != nil {
		t.Fatalf("CompletePlanWithOptions done: %v", err)
	}

	projectList := func(query string) map[string]map[string]any {
		resp := orgScopedGet(t, s.URL+"/api/projects/"+pid+"/tasks"+query, sess)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("project list %q status=%d", query, resp.StatusCode)
		}
		var l struct {
			Tasks []map[string]any `json:"tasks"`
			Total int              `json:"total"`
		}
		json.NewDecoder(resp.Body).Decode(&l)
		if l.Total != len(l.Tasks) {
			t.Fatalf("project list total=%d len=%d query=%q", l.Total, len(l.Tasks), query)
		}
		out := map[string]map[string]any{}
		for _, tk := range l.Tasks {
			out[tk["id"].(string)] = tk
		}
		return out
	}
	orgList := func(query string) map[string]map[string]any {
		resp := orgScopedGet(t, s.URL+"/api/tasks"+query, sess)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("org list %q status=%d", query, resp.StatusCode)
		}
		items := decodeItems(t, resp)
		out := map[string]map[string]any{}
		for _, tk := range items {
			out[tk["id"].(string)] = tk
		}
		return out
	}

	def := projectList("")
	if !containsTask(def, activeFailed) || containsTask(def, doneFailed) {
		t.Fatalf("default project list should include active failed and hide completed-plan failed, got %+v", def)
	}
	inc := projectList("?include_completed_plan_failures=1")
	if !containsTask(inc, activeFailed) || !containsTask(inc, doneFailed) {
		t.Fatalf("include flag should surface both failed tasks, got %+v", inc)
	}
	if inc[doneFailed]["plan_status"] != "done" {
		t.Fatalf("included historical row plan_status=%v want done", inc[doneFailed]["plan_status"])
	}
	explicitFailed := projectList("?status=failed")
	if !containsTask(explicitFailed, doneFailed) {
		t.Fatalf("explicit status=failed should still surface completed-plan failures, got %+v", explicitFailed)
	}
	orgDef := orgList("")
	if !containsTask(orgDef, activeFailed) || containsTask(orgDef, doneFailed) {
		t.Fatalf("default org list should include active failed and hide completed-plan failed, got %+v", orgDef)
	}
	orgInc := orgList("?include_completed_plan_failures=1")
	if !containsTask(orgInc, activeFailed) || !containsTask(orgInc, doneFailed) {
		t.Fatalf("include flag should surface both org failed tasks, got %+v", orgInc)
	}
}

func containsTask(rows map[string]map[string]any, id string) bool {
	_, ok := rows[id]
	return ok
}
