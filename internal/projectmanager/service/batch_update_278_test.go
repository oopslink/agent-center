package service

import (
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func strptr(s string) *string { return &s }

// TestBatchUpdateTask_Partial: a {tags}-only patch leaves status + assignee
// untouched (v2.8.1 edit-task #278).
func TestBatchUpdateTask_Partial(t *testing.T) {
	svc, _, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})
	if err := svc.AssignTask(ctx, tid, "user:a", "user:a"); err != nil {
		t.Fatal(err)
	}

	tags := []string{"x", "y"}
	if err := svc.BatchUpdateTask(ctx, tid, BatchTaskPatch{Tags: &tags}, "user:a"); err != nil {
		t.Fatalf("BatchUpdateTask: %v", err)
	}
	got, err := svc.GetTask(ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tags()) != 2 {
		t.Fatalf("tags not applied: %v", got.Tags())
	}
	if got.Status() != pm.TaskOpen {
		t.Fatalf("status changed by tags-only patch: %v", got.Status())
	}
	if got.Assignee() != "user:a" {
		t.Fatalf("assignee changed by tags-only patch: %v", got.Assignee())
	}
}

// TestBatchUpdateTask_Atomic: an invalid status in a {status,tags} patch rolls
// back the WHOLE patch — tags must NOT be applied.
func TestBatchUpdateTask_Atomic(t *testing.T) {
	svc, _, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})

	tags := []string{"should-not-stick"}
	err := svc.BatchUpdateTask(ctx, tid, BatchTaskPatch{Status: strptr("bogus"), Tags: &tags}, "user:a")
	if err == nil {
		t.Fatalf("expected error for invalid status")
	}
	got, err := svc.GetTask(ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status() != pm.TaskOpen {
		t.Fatalf("status mutated despite rollback: %v", got.Status())
	}
	if got.Tags() != nil {
		t.Fatalf("tags applied despite rollback (not atomic): %v", got.Tags())
	}
	if got.Version() != 1 {
		t.Fatalf("version bumped despite rollback: %d", got.Version())
	}
}

// TestBatchUpdateTask_AssigneeUnassign: assignee:"" clears the assignee.
func TestBatchUpdateTask_AssigneeUnassign(t *testing.T) {
	svc, _, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})
	if err := svc.AssignTask(ctx, tid, "user:a", "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.BatchUpdateTask(ctx, tid, BatchTaskPatch{Assignee: strptr("")}, "user:a"); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.GetTask(ctx, tid)
	if got.Assignee() != "" {
		t.Fatalf("assignee not cleared: %v", got.Assignee())
	}
}

// TestBatchUpdateIssue_Partial: a {tags}-only patch leaves status untouched
// (issues have no assignee). Mirrors the Task partial-patch contract for #232's
// Issue analogue (v2.8.1 edit-consolidation).
func TestBatchUpdateIssue_Partial(t *testing.T) {
	svc, _, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	iid, _ := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: pid, Title: "bug", CreatedBy: "user:a"})
	tags := []string{"x", "y"}
	if err := svc.BatchUpdateIssue(ctx, iid, BatchIssuePatch{Tags: &tags}, "user:a"); err != nil {
		t.Fatalf("BatchUpdateIssue: %v", err)
	}
	got, err := svc.GetIssue(ctx, iid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tags()) != 2 || got.Tags()[0] != "x" {
		t.Fatalf("tags = %v, want [x y]", got.Tags())
	}
	if got.Status() != pm.IssueOpen {
		t.Fatalf("status moved on a tags-only patch: %v != open", got.Status())
	}
}

// TestBatchUpdateIssue_Atomic: an invalid status in a {status,tags} patch rolls
// back the whole tx — tags must NOT be applied (all-or-none).
func TestBatchUpdateIssue_Atomic(t *testing.T) {
	svc, _, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	iid, _ := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: pid, Title: "bug", CreatedBy: "user:a"})
	tags := []string{"z"}
	if err := svc.BatchUpdateIssue(ctx, iid, BatchIssuePatch{Status: strptr("bogus"), Tags: &tags}, "user:a"); err == nil {
		t.Fatal("expected error for invalid status")
	}
	got, err := svc.GetIssue(ctx, iid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tags()) != 0 {
		t.Fatalf("atomic rollback failed: tags applied despite bogus status: %v", got.Tags())
	}
}

// TestBatchUpdateIssue_MultiField: title+status+tags applied together in one tx.
func TestBatchUpdateIssue_MultiField(t *testing.T) {
	svc, _, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	iid, _ := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: pid, Title: "bug", CreatedBy: "user:a"})
	tags := []string{"p"}
	if err := svc.BatchUpdateIssue(ctx, iid, BatchIssuePatch{
		Title: strptr("renamed"), Status: strptr(string(pm.IssueInProgress)), Tags: &tags,
	}, "user:a"); err != nil {
		t.Fatalf("BatchUpdateIssue multi: %v", err)
	}
	got, _ := svc.GetIssue(ctx, iid)
	if got.Title() != "renamed" || got.Status() != pm.IssueInProgress || len(got.Tags()) != 1 {
		t.Fatalf("multi-field not all applied: title=%q status=%v tags=%v", got.Title(), got.Status(), got.Tags())
	}
}
