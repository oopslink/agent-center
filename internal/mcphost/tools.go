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
//   - create_task                 : {project_id, title, description?, derived_from_issue?, assignee?, dispatch?}
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

// --- create_issue (v2.10.3 T170) ---------------------------------------------

type createIssueArgs struct {
	ProjectID   string   `json:"project_id" jsonschema:"the project to create the issue in (you must be a member)"`
	Title       string   `json:"title" jsonschema:"the issue title"`
	Description string   `json:"description,omitempty" jsonschema:"optional issue body/description"`
	Tags        []string `json:"tags,omitempty" jsonschema:"optional labels (each 1..16 chars, up to 10)"`
}

func makeCreateIssue(cfg Config) mcp.ToolHandlerFor[createIssueArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args createIssueArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":    cfg.AgentID,
			"project_id":  args.ProjectID,
			"title":       args.Title,
			"description": args.Description,
		}
		if len(args.Tags) > 0 {
			body["tags"] = args.Tags
		}
		return callAdmin(ctx, cfg, "create_issue", body)
	}
}

// --- update_issue (v2.10.3 T170) ---------------------------------------------

// updateIssueArgs uses pointers so an OMITTED field is left unchanged — only the
// fields you set are patched (title/description/status/tags), all-or-none.
type updateIssueArgs struct {
	IssueID     string    `json:"issue_id" jsonschema:"the issue to edit"`
	Title       *string   `json:"title,omitempty" jsonschema:"new title (omit to leave unchanged)"`
	Description *string   `json:"description,omitempty" jsonschema:"new description (omit to leave unchanged)"`
	Status      *string   `json:"status,omitempty" jsonschema:"new status: open|in_progress|resolved|closed|discarded|reopened (omit to leave unchanged)"`
	Tags        *[]string `json:"tags,omitempty" jsonschema:"replacement label set (omit to leave unchanged; pass [] to clear)"`
}

func makeUpdateIssue(cfg Config) mcp.ToolHandlerFor[updateIssueArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args updateIssueArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"issue_id": args.IssueID,
		}
		if args.Title != nil {
			body["title"] = *args.Title
		}
		if args.Description != nil {
			body["description"] = *args.Description
		}
		if args.Status != nil {
			body["status"] = *args.Status
		}
		if args.Tags != nil {
			body["tags"] = *args.Tags
		}
		return callAdmin(ctx, cfg, "update_issue", body)
	}
}

// --- close_issue / reopen_issue (v2.10.3 T170) -------------------------------

type issueIDArgs struct {
	IssueID string `json:"issue_id" jsonschema:"the issue to act on"`
}

// makeIssueID backs the single-issue-id tools (close_issue, reopen_issue). The
// tool string MUST equal the admin route segment, so it is supplied explicitly.
func makeIssueID(cfg Config, tool string) mcp.ToolHandlerFor[issueIDArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args issueIDArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"issue_id": args.IssueID,
		}
		return callAdmin(ctx, cfg, tool, body)
	}
}

// --- post_issue_message (v2.10.3 T170) ---------------------------------------

type postIssueMessageArgs struct {
	IssueID string `json:"issue_id" jsonschema:"the issue to comment on"`
	Text    string `json:"text" jsonschema:"the comment text (@mention a participant by name to notify them)"`
	// ParentMessageID (v2.9.1 Thread F4): set to reply IN a thread; omit for a top-level comment.
	ParentMessageID string `json:"parent_message_id,omitempty" jsonschema:"to reply inside a thread, the thread root message id; omit for a top-level comment"`
}

func makePostIssueMessage(cfg Config) mcp.ToolHandlerFor[postIssueMessageArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args postIssueMessageArgs) (*mcp.CallToolResult, any, error) {
		// Model-facing arg is "text"; the admin endpoint reads "content".
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"issue_id": args.IssueID,
			"content":  args.Text,
		}
		if args.ParentMessageID != "" {
			body["parent_message_id"] = args.ParentMessageID
		}
		return callAdmin(ctx, cfg, "post_issue_message", body)
	}
}

