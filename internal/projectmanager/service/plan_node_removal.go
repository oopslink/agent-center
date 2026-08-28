package service

import (
	"context"
	"fmt"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// requireRemovablePendingPlanNode is the destructive node-removal gate. A node can
// leave a pending/open DAG only while it is still a never-dispatched, never-started
// candidate: open status, no current dispatch record, and no persisted lifecycle
// history. Any running/terminal/parked/reset/reopened/history-bearing task is
// retained permanently; follow-up work must use the immutable generation/remediation
// paths rather than deleting history out of the plan.
func (s *Service) requireRemovablePendingPlanNode(ctx context.Context, planID pm.PlanID, taskID pm.TaskID, t *pm.Task, dispatched bool) error {
	if t == nil || t.PlanID() != planID {
		return fmt.Errorf("%w: task %s", pm.ErrPlanProjectMismatch, taskID)
	}
	if t.Status() != pm.TaskOpen || dispatched {
		return fmt.Errorf("%w: task %s", pm.ErrPlanNodeInFlight, taskID)
	}
	if s.actionLogs == nil {
		return nil
	}
	logs, _, err := s.actionLogs.ListByTaskPage(ctx, taskID, 0, 1)
	if err != nil {
		return err
	}
	if len(logs) > 0 {
		return fmt.Errorf("%w: task %s has lifecycle history", pm.ErrPlanNodeInFlight, taskID)
	}
	return nil
}
