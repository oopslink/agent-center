package api

import (
	"context"
	"net/http"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// =============================================================================
// T192 — set_task_issue agent tool: (re)set or clear a task's derived_from_issue
// after creation. Authorized by the relaxed requireTaskAccess gate (creator /
// project member / own-work), like discard_task. Validates exist + same-project.
// =============================================================================

// seedMemberTaskWithIssue creates a project, makes AG1 a member, an issue, and a
// task (unlinked). Returns (taskID, issueID).
func (f *writeToolsFixture) seedMemberTaskWithIssue(t *testing.T) (string, string) {
	t.Helper()
	ctx := context.Background()
	owner := pm.IdentityRef("user:owner")
	pid, err := f.pmSvc.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: atTestOrg, Name: "P", CreatedBy: owner})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pmSvc.AddProjectMember(ctx, pmservice.AddProjectMemberCommand{
		ProjectID: pid, IdentityID: pm.IdentityRef("agent:" + atAgent1), Actor: owner,
	}); err != nil {
		t.Fatal(err)
	}
	iid, err := f.pmSvc.CreateIssue(ctx, pmservice.CreateIssueCommand{ProjectID: pid, Title: "bug", CreatedBy: owner})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "fix", CreatedBy: owner})
	if err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	return string(tid), string(iid)
}

func (f *writeToolsFixture) derivedFromIssue(t *testing.T, taskID string) string {
	t.Helper()
	tk, err := f.pmSvc.GetTask(context.Background(), pm.TaskID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	return string(tk.DerivedFromIssue())
}

func TestSetTaskIssue_SetAndClear_OK_T192(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid, iid := f.seedMemberTaskWithIssue(t)
	srv := f.server(t)

	// Set.
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/set_task_issue", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid, "issue_id": iid})
	if status != http.StatusOK {
		t.Fatalf("set status = %d, want 200; body = %v", status, body)
	}
	if body["derived_from_issue"] != iid {
		t.Fatalf("body derived_from_issue = %v, want %s", body["derived_from_issue"], iid)
	}
	if got := f.derivedFromIssue(t, tid); got != iid {
		t.Fatalf("persisted derived_from_issue = %q, want %q", got, iid)
	}

	// Clear (issue_id="").
	status, body = postBearer(t, srv.URL, "/admin/agent-tools/set_task_issue", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid, "issue_id": ""})
	if status != http.StatusOK {
		t.Fatalf("clear status = %d, want 200; body = %v", status, body)
	}
	if got := f.derivedFromIssue(t, tid); got != "" {
		t.Fatalf("link not cleared, got %q", got)
	}
}

func TestSetTaskIssue_CrossProject_409_T192(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid, _ := f.seedMemberTaskWithIssue(t)
	// An issue in a DIFFERENT project.
	ctx := context.Background()
	owner := pm.IdentityRef("user:owner")
	p2, _ := f.pmSvc.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: atTestOrg, Name: "P2", CreatedBy: owner})
	other, _ := f.pmSvc.CreateIssue(ctx, pmservice.CreateIssueCommand{ProjectID: p2, Title: "x", CreatedBy: owner})
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/set_task_issue", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid, "issue_id": string(other)})
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %v", status, body)
	}
	if body["error"] != "derived_issue_project_mismatch" {
		t.Fatalf("error = %v, want derived_issue_project_mismatch", body["error"])
	}
	if got := f.derivedFromIssue(t, tid); got != "" {
		t.Fatalf("rejected link must not persist, got %q", got)
	}
}

func TestSetTaskIssue_MissingIssue_404_T192(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid, _ := f.seedMemberTaskWithIssue(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/set_task_issue", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid, "issue_id": "issue-nope"})
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %v", status, body)
	}
	if body["error"] != "not_found" {
		t.Fatalf("error = %v, want not_found", body["error"])
	}
}

// A non-member with no WorkItem is fail-closed (mirrors discard_task's 403).
func TestSetTaskIssue_NonMember_403_T192(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	ctx := context.Background()
	owner := pm.IdentityRef("user:owner")
	pid, _ := f.pmSvc.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: atTestOrg, Name: "P", CreatedBy: owner})
	iid, _ := f.pmSvc.CreateIssue(ctx, pmservice.CreateIssueCommand{ProjectID: pid, Title: "bug", CreatedBy: owner})
	tid, _ := f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "t", CreatedBy: owner})
	f.drain(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/set_task_issue", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": string(tid), "issue_id": string(iid)})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %v", status, body)
	}
	if body["error"] != "not_agents_task" {
		t.Fatalf("error = %v, want not_agents_task", body["error"])
	}
}