// --- list_issues (v2.10.3 T170) ----------------------------------------------

type listIssuesArgs struct {
	ProjectID string   `json:"project_id" jsonschema:"the project whose issues to list (required; you must be a member)"`
	Status    []string `json:"status,omitempty" jsonschema:"optional issue statuses to include (e.g. open, in_progress, resolved); omit for all"`
	Author    string   `json:"author,omitempty" jsonschema:"optional author identity ref to filter by (agent:<id> / user:<id>)"`
}

func makeListIssues(cfg Config) mcp.ToolHandlerFor[listIssuesArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args listIssuesArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":   cfg.AgentID,
			"project_id": args.ProjectID,
		}
		if len(args.Status) > 0 {
			body["status"] = args.Status
		}
		if args.Author != "" {
			body["author"] = args.Author
		}
		return callAdmin(ctx, cfg, "list_issues", body)
	}
}

// --- list_tasks_of_issue (v2.10.3 T170) --------------------------------------

func makeListTasksOfIssue(cfg Config) mcp.ToolHandlerFor[issueIDArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args issueIDArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"issue_id": args.IssueID,
		}
		return callAdmin(ctx, cfg, "list_tasks_of_issue", body)
	}
}

// --- create_task -------------------------------------------------------------

type createTaskArgs struct {
	ProjectID        string `json:"project_id" jsonschema:"the project to create the task in"`
	Title            string `json:"title" jsonschema:"the task title"`
	Description      string `json:"description,omitempty" jsonschema:"optional task description"`
	DerivedFromIssue string `json:"derived_from_issue,omitempty" jsonschema:"optional id of the issue this task derives from"`
	// T199/WS3: one-step create→dispatch. Omit both to leave the task in the
	// backlog (the pre-T199 default).
	Assignee string `json:"assignee,omitempty" jsonschema:"optional identity ref to assign on create (e.g. agent:X or user:Y); emits the work item + wakes an agent assignee"`
	Dispatch bool   `json:"dispatch,omitempty" jsonschema:"when true, also dispatch the task into the project's assignment pool so it is immediately claimable (unassigned) / runnable (assigned) — no separate add_task_to_plan needed"`
}

func makeCreateTask(cfg Config) mcp.ToolHandlerFor[createTaskArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args createTaskArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":           cfg.AgentID,
			"project_id":         args.ProjectID,
			"title":              args.Title,
			"description":        args.Description,
			"derived_from_issue": args.DerivedFromIssue,
			"assignee":           args.Assignee,
			"dispatch":           args.Dispatch,
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

// --- start_task / fail_task (v2.8.1 #278 D pull model) -----------------------

type startWorkArgs struct {
	WorkItemID string `json:"work_item_id" jsonschema:"the id of one of YOUR queued work items (from get_my_work) to start working on now"`
}

func makeStartTask(cfg Config) mcp.ToolHandlerFor[startWorkArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args startWorkArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":     cfg.AgentID,
			"work_item_id": args.WorkItemID,
		}
		return callAdmin(ctx, cfg, "start_task", body)
	}
}

type failWorkArgs struct {
	WorkItemID string `json:"work_item_id" jsonschema:"the id of the work item you are currently running that has failed"`
}

func makeFailTask(cfg Config) mcp.ToolHandlerFor[failWorkArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args failWorkArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":     cfg.AgentID,
			"work_item_id": args.WorkItemID,
		}
		return callAdmin(ctx, cfg, "fail_task", body)
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

// --- pause_task / resume_task (v2.8.1 #278 D PR4 scheduling) -----------

type pauseWorkArgs struct {
	WorkItemID string `json:"work_item_id" jsonschema:"the id of your currently-running work item to pause (set aside)"`
	Reason     string `json:"reason" jsonschema:"a short reason you are pausing (for observability)"`
}

