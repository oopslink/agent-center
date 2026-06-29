package service

import (
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// T581 — ListRelatedPlans: the OTHER structured plans derived from the same source
// issue as a plan (the plan-detail "Related Plans" rail).

// mkIssuePlan seeds a draft structured plan with one task derived from issueID, and
// returns the plan id.
func mkIssuePlan(t *testing.T, h *planAdvanceHarness, pid pm.ProjectID, name string, issueID pm.IssueID) pm.PlanID {
	t.Helper()
	planID, err := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: name, CreatedBy: "user:a"})
	if err != nil {
		t.Fatalf("CreatePlan %s: %v", name, err)
	}
	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: name + " task", CreatedBy: "user:a", DerivedFromIssue: issueID})
	if err != nil {
		t.Fatalf("CreateTask %s: %v", name, err)
	}
	if err := h.svc.SelectTaskIntoPlan(h.ctx, planID, tid, "user:a"); err != nil {
		t.Fatalf("SelectTaskIntoPlan %s: %v", name, err)
	}
	return planID
}

func TestListRelatedPlans_SameIssueSiblings(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	issueX, _ := h.svc.CreateIssue(h.ctx, CreateIssueCommand{ProjectID: pid, Title: "X", CreatedBy: "user:a"})
	issueY, _ := h.svc.CreateIssue(h.ctx, CreateIssueCommand{ProjectID: pid, Title: "Y", CreatedBy: "user:a"})

	planA := mkIssuePlan(t, h, pid, "A", issueX)
	planB := mkIssuePlan(t, h, pid, "B", issueX)
	_ = mkIssuePlan(t, h, pid, "C", issueY) // different issue → not related

	// A builtin-pool task ALSO derived from issue X must never count as a related plan.
	pool := findBuiltinPlan(t, h, pid)
	poolTid, _ := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "pool", CreatedBy: "user:a", DerivedFromIssue: issueX})
	if err := h.svc.SelectTaskIntoPlan(h.ctx, pool.ID(), poolTid, "user:a"); err != nil {
		t.Fatalf("select pool: %v", err)
	}

	related, err := h.svc.ListRelatedPlans(h.ctx, planA, "user:a")
	if err != nil {
		t.Fatalf("ListRelatedPlans: %v", err)
	}
	if len(related) != 1 {
		t.Fatalf("related count=%d, want 1 (only planB); got %v", len(related), planIDs(related))
	}
	if related[0].ID() != planB {
		t.Fatalf("related[0]=%s, want planB %s", related[0].ID(), planB)
	}
	// Self is excluded.
	for _, p := range related {
		if p.ID() == planA {
			t.Fatal("related must not include the plan itself")
		}
	}
}

func TestListRelatedPlans_NoSourceIssue_Empty(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	// A plan whose task carries no derived_from_issue → no "issue" → no related plans.
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "solo", CreatedBy: "user:a"})
	tid, _ := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "t", CreatedBy: "user:a"})
	if err := h.svc.SelectTaskIntoPlan(h.ctx, planID, tid, "user:a"); err != nil {
		t.Fatalf("select: %v", err)
	}
	related, err := h.svc.ListRelatedPlans(h.ctx, planID, "user:a")
	if err != nil {
		t.Fatalf("ListRelatedPlans: %v", err)
	}
	if len(related) != 0 {
		t.Fatalf("related count=%d, want 0 (no source issue)", len(related))
	}
}

func TestListRelatedPlans_NonMemberRejected(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	issueX, _ := h.svc.CreateIssue(h.ctx, CreateIssueCommand{ProjectID: pid, Title: "X", CreatedBy: "user:a"})
	planA := mkIssuePlan(t, h, pid, "A", issueX)
	if _, err := h.svc.ListRelatedPlans(h.ctx, planA, "user:stranger"); err == nil {
		t.Fatal("a non-member must be rejected")
	}
}

func planIDs(plans []*pm.Plan) []pm.PlanID {
	out := make([]pm.PlanID, len(plans))
	for i, p := range plans {
		out[i] = p.ID()
	}
	return out
}
