package api

import (
	"context"
	"net/http"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// =============================================================================
// v2.13.0 / I18 F4 — list_unmerged_branches agent tool (the unmerged-branch
// board: un-done Integrate nodes). Asserts the PASSTHROUGH wiring — args parsed,
// the pm read called, project-scoping enforced, the board serialized, and the
// nil-safe cycle-metadata behavior (no meta ⇒ all_merged). The projection itself
// is unit-tested in internal/projectmanager (UnmergedIntegrations).
// =============================================================================

// fakeCycleMeta is a stub CycleNodeMetaPort returning a fixed per-plan map.
type fakeCycleMeta struct{ m map[pm.TaskID]pm.CycleNodeMeta }

func (f fakeCycleMeta) CycleNodeMeta(_ context.Context, _ pm.PlanID) (map[pm.TaskID]pm.CycleNodeMeta, error) {
	return f.m, nil
}

// TestListUnmergedBranches_NoMeta_AllMerged: with no cycle metadata wired, the
// board is empty (all_merged) rather than wrong — a non-scaffolded plan has no
// Integrate nodes to report.
func TestListUnmergedBranches_NoMeta_AllMerged(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	f.seedPlanTask(t, pid, planID)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/list_unmerged_branches", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid), "plan_id": planID})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	if allMerged, _ := body["all_merged"].(bool); !allMerged {
		t.Fatalf("all_merged = %v, want true; body = %v", body["all_merged"], body)
	}
	if cnt, _ := body["unmerged_count"].(float64); cnt != 0 {
		t.Fatalf("unmerged_count = %v, want 0", body["unmerged_count"])
	}
	if rows, ok := body["unmerged"].([]any); !ok || len(rows) != 0 {
		t.Fatalf("unmerged = %v, want empty slice", body["unmerged"])
	}
}

// TestListUnmergedBranches_WithMeta_ListsUndoneIntegrate: a task marked role=
// integrate (not yet done) shows on the board with its branch/base/node_status.
func TestListUnmergedBranches_WithMeta_ListsUndoneIntegrate(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	tid := f.seedPlanTask(t, pid, planID)
	// Wire a fake cycle-meta port marking the node as an Integrate node.
	f.pmSvc.SetCycleNodeMetaProvider(fakeCycleMeta{m: map[pm.TaskID]pm.CycleNodeMeta{
		pm.TaskID(tid): {Role: pm.CycleRoleIntegrate, Branch: "f4-unmerged-board", Base: "dev/v2.13.0"},
	}})
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/list_unmerged_branches", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid), "plan_id": planID})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	if allMerged, _ := body["all_merged"].(bool); allMerged {
		t.Fatalf("all_merged = true, want false (one Integrate node still open); body = %v", body)
	}
	rows, _ := body["unmerged"].([]any)
	if len(rows) != 1 {
		t.Fatalf("unmerged rows = %d, want 1; body = %v", len(rows), body)
	}
	row, _ := rows[0].(map[string]any)
	if row["task_id"] != tid {
		t.Errorf("row task_id = %v, want %s", row["task_id"], tid)
	}
	if row["branch"] != "f4-unmerged-board" || row["base"] != "dev/v2.13.0" {
		t.Errorf("row branch/base = %v/%v, want f4-unmerged-board/dev/v2.13.0", row["branch"], row["base"])
	}
	if row["node_status"] == "" || row["node_status"] == nil {
		t.Errorf("row node_status missing: %v", row)
	}
}

// TestListUnmergedBranches_ForeignProject_404: a plan named under the wrong
// project is not found (mirrors get_plan's project scoping).
func TestListUnmergedBranches_ForeignProject_404(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, planID := f.seedPlanMember(t)
	srv := f.server(t)

	status, _ := postBearer(t, srv.URL, "/admin/agent-tools/list_unmerged_branches", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": "project-not-the-plans", "plan_id": planID})
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (plan not in named project)", status)
	}
}

// TestListUnmergedBranches_MissingPlanID_400: the plan_id is required.
func TestListUnmergedBranches_MissingPlanID_400(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedPlanMember(t)
	srv := f.server(t)

	status, _ := postBearer(t, srv.URL, "/admin/agent-tools/list_unmerged_branches", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid)})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (missing plan_id)", status)
	}
}
