package service

import (
	"context"
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
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	if err := actor.Validate(); err != nil {
		return err
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
		eval, err := s.canCompletePlan(txCtx, p)
		if err != nil {
			return err
		}
		if !eval.CanComplete {
			return fmt.Errorf("%w: %s", pm.ErrPlanNotComplete, strings.Join(eval.Reasons, "; "))
		}
		return s.markPlanDone(txCtx, p, now)
	})
}

func (s *Service) completePlanIfEligible(ctx context.Context, p *pm.Plan) (bool, error) {
	if p == nil || p.IsBuiltin() || p.Status() == pm.PlanDone {
		return p != nil && p.Status() == pm.PlanDone, nil
	}
	if p.Status() != pm.PlanRunning && p.Status() != pm.PlanPaused {
		return false, nil
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
	opts, err := s.planViewOptions(ctx, p, tasks)
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
	pendingConditions, err := s.unresolvedGraphConditions(ctx, p)
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

func (s *Service) unresolvedGraphConditions(ctx context.Context, p *pm.Plan) ([]string, error) {
	if s.orch == nil || p == nil || p.GraphID() == "" {
		return nil, nil
	}
	nodes, err := s.orch.ListNodes(ctx, orch.GraphID(p.GraphID()))
	if err != nil {
		return nil, err
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
		if decisionID == "" {
			decisionID = string(node.ID())
		}
		pending = append(pending, decisionID)
	}
	sort.Strings(pending)
	return pending, nil
}

func (s *Service) planViewOptions(ctx context.Context, p *pm.Plan, tasks []*pm.Task) (pm.PlanViewOptions, error) {
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
		if cur.Reason != "" && cur.Reason != reason {
			reason = cur.Reason + "," + reason
		}
		inactive[oldID] = pm.PlanNodeReplacement{By: merged, Reason: reason}
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

	return pm.PlanViewOptions{InactiveTasks: inactive}, nil
}
