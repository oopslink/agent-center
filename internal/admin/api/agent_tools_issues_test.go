package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// =============================================================================
// v2.10.3 T170 — agent issue-management MCP tools (create_issue, update_issue,
// close_issue / reopen_issue, post_issue_message, list_issues,
// list_tasks_of_issue). Same writeToolsFixture as the task tools: a real admin
// server + AuthMiddleware over the full pm → outbox → projector pipeline. The
// WRITE tools go through the pm AppService whose requireProjectMember is the
// gate (the agent is a member of its assigned task's project via #5a).
// =============================================================================

// issueMessages reads the issue Conversation's messages (owner_ref pm://issues/{id}).
func (f *writeToolsFixture) issueMessages(t *testing.T, issueID string) []*conversation.Message {
	t.Helper()
	ctx := context.Background()
	conv, err := f.convRepo.FindByOwnerRef(ctx, conversation.NewIssueOwnerRef(issueID))
	if err != nil {
		t.Fatalf("issue conv not found: %v", err)
	}
	msgs, err := f.msgRepo.FindByConversationID(ctx, conv.ID(), conversation.MessageFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	return msgs
}

func (f *writeToolsFixture) getIssue(t *testing.T, issueID string) *pm.Issue {
	t.Helper()
	i, err := f.pmSvc.GetIssue(context.Background(), pm.IssueID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	return i
}

// --- create_issue ------------------------------------------------------------

func TestCreateIssue_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_issue", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid),
			"title": "agent issue", "description": "d", "tags": []string{"bug", "p1"}})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	iid, _ := body["issue_id"].(string)
	if iid == "" {
		t.Fatalf("no issue_id in body: %v", body)
	}
	i := f.getIssue(t, iid)
	if got := string(i.CreatedBy()); got != "agent:"+atAgent1 {
		t.Fatalf("created_by = %q, want agent:%s", got, atAgent1)
	}
	if got := i.Tags(); len(got) != 2 {
		t.Fatalf("tags = %v, want [bug p1]", got)
	}
	if i.Status() != pm.IssueOpen {
		t.Fatalf("status = %q, want open", i.Status())
	}
}

func TestCreateIssue_ForeignProject_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.seedMemberProject(t) // AG1 resolves (member of some project)…
	pid := f.seedForeignProject(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_issue", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid), "title": "nope"})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (ErrNotMember); body = %v", status, body)
	}
}

func TestCreateIssue_MissingTitle_400(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_issue", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid)})
	if status != http.StatusBadRequest || body["error"] != "missing_title" {
		t.Fatalf("status = %d err=%v, want 400 missing_title", status, body["error"])
	}
}

// --- update_issue ------------------------------------------------------------

func TestUpdateIssue_PatchFields_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	iid, err := f.pmSvc.CreateIssue(context.Background(), pmservice.CreateIssueCommand{
		ProjectID: pid, Title: "old", Description: "d0", CreatedBy: pm.IdentityRef("user:owner"),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/update_issue", "acat_w1",
		map[string]any{"agent_id": atAgent1, "issue_id": string(iid),
			"title": "new", "description": "d1", "status": "in_progress", "tags": []string{"x"}})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d body=%v, want 200 ok=true", status, body)
	}
	i := f.getIssue(t, string(iid))
	if i.Title() != "new" || i.Description() != "d1" || i.Status() != pm.IssueInProgress {
		t.Fatalf("issue not patched: title=%q desc=%q status=%q", i.Title(), i.Description(), i.Status())
	}
	if got := i.Tags(); len(got) != 1 || got[0] != "x" {
		t.Fatalf("tags = %v, want [x]", got)
	}
}

func TestUpdateIssue_PartialPatch_LeavesOthers(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	iid, _ := f.pmSvc.CreateIssue(context.Background(), pmservice.CreateIssueCommand{
		ProjectID: pid, Title: "keep", Description: "keepdesc", CreatedBy: pm.IdentityRef("user:owner"),
	})
	srv := f.server(t)

	// Only status set → title/description untouched.
	status, _ := postBearer(t, srv.URL, "/admin/agent-tools/update_issue", "acat_w1",
		map[string]any{"agent_id": atAgent1, "issue_id": string(iid), "status": "resolved"})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	i := f.getIssue(t, string(iid))
	if i.Title() != "keep" || i.Description() != "keepdesc" {
		t.Fatalf("partial patch clobbered fields: title=%q desc=%q", i.Title(), i.Description())
	}
	if i.Status() != pm.IssueResolved {
		t.Fatalf("status = %q, want resolved", i.Status())
	}
}

