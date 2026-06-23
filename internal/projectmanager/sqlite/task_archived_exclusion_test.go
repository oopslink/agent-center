package sqlite

import (
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TestTaskRepo_ListOrgPage_ExcludesArchived is the T339 display fix: an archived task
// (archived_at != '') must NOT leak into the default tasks list / board, even when its
// status still matches the filter (the open+archived leak). IncludeArchived opts back
// in for an explicit archived view.
func TestTaskRepo_ListOrgPage_ExcludesArchived(t *testing.T) {
	ctx, pr, _, _, tr, _, _, _ := setup(t)
	p, err := pm.NewProject(pm.NewProjectInput{ID: "P1", OrganizationID: "org-1", Name: "P1", CreatedBy: "user:a", CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	if err := pr.Save(ctx, p); err != nil {
		t.Fatal(err)
	}

	archivedAt := t0.Add(time.Hour)
	mk := func(id string, status pm.TaskStatus, archived bool) {
		in := pm.RehydrateTaskInput{
			ID: pm.TaskID(id), ProjectID: "P1", Title: id, Status: status,
			CreatedBy: "user:a", CreatedAt: t0, UpdatedAt: t0, Version: 1,
		}
		if archived {
			in.ArchivedAt = &archivedAt
			in.ArchivedBy = "user:a"
		}
		tk, err := pm.RehydrateTask(in)
		if err != nil {
			t.Fatal(err)
		}
		if err := tr.Save(ctx, tk); err != nil {
			t.Fatal(err)
		}
	}
	mk("live-open", pm.TaskOpen, false)       // live → visible
	mk("archived-open", pm.TaskOpen, true)    // the leak → hidden by default
	mk("archived-done", pm.TaskCompleted, true)

	q := pm.OrgListQuery{ProjectIDs: []pm.ProjectID{"P1"}}

	t.Run("default hides archived", func(t *testing.T) {
		got, total, err := tr.ListOrgPage(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if total != 1 || len(got) != 1 {
			t.Fatalf("want only the 1 live task, got total=%d len=%d", total, len(got))
		}
		if string(got[0].ID()) != "live-open" {
			t.Fatalf("want live-open, got %s", got[0].ID())
		}
	})

	t.Run("status filter does not re-surface archived", func(t *testing.T) {
		got, total, err := tr.ListOrgPage(ctx, pm.OrgListQuery{ProjectIDs: []pm.ProjectID{"P1"}, Statuses: []string{"open"}})
		if err != nil {
			t.Fatal(err)
		}
		if total != 1 || len(got) != 1 || string(got[0].ID()) != "live-open" {
			t.Fatalf("status=open must still exclude archived-open; got total=%d len=%d", total, len(got))
		}
	})

	t.Run("IncludeArchived opts back in", func(t *testing.T) {
		qa := q
		qa.IncludeArchived = true
		_, total, err := tr.ListOrgPage(ctx, qa)
		if err != nil {
			t.Fatal(err)
		}
		if total != 3 {
			t.Fatalf("IncludeArchived must return all 3, got %d", total)
		}
	})
}
