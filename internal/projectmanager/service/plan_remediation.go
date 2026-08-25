package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

// RecordStageGateVerdictCommand closes one immutable stage generation. Reject
// carries (or deterministically synthesizes) the next incremental stage; it never
// edits or reopens the completed generation.
type RecordStageGateVerdictCommand struct {
	GateTaskID     pm.TaskID
	Outcome        pm.GateVerdictOutcome
	Evidence       string
	ReviewedSHA    string
	IdempotencyKey string
	Actor          pm.IdentityRef
	Proposal       *pm.RemediationProposalPayload
}

type RecordStageGateVerdictResult struct {
	Verdict      pm.GateVerdict
	Continuation *pm.PlanContinuation
	Proposal     *pm.RemediationProposal
	StageID      pm.StageID
	Duplicate    bool
}

func (s *Service) RecordStageGateVerdict(ctx context.Context, cmd RecordStageGateVerdictCommand) (RecordStageGateVerdictResult, error) {
	var result RecordStageGateVerdictResult
	if s.remediation == nil || s.stages == nil || s.orch == nil || s.plans == nil {
		return result, pm.ErrRemediationUnavailable
	}
	if err := cmd.Actor.Validate(); err != nil {
		return result, err
	}
	cmd.Evidence = strings.TrimSpace(cmd.Evidence)
	cmd.ReviewedSHA = strings.TrimSpace(cmd.ReviewedSHA)
	cmd.IdempotencyKey = strings.TrimSpace(cmd.IdempotencyKey)
	if cmd.IdempotencyKey == "" {
		return result, errors.New("projectmanager: verdict idempotency_key required")
	}

	err := s.runInTx(ctx, func(txCtx context.Context) error {
		var replay bool
		var replayProposal *pm.RemediationProposal
		gateTask, err := s.tasks.FindByID(txCtx, cmd.GateTaskID)
		if err != nil {
			return err
		}
		stage, ok, err := s.StageForGateTask(txCtx, cmd.GateTaskID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrNotStageGate
		}
		plan, err := s.plans.FindByID(txCtx, stage.PlanID())
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, plan.ProjectID(), cmd.Actor); err != nil {
			return err
		}
		if plan.Status() != pm.PlanRunning && plan.Status() != pm.PlanPaused {
			return pm.ErrPlanNotRunning
		}
		if gateTask.Status() != pm.TaskCompleted {
			return pm.ErrIllegalTransition
		}

		if prior, found, ferr := s.remediation.FindVerdictByKey(txCtx, cmd.IdempotencyKey); ferr != nil {
			return ferr
		} else if found {
			if !sameVerdictCommand(prior, cmd) {
				return pm.ErrIdempotencyConflict
			}
			result.Verdict, result.Duplicate, replay = prior, true, true
			continuation, found, cerr := s.findContinuationForVerdict(txCtx, prior)
			if cerr != nil {
				return cerr
			}
			if found {
				result.Continuation = continuation
				result.StageID = continuation.CurrentStageID
			}
			if proposal, found, perr := s.remediation.FindProposalByKey(txCtx, cmd.IdempotencyKey+":proposal"); perr != nil {
				return perr
			} else if found {
				if cmd.Proposal != nil {
					compiled, diagnostics := pm.CompileRemediationProposal(*cmd.Proposal, stage.GateSpec().AcceptanceContract, prior.Evidence)
					if len(diagnostics) != 0 || MarshalRemediationPayload(compiled) != MarshalRemediationPayload(proposal.Payload) {
						return pm.ErrIdempotencyConflict
					}
				}
				replayProposal = &proposal
				result.Proposal = replayProposal
				if proposal.Status == "committed" || result.Continuation == nil || result.Continuation.Status != pm.ContinuationAwaitingRemediation {
					return nil
				}
			}
			if prior.Outcome == pm.GateVerdictPass || result.Continuation == nil ||
				result.Continuation.Status != pm.ContinuationAwaitingRemediation || plan.Status() == pm.PlanPaused {
				if prior.Outcome == pm.GateVerdictPass {
					if _, cerr := s.completePlanIfEligible(txCtx, plan); cerr != nil {
						return cerr
					}
				}
				return nil
			}
		}
		if !replay {
			_, found, ferr := s.remediation.FindVerdictByGate(txCtx, cmd.GateTaskID)
			if ferr != nil {
				return ferr
			}
			if found {
				return pm.ErrGateAlreadyVerdicted
			}
		}

		now := s.clock.Now()
		verdict := result.Verdict
		if !replay {
			verdict, err = pm.NewGateVerdict(pm.GateVerdict{
				ID: pm.GateVerdictID(s.idgen.NewEntityID("verdict")), ProjectID: plan.ProjectID(),
				PlanID: plan.ID(), StageID: stage.ID(), GateTaskID: cmd.GateTaskID,
				Outcome: cmd.Outcome, Evidence: cmd.Evidence, ReviewedSHA: cmd.ReviewedSHA,
				ActorRef: cmd.Actor, IdempotencyKey: cmd.IdempotencyKey, CreatedAt: now,
			})
			if err != nil {
				return err
			}
			if err := s.remediation.SaveVerdict(txCtx, verdict); err != nil {
				return err
			}
			result.Verdict = verdict
			// Compatibility read model only: the immutable verdict above is authoritative.
			if err := s.plans.RecordDecisionOutcome(txCtx, plan.ID(), cmd.GateTaskID, string(cmd.Outcome), now); err != nil {
				return err
			}
			if err := s.emit(txCtx, EvtStageGateVerdictRecorded,
				refsJSON(map[string]string{"plan_id": string(plan.ID()), "stage_id": string(stage.ID()), "verdict_id": string(verdict.ID)}), verdict); err != nil {
				return err
			}
		}

		gateNode, err := s.orch.GetNode(txCtx, orch.NodeID(stage.GateNodeID()))
		if err != nil {
			return err
		}
		edges, err := s.orch.ListEdges(txCtx, orch.GraphID(plan.GraphID()))
		if err != nil {
			return err
		}
		downstream := outgoingNodeIDs(edges, gateNode.ID())
		boundary := pm.ContinuationBoundaryFingerprint(plan.ID(), stage.ID(), stringNodeIDs(downstream), plan.Version())

		continuation, found := result.Continuation, result.Continuation != nil
		if !replay {
			continuation, found, err = s.remediation.FindOpenContinuationByStage(txCtx, stage.ID())
			if err != nil {
				return err
			}
		}
		if cmd.Outcome == pm.GateVerdictPass {
			if err := s.orch.ResolveCondition(txCtx, gateNode.ID(), "success"); err != nil {
				return err
			}
			if found {
				expected := continuation.Version
				if err := continuation.Close(verdict.ID, now); err != nil {
					return err
				}
				if updated, err := s.remediation.UpdateContinuation(txCtx, continuation, expected); err != nil {
					return err
				} else if !updated {
					return pm.ErrPlanVersionConflict
				}
				result.Continuation = continuation
			}
			if _, cerr := s.completePlanIfEligible(txCtx, plan); cerr != nil {
				return cerr
			}
			return nil
		}
		// A reject may be recorded while the Plan is paused. Resume changes only
		// the Plan control version, not this DAG boundary. Before compiling the
		// first proposal, rebase the awaiting continuation to the resumed version;
		// a committed/pending proposal is never silently rebased.
		if replay && continuation != nil && replayProposal == nil &&
			continuation.Status == pm.ContinuationAwaitingRemediation &&
			continuation.BoundaryFingerprint != boundary {
			expected := continuation.Version
			if err := continuation.RefreshAwaitingBoundary(boundary, now); err != nil {
				return err
			}
			if updated, err := s.remediation.UpdateContinuation(txCtx, continuation, expected); err != nil {
				return err
			} else if !updated {
				return pm.ErrPlanVersionConflict
			}
		}

		if replay {
			// The first attempt already advanced the continuation to
			// awaiting_remediation. Resume/retry must not consume another generation.
		} else if found {
			expected := continuation.Version
			if err := continuation.AwaitNext(verdict, boundary, now); err != nil {
				return err
			}
			if updated, err := s.remediation.UpdateContinuation(txCtx, continuation, expected); err != nil {
				return err
			} else if !updated {
				return pm.ErrPlanVersionConflict
			}
		} else {
			continuation, err = pm.NewPlanContinuation(pm.ContinuationID(s.idgen.NewEntityID("continuation")), verdict, boundary, stage.MaxRounds(), now)
			if err != nil {
				return err
			}
			if err := s.remediation.SaveContinuation(txCtx, continuation); err != nil {
				return err
			}
		}
		result.Continuation = continuation
		if plan.Status() == pm.PlanPaused {
			return nil
		}
		if continuation.RemainingBudget <= 0 {
			continuation.Status = pm.ContinuationBudgetExhausted
			expected := continuation.Version
			continuation.Version++
			continuation.UpdatedAt = now.UTC()
			if updated, err := s.remediation.UpdateContinuation(txCtx, continuation, expected); err != nil {
				return err
			} else if !updated {
				return pm.ErrPlanVersionConflict
			}
			return nil
		}

		var proposal pm.RemediationProposal
		if replayProposal != nil {
			proposal = *replayProposal
		} else {
			payload := defaultRemediationProposal(stage, gateTask, verdict, cmd.Actor)
			if cmd.Proposal != nil {
				payload = *cmd.Proposal
			}
			payload, diagnostics := pm.CompileRemediationProposal(payload, stage.GateSpec().AcceptanceContract, verdict.Evidence)
			if len(diagnostics) != 0 {
				return fmt.Errorf("%w: %s", pm.ErrRemediationProposalInvalid, strings.Join(diagnostics, ","))
			}
			proposal = pm.RemediationProposal{
				ID: pm.RemediationProposalID(s.idgen.NewEntityID("proposal")), ProjectID: plan.ProjectID(),
				PlanID: plan.ID(), ContinuationID: continuation.ID, TriggerVerdictID: verdict.ID,
				IdempotencyKey: cmd.IdempotencyKey + ":proposal", BasedOnPlanVersion: plan.Version(),
				BoundaryFingerprint: boundary, Payload: payload, Status: "ready", CreatedBy: cmd.Actor, CreatedAt: now,
			}
			if err := s.remediation.SaveProposal(txCtx, proposal); err != nil {
				return err
			}
		}
		stageID, err := s.appendRemediationStage(txCtx, plan, stage, gateNode, downstream, verdict, continuation, &proposal, cmd.Actor, now)
		if err != nil {
			return err
		}
		result.StageID, result.Proposal = stageID, &proposal
		return nil
	})
	return result, err
}

