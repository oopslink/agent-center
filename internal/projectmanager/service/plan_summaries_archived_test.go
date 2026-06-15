package service

import (
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// T124 (T98 regression): ListPlanSummaries excludes archived plans (Work Board /
// agent-tools default), while ListPlanSummariesIncludingArchived returns them so
// the org Plan list's status filter can surface archived on ?status=archived.
// Before this fix the archived plan was stripped unconditionally → the global
// "Archived" filter returned 0 items (ACCEPTANCE §3.11 FAIL).
func TestListPlanSummaries_ArchivedInclusion(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "shipped plan", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.ArchivePlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("ArchivePlan: %v", err)
	}

	has := func(list []*PlanDetail, id pm.PlanID) bool {
		for _, d := range list {
			if d.Plan.ID() == id {
				return true
			}
		}
		return false
	}

	// Default list excludes the archived plan.
	def, err := h.svc.ListPlanSummaries(h.ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if has(def, planID) {
		t.Fatal("ListPlanSummaries must EXCLUDE the archived plan (Work Board default)")
	}

	// Archived-aware list includes it (the org list then filters via statusPasses).
	all, err := h.svc.ListPlanSummariesIncludingArchived(h.ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if !has(all, planID) {
		t.Fatal("ListPlanSummariesIncludingArchived must INCLUDE the archived plan")
	}
}
