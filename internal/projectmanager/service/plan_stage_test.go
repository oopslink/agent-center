package service

import (
	"errors"
	"strings"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

// dispatchedSet returns the set of task ids with a dispatch record for the plan.
func dispatchedSet(t *testing.T, h *planAdvanceHarness, planID pm.PlanID) map[pm.TaskID]bool {
	t.Helper()
	recs, err := h.plans.ListDispatchRecords(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	out := map[pm.TaskID]bool{}
	for _, r := range recs {
		out[r.TaskID] = true
	}
	return out
}

// seedTwoStagePlan builds: Stage A = {a1 → a2}, Stage B = {b1}, B depends_on A.
// Returns the task ids + stage ids. StartPlan is left to the caller.
func seedTwoStagePlan(t *testing.T, h *planAdvanceHarness, pid pm.ProjectID, planID pm.PlanID, maxRounds int) (a1, a2, b1 pm.TaskID, stageA, stageB pm.StageID) {
	t.Helper()
	stageA, err := h.svc.CreateStage(h.ctx, CreateStageCommand{PlanID: planID, Name: "A", MaxRounds: maxRounds, Actor: "user:a"})
	if err != nil {
		t.Fatalf("CreateStage A: %v", err)
	}
	stageB, err = h.svc.CreateStage(h.ctx, CreateStageCommand{PlanID: planID, Name: "B", DependsOnStages: []pm.StageID{stageA}, MaxRounds: maxRounds, Actor: "user:a"})
	if err != nil {
		t.Fatalf("CreateStage B: %v", err)
	}
	a1 = h.seedAssignedTask(t, pid, planID, "a1", "user:a1")
	a2 = h.seedAssignedTask(t, pid, planID, "a2", "user:a2")
	b1 = h.seedAssignedTask(t, pid, planID, "b1", "user:b1")
	for _, tk := range []pm.TaskID{a1, a2} {
		if err := h.svc.AssignTaskToStage(h.ctx, planID, tk, stageA, "user:a"); err != nil {
			t.Fatalf("AssignTaskToStage A: %v", err)
		}
	}
	if err := h.svc.AssignTaskToStage(h.ctx, planID, b1, stageB, "user:a"); err != nil {
		t.Fatalf("AssignTaskToStage B: %v", err)
	}
	// Intra-stage edge: a2 depends_on a1 (a1 is the stage entry).
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: a2, ToTaskID: a1, Kind: pm.EdgeSeq})
	return a1, a2, b1, stageA, stageB
}

func TestCreateStage_ProvisionsExecutableGateTask(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)

	spec := pm.DefaultHumanGateSpec("user:a")
	spec.RoleRef = "reviewer"
	spec.AcceptanceContract = "All integration tests pass and the reviewed SHA is on main."
	stageID, err := h.svc.CreateStage(ctx, CreateStageCommand{PlanID: planID, Name: "Acceptance", GateSpec: spec, Actor: "user:a"})
	if err != nil {
		t.Fatalf("CreateStage: %v", err)
	}
	detail, err := h.svc.GetStage(ctx, stageID)
	if err != nil {
		t.Fatalf("GetStage: %v", err)
	}
	if detail.Stage.GateTaskID() == "" {
		t.Fatal("gate_task_id empty")
	}
	gateTask, err := h.tasks.FindByID(ctx, detail.Stage.GateTaskID())
	if err != nil {
		t.Fatalf("gate task: %v", err)
	}
	if gateTask.PlanID() != planID || gateTask.StageID() != stageID {
		t.Fatalf("gate binding plan=%s stage=%s", gateTask.PlanID(), gateTask.StageID())
	}
	if gateTask.Assignee() != "user:a" || !gateTask.DispatchMode().RoutesInline() {
		t.Fatalf("gate execution assignee=%s mode=%s", gateTask.Assignee(), gateTask.DispatchMode())
	}
	if got := detail.Stage.GateSpec(); got.AcceptanceContract != spec.AcceptanceContract || got.RoleRef != "reviewer" {
		t.Fatalf("gate spec = %+v, want persisted contract and role", got)
	}
	if diagnostics, err := h.svc.CompileAndValidatePlan(ctx, planID, "user:a"); err != nil || len(diagnostics) != 0 {
		t.Fatalf("CompileAndValidatePlan diagnostics=%+v err=%v", diagnostics, err)
	}
}

