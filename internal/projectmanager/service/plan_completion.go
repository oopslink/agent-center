package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

// PlanCompletionEvaluation is the single completion predicate used by automatic
// advancement and the explicit complete_plan API. Historical superseded failures
// remain observable in View.HistoricalFailures, but only ActiveFailures block.
type PlanCompletionEvaluation struct {
	CanComplete       bool
	Reasons           []string
	View              pm.PlanView
	OpenContinuations []pm.ContinuationID
}

// CompletePlan explicitly moves a running/paused plan to done when the same
// evaluator used by auto-advance says the current effective node set is complete.
// It is idempotent for already-done plans.
func (s *Service) CompletePlan(ctx context.Context, planID pm.PlanID, actor pm.IdentityRef) error {
	return s.CompletePlanWithOptions(ctx, planID, actor, CompletePlanOptions{})
}

// CompletePlanOptions controls the explicit, human-only completion escape hatch.
// Automatic completion never supplies these options.
type CompletePlanOptions struct {
	Force  bool
	Reason string
}

// CompletePlanWithOptions force-completes a plan only when the caller explicitly
// opts in and supplies an audit reason. Force bypasses completion eligibility,
// but not identity, membership, existence, or lifecycle-state checks.
func (s *Service) CompletePlanWithOptions(ctx context.Context, planID pm.PlanID, actor pm.IdentityRef, opts CompletePlanOptions) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	if err := actor.Validate(); err != nil {
		return err
	}
	reason := strings.TrimSpace(opts.Reason)
	if opts.Force && reason == "" {
		return pm.ErrForceReasonRequired
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
		if p.Status() == pm.PlanDone {
			return nil
		}
		if p.Status() != pm.PlanRunning && p.Status() != pm.PlanPaused {
			return pm.ErrPlanNotRunning
		}
		if err := s.guardPlanProgressHolds(txCtx, planID, false, false, true); err != nil {
			return err
		}
		eval, err := s.canCompletePlan(txCtx, p)
		if err != nil {
			return err
		}
		if !opts.Force && !eval.CanComplete {
			return fmt.Errorf("%w: %s", pm.ErrPlanNotComplete, strings.Join(eval.Reasons, "; "))
		}
		if err := s.markPlanDone(txCtx, p, now); err != nil {
			return err
		}
		if opts.Force {
			s.auditPlan(txCtx, p, pm.AuditPlanForceCompleted, actor, map[string]any{
				"reason": reason, "bypassed_blockers": eval.Reasons,
			})
		}
		return nil
	})
}

func (s *Service) completePlanIfEligible(ctx context.Context, p *pm.Plan) (bool, error) {
	if p == nil || p.IsBuiltin() || p.Status() == pm.PlanDone {
		return p != nil && p.Status() == pm.PlanDone, nil
	}
	if p.Status() != pm.PlanRunning && p.Status() != pm.PlanPaused {
		return false, nil
	}
	if err := s.guardPlanProgressHolds(ctx, p.ID(), false, false, true); err != nil {
		if errors.Is(err, pm.ErrProgressHoldOpen) {
			return false, nil
		}
		return false, err
	}
	eval, err := s.canCompletePlan(ctx, p)
	if err != nil {
		return false, err
	}
	if !eval.CanComplete {
		return false, nil
	}
	if err := s.markPlanDone(ctx, p, s.clock.Now()); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) markPlanDone(ctx context.Context, p *pm.Plan, now time.Time) error {
	if p.Status() == pm.PlanDone {
		return nil
	}
	if err := p.MarkDone(now); err != nil {
		return err
	}
	if err := s.plans.Update(ctx, p); err != nil {
		return err
	}
	if err := s.clearPlanBlockedOn(ctx, p.ID()); err != nil {
		return err
	}
	return s.emitPlanLifecycle(ctx, p, EvtPlanCompleted)
}