func TestUpdateIssue_EmptyPatch_400(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	iid, _ := f.pmSvc.CreateIssue(context.Background(), pmservice.CreateIssueCommand{
		ProjectID: pid, Title: "i", CreatedBy: pm.IdentityRef("user:owner"),
	})
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/update_issue", "acat_w1",
		map[string]any{"agent_id": atAgent1, "issue_id": string(iid)})
	if status != http.StatusBadRequest || body["error"] != "empty_patch" {
		t.Fatalf("status = %d err=%v, want 400 empty_patch", status, body["error"])
	}
}

func TestUpdateIssue_InvalidStatus_422(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	iid, _ := f.pmSvc.CreateIssue(context.Background(), pmservice.CreateIssueCommand{
		ProjectID: pid, Title: "i", CreatedBy: pm.IdentityRef("user:owner"),
	})
	srv := f.server(t)

	status, _ := postBearer(t, srv.URL, "/admin/agent-tools/update_issue", "acat_w1",
		map[string]any{"agent_id": atAgent1, "issue_id": string(iid), "status": "bogus"})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (invalid status enum)", status)
	}
}

// --- close_issue / reopen_issue ----------------------------------------------

func TestCloseAndReopenIssue_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	iid, _ := f.pmSvc.CreateIssue(context.Background(), pmservice.CreateIssueCommand{
		ProjectID: pid, Title: "i", CreatedBy: pm.IdentityRef("user:owner"),
	})
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/close_issue", "acat_w1",
		map[string]any{"agent_id": atAgent1, "issue_id": string(iid)})
	if status != http.StatusOK || body["status"] != "closed" {
		t.Fatalf("close: status = %d body=%v, want 200 status=closed", status, body)
	}
	if got := f.getIssue(t, string(iid)).Status(); got != pm.IssueClosed {
		t.Fatalf("issue status = %q, want closed", got)
	}

	status, body = postBearer(t, srv.URL, "/admin/agent-tools/reopen_issue", "acat_w1",
		map[string]any{"agent_id": atAgent1, "issue_id": string(iid)})
	if status != http.StatusOK || body["status"] != "open" {
		t.Fatalf("reopen: status = %d body=%v, want 200 status=open", status, body)
	}
	if got := f.getIssue(t, string(iid)).Status(); got != pm.IssueOpen {
		t.Fatalf("issue status = %q, want open", got)
	}
}

// --- post_issue_message ------------------------------------------------------

func TestPostIssueMessage_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	iid, err := f.pmSvc.CreateIssue(context.Background(), pmservice.CreateIssueCommand{
		ProjectID: pid, Title: "discuss", CreatedBy: pm.IdentityRef("user:owner"),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.drain(t) // let the issue.created projector create the issue Conversation
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/post_issue_message", "acat_w1",
		map[string]any{"agent_id": atAgent1, "issue_id": string(iid), "content": "my comment @oopslink"})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	if body["message_id"] == "" || body["message_id"] == nil {
		t.Fatalf("no message_id in body: %v", body)
	}
	msgs := f.issueMessages(t, string(iid))
	found := false
	for _, m := range msgs {
		if m.Content() == "my comment @oopslink" && string(m.SenderIdentityID()) == "agent:"+atAgent1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("agent comment not in issue conversation; got %d msgs", len(msgs))
	}
}

func TestPostIssueMessage_NonMember_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.seedMemberProject(t)
	foreign := f.seedForeignProject(t)
	iid, _ := f.pmSvc.CreateIssue(context.Background(), pmservice.CreateIssueCommand{
		ProjectID: foreign, Title: "x", CreatedBy: pm.IdentityRef("user:other"),
	})
	f.drain(t)
	srv := f.server(t)

	status, _ := postBearer(t, srv.URL, "/admin/agent-tools/post_issue_message", "acat_w1",
		map[string]any{"agent_id": atAgent1, "issue_id": string(iid), "content": "intrude"})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (non-member may not comment)", status)
	}
}

func TestPostIssueMessage_MissingContent_400(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	iid, _ := f.pmSvc.CreateIssue(context.Background(), pmservice.CreateIssueCommand{
		ProjectID: pid, Title: "i", CreatedBy: pm.IdentityRef("user:owner"),
	})
	f.drain(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/post_issue_message", "acat_w1",
		map[string]any{"agent_id": atAgent1, "issue_id": string(iid), "content": ""})
	if status != http.StatusBadRequest || body["error"] != "missing_content" {
		t.Fatalf("status = %d err=%v, want 400 missing_content", status, body["error"])
	}
}