func TestCreateStage_RejectsEmptyHumanGateContract(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	spec := pm.DefaultHumanGateSpec("user:a")
	spec.AcceptanceContract = ""
	if _, err := h.svc.CreateStage(ctx, CreateStageCommand{PlanID: planID, Name: "Acceptance", GateSpec: spec, Actor: "user:a"}); !errors.Is(err, pm.ErrMissingGateContract) {
		t.Fatalf("CreateStage error = %v, want ErrMissingGateContract", err)
	}
}

func TestCreateStage_RunningPausedFailClosed(t *testing.T) {
	h := planAdvanceSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)
	h.seedAssignedTask(t, pid, planID, "work", "user:a1")
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	if _, err := h.svc.CreateStage(ctx, CreateStageCommand{PlanID: planID, Name: "late", Actor: "user:a"}); !errors.Is(err, pm.ErrPlanNotPending) {
		t.Fatalf("CreateStage running err=%v, want ErrPlanNotPending", err)
	}
	if err := h.svc.PausePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("PausePlan: %v", err)
	}
	if _, err := h.svc.CreateStage(ctx, CreateStageCommand{PlanID: planID, Name: "paused", Actor: "user:a"}); !errors.Is(err, pm.ErrPlanNotPending) {
		t.Fatalf("CreateStage paused err=%v, want ErrPlanNotPending", err)
	}
}