func (s *Service) canCompletePlan(ctx context.Context, p *pm.Plan) (PlanCompletionEvaluation, error) {
	var eval PlanCompletionEvaluation
	if p == nil {
		eval.Reasons = []string{"plan_missing"}
		return eval, nil
	}
	tasks, err := s.tasks.ListByPlan(ctx, p.ID())
	if err != nil {
		return eval, err
	}
	edges, err := s.plans.ListDependencies(ctx, p.ID())
	if err != nil {
		return eval, err
	}
	records, err := s.plans.ListDispatchRecords(ctx, p.ID())
	if err != nil {
		return eval, err
	}
	outcomes, err := s.plans.ListDecisionOutcomes(ctx, p.ID())
	if err != nil {
		return eval, err
	}
	paused, err := s.pausedSet(ctx, tasks)
	if err != nil {
		return eval, err
	}
	opts, err := s.planViewOptions(ctx, p, tasks, edges)
	if err != nil {
		return eval, err
	}
	view := pm.DerivePlanViewWithOptions(tasks, edges, records, outcomes, paused, opts)
	eval.View = view
	if view.Progress.Total == 0 {
		eval.Reasons = append(eval.Reasons, "no_effective_nodes")
	}
	if len(view.ReadySet) > 0 {
		eval.Reasons = append(eval.Reasons, "ready_work:"+taskIDsReason(view.ReadySet))
	}
	for _, n := range view.Nodes {
		if !n.Effective {
			continue
		}
		switch n.NodeStatus {
		case pm.NodeDone, pm.NodeSkipped:
			continue
		case pm.NodeFailed:
			eval.Reasons = append(eval.Reasons, "unreplaced_failed:"+string(n.TaskID))
		case pm.NodeReady, pm.NodeDispatched, pm.NodeRunning, pm.NodePaused:
			eval.Reasons = append(eval.Reasons, "active_work:"+string(n.TaskID)+":"+string(n.NodeStatus))
		default:
			eval.Reasons = append(eval.Reasons, "unsettled_work:"+string(n.TaskID)+":"+string(n.NodeStatus))
		}
	}
	for _, id := range incompleteEffectiveLeaves(view, edges) {
		eval.Reasons = append(eval.Reasons, "incomplete_effective_leaf:"+string(id))
	}
	// Conditions belong to the current effective topology, not to the Plan forever.
	// Evolution deliberately keeps historical graph facts for audit, so scanning every
	// unresolved condition in the graph resurrects superseded gates and can strand a
	// 52/52 Plan in running with an empty frontier. Filter through the same effective
	// node projection used by progress/ready/completion before applying condition gates.
	pendingConditions, err := s.unresolvedGraphConditions(ctx, p, view)
	if err != nil {
		return eval, err
	}
	for _, condition := range pendingConditions {
		eval.Reasons = append(eval.Reasons, "unresolved_condition:"+condition)
	}
	if s.remediation != nil {
		continuations, err := s.remediation.ListContinuationsByPlan(ctx, p.ID())
		if err != nil {
			return eval, err
		}
		for _, c := range continuations {
			if c.Status != pm.ContinuationClosed {
				eval.OpenContinuations = append(eval.OpenContinuations, c.ID)
			}
		}
		if len(eval.OpenContinuations) > 0 {
			eval.Reasons = append(eval.Reasons, "open_remediation:"+continuationIDsReason(eval.OpenContinuations))
		}
	}
	eval.CanComplete = len(eval.Reasons) == 0
	return eval, nil
}

