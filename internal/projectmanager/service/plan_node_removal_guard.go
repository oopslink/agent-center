package service

import (
	"context"
	"fmt"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// requirePendingPlanNodeRemovable is the shared legacy/batch guard for moving a
// structured-plan node back to the backlog. Incident dependency edges are current
// topology and may be removed in the same transaction; execution/review/gate facts
// are append-only history and permanently block removing the node itself.
func (s *Service) requirePendingPlanNodeRemovable(ctx context.Context, planID pm.PlanID, t *pm.Task, dispatched bool) error {
	if t.PlanID() != planID {
		return pm.ErrPlanProjectMismatch
	}
	if !pm.NodeMutable(t.Status(), dispatched) {
		return fmt.Errorf("%w: task %s", pm.ErrPlanNodeInFlight, t.ID())
	}
	if t.FollowsTaskID() != "" || t.OriginVerdictID() != "" {
		return fmt.Errorf("%w: task %s has immutable continuation history", pm.ErrPlanNodeInFlight, t.ID())
	}
	if s.actionLogs != nil {
		logs, err := s.actionLogs.ListByTask(ctx, t.ID())
		if err != nil {
			return err
		}
		if len(logs) != 0 {
			return fmt.Errorf("%w: task %s has action history", pm.ErrPlanNodeInFlight, t.ID())
		}
	} else if len(t.ActionLogs()) != 0 {
		return fmt.Errorf("%w: task %s has action history", pm.ErrPlanNodeInFlight, t.ID())
	}
	if s.audit != nil {
		has, err := s.hasImmutableTaskAudit(ctx, t.ID())
		if err != nil {
			return err
		}
		if has {
			return fmt.Errorf("%w: task %s has audit history", pm.ErrPlanNodeInFlight, t.ID())
		}
	}
	if s.remediation != nil {
		if _, found, err := s.remediation.FindVerdictByGate(ctx, t.ID()); err != nil {
			return err
		} else if found {
			return fmt.Errorf("%w: task %s has gate verdict history", pm.ErrPlanNodeInFlight, t.ID())
		}
	}
	return nil
}

func (s *Service) hasImmutableTaskAudit(ctx context.Context, taskID pm.TaskID) (bool, error) {
	cursor := ""
	for {
		entries, next, err := s.audit.ListByObject(ctx, pm.AuditObjectTask, string(taskID), cursor, 100)
		if err != nil {
			return false, err
		}
		for _, e := range entries {
			switch e.ChangeType {
			case pm.AuditTaskStatusChanged, pm.AuditTaskClaimed, pm.AuditTaskReviewVerdict:
				return true, nil
			}
		}
		if next == "" {
			return false, nil
		}
		cursor = next
	}
}