func TestLegacyBareStageGate_RejectsThenReconciles(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "legacy", CreatedBy: "user:a"})
	h.drain(t)
	taskID := h.seedAssignedTask(t, pid, planID, "work", "user:a")
	stage, err := pm.NewStage(pm.NewStageInput{ID: "stage-legacy", PlanID: planID, Name: "Legacy", CreatedAt: h.clk.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.stages.Save(ctx, stage); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.AssignTaskToStage(ctx, planID, taskID, stage.ID(), "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.StartPlan(ctx, planID, "user:a"); !errors.Is(err, pm.ErrMissingGateEvaluator) {
		t.Fatalf("StartPlan = %v, want missing_gate_evaluator", err)
	}
	obs, err := h.plans.ListOpenProgressObligations(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if obligationsOfKind(obs, pm.ObligationBindGate) != 1 {
		t.Fatalf("missing gate obligations=%+v, want one bind_gate_evaluator", obs)
	}
	incs, err := h.plans.ListOpenProgressIncidents(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if incidentsOfKind(incs, pm.IncidentMissingGateEvaluator) != 1 {
		t.Fatalf("missing gate incidents=%+v, want one missing_gate_evaluator", incs)
	}
	if got := len(planOwnerBlockWakeEvents(t, h, planID)); got != 1 {
		t.Fatalf("missing gate owner wakes=%d, want 1", got)
	}
	if err := h.svc.ReconcileStageGates(ctx, planID); err != nil {
		t.Fatalf("ReconcileStageGates: %v", err)
	}
	if diagnostics, err := h.svc.CompileAndValidatePlan(ctx, planID, "user:a"); err != nil || len(diagnostics) != 0 {
		t.Fatalf("post-reconcile diagnostics=%+v err=%v", diagnostics, err)
	}
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan after reconcile: %v", err)
	}
}

// TestStage_Barrier_GatePass is the end-to-end for the stage barrier + gate PASS: a
// downstream stage's entry does NOT dispatch until every upstream-stage business node
// is done AND the upstream gate passes. Then a gate pass releases it.
func TestStage_Barrier_GatePass(t *testing.T) {
	h, orchSvc := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)
	a1, a2, b1, stageA, stageB := seedTwoStagePlan(t, h, pid, planID, 3)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	p, _ := h.plans.FindByID(ctx, planID)

	// --- 落图: two gate nodes, and stage A's gate stamped on the aggregate ---
	if got := stageGateNodeCountCtx(t, h, orchSvc, p.GraphID()); got != 2 {
		t.Fatalf("stage gate nodes = %d, want 2", got)
	}
	detA, _ := h.svc.GetStage(ctx, stageA)
	if detA.Stage.GateNodeID() == "" {
		t.Fatal("stage A gate_node_id not stamped by buildStages")
	}
	if detA.Status != pm.StageOpen {
		t.Fatalf("stage A status = %q, want open (nothing started)", detA.Status)
	}

	// --- dispatch #1: only a1 (stage A entry). a2 blocked on a1; b1 blocked on gate A ---
	d1, _ := h.svc.AdvancePlan(ctx, planID, "user:a")
	if len(d1) != 1 || d1[0] != a1 {
		t.Fatalf("dispatch #1 = %v, want [a1]", d1)
	}
	// b1 must NOT be dispatched — the stage barrier holds it behind gate A.
	if dispatchedSet(t, h, planID)[b1] {
		t.Fatal("b1 dispatched before stage A gate passed — barrier bypassed")
	}

	// --- complete a1 → a2 ready; complete a2 → stage A business all done ---
	h.setTaskStatus(t, a1, pm.TaskCompleted)
	d2, _ := h.svc.AdvancePlan(ctx, planID, "user:a")
	if len(d2) != 1 || d2[0] != a2 {
		t.Fatalf("dispatch #2 = %v, want [a2]", d2)
	}
	h.setTaskStatus(t, a2, pm.TaskCompleted)
	if d, _ := h.svc.AdvancePlan(ctx, planID, "user:a"); len(d) != 1 {
		t.Fatalf("dispatch after a2 done = %v, want the single stage evaluator", d)
	} else {
		det, _ := h.svc.GetStage(ctx, stageA)
		if d[0] != det.Stage.GateTaskID() {
			t.Fatalf("dispatch after a2 done = %v, want gate task %s", d, det.Stage.GateTaskID())
		}
	}
	if dispatchedSet(t, h, planID)[b1] {
		t.Fatal("b1 dispatched before gate resolution — barrier bypassed")
	}
	// stage A: all members done but gate pending → running (§4.1).
	detA, _ = h.svc.GetStage(ctx, stageA)
	if detA.Status != pm.StageRunning {
		t.Fatalf("stage A status = %q, want running (gate pending)", detA.Status)
	}

	// --- gate PASS releases stage B's entry b1 (barrier lifts) ---
	h.setTaskStatus(t, detA.Stage.GateTaskID(), pm.TaskCompleted)
	if err := h.svc.RecordDecisionOutcome(ctx, detA.Stage.GateTaskID(), "pass", "user:a"); err != nil {
		t.Fatalf("RecordDecisionOutcome pass: %v", err)
	}
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan after gate pass: %v", err)
	}
	if !dispatchedSet(t, h, planID)[b1] {
		t.Fatal("b1 not dispatched after gate A pass — barrier did not lift")
	}
	detA, _ = h.svc.GetStage(ctx, stageA)
	if detA.Status != pm.StageDone {
		t.Fatalf("stage A status = %q, want done (all members done + gate passed)", detA.Status)
	}
	// b1 is dispatched but not yet started → stage B projects open (§4.1 全未起).
	detB, _ := h.svc.GetStage(ctx, stageB)
	if detB.Status != pm.StageOpen {
		t.Fatalf("stage B status = %q, want open (b1 dispatched but not started)", detB.Status)
	}
	// Once b1 starts running, stage B projects running.
	h.setTaskStatus(t, b1, pm.TaskRunning)
	detB, _ = h.svc.GetStage(ctx, stageB)
	if detB.Status != pm.StageRunning {
		t.Fatalf("stage B status = %q, want running (b1 running)", detB.Status)
	}
}

// stageGateNodeCountCtx is the ctx-correct gate counter (the nil-ctx helper above is
// unused; kept minimal here).
func stageGateNodeCountCtx(t *testing.T, h *planAdvanceHarness, orchSvc *orch.Service, graphID string) int {
	t.Helper()
	nodes, err := orchSvc.ListNodes(h.ctx, orch.GraphID(graphID))
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	c := 0
	for _, n := range nodes {
		if _, ok := n.Metadata()["stage_gate"]; ok {
			c++
		}
	}
	return c
}

