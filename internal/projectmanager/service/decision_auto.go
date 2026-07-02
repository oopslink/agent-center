package service

import (
	"context"
	"fmt"
	"strings"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// =============================================================================
// B3 — decision automation, service layer.
//
// ComputeAutoDecision checks whether a just-completed node is a control-flow
// decision node. Currently (v2.28.0+) the gate evaluation has been removed
// (cycle-specific branch/base fields were removed from Task), so the gate is
// always GateUnknown and every decision defers to a human ruling.
//
// Everything here is a NO-OP for an ordinary task (IsDecision==false).
// =============================================================================

// AutoDecision is the result of ComputeAutoDecision: what (if anything) B3 derived
// for a node, plus the inputs that drove it (for the @mention / logs).
type AutoDecision struct {
	// IsDecision is true iff the node is a control-flow decision node (has a
	// conditional/loopback out-edge). False ⇒ B3 is a no-op for it (ordinary task).
	IsDecision bool
	// Decided is true iff B3 determined an outcome to record. False on a decision
	// node means "defer to a human" (NOT an error) — record nothing.
	Decided bool
	// Outcome is the label to record when Decided (pm.OutcomePass / pm.OutcomeReject).
	Outcome string
	// Gate is the verdict that drove the decision (for the deferral @mention / audit).
	Gate pm.GateVerdict
	// OpenComments is the count of unresolved review comments consulted (only on a
	// green gate AND only in the legacy fallback when no structured verdict exists).
	OpenComments int
	// Verdict is the structured review verdict B3 consulted on a green gate (T468),
	// when one was found (current OR stale round). nil ⇒ no verdict recorded (legacy
	// open-comment fallback path). Surfaced in the deferral @mention so the PD can read
	// the reviewer's verdict WITHOUT entering the Review conversation.
	Verdict *pm.ReviewVerdict
	// Reason is a short human-readable explanation (surfaced in the deferral @mention).
	Reason string
}

// ComputeAutoDecision evaluates the B3 auto-decision for a just-/about-to-be-
// completed node. It is read-only (no tx): it loads the node, confirms it is a
// decision node, evaluates the gate (via the DecisionGate port) and — only when the
// gate is green — the open-comment count, then applies pm.AutoDecideOutcome.
//
// It returns IsDecision==false (a silent no-op) for an ordinary task or a node not
// in a plan. For a decision node it ALWAYS returns Decided + Outcome per the rule;
// Decided==false means the caller should defer to a human (see NotifyDecisionDeferred)
// rather than record an outcome. An error is returned only for a genuine load
// failure (task/deps unreadable) — the caller treats that like "no auto outcome"
// (degrades to manual), never as a crash.
func (s *Service) ComputeAutoDecision(ctx context.Context, taskID pm.TaskID) (AutoDecision, error) {
	if s.plans == nil {
		return AutoDecision{}, nil // plans unavailable ⇒ no control-flow ⇒ no-op
	}
	t, err := s.tasks.FindByID(ctx, taskID)
	if err != nil {
		return AutoDecision{}, err
	}
	if t.PlanID() == "" {
		return AutoDecision{}, nil // not a plan node ⇒ no routing ⇒ no-op
	}
	edges, err := s.plans.ListDependencies(ctx, t.PlanID())
	if err != nil {
		return AutoDecision{}, err
	}
	if !pm.IsDecisionNode(edges, taskID) {
		return AutoDecision{}, nil // ordinary node ⇒ B3 does nothing
	}

	// Gate evaluation removed (v2.28.0): cycle-specific branch/base fields were
	// removed from Task; the gate always returns GateUnknown. When the orchestration
	// engine supplies gate evaluation via condition nodes, this function can be
	// simplified further or removed entirely.
	return AutoDecision{
		IsDecision: true, Decided: false, Gate: pm.GateUnknown,
		Reason: "gate verdict unavailable → human ruling required (record the outcome manually with complete_task outcome=…)",
	}, nil
}

// verdictState classifies the review-verdict lookup for a decision's current round.
type verdictState int

const (
	verdictNone    verdictState = iota // no structured verdict recorded on any upstream
	verdictStale                       // a verdict exists but for an EARLIER review round
	verdictCurrent                     // a verdict for THIS round exists (authoritative)
)

// currentRoundReviewVerdict reads the structured review verdict that drives B3 for a
// decision node (T468). It computes the decision's CURRENT loop round, then scans the
// decision's forward upstreams (the Review node(s)) for a recorded verdict:
//   - a verdict whose Round == current → verdictCurrent (authoritative; B3 routes by it);
//   - else a verdict from a different (earlier) round → verdictStale (B3 defers — never
//     auto-routes on a stale review);
//   - no verdict at all → verdictNone (B3 falls back to the legacy open-comment rule).
func (s *Service) currentRoundReviewVerdict(ctx context.Context, planID pm.PlanID, edges []pm.Dependency, decisionID pm.TaskID) (pm.ReviewVerdict, verdictState, error) {
	round, err := s.currentDecisionRound(ctx, planID, edges, decisionID)
	if err != nil {
		return pm.ReviewVerdict{}, verdictNone, err
	}
	var stale pm.ReviewVerdict
	haveStale := false
	for _, up := range pm.ForwardUpstreams(edges, decisionID) {
		vd, ok, gerr := s.plans.GetReviewVerdict(ctx, planID, up)
		if gerr != nil {
			return pm.ReviewVerdict{}, verdictNone, gerr
		}
		if !ok {
			continue
		}
		if vd.Round == round {
			return vd, verdictCurrent, nil
		}
		stale, haveStale = vd, true
	}
	if haveStale {
		return stale, verdictStale, nil
	}
	return pm.ReviewVerdict{}, verdictNone, nil
}

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

// NotifyDecisionDeferred @mentions the decision node's assignee (or, absent one, the
// plan creator) in the plan conversation that B3 could not auto-decide and a human
// must record the outcome. Best-effort (requires a dispatcher); returns nil when no
// dispatcher is wired. Called by the complete_task handler AFTER the completion tx
// commits, only when ComputeAutoDecision returned IsDecision && !Decided.
func (s *Service) NotifyDecisionDeferred(ctx context.Context, taskID pm.TaskID, ad AutoDecision) error {
	if s.planDispatcher == nil || !ad.IsDecision || ad.Decided {
		return nil
	}
	t, err := s.tasks.FindByID(ctx, taskID)
	if err != nil {
		return err
	}
	plan, err := s.GetPlan(ctx, t.PlanID())
	if err != nil {
		return err
	}
	target := string(t.Assignee())
	if target == "" {
		target = string(plan.CreatorRef())
	}
	content := fmt.Sprintf("decision node %q completed but B3 could not auto-decide its outcome: %s. Please rule manually — re-run complete_task with outcome=\"pass\" or outcome=\"reject\".", t.Title(), ad.Reason)
	// T468: surface the structured review verdict inline so the PD can rule WITHOUT
	// opening the Review conversation (no more not_agents_task dead-end).
	if ad.Verdict != nil {
		content += fmt.Sprintf(" Latest review verdict: %s (blocking=%t, round=%d)", ad.Verdict.Verdict, ad.Verdict.Blocking, ad.Verdict.Round)
		if r := strings.TrimSpace(ad.Verdict.Reason); r != "" {
			content += " — " + r
		}
	}
	_, err = s.planDispatcher.PostMention(ctx, plan.ConversationID(), target, content)
	return err
}
