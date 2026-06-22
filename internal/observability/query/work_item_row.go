package query

import (
	"strings"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// WorkItemRow is the shared agent-execution row VO. v2.14.0 F7 (issue I14):
// the AgentWorkItem model was removed and its responsibilities collapsed into
// pm.Task, so this row is now sourced from a pm.Task (was: agent_work_item
// projection). The JSON shape is PRESERVED — the Web Console fleet view and the
// per-agent work-items panel still render these field names — so the
// "work_item" naming survives in the wire contract even though the unit of work
// is now the Task itself (work_item_id == task_id).
//
// It is the SINGLE source of the execution-row shape across read surfaces: the
// fleet snapshot (FleetSnapshot.WorkItems) and the inspect/query verbs. One
// definition + one formatter keeps the surfaces byte-identical (no copy-drift).
type WorkItemRow struct {
	WorkItemID string `json:"work_item_id"`
	AgentID    string `json:"agent_id"`
	TaskID     string `json:"task_id,omitempty"`
	// TaskTitle + ProjectID are read-time enrichments (Home/AgentDetail show the
	// real task title + link to /projects/{project_id}/tasks/{task_id}). Populated
	// by the user-facing fleet snapshot (which already loads the pm task + project
	// to resolve org scope); admin inspect/query verbs leave them empty (omitempty).
	TaskTitle string `json:"task_title,omitempty"`
	// TaskOrgRef is the human org reference token ("T<n>") so the Worker Activity
	// feed shows "T<n> + title" instead of a raw "task-<id>". "" when the task or
	// its org-number can't be resolved (UI falls back to a clean #hash).
	TaskOrgRef        string `json:"task_org_ref,omitempty"`
	ProjectID         string `json:"project_id,omitempty"`
	Status            string `json:"status"`
	CurrentActivity   string `json:"current_activity,omitempty"`
	TotalToolCalls    int64  `json:"total_tool_calls"`
	TotalTokensInput  int64  `json:"total_tokens_input"`
	TotalTokensOutput int64  `json:"total_tokens_output"`
	// WorkingSeconds is 0 (no per-turn duration source on the Task model).
	WorkingSeconds int64  `json:"working_seconds"`
	LastActivityAt string `json:"last_activity_at,omitempty"`
}

// execStatusQueued/Active/WaitingInput are the legacy agent-execution status
// vocabulary the Web Console still renders. v2.14.0 F7 maps the pm.Task
// running/blocked annotation back onto these labels so the fleet UI keeps a
// stable contract after the AgentWorkItem statuses were retired.
const (
	execStatusQueued       = "queued"
	execStatusActive       = "active"
	execStatusWaitingInput = "waiting_input"
)

// taskExecStatus maps a pm.Task onto the legacy execution-status vocabulary:
//   - any blocked annotation (blocked_reason != "") → "waiting_input" (the task
//     is paused awaiting input or owner/PM intervention),
//   - running (unblocked) → "active",
//   - open/reopened (assigned, not yet started) → "queued".
//
// Terminal tasks are filtered out by callers, so they never reach this map.
func taskExecStatus(t *pm.Task) string {
	if t.BlockedReason() != "" {
		return execStatusWaitingInput
	}
	if t.Status() == pm.TaskRunning {
		return execStatusActive
	}
	return execStatusQueued
}

// agentMemberIDFromAssignee strips the "agent:" identity-ref prefix from a task
// assignee, yielding the business-layer agent member id the Web Console expects
// in agent_id. A non-agent (human) or empty assignee returns "" — callers skip
// such tasks (executions are agent work only).
func agentMemberIDFromAssignee(assignee pm.IdentityRef) string {
	const p = "agent:"
	s := string(assignee)
	if strings.HasPrefix(s, p) && len(s) > len(p) {
		return strings.TrimPrefix(s, p)
	}
	return ""
}

// taskExecutionRow builds the execution WorkItemRow from a pm.Task. It is pure
// (task→row) and carries NO org/project scoping — org-scoping is a filter
// concern owned by the caller (fleet resolves it via taskProjectOrg; inspect/
// query is global admin). work_item_id == task_id (the task IS the unit of agent
// work now); token/tool/duration metrics are 0 (no Task-model source); the
// blocked reason surfaces as current_activity so a paused row shows WHY.
func taskExecutionRow(t *pm.Task) WorkItemRow {
	return WorkItemRow{
		WorkItemID:      string(t.ID()),
		AgentID:         agentMemberIDFromAssignee(t.Assignee()),
		TaskID:          string(t.ID()),
		Status:          taskExecStatus(t),
		CurrentActivity: t.BlockedReason(),
		LastActivityAt:  t.UpdatedAt().UTC().Format(time.RFC3339Nano),
	}
}