// TestStage_GateReject_ReopensStageSubgraph asserts a bounded gate reject reopens the
// stage's member tasks (its sub-DAG) and does NOT release the downstream stage.
func TestStage_GateReject_LegacyEntrypointIsRetired(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)
	a1, a2, b1, stageA, _ := seedTwoStagePlan(t, h, pid, planID, 3)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	// Drive stage A to all-done.
	h.svc.AdvancePlan(ctx, planID, "user:a")
	h.setTaskStatus(t, a1, pm.TaskCompleted)
	h.svc.AdvancePlan(ctx, planID, "user:a")
	h.setTaskStatus(t, a2, pm.TaskCompleted)
	h.svc.AdvancePlan(ctx, planID, "user:a")

	detA, _ := h.svc.GetStage(ctx, stageA)
	gate := detA.Stage.GateNodeID()

	// Reject must enter RecordStageGateVerdict, which appends a remediation Stage.
	// The old raw outcome entrypoint may never rewrite completed members.
	if err := h.svc.ResolveStageGate(ctx, gate, "reject", "user:a"); err != pm.ErrTaskReopenRetired {
		t.Fatalf("ResolveStageGate reject = %v want ErrTaskReopenRetired", err)
	}
	ta1, _ := h.tasks.FindByID(ctx, a1)
	ta2, _ := h.tasks.FindByID(ctx, a2)
	if ta1.Status() != pm.TaskCompleted || ta2.Status() != pm.TaskCompleted {
		t.Fatalf("legacy reject mutated history: a1=%s a2=%s", ta1.Status(), ta2.Status())
	}
	if dispatchedSet(t, h, planID)[b1] {
		t.Fatal("b1 dispatched after rejected legacy call")
	}
	detA, _ = h.svc.GetStage(ctx, stageA)
	if detA.Rounds != 0 {
		t.Fatalf("legacy reject changed stage rounds to %d", detA.Rounds)
	}
}

func TestStageGateReadiness_RequiresEveryMemberTerminal(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)
	a1, a2, _, stageA, _ := seedTwoStagePlan(t, h, pid, planID, 3)
	detail, err := h.svc.GetStage(ctx, stageA)
	if err != nil {
		t.Fatalf("GetStage: %v", err)
	}

	assertReady := func(want bool) {
		t.Helper()
		got, rerr := h.svc.stageGateReadiness(ctx, planID, mustListPlanTasks(t, h, planID))
		if rerr != nil {
			t.Fatalf("stageGateReadiness: %v", rerr)
		}
		if got[detail.Stage.GateTaskID()] != want {
			t.Fatalf("gate ready = %v, want %v", got[detail.Stage.GateTaskID()], want)
		}
	}

	assertReady(false)
	h.setTaskStatus(t, a1, pm.TaskCompleted)
	assertReady(false)
	h.setTaskStatus(t, a2, pm.TaskCompleted)
	assertReady(true)
}

func TestStageGateReadiness_ReloadsPersistedMembersFailClosed(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)
	a1, a2, _, stageA, _ := seedTwoStagePlan(t, h, pid, planID, 3)
	detail, err := h.svc.GetStage(ctx, stageA)
	if err != nil {
		t.Fatalf("GetStage: %v", err)
	}

	h.setTaskStatus(t, a1, pm.TaskCompleted)
	firstMemberOnly, err := h.tasks.FindByID(ctx, a1)
	if err != nil {
		t.Fatalf("FindByID(a1): %v", err)
	}
	ready, err := h.svc.stageGateReadiness(ctx, planID, []*pm.Task{firstMemberOnly})
	if err != nil {
		t.Fatalf("stageGateReadiness: %v", err)
	}
	if ready[detail.Stage.GateTaskID()] {
		t.Fatalf("gate ready with only first completed member supplied; persisted member %s is still open", a2)
	}

	h.setTaskStatus(t, a2, pm.TaskCompleted)
	ready, err = h.svc.stageGateReadiness(ctx, planID, []*pm.Task{firstMemberOnly})
	if err != nil {
		t.Fatalf("stageGateReadiness after all persisted members done: %v", err)
	}
	if !ready[detail.Stage.GateTaskID()] {
		t.Fatal("gate not ready after every persisted stage member completed")
	}
}

