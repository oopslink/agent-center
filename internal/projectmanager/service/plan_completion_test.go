package service

import (
	"errors"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func (h *planAdvanceHarness) seedAssignedTaskFollowing(t *testing.T, pid pm.ProjectID, planID pm.PlanID, title, assignee string, follows pm.TaskID) pm.TaskID {
	t.Helper()
	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{
		ProjectID:     pid,
		Title:         title,
		CreatedBy:     "user:a",
		FollowsTaskID: follows,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := assignee
	if err := h.svc.BatchUpdateTask(h.ctx, tid, BatchTaskPatch{Assignee: &a}, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.SelectTaskIntoPlan(h.ctx, planID, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	return tid
}

func TestPlanCompletion_RemediatedHistoricalFailuresAutoCompletePlan5a432139Shape(t *testing.T) {
	h := planAdvanceSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "plan-5a432139 shape", CreatedBy: "user:a"})
	h.drain(t)

	oldT1286 := h.seedAssignedTask(t, pid, planID, "T1286 early failed implementation", "user:dev1")
	oldT1287 := h.seedAssignedTask(t, pid, planID, "T1287 early failed verification", "user:qa1")
	remediate1286 := h.seedAssignedTaskFollowing(t, pid, planID, "recovery/remediation for T1286", "user:dev2", oldT1286)
	remediate1287 := h.seedAssignedTaskFollowing(t, pid, planID, "recovery/remediation for T1287", "user:qa2", oldT1287)
	reverify := h.seedAssignedTask(t, pid, planID, "independent reverify", "user:qa3")
	integrate := h.seedAssignedTask(t, pid, planID, "integration", "user:int")
	mainAcceptance := h.seedAssignedTask(t, pid, planID, "main final acceptance", "user:pd")

	for _, dep := range []struct {
		from pm.TaskID
		to   pm.TaskID
	}{
		{reverify, oldT1286},
		{reverify, oldT1287},
		{reverify, remediate1286},
		{reverify, remediate1287},
		{integrate, reverify},
		{mainAcceptance, integrate},
		{mainAcceptance, oldT1286},
		{mainAcceptance, oldT1287},
	} {
		if err := h.svc.AddPlanDependency(ctx, planID, dep.from, dep.to, "user:a"); err != nil {
			t.Fatalf("AddPlanDependency(%s,%s): %v", dep.from, dep.to, err)
		}
	}

	h.setTaskStatus(t, oldT1286, pm.TaskDiscarded)
	h.setTaskStatus(t, oldT1287, pm.TaskDiscarded)
	for _, id := range []pm.TaskID{remediate1286, remediate1287, reverify, integrate, mainAcceptance} {
		h.setTaskStatus(t, id, pm.TaskCompleted)
	}
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	if dispatched, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan: %v", err)
	} else if len(dispatched) != 0 {
		t.Fatalf("dispatched=%v want no current work", dispatched)
	}
	p, err := h.plans.FindByID(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status() != pm.PlanDone {
		t.Fatalf("plan status=%s want done", p.Status())
	}
	if err := h.svc.CompletePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("CompletePlan idempotent on done plan: %v", err)
	}

	detail, err := h.svc.GetPlanDetail(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.View.Progress.Done != 5 || detail.View.Progress.Total != 5 {
		t.Fatalf("progress=%+v want current effective 5/5", detail.View.Progress)
	}
	if !detail.View.HasFailed {
		t.Fatal("has_failed=false; historical failures must remain observable")
	}
	if len(detail.View.HistoricalFailures) != 2 || len(detail.View.ActiveFailures) != 0 {
		t.Fatalf("historical_failures=%v active_failures=%v want 2 historical and 0 active", detail.View.HistoricalFailures, detail.View.ActiveFailures)
	}
	nodes := nodesByID(detail.View.Nodes)
	for _, id := range []pm.TaskID{oldT1286, oldT1287} {
		node := nodes[id]
		if node.NodeStatus != pm.NodeFailed || node.Effective {
			t.Fatalf("historical node %s=%+v want failed effective=false", id, node)
		}
		if len(node.SupersededBy) == 0 {
			t.Fatalf("historical node %s missing superseded_by", id)
		}
		task, err := h.tasks.FindByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status() != pm.TaskDiscarded {
			t.Fatalf("historical task %s status=%s want discarded", id, task.Status())
		}
	}
}

func TestPlanCompletion_ManualCompleteAcceptsRemediatedHistoricalFailure(t *testing.T) {
	h := planAdvanceSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "manual remediation completion", CreatedBy: "user:a"})
	h.drain(t)
	old := h.seedAssignedTask(t, pid, planID, "old failed node", "user:dev1")
	fix := h.seedAssignedTaskFollowing(t, pid, planID, "replacement node", "user:dev2", old)
	if err := h.svc.AddPlanDependency(ctx, planID, fix, old, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, old, pm.TaskDiscarded)
	h.setTaskStatus(t, fix, pm.TaskCompleted)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.CompletePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("CompletePlan: %v", err)
	}
	p, _ := h.plans.FindByID(ctx, planID)
	if p.Status() != pm.PlanDone {
		t.Fatalf("plan status=%s want done", p.Status())
	}
}

