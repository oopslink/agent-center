package api

import (
	"context"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// fakeCycleMeta is a stub CycleNodeMetaPort returning a fixed per-plan map.
type fakeCycleMeta struct{ m map[pm.TaskID]pm.CycleNodeMeta }

func (f fakeCycleMeta) CycleNodeMeta(_ context.Context, _ pm.PlanID) (map[pm.TaskID]pm.CycleNodeMeta, error) {
	return f.m, nil
}

// TestUnmergedBranchesAPI: the F4 ship-gate board endpoint. With no cycle metadata
// the board is empty (all_merged); once a node is marked role=integrate (and is
// not yet done) it appears with its branch/base.
func TestUnmergedBranchesAPI(t *testing.T) {
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
	resp := orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/plans", `{"name":"v2.13.0","description":"cycle"}`, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("create plan status=%d", resp.StatusCode)
	}
	planID := decodeBody(t, resp)["id"].(string)
	fx.drain(t)
	tid := fx.seedSelectedTask(t, sess, pid, pm.PlanID(planID), "F4-Integrate", "user:"+sess.IdentityID)
	fx.drain(t)

	url := s.URL + "/api/projects/" + string(pid) + "/plans/" + planID + "/unmerged-branches"

	// No cycle metadata wired ⇒ empty board (all_merged), never a false positive.
	resp = orgScopedGet(t, url, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("unmerged-branches status=%d", resp.StatusCode)
	}
	board := decodeBody(t, resp)
	if board["all_merged"] != true {
		t.Fatalf("all_merged=%v want true; board=%v", board["all_merged"], board)
	}
	if got := board["unmerged"].([]any); len(got) != 0 {
		t.Fatalf("unmerged=%v want empty", got)
	}

	// Mark the node as an Integrate node (not yet done) ⇒ it shows on the board.
	fx.deps.PM.SetCycleNodeMetaProvider(fakeCycleMeta{m: map[pm.TaskID]pm.CycleNodeMeta{
		tid: {Role: pm.CycleRoleIntegrate, Branch: "f4-unmerged-board", Base: "dev/v2.13.0"},
	}})
	resp = orgScopedGet(t, url, sess)
	board = decodeBody(t, resp)
	if board["all_merged"] != false {
		t.Fatalf("all_merged=%v want false (one Integrate node open)", board["all_merged"])
	}
	rows := board["unmerged"].([]any)
	if len(rows) != 1 {
		t.Fatalf("unmerged rows=%d want 1; board=%v", len(rows), board)
	}
	row := rows[0].(map[string]any)
	if row["task_id"] != string(tid) || row["branch"] != "f4-unmerged-board" || row["base"] != "dev/v2.13.0" {
		t.Fatalf("row=%v want task=%s branch=f4-unmerged-board base=dev/v2.13.0", row, tid)
	}
	if row["node_status"] == nil || row["node_status"] == "" {
		t.Fatalf("row missing node_status: %v", row)
	}
}
