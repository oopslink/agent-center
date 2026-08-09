package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

const (
	// PlanLivenessDeadEndAfter is the quiet window before a running structured plan
	// with no reachable frontier is declared stuck. It is intentionally aligned with
	// the stuck-node progress-stale window: short enough to stop silent spinning, long
	// enough to avoid racing normal dispatch/event propagation.
	PlanLivenessDeadEndAfter = 10 * time.Minute

	// PlanLivenessAlertRepeatAfter limits repeated diagnostics for the same plan and
	// reason within one daemon process. Recovery still runs every sweep; only the alert
	// event/message is rate-limited.
	PlanLivenessAlertRepeatAfter = time.Hour
)

const auditPlanLivenessWatchdog = pm.AuditChangeType("liveness_watchdog")

const (
	planLivenessReasonLeaseOnlyRunning = "lease_only_running_no_executor"
	planLivenessReasonAcceptanceWait   = "acceptance_not_pass_no_frontier"
	planLivenessReasonUnhandledReject  = "unhandled_reject_no_frontier"
	planLivenessReasonOpenContinuation = "open_continuation_no_frontier"

	planLivenessActionTriggerStuckRecovery = "trigger_stuck_node_recovery"
	planLivenessActionReplayRejectVerdict  = "replay_reject_verdict"
	planLivenessActionEscalate             = "escalate"
)

type planLivenessWatchdogPayload struct {
	PlanID                string            `json:"plan_id"`
	ProjectID             string            `json:"project_id"`
	Reason                string            `json:"reason"`
	Action                string            `json:"action"`
	ReadySetCount         int               `json:"ready_set_count"`
	BlockedOnCount        int               `json:"blocked_on_count"`
	AcceptanceWaitTaskIDs []string          `json:"acceptance_wait_task_ids,omitempty"`
	RunningTaskIDs        []string          `json:"running_task_ids,omitempty"`
	LeaseOnlyTaskIDs      []string          `json:"lease_only_task_ids,omitempty"`
	DispatchedFrontierIDs []string          `json:"dispatched_frontier_ids,omitempty"`
	ContinuationIDs       []string          `json:"continuation_ids,omitempty"`
	ContinuationStatuses  map[string]string `json:"continuation_statuses,omitempty"`
	TriggerVerdictIDs     []string          `json:"trigger_verdict_ids,omitempty"`
	RecoveryVerdictID     string            `json:"recovery_verdict_id,omitempty"`
	GraphAllDone          bool              `json:"graph_all_done"`
	IdleSince             time.Time         `json:"idle_since"`
	DetectedAt            time.Time         `json:"detected_at"`
	PlanUpdatedAt         time.Time         `json:"plan_updated_at"`
	OpenContinuationCount int               `json:"open_continuation_count"`
	ThresholdSeconds      int               `json:"threshold_seconds"`
	DiagnosticDescription string            `json:"diagnostic_description"`
}

type planLivenessAssessment struct {
	planID                pm.PlanID
	projectID             pm.ProjectID
	reason                string
	action                string
	readySetCount         int
	blockedOnCount        int
	acceptanceWaitTaskIDs []pm.TaskID
	runningTaskIDs        []pm.TaskID
	leaseOnlyTaskIDs      []pm.TaskID
	dispatchedFrontierIDs []pm.TaskID
	continuations         []*pm.PlanContinuation
	recoveryVerdict       *pm.GateVerdict
	leaseOnlyTasks        []*pm.Task
	graphAllDone          bool
	idleSince             time.Time
	detectedAt            time.Time
	planUpdatedAt         time.Time
	description           string
}

