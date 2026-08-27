package service

import (
	"fmt"
	"sort"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

const progressFreshnessThreshold = 15 * time.Minute

func (s *Service) fillProgressControl(detail *PlanDetail) {
	if detail == nil || detail.Plan == nil {
		return
	}
	now := s.clock.Now().UTC()
	pc := pm.ProgressControl{
		AsOf:                now,
		Health:              pm.ProgressHealthHealthy,
		Freshness:           pm.ProgressFreshness{State: pm.ProgressFreshnessFresh, Threshold: progressFreshnessThreshold, Source: "projectmanager.plan_detail"},
		Decision:            pm.ProgressDecisionVerified,
		ObservationVectorID: fmt.Sprintf("plan-progress:%s:%d", detail.Plan.ID(), detail.Plan.Version()),
		Quality:             pm.ProgressQualityValid,
	}
	taskAssignee := make(map[pm.TaskID]pm.IdentityRef, len(detail.Tasks))
	for _, task := range detail.Tasks {
		taskAssignee[task.ID()] = task.Assignee()
	}
	for _, node := range detail.View.Nodes {
		pc.Coverage.TotalNodes++
		switch node.NodeStatus {
		case pm.NodeDone, pm.NodeFailed, pm.NodeReady:
			pc.Coverage.VerifiedProgressNodes++
			pc.Coverage.ClassifiedNodes++
		case pm.NodeRunning, pm.NodePaused, pm.NodeDispatched:
			pc.Coverage.VerifiedProgressNodes++
			pc.Coverage.ValidInFlightNodes++
			pc.Coverage.ClassifiedNodes++
			pc.ValidInFlight = append(pc.ValidInFlight, pm.ProgressInFlight{
				TaskID:      node.TaskID,
				Status:      node.NodeStatus,
				AssigneeRef: taskAssignee[node.TaskID],
				StartedAt:   node.DispatchedAt,
				Quality:     pm.ProgressQualityValid,
				Source:      "plan_view",
			})
		}
	}
	sort.Slice(pc.ValidInFlight, func(i, j int) bool { return pc.ValidInFlight[i].TaskID < pc.ValidInFlight[j].TaskID })

	for _, b := range detail.BlockedOn {
		pc.Coverage.BlockedOnRowsObserved++
		pc.Coverage.ClassifiedNodes++
		sourceRef := fmt.Sprintf("blocked_on:%s:%s", detail.Plan.ID(), b.TaskID)
		lag := durationSince(now, b.WaitedSince)
		if lag > pc.Freshness.WatermarkLag {
			pc.Freshness.WatermarkLag = lag
		}
		if b.Deadline.IsZero() || b.OnTimeout == "" {
			pc.Coverage.CannotDetermineNodes++
			pc.Coverage.SuspectNodes++
			pc.Coverage.MissingDeadlineHolds++
			pc.Decision = pm.ProgressDecisionCannotDetermine
			pc.Health = pm.ProgressHealthDegraded
			pc.Quality = pm.ProgressQualitySuspect
			pc.Freshness.State = pm.ProgressFreshnessDegraded
			incidentID := fmt.Sprintf("incident:%s:%s:classification_unknown", detail.Plan.ID(), b.TaskID)
			holdID := fmt.Sprintf("hold:%s:%s:classification_unknown", detail.Plan.ID(), b.TaskID)
			incident := pm.ProgressIncident{
				ID:                   incidentID,
				PlanID:               detail.Plan.ID(),
				TaskID:               b.TaskID,
				Kind:                 "progress_classification_unknown",
				OwnerRef:             pm.IdentityRef("service:projectmanager"),
				OwnerDisplay:         "ProjectManager on-call",
				DeadlineAt:           now.Add(progressFreshnessThreshold),
				AckRequired:          true,
				EscalateToRef:        pm.IdentityRef("role:project-owner"),
				EscalationDeadlineAt: now.Add(progressFreshnessThreshold),
				SourceFactRefs:       []string{sourceRef},
				Status:               "open",
				CreatedAt:            firstNonZeroTime(b.WaitedSince, now),
				UpdatedAt:            now,
			}
			pc.OpenIncidents = append(pc.OpenIncidents, incident)
			hold := progressHoldFromBlockedOn(detail.Plan.ID(), b, holdID, "incident", incidentID, now, progressFreshnessThreshold)
			pc.OpenHolds = append(pc.OpenHolds, hold)
			pc.RequiredActions = append(pc.RequiredActions, pm.ProgressRequiredAction{
				ID:         "required:" + incidentID,
				Kind:       pm.ProgressAttentionIncident,
				Action:     "repair_progress_classification",
				SubjectID:  string(b.TaskID),
				NodeID:     b.NodeID,
				OwnerRef:   incident.OwnerRef,
				DeadlineAt: incident.DeadlineAt,
				HoldID:     holdID,
				IncidentID: incidentID,
				Severity:   "critical",
				Source:     sourceRef,
				Summary:    "Classification source is missing the required owner/deadline discipline; progress cannot be determined.",
			})
			continue
		}
		pc.Coverage.ResponsibilityNodes++
		if pc.Decision != pm.ProgressDecisionCannotDetermine {
			pc.Decision = pm.ProgressDecisionResponsibility
			pc.Health = pm.ProgressHealthAttention
		}
		obligationID := fmt.Sprintf("obligation:%s:%s:%s", detail.Plan.ID(), b.TaskID, b.WaitType)
		holdID := fmt.Sprintf("hold:%s:%s:%s", detail.Plan.ID(), b.TaskID, b.WaitType)
		owner := taskAssignee[b.TaskID]
		if owner == "" {
			owner = pm.IdentityRef("role:project-owner")
		}
		obligation := pm.ProgressObligation{
			ID:                   obligationID,
			PlanID:               detail.Plan.ID(),
			TaskID:               b.TaskID,
			Kind:                 obligationKindForWaitType(b.WaitType),
			OwnerRef:             owner,
			OwnerDisplay:         string(owner),
			DeadlineAt:           b.Deadline.UTC(),
			AckRequired:          true,
			EscalateToRef:        pm.IdentityRef("role:project-owner"),
			EscalationDeadlineAt: b.Deadline.UTC(),
			SourceFactRefs:       []string{sourceRef},
			Status:               "open",
			CreatedAt:            firstNonZeroTime(b.WaitedSince, now),
			UpdatedAt:            now,
		}
		pc.OpenObligations = append(pc.OpenObligations, obligation)
		hold := progressHoldFromBlockedOn(detail.Plan.ID(), b, holdID, "obligation", obligationID, now, b.Deadline.Sub(firstNonZeroTime(b.WaitedSince, now)))
		pc.OpenHolds = append(pc.OpenHolds, hold)
		action := pm.ProgressRequiredAction{
			ID:           "required:" + obligationID,
			Kind:         pm.ProgressAttentionObligation,
			Action:       obligation.Kind,
			SubjectID:    string(b.TaskID),
			NodeID:       b.NodeID,
			OwnerRef:     owner,
			DeadlineAt:   b.Deadline.UTC(),
			AckRequired:  true,
			HoldID:       holdID,
			ObligationID: obligationID,
			Severity:     severityForDeadline(now, b.Deadline),
			Source:       sourceRef,
			Summary:      requiredActionSummary(b.WaitType),
		}
		pc.RequiredActions = append(pc.RequiredActions, action)
		if now.After(b.Deadline) && pc.Freshness.State == pm.ProgressFreshnessFresh {
			pc.Freshness.State = pm.ProgressFreshnessStale
		}
	}
	pc.Coverage.OpenObligations = len(pc.OpenObligations)
	pc.Coverage.OpenIncidents = len(pc.OpenIncidents)
	pc.Coverage.OpenHolds = len(pc.OpenHolds)
	pc.PrimaryAttention = primaryRequiredAction(pc.RequiredActions)
	detail.ProgressControl = &pc
}

func progressHoldFromBlockedOn(planID pm.PlanID, b pm.BlockedOn, holdID, reasonKind, reasonID string, now time.Time, maxDuration time.Duration) pm.ProgressHold {
	started := firstNonZeroTime(b.WaitedSince, now)
	deadline := b.Deadline.UTC()
	if deadline.IsZero() {
		deadline = now.Add(progressFreshnessThreshold)
	}
	if maxDuration <= 0 {
		maxDuration = deadline.Sub(started)
	}
	return pm.ProgressHold{
		ID:                               holdID,
		PlanID:                           planID,
		TaskID:                           b.TaskID,
		ReasonKind:                       reasonKind,
		ReasonID:                         reasonID,
		BlocksNewDispatch:                true,
		BlocksGatePassToken:              true,
		BlocksDestructiveDownstreamStart: true,
		InFlightPolicy:                   "do_not_kill_unproven_execution",
		HoldAckDeadline:                  deadline,
		MaxHoldDuration:                  maxDuration,
		StartedAt:                        started,
		DeadlineAt:                       deadline,
		Age:                              durationSince(now, started),
		DeadlineRemaining:                deadline.Sub(now),
	}
}

func obligationKindForWaitType(w pm.WaitType) string {
	switch w {
	case pm.WaitAcceptanceVerdict, pm.WaitStageBarrier, pm.WaitHumanDecision:
		return "acceptance_verdict"
	case pm.WaitExecutorLiveness:
		return "produce_delivery"
	case pm.WaitExternalEvent:
		return "source_recovery"
	default:
		return "human_decision"
	}
}

func requiredActionSummary(w pm.WaitType) string {
	switch w {
	case pm.WaitAcceptanceVerdict:
		return "Record an authoritative acceptance verdict for the exact subject."
	case pm.WaitStageBarrier:
		return "Resolve the stage gate with an authoritative verdict."
	case pm.WaitHumanDecision:
		return "Provide the required human decision."
	case pm.WaitExecutorLiveness:
		return "Produce durable execution or delivery evidence, or escalate liveness."
	case pm.WaitExternalEvent:
		return "Recover the missing external source signal."
	case pm.WaitUpstreamCompletion:
		return "Complete or explicitly reclassify the upstream responsibility."
	default:
		return "Bind the blocked node to an owner action before its deadline."
	}
}

func severityForDeadline(now, deadline time.Time) string {
	if deadline.IsZero() {
		return "critical"
	}
	if !now.Before(deadline) {
		return "critical"
	}
	if deadline.Sub(now) <= time.Hour {
		return "warning"
	}
	return "attention"
}

func primaryRequiredAction(actions []pm.ProgressRequiredAction) *pm.ProgressRequiredAction {
	if len(actions) == 0 {
		return nil
	}
	best := actions[0]
	rank := func(a pm.ProgressRequiredAction) int {
		switch a.Severity {
		case "critical":
			return 0
		case "warning":
			return 1
		default:
			return 2
		}
	}
	for _, a := range actions[1:] {
		if rank(a) < rank(best) || (rank(a) == rank(best) && !a.DeadlineAt.IsZero() && (best.DeadlineAt.IsZero() || a.DeadlineAt.Before(best.DeadlineAt))) {
			best = a
		}
	}
	return &best
}

func firstNonZeroTime(a, fallback time.Time) time.Time {
	if a.IsZero() {
		return fallback.UTC()
	}
	return a.UTC()
}

func durationSince(now, since time.Time) time.Duration {
	if since.IsZero() {
		return 0
	}
	d := now.Sub(since.UTC())
	if d < 0 {
		return 0
	}
	return d
}