func TestEnsureTaskRunnable_StageGateRequiresEveryPersistedMemberTerminal(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{
		OrganizationID: "org-1", Name: "P", CreatedBy: "user:a",
	})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{
		ProjectID: pid, Name: "staged", CreatedBy: "user:a",
	})
	stageID, err := h.svc.CreateStage(ctx, CreateStageCommand{
		PlanID: planID, Name: "S1", MaxRounds: 2, Actor: "user:a",
	})
	if err != nil {
		t.Fatalf("CreateStage: %v", err)
	}
	member := h.seedAssignedTask(t, pid, planID, "member", "user:a")
	if err := h.svc.AssignTaskToStage(ctx, planID, member, stageID, "user:a"); err != nil {
		t.Fatalf("AssignTaskToStage: %v", err)
	}
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	detail, err := h.svc.GetStage(ctx, stageID)
	if err != nil {
		t.Fatalf("GetStage: %v", err)
	}
	gateTaskID := detail.Stage.GateTaskID()
	if gateTaskID == "" {
		t.Fatal("missing stage gate task")
	}

	if err := h.svc.EnsureTaskRunnable(ctx, gateTaskID); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("gate with open member: EnsureTaskRunnable = %v, want ErrTaskNotRunnable", err)
	}

	h.setTaskStatus(t, member, pm.TaskCompleted)
	if err := h.svc.EnsureTaskRunnable(ctx, gateTaskID); err != nil {
		t.Fatalf("gate after all members terminal: EnsureTaskRunnable = %v, want nil", err)
	}
}

func TestEnsureTaskRunnable_DownstreamStageGateRequiresUpstreamGatePass(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{
		OrganizationID: "org-1", Name: "P", CreatedBy: "user:a",
	})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{
		ProjectID: pid, Name: "staged", CreatedBy: "user:a",
	})
	stageA, err := h.svc.CreateStage(ctx, CreateStageCommand{
		PlanID: planID, Name: "A", MaxRounds: 2, Actor: "user:a",
	})
	if err != nil {
		t.Fatalf("CreateStage A: %v", err)
	}
	stageB, err := h.svc.CreateStage(ctx, CreateStageCommand{
		PlanID: planID, Name: "B", DependsOnStages: []pm.StageID{stageA},
		MaxRounds: 2, Actor: "user:a",
	})
	if err != nil {
		t.Fatalf("CreateStage B: %v", err)
	}
	memberA := h.seedAssignedTask(t, pid, planID, "member-a", "user:a")
	memberB := h.seedAssignedTask(t, pid, planID, "member-b", "user:a")
	if err := h.svc.AssignTaskToStage(ctx, planID, memberA, stageA, "user:a"); err != nil {
		t.Fatalf("AssignTaskToStage A: %v", err)
	}
	if err := h.svc.AssignTaskToStage(ctx, planID, memberB, stageB, "user:a"); err != nil {
		t.Fatalf("AssignTaskToStage B: %v", err)
	}
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	h.setTaskStatus(t, memberA, pm.TaskCompleted)
	h.setTaskStatus(t, memberB, pm.TaskCompleted)
	detailA, _ := h.svc.GetStage(ctx, stageA)
	detailB, _ := h.svc.GetStage(ctx, stageB)

	if err := h.svc.EnsureTaskRunnable(ctx, detailB.Stage.GateTaskID()); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("downstream gate before upstream pass: EnsureTaskRunnable = %v, want ErrTaskNotRunnable", err)
	}
	if err := h.svc.ResolveStageGate(ctx, detailA.Stage.GateNodeID(), "pass", "user:a"); err != nil {
		t.Fatalf("ResolveStageGate A pass: %v", err)
	}
	if err := h.svc.EnsureTaskRunnable(ctx, detailB.Stage.GateTaskID()); err != nil {
		t.Fatalf("downstream gate after upstream pass: EnsureTaskRunnable = %v, want nil", err)
	}
}

func TestGetPlanDetail_HidesStageGatesUntilReadinessPasses(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{
		OrganizationID: "org-1", Name: "P", CreatedBy: "user:a",
	})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{
		ProjectID: pid, Name: "staged", CreatedBy: "user:a",
	})
	stageID, err := h.svc.CreateStage(ctx, CreateStageCommand{
		PlanID: planID, Name: "S1", MaxRounds: 2, Actor: "user:a",
	})
	if err != nil {
		t.Fatalf("CreateStage: %v", err)
	}
	member := h.seedAssignedTask(t, pid, planID, "member", "user:a")
	if err := h.svc.AssignTaskToStage(ctx, planID, member, stageID, "user:a"); err != nil {
		t.Fatalf("AssignTaskToStage: %v", err)
	}
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	stage, _ := h.svc.GetStage(ctx, stageID)
	gateTaskID := stage.Stage.GateTaskID()

	detail, err := h.svc.GetPlanDetail(ctx, planID)
	if err != nil {
		t.Fatalf("GetPlanDetail: %v", err)
	}
	assertGateReadView(t, detail, gateTaskID, pm.NodeBlocked, false)

	h.setTaskStatus(t, member, pm.TaskCompleted)
	detail, err = h.svc.GetPlanDetail(ctx, planID)
	if err != nil {
		t.Fatalf("GetPlanDetail after member complete: %v", err)
	}
	assertGateReadView(t, detail, gateTaskID, pm.NodeReady, true)
}