func sameVerdictCommand(v pm.GateVerdict, cmd RecordStageGateVerdictCommand) bool {
	return v.GateTaskID == cmd.GateTaskID && v.Outcome == cmd.Outcome &&
		v.Evidence == strings.TrimSpace(cmd.Evidence) && v.ReviewedSHA == strings.TrimSpace(cmd.ReviewedSHA) && v.ActorRef == cmd.Actor
}

func (s *Service) findContinuationForVerdict(ctx context.Context, verdict pm.GateVerdict) (*pm.PlanContinuation, bool, error) {
	continuations, err := s.remediation.ListContinuationsByPlan(ctx, verdict.PlanID)
	if err != nil {
		return nil, false, err
	}
	for _, continuation := range continuations {
		if continuation.TriggerVerdictID == verdict.ID || continuation.RootStageID == verdict.StageID {
			return continuation, true, nil
		}
	}
	return nil, false, nil
}

func defaultRemediationProposal(stage *pm.Stage, gateTask *pm.Task, verdict pm.GateVerdict, actor pm.IdentityRef) pm.RemediationProposalPayload {
	assignee := actor
	if gateTask.Assignee() != "" {
		assignee = gateTask.Assignee()
	}
	base := strings.TrimSpace(stage.GateSpec().AcceptanceContract)
	contract := base + "\n\nReject evidence to address:\n" + verdict.Evidence
	return pm.RemediationProposalPayload{
		Name: "Remediation: " + stage.Name(), Rationale: verdict.Evidence,
		Tasks: []pm.RemediationTaskSpec{{Ref: "fix", Title: "Address rejection for " + stage.Name(), Description: verdict.Evidence, AssigneeRef: assignee, DispatchMode: pm.DispatchExecutorFork, DeliveryContract: pm.DeliveryCodeChange, FollowsTaskID: gateTask.ID()}},
		Gate:  pm.RemediationGateSpec{AssigneeRef: actor, AcceptanceContract: contract},
	}
}

