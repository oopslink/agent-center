package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// =============================================================================
// v2.7 D2 b2/d-ii-B — agent MCP passthrough tools (create_task, assign_task /
// reassign_task, subscribe / unsubscribe, get_task, get_issue).
//
// These reuse the writeToolsFixture (real admin server + AuthMiddleware over the
// full pm → outbox → projector pipeline). The WRITE tools go through the pm
// AppService whose own requireProjectMember is the write-gate (the agent is a
// ProjectMember of its assigned task's project via #5a). The READ tools scope
// per-agent STRICTLY to own work (membership is the WRITE gate, not read):
// get_task = own-work (requireOwnTask), get_issue = own-link (the agent holds a
// WorkItem for a Task derived from the issue).
// =============================================================================

// getBearer GETs path with an Authorization: Bearer <plaintext> header.
func getBearer(t *testing.T, base, path, bearer string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// seedMemberProject creates a project + task, assigns it to AG1 (which makes AG1
// a ProjectMember via #5a and creates AG1's WorkItem via the projector). Returns
// the project id + task id. AG1 is a member of this project after this returns.
func (f *writeToolsFixture) seedMemberProject(t *testing.T) (pm.ProjectID, string) {
	t.Helper()
	ctx := context.Background()
	owner := pm.IdentityRef("user:owner")
	pid, err := f.pmSvc.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: atTestOrg, Name: "Member", CreatedBy: owner,
	})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{
		ProjectID: pid, Title: "seed", CreatedBy: owner,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	if err := f.pmSvc.AssignTask(ctx, tid, pm.IdentityRef("agent:"+atAgent1), owner); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	return pid, string(tid)
}

// seedForeignProject creates a project the agent is NOT a member of (no
// assignment), returning its id.
func (f *writeToolsFixture) seedForeignProject(t *testing.T) pm.ProjectID {
	t.Helper()
	pid, err := f.pmSvc.CreateProject(context.Background(), pmservice.CreateProjectCommand{
		OrganizationID: atTestOrg, Name: "Foreign", CreatedBy: pm.IdentityRef("user:other"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

// --- list_tasks (v2.9.1 #T38) ------------------------------------------------

func TestListTasks_FiltersAndIsolation(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, seedTID := f.seedMemberProject(t) // task "seed" assigned to atAgent1 (open)
	ctx := context.Background()
	owner := pm.IdentityRef("user:owner")
	// A second task assigned to atAgent1, started → running. v2.14.0 I14/F3 §13.A:
	// StartTask now passes the run-ahead gate, so t2 must be a runnable (dispatched
	// built-in-pool) member first — a backlog task is not startable.
	t2, _ := f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "two", CreatedBy: owner})
	f.drain(t)
	plans, _ := f.pmSvc.ListPlans(ctx, pid)
	var pool pm.PlanID
	for _, p := range plans {
		if p.IsBuiltin() {
			pool = p.ID()
		}
	}
	if err := f.pmSvc.SelectTaskIntoPlan(ctx, pool, t2, owner); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	if err := f.pmSvc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	_ = f.pmSvc.AssignTask(ctx, t2, pm.IdentityRef("agent:"+atAgent1), owner)
	f.drain(t)
	if err := f.pmSvc.StartTask(ctx, t2, pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatalf("start t2: %v", err)
	}
	// A third task assigned to a different identity (open).
	t3, _ := f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "three", CreatedBy: owner})
	f.drain(t)
	_ = f.pmSvc.AssignTask(ctx, t3, pm.IdentityRef("user:bob"), owner)
	f.drain(t)
	srv := f.server(t)

	list := func(body map[string]any) []map[string]any {
		status, resp := postBearer(t, srv.URL, "/admin/agent-tools/list_tasks", "acat_w1", body)
		if status != http.StatusOK {
			t.Fatalf("list_tasks status=%d body=%v", status, resp)
		}
		raw, _ := resp["tasks"].([]any)
		out := make([]map[string]any, 0, len(raw))
		for _, x := range raw {
			out = append(out, x.(map[string]any))
		}
		return out
	}

	// All three tasks in the project.
	all := list(map[string]any{"agent_id": atAgent1, "project_id": string(pid)})
	if len(all) != 3 {
		t.Fatalf("list all: got %d want 3", len(all))
	}
	// Status filter → only the running one (t2).
	running := list(map[string]any{"agent_id": atAgent1, "project_id": string(pid), "status": []string{"running"}})
	if len(running) != 1 || running[0]["id"] != string(t2) {
		t.Fatalf("status=running: got %v", running)
	}
	// Assignee filter → the two assigned to atAgent1 (seed + t2).
	mine := list(map[string]any{"agent_id": atAgent1, "project_id": string(pid), "assignee": "agent:" + atAgent1})
	if len(mine) != 2 {
		t.Fatalf("assignee filter: got %d want 2", len(mine))
	}
	for _, m := range mine {
		if m["id"] != seedTID && m["id"] != string(t2) {
			t.Fatalf("assignee filter returned unexpected task %v", m["id"])
		}
	}

	// Org-isolation: a project the agent is NOT a member of → rejected (no listing).
	foreign := f.seedForeignProject(t)
	status, _ := postBearer(t, srv.URL, "/admin/agent-tools/list_tasks", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(foreign)})
	if status == http.StatusOK || status < 400 {
		t.Fatalf("foreign project must be rejected, got %d", status)
	}
}

