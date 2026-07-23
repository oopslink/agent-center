package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

// =============================================================================
// Plan Stage AppServices (2026-07-03 plan-stage-model design §6). The authoring
// surface (create_stage / add_task_to_plan(stage)), the read projection (get_stage,
// §4.1 status is DERIVED), and the gate driver (ResolveStageGate, §5 — pass/reject/
// exhaust). Stage does NOT re-implement execution: the driver reuses the engine's
// condition machinery (ResolveCondition / ApplyConditionResult / countReopens).
// =============================================================================

// ErrStagesUnavailable is returned by the Stage AppServices when no StageRepository is
// wired (s.stages == nil) — fail-loud, mirroring ErrPlansUnavailable.
var ErrStagesUnavailable = errors.New("projectmanager: stage repository unavailable — Stage operations are not wired")

type PlanDiagnostic struct {
	NodeID  string `json:"node_id,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func (s *Service) CompileAndValidatePlan(ctx context.Context, planID pm.PlanID, actor pm.IdentityRef) ([]PlanDiagnostic, error) {
	p, err := s.plans.FindByID(ctx, planID)
	if err != nil {
		return nil, err
	}
	if err := s.requireProjectMember(ctx, p.ProjectID(), actor); err != nil {
		return nil, err
	}
	tasks, err := s.tasks.ListByPlan(ctx, planID)
	if err != nil {
		return nil, err
	}
	return s.validateStageGateSpecs(ctx, p, tasks), nil
}

func (s *Service) validateStageGateSpecs(ctx context.Context, p *pm.Plan, tasks []*pm.Task) []PlanDiagnostic {
	if s.stages == nil {
		return nil
	}
	stages, err := s.stages.ListByPlan(ctx, p.ID())
	if err != nil {
		return []PlanDiagnostic{{Code: "stage_read_failed", Message: err.Error()}}
	}
	byID := make(map[pm.TaskID]*pm.Task, len(tasks))
	for _, task := range tasks {
		byID[task.ID()] = task
	}
	var diagnostics []PlanDiagnostic
	for _, stage := range stages {
		spec := stage.GateSpec()
		gateTask := byID[stage.GateTaskID()]
		if stage.GateTaskID() == "" || gateTask == nil {
			diagnostics = append(diagnostics, PlanDiagnostic{
				NodeID: string(stage.ID()), Code: "missing_gate_evaluator",
				Message: "stage gate has no executable evaluator task",
				Hint:    "bind a supervisor_inline Decision task before starting the plan",
			})
			continue
		}
		if spec.EvaluatorKind != pm.GateEvaluatorHuman {
			diagnostics = append(diagnostics, PlanDiagnostic{
				NodeID: string(stage.ID()), Code: "unsupported_gate_evaluator",
				Message: "only a bound human evaluator is currently executable",
			})
		}
		if spec.AssigneeRef == "" && strings.TrimSpace(spec.RoleRef) == "" {
			diagnostics = append(diagnostics, PlanDiagnostic{
				NodeID: string(stage.ID()), Code: "missing_gate_assignee",
				Message: "human gate requires assignee_ref or role_ref",
			})
		}
		if gateTask.PlanID() != p.ID() || gateTask.StageID() != stage.ID() {
			diagnostics = append(diagnostics, PlanDiagnostic{
				NodeID: string(stage.ID()), Code: "invalid_gate_binding",
				Message: "gate evaluator task must belong to the same plan and stage",
			})
		}
		if !gateTask.DispatchMode().RoutesInline() {
			diagnostics = append(diagnostics, PlanDiagnostic{
				NodeID: string(stage.ID()), Code: "invalid_gate_dispatch_mode",
				Message: "human gate evaluator must use supervisor_inline dispatch",
			})
		}
		if spec.AssigneeRef != "" && gateTask.Assignee() != spec.AssigneeRef {
			diagnostics = append(diagnostics, PlanDiagnostic{
				NodeID: string(stage.ID()), Code: "gate_assignee_mismatch",
				Message: "gate task assignee does not match GateSpec",
			})
		}
	}
	return diagnostics
}

func (s *Service) StageForGateTask(ctx context.Context, taskID pm.TaskID) (*pm.Stage, bool, error) {
	task, err := s.tasks.FindByID(ctx, taskID)
	if err != nil {
		return nil, false, err
	}
	if task.PlanID() == "" || s.stages == nil {
		return nil, false, nil
	}
	stages, err := s.stages.ListByPlan(ctx, task.PlanID())
	if err != nil {
		return nil, false, err
	}
	for _, stage := range stages {
		if stage.GateTaskID() == taskID {
			return stage, true, nil
		}
	}
	return nil, false, nil
}

// ErrNotStageGate is returned by ResolveStageGate when the target node is not a stage
// gate condition node (no stage_gate metadata) — a caller pointed at the wrong node.
var ErrNotStageGate = errors.New("projectmanager: node is not a stage gate")

// CreateStageCommand authors a Stage in a draft Plan (design §6).
type CreateStageCommand struct {
	PlanID          pm.PlanID
	Name            string
	DependsOnStages []pm.StageID
	MaxRounds       int // 0 ⇒ pm.DefaultStageMaxRounds
	Actor           pm.IdentityRef
}

// CreateStage authors a new Stage for a DRAFT plan (§6 create_stage). Guards: the
// actor must be a project member; the plan must be in draft (stages/DAG are editable
// only in draft, §9.4); every depends_on target must already exist in THIS plan and
// the resulting outer stage DAG must stay acyclic (ValidateStageDAG). Returns the new
// StageID.
func (s *Service) CreateStage(ctx context.Context, cmd CreateStageCommand) (pm.StageID, error) {
	if s.stages == nil {
		return "", ErrStagesUnavailable
	}
	if s.plans == nil {
		return "", ErrPlansUnavailable
	}
	now := s.clock.Now()
	stageID := pm.StageID(s.idgen.NewEntityID("stage"))
	err := s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.plans.FindByID(txCtx, cmd.PlanID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, p.ProjectID(), cmd.Actor); err != nil {
			return err
		}
		if err := s.requireProjectMutable(txCtx, p.ProjectID()); err != nil {
			return err
		}
		if !p.IsBuiltin() && p.Status() != pm.PlanDraft {
			return pm.ErrPlanNotDraft
		}
		st, nerr := pm.NewStage(pm.NewStageInput{
			ID: stageID, PlanID: cmd.PlanID, Name: cmd.Name,
			DependsOnStages: cmd.DependsOnStages, MaxRounds: cmd.MaxRounds, CreatedAt: now,
		})
		if nerr != nil {
			return nerr
		}
		// Validate the outer stage DAG WITH the new stage folded in: every depends_on
		// target must be an existing sibling stage of this plan, and the graph must stay
		// acyclic (§4.2). Loading the current stages + appending the new one is the whole
		// stage set the invariant runs over.
		existing, lerr := s.stages.ListByPlan(txCtx, cmd.PlanID)
		if lerr != nil {
			return lerr
		}
		if verr := pm.ValidateStageDAG(append(existing, st)); verr != nil {
			return verr
		}
		if serr := s.stages.Save(txCtx, st); serr != nil {
			return serr
		}
		gateTaskID, terr := s.provisionStageGateTask(txCtx, p, st, cmd.Actor, now)
		if terr != nil {
			return terr
		}
		s.auditPlan(txCtx, p, pm.AuditPlanNodeAdded, cmd.Actor, map[string]any{
			"stage_id": string(stageID), "stage_name": cmd.Name, "gate_task_id": string(gateTaskID),
		})
		return nil
	})
	if err != nil {
		return "", err
	}
	return stageID, nil
}

func (s *Service) provisionStageGateTask(txCtx context.Context, p *pm.Plan, st *pm.Stage, actor pm.IdentityRef, now time.Time) (pm.TaskID, error) {
	gateSpec := pm.DefaultHumanGateSpec(actor)
	gateTaskID, err := s.CreateTask(txCtx, CreateTaskCommand{
		ProjectID:    p.ProjectID(),
		Title:        "Gate: " + st.Name(),
		Description:  "Evaluate the stage acceptance contract and complete with outcome=pass or outcome=reject.",
		CreatedBy:    actor,
		Assignee:     actor,
		DispatchMode: pm.DispatchSupervisorInline,
	})
	if err != nil {
		return "", err
	}
	gateTask, err := s.tasks.FindByID(txCtx, gateTaskID)
	if err != nil {
		return "", err
	}
	if err := gateTask.SetPlan(p.ID(), now); err != nil {
		return "", err
	}
	if err := gateTask.SetStage(st.ID(), now); err != nil {
		return "", err
	}
	if err := s.tasks.Update(txCtx, gateTask); err != nil {
		return "", err
	}
	st.SetGateTask(gateTaskID, gateSpec, now)
	if err := s.stages.Update(txCtx, st); err != nil {
		return "", err
	}
	return gateTaskID, nil
}

// ReconcileStageGates is the idempotent recovery path for legacy bare gates.
// It provisions the missing evaluator task and, for an already-running graph,
// rewires the existing condition through that task without passing the gate.
func (s *Service) ReconcileStageGates(ctx context.Context, planID pm.PlanID) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.plans.FindByID(txCtx, planID)
		if err != nil {
			return err
		}
		stages, err := s.stages.ListByPlan(txCtx, planID)
		if err != nil {
			return err
		}
		for _, stage := range stages {
			if stage.GateTaskID() != "" {
				continue
			}
			gateTaskID, err := s.provisionStageGateTask(txCtx, p, stage, p.CreatorRef(), now)
			if err != nil {
				return err
			}
			if p.GraphID() == "" || stage.GateNodeID() == "" || s.orch == nil {
				continue
			}
			graphID := orch.GraphID(p.GraphID())
			gateID := orch.NodeID(stage.GateNodeID())
			nodeID, err := s.orch.AddNode(txCtx, graphID, string(orch.NodeCategoryBusiness), "",
				"Gate: "+stage.Name(), map[string]any{
					"task_id": string(gateTaskID), "stage_id": string(stage.ID()), "stage_gate_evaluator": true,
				})
			if err != nil {
				return err
			}
			gateTask, err := s.tasks.FindByID(txCtx, gateTaskID)
			if err != nil {
				return err
			}
			gateTask.SetNodeID(string(nodeID), now)
			if err := s.tasks.Update(txCtx, gateTask); err != nil {
				return err
			}
			edges, err := s.orch.ListEdges(txCtx, graphID)
			if err != nil {
				return err
			}
			for _, edge := range edges {
				if edge.ToNodeID != gateID {
					continue
				}
				if err := s.orch.RemoveEdge(txCtx, graphID, edge.FromNodeID, gateID); err != nil {
					return err
				}
				if err := s.orch.AddEdge(txCtx, graphID, edge.FromNodeID, nodeID); err != nil {
					return err
				}
			}
			if err := s.orch.AddEdge(txCtx, graphID, nodeID, gateID); err != nil {
				return err
			}
			gate, err := s.orch.GetNode(txCtx, gateID)
			if err != nil {
				return err
			}
			meta := gate.Metadata()
			meta["condition_for"] = string(gateTaskID)
			meta["pass_whens"] = []any{"pass"}
			if err := s.orch.UpdateNode(txCtx, gateID, gate.Title(), meta); err != nil {
				return err
			}
		}
		return nil
	})
}

// AssignTaskToStage sets (or, with stageID=="", clears) a task's Stage membership in a
// DRAFT plan (§6 — the add_task_to_plan `stage` parameter's write). The task must
// already be in the plan; a non-empty stage must belong to the SAME plan. Draft-only,
// project-member-gated, a pure metadata edit.
func (s *Service) AssignTaskToStage(ctx context.Context, planID pm.PlanID, taskID pm.TaskID, stageID pm.StageID, actor pm.IdentityRef) error {
	if s.stages == nil {
		return ErrStagesUnavailable
	}
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.plans.FindByID(txCtx, planID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, p.ProjectID(), actor); err != nil {
			return err
		}
		if err := s.requireProjectMutable(txCtx, p.ProjectID()); err != nil {
			return err
		}
		if !p.IsBuiltin() && p.Status() != pm.PlanDraft {
			return pm.ErrPlanNotDraft
		}
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if t.PlanID() != planID {
			return pm.ErrTaskInOtherPlan
		}
		if stageID != "" {
			st, serr := s.stages.FindByID(txCtx, stageID)
			if serr != nil {
				return serr
			}
			if st.PlanID() != planID {
				return pm.ErrStageProjectMismatch
			}
		}
		if err := t.SetStage(stageID, now); err != nil {
			return err
		}
		return s.tasks.Update(txCtx, t)
	})
}

// StageMemberView is one member node of a stage in the get_stage projection: the bound
// task's identity + its live task status (the node status is lock-step with it, §9.2).
type StageMemberView struct {
	TaskID     pm.TaskID
	Title      string
	TaskStatus pm.TaskStatus
}

// StageDetail is the get_stage read model (§4.1/§7): the Stage aggregate + its member
// nodes + the DERIVED status projection + the current bounded-retry round.
type StageDetail struct {
	Stage   *pm.Stage
	Members []StageMemberView
	Status  pm.StageStatus
	// Rounds is the number of completed gate-reject reopen rounds (the stage-local
	// bounded-retry counter, = countReopens on the gate node). 0 before any reject.
	Rounds int
}

// GetStage returns the DERIVED read model for a stage (§4.1 — status is a projection,
// never stored; §7 monitoring). Member states come from the member TASKS' statuses (the
// graph nodes are kept lock-step, §9.2); the gate state + retry round come from the
// gate CONDITION node when the plan has been graphed.
func (s *Service) GetStage(ctx context.Context, stageID pm.StageID) (*StageDetail, error) {
	if s.stages == nil {
		return nil, ErrStagesUnavailable
	}
	st, err := s.stages.FindByID(ctx, stageID)
	if err != nil {
		return nil, err
	}
	planTasks, err := s.tasks.ListByPlan(ctx, st.PlanID())
	if err != nil {
		return nil, err
	}
	return s.projectStage(ctx, st, planTasks), nil
}

// ListStagesForPlan returns the DERIVED read model for EVERY stage of a plan (§7 stage-
// level monitoring: the detail page lists a plan's stages + their projected status /
// rounds / members). It shares the SINGLE projection path with GetStage via projectStage
// — never a second copy of the status/rounds/members derivation, so a single stage and
// the list can never drift. Returns an empty slice for a plan with no stages (the FE then
// renders the legacy no-stage view — §8 backward compat). The plan's tasks are listed
// ONCE and grouped, rather than re-listed per stage.
func (s *Service) ListStagesForPlan(ctx context.Context, planID pm.PlanID) ([]*StageDetail, error) {
	if s.stages == nil {
		return nil, ErrStagesUnavailable
	}
	stages, err := s.stages.ListByPlan(ctx, planID)
	if err != nil {
		return nil, err
	}
	planTasks, err := s.tasks.ListByPlan(ctx, planID)
	if err != nil {
		return nil, err
	}
	out := make([]*StageDetail, 0, len(stages))
	for _, st := range stages {
		out = append(out, s.projectStage(ctx, st, planTasks))
	}
	return out, nil
}

// projectStage is the SINGLE SOURCE of the stage read-model projection (§4.1) shared by
// GetStage and ListStagesForPlan (pd constraint: never duplicate the derivation). It
// derives one stage's members + status + retry round from the plan's already-listed
// tasks: member states come from the member TASKS' statuses (the graph nodes are kept
// lock-step, §9.2); the gate state + round come from the gate CONDITION node.
func (s *Service) projectStage(ctx context.Context, st *pm.Stage, planTasks []*pm.Task) *StageDetail {
	var views []StageMemberView
	var memberStates []pm.StageMemberState
	for _, t := range planTasks {
		if t.StageID() != st.ID() {
			continue
		}
		views = append(views, StageMemberView{TaskID: t.ID(), Title: t.Title(), TaskStatus: t.Status()})
		memberStates = append(memberStates, taskToStageMemberState(t.Status()))
	}
	gateState, rounds := s.stageGateState(ctx, st)
	return &StageDetail{
		Stage:   st,
		Members: views,
		Status:  pm.ProjectStageStatus(memberStates, gateState),
		Rounds:  rounds,
	}
}

// taskToStageMemberState maps a task status onto the coarse member state the stage
// projection consumes (§4.1): terminal (completed/discarded) → done, running or
// parked (blocked) → running, otherwise (open/reopened) → open.
//
// A parked member is `running`, NOT `done` and NOT `open`. Not open either — the
// `default` arm used to swallow it there, and `open` reads as
// un-started/startable, which a parked member is not. `running` = "in flight, not
// settled", which is the truthful coarse answer for both.
func taskToStageMemberState(status pm.TaskStatus) pm.StageMemberState {
	switch {
	case pm.TaskIsDone(status), pm.TaskIsFailed(status):
		return pm.StageMemberDone
	case status == pm.TaskRunning, status.IsParked():
		return pm.StageMemberRunning
	default:
		return pm.StageMemberOpen
	}
}

// stageGateState reads the gate CONDITION node's resolution state + reopen round for a
// stage (§4.1). Returns (StageGateNone, 0) when the stage has no gate node yet (no
// acceptance gate, or the plan has not started so the gate node does not exist).
func (s *Service) stageGateState(ctx context.Context, st *pm.Stage) (pm.StageGateState, int) {
	if s.orch == nil || st.GateNodeID() == "" {
		return pm.StageGateNone, 0
	}
	n, err := s.orch.GetNode(ctx, orch.NodeID(st.GateNodeID()))
	if err != nil || n == nil {
		return pm.StageGateNone, 0
	}
	rounds := conditionReopenCount(n)
	switch n.Status() {
	case orch.NodeCompleted, orch.NodeDiscarded:
		return pm.StageGatePassed, rounds
	case orch.NodeReopen:
		return pm.StageGateReopened, rounds
	default: // open / running — created, awaiting acceptance
		return pm.StageGatePending, rounds
	}
}

// ResolveStageGate is the Stage gate DRIVER (§5). It resolves a stage's exit gate,
// REUSING the engine's condition machinery (Stage does NOT rewrite execution):
//
//   - PASS: ResolveCondition("success") completes the gate → the engine's ReadyNodes
//     releases the downstream stages' entry nodes (the barrier lifts).
//   - REJECT within bounds (round ≤ stage.max_rounds): ResolveCondition("reject") drives
//     the engine's bounded reopen (ApplyConditionResult reopens this stage's sub-DAG via
//     the gate's on_failure chain, bumping countReopens); reopenStageSubgraph then
//     MIRRORS that onto the member TASKS so the reopened sub-DAG re-dispatches. It never
//     touches an upstream stage (§2 决策5 — no cross-stage rollback).
//   - REJECT exhausted (round > stage.max_rounds): the gate is LEFT UNRESOLVED (so the
//     downstream stages stay blocked — a closed barrier, §5) and the exhaustion is
//     escalated to a human (@mention the plan creator). It does NOT settle the gate,
//     which would wrongly release the downstream — mirroring the decision driver's
//     exhaustion boundary.
//
// After a pass or a bounded reject it advances the plan (best-effort — only when a
// dispatcher is wired) so the released / reopened nodes dispatch in the same call.
func (s *Service) ResolveStageGate(ctx context.Context, gateNodeID string, result string, actor pm.IdentityRef) error {
	if s.stages == nil || s.orch == nil {
		return ErrStagesUnavailable
	}
	gate, err := s.orch.GetNode(ctx, orch.NodeID(gateNodeID))
	if err != nil {
		return err
	}
	stageID, _ := gate.Metadata()["stage_gate"].(string)
	if stageID == "" {
		return ErrNotStageGate
	}
	st, err := s.stages.FindByID(ctx, pm.StageID(stageID))
	if err != nil {
		return err
	}
	p, err := s.plans.FindByID(ctx, st.PlanID())
	if err != nil {
		return err
	}
	if err := s.requireProjectMember(ctx, p.ProjectID(), actor); err != nil {
		return err
	}
	pass := result == "pass" || result == "success"

	if pass {
		if rerr := s.runInTx(ctx, func(txCtx context.Context) error {
			if cerr := s.orch.ResolveCondition(txCtx, orch.NodeID(gateNodeID), "success"); cerr != nil {
				return cerr
			}
			s.auditPlanByID(txCtx, p.ProjectID(), p.ID(), pm.AuditPlanDecisionOutcome, actor, map[string]any{
				"stage_id": stageID, "gate": "pass",
			})
			return nil
		}); rerr != nil {
			return rerr
		}
		return s.advanceAfterStageGate(ctx, p.ID(), actor)
	}

	// REJECT: enforce the stage-local bounded-retry cap on the engine's own round.
	round := conditionReopenCount(gate) + 1
	maxRounds := st.MaxRounds()
	if maxRounds <= 0 {
		maxRounds = pm.DefaultStageMaxRounds
	}
	if round > maxRounds {
		// Exhausted (§5 卡死升级): leave the gate UNRESOLVED so the downstream barrier
		// stays closed, record it, and escalate to a human. Best-effort escalation.
		return s.runInTx(ctx, func(txCtx context.Context) error {
			s.auditPlanByID(txCtx, p.ProjectID(), p.ID(), pm.AuditPlanDecisionOutcome, pm.SystemActor("plan-engine"), map[string]any{
				"stage_id": stageID, "gate": "reject", "round": round, "exhausted": true,
			})
			return s.escalateStageExhaustion(txCtx, p, st)
		})
	}

	if rerr := s.runInTx(ctx, func(txCtx context.Context) error {
		// Engine reopen: reopens the gate's on_failure chain (this stage's sub-DAG) +
		// the gate, bumping countReopens. Joins THIS tx (orch shares the reentrant RunInTx).
		if cerr := s.orch.ResolveCondition(txCtx, orch.NodeID(gateNodeID), "reject"); cerr != nil {
			return cerr
		}
		if merr := s.reopenStageSubgraph(txCtx, p, st, gate); merr != nil {
			return merr
		}
		s.auditPlanByID(txCtx, p.ProjectID(), p.ID(), pm.AuditPlanLoopback, pm.SystemActor("plan-engine"), map[string]any{
			"stage_id": stageID, "gate": "reject", "round": round,
		})
		return nil
	}); rerr != nil {
		return rerr
	}
	return s.advanceAfterStageGate(ctx, p.ID(), actor)
}

// reopenStageSubgraph mirrors the engine's node reopen (ApplyConditionResult on a gate
// reject) onto the member TASKS: for every business node the engine reopened (the gate's
// on_failure chain), it reopens the bound task (Completed→Reopened) and clears its
// dispatch record so it re-enters the ready-set. This is the node↔task mapping the
// (task-keyed) graph dispatch needs — the stage analogue of reopenLoopSubgraph.
func (s *Service) reopenStageSubgraph(txCtx context.Context, p *pm.Plan, st *pm.Stage, gate *orch.Node) error {
	g, err := s.orch.GetGraph(txCtx, orch.GraphID(p.GraphID()))
	if err != nil {
		return err
	}
	// The reopened node set = ReopenChain(gate → each on_failure target) over the graph's
	// edges (the stage's sub-DAG). Dedup across targets.
	toReopen := map[orch.NodeID]bool{}
	for _, target := range gateOnFailureTargets(gate) {
		for _, nid := range orch.ReopenChain(g.Edges(), gate.ID(), target) {
			toReopen[nid] = true
		}
	}
	now := s.clock.Now()
	for nid := range toReopen {
		n := g.FindNode(nid)
		if n == nil {
			continue
		}
		taskID := nodeTaskID(n)
		if taskID == "" {
			continue
		}
		t, ferr := s.tasks.FindByID(txCtx, taskID)
		if ferr != nil {
			return ferr
		}
		if pm.TaskIsDone(t.Status()) {
			prev := t.Status()
			if rerr := t.Reopen(now); rerr != nil {
				return rerr
			}
			if uerr := s.tasks.Update(txCtx, t); uerr != nil {
				return uerr
			}
			s.auditTaskStatusChange(txCtx, t, prev, pm.SystemActor("plan-engine"))
		}
		if cerr := s.plans.ClearDispatch(txCtx, p.ID(), taskID); cerr != nil {
			return cerr
		}
	}
	return nil
}

// gateOnFailureTargets reads a gate node's on_failure reopen targets (the stage entry
// node ids stamped by buildStages) from its metadata.
func gateOnFailureTargets(gate *orch.Node) []orch.NodeID {
	raw, ok := gate.Metadata()["on_failure"].([]any)
	if !ok {
		return nil
	}
	var out []orch.NodeID
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, orch.NodeID(s))
		}
	}
	return out
}

// escalateStageExhaustion surfaces a bounded-retry exhaustion (§5): the stage's gate has
// been rejected max_rounds times, so it @mentions the plan creator (a closed stage
// barrier blocks the whole downstream plan — a human must rule: re-run, re-decide, or
// abandon). Best-effort: a nil dispatcher is a no-op.
func (s *Service) escalateStageExhaustion(txCtx context.Context, plan *pm.Plan, st *pm.Stage) error {
	if s.planDispatcher == nil {
		return nil
	}
	target := string(plan.CreatorRef())
	content := fmt.Sprintf("stage %q exhausted its acceptance-gate retry rounds (escalated). The plan will NOT advance past this stage — please rule: re-run the stage, re-review, or abandon.", st.Name())
	_, err := s.planDispatcher.PostMention(txCtx, plan.ConversationID(), target, content)
	return err
}

// advanceAfterStageGate dispatches the nodes released/reopened by a gate resolution. It
// is best-effort: with no dispatcher wired (a test harness), it is a no-op rather than a
// fail — the reconcile sweep will pick the plan up. Runs in its OWN tx (AdvancePlan owns
// the dispatch @mention side-effects).
func (s *Service) advanceAfterStageGate(ctx context.Context, planID pm.PlanID, actor pm.IdentityRef) error {
	if s.planDispatcher == nil {
		return nil
	}
	_, err := s.AdvancePlan(ctx, planID, actor)
	return err
}
