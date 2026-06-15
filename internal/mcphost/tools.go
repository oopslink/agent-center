// tools.go (v2.7 b3-ii, ADR-0049) — the remaining OQ4 JSON passthrough tools
// completing the per-agent MCP surface on top of the locked b3-i form
// (server.go). Every handler follows the b3-i pattern EXACTLY: a typed In
// struct with NO agent_id field, a handler that injects the process-fixed
// agent_id from cfg.AgentID and forwards via callAdmin to the matching
// /admin/agent-tools/<tool> endpoint. The MCP tool name equals the admin
// route's <tool> segment (callAdmin POSTs to /admin/agent-tools/<tool>), so
// the registration name and the callAdmin tool string must stay in lockstep.
//
// Admin request field names are matched VERBATIM to the handlers in
// internal/admin/api/agent_tools_passthrough.go + agent_tools_write.go:
//   - assign_task / reassign_task : {task_id, assignee}
//   - subscribe / unsubscribe     : {task_id, identity?}  (defaults to self)
//   - request_input               : {task_id, question}
//   - block_task                  : {task_id, reason}
//   - complete_task               : {task_id, summary?}
//   - discard_task                : {task_id, reason?}
//   - create_task                 : {project_id, title, description?, derived_from_issue?}
//   - get_task                    : {task_id}
//   - get_issue                   : {issue_id}
//   - verify_task                 : {task_id}
package mcphost

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- get_task ----------------------------------------------------------------

type getTaskArgs struct {
	TaskID string `json:"task_id" jsonschema:"the task to read (the agent must own it)"`
}

func makeGetTask(cfg Config) mcp.ToolHandlerFor[getTaskArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args getTaskArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"task_id":  args.TaskID,
		}
		return callAdmin(ctx, cfg, "get_task", body)
	}
}

// --- list_tasks (v2.9.1 #T38) ------------------------------------------------

type listTasksArgs struct {
	ProjectID string   `json:"project_id" jsonschema:"the project whose tasks to list (required)"`
	Status    []string `json:"status,omitempty" jsonschema:"optional task statuses to include (e.g. open, running, completed); omit for all"`
	Assignee  string   `json:"assignee,omitempty" jsonschema:"optional assignee identity ref to filter by (agent:<id> / user:<id>)"`
}

// makeListTasks lists ALL tasks in a project (board overview), optionally filtered
// by status and/or assignee — fills the gap where get_my_work is self-only and
// list_plans only covers plan nodes.
func makeListTasks(cfg Config) mcp.ToolHandlerFor[listTasksArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args listTasksArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":   cfg.AgentID,
			"project_id": args.ProjectID,
		}
		if len(args.Status) > 0 {
			body["status"] = args.Status
		}
		if args.Assignee != "" {
			body["assignee"] = args.Assignee
		}
		return callAdmin(ctx, cfg, "list_tasks", body)
	}
}

// --- get_issue ---------------------------------------------------------------

type getIssueArgs struct {
	IssueID string `json:"issue_id" jsonschema:"the issue to read (the agent's own task must derive from it)"`
}

func makeGetIssue(cfg Config) mcp.ToolHandlerFor[getIssueArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args getIssueArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"issue_id": args.IssueID,
		}
		return callAdmin(ctx, cfg, "get_issue", body)
	}
}

// --- create_task -------------------------------------------------------------

type createTaskArgs struct {
	ProjectID        string `json:"project_id" jsonschema:"the project to create the task in"`
	Title            string `json:"title" jsonschema:"the task title"`
	Description      string `json:"description,omitempty" jsonschema:"optional task description"`
	DerivedFromIssue string `json:"derived_from_issue,omitempty" jsonschema:"optional id of the issue this task derives from"`
}

func makeCreateTask(cfg Config) mcp.ToolHandlerFor[createTaskArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args createTaskArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":           cfg.AgentID,
			"project_id":         args.ProjectID,
			"title":              args.Title,
			"description":        args.Description,
			"derived_from_issue": args.DerivedFromIssue,
		}
		return callAdmin(ctx, cfg, "create_task", body)
	}
}

