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
//     subgraph (Reopen + clear dispatch/outcome) up to MaxRounds. On exhaustion it
//     records the terminal reject_exhausted outcome and either routes to a HISTORICAL
//     Escape vertex (legacy double-track) or escalates to the PD/creator (v2.23.0:
//     escape demoted from a graph vertex to a Decision terminal + escalation).
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
		// Exhausted (round == MaxRounds). Record the terminal reject_exhausted outcome
		// — kept as the value name for backward compat (v2.23.0 escape redesign A):
		// it is now the Decision's TERMINAL outcome semantics, no longer a routing
		// label to an Escape vertex. Recorded for BOTH tracks so plan-view learns the
		// Decision concluded non-pass (→ the feature's Integrate auto-skips).
		exhausted := outcome + "_exhausted"
		if rerr := s.plans.RecordDecisionOutcome(txCtx, plan.ID(), changed, exhausted, now); rerr != nil {
			return rerr
		}
		if hasLegacyEscapeEdge(edges, changed, exhausted) {
			// HISTORICAL plan (§5 / Q7 double-track): a pre-scaffolded Escape vertex
			// picks up this outcome via its conditional in-edge (When==…_exhausted) and
			// becomes ready for a human — that IS the surfacing, so do NOT also escalate
			// (mirrors the pre-v2.23 behaviour; T272 regression keeps this).
			continue
		}
		// NEW scaffold: escape is no longer a graph vertex. The feature's Integrate
		// in-edge (When=pass) no longer matches → it auto-skips (NodeSkipped); surface
		// the exhaustion to a human so they rule (reopen / re-decide / abandon).
		if eerr := s.escalateExhaustion(txCtx, plan, t); eerr != nil {
			return eerr
		}
	}
	return nil
}

// hasLegacyEscapeEdge reports whether the plan still carries a HISTORICAL Escape
// vertex's conditional in-edge for this decision (From=Escape, To=decision,
// When==<outcome>_exhausted, per the condUp convention — matched by ToTaskID). New
// scaffolds no longer emit it (v2.23.0 demoted escape to a Decision terminal), but
// the engine keeps this double-track so a plan minted before the cutover and still
// mid-flight routes exhaustion to its Escape node exactly as before (§5 / Q7).
func hasLegacyEscapeEdge(edges []pm.Dependency, decision pm.TaskID, exhaustedWhen string) bool {
	for _, e := range edges {
		if e.ToTaskID == decision && pm.NormalizeEdgeKind(e.Kind) == pm.EdgeConditional && e.When == exhaustedWhen {
			return true
		}
	}
	return false
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

// escalateExhaustion is the v2.23.0 escape-redesign (A) terminal handler: when a
// NEW-scaffold cycle's Decision loopback exhausts, escape is no longer a graph
// vertex, so the engine surfaces the exhaustion by @mentioning the Decision's owner
// (or, absent one, the plan creator) in the plan conversation — telling the human
// the feature will NOT auto-integrate and asking them to rule. It deliberately does
// NOT BlockTask anything: the Decision is already TaskCompleted (terminal) and the
// feature's Integrate is blocked, neither of which BlockTask (running-only) accepts
// (design Q6); and it leaves has_failed UNSET — an exhausted loop awaiting a human
// ruling is not a failure (Q1). Best-effort: a nil dispatcher is a no-op.
func (s *Service) escalateExhaustion(txCtx context.Context, plan *pm.Plan, decision *pm.Task) error {
	if s.planDispatcher == nil {
		return nil
	}
	target := string(decision.Assignee())
	if target == "" {
		target = string(plan.CreatorRef())
	}
	content := fmt.Sprintf("decision %q exhausted its review-reject loopback rounds (escalated). The feature will NOT auto-integrate — please rule: reopen for another round, re-decide the outcome, or abandon the feature.", decision.Title())
	_, err := s.planDispatcher.PostMention(txCtx, plan.ConversationID(), target, content)
	return err
}