func TestPlanCompletion_UnremediatedFailureBlocksAutoAndManualCompletion(t *testing.T) {
	h := planAdvanceSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "unrecovered failure", CreatedBy: "user:a"})
	h.drain(t)
	failed := h.seedAssignedTask(t, pid, planID, "failed leaf", "user:dev")
	accept := h.seedAssignedTask(t, pid, planID, "acceptance", "user:pd")
	if err := h.svc.AddPlanDependency(ctx, planID, accept, failed, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, failed, pm.TaskDiscarded)
	h.setTaskStatus(t, accept, pm.TaskCompleted)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	p, _ := h.plans.FindByID(ctx, planID)
	if p.Status() != pm.PlanRunning {
		t.Fatalf("plan status=%s want running while failed node is unreplaced", p.Status())
	}
	if err := h.svc.CompletePlan(ctx, planID, "user:a"); !errors.Is(err, pm.ErrPlanNotComplete) {
		t.Fatalf("CompletePlan err=%v want ErrPlanNotComplete", err)
	}
	detail, _ := h.svc.GetPlanDetail(ctx, planID)
	if len(detail.View.ActiveFailures) != 1 || detail.View.ActiveFailures[0] != failed {
		t.Fatalf("active_failures=%v want [%s]", detail.View.ActiveFailures, failed)
	}
}

func TestPlanCompletion_ReadyDispatchedAndRunningWorkBlockManualCompletion(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, h *planAdvanceHarness, planID pm.PlanID, taskID pm.TaskID)
	}{
		{name: "ready"},
		{name: "dispatched", setup: func(t *testing.T, h *planAdvanceHarness, planID pm.PlanID, taskID pm.TaskID) {
			t.Helper()
			if dispatched, err := h.svc.AdvancePlan(h.ctx, planID, "user:a"); err != nil {
				t.Fatal(err)
			} else if len(dispatched) != 1 || dispatched[0] != taskID {
				t.Fatalf("dispatched=%v want [%s]", dispatched, taskID)
			}
		}},
		{name: "running", setup: func(t *testing.T, h *planAdvanceHarness, _ pm.PlanID, taskID pm.TaskID) {
			t.Helper()
			h.setTaskStatus(t, taskID, pm.TaskRunning)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := planAdvanceSetup(t)
			ctx := h.ctx
			pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
			planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "active " + tc.name, CreatedBy: "user:a"})
			h.drain(t)
			taskID := h.seedAssignedTask(t, pid, planID, "current work", "user:dev")
			if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
				t.Fatal(err)
			}
			if tc.setup != nil {
				tc.setup(t, h, planID, taskID)
			}
			if err := h.svc.CompletePlan(ctx, planID, "user:a"); !errors.Is(err, pm.ErrPlanNotComplete) {
				t.Fatalf("CompletePlan err=%v want ErrPlanNotComplete", err)
			}
			p, _ := h.plans.FindByID(ctx, planID)
			if p.Status() != pm.PlanRunning {
				t.Fatalf("plan status=%s want running", p.Status())
			}
		})
	}
}

func nodesByID(nodes []pm.PlanNodeView) map[pm.TaskID]pm.PlanNodeView {
	out := make(map[pm.TaskID]pm.PlanNodeView, len(nodes))
	for _, node := range nodes {
		out[node.TaskID] = node
	}
	return out
}
