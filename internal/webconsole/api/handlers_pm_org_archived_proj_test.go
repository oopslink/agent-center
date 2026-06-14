package api

import (
	"context"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// v2.9.1 T42: an ARCHIVED project's tasks/issues are hidden from the default
// org-level task/issue lists, but remain visible when the user explicitly filters
// to that project. A non-archived project is unaffected. (Class-guard against the
// "archived project's items leak into the global board" regression.)
func TestListOrgTasksIssues_ArchivedProjectHidden(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	// Active project + an archived project, each with 1 task + 1 issue.
	active, _ := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "Active", CreatedBy: caller})
	archived, _ := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "Archived", CreatedBy: caller})
	for _, pid := range []pm.ProjectID{active, archived} {
		if _, err := deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "tsk", CreatedBy: caller}); err != nil {
			t.Fatal(err)
		}
		if _, err := deps.PM.CreateIssue(ctx, pmservice.CreateIssueCommand{ProjectID: pid, Title: "iss", CreatedBy: caller}); err != nil {
			t.Fatal(err)
		}
	}
	if err := deps.PM.ArchiveProject(ctx, archived, caller); err != nil {
		t.Fatalf("ArchiveProject: %v", err)
	}

	// Default list: only the ACTIVE project's items (archived hidden).
	if tasks := decodeItems(t, orgScopedGet(t, s.URL+"/api/tasks", sess)); len(tasks) != 1 {
		t.Fatalf("default org tasks = %d, want 1 (archived project's task hidden)", len(tasks))
	}
	if issues := decodeItems(t, orgScopedGet(t, s.URL+"/api/issues", sess)); len(issues) != 1 {
		t.Fatalf("default org issues = %d, want 1 (archived project's issue hidden)", len(issues))
	}

	// Explicit filter to the ARCHIVED project: its items are shown (else filtering
	// by it would be a confusing empty list).
	if tasks := decodeItems(t, orgScopedGet(t, s.URL+"/api/tasks?project="+string(archived), sess)); len(tasks) != 1 {
		t.Fatalf("explicit archived-project task filter = %d, want 1 (shown when explicitly filtered)", len(tasks))
	}
	if issues := decodeItems(t, orgScopedGet(t, s.URL+"/api/issues?project="+string(archived), sess)); len(issues) != 1 {
		t.Fatalf("explicit archived-project issue filter = %d, want 1", len(issues))
	}

	// Explicit filter to the ACTIVE project: unaffected.
	if tasks := decodeItems(t, orgScopedGet(t, s.URL+"/api/tasks?project="+string(active), sess)); len(tasks) != 1 {
		t.Fatalf("active-project task filter = %d, want 1", len(tasks))
	}
}
