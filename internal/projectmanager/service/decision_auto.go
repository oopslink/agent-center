package service

import (
	"context"
	"fmt"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// =============================================================================
// Decision / review-verdict service helpers.
//
// T810 ⑤: the B3 auto-decision (ComputeAutoDecision / AutoDecision / the
// verdict-lookup helpers) was DELETED — the gate was removed in v2.28.0 so B3 always
// deferred to a human, and the orchestration engine now owns decision routing. What
// remains is the review-verdict RECORDING (RecordReviewVerdict / ListReviewVerdicts),
// the decision-round helper it stamps with (currentDecisionRound), and the deferral
// @mention (NotifyDecisionDeferred) — all preserved unchanged in behaviour.
// =============================================================================

// currentDecisionRound returns the decision node's current loop round — the round its
// reviewer should record a verdict for. It is the LoopRound counter of the decision's
// loopback edge (decision→loop-target); a decision with no loopback edge is a
// single-round decision (round 0).
func (s *Service) currentDecisionRound(ctx context.Context, planID pm.PlanID, edges []pm.Dependency, decisionID pm.TaskID) (int, error) {
	target, ok := pm.LoopbackTargetOf(edges, decisionID)
	if !ok {
		return 0, nil
	}
	return s.plans.GetLoopRound(ctx, planID, decisionID, target)
}

// RecordReviewVerdict stores a Review node's structured verdict (T468), stamped with
// the downstream decision's CURRENT loop round so B3's round gate can reject a stale
// verdict. Single-slot latest-wins (each round overwrites). Reentrant in the caller's
// tx (the complete_task handler records the verdict in the SAME tx as the Review
// node's completion). The actor must be a project member; the task must be in a plan.
// A non-pass/reject verdict is rejected; a node not upstream of any decision still
// records (round 0) — harmless, B3 only reads verdicts on a decision's upstreams.
func (s *Service) RecordReviewVerdict(ctx context.Context, taskID pm.TaskID, verdict string, blocking bool, reason, sha string, actor pm.IdentityRef) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	if !pm.ValidReviewVerdict(verdict) {
		return fmt.Errorf("projectmanager: invalid review verdict %q (want %q or %q)", verdict, pm.ReviewPass, pm.ReviewReject)
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
		if err := s.requireProjectMutable(txCtx, t.ProjectID()); err != nil {
			return err
		}
		if t.PlanID() == "" {
			return fmt.Errorf("projectmanager: task %s is not in a plan — no review verdict to record", taskID)
		}
		edges, err := s.plans.ListDependencies(txCtx, t.PlanID())
		if err != nil {
			return err
		}
		round := 0
		if decisionID, ok := pm.DecisionForReview(edges, taskID); ok {
			round, err = s.currentDecisionRound(txCtx, t.PlanID(), edges, decisionID)
			if err != nil {
				return err
			}
		}
		return s.plans.RecordReviewVerdict(txCtx, t.PlanID(), pm.ReviewVerdict{
			PlanID: t.PlanID(), TaskID: taskID, Verdict: verdict, Blocking: blocking, Reason: reason, SHA: sha, Round: round,
		}, now)
	})
}

// ListReviewVerdicts returns a plan's recorded review verdicts (T468 — the PD read
// path: see the reviewers' structured verdicts WITHOUT entering each Review
// conversation). Project-member gated. A nil plans repo ⇒ empty.
func (s *Service) ListReviewVerdicts(ctx context.Context, planID pm.PlanID, actor pm.IdentityRef) ([]pm.ReviewVerdict, error) {
	if s.plans == nil {
		return nil, nil
	}
	plan, err := s.plans.FindByID(ctx, planID)
	if err != nil {
		return nil, err
	}
	if err := s.requireProjectMember(ctx, plan.ProjectID(), actor); err != nil {
		return nil, err
	}
	return s.plans.ListReviewVerdicts(ctx, planID)
}

// deferredDecisionReason is the fixed deferral explanation surfaced in the @mention.
// (Since the v2.28.0 gate removal, B3 always defers with this reason — previously
// carried on AutoDecision.Reason, now inlined here after T810 ⑤ deleted that struct.)
const deferredDecisionReason = "gate verdict unavailable → human ruling required (record the outcome manually with complete_task outcome=…)"

// NotifyDecisionDeferred @mentions the decision node's assignee (or, absent one, the
// plan creator) in the plan conversation that a human must record the outcome (the
// engine does not auto-decide — the gate was removed in v2.28.0). It is a NO-OP for an
// ordinary (non-decision) task or when no dispatcher is wired. Called by the
// complete_task handler AFTER the completion tx commits, when the completed node was a
// decision left without a manual outcome. T810 ⑤: it now determines "is a decision
// node" itself (pm.IsDecisionNode) — the deleted ComputeAutoDecision no longer supplies
// that — so the @mention behaviour is byte-for-byte unchanged.
func (s *Service) NotifyDecisionDeferred(ctx context.Context, taskID pm.TaskID) error {
	if s.planDispatcher == nil {
		return nil
	}
	t, err := s.tasks.FindByID(ctx, taskID)
	if err != nil {
		return err
	}
	if t.PlanID() == "" {
		return nil // not a plan node ⇒ no decision ⇒ no-op
	}
	edges, err := s.plans.ListDependencies(ctx, t.PlanID())
	if err != nil {
		return err
	}
	if !pm.IsDecisionNode(edges, taskID) {
		return nil // ordinary node ⇒ nothing to defer
	}
	plan, err := s.GetPlan(ctx, t.PlanID())
	if err != nil {
		return err
	}
	target := string(t.Assignee())
	if target == "" {
		target = string(plan.CreatorRef())
	}
	content := fmt.Sprintf("decision node %q completed but B3 could not auto-decide its outcome: %s. Please rule manually — re-run complete_task with outcome=\"pass\" or outcome=\"reject\".", t.Title(), deferredDecisionReason)
	_, err = s.planDispatcher.PostMention(ctx, plan.ConversationID(), target, content)
	return err
}