// --- list_issues -------------------------------------------------------------

func TestListIssues_FiltersAndIsolation(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	ctx := context.Background()
	// user:other must be a member to author an issue (only members may create).
	if _, err := f.pmSvc.AddProjectMember(ctx, pmservice.AddProjectMemberCommand{
		ProjectID: pid, IdentityID: pm.IdentityRef("user:other"), Actor: pm.IdentityRef("user:owner"),
	}); err != nil {
		t.Fatal(err)
	}
	// Two issues with different authors; move one to in_progress.
	i1, _ := f.pmSvc.CreateIssue(ctx, pmservice.CreateIssueCommand{ProjectID: pid, Title: "a", CreatedBy: pm.IdentityRef("user:owner")})
	i2, _ := f.pmSvc.CreateIssue(ctx, pmservice.CreateIssueCommand{ProjectID: pid, Title: "b", CreatedBy: pm.IdentityRef("user:other")})
	if err := f.pmSvc.SetIssueStatus(ctx, i2, pm.IssueInProgress, pm.IdentityRef("user:owner")); err != nil {
		t.Fatal(err)
	}
	_ = i1
	srv := f.server(t)

	// No filter → both.
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/list_issues", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid)})
	if status != http.StatusOK {
		t.Fatalf("status = %d body=%v, want 200", status, body)
	}
	if n, _ := body["total"].(float64); int(n) != 2 {
		t.Fatalf("total = %v, want 2", body["total"])
	}

	// status filter → only in_progress (i2).
	_, body = postBearer(t, srv.URL, "/admin/agent-tools/list_issues", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid), "status": []string{"in_progress"}})
	if n, _ := body["total"].(float64); int(n) != 1 {
		t.Fatalf("status-filtered total = %v, want 1", body["total"])
	}

	// author filter → only user:owner (i1).
	_, body = postBearer(t, srv.URL, "/admin/agent-tools/list_issues", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid), "author": "user:owner"})
	if n, _ := body["total"].(float64); int(n) != 1 {
		t.Fatalf("author-filtered total = %v, want 1", body["total"])
	}
}

func TestListIssues_ForeignProject_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.seedMemberProject(t)
	foreign := f.seedForeignProject(t)
	srv := f.server(t)

	status, _ := postBearer(t, srv.URL, "/admin/agent-tools/list_issues", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(foreign)})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (non-member)", status)
	}
}

// --- list_tasks_of_issue -----------------------------------------------------

func TestListTasksOfIssue_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	ctx := context.Background()
	owner := pm.IdentityRef("user:owner")
	iid, _ := f.pmSvc.CreateIssue(ctx, pmservice.CreateIssueCommand{ProjectID: pid, Title: "epic", CreatedBy: owner})
	// Two derived tasks + one unrelated (not derived).
	d1, _ := f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "d1", DerivedFromIssue: iid, CreatedBy: owner})
	d2, _ := f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "d2", DerivedFromIssue: iid, CreatedBy: owner})
	_, _ = f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "unrelated", CreatedBy: owner})
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/list_tasks_of_issue", "acat_w1",
		map[string]any{"agent_id": atAgent1, "issue_id": string(iid)})
	if status != http.StatusOK {
		t.Fatalf("status = %d body=%v, want 200", status, body)
	}
	if n, _ := body["total"].(float64); int(n) != 2 {
		t.Fatalf("total = %v, want 2 derived tasks", body["total"])
	}
	tasks, _ := body["tasks"].([]any)
	ids := map[string]bool{}
	for _, raw := range tasks {
		m, _ := raw.(map[string]any)
		ids[m["id"].(string)] = true
		if m["derived_from_issue"] != string(iid) {
			t.Fatalf("task %v not derived from issue", m["id"])
		}
	}
	if !ids[string(d1)] || !ids[string(d2)] {
		t.Fatalf("derived tasks missing from result: %v", ids)
	}
}

func TestListTasksOfIssue_NonMember_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.seedMemberProject(t)
	foreign := f.seedForeignProject(t)
	iid, _ := f.pmSvc.CreateIssue(context.Background(), pmservice.CreateIssueCommand{
		ProjectID: foreign, Title: "x", CreatedBy: pm.IdentityRef("user:other"),
	})
	srv := f.server(t)

	status, _ := postBearer(t, srv.URL, "/admin/agent-tools/list_tasks_of_issue", "acat_w1",
		map[string]any{"agent_id": atAgent1, "issue_id": string(iid)})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (non-member)", status)
	}
}
