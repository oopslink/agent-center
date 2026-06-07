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

// --- verify_task -------------------------------------------------------------

type verifyTaskArgs struct {
	TaskID string `json:"task_id" jsonschema:"the completed task to verify"`
}

func makeVerifyTask(cfg Config) mcp.ToolHandlerFor[verifyTaskArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args verifyTaskArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"task_id":  args.TaskID,
		}
		return callAdmin(ctx, cfg, "verify_task", body)
	}
}