// TestListTasks_Pagination verifies the SQL page window (page_size/offset),
// the pre-page total + has_more flags, and the hard page-size cap — the fix for
// the unbounded-board token overflow.
func TestListTasks_Pagination(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t) // 1 task ("seed")
	ctx := context.Background()
	owner := pm.IdentityRef("user:owner")
	// Seed up to 5 tasks total in the project.
	for i := 0; i < 4; i++ {
		_, _ = f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "t", CreatedBy: owner})
		f.drain(t)
	}
	srv := f.server(t)

	call := func(body map[string]any) map[string]any {
		status, resp := postBearer(t, srv.URL, "/admin/agent-tools/list_tasks", "acat_w1", body)
		if status != http.StatusOK {
			t.Fatalf("list_tasks status=%d body=%v", status, resp)
		}
		return resp
	}
	ids := func(resp map[string]any) []string {
		raw, _ := resp["tasks"].([]any)
		out := make([]string, 0, len(raw))
		for _, x := range raw {
			out = append(out, x.(map[string]any)["id"].(string))
		}
		return out
	}

	// First page of 2 of 5 → has_more, total=5, page_size echoed.
	p1 := call(map[string]any{"agent_id": atAgent1, "project_id": string(pid), "page_size": 2})
	if got := int(p1["total"].(float64)); got != 5 {
		t.Fatalf("total=%d want 5", got)
	}
	if int(p1["page_size"].(float64)) != 2 || p1["has_more"] != true {
		t.Fatalf("page1 meta unexpected: %v", p1)
	}
	if len(ids(p1)) != 2 {
		t.Fatalf("page1 size=%d want 2", len(ids(p1)))
	}

	// Page through with offset; collect all unique ids → must cover all 5 exactly.
	seen := map[string]bool{}
	for off := 0; off < 5; off += 2 {
		page := call(map[string]any{"agent_id": atAgent1, "project_id": string(pid), "page_size": 2, "offset": off})
		for _, id := range ids(page) {
			if seen[id] {
				t.Fatalf("duplicate id across pages: %s", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != 5 {
		t.Fatalf("paged total unique=%d want 5", len(seen))
	}

	// Last page → has_more=false.
	last := call(map[string]any{"agent_id": atAgent1, "project_id": string(pid), "page_size": 2, "offset": 4})
	if last["has_more"] != false || len(ids(last)) != 1 {
		t.Fatalf("last page meta unexpected: %v", last)
	}

	// Hard cap: an over-large page_size is clamped to the max in the echoed meta.
	capped := call(map[string]any{"agent_id": atAgent1, "project_id": string(pid), "page_size": 100000})
	if int(capped["page_size"].(float64)) != agentListMaxPageSize {
		t.Fatalf("page_size cap=%v want %d", capped["page_size"], agentListMaxPageSize)
	}
}

func TestListTasks_MissingProjectID_400(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/list_tasks", "acat_w1",
		map[string]any{"agent_id": atAgent1})
	if status != http.StatusBadRequest || body["error"] != "missing_project_id" {
		t.Fatalf("got status=%d err=%v want 400 missing_project_id", status, body["error"])
	}
}

// --- create_task -------------------------------------------------------------

func TestCreateTask_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid), "title": "agent task", "description": "d"})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	tid, _ := body["task_id"].(string)
	if tid == "" {
		t.Fatalf("no task_id in body: %v", body)
	}
	// The task exists with actor=agent as creator.
	tk, err := f.pmSvc.GetTask(context.Background(), pm.TaskID(tid))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(tk.CreatedBy()); got != "agent:"+atAgent1 {
		t.Fatalf("created_by = %q, want agent:%s", got, atAgent1)
	}
}