// --- assign_task / reassign_task ---------------------------------------------

type assignTaskArgs struct {
	TaskID   string `json:"task_id" jsonschema:"the task to (re)assign"`
	Assignee string `json:"assignee" jsonschema:"the identity ref to assign to (e.g. agent:X or user:Y)"`
}

// makeAssignTask backs BOTH assign_task and reassign_task. The tool string
// MUST equal the admin route segment, so it is supplied explicitly.
func makeAssignTask(cfg Config, tool string) mcp.ToolHandlerFor[assignTaskArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args assignTaskArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"task_id":  args.TaskID,
			"assignee": args.Assignee,
		}
		return callAdmin(ctx, cfg, tool, body)
	}
}

// --- subscribe / unsubscribe -------------------------------------------------

type subscribeArgs struct {
	TaskID   string `json:"task_id" jsonschema:"the task to (un)subscribe"`
	Identity string `json:"identity,omitempty" jsonschema:"optional identity ref; defaults to the calling agent"`
}

// makeSubscribe backs BOTH subscribe and unsubscribe. The admin field is
// `identity` (not identity_id), optional — omitted defaults server-side to
// the agent's own ref.
func makeSubscribe(cfg Config, tool string) mcp.ToolHandlerFor[subscribeArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args subscribeArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"task_id":  args.TaskID,
			"identity": args.Identity,
		}
		return callAdmin(ctx, cfg, tool, body)
	}
}

// --- request_input -----------------------------------------------------------

type requestInputArgs struct {
	TaskID   string `json:"task_id" jsonschema:"the task to post the question into"`
	Question string `json:"question" jsonschema:"the question for the human; parks the agent's work item waiting_input"`
}

func makeRequestInput(cfg Config) mcp.ToolHandlerFor[requestInputArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args requestInputArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"task_id":  args.TaskID,
			"question": args.Question,
		}
		return callAdmin(ctx, cfg, "request_input", body)
	}
}

// --- start_work / fail_work (v2.8.1 #278 D pull model) -----------------------

type startWorkArgs struct {
	WorkItemID string `json:"work_item_id" jsonschema:"the id of one of YOUR queued work items (from get_my_work) to start working on now"`
}

func makeStartWork(cfg Config) mcp.ToolHandlerFor[startWorkArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args startWorkArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":     cfg.AgentID,
			"work_item_id": args.WorkItemID,
		}
		return callAdmin(ctx, cfg, "start_work", body)
	}
}

type failWorkArgs struct {
	WorkItemID string `json:"work_item_id" jsonschema:"the id of the work item you are currently running that has failed"`
}

func makeFailWork(cfg Config) mcp.ToolHandlerFor[failWorkArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args failWorkArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":     cfg.AgentID,
			"work_item_id": args.WorkItemID,
		}
		return callAdmin(ctx, cfg, "fail_work", body)
	}
}

// --- claim_task (T83: open-claim of a built-in assignment-pool task) ---------

type claimTaskArgs struct {
	TaskID string `json:"task_id" jsonschema:"the id of an OPEN assignment-pool task (from get_my_work claimable_tasks) to claim now — atomically assigns it to you and starts it (open→running)"`
}

func makeClaimTask(cfg Config) mcp.ToolHandlerFor[claimTaskArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args claimTaskArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"task_id":  args.TaskID,
		}
		return callAdmin(ctx, cfg, "claim_task", body)
	}
}

type listAssignmentPoolArgs struct{}

func makeListAssignmentPool(cfg Config) mcp.ToolHandlerFor[listAssignmentPoolArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ listAssignmentPoolArgs) (*mcp.CallToolResult, any, error) {
		return callAdmin(ctx, cfg, "list_assignment_pool", map[string]any{"agent_id": cfg.AgentID})
	}
}

// --- pause_work / resume_paused_work (v2.8.1 #278 D PR4 scheduling) -----------

