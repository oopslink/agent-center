package service

import (
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// ListRelatedIssues / ListPlansForIssue — the issue↔plan derive mirror behind the
// plan-detail "Related Issues" rail and the issue-detail "Related Plans" panel.

func TestListRelatedIssues_PlanSourceIssues(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	issueX, _ := h.svc.CreateIssue(h.ctx, CreateIssueCommand{ProjectID: pid, Title: "X", CreatedBy: "user:a"})

	planA := mkIssuePlan(t, h, pid, "A", issueX)

	issues, err := h.svc.ListRelatedIssues(h.ctx, planA, "user:a")
	if err != nil {
		t.Fatalf("ListRelatedIssues: %v", err)
	}
	if len(issues) != 1 || issues[0].ID() != issueX {
		t.Fatalf("related issues=%v, want exactly [issueX %s]", issueIDs(issues), issueX)
	}
}

func TestListRelatedIssues_NoSourceIssue_Empty(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "solo", CreatedBy: "user:a"})
	tid, _ := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "t", CreatedBy: "user:a"})
	if err := h.svc.SelectTaskIntoPlan(h.ctx, planID, tid, "user:a"); err != nil {
		t.Fatalf("select: %v", err)
	}
	issues, err := h.svc.ListRelatedIssues(h.ctx, planID, "user:a")
	if err != nil {
		t.Fatalf("ListRelatedIssues: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("related issues=%v, want 0 (no source issue)", issueIDs(issues))
	}
}

func TestListRelatedIssues_NonMemberRejected(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	issueX, _ := h.svc.CreateIssue(h.ctx, CreateIssueCommand{ProjectID: pid, Title: "X", CreatedBy: "user:a"})
	planA := mkIssuePlan(t, h, pid, "A", issueX)
	if _, err := h.svc.ListRelatedIssues(h.ctx, planA, "user:stranger"); err == nil {
		t.Fatal("a non-member must be rejected")
	}
}

func TestListPlansForIssue_DerivedPlans(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	issueX, _ := h.svc.CreateIssue(h.ctx, CreateIssueCommand{ProjectID: pid, Title: "X", CreatedBy: "user:a"})
	issueY, _ := h.svc.CreateIssue(h.ctx, CreateIssueCommand{ProjectID: pid, Title: "Y", CreatedBy: "user:a"})

	planA := mkIssuePlan(t, h, pid, "A", issueX)
	planB := mkIssuePlan(t, h, pid, "B", issueX)
	_ = mkIssuePlan(t, h, pid, "C", issueY) // different issue → not in issueX's list

	// A builtin-pool task ALSO derived from issue X must never count as a related plan.
	pool := findBuiltinPlan(t, h, pid)
	poolTid, _ := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "pool", CreatedBy: "user:a", DerivedFromIssue: issueX})
	if err := h.svc.SelectTaskIntoPlan(h.ctx, pool.ID(), poolTid, "user:a"); err != nil {
		t.Fatalf("select pool: %v", err)
	}

	plans, err := h.svc.ListPlansForIssue(h.ctx, issueX, "user:a")
	if err != nil {
		t.Fatalf("ListPlansForIssue: %v", err)
	}
	// Both structured plans on issueX; the builtin pool excluded; planC (issueY) absent.
	if len(plans) != 2 {
		t.Fatalf("plans=%v, want 2 (planA, planB)", planIDs(plans))
	}
	got := map[pm.PlanID]bool{plans[0].ID(): true, plans[1].ID(): true}
	if !got[planA] || !got[planB] {
		t.Fatalf("plans=%v, want {planA %s, planB %s}", planIDs(plans), planA, planB)
	}
}

func TestListPlansForIssue_NonMemberRejected(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	issueX, _ := h.svc.CreateIssue(h.ctx, CreateIssueCommand{ProjectID: pid, Title: "X", CreatedBy: "user:a"})
	_ = mkIssuePlan(t, h, pid, "A", issueX)
	if _, err := h.svc.ListPlansForIssue(h.ctx, issueX, "user:stranger"); err == nil {
		t.Fatal("a non-member must be rejected")
	}
}

func issueIDs(issues []*pm.Issue) []pm.IssueID {
	out := make([]pm.IssueID, len(issues))
	for i, iss := range issues {
		out[i] = iss.ID()
	}
	return out
}
