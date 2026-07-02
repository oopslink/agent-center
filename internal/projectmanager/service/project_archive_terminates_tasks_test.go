package service

import (
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TestArchiveProject_TerminatesInFlightTasks pins the fix for the "orphan running
// task after archive" bug (the Environment showed a superseded task T4 still as
// in-flight because its project was archived while the task stayed non-terminal).
// Archiving a project must conclude its non-terminal tasks to discarded — otherwise
// they linger as live work that can neither be run (child writes frozen) nor be
// discarded (discard_task rejects an archived project).
func TestArchiveProject_TerminatesInFlightTasks(t *testing.T) {
	svc, _, _, tasks, _, ctx := planSetup(t)
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	// An open (non-terminal) task in the project.
	tid, err := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "in-flight", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.ArchiveProject(ctx, pid, "user:a"); err != nil {
		t.Fatalf("ArchiveProject: %v", err)
	}

	tk, err := tasks.FindByID(ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Status() != pm.TaskDiscarded {
		t.Fatalf("archived project's in-flight task status=%s, want discarded", tk.Status())
	}
}