func incompleteEffectiveLeaves(view pm.PlanView, edges []pm.Dependency) []pm.TaskID {
	effective := make(map[pm.TaskID]pm.NodeStatus, len(view.Nodes))
	for _, n := range view.Nodes {
		if n.Effective {
			effective[n.TaskID] = n.NodeStatus
		}
	}
	hasEffectiveDependent := make(map[pm.TaskID]bool, len(effective))
	for _, e := range edges {
		if e.IsLoopback() {
			continue
		}
		if _, ok := effective[e.FromTaskID]; !ok {
			continue
		}
		if _, ok := effective[e.ToTaskID]; !ok {
			continue
		}
		hasEffectiveDependent[e.ToTaskID] = true
	}
	var out []pm.TaskID
	for id, status := range effective {
		if hasEffectiveDependent[id] {
			continue
		}
		if status != pm.NodeDone && status != pm.NodeSkipped {
			out = append(out, id)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func taskIDsReason(ids []pm.TaskID) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, string(id))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func continuationIDsReason(ids []pm.ContinuationID) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, string(id))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func (s *Service) unresolvedGraphConditions(ctx context.Context, p *pm.Plan, view pm.PlanView) ([]string, error) {
	if s.orch == nil || p == nil || p.GraphID() == "" {
		return nil, nil
	}
	nodes, err := s.orch.ListNodes(ctx, orch.GraphID(p.GraphID()))
	if err != nil {
		return nil, err
	}
	pending := activeUnresolvedGraphConditions(nodes, view)
	sort.Strings(pending)
	return pending, nil
}

// activeUnresolvedGraphConditions is the single generation/effective-topology
// ownership filter for orchestration conditions. A condition created for a task
// that is no longer effective is historical audit state and must never participate
// in current completion eligibility. Unknown/unowned conditions remain fail-closed:
// they are returned by node id so corrupt or legacy ambiguous state becomes an owned
// progress incident instead of being silently ignored.
func activeUnresolvedGraphConditions(nodes []*orch.Node, view pm.PlanView) []string {
	effective := make(map[pm.TaskID]bool, len(view.Nodes))
	known := make(map[pm.TaskID]bool, len(view.Nodes))
	for _, node := range view.Nodes {
		known[node.TaskID] = true
		if node.Effective {
			effective[node.TaskID] = true
		}
	}
	var pending []string
	for _, node := range nodes {
		if node.ControlKind() != orch.ControlKindCondition {
			continue
		}
		if node.Status() == orch.NodeCompleted || node.Status() == orch.NodeDiscarded {
			continue
		}
		decisionID, _ := node.Metadata()["condition_for"].(string)
		if decisionID != "" {
			taskID := pm.TaskID(decisionID)
			if known[taskID] && !effective[taskID] {
				continue
			}
		} else {
			decisionID = string(node.ID())
		}
		pending = append(pending, decisionID)
	}
	return pending
}

func (s *Service) planViewOptions(ctx context.Context, p *pm.Plan, tasks []*pm.Task, edges []pm.Dependency) (pm.PlanViewOptions, error) {
	inactive := make(map[pm.TaskID]pm.PlanNodeReplacement)
	taskByID := make(map[pm.TaskID]*pm.Task, len(tasks))
	for _, task := range tasks {
		taskByID[task.ID()] = task
	}
	addReplacement := func(oldID pm.TaskID, newIDs []pm.TaskID, reason string) {
		if oldID == "" || len(newIDs) == 0 {
			return
		}
		if _, ok := taskByID[oldID]; !ok {
			return
		}
		filtered := make([]pm.TaskID, 0, len(newIDs))
		for _, id := range newIDs {
			if id == "" || id == oldID {
				continue
			}
			if _, ok := taskByID[id]; ok {
				filtered = append(filtered, id)
			}
		}
		if len(filtered) == 0 {
			return
		}
		sort.SliceStable(filtered, func(i, j int) bool { return filtered[i] < filtered[j] })
		cur := inactive[oldID]
		seen := make(map[pm.TaskID]bool, len(cur.By)+len(filtered))
		merged := make([]pm.TaskID, 0, len(cur.By)+len(filtered))
		for _, id := range cur.By {
			if !seen[id] {
				seen[id] = true
				merged = append(merged, id)
			}
		}
		for _, id := range filtered {
			if !seen[id] {
				seen[id] = true
				merged = append(merged, id)
			}
		}
		sort.SliceStable(merged, func(i, j int) bool { return merged[i] < merged[j] })
		if cur.Reason != "" && cur.Reason != reason {
			reason = cur.Reason + "," + reason
		}
		inactive[oldID] = pm.PlanNodeReplacement{By: merged, Reason: reason}
	}

	// Generation supersede decisions are immutable history. Non-terminal tasks now
	// retain plan_id (container attribution), so derive their inactive overlay from
	// the active generation lineage instead of relying on physical detachment. Walk
	// every ancestor: a later generation need not repeat an earlier supersede.
	if generationID := p.ActiveGenerationID(); generationID != "" {
		seen := make(map[pm.PlanGenerationID]bool)
		for generationID != "" && !seen[generationID] {
			seen[generationID] = true
			generation, err := s.plans.FindGenerationByID(ctx, generationID)
			if err != nil {
				return pm.PlanViewOptions{}, err
			}
			for _, decision := range generation.Diff.NodeDecisions {
				if decision.Action != pm.EvolutionSupersede || taskByID[decision.TaskID] == nil {
					continue
				}
				var replacements []pm.TaskID
				for _, task := range tasks {
					if task.FollowsTaskID() == decision.TaskID {
						replacements = append(replacements, task.ID())
					}
				}
				if len(replacements) == 0 {
					inactive[decision.TaskID] = pm.PlanNodeReplacement{Reason: "generation_supersede"}
				} else {
					addReplacement(decision.TaskID, replacements, "generation_supersede")
				}
			}
			generationID = generation.ParentGenerationID
		}
	}

	// Current-node lineage is append-only. Newer replacements stamp
	// pm_tasks.follows_task_id directly; older ADR-0055 remediation plans may only
	// have continuation/stage generation rows, so use both as compatible sources.
	for _, task := range tasks {
		if follows := task.FollowsTaskID(); follows != "" {
			if old := taskByID[follows]; old != nil && pm.TaskIsFailed(old.Status()) {
				addReplacement(follows, []pm.TaskID{task.ID()}, "follows_task")
			}
		}
	}

	if s.remediation != nil && s.stages != nil {
		stages, err := s.stages.ListByPlan(ctx, p.ID())
		if err != nil {
			return pm.PlanViewOptions{}, err
		}
		stageByID := make(map[pm.StageID]*pm.Stage, len(stages))
		tasksByStage := make(map[pm.StageID][]pm.TaskID)
		for _, stage := range stages {
			stageByID[stage.ID()] = stage
		}
		for _, task := range tasks {
			if task.StageID() != "" {
				tasksByStage[task.StageID()] = append(tasksByStage[task.StageID()], task.ID())
			}
		}
		continuations, err := s.remediation.ListContinuationsByPlan(ctx, p.ID())
		if err != nil {
			return pm.PlanViewOptions{}, err
		}
		for _, continuation := range continuations {
			if continuation.CurrentStageID == "" || continuation.CurrentStageID == continuation.RootStageID {
				continue
			}
			replacements := tasksByStage[continuation.CurrentStageID]
			if len(replacements) == 0 {
				continue
			}
			currentGeneration := 0
			if current := stageByID[continuation.CurrentStageID]; current != nil {
				currentGeneration = current.Generation()
			}
			for _, oldID := range tasksByStage[continuation.RootStageID] {
				if taskByID[oldID] != nil && pm.TaskIsFailed(taskByID[oldID].Status()) {
					addReplacement(oldID, replacements, "remediation_continuation")
				}
			}
			for _, stage := range stages {
				if stage.ContinuationID() != continuation.ID || stage.ID() == continuation.CurrentStageID {
					continue
				}
				if currentGeneration > 0 && stage.Generation() >= currentGeneration {
					continue
				}
				for _, oldID := range tasksByStage[stage.ID()] {
					if taskByID[oldID] != nil && pm.TaskIsFailed(taskByID[oldID].Status()) {
						addReplacement(oldID, replacements, "remediation_continuation")
					}
				}
			}
		}
	}

	for oldID, replacement := range legacyCompletedRemediationReplacements(tasks, edges, inactive) {
		addReplacement(oldID, replacement.By, replacement.Reason)
	}

	return pm.PlanViewOptions{InactiveTasks: inactive}, nil
}

// legacyCompletedRemediationReplacements recognizes pre-ADR-0055 remediation
// plans that never wrote follows_task_id/origin_verdict_id. It is deliberately
// narrow: every current task must be terminal, each unmarked failure must be a
// leaf, and the completed DAG must contain remediation/recovery -> ship -> final
// acceptance evidence. Anything still active, non-leaf failed, or missing that
// evidence stays in the effective graph and continues to block completion.
func legacyCompletedRemediationReplacements(tasks []*pm.Task, edges []pm.Dependency, inactive map[pm.TaskID]pm.PlanNodeReplacement) map[pm.TaskID]pm.PlanNodeReplacement {
	taskByID := make(map[pm.TaskID]*pm.Task, len(tasks))
	var failedLeaves []pm.TaskID
	for _, task := range tasks {
		if task == nil {
			continue
		}
		taskByID[task.ID()] = task
		if _, alreadyReplaced := inactive[task.ID()]; alreadyReplaced {
			continue
		}
		switch {
		case pm.TaskIsDone(task.Status()):
			continue
		case pm.TaskIsFailed(task.Status()):
			failedLeaves = append(failedLeaves, task.ID())
		default:
			return nil
		}
	}
	if len(failedLeaves) == 0 {
		return nil
	}

	upstream := make(map[pm.TaskID][]pm.TaskID)
	downstream := make(map[pm.TaskID][]pm.TaskID)
	hasDependent := make(map[pm.TaskID]bool)
	for _, edge := range edges {
		if edge.IsLoopback() {
			continue
		}
		if _, ok := taskByID[edge.FromTaskID]; !ok {
			continue
		}
		if _, ok := taskByID[edge.ToTaskID]; !ok {
			continue
		}
		upstream[edge.FromTaskID] = append(upstream[edge.FromTaskID], edge.ToTaskID)
		downstream[edge.ToTaskID] = append(downstream[edge.ToTaskID], edge.FromTaskID)
		hasDependent[edge.ToTaskID] = true
	}
	for _, id := range failedLeaves {
		if hasDependent[id] {
			return nil
		}
	}

	out := make(map[pm.TaskID]pm.PlanNodeReplacement, len(failedLeaves))
	for _, id := range failedLeaves {
		evidence := legacyCompletedRemediationEvidence(id, taskByID, upstream, downstream, hasDependent)
		if len(evidence) == 0 {
			// A recovery chain elsewhere in the plan must not erase an unrelated
			// failed leaf. Legacy plans have no explicit lineage, so require the
			// remediation chain to share one of this failure's prerequisites.
			return nil
		}
		out[id] = pm.PlanNodeReplacement{By: evidence, Reason: "legacy_completed_remediation"}
	}
	return out
}

func legacyCompletedRemediationEvidence(failedID pm.TaskID, taskByID map[pm.TaskID]*pm.Task, upstream, downstream map[pm.TaskID][]pm.TaskID, hasDependent map[pm.TaskID]bool) []pm.TaskID {
	failedPrereqs := legacyUpstreamClosure(failedID, upstream)
	if len(failedPrereqs) == 0 {
		return nil
	}
	finals := make([]pm.TaskID, 0)
	for id, task := range taskByID {
		if !pm.TaskIsDone(task.Status()) || hasDependent[id] || !legacyLooksLikeFinalAcceptance(task) {
			continue
		}
		finals = append(finals, id)
	}
	sort.SliceStable(finals, func(i, j int) bool { return finals[i] < finals[j] })
	for _, finalID := range finals {
		ancestors := legacyUpstreamClosure(finalID, upstream)
		var remediations []pm.TaskID
		var ships []pm.TaskID
		for id := range ancestors {
			task := taskByID[id]
			if task == nil || !pm.TaskIsDone(task.Status()) {
				continue
			}
			if legacyLooksLikeRecoveryOrRemediation(task) && legacySharesPrerequisite(id, failedPrereqs, upstream) {
				remediations = append(remediations, id)
			}
			if legacyLooksLikeShip(task) {
				ships = append(ships, id)
			}
		}
		sort.SliceStable(remediations, func(i, j int) bool { return remediations[i] < remediations[j] })
		sort.SliceStable(ships, func(i, j int) bool { return ships[i] < ships[j] })
		for _, remediationID := range remediations {
			for _, shipID := range ships {
				if !legacyReachable(downstream, remediationID, shipID) || !legacyReachable(downstream, shipID, finalID) {
					continue
				}
				return uniqueTaskIDs([]pm.TaskID{remediationID, shipID, finalID})
			}
		}
	}
	return nil
}

func legacySharesPrerequisite(taskID pm.TaskID, failedPrereqs map[pm.TaskID]bool, upstream map[pm.TaskID][]pm.TaskID) bool {
	for prereq := range legacyUpstreamClosure(taskID, upstream) {
		if failedPrereqs[prereq] {
			return true
		}
	}
	return false
}

func legacyUpstreamClosure(from pm.TaskID, upstream map[pm.TaskID][]pm.TaskID) map[pm.TaskID]bool {
	seen := make(map[pm.TaskID]bool)
	stack := append([]pm.TaskID(nil), upstream[from]...)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		stack = append(stack, upstream[id]...)
	}
	return seen
}

