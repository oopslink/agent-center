package service

import (
	"errors"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

// authorCycleViaAgentTools builds a Dev→Review→Decision→Integrate cycle plan using
// ONLY the agent-facing service methods (CreateTask + SelectTaskIntoPlan via
// seedAssignedTask == add_task_to_plan, and AddPlanControlEdge == add_plan_dependency
// with kind/when/max_rounds). This is exactly the path T802 unlocks: an agent can now
// author a Decision/loopback plan with no scaffold tool.
func authorCycleViaAgentTools(t *testing.T, h *planAdvanceHarness, pid pm.ProjectID, planID pm.PlanID) (dev, rev, dec, integ pm.TaskID) {
	t.Helper()
	dev = h.seedAssignedTask(t, pid, planID, "Dev", "user:dev")
	rev = h.seedAssignedTask(t, pid, planID, "Review", "user:rev")
	dec = h.seedAssignedTask(t, pid, planID, "Decision", "user:pd")
	integ = h.seedAssignedTask(t, pid, planID, "Integrate", "user:int")

	edges := []pm.Dependency{
		{FromTaskID: rev, ToTaskID: dev, Kind: pm.EdgeSeq},
		{FromTaskID: dec, ToTaskID: rev, Kind: pm.EdgeSeq},
		{FromTaskID: integ, ToTaskID: dec, Kind: pm.EdgeConditional, When: "pass"},
		{FromTaskID: dec, ToTaskID: dev, Kind: pm.EdgeLoopback, When: "reject", MaxRounds: 2},
	}
	for _, e := range edges {
		if err := h.svc.AddPlanControlEdge(h.ctx, planID, e, "user:a"); err != nil {
			t.Fatalf("AddPlanControlEdge %+v: %v", e, err)
		}
	}
	return dev, rev, dec, integ
}

// TestAddPlanControlEdge_AuthorsCyclePlan_ViaAgentTools proves an agent can author a
// Decision/loopback cycle purely through create_plan + add_task_to_plan +
// add_plan_dependency(kind=…): after StartPlan, buildPlanGraph produces a condition
// node gating the pass branch, and the loopback back-edge drives a reject round.
func TestAddPlanControlEdge_AuthorsCyclePlan_ViaAgentTools(t *testing.T) {
	h, orchSvc := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "authored", CreatedBy: "user:a"})
	h.drain(t)

	dev, rev, dec, integ := authorCycleViaAgentTools(t, h, pid, planID)

	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}

	// buildPlanGraph turned the conditional edge into a condition node gating the
	// decision's pass branch.
	p, _ := h.plans.FindByID(ctx, planID)
	if p.GraphID() == "" {
		t.Fatal("plan has no graph_id — buildPlanGraph did not run")
	}
	nodes, err := orchSvc.ListNodes(ctx, orch.GraphID(p.GraphID()))
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	var condFound bool
	for _, n := range nodes {
		if n.ControlKind() != orch.ControlKindCondition {
			continue
		}
		if cf, _ := n.Metadata()["condition_for"].(string); cf == string(dec) {
			condFound = true
			if !metaHasWhen(n.Metadata()["pass_whens"], "pass") {
				t.Fatalf("condition node pass_whens = %v, want to include 'pass'", n.Metadata()["pass_whens"])
			}
		}
	}
	if !condFound {
		t.Fatal("no condition node for the Decision — conditional edge was not turned into a gate")
	}

	// The loopback back-edge drives: walk Dev→Review→Decision, reject, and confirm
	// Dev is re-dispatched for another round (proves the loopback edge persisted).
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, dev, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, rev, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.SetDecisionOutcome(ctx, dec, "reject", "user:a"); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, dec, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	p, _ = h.plans.FindByID(ctx, planID)
	if p.Status() == pm.PlanDone {
		t.Fatal("plan done on reject — the loopback pass branch (Integrate) should stay gated")
	}
	if err := h.svc.applyLoopbacks(ctx, p, dec); err != nil {
		t.Fatalf("applyLoopbacks: %v", err)
	}
	dLoop, err := h.svc.AdvancePlan(ctx, planID, "user:a")
	if err != nil {
		t.Fatalf("AdvancePlan loopback: %v", err)
	}
	if !graphHasTaskID(dLoop, dev) {
		t.Fatalf("after reject, dispatch = %v, want Dev re-dispatched (loopback)", dLoop)
	}
	_ = integ
}

// twoTaskDraft creates a draft plan with two in-plan tasks (a, b) for edge-validation
// tests, returning their ids.
func twoTaskDraft(t *testing.T, h *planAdvanceHarness) (planID pm.PlanID, a, b pm.TaskID) {
	t.Helper()
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ = h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "edges", CreatedBy: "user:a"})
	h.drain(t)
	a = h.seedAssignedTask(t, pid, planID, "A", "user:a")
	b = h.seedAssignedTask(t, pid, planID, "B", "user:b")
	return planID, a, b
}

func TestAddPlanControlEdge_ConditionalNeedsWhen(t *testing.T) {
	h, _ := planGraphSetup(t)
	planID, a, b := twoTaskDraft(t, h)
	err := h.svc.AddPlanControlEdge(h.ctx, planID, pm.Dependency{FromTaskID: a, ToTaskID: b, Kind: pm.EdgeConditional}, "user:a")
	if !errors.Is(err, pm.ErrConditionalNeedsWhen) {
		t.Fatalf("conditional without when → %v, want ErrConditionalNeedsWhen", err)
	}
}

func TestAddPlanControlEdge_UnknownKind(t *testing.T) {
	h, _ := planGraphSetup(t)
	planID, a, b := twoTaskDraft(t, h)
	err := h.svc.AddPlanControlEdge(h.ctx, planID, pm.Dependency{FromTaskID: a, ToTaskID: b, Kind: pm.EdgeKind("weird")}, "user:a")
	if !errors.Is(err, pm.ErrInvalidEdgeKind) {
		t.Fatalf("unknown kind → %v, want ErrInvalidEdgeKind", err)
	}
}

func TestAddPlanControlEdge_LoopbackNeedsMaxRounds(t *testing.T) {
	h, _ := planGraphSetup(t)
	planID, a, b := twoTaskDraft(t, h)
	// b→a forward so a is an ancestor of b; loopback b→a with When but MaxRounds=0.
	if err := h.svc.AddPlanControlEdge(h.ctx, planID, pm.Dependency{FromTaskID: b, ToTaskID: a, Kind: pm.EdgeSeq}, "user:a"); err != nil {
		t.Fatalf("seed seq edge: %v", err)
	}
	err := h.svc.AddPlanControlEdge(h.ctx, planID, pm.Dependency{FromTaskID: b, ToTaskID: a, Kind: pm.EdgeLoopback, When: "reject"}, "user:a")
	if !errors.Is(err, pm.ErrInvalidLoopback) {
		t.Fatalf("loopback with MaxRounds=0 → %v, want ErrInvalidLoopback", err)
	}
}

func TestAddPlanControlEdge_PlainSeqStillWorks(t *testing.T) {
	h, _ := planGraphSetup(t)
	planID, a, b := twoTaskDraft(t, h)
	if err := h.svc.AddPlanControlEdge(h.ctx, planID, pm.Dependency{FromTaskID: a, ToTaskID: b}, "user:a"); err != nil {
		t.Fatalf("plain seq edge via AddPlanControlEdge: %v", err)
	}
}