func outgoingNodeIDs(edges []orch.Edge, from orch.NodeID) []orch.NodeID {
	var out []orch.NodeID
	for _, edge := range edges {
		if edge.FromNodeID == from {
			out = append(out, edge.ToNodeID)
		}
	}
	return out
}

func stringNodeIDs(ids []orch.NodeID) []string {
	out := make([]string, len(ids))
	for i := range ids {
		out[i] = string(ids[i])
	}
	return out
}

func (s *Service) appendRemediationStage(
	ctx context.Context, plan *pm.Plan, priorStage *pm.Stage, priorGate *orch.Node, downstream []orch.NodeID,
	verdict pm.GateVerdict, continuation *pm.PlanContinuation, proposal *pm.RemediationProposal,
	actor pm.IdentityRef, now time.Time,
) (pm.StageID, error) {
	if plan.Version() != proposal.BasedOnPlanVersion {
		return "", pm.ErrRemediationProposalStale
	}
	currentBoundary := pm.ContinuationBoundaryFingerprint(plan.ID(), priorStage.ID(), stringNodeIDs(downstream), plan.Version())
	if currentBoundary != proposal.BoundaryFingerprint || currentBoundary != continuation.BoundaryFingerprint {
		return "", pm.ErrRemediationProposalStale
	}

	stageID := pm.StageID(s.idgen.NewEntityID("stage"))
	taskOf := make(map[string]pm.TaskID, len(proposal.Payload.Tasks))
	nodeOf := make(map[string]orch.NodeID, len(proposal.Payload.Tasks))
	graphID := orch.GraphID(plan.GraphID())
	for _, spec := range proposal.Payload.Tasks {
		taskID, err := s.CreateTask(ctx, CreateTaskCommand{
			ProjectID: plan.ProjectID(), Title: spec.Title, Description: spec.Description,
			CreatedBy: actor, Assignee: spec.AssigneeRef, DispatchMode: spec.DispatchMode, DeliveryContract: spec.DeliveryContract,
			FollowsTaskID: spec.FollowsTaskID, OriginVerdictID: verdict.ID,
		})
		if err != nil {
			return "", err
		}
		task, err := s.tasks.FindByID(ctx, taskID)
		if err != nil {
			return "", err
		}
		if err := task.SetPlan(plan.ID(), now); err != nil {
			return "", err
		}
		if err := task.SetStage(stageID, now); err != nil {
			return "", err
		}
		nodeID, err := s.orch.AddNode(ctx, graphID, string(orch.NodeCategoryBusiness), "", task.Title(), map[string]any{
			"task_id": string(taskID), "stage_id": string(stageID), "origin_verdict_id": string(verdict.ID), "continuation_id": string(continuation.ID), "generation": continuation.Generation + 1,
		})
		if err != nil {
			return "", err
		}
		task.SetNodeID(string(nodeID), now)
		if err := s.tasks.Update(ctx, task); err != nil {
			return "", err
		}
		taskOf[spec.Ref], nodeOf[spec.Ref] = taskID, nodeID
	}

	gateSpec := pm.GateSpec{
		EvaluatorKind: pm.GateEvaluatorHuman, AssigneeRef: proposal.Payload.Gate.AssigneeRef,
		AcceptanceContract: proposal.Payload.Gate.AcceptanceContract,
		PassRoute:          "downstream", RejectRoute: "append_remediation", ExhaustedRoute: "escalate",
	}
	if err := gateSpec.Validate(); err != nil {
		return "", err
	}
	gateTaskID, err := s.CreateTask(ctx, CreateTaskCommand{
		ProjectID: plan.ProjectID(), Title: "Gate: " + proposal.Payload.Name,
		Description: "Evaluate the incremental remediation against its acceptance contract.",
		CreatedBy:   actor, Assignee: gateSpec.AssigneeRef, DispatchMode: pm.DispatchSupervisorInline,
		FollowsTaskID: priorStage.GateTaskID(), OriginVerdictID: verdict.ID,
	})
	if err != nil {
		return "", err
	}
	gateTask, err := s.tasks.FindByID(ctx, gateTaskID)
	if err != nil {
		return "", err
	}
	if err := gateTask.SetPlan(plan.ID(), now); err != nil {
		return "", err
	}
	if err := gateTask.SetStage(stageID, now); err != nil {
		return "", err
	}
	gateTaskNode, err := s.orch.AddNode(ctx, graphID, string(orch.NodeCategoryBusiness), "", gateTask.Title(), map[string]any{
		"task_id": string(gateTaskID), "stage_id": string(stageID), "stage_gate_evaluator": true,
		"origin_verdict_id": string(verdict.ID), "continuation_id": string(continuation.ID), "generation": continuation.Generation + 1,
	})
	if err != nil {
		return "", err
	}
	gateTask.SetNodeID(string(gateTaskNode), now)
	if err := s.tasks.Update(ctx, gateTask); err != nil {
		return "", err
	}

	indegree := make(map[string]int, len(taskOf))
	outdegree := make(map[string]int, len(taskOf))
	for ref := range taskOf {
		indegree[ref], outdegree[ref] = 0, 0
	}
	for _, edge := range proposal.Payload.Edges {
		if err := s.orch.AddEdge(ctx, graphID, nodeOf[edge.From], nodeOf[edge.To]); err != nil {
			return "", err
		}
		if err := s.plans.AddDependency(ctx, pm.Dependency{PlanID: plan.ID(), FromTaskID: taskOf[edge.To], ToTaskID: taskOf[edge.From]}); err != nil {
			return "", err
		}
		indegree[edge.To]++
		outdegree[edge.From]++
	}
	for ref, degree := range indegree {
		if degree == 0 {
			if err := s.orch.AddEdge(ctx, graphID, priorGate.ID(), nodeOf[ref]); err != nil {
				return "", err
			}
		}
	}
	for ref, degree := range outdegree {
		if degree == 0 {
			if err := s.orch.AddEdge(ctx, graphID, nodeOf[ref], gateTaskNode); err != nil {
				return "", err
			}
		}
	}
	conditionID, err := s.orch.AddNode(ctx, graphID, string(orch.NodeCategoryControl), string(orch.ControlKindCondition), "gate:"+proposal.Payload.Name, map[string]any{
		"evaluator": string(orch.EvaluatorManual), "stage_gate": string(stageID), "condition_for": string(gateTaskID), "pass_whens": []any{"pass"},
		"origin_verdict_id": string(verdict.ID), "continuation_id": string(continuation.ID), "generation": continuation.Generation + 1,
	})
	if err != nil {
		return "", err
	}
	if err := s.orch.AddEdge(ctx, graphID, gateTaskNode, conditionID); err != nil {
		return "", err
	}
	for _, target := range downstream {
		if err := s.orch.RemoveEdge(ctx, graphID, priorGate.ID(), target); err != nil {
			return "", err
		}
		if err := s.orch.AddEdge(ctx, graphID, conditionID, target); err != nil {
			return "", err
		}
	}

	nextVersion := plan.Version() + 1
	nextBoundary := pm.ContinuationBoundaryFingerprint(plan.ID(), stageID, stringNodeIDs(downstream), nextVersion)
	stage, err := pm.NewStage(pm.NewStageInput{
		ID: stageID, PlanID: plan.ID(), Name: proposal.Payload.Name, DependsOnStages: []pm.StageID{priorStage.ID()},
		MaxRounds: priorStage.MaxRounds(), GateTaskID: gateTaskID, GateSpec: gateSpec,
		OriginVerdictID: verdict.ID, ContinuationID: continuation.ID, Generation: continuation.Generation + 1,
		AcceptanceContract: gateSpec.AcceptanceContract, TopologyFingerprint: nextBoundary, CreatedAt: now,
	})
	if err != nil {
		return "", err
	}
	stage.SetGateNodeID(string(conditionID), now)
	if err := s.stages.Save(ctx, stage); err != nil {
		return "", err
	}
	if err := s.orch.ResolveCondition(ctx, priorGate.ID(), "success"); err != nil {
		return "", err
	}
	expectedContinuationVersion := continuation.Version
	if err := continuation.AttachStage(stageID, proposal.ID, nextBoundary, now); err != nil {
		return "", err
	}
	if updated, err := s.remediation.UpdateContinuation(ctx, continuation, expectedContinuationVersion); err != nil {
		return "", err
	} else if !updated {
		return "", pm.ErrPlanVersionConflict
	}
	plan.SetVersion(nextVersion, now)
	if err := s.plans.Update(ctx, plan); err != nil {
		return "", err
	}
	proposal.Status, proposal.CommittedAt = "committed", now.UTC()
	if err := s.remediation.UpdateProposalStatus(ctx, proposal.ID, proposal.Status, nil, proposal.CommittedAt); err != nil {
		return "", err
	}
	s.auditPlanByID(ctx, plan.ProjectID(), plan.ID(), pm.AuditPlanNodeAdded, actor, map[string]any{
		"stage_id": string(stageID), "origin_verdict_id": string(verdict.ID), "continuation_id": string(continuation.ID), "generation": continuation.Generation,
	})
	if err := s.emit(ctx, EvtRemediationStageAppended,
		refsJSON(map[string]string{"plan_id": string(plan.ID()), "stage_id": string(stageID), "verdict_id": string(verdict.ID), "continuation_id": string(continuation.ID)}),
		map[string]any{"plan_id": plan.ID(), "stage_id": stageID, "origin_verdict_id": verdict.ID, "continuation_id": continuation.ID, "generation": continuation.Generation}); err != nil {
		return "", err
	}
	return stageID, nil
}

// MarshalRemediationPayload supplies a stable representation for API/tests that
// need to compare a proposal without depending on struct pointer identity.
func MarshalRemediationPayload(payload pm.RemediationProposalPayload) string {
	b, _ := json.Marshal(payload)
	return string(b)
}