type pauseWorkArgs struct {
	WorkItemID string `json:"work_item_id" jsonschema:"the id of your currently-running work item to pause (set aside)"`
	Reason     string `json:"reason" jsonschema:"a short reason you are pausing (for observability)"`
}

func makePauseWork(cfg Config) mcp.ToolHandlerFor[pauseWorkArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args pauseWorkArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":     cfg.AgentID,
			"work_item_id": args.WorkItemID,
			"reason":       args.Reason,
		}
		return callAdmin(ctx, cfg, "pause_work", body)
	}
}

type resumeWorkArgs struct {
	WorkItemID string `json:"work_item_id" jsonschema:"the id of a paused work item (from list_my_paused_work) to resume"`
}

func makeResumeWork(cfg Config) mcp.ToolHandlerFor[resumeWorkArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args resumeWorkArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":     cfg.AgentID,
			"work_item_id": args.WorkItemID,
		}
		return callAdmin(ctx, cfg, "resume_paused_work", body)
	}
}

type getMyActiveWorkArgs struct{}

func makeGetMyActiveWork(cfg Config) mcp.ToolHandlerFor[getMyActiveWorkArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ getMyActiveWorkArgs) (*mcp.CallToolResult, any, error) {
		return callAdmin(ctx, cfg, "get_my_active_work", map[string]any{"agent_id": cfg.AgentID})
	}
}

type listMyPausedWorkArgs struct{}

func makeListMyPausedWork(cfg Config) mcp.ToolHandlerFor[listMyPausedWorkArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ listMyPausedWorkArgs) (*mcp.CallToolResult, any, error) {
		return callAdmin(ctx, cfg, "list_my_paused_work", map[string]any{"agent_id": cfg.AgentID})
	}
}

// --- get_my_unread (v2.8.1 #278 D PR4b dual-stream) ------------------------

type getMyUnreadArgs struct{}

func makeGetMyUnread(cfg Config) mcp.ToolHandlerFor[getMyUnreadArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ getMyUnreadArgs) (*mcp.CallToolResult, any, error) {
		return callAdmin(ctx, cfg, "get_my_unread", map[string]any{"agent_id": cfg.AgentID})
	}
}

type markSeenArgs struct {
	ConversationID string `json:"conversation_id" jsonschema:"the conversation of the message you handled (from get_my_unread)"`
	MessageID      string `json:"message_id" jsonschema:"the id of the latest message you have handled in that conversation"`
}

func makeMarkSeen(cfg Config) mcp.ToolHandlerFor[markSeenArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args markSeenArgs) (*mcp.CallToolResult, any, error) {
		return callAdmin(ctx, cfg, "mark_seen", map[string]any{
			"agent_id":        cfg.AgentID,
			"conversation_id": args.ConversationID,
			"message_id":      args.MessageID,
		})
	}
}

// --- block_task --------------------------------------------------------------

type blockTaskArgs struct {
	TaskID string `json:"task_id" jsonschema:"the task to block"`
	Reason string `json:"reason" jsonschema:"why the task is blocked (required)"`
}

func makeBlockTask(cfg Config) mcp.ToolHandlerFor[blockTaskArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args blockTaskArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"task_id":  args.TaskID,
			"reason":   args.Reason,
		}
		return callAdmin(ctx, cfg, "block_task", body)
	}
}

// --- unblock_task / rerun_failed_node (v2.9.1 P0 recovery) -------------------

type unblockTaskArgs struct {
	TaskID string `json:"task_id" jsonschema:"the blocked task to recover"`
}

// makeUnblockTask recovers a blocked task: blocked→running + a fresh re-dispatch
// (re-wakes the assignee). Recovery for a task stuck blocked after a
// restart/stale-release (reason "agent execution failed").
func makeUnblockTask(cfg Config) mcp.ToolHandlerFor[unblockTaskArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args unblockTaskArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"task_id":  args.TaskID,
		}
		return callAdmin(ctx, cfg, "unblock_task", body)
	}
}