// watchPlanLiveness is the last step of the running-plan reconcile sweep. It only
// runs after dispatch, BlockedOn materialization, and timeout routing have observed
// the current plan state. Anything with a normal frontier is left alone; residual
// no-frontier states get an explicit diagnostic and either use an existing recovery
// path or escalate to the plan creator.
func (s *Service) watchPlanLiveness(ctx context.Context, planID pm.PlanID) error {
	if s.plans == nil || s.tasks == nil {
		return nil
	}
	var assessment *planLivenessAssessment
	if err := s.runInTx(ctx, func(txCtx context.Context) error {
		a, err := s.assessPlanLiveness(txCtx, planID)
		if err != nil || a == nil {
			return err
		}
		assessment = a
		if !s.shouldEmitPlanLivenessDiagnostic(a) {
			return nil
		}
		return s.recordPlanLivenessDiagnostic(txCtx, a)
	}); err != nil {
		return err
	}
	if assessment == nil {
		return nil
	}

	switch assessment.action {
	case planLivenessActionTriggerStuckRecovery:
		return s.triggerStuckNodeRecovery(ctx, assessment)
	case planLivenessActionReplayRejectVerdict:
		return s.replayRejectVerdictRecovery(ctx, assessment)
	case planLivenessActionEscalate:
		if s.shouldEscalatePlanLiveness(assessment) {
			return s.escalatePlanLiveness(ctx, assessment)
		}
	}
	return nil
}

func (s *Service) assessPlanLiveness(txCtx context.Context, planID pm.PlanID) (*planLivenessAssessment, error) {
	p, err := s.plans.FindByID(txCtx, planID)
	if err != nil {
		return nil, err
	}
	if p.Status() != pm.PlanRunning || p.IsBuiltin() || p.GraphID() == "" || s.orch == nil {
		return nil, nil
	}
	now := s.clock.Now()
	tasks, err := s.tasks.ListByPlan(txCtx, planID)
	if err != nil {
		return nil, err
	}
	records, err := s.plans.ListDispatchRecords(txCtx, planID)
	if err != nil {
		return nil, err
	}
	if err := s.syncGraphToTasks(txCtx, p, tasks); err != nil {
		return nil, err
	}
	readySet, allDone, err := s.graphReadySet(txCtx, p, tasks, records)
	if err != nil {
		return nil, err
	}
	if len(readySet) > 0 {
		return nil, nil
	}

	blocked, err := s.plans.ListBlockedOn(txCtx, planID)
	if err != nil {
		return nil, err
	}
	continuations, err := s.openContinuations(txCtx, planID)
	if err != nil {
		return nil, err
	}
	idleSince := latestPlanLivenessActivity(p.UpdatedAt(), tasks, blocked, continuations)
	if now.Sub(idleSince) < PlanLivenessDeadEndAfter {
		return nil, nil
	}

	frontier, err := s.classifyPlanLivenessFrontier(txCtx, p, tasks, records, now)
	if err != nil {
		return nil, err
	}
	if len(frontier.realOrUnknownRunning) > 0 || len(frontier.dispatchedRunnable) > 0 || len(frontier.parked) > 0 {
		return nil, nil
	}

	acceptanceWaits, hasNormalBlockedWait := classifyPlanLivenessWaits(blocked)
	directAcceptanceWaits, err := s.acceptanceBlockedTaskIDs(txCtx, p, tasks)
	if err != nil {
		return nil, err
	}
	acceptanceWaits = appendUniqueTaskIDs(acceptanceWaits, directAcceptanceWaits...)
	base := &planLivenessAssessment{
		planID:                p.ID(),
		projectID:             p.ProjectID(),
		readySetCount:         len(readySet),
		blockedOnCount:        len(blocked),
		acceptanceWaitTaskIDs: acceptanceWaits,
		runningTaskIDs:        frontier.runningTaskIDs,
		leaseOnlyTaskIDs:      frontier.leaseOnlyTaskIDs,
		dispatchedFrontierIDs: frontier.dispatchedRunnable,
		continuations:         continuations,
		graphAllDone:          allDone,
		idleSince:             idleSince,
		detectedAt:            now,
		planUpdatedAt:         p.UpdatedAt(),
	}

	if len(frontier.leaseOnlyTasks) > 0 {
		base.reason = planLivenessReasonLeaseOnlyRunning
		base.action = planLivenessActionTriggerStuckRecovery
		base.leaseOnlyTasks = frontier.leaseOnlyTasks
		base.description = "running task rows have fresh worker snapshots proving no live executor; delegated to stuck-node recovery"
		return base, nil
	}

	if len(continuations) > 0 {
		verdicts, err := s.remediation.ListVerdictsByPlan(txCtx, planID)
		if err != nil {
			return nil, err
		}
		verdictByID := make(map[pm.GateVerdictID]pm.GateVerdict, len(verdicts))
		for _, v := range verdicts {
			verdictByID[v.ID] = v
		}
		for _, c := range continuations {
			v, ok := verdictByID[c.TriggerVerdictID]
			if !ok || v.Outcome != pm.GateVerdictReject {
				continue
			}
			if c.Status == pm.ContinuationAwaitingRemediation && c.RemainingBudget > 0 {
				base.reason = planLivenessReasonUnhandledReject
				base.action = planLivenessActionReplayRejectVerdict
				vv := v
				base.recoveryVerdict = &vv
				base.description = "reject verdict has an open awaiting-remediation continuation but no appended remediation frontier"
				return base, nil
			}
		}
	}

	if hasNormalBlockedWait {
		return nil, nil
	}
	if len(acceptanceWaits) > 0 {
		base.reason = planLivenessReasonAcceptanceWait
		base.action = planLivenessActionEscalate
		base.description = "final acceptance is not pass and no runnable/running frontier remains"
		return base, nil
	}
	if len(continuations) == 0 {
		// A historical terminal plan should already have been marked done by the dispatch
		// phase. With no open continuation and no specific gate wait, do not invent a
		// dead-end diagnosis here.
		return nil, nil
	}

	base.reason = planLivenessReasonOpenContinuation
	base.action = planLivenessActionEscalate
	base.description = "open remediation continuation remains but the plan has no runnable/running frontier"
	return base, nil
}

