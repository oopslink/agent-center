package service

import (
	"context"
	"fmt"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// =============================================================================
// Control-flow engine runtime (v2.13.0 I18/B1 — docs/design/v2.13.0/
// control-flow-engine-spec.md). Two concerns live here:
//   - SetDecisionOutcome: record a decision node's outcome (routes its conditional/
//     loopback out-edges via ComputePlanView).
//   - applyLoopbacks: the §4 bounded-loopback driver — when a decision completes
//     with an outcome that fires a loopback edge, re-activate the loop target
//     subgraph (Reopen + clear dispatch/outcome) up to MaxRounds, then take the
//     escape branch (or notify the creator that the loop is stuck).
//
// Both are NO-OPs for a pure DAG plan (no decision outcomes, no loopback edges), so
// existing plans are unaffected (the back-compat anchor).
// =============================================================================

// SetDecisionOutcome records (latest-wins) a decision node's outcome. The actor must
// be a project member; the task must belong to a plan. Reuses the caller's tx if one
// is open (the complete_task handler records the outcome in the SAME tx as the
// complete, so the subsequent auto-advance sees it).
func (s *Service) SetDecisionOutcome(ctx context.Context, taskID pm.TaskID, outcome string, actor pm.IdentityRef) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		if t.PlanID() == "" {
			return fmt.Errorf("projectmanager: task %s is not in a plan — no decision outcome to record", taskID)
		}
		return s.plans.RecordDecisionOutcome(txCtx, t.PlanID(), taskID, outcome, now)
	})
}

// applyLoopbacks is the §4 bounded-loopback driver, invoked by the orchestrator when
// a task transitions (EvtTaskStateChanged). It is a no-op unless `changed` is a
// just-COMPLETED decision node (a node with loopback out-edges) whose recorded
// outcome matches a loopback edge's When. It runs INSIDE the projector's advance tx,
// BEFORE dispatchReadyNodes, so the reopened subgraph re-dispatches in the same pass.
func (s *Service) applyLoopbacks(txCtx context.Context, plan *pm.Plan, changed pm.TaskID) error {
	if changed == "" {
		return nil
	}
	t, err := s.tasks.FindByID(txCtx, changed)
	if err != nil {
		return err
	}
	if !pm.TaskIsDone(t.Status()) { // only a completed decision fires its loopbacks
		return nil
	}
	edges, err := s.plans.ListDependencies(txCtx, plan.ID())
	if err != nil {
		return err
	}
	var loopbacks []pm.Dependency
	for _, e := range edges {
		if e.IsLoopback() && e.FromTaskID == changed {
			loopbacks = append(loopbacks, e)
		}
	}
	if len(loopbacks) == 0 {
		return nil // not a loopback decision — nothing to drive.
	}
	outcomes, err := s.plans.ListDecisionOutcomes(txCtx, plan.ID())
	if err != nil {
		return err
	}
	outcome := ""
	for _, o := range outcomes {
		if o.TaskID == changed {
			outcome = o.Outcome
			break
		}
	}
	if outcome == "" {
		return nil // decision completed without a recorded outcome → nothing to route.
	}
	now := s.clock.Now()
	for _, lb := range loopbacks {
		if lb.When != outcome {
			continue // this loopback is for a different outcome.
		}
		round, gerr := s.plans.GetLoopRound(txCtx, plan.ID(), lb.FromTaskID, lb.ToTaskID)
		if gerr != nil {
			return gerr
		}
		if round < lb.MaxRounds {
			// Another round: bump the counter + re-activate the loop subgraph.
			if _, ierr := s.plans.IncrementLoopRound(txCtx, plan.ID(), lb.FromTaskID, lb.ToTaskID); ierr != nil {
				return ierr
			}
			if rerr := s.reopenLoopSubgraph(txCtx, plan.ID(), edges, lb.ToTaskID, lb.FromTaskID, now); rerr != nil {
				return rerr
			}
			continue
		}
		// Exhausted (round == MaxRounds): take the escape branch if the decision has a
		// conditional out-edge When==<outcome>_exhausted; else notify the creator (stuck).
		escape := outcome + "_exhausted"
		hasEscape := false
		for _, e := range edges {
			// The escape edge is a conditional with To=decision (From=Escape node),
			// per the condUp convention — match by ToTaskID, not FromTaskID. (T272 /
			// F-T272-1: the previous FromTaskID check never matched a real escape edge,
			// so exhaustion fell through to notifyLoopStuck instead of routing to Escape.)
			if e.ToTaskID == changed && pm.NormalizeEdgeKind(e.Kind) == pm.EdgeConditional && e.When == escape {
				hasEscape = true
				break
			}
		}
		if hasEscape {
			if rerr := s.plans.RecordDecisionOutcome(txCtx, plan.ID(), changed, escape, now); rerr != nil {
				return rerr
			}
			continue
		}
		if nerr := s.notifyLoopStuck(txCtx, plan, t); nerr != nil {
			return nerr
		}
	}
	return nil
}

// reopenLoopSubgraph re-activates every node on the forward path from `to` (the loop
// target) to `from` (the decision), inclusive (§4.2): a completed node is Reopened
// (Completed→Reopened, a non-terminal state ComputePlanView treats as re-dispatchable),
// its dispatch record is cleared (so it re-enters the ready-set), and its decision
// outcome is cleared (so a re-run decision re-decides). Non-completed nodes are left
// as-is (a still-running node keeps running).
func (s *Service) reopenLoopSubgraph(txCtx context.Context, planID pm.PlanID, edges []pm.Dependency, to, from pm.TaskID, now time.Time) error {
	for _, nodeID := range pm.LoopbackResetSet(edges, to, from) {
		nt, err := s.tasks.FindByID(txCtx, nodeID)
		if err != nil {
			return err
		}
		if pm.TaskIsDone(nt.Status()) { // Completed→Reopened (a re-dispatchable non-terminal state)
			if rerr := nt.Reopen(now); rerr != nil {
				return rerr
			}
			if uerr := s.tasks.Update(txCtx, nt); uerr != nil {
				return uerr
			}
		}
		// Clear the dispatch record so the reopened node re-enters the ready-set, and
		// the decision outcome so a re-run decision re-decides (harmless for non-decisions).
		if cerr := s.plans.ClearDispatch(txCtx, planID, nodeID); cerr != nil {
			return cerr
		}
		if cerr := s.plans.ClearDecisionOutcome(txCtx, planID, nodeID); cerr != nil {
			return cerr
		}
	}
	return nil
}

// notifyLoopStuck @mentions the plan creator that a loopback exhausted its rounds
// with no escape edge (the §4.1 fail-safe). Best-effort: requires a dispatcher.
func (s *Service) notifyLoopStuck(txCtx context.Context, plan *pm.Plan, decision *pm.Task) error {
	if s.planDispatcher == nil {
		return nil
	}
	content := fmt.Sprintf("decision %q exhausted its loopback rounds with no escape branch — the loop is stuck pending resolution.", decision.Title())
	_, err := s.planDispatcher.PostMention(txCtx, plan.ConversationID(), string(plan.CreatorRef()), content)
	return err
}