type rerunFailedNodeArgs struct {
	PlanID string `json:"plan_id" jsonschema:"the plan the node belongs to"`
	TaskID string `json:"task_id" jsonschema:"the plan node's task to re-run"`
}

// makeRerunFailedNode clears a plan node's dispatch record so the next plan
// advance re-dispatches it — plan-aware recovery for a stuck node.
func makeRerunFailedNode(cfg Config) mcp.ToolHandlerFor[rerunFailedNodeArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args rerunFailedNodeArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"plan_id":  args.PlanID,
			"task_id":  args.TaskID,
		}
		return callAdmin(ctx, cfg, "rerun_failed_node", body)
	}
}

type resumePausedNodeArgs struct {
	PlanID string `json:"plan_id" jsonschema:"the plan the node belongs to"`
	TaskID string `json:"task_id" jsonschema:"the paused plan node's task to resume"`
}

// makeResumePausedNode resumes a plan node whose agent PAUSED its work item and
// went idle (the node shows `paused`): it resumes the node's work item and wakes
// the agent so it continues. Operator recovery — distinct from rerun_failed_node
// (which is for a FAILED/undispatched node).
func makeResumePausedNode(cfg Config) mcp.ToolHandlerFor[resumePausedNodeArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args resumePausedNodeArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"plan_id":  args.PlanID,
			"task_id":  args.TaskID,
		}
		return callAdmin(ctx, cfg, "resume_paused_node", body)
	}
}

// --- complete_task -----------------------------------------------------------

type completeTaskArgs struct {
	TaskID  string `json:"task_id" jsonschema:"the task to complete"`
	Summary string `json:"summary,omitempty" jsonschema:"optional completion summary posted to the task"`
}

func makeCompleteTask(cfg Config) mcp.ToolHandlerFor[completeTaskArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args completeTaskArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"task_id":  args.TaskID,
			"summary":  args.Summary,
		}
		return callAdmin(ctx, cfg, "complete_task", body)
	}
}

// --- discard_task (T119) -----------------------------------------------------

type discardTaskArgs struct {
	TaskID string `json:"task_id" jsonschema:"the task to discard (terminal)"`
	Reason string `json:"reason,omitempty" jsonschema:"optional reason posted to the task before discarding"`
}

// makeDiscardTask terminally discards a NON-terminal task (open/running →
// discarded) — the correct way to retire a superseded or mis-created task. Unlike
// complete_task it does not mark the work done (status shows Discarded, not
// Completed), and unlike block_task it does not leave a pool task to be re-dispatched.
func makeDiscardTask(cfg Config) mcp.ToolHandlerFor[discardTaskArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args discardTaskArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"task_id":  args.TaskID,
			"reason":   args.Reason,
		}
		return callAdmin(ctx, cfg, "discard_task", body)
	}
}

// --- Plan tools (v2.9 P3 Stage C, #285) --------------------------------------
//
// These mirror the admin Plan agent-tool handlers in
// internal/admin/api/agent_tools_plans.go VERBATIM: the MCP tool name equals
// the admin route segment (callAdmin POSTs to /admin/agent-tools/<tool>), and
// each body key matches the handler's decode struct EXACTLY:
//   - create_plan          : {project_id, name, description?, target_date?}
//   - add_task_to_plan      : {plan_id, task_id}
//   - remove_task_from_plan : {plan_id, task_id}
//   - add_plan_dependency    : {plan_id, from_task_id, to_task_id}
//   - remove_plan_dependency : {plan_id, from_task_id, to_task_id}
//   - start_plan / stop_plan : {plan_id}
//   - delete_plan / archive_plan : {plan_id}
//   - get_plan               : {project_id, plan_id}
//   - list_plans             : {project_id}
// As everywhere in this file, agent_id is process-fixed (injected from cfg,
// NEVER from args) so a PM-agent cannot drive another agent's plans.

// --- create_plan -------------------------------------------------------------

