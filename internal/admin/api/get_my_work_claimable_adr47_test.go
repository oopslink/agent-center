package api

import (
	"context"
	"net/http"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TestGetMyWork_SurfacesClaimablePoolTasks (ADR-0047) proves get_my_work surfaces
// the agent's CLAIMABLE built-in-pool tasks under "claimable_tasks" — the pull pool
// creates NO WorkItem (no wake), so the WorkItem-only surface would miss them. After
// selecting an assigned task into the (running) built-in pool and sweeping the
// reconcile loop (node_status ready→dispatched), get_my_work returns the task with
// claimable=true and node_status=dispatched.
func TestGetMyWork_SurfacesClaimablePoolTasks(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	ctx := context.Background()
	pid, tid := f.seedMemberProject(t) // task tid assigned to agent:AG1, in backlog.

	// Find the project's auto-created built-in pool.
	plans, err := f.pmSvc.ListPlans(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	var pool pm.PlanID
	for _, p := range plans {
		if p.IsBuiltin() {
			pool = p.ID()
		}
	}
	if pool == "" {
		t.Fatal("no built-in pool found for project")
	}

	// Select the assigned task into the running pool (allowed; that is how a task
	// enters the claimable pool).
	if err := f.pmSvc.SelectTaskIntoPlan(ctx, pool, pm.TaskID(tid), pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatalf("SelectTaskIntoPlan into pool: %v", err)
	}
	f.drain(t)
	// Sweep the pull dispatch (records the dispatch → node becomes dispatched →
	// claimable). No PlanDispatcher call, no wake, no WorkItem.
	if err := f.pmSvc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatalf("ReconcileRunningPlans: %v", err)
	}

	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_my_work", "acat_w1",
		map[string]any{"agent_id": atAgent1})
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%v", status, body)
	}
	claimable, _ := body["claimable_tasks"].([]any)
	if len(claimable) != 1 {
		t.Fatalf("claimable_tasks len=%d, want 1; body=%v", len(claimable), body)
	}
	row, _ := claimable[0].(map[string]any)
	if row["id"] != tid {
		t.Fatalf("claimable task id=%v, want %s", row["id"], tid)
	}
	if row["claimable"] != true {
		t.Fatalf("claimable flag=%v, want true", row["claimable"])
	}
	if row["node_status"] != string(pm.NodeDispatched) {
		t.Fatalf("node_status=%v, want dispatched", row["node_status"])
	}
}

// TestGetMyWork_NoClaimableWhenBacklog proves a still-backlog assigned task (not yet
// in any plan) is NOT surfaced as claimable (planID=="" → never claimable).
func TestGetMyWork_NoClaimableWhenBacklog(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.seedMemberProject(t) // task assigned but left in the backlog.

	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_my_work", "acat_w1",
		map[string]any{"agent_id": atAgent1})
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%v", status, body)
	}
	claimable, _ := body["claimable_tasks"].([]any)
	if len(claimable) != 0 {
		t.Fatalf("claimable_tasks len=%d, want 0 (backlog task not claimable); body=%v", len(claimable), body)
	}
}
