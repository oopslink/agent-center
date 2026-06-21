package service

import (
	"context"
	"fmt"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// =============================================================================
// B3 — decision automation, service layer (v2.13.0 I18/B3 — docs/design/v2.13.0/
// control-flow-engine-spec.md §2.3 / §10).
//
// ComputeAutoDecision gathers the two inputs of the B3 rule for a control-flow
// decision node — the §-1 gate verdict (via the DecisionGate port) and the count of
// open review comments — and applies pm.AutoDecideOutcome. It performs READS + gate
// I/O ONLY (no writes, no tx), so the complete_task handler can call it BEFORE the
// completion tx (the gate is git/CI I/O — slow, must run outside the tx, exactly
// like the F3 guardIntegrateMerge pre-check) and then record the resulting outcome
// INSIDE the same tx as the complete (so the subsequent auto-advance routes the
// decision's conditional/loopback edges in the same pass).
//
// Everything here is a NO-OP for an ordinary task (IsDecision==false) and degrades
// to "human ruling" when the gate cannot be determined — so a plan without decision
// nodes, or a deployment with no DecisionGate wired, behaves exactly as B1 (manual
// outcome only). The back-compat anchor.
// =============================================================================

// DecisionGate reports the §-1 gate verdict (build/lint/tsc/test —
// cycle-node-graph-spec.md §2.3) for a feature branch. It is the runtime input to
// B3's auto-decision. OPTIONAL / nil-safe: when nil, every gate is GateUnknown, so
// B3 records no outcome and defers every decision to a human (pre-B3 behaviour). It
// is a CONSUMER-owned port (mirroring MergeChecker): the concrete adapter lives in
// internal/projectmanager/gatecheck and is wired at composition, so the pm BC never
// shells out itself.
type DecisionGate interface {
	// GateStatus returns the §-1 gate verdict for <branch> (checked against <base>)
	// in the repo at repoURL. It returns GateUnknown + a non-nil error when the gate
	// could not be run/determined (the caller maps that to a human ruling — it never
	// auto-passes or auto-rejects on a gate it could not actually evaluate).
	GateStatus(ctx context.Context, repoURL, branch, base string) (pm.GateVerdict, error)
}

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
	// green gate; 0 otherwise).
	OpenComments int
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

	gate := s.evaluateGate(ctx, t)
	switch gate {
	case pm.GateRed:
		// Unambiguous failure: reject (B1's bounded loopback re-runs Dev). Comments
		// are irrelevant — a red gate rejects regardless.
		return AutoDecision{
			IsDecision: true, Decided: true, Outcome: pm.OutcomeReject, Gate: gate,
			Reason: "§-1 gate failed (red) → auto-reject (re-run Dev)",
		}, nil
	case pm.GateGreen:
		// Green: auto-pass ONLY if there is not a single open review comment;
		// otherwise a human weighs the objection (no-false-pass invariant).
		open, cerr := s.countOpenReviewComments(ctx, t.PlanID(), taskID)
		if cerr != nil {
			// Could not confirm "no open comments" → must not auto-pass; defer.
			return AutoDecision{
				IsDecision: true, Decided: false, Gate: gate,
				Reason: "§-1 gate green but review comments could not be read → human ruling required",
			}, nil
		}
		outcome, decided := pm.AutoDecideOutcome(gate, open)
		reason := "§-1 gate green, no open review comments → auto-pass"
		if !decided {
			reason = fmt.Sprintf("§-1 gate green but %d open review comment(s) → human ruling required", open)
		}
		return AutoDecision{
			IsDecision: true, Decided: decided, Outcome: outcome, Gate: gate, OpenComments: open,
			Reason: reason,
		}, nil
	default: // GateUnknown
		return AutoDecision{
			IsDecision: true, Decided: false, Gate: gate,
			Reason: "§-1 gate verdict unavailable → human ruling required (record the outcome manually with complete_task outcome=…)",
		}, nil
	}
}

// evaluateGate resolves the gate verdict for a decision node's feature branch,
// failing SAFE to GateUnknown on every "can't determine" case (no port wired, no
// branch/base metadata, no project repo, or an infra error from the gate) so an
// indeterminate gate never drives an auto outcome.
func (s *Service) evaluateGate(ctx context.Context, t *pm.Task) pm.GateVerdict {
	if s.decisionGate == nil {
		return pm.GateUnknown // gate disabled (pre-B3 / not wired)
	}
	branch, base := t.Branch(), t.Base()
	if branch == "" || base == "" {
		return pm.GateUnknown // no merge target metadata to run the gate against
	}
	url, err := s.primaryRepoURL(ctx, t.ProjectID())
	if err != nil || url == "" {
		return pm.GateUnknown // no repo to check out / read error → defer to human
	}
	v, err := s.decisionGate.GateStatus(ctx, url, branch, base)
	if err != nil {
		return pm.GateUnknown // gate could not run → defer (never auto pass/reject)
	}
	return v
}

// countOpenReviewComments counts UNRESOLVED review comments on the decision node.
// The cycle has no dedicated resolved-review-comment store yet (findings are
// immutable DeLM gists, not resolvable comments), so B3's interim signal is: a
// finding of kind `failure` recorded ON the decision node — a reviewer's explicit
// "this failed" objection grounded in the very node being decided. It is read-only;
// a nil findings repo ⇒ 0 (no objections known). A read error is returned so the
// caller defers (never auto-passes on an unverifiable comment state). This signal is
// deliberately conservative (it can only DEFER, never auto-pass over an objection)
// and is the documented seam to a richer review-comment model later.
func (s *Service) countOpenReviewComments(ctx context.Context, planID pm.PlanID, taskID pm.TaskID) (int, error) {
	if s.findings == nil {
		return 0, nil
	}
	all, err := s.findings.ListByPlan(ctx, planID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, f := range all {
		if f.TaskID() == taskID && f.Kind() == pm.FindingFailure {
			n++
		}
	}
	return n, nil
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
	_, err = s.planDispatcher.PostMention(ctx, plan.ConversationID(), target, content)
	return err
}