func assertGateReadView(t *testing.T, detail *PlanDetail, gateTaskID pm.TaskID, wantStatus pm.NodeStatus, wantReady bool) {
	t.Helper()
	var gotStatus pm.NodeStatus
	for _, node := range detail.View.Nodes {
		if node.TaskID == gateTaskID {
			gotStatus = node.NodeStatus
			break
		}
	}
	if gotStatus != wantStatus {
		t.Fatalf("gate node status = %s, want %s", gotStatus, wantStatus)
	}
	gotReady := false
	for _, taskID := range detail.View.ReadySet {
		if taskID == gateTaskID {
			gotReady = true
			break
		}
	}
	if gotReady != wantReady {
		t.Fatalf("gate in ready_set = %v, want %v; ready_set=%v", gotReady, wantReady, detail.View.ReadySet)
	}
}

func TestStageGate_FirstMemberComplete_ReconcileDoesNotDispatch(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)
	a1, _, _, stageA, _ := seedTwoStagePlan(t, h, pid, planID, 3)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan initial: %v", err)
	}
	h.setTaskStatus(t, a1, pm.TaskCompleted)
	if _, err := h.svc.AdvancePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan after first member: %v", err)
	}
	if err := h.svc.ReconcileRunningPlans(ctx, nil); err != nil {
		t.Fatalf("ReconcileRunningPlans: %v", err)
	}
	detail, err := h.svc.GetStage(ctx, stageA)
	if err != nil {
		t.Fatalf("GetStage: %v", err)
	}
	if dispatchedSet(t, h, planID)[detail.Stage.GateTaskID()] {
		t.Fatal("gate dispatched while another stage member remained open")
	}
	gateTask, err := h.tasks.FindByID(ctx, detail.Stage.GateTaskID())
	if err != nil {
		t.Fatalf("FindByID gate task: %v", err)
	}
	if gateTask.DispatchMode() != pm.DispatchSupervisorInline {
		t.Fatalf("gate dispatch mode = %q, want supervisor_inline", gateTask.DispatchMode())
	}
}

func mustListPlanTasks(t *testing.T, h *planAdvanceHarness, planID pm.PlanID) []*pm.Task {
	t.Helper()
	tasks, err := h.tasks.ListByPlan(h.ctx, planID)
	if err != nil {
		t.Fatalf("ListByPlan: %v", err)
	}
	return tasks
}

// TestStage_GateReject_ExhaustEscalates asserts that once max_rounds is exhausted the
// gate is LEFT UNRESOLVED (downstream stays blocked — a closed barrier, §5).
func TestReopenExhaustedStage_IsRetired(t *testing.T) {
	h, _ := planGraphSetup(t)
	_, err := h.svc.ReopenExhaustedStage(h.ctx, ReopenExhaustedStageCommand{})
	if !errors.Is(err, pm.ErrTaskReopenRetired) {
		t.Fatalf("ReopenExhaustedStage = %v, want ErrTaskReopenRetired", err)
	}
}

// TestStage_ZeroRegression asserts a plan with NO stages builds a graph with NO stage
// gate nodes (§8: pm_stages empty + stage_id all empty = today's pure-node DAG).
func TestStage_ZeroRegression(t *testing.T) {
	h, orchSvc := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "nostages", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:b1")
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: b, ToTaskID: a, Kind: pm.EdgeSeq})
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	p, _ := h.plans.FindByID(ctx, planID)
	if got := stageGateNodeCountCtx(t, h, orchSvc, p.GraphID()); got != 0 {
		t.Fatalf("stage gate nodes = %d in a stageless plan, want 0 (§8 zero-regression)", got)
	}
}

