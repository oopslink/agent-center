package projectmanager

import (
	"errors"
	"fmt"
)

const PlanNodeNotRemovableCode = "plan_node_not_removable"

var ErrPlanNodeNotRemovable = errors.New("projectmanager: plan node is not removable")

type PlanNodeNotRemovableError struct {
	PlanID          PlanID
	NodeID          string
	TaskID          TaskID
	CurrentStatus   TaskStatus
	AllowedStatus   string
	HistoryBlockers []string
}

func (e *PlanNodeNotRemovableError) Error() string {
	return fmt.Sprintf("%s: plan_id=%s task_id=%s current_status=%s allowed_status=%s blockers=%v",
		ErrPlanNodeNotRemovable, e.PlanID, e.TaskID, e.CurrentStatus, e.AllowedStatus, e.HistoryBlockers)
}

func (e *PlanNodeNotRemovableError) Is(target error) bool {
	return target == ErrPlanNodeNotRemovable
}
