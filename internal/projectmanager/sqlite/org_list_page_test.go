package sqlite

import (
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// SQL-level pagination/sort/filter for the org Issues list (IssueRepo.ListOrgPage).
// Tasks/Plans share buildOrgListWhere + orgOrderBy + orgLimitOffset, so exercising
// issues covers the shared query builder; entity-specific bits (assignee/builtin)
// are covered by the handler tests.
func TestIssueRepo_ListOrgPage(t *testing.T) {
	ctx, pr, _, ir, _, _, _, _ := setup(t)
	mkProj := func(id string) {
		p, err := pm.NewProject(pm.NewProjectInput{ID: pm.ProjectID(id), OrganizationID: "org-1", Name: id, CreatedBy: "user:a", CreatedAt: t0})
		if err != nil {
			t.Fatal(err)
		}
		if err := pr.Save(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	mkProj("P1")
	mkProj("P2")
	mkProj("P3") // not in the query's project set → its issues must never appear
	upd := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Hour) }
	mk := func(id, proj, title string, st pm.IssueStatus, org int, updatedN int) {
		i, err := pm.RehydrateIssue(pm.RehydrateIssueInput{
			ID: pm.IssueID(id), ProjectID: pm.ProjectID(proj), Title: title, Status: st,
			CreatedBy: "user:a", CreatedAt: t0, UpdatedAt: upd(updatedN), Version: 1, OrgNumber: org,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := ir.Save(ctx, i); err != nil {
			t.Fatal(err)
		}
	}
	mk("i1", "P1", "Banana split", pm.IssueOpen, 1, 3)
	mk("i2", "P2", "Apple pie", pm.IssueInProgress, 2, 5)
	mk("i3", "P1", "Cherry tart", pm.IssueOpen, 10, 1)
	mk("i4", "P1", "Date cake", pm.IssueResolved, 4, 9) // terminal — excluded by default
	mk("i5", "P3", "Elsewhere", pm.IssueOpen, 5, 9)     // wrong project — excluded

	projects := []pm.ProjectID{"P1", "P2"}
	terminal := []string{"resolved", "closed", "withdrawn", "discarded"}

	t.Run("default excludes terminal + foreign projects; total is pre-page", func(t *testing.T) {
		got, total, err := ir.ListOrgPage(ctx, pm.OrgListQuery{ProjectIDs: projects, ExcludeStatuses: terminal})
		if err != nil {
			t.Fatal(err)
		}
		if total != 3 || len(got) != 3 {
			t.Fatalf("want 3 (i1,i2,i3), got total=%d len=%d", total, len(got))
		}
	})

	t.Run("sort title ASC + LIMIT/OFFSET pages, total stays full", func(t *testing.T) {
		got, total, err := ir.ListOrgPage(ctx, pm.OrgListQuery{
			ProjectIDs: projects, ExcludeStatuses: terminal,
			SortColumn: "title", SortDesc: false, Limit: 2, Offset: 0,
		})
		if err != nil {
			t.Fatal(err)
		}
		if total != 3 {
			t.Fatalf("total=%d want 3 (pre-page)", total)
		}
		// title ASC: Apple, Banana, Cherry → page 1 of 2 = [Apple, Banana].
		if len(got) != 2 || got[0].Title() != "Apple pie" || got[1].Title() != "Banana split" {
			t.Fatalf("page1 = %v", titles(got))
		}
		page2, _, _ := ir.ListOrgPage(ctx, pm.OrgListQuery{
			ProjectIDs: projects, ExcludeStatuses: terminal,
			SortColumn: "title", Limit: 2, Offset: 2,
		})
		if len(page2) != 1 || page2[0].Title() != "Cherry tart" {
			t.Fatalf("page2 = %v want [Cherry tart]", titles(page2))
		}
	})

	t.Run("org_ref sorts numerically by org_number (T2 < T10)", func(t *testing.T) {
		got, _, err := ir.ListOrgPage(ctx, pm.OrgListQuery{
			ProjectIDs: projects, ExcludeStatuses: terminal, SortColumn: "org_ref", SortDesc: false,
		})
		if err != nil {
			t.Fatal(err)
		}
		// org numbers among non-terminal: i1=1, i2=2, i3=10 → ASC = i1,i2,i3.
		if len(got) != 3 || got[0].OrgNumber() != 1 || got[1].OrgNumber() != 2 || got[2].OrgNumber() != 10 {
			t.Fatalf("org_ref asc order wrong: %v", orgNums(got))
		}
	})

	t.Run("explicit status include", func(t *testing.T) {
		got, total, err := ir.ListOrgPage(ctx, pm.OrgListQuery{ProjectIDs: projects, Statuses: []string{"in_progress"}})
		if err != nil {
			t.Fatal(err)
		}
		if total != 1 || len(got) != 1 || got[0].ID() != "i2" {
			t.Fatalf("status=in_progress → just i2, got %v", titles(got))
		}
	})

	t.Run("q substring search (case-insensitive)", func(t *testing.T) {
		got, total, err := ir.ListOrgPage(ctx, pm.OrgListQuery{ProjectIDs: projects, ExcludeStatuses: terminal, Q: "APPLE"})
		if err != nil {
			t.Fatal(err)
		}
		if total != 1 || len(got) != 1 || got[0].ID() != "i2" {
			t.Fatalf("q=APPLE → i2, got %v", titles(got))
		}
	})

	t.Run("empty project set → no rows, total 0", func(t *testing.T) {
		got, total, err := ir.ListOrgPage(ctx, pm.OrgListQuery{ProjectIDs: nil})
		if err != nil {
			t.Fatal(err)
		}
		if total != 0 || len(got) != 0 {
			t.Fatalf("empty projects → empty, got total=%d len=%d", total, len(got))
		}
	})
}

func titles(is []*pm.Issue) []string {
	out := make([]string, len(is))
	for i, x := range is {
		out[i] = x.Title()
	}
	return out
}
func orgNums(is []*pm.Issue) []int {
	out := make([]int, len(is))
	for i, x := range is {
		out[i] = x.OrgNumber()
	}
	return out
}