// TestListStagesForPlan_SharesProjectionWithGetStage locks the T981 §7 web-read glue:
// ListStagesForPlan returns EVERY stage's projection, and each one is IDENTICAL to
// GetStage(stageID) — proving the single shared projStage path (pd constraint 1: no
// second copy of the derivation that could drift). Also covers the §8 no-stage empty.
func TestListStagesForPlan_SharesProjectionWithGetStage(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})

	// No-stage plan → ListStagesForPlan is empty (§8 backward compat).
	noStage, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "nostages", CreatedBy: "user:a"})
	h.drain(t)
	if dets, err := h.svc.ListStagesForPlan(ctx, noStage); err != nil || len(dets) != 0 {
		t.Fatalf("ListStagesForPlan(no-stage) = (%d stages, %v), want (0, nil)", len(dets), err)
	}

	// Two-stage plan, driven into a mixed state (stage A members done, gate pending →
	// running; stage B not started → open), so the projection has something to compare.
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)
	_, _, _, stageA, stageB := seedTwoStagePlan(t, h, pid, planID, 3)
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}

	dets, err := h.svc.ListStagesForPlan(ctx, planID)
	if err != nil {
		t.Fatalf("ListStagesForPlan: %v", err)
	}
	if len(dets) != 2 {
		t.Fatalf("ListStagesForPlan returned %d stages, want 2", len(dets))
	}
	byID := map[pm.StageID]*StageDetail{}
	for _, d := range dets {
		byID[d.Stage.ID()] = d
	}
	// Each listed stage must be byte-for-byte the same projection as GetStage — the
	// single-source guarantee.
	for _, sid := range []pm.StageID{stageA, stageB} {
		listed, ok := byID[sid]
		if !ok {
			t.Fatalf("ListStagesForPlan missing stage %s", sid)
		}
		got, err := h.svc.GetStage(ctx, sid)
		if err != nil {
			t.Fatalf("GetStage(%s): %v", sid, err)
		}
		if listed.Status != got.Status || listed.Rounds != got.Rounds || len(listed.Members) != len(got.Members) {
			t.Fatalf("stage %s: list projection {status=%q rounds=%d members=%d} != get {status=%q rounds=%d members=%d} — projection drift",
				sid, listed.Status, listed.Rounds, len(listed.Members), got.Status, got.Rounds, len(got.Members))
		}
		for i := range listed.Members {
			if listed.Members[i] != got.Members[i] {
				t.Fatalf("stage %s member[%d] drift: list=%+v get=%+v", sid, i, listed.Members[i], got.Members[i])
			}
		}
	}
}

// TestStage_CrossEdge_RejectedAtBuild asserts the §5 build-time invariant: a manual
// plan edge between two DIFFERENT stages is rejected at StartPlan (graph build).
func TestStage_CrossEdge_RejectedAtBuild(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)
	a1, _, b1, _, _ := seedTwoStagePlan(t, h, pid, planID, 3)
	// A hand-drawn cross-stage business edge b1 depends_on a1 (bypasses the gate barrier).
	mustAddDep(t, h, planID, pm.Dependency{PlanID: planID, FromTaskID: b1, ToTaskID: a1, Kind: pm.EdgeSeq})
	if err := h.svc.StartPlan(ctx, planID, "user:a"); !errors.Is(err, pm.ErrStageCrossEdge) {
		t.Fatalf("StartPlan with cross-stage edge = %v, want ErrStageCrossEdge", err)
	}
}

// TestStage_StagelessNode_RejectedAtBuild asserts the author-time invariant (§5,
// quick-fix 1a): once a plan has ≥1 stage, a business node with no stage_id is rejected
// at StartPlan (it would run ahead of the staged flow, bypassing the gate/barrier). The
// error names the orphan node. A fully-assigned staged plan is unaffected (covered by
// TestStage_Barrier_GatePass); a plan with NO stages is unaffected (TestStage_ZeroRegression).
func TestStage_StagelessNode_RejectedAtBuild(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "stages", CreatedBy: "user:a"})
	h.drain(t)
	seedTwoStagePlan(t, h, pid, planID, 3)
	// A business task selected into the staged plan but NEVER assigned to a stage — the
	// stageless run-ahead 1a closes.
	orphan := h.seedAssignedTask(t, pid, planID, "run-ahead", "user:x")
	err := h.svc.StartPlan(ctx, planID, "user:a")
	if !errors.Is(err, pm.ErrStageStagelessNode) {
		t.Fatalf("StartPlan with stageless business node = %v, want ErrStageStagelessNode", err)
	}
	if !strings.Contains(err.Error(), string(orphan)) {
		t.Fatalf("error %q does not name the orphan node %s", err, orphan)
	}
}