func makePauseTask(cfg Config) mcp.ToolHandlerFor[pauseWorkArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args pauseWorkArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":     cfg.AgentID,
			"work_item_id": args.WorkItemID,
			"reason":       args.Reason,
		}
		return callAdmin(ctx, cfg, "pause_task", body)
	}
}

type resumeWorkArgs struct {
	WorkItemID string `json:"work_item_id" jsonschema:"the id of a paused work item (from get_my_work's paused bucket) to resume"`
}

func makeResumeTask(cfg Config) mcp.ToolHandlerFor[resumeWorkArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args resumeWorkArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":     cfg.AgentID,
			"work_item_id": args.WorkItemID,
		}
		return callAdmin(ctx, cfg, "resume_task", body)
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

// --- set_task_issue (T192) ---------------------------------------------------

type setTaskIssueArgs struct {
	TaskID  string `json:"task_id" jsonschema:"the task whose derived_from_issue link to (re)set or clear"`
	IssueID string `json:"issue_id" jsonschema:"the issue id to link as derived_from_issue; pass an empty string to CLEAR the link. The issue must exist and be in the task's project."`
}

// makeSetTaskIssue (T192) backs set_task_issue — (re)set or CLEAR a task's
// derived_from_issue AFTER creation (the link used to be create-only). Authorized
// by the relaxed task-access gate (creator / project member / own-work), same as
// discard_task — no WorkItem required.
func makeSetTaskIssue(cfg Config) mcp.ToolHandlerFor[setTaskIssueArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args setTaskIssueArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"task_id":  args.TaskID,
			"issue_id": args.IssueID,
		}
		return callAdmin(ctx, cfg, "set_task_issue", body)
	}
}

// --- reminder tools (T206, Cognition BC) -------------------------------------

type reminderScheduleArg struct {
	Kind     string `json:"kind" jsonschema:"once or cron"`
	OnceAt   string `json:"once_at,omitempty" jsonschema:"RFC3339 time for a one-shot (kind=once)"`
	CronExpr string `json:"cron_expr,omitempty" jsonschema:"5-field cron expression (kind=cron)"`
	Timezone string `json:"timezone,omitempty" jsonschema:"IANA timezone for the cron (e.g. Asia/Shanghai); default UTC"`
}

type reminderEndArg struct {
	Kind     string `json:"kind,omitempty" jsonschema:"never (default) | until | max_count (recurring only)"`
	Until    string `json:"until,omitempty" jsonschema:"RFC3339 cutoff (kind=until)"`
	MaxCount int    `json:"max_count,omitempty" jsonschema:"max fire count (kind=max_count)"`
}

type createReminderArgs struct {
	RemindeeAgentID string              `json:"remindee_agent_id" jsonschema:"the agent to remind (must be in your project; owner may cross projects)"`
	Schedule        reminderScheduleArg `json:"schedule" jsonschema:"when to fire — once{once_at} or cron{cron_expr,timezone}"`
	Content         string              `json:"content" jsonschema:"the reminder text injected to the remindee when it fires"`
	SkipIfOverlap   *bool               `json:"skip_if_overlap,omitempty" jsonschema:"skip a fire if the previous one is still being handled (default true)"`
	EndCondition    reminderEndArg      `json:"end_condition,omitempty" jsonschema:"when a recurring reminder stops (never|until|max_count)"`
}

func makeCreateReminder(cfg Config) mcp.ToolHandlerFor[createReminderArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args createReminderArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":          cfg.AgentID,
			"remindee_agent_id": args.RemindeeAgentID,
			"schedule":          args.Schedule,
			"content":           args.Content,
			"end_condition":     args.EndCondition,
		}
		if args.SkipIfOverlap != nil {
			body["skip_if_overlap"] = *args.SkipIfOverlap
		}
		return callAdmin(ctx, cfg, "create_reminder", body)
	}
}