func TestCreateTask_ForeignProject_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	// AG1 must be bound/known; give it a member project so the agent resolves,
	// then target a DIFFERENT project it is not a member of.
	f.seedMemberProject(t)
	pid := f.seedForeignProject(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid), "title": "nope"})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (pm ErrNotMember); body = %v", status, body)
	}
}

func TestCreateTask_CrossWorker_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	srv := f.server(t)
	// W1 token operating AG2 (bound to W2) → guardrail 403.
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_task", "acat_w1",
		map[string]any{"agent_id": atAgent2, "project_id": string(pid), "title": "x"})
	if status != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("status = %d err=%v, want 403 agent_not_bound_to_worker", status, body["error"])
	}
}

// --- create_task one-step dispatch (T199/WS3) --------------------------------

// dispatch=true (no assignee) → the created task lands in the project's built-in
// pool and is immediately runnable (no separate add_task_to_plan).
func TestCreateTask_Dispatch_NoAssignee_Runnable(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid), "title": "dispatched", "dispatch": true})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	f.drain(t)
	tid, _ := body["task_id"].(string)
	if tid == "" {
		t.Fatalf("no task_id: %v", body)
	}
	// Runnable in one step (dispatched pool member).
	if err := f.pmSvc.EnsureTaskRunnable(context.Background(), pm.TaskID(tid)); err != nil {
		t.Fatalf("EnsureTaskRunnable = %v, want nil", err)
	}
}

// dispatch=true + assignee → the task is assigned AND dispatched; it surfaces in
// the assignee's list_my_tasks (run-real acceptance: "出现在 assignee 队列").
// v2.14.0 F7 (issue I14): get_my_work was replaced by list_my_tasks (the runnable
// assigned-task query); AgentWorkItem and its buckets were retired.
func TestCreateTask_Dispatch_WithAssignee_ShowsInListMyTasks(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid), "title": "assigned+dispatched",
			"assignee": "agent:" + atAgent1, "dispatch": true})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	f.drain(t)
	tid, _ := body["task_id"].(string)

	// list_my_tasks for the assignee must surface the dispatched+assigned task
	// (it is runnable now).
	st, work := postBearer(t, srv.URL, "/admin/agent-tools/list_my_tasks", "acat_w1",
		map[string]any{"agent_id": atAgent1})
	if st != http.StatusOK {
		t.Fatalf("list_my_tasks status = %d, body = %v", st, work)
	}
	if !listMyTasksHasTask(work, tid) {
		t.Fatalf("list_my_tasks missing dispatched+assigned task %s: %v", tid, work)
	}
	if err := f.pmSvc.EnsureTaskRunnable(context.Background(), pm.TaskID(tid)); err != nil {
		t.Fatalf("EnsureTaskRunnable = %v, want nil", err)
	}
}

// A malformed assignee ref is a clear 400 (not an opaque 500).
func TestCreateTask_InvalidAssignee_400(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid), "title": "bad", "assignee": "bob"})
	if status != http.StatusBadRequest || body["error"] != "invalid_assignee" {
		t.Fatalf("status = %d err = %v, want 400 invalid_assignee", status, body["error"])
	}
}

// listMyTasksHasTask reports whether the list_my_tasks response surfaces taskID in
// its "tasks" array (each entry carries the task identity as "task_id").
func listMyTasksHasTask(resp map[string]any, taskID string) bool {
	raw, _ := resp["tasks"].([]any)
	for _, x := range raw {
		if m, ok := x.(map[string]any); ok && m["task_id"] == taskID {
			return true
		}
	}
	return false
}

// --- assign_task / reassign_task ---------------------------------------------

func TestAssignTask_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	// A fresh, unassigned task in the agent's project.
	tid, err := f.pmSvc.CreateTask(context.Background(), pmservice.CreateTaskCommand{
		ProjectID: pid, Title: "to assign", CreatedBy: pm.IdentityRef("user:owner"),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/assign_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": string(tid), "assignee": "user:bob"})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d body=%v, want 200 ok=true", status, body)
	}
	tk, _ := f.pmSvc.GetTask(context.Background(), tid)
	if got := string(tk.Assignee()); got != "user:bob" {
		t.Fatalf("assignee = %q, want user:bob", got)
	}
}