type planLivenessFrontier struct {
	realOrUnknownRunning []pm.TaskID
	runningTaskIDs       []pm.TaskID
	leaseOnlyTaskIDs     []pm.TaskID
	leaseOnlyTasks       []*pm.Task
	dispatchedRunnable   []pm.TaskID
	parked               []pm.TaskID
}

func (s *Service) classifyPlanLivenessFrontier(ctx context.Context, p *pm.Plan, tasks []*pm.Task, records []pm.DispatchRecord, now time.Time) (planLivenessFrontier, error) {
	dispatched := make(map[pm.TaskID]struct{}, len(records))
	for _, r := range records {
		dispatched[r.TaskID] = struct{}{}
	}
	var out planLivenessFrontier
	for _, t := range tasks {
		switch t.Status() {
		case pm.TaskRunning:
			out.runningTaskIDs = append(out.runningTaskIDs, t.ID())
			known, missing := s.liveExecutorState(t, now)
			if known && missing {
				out.leaseOnlyTaskIDs = append(out.leaseOnlyTaskIDs, t.ID())
				out.leaseOnlyTasks = append(out.leaseOnlyTasks, t)
				continue
			}
			// Unknown live state is deliberately treated as active. A stale/missing
			// heartbeat is not proof of death.
			out.realOrUnknownRunning = append(out.realOrUnknownRunning, t.ID())
		case pm.TaskBlocked:
			out.parked = append(out.parked, t.ID())
		case pm.TaskOpen:
			if _, ok := dispatched[t.ID()]; !ok {
				continue
			}
			if err := s.EnsureTaskRunnable(ctx, t.ID()); err == nil {
				out.dispatchedRunnable = append(out.dispatchedRunnable, t.ID())
			} else if !errors.Is(err, pm.ErrTaskNotRunnable) {
				return out, err
			}
		}
	}
	return out, nil
}

func (s *Service) acceptanceBlockedTaskIDs(ctx context.Context, p *pm.Plan, tasks []*pm.Task) ([]pm.TaskID, error) {
	var out []pm.TaskID
	for _, t := range tasks {
		if t == nil || t.Status().IsTerminal() {
			continue
		}
		blocked, err := s.acceptanceVerdictBlocks(ctx, p, t)
		if err != nil {
			return nil, err
		}
		if blocked {
			out = append(out, t.ID())
		}
	}
	return out, nil
}

func classifyPlanLivenessWaits(blocked []pm.BlockedOn) ([]pm.TaskID, bool) {
	var acceptance []pm.TaskID
	for _, b := range blocked {
		switch b.WaitType {
		case pm.WaitExecutorLiveness:
			continue
		case pm.WaitAcceptanceVerdict:
			acceptance = append(acceptance, b.TaskID)
		default:
			return acceptance, true
		}
	}
	return acceptance, false
}

