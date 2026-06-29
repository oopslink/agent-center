package api

import (
	"context"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// TestRelatedPlansAPI (T581): GET …/plans/{id}/related-plans returns the OTHER plans
// derived from the same source issue (current plan excluded), as the plan-detail rail
// "Related Plans" list. A plan with no source issue gets an empty list.
func TestRelatedPlansAPI(t *testing.T) {
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

	// planA's related set = {planB} (same issue, self excluded).
	resp := orgScopedGet(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+string(planA)+"/related-plans", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("related-plans status=%d", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	plans, ok := body["plans"].([]any)
	if !ok || len(plans) != 1 {
		t.Fatalf("plans=%v want exactly 1 (planB)", body["plans"])
	}
	row := plans[0].(map[string]any)
	if row["id"] != string(planB) {
		t.Fatalf("related[0].id=%v want planB %s", row["id"], planB)
	}

	// A plan with no source issue → empty related list.
	solo, _ := fx.deps.PM.CreatePlan(ctx, pmservice.CreatePlanCommand{ProjectID: pid, Name: "solo", CreatedBy: caller})
	stid, _ := fx.deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "solo task", CreatedBy: caller})
	if err := fx.deps.PM.SelectTaskIntoPlan(ctx, solo, stid, caller); err != nil {
		t.Fatal(err)
	}
	resp = orgScopedGet(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+string(solo)+"/related-plans", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("solo related-plans status=%d", resp.StatusCode)
	}
	if got := decodeBody(t, resp)["plans"].([]any); len(got) != 0 {
		t.Fatalf("solo plans=%v want empty", got)
	}
}
