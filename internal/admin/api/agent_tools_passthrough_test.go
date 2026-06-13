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

// TestGetIssue_OwnDerivedTask_OK: an agent may read an issue ONLY via own-work
// association — it holds a WorkItem for a Task derived from the issue.
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

// TestGetIssue_MemberButNoDerivedTask_403: membership (#5a, the WRITE gate) does
// NOT grant reading arbitrary project issues — only own-associated reads (OQ4).
// AG1 is a member of the project and the issue is in it, but AG1 has no task
// derived from this issue → 403.
func TestGetIssue_MemberButNoDerivedTask_403(t *testing.T) {
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
	if status != http.StatusForbidden || body["error"] != "not_in_issue_domain" {
		t.Fatalf("status = %d err=%v, want 403 not_in_issue_domain (membership != read)", status, body["error"])
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
