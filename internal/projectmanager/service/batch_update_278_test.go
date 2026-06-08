package service

import (
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func strptr(s string) *string { return &s }

// TestBatchUpdateTask_Partial: a {tags}-only patch leaves status + assignee
// untouched (v2.8.1 edit-task #278).
func TestBatchUpdateTask_Partial(t *testing.T) {
	svc, _, _, ctx := flowSetup(t)
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
	svc, _, _, ctx := flowSetup(t)
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
	svc, _, _, ctx := flowSetup(t)
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
