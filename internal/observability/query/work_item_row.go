package query

import (
	"time"

	"github.com/oopslink/agent-center/internal/observability/projection"
)

// WorkItemRow is the shared work-item row VO (v2.7 #107: the new work-item
// model replaced the retired task-execution model — execution→work-item). It is
// the SINGLE source of the work-item row shape across read surfaces: the fleet
// snapshot (FleetSnapshot.WorkItems) and the inspect/query verbs (projections
// repoint). Keeping one definition + one formatter guarantees the three surfaces
// stay byte-identical (no copy-drift).
type WorkItemRow struct {
	WorkItemID        string `json:"work_item_id"`
	AgentID           string `json:"agent_id"`
	TaskID            string `json:"task_id,omitempty"`
	Status            string `json:"status"`
	CurrentActivity   string `json:"current_activity,omitempty"`
	TotalToolCalls    int64  `json:"total_tool_calls"`
	TotalTokensInput  int64  `json:"total_tokens_input"`
	TotalTokensOutput int64  `json:"total_tokens_output"`
	// WorkingSeconds is 0 in v2.7 (no per-turn duration source; Opt2 deferred v2.8).
	WorkingSeconds int64  `json:"working_seconds"`
	LastActivityAt string `json:"last_activity_at,omitempty"`
}

// workItemRowFromProjection builds a WorkItemRow from a live work-item
// projection plus the resolved task id. It is pure (projection→row) and carries
// NO org/project scoping — org-scoping is a filter concern owned by the caller
// (fleet resolves it via workItemTaskProjectOrg; inspect/query is global admin).
func workItemRowFromProjection(p *projection.AgentWorkItemProjection, taskID string) WorkItemRow {
	return WorkItemRow{
		WorkItemID:        p.WorkItemID,
		AgentID:           p.AgentID,
		TaskID:            taskID,
		Status:            p.Status,
		CurrentActivity:   p.CurrentActivity,
		TotalToolCalls:    p.TotalToolCalls,
		TotalTokensInput:  p.TotalTokensInput,
		TotalTokensOutput: p.TotalTokensOutput,
		WorkingSeconds:    p.WorkingSecondsAccumulated,
		LastActivityAt:    p.LastActivityAt.UTC().Format(time.RFC3339Nano),
	}
}
