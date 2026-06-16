package service

import (
	"errors"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// =============================================================================
// T192 — derived_from_issue editable after creation. UpdateTask / BatchUpdateTask
// can (re)set or CLEAR a task's derived_from_issue, validated to EXIST + be in the
// SAME project. nil pointer leaves the link untouched (create-time behavior intact).
// =============================================================================

// strptr lives in batch_update_278_test.go (same package).

func issptr(s pm.IssueID) *pm.IssueID { return &s }

// set on a task created without a link; then clear it.
func TestUpdateTask_SetAndClearDerivedFromIssue_T192(t *testing.T) {
	svc, _, _, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	iid, _ := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: pid, Title: "bug", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "fix it", CreatedBy: "user:a"})

	// Initially unlinked.
	if tk, _ := svc.tasks.FindByID(ctx, tid); tk.DerivedFromIssue() != "" {
		t.Fatalf("fresh task should be unlinked, got %q", tk.DerivedFromIssue())
	}
	// Link it after creation.
	if err := svc.UpdateTask(ctx, UpdateTaskCommand{TaskID: tid, DerivedFromIssue: issptr(iid), Actor: "user:a"}); err != nil {
		t.Fatalf("link: %v", err)
	}
	if tk, _ := svc.tasks.FindByID(ctx, tid); tk.DerivedFromIssue() != iid {
		t.Fatalf("derived_from_issue = %q, want %q", tk.DerivedFromIssue(), iid)
	}
	// Clear it ("" is a non-nil clear).
	if err := svc.UpdateTask(ctx, UpdateTaskCommand{TaskID: tid, DerivedFromIssue: issptr(""), Actor: "user:a"}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if tk, _ := svc.tasks.FindByID(ctx, tid); tk.DerivedFromIssue() != "" {
		t.Fatalf("derived_from_issue not cleared, got %q", tk.DerivedFromIssue())
	}
}

// a non-nil-but-unchanged path: updating only the title must NOT touch the link.
func TestUpdateTask_NilDerivedFromIssue_LeavesLink_T192(t *testing.T) {
	svc, _, _, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	iid, _ := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: pid, Title: "bug", CreatedBy: "user:a"})
	// Created WITH a link (create-time behavior).
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "t", DerivedFromIssue: iid, CreatedBy: "user:a"})

	if err := svc.UpdateTask(ctx, UpdateTaskCommand{TaskID: tid, Title: strptr("renamed"), Actor: "user:a"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	tk, _ := svc.tasks.FindByID(ctx, tid)
	if tk.Title() != "renamed" {
		t.Fatalf("title = %q, want renamed", tk.Title())
	}
	if tk.DerivedFromIssue() != iid {
		t.Fatalf("nil DerivedFromIssue must leave the link intact; got %q want %q", tk.DerivedFromIssue(), iid)
	}
}

// linking to an issue in ANOTHER project is rejected.
func TestUpdateTask_CrossProjectIssue_Rejected_T192(t *testing.T) {
	svc, _, _, ctx := flowSetup(t)
	p1, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P1", CreatedBy: "user:a"})
	p2, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P2", CreatedBy: "user:a"})
	otherIssue, _ := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: p2, Title: "elsewhere", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: p1, Title: "t", CreatedBy: "user:a"})

	err := svc.UpdateTask(ctx, UpdateTaskCommand{TaskID: tid, DerivedFromIssue: issptr(otherIssue), Actor: "user:a"})
	if !errors.Is(err, pm.ErrDerivedIssueProjectMismatch) {
		t.Fatalf("cross-project link = %v, want ErrDerivedIssueProjectMismatch", err)
	}
	// Unchanged.
	if tk, _ := svc.tasks.FindByID(ctx, tid); tk.DerivedFromIssue() != "" {
		t.Fatalf("rejected link must not persist, got %q", tk.DerivedFromIssue())
	}
}

// linking to a non-existent issue surfaces ErrIssueNotFound.
func TestUpdateTask_MissingIssue_Rejected_T192(t *testing.T) {
	svc, _, _, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "t", CreatedBy: "user:a"})

	err := svc.UpdateTask(ctx, UpdateTaskCommand{TaskID: tid, DerivedFromIssue: issptr("issue-does-not-exist"), Actor: "user:a"})
	if !errors.Is(err, pm.ErrIssueNotFound) {
		t.Fatalf("missing-issue link = %v, want ErrIssueNotFound", err)
	}
}

// the BatchUpdateTask path applies derived_from_issue too (same validation).
func TestBatchUpdateTask_DerivedFromIssue_T192(t *testing.T) {
	svc, _, _, ctx := flowSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	iid, _ := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: pid, Title: "bug", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "t", CreatedBy: "user:a"})

	if err := svc.BatchUpdateTask(ctx, tid, BatchTaskPatch{DerivedFromIssue: issptr(iid)}, "user:a"); err != nil {
		t.Fatalf("batch link: %v", err)
	}
	if tk, _ := svc.tasks.FindByID(ctx, tid); tk.DerivedFromIssue() != iid {
		t.Fatalf("batch derived_from_issue = %q, want %q", tk.DerivedFromIssue(), iid)
	}
	// Cross-project still rejected via the batch path.
	p2, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P2", CreatedBy: "user:a"})
	other, _ := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: p2, Title: "x", CreatedBy: "user:a"})
	if err := svc.BatchUpdateTask(ctx, tid, BatchTaskPatch{DerivedFromIssue: issptr(other)}, "user:a"); !errors.Is(err, pm.ErrDerivedIssueProjectMismatch) {
		t.Fatalf("batch cross-project = %v, want ErrDerivedIssueProjectMismatch", err)
	}
}