func TestReassignTask_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	tid, _ := f.pmSvc.CreateTask(context.Background(), pmservice.CreateTaskCommand{
		ProjectID: pid, Title: "re", CreatedBy: pm.IdentityRef("user:owner"),
	})
	// First assignment so reassign re-targets an already-assigned task.
	if err := f.pmSvc.AssignTask(context.Background(), tid, pm.IdentityRef("user:bob"), pm.IdentityRef("user:owner")); err != nil {
		t.Fatal(err)
	}
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/reassign_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": string(tid), "assignee": "user:carol"})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d body=%v, want 200 ok=true", status, body)
	}
	tk, _ := f.pmSvc.GetTask(context.Background(), tid)
	if got := string(tk.Assignee()); got != "user:carol" {
		t.Fatalf("assignee = %q, want user:carol", got)
	}
}

// --- subscribe / unsubscribe -------------------------------------------------

func TestSubscribe_DefaultSelf_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, tid := f.seedMemberProject(t)
	srv := f.server(t)
	// identity omitted → defaults to the agent's own ref.
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/subscribe", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d body=%v, want 200 ok=true", status, body)
	}
}

func TestSubscribe_ExplicitIdentity_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, tid := f.seedMemberProject(t)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/subscribe", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid, "identity": "user:watcher"})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d body=%v, want 200 ok=true", status, body)
	}
	subs, err := f.pmSvc.ListTaskSubscribers(context.Background(), pm.TaskID(tid))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range subs {
		if string(s.IdentityID()) == "user:watcher" {
			found = true
		}
	}
	if !found {
		t.Fatalf("explicit subscriber not persisted; got %v", subs)
	}
}

func TestUnsubscribe_DefaultSelf_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, tid := f.seedMemberProject(t)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/unsubscribe", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d body=%v, want 200 ok=true", status, body)
	}
}

func TestUnsubscribe_ExplicitIdentity_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, tid := f.seedMemberProject(t)
	// Subscribe a watcher then remove it.
	if err := f.pmSvc.SubscribeTask(context.Background(), pm.TaskID(tid),
		pm.IdentityRef("user:watcher"), pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatal(err)
	}
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/unsubscribe", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid, "identity": "user:watcher"})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d body=%v, want 200 ok=true", status, body)
	}
	subs, _ := f.pmSvc.ListTaskSubscribers(context.Background(), pm.TaskID(tid))
	for _, s := range subs {
		if string(s.IdentityID()) == "user:watcher" {
			t.Fatalf("watcher still subscribed after unsubscribe: %v", subs)
		}
	}
}

// ADR-0046: verify_task is DELETED (verification capability removed). The former
// TestVerifyTask_SelfVerify_Rejected / TestVerifyTask_NonCompleter_OK were removed.

// --- get_task ----------------------------------------------------------------

func TestGetTask_OwnTask_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, tid := f.seedMemberProject(t) // AG1 holds a WorkItem for this task.
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	if body["id"] != tid {
		t.Fatalf("id = %v, want %s", body["id"], tid)
	}
	// Spot-check the projection shape.
	for _, k := range []string{"project_id", "title", "status", "assignee", "version", "created_at", "updated_at"} {
		if _, ok := body[k]; !ok {
			t.Fatalf("task projection missing %q: %v", k, body)
		}
	}
}

func TestGetTask_GET_Query_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, tid := f.seedMemberProject(t)
	srv := f.server(t)

	status, body := getBearer(t, srv.URL, "/admin/agent-tools/get_task?agent_id="+atAgent1+"&task_id="+tid, "acat_w1")
	if status != http.StatusOK || body["id"] != tid {
		t.Fatalf("GET get_task status = %d body=%v, want 200 id=%s", status, body, tid)
	}
}

func TestGetTask_NotOwn_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.seedMemberProject(t) // AG1 has a member project (so the agent resolves).
	// A task AG1 has NO WorkItem for (not assigned to it).
	pid := f.seedForeignProject(t)
	tid, _ := f.pmSvc.CreateTask(context.Background(), pmservice.CreateTaskCommand{
		ProjectID: pid, Title: "not mine", CreatedBy: pm.IdentityRef("user:other"),
	})
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": string(tid)})
	if status != http.StatusForbidden || body["error"] != "not_agents_task" {
		t.Fatalf("status = %d err=%v, want 403 not_agents_task", status, body["error"])
	}
}