func latestPlanLivenessActivity(planUpdatedAt time.Time, tasks []*pm.Task, blocked []pm.BlockedOn, continuations []*pm.PlanContinuation) time.Time {
	latest := planUpdatedAt
	for _, t := range tasks {
		if t != nil && !t.Status().IsTerminal() && t.UpdatedAt().After(latest) {
			latest = t.UpdatedAt()
		}
	}
	for _, b := range blocked {
		if b.WaitType == pm.WaitExecutorLiveness {
			continue
		}
		if b.WaitedSince.After(latest) {
			latest = b.WaitedSince
		}
	}
	for _, c := range continuations {
		if c != nil && c.UpdatedAt.After(latest) {
			latest = c.UpdatedAt
		}
	}
	return latest
}

func (s *Service) stageRejectHasOpenContinuation(ctx context.Context, planID pm.PlanID, gateTaskID pm.TaskID) (bool, error) {
	if s.remediation == nil {
		return false, nil
	}
	verdict, found, err := s.remediation.FindVerdictByGate(ctx, gateTaskID)
	if err != nil || !found || verdict.Outcome != pm.GateVerdictReject {
		return false, err
	}
	continuations, err := s.remediation.ListContinuationsByPlan(ctx, planID)
	if err != nil {
		return false, err
	}
	for _, c := range continuations {
		if c != nil && c.Status != pm.ContinuationClosed && c.TriggerVerdictID == verdict.ID {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) openContinuations(ctx context.Context, planID pm.PlanID) ([]*pm.PlanContinuation, error) {
	if s.remediation == nil {
		return nil, nil
	}
	continuations, err := s.remediation.ListContinuationsByPlan(ctx, planID)
	if err != nil {
		return nil, err
	}
	open := make([]*pm.PlanContinuation, 0, len(continuations))
	for _, c := range continuations {
		if c != nil && c.Status != pm.ContinuationClosed {
			open = append(open, c)
		}
	}
	return open, nil
}

func (s *Service) shouldEmitPlanLivenessDiagnostic(a *planLivenessAssessment) bool {
	key := string(a.planID) + ":" + a.reason + ":" + a.action
	s.planWatchdogMu.Lock()
	defer s.planWatchdogMu.Unlock()
	if s.planWatchdogAlerts == nil {
		s.planWatchdogAlerts = make(map[string]time.Time)
	}
	if last, ok := s.planWatchdogAlerts[key]; ok && a.detectedAt.Sub(last) < PlanLivenessAlertRepeatAfter {
		return false
	}
	s.planWatchdogAlerts[key] = a.detectedAt
	return true
}

func (s *Service) shouldEscalatePlanLiveness(a *planLivenessAssessment) bool {
	key := string(a.planID) + ":" + a.reason + ":" + a.action + ":mention"
	s.planWatchdogMu.Lock()
	defer s.planWatchdogMu.Unlock()
	if s.planWatchdogAlerts == nil {
		s.planWatchdogAlerts = make(map[string]time.Time)
	}
	if last, ok := s.planWatchdogAlerts[key]; ok && a.detectedAt.Sub(last) < PlanLivenessAlertRepeatAfter {
		return false
	}
	s.planWatchdogAlerts[key] = a.detectedAt
	return true
}

func (s *Service) recordPlanLivenessDiagnostic(txCtx context.Context, a *planLivenessAssessment) error {
	payload := a.payload()
	if err := s.emit(txCtx, EvtPlanLivenessWatchdog,
		refsJSON(map[string]string{"plan_id": string(a.planID), "project_id": string(a.projectID)}),
		payload); err != nil {
		return err
	}
	s.auditPlanByID(txCtx, a.projectID, a.planID, auditPlanLivenessWatchdog, pm.SystemActor("plan-liveness-watchdog"), map[string]any{
		"reason":                   a.reason,
		"action":                   a.action,
		"ready_set_count":          a.readySetCount,
		"blocked_on_count":         a.blockedOnCount,
		"open_continuation_count":  len(a.continuations),
		"lease_only_task_ids":      planLivenessTaskIDStrings(a.leaseOnlyTaskIDs),
		"acceptance_wait_task_ids": planLivenessTaskIDStrings(a.acceptanceWaitTaskIDs),
	})
	return nil
}

func (a *planLivenessAssessment) payload() planLivenessWatchdogPayload {
	statuses := make(map[string]string, len(a.continuations))
	var continuationIDs, verdictIDs []string
	for _, c := range a.continuations {
		if c == nil {
			continue
		}
		id := string(c.ID)
		continuationIDs = append(continuationIDs, id)
		statuses[id] = string(c.Status)
		if c.TriggerVerdictID != "" {
			verdictIDs = append(verdictIDs, string(c.TriggerVerdictID))
		}
	}
	recoveryVerdictID := ""
	if a.recoveryVerdict != nil {
		recoveryVerdictID = string(a.recoveryVerdict.ID)
	}
	return planLivenessWatchdogPayload{
		PlanID:                string(a.planID),
		ProjectID:             string(a.projectID),
		Reason:                a.reason,
		Action:                a.action,
		ReadySetCount:         a.readySetCount,
		BlockedOnCount:        a.blockedOnCount,
		AcceptanceWaitTaskIDs: planLivenessTaskIDStrings(a.acceptanceWaitTaskIDs),
		RunningTaskIDs:        planLivenessTaskIDStrings(a.runningTaskIDs),
		LeaseOnlyTaskIDs:      planLivenessTaskIDStrings(a.leaseOnlyTaskIDs),
		DispatchedFrontierIDs: planLivenessTaskIDStrings(a.dispatchedFrontierIDs),
		ContinuationIDs:       continuationIDs,
		ContinuationStatuses:  statuses,
		TriggerVerdictIDs:     verdictIDs,
		RecoveryVerdictID:     recoveryVerdictID,
		GraphAllDone:          a.graphAllDone,
		IdleSince:             a.idleSince,
		DetectedAt:            a.detectedAt,
		PlanUpdatedAt:         a.planUpdatedAt,
		OpenContinuationCount: len(a.continuations),
		ThresholdSeconds:      int(PlanLivenessDeadEndAfter.Seconds()),
		DiagnosticDescription: a.description,
	}
}

func (s *Service) triggerStuckNodeRecovery(ctx context.Context, a *planLivenessAssessment) error {
	for _, t := range a.leaseOnlyTasks {
		if t == nil {
			continue
		}
		if _, err := s.reconcileStuckNode(ctx, t, t.ExecutionLeaseExpiresAt(), a.detectedAt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) replayRejectVerdictRecovery(ctx context.Context, a *planLivenessAssessment) error {
	if a.recoveryVerdict == nil {
		return nil
	}
	v := *a.recoveryVerdict
	if _, err := s.RecordStageGateVerdict(ctx, RecordStageGateVerdictCommand{
		GateTaskID:     v.GateTaskID,
		Outcome:        v.Outcome,
		Evidence:       v.Evidence,
		ReviewedSHA:    v.ReviewedSHA,
		IdempotencyKey: v.IdempotencyKey,
		Actor:          v.ActorRef,
	}); err != nil {
		return err
	}
	return s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.plans.FindByID(txCtx, a.planID)
		if err != nil {
			return err
		}
		if p.Status() != pm.PlanRunning {
			return nil
		}
		_, err = s.dispatchReadyNodes(txCtx, p)
		return err
	})
}

func (s *Service) escalatePlanLiveness(ctx context.Context, a *planLivenessAssessment) error {
	if s.planDispatcher == nil {
		return nil
	}
	return s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.plans.FindByID(txCtx, a.planID)
		if err != nil {
			return err
		}
		if p.Status() != pm.PlanRunning || strings.TrimSpace(p.ConversationID()) == "" {
			return nil
		}
		target := string(p.CreatorRef())
		if target == "" {
			return nil
		}
		content := fmt.Sprintf("plan liveness watchdog detected no reachable frontier: %s. Action: %s. Please inspect the plan gate/remediation state and resolve or discard the blocked path.", a.reason, a.action)
		_, err = s.planDispatcher.PostMention(txCtx, p.ConversationID(), target, content)
		return err
	})
}

func planLivenessTaskIDStrings(ids []pm.TaskID) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, len(ids))
	for i := range ids {
		out[i] = string(ids[i])
	}
	return out
}

func appendUniqueTaskIDs(base []pm.TaskID, more ...pm.TaskID) []pm.TaskID {
	seen := make(map[pm.TaskID]struct{}, len(base)+len(more))
	out := make([]pm.TaskID, 0, len(base)+len(more))
	for _, id := range base {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, id := range more {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