func legacyReachable(downstream map[pm.TaskID][]pm.TaskID, from, to pm.TaskID) bool {
	if from == "" || to == "" || from == to {
		return false
	}
	seen := make(map[pm.TaskID]bool)
	stack := append([]pm.TaskID(nil), downstream[from]...)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if id == to {
			return true
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		stack = append(stack, downstream[id]...)
	}
	return false
}

func legacyLooksLikeRecoveryOrRemediation(task *pm.Task) bool {
	return legacyTaskTextHasAny(task, []string{"remediation", "remediate", "recovery", "recover", "rework", "修复", "恢复", "整改", "返工"})
}

func legacyLooksLikeShip(task *pm.Task) bool {
	return pm.RequiresAcceptance(task) || legacyTaskTextHasAny(task, []string{"ship", "release", "deploy", "发布", "部署", "上线", "合并"})
}

func legacyLooksLikeFinalAcceptance(task *pm.Task) bool {
	return legacyTaskTextHasAny(task, []string{"final acceptance", "acceptance", "final review", "final verification", "最终验收", "验收", "最终复验", "复验"})
}

func legacyTaskTextHasAny(task *pm.Task, needles []string) bool {
	if task == nil {
		return false
	}
	text := strings.ToLower(task.Title() + " " + task.Description() + " " + strings.Join(task.Tags(), " "))
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func uniqueTaskIDs(ids []pm.TaskID) []pm.TaskID {
	seen := make(map[pm.TaskID]bool, len(ids))
	out := make([]pm.TaskID, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