// --- get_issue ---------------------------------------------------------------

// TestGetIssue_OwnDerivedTask_OK: an agent that is a project member may read an
// issue in its project. (v2.10.3 T170: scope relaxed from own-link to
// project-member; a member holding a WorkItem for a derived task still reads OK.)
func TestGetIssue_OwnDerivedTask_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t) // AG1 is a member (+ a non-derived WorkItem).
	ctx := context.Background()
	owner := pm.IdentityRef("user:owner")
	iid, err := f.pmSvc.CreateIssue(ctx, pmservice.CreateIssueCommand{
		ProjectID: pid, Title: "an issue", Description: "d", CreatedBy: owner,
	})
	if err != nil {
		t.Fatal(err)
	}
	// A Task DERIVED from the issue, assigned to AG1 → AG1 holds a WorkItem for a
	// task whose DerivedFromIssue == iid, which is the only thing that grants read.
	tid, err := f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{
		ProjectID: pid, Title: "derived", DerivedFromIssue: iid, CreatedBy: owner,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	if err := f.pmSvc.AssignTask(ctx, tid, pm.IdentityRef("agent:"+atAgent1), owner); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_issue", "acat_w1",
		map[string]any{"agent_id": atAgent1, "issue_id": string(iid)})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	if body["id"] != string(iid) {
		t.Fatalf("id = %v, want %s", body["id"], iid)
	}
	for _, k := range []string{"project_id", "title", "description", "status", "created_by", "version"} {
		if _, ok := body[k]; !ok {
			t.Fatalf("issue projection missing %q: %v", k, body)
		}
	}
}

// TestGetIssue_MemberNoDerivedTask_OK: v2.10.3 T170 relaxation — a project
// member may now read ANY issue in its project, even one it has NO derived task
// for. (Pre-T170 this was a 403 not_in_issue_domain; the own-link scope was too
// tight for the "open an issue to discuss" flow. Owner-approved.)
func TestGetIssue_MemberNoDerivedTask_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t) // AG1 member + only a NON-derived WorkItem.
	iid, err := f.pmSvc.CreateIssue(context.Background(), pmservice.CreateIssueCommand{
		ProjectID: pid, Title: "unrelated", CreatedBy: pm.IdentityRef("user:owner"),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_issue", "acat_w1",
		map[string]any{"agent_id": atAgent1, "issue_id": string(iid)})
	if status != http.StatusOK || body["id"] != string(iid) {
		t.Fatalf("status = %d body=%v, want 200 id=%s (member may read any project issue)", status, body, iid)
	}
}

// TestGetIssue_NonMember_403: the relaxation does NOT cross the membership
// boundary — an agent that is NOT a member of the issue's project still gets a
// 403 (ErrNotMember → terminal). Org-isolation / β write-gate held as a read-gate.
func TestGetIssue_NonMember_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.seedMemberProject(t) // AG1 resolves (member of SOME project)…
	// …but the issue lives in a project AG1 is NOT a member of.
	foreign := f.seedForeignProject(t)
	iid, err := f.pmSvc.CreateIssue(context.Background(), pmservice.CreateIssueCommand{
		ProjectID: foreign, Title: "secret", CreatedBy: pm.IdentityRef("user:other"),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := f.server(t)

	status, _ := postBearer(t, srv.URL, "/admin/agent-tools/get_issue", "acat_w1",
		map[string]any{"agent_id": atAgent1, "issue_id": string(iid)})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (non-member may not read a foreign project's issue)", status)
	}
}

func TestGetIssue_CrossWorker_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	iid, _ := f.pmSvc.CreateIssue(context.Background(), pmservice.CreateIssueCommand{
		ProjectID: pid, Title: "x", CreatedBy: pm.IdentityRef("user:owner"),
	})
	srv := f.server(t)
	// W1 token operating AG2 (bound to W2) → guardrail 403 before any read scope.
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_issue", "acat_w1",
		map[string]any{"agent_id": atAgent2, "issue_id": string(iid)})
	if status != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("status = %d err=%v, want 403 agent_not_bound_to_worker", status, body["error"])
	}
}
