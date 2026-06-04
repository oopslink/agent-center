package api

import (
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// v2.7.1 #245: the task/issue DTO carries org_ref ("T<n>"/"I<n>") when a number
// is allocated, and OMITS it when 0 (rows predating the backfill) so the SPA
// gracefully falls back to the hash handle.
func TestPMTaskIssueMap_245_OrgRef(t *testing.T) {
	now := time.Date(2026, 6, 4, 0, 0, 0, 0, time.UTC)

	task, err := pm.RehydrateTask(pm.RehydrateTaskInput{
		ID: "task-abc", ProjectID: "proj-1", Title: "T", Status: pm.TaskOpen,
		CreatedBy: "user:a", CreatedAt: now, UpdatedAt: now, Version: 1, OrgNumber: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := pmTaskMap(task)["org_ref"]; got != "T7" {
		t.Fatalf("task org_ref = %v, want T7", got)
	}

	issue, err := pm.RehydrateIssue(pm.RehydrateIssueInput{
		ID: "issue-abc", ProjectID: "proj-1", Title: "I", Status: pm.IssueOpen,
		CreatedBy: "user:a", CreatedAt: now, UpdatedAt: now, Version: 1, OrgNumber: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := pmIssueMap(issue)["org_ref"]; got != "I3" {
		t.Fatalf("issue org_ref = %v, want I3", got)
	}

	// org_number 0 → org_ref omitted (graceful fallback to hash handle in the UI).
	unNumbered, _ := pm.RehydrateTask(pm.RehydrateTaskInput{
		ID: "task-x", ProjectID: "proj-1", Title: "T", Status: pm.TaskOpen,
		CreatedBy: "user:a", CreatedAt: now, UpdatedAt: now, Version: 1, OrgNumber: 0,
	})
	if _, present := pmTaskMap(unNumbered)["org_ref"]; present {
		t.Fatalf("org_ref must be omitted when org_number is 0")
	}
}
