package service

import (
	"context"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// fakePausedPort is a test PausedTaskPort: it reports the configured task ids as
// paused (intersected with the requested set, like the real adapter).
type fakePausedPort struct{ paused map[string]bool }

func (f fakePausedPort) PausedTasks(_ context.Context, taskIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, id := range taskIDs {
		if f.paused[id] {
			out[id] = true
		}
	}
	return out, nil
}

func nodeStatusOf(d *PlanDetail, tid pm.TaskID) pm.NodeStatus {
	for _, n := range d.View.Nodes {
		if n.TaskID == tid {
			return n.NodeStatus
		}
	}
	return ""
}

// T53: a RUNNING plan node whose work item is paused derives node_status=paused
// through the read model (GetPlanDetail), once the PausedTaskPort is wired — and
// stays `running` without it. This is the display-truth half of the fix: the DAG
// stops mis-showing a set-aside node as running.
func TestGetPlanDetail_PausedOverlay(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "alpha", CreatedBy: "user:a"})
	h.drain(t)
	tRun := h.seedAssignedTask(t, pid, planID, "running-node", "agent:AG1")
	h.setTaskStatus(t, tRun, pm.TaskRunning)

	// No provider wired → the running node shows `running` (pre-T53 behavior, and the
	// nil-safe default).
	detail, err := h.svc.GetPlanDetail(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if got := nodeStatusOf(detail, tRun); got != pm.NodeRunning {
		t.Fatalf("no-provider node_status=%s want running", got)
	}

	// Wire a provider marking the task paused → the SAME running task now derives
	// `paused` in the read model.
	h.svc.SetPausedTaskProvider(fakePausedPort{paused: map[string]bool{string(tRun): true}})
	detail, err = h.svc.GetPlanDetail(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if got := nodeStatusOf(detail, tRun); got != pm.NodePaused {
		t.Fatalf("with-provider node_status=%s want paused (work item paused)", got)
	}
	if detail.View.AllDone {
		t.Fatal("AllDone must stay false while a node is paused")
	}

	// ListPlanSummaries (the Work Board batched read) overlays paused too.
	summaries, err := h.svc.ListPlanSummaries(h.ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, d := range summaries {
		if d.Plan.ID() == planID {
			found = true
			if got := nodeStatusOf(d, tRun); got != pm.NodePaused {
				t.Fatalf("summary node_status=%s want paused", got)
			}
		}
	}
	if !found {
		t.Fatal("plan missing from summaries")
	}
}