type listRemindersArgs struct {
	CreatorRef      string   `json:"creator_ref,omitempty" jsonschema:"filter by creator ref; default = reminders YOU created"`
	RemindeeAgentID string   `json:"remindee_agent_id,omitempty" jsonschema:"filter by remindee agent instead of creator"`
	Statuses        []string `json:"statuses,omitempty" jsonschema:"optional status filter: active|paused|completed|canceled"`
}

func makeListReminders(cfg Config) mcp.ToolHandlerFor[listRemindersArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args listRemindersArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{"agent_id": cfg.AgentID}
		if args.CreatorRef != "" {
			body["creator_ref"] = args.CreatorRef
		}
		if args.RemindeeAgentID != "" {
			body["remindee_agent_id"] = args.RemindeeAgentID
		}
		if len(args.Statuses) > 0 {
			body["statuses"] = args.Statuses
		}
		return callAdmin(ctx, cfg, "list_reminders", body)
	}
}

type getReminderArgs struct {
	ReminderID string `json:"reminder_id" jsonschema:"the reminder to read"`
}

func makeGetReminder(cfg Config) mcp.ToolHandlerFor[getReminderArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args getReminderArgs) (*mcp.CallToolResult, any, error) {
		return callAdmin(ctx, cfg, "get_reminder", map[string]any{"agent_id": cfg.AgentID, "reminder_id": args.ReminderID})
	}
}

type updateReminderArgs struct {
	ReminderID string               `json:"reminder_id" jsonschema:"the reminder to update"`
	Action     string               `json:"action" jsonschema:"pause | resume | cancel | edit"`
	Schedule   *reminderScheduleArg `json:"schedule,omitempty" jsonschema:"new schedule (action=edit)"`
	Content    string               `json:"content,omitempty" jsonschema:"new content (action=edit; empty leaves unchanged)"`
}

func makeUpdateReminder(cfg Config) mcp.ToolHandlerFor[updateReminderArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args updateReminderArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":    cfg.AgentID,
			"reminder_id": args.ReminderID,
			"action":      args.Action,
		}
		if args.Schedule != nil {
			body["schedule"] = *args.Schedule
		}
		if args.Content != "" {
			body["content"] = args.Content
		}
		return callAdmin(ctx, cfg, "update_reminder", body)
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

// --- Plan Shared Findings tools (v2.10, ADR-0053 — DeLM shared context) -------
//
// record_finding / list_findings mirror the admin handlers in
// internal/admin/api/agent_tools_findings.go VERBATIM (tool name = route segment;
// body keys = decode struct). agent_id is process-fixed (injected from cfg), so an
// agent can only record findings AS itself — the admission gate (author == the
// source task's assignee) is enforced server-side.

type recordFindingArgs struct {
	PlanID  string `json:"plan_id" jsonschema:"the plan to record the finding in (your task must belong to it)"`
	TaskID  string `json:"task_id" jsonschema:"the source task you are/were assigned that produced this finding"`
	Kind    string `json:"kind" jsonschema:"one of: fact (a verified discovery), failure (a dead end others should skip), constraint (a binding rule later work must respect), patch_summary (a compact summary of a completed change)"`
	Content string `json:"content" jsonschema:"the compact gist (keep it short — a sentence or two; this is shared with sibling/downstream agents)"`
}

func makeRecordFinding(cfg Config) mcp.ToolHandlerFor[recordFindingArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args recordFindingArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"plan_id":  args.PlanID,
			"task_id":  args.TaskID,
			"kind":     args.Kind,
			"content":  args.Content,
		}
		return callAdmin(ctx, cfg, "record_finding", body)
	}
}

type listFindingsArgs struct {
	PlanID string `json:"plan_id" jsonschema:"the plan whose shared findings to read"`
}

func makeListFindings(cfg Config) mcp.ToolHandlerFor[listFindingsArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args listFindingsArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"plan_id":  args.PlanID,
		}
		return callAdmin(ctx, cfg, "list_findings", body)
	}
}
