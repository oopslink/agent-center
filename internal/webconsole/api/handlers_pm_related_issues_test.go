package api

import (
	"context"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// TestRelatedIssuesAndPlansAPI: the issue↔plan derive mirror endpoints.
//
//	GET …/plans/{id}/related-issues  → the plan's source issue(s).
//	GET …/issues/{id}/related-plans  → the plans derived from the issue.
func TestRelatedIssuesAndPlansAPI(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, err := fx.deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "P", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	issueX, err := fx.deps.PM.CreateIssue(ctx, pmservice.CreateIssueCommand{ProjectID: pid, Title: "X", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}

	mkPlan := func(name string, issue pm.IssueID) pm.PlanID {
		plID, perr := fx.deps.PM.CreatePlan(ctx, pmservice.CreatePlanCommand{ProjectID: pid, Name: name, CreatedBy: caller})
		if perr != nil {
			t.Fatalf("CreatePlan %s: %v", name, perr)
		}
		tid, terr := fx.deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: name + " task", CreatedBy: caller, DerivedFromIssue: issue})
		if terr != nil {
			t.Fatalf("CreateTask %s: %v", name, terr)
		}
		if serr := fx.deps.PM.SelectTaskIntoPlan(ctx, plID, tid, caller); serr != nil {
			t.Fatalf("SelectTaskIntoPlan %s: %v", name, serr)
		}
		return plID
	}

	planA := mkPlan("A", issueX)
	planB := mkPlan("B", issueX)
	fx.drain(t)

	// planA's related issues = {issueX}.
	resp := orgScopedGet(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+string(planA)+"/related-issues", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("related-issues status=%d", resp.StatusCode)
	}
	issues, ok := decodeBody(t, resp)["issues"].([]any)
	if !ok || len(issues) != 1 {
		t.Fatalf("issues=%v want exactly 1 (issueX)", issues)
	}
	if row := issues[0].(map[string]any); row["id"] != string(issueX) {
		t.Fatalf("related[0].id=%v want issueX %s", row["id"], issueX)
	}

	// issueX's related plans = {planA, planB}.
	resp = orgScopedGet(t, s.URL+"/api/projects/"+string(pid)+"/issues/"+string(issueX)+"/related-plans", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("related-plans status=%d", resp.StatusCode)
	}
	plans, ok := decodeBody(t, resp)["plans"].([]any)
	if !ok || len(plans) != 2 {
		t.Fatalf("plans=%v want exactly 2 (planA, planB)", plans)
	}
	got := map[string]bool{}
	for _, p := range plans {
		got[p.(map[string]any)["id"].(string)] = true
	}
	if !got[string(planA)] || !got[string(planB)] {
		t.Fatalf("plans=%v want {planA %s, planB %s}", plans, planA, planB)
	}

	// A plan with no source issue → empty related-issues list.
	solo, _ := fx.deps.PM.CreatePlan(ctx, pmservice.CreatePlanCommand{ProjectID: pid, Name: "solo", CreatedBy: caller})
	stid, _ := fx.deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "solo task", CreatedBy: caller})
	if err := fx.deps.PM.SelectTaskIntoPlan(ctx, solo, stid, caller); err != nil {
		t.Fatal(err)
	}
	resp = orgScopedGet(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+string(solo)+"/related-issues", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("solo related-issues status=%d", resp.StatusCode)
	}
	if got := decodeBody(t, resp)["issues"].([]any); len(got) != 0 {
		t.Fatalf("solo issues=%v want empty", got)
	}
}