type createPlanArgs struct {
	ProjectID   string `json:"project_id" jsonschema:"the project to create the plan in (you must be a member)"`
	Name        string `json:"name" jsonschema:"the plan name"`
	Description string `json:"description,omitempty" jsonschema:"optional plan description"`
	TargetDate  string `json:"target_date,omitempty" jsonschema:"optional target date, RFC3339 (e.g. 2026-06-30T00:00:00Z)"`
}

func makeCreatePlan(cfg Config) mcp.ToolHandlerFor[createPlanArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args createPlanArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":    cfg.AgentID,
			"project_id":  args.ProjectID,
			"name":        args.Name,
			"description": args.Description,
			"target_date": args.TargetDate,
		}
		return callAdmin(ctx, cfg, "create_plan", body)
	}
}

// --- add_task_to_plan / remove_task_from_plan --------------------------------

type planTaskArgs struct {
	PlanID string `json:"plan_id" jsonschema:"the draft plan to modify"`
	TaskID string `json:"task_id" jsonschema:"the task to add to / remove from the plan"`
}

// makePlanTask backs BOTH add_task_to_plan and remove_task_from_plan. The tool
// string MUST equal the admin route segment, so it is supplied explicitly.
func makePlanTask(cfg Config, tool string) mcp.ToolHandlerFor[planTaskArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args planTaskArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"plan_id":  args.PlanID,
			"task_id":  args.TaskID,
		}
		return callAdmin(ctx, cfg, tool, body)
	}
}

// --- add_plan_dependency / remove_plan_dependency ----------------------------

type planDepArgs struct {
	PlanID     string `json:"plan_id" jsonschema:"the draft plan whose dependency DAG to modify"`
	FromTaskID string `json:"from_task_id" jsonschema:"the dependent task (it depends_on to_task_id)"`
	ToTaskID   string `json:"to_task_id" jsonschema:"the prerequisite task that must finish first"`
}

// makePlanDep backs BOTH add_plan_dependency and remove_plan_dependency. The
// tool string MUST equal the admin route segment, so it is supplied explicitly.
func makePlanDep(cfg Config, tool string) mcp.ToolHandlerFor[planDepArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args planDepArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":     cfg.AgentID,
			"plan_id":      args.PlanID,
			"from_task_id": args.FromTaskID,
			"to_task_id":   args.ToTaskID,
		}
		return callAdmin(ctx, cfg, tool, body)
	}
}

// --- start_plan / stop_plan / delete_plan / archive_plan ---------------------

type planIDArgs struct {
	PlanID string `json:"plan_id" jsonschema:"the plan to operate on"`
}

// makePlanID backs the single-plan-id tools (start_plan, stop_plan,
// delete_plan, archive_plan). The tool string MUST equal the admin route
// segment, so it is supplied explicitly.
func makePlanID(cfg Config, tool string) mcp.ToolHandlerFor[planIDArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args planIDArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"plan_id":  args.PlanID,
		}
		return callAdmin(ctx, cfg, tool, body)
	}
}

// --- get_plan ----------------------------------------------------------------

type getPlanArgs struct {
	ProjectID string `json:"project_id" jsonschema:"the project the plan belongs to (scopes the read)"`
	PlanID    string `json:"plan_id" jsonschema:"the plan to read"`
}

func makeGetPlan(cfg Config) mcp.ToolHandlerFor[getPlanArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args getPlanArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":   cfg.AgentID,
			"project_id": args.ProjectID,
			"plan_id":    args.PlanID,
		}
		return callAdmin(ctx, cfg, "get_plan", body)
	}
}

// --- list_plans --------------------------------------------------------------

type listPlansArgs struct {
	ProjectID string `json:"project_id" jsonschema:"the project whose plans to list"`
}

func makeListPlans(cfg Config) mcp.ToolHandlerFor[listPlansArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args listPlansArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":   cfg.AgentID,
			"project_id": args.ProjectID,
		}
		return callAdmin(ctx, cfg, "list_plans", body)
	}
}
