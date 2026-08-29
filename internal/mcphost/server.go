// Package mcphost implements the per-agent stdio MCP server (v2.7 b3-i,
// ADR-0049). One `mcp-host` process is bound to exactly ONE agent: it
// bridges MCP tool calls from a claude process to the center's admin
// agent-tool HTTP endpoints (/admin/agent-tools/<tool>).
//
// Security spine (mirrors internal/admin/api requireAgentOnWorker): the
// operating agent_id is PROCESS-FIXED — it comes from Config.AgentID
// (sourced from the AC_MCP_AGENT_ID env by the subcommand), and is
// injected into every admin call body. It is NEVER taken from the model's
// tool args, so the model cannot act as another agent. The worker bearer
// token (owner worker:<id>) rides the AdminCaller transport; the center
// re-checks requireAgentOnWorker + per-agent domain authz on every call.
//
// Built on the official MCP Go SDK (github.com/modelcontextprotocol/go-sdk
// @v1.6.1). b3-i ships 2 representative tools (get_my_work +
// post_task_message) to prove the shape; the full tool set + file tools +
// config generation are b3-ii.
package mcphost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxAgentToolResultBytes = 64 * 1024

// AdminCaller is the seam the MCP tool handlers call to reach the center's
// admin agent-tool endpoints. Implementations POST `body` (a JSON object
// that already carries the process-fixed agent_id) to
// /admin/agent-tools/<tool> and write the raw admin JSON response into
// *out. On a non-2xx response they MUST return an error; ideally a typed
// *AdminToolError exposing the status + body so the handler can surface it
// to the model as an IsError CallToolResult.
//
// internal/workerdaemon.AdminClient satisfies this (see
// AdminClient.CallAgentTool).
type AdminCaller interface {
	CallAgentTool(ctx context.Context, tool string, body any, out *json.RawMessage) error
}

// AdminToolError is the typed error an AdminCaller returns on a non-2xx
// admin response. The handler unwraps it to build an IsError
// CallToolResult carrying the body, so claude sees the failure verbatim
// instead of a silent protocol error.
type AdminToolError struct {
	Status int
	Body   string
}

// Error implements error.
func (e *AdminToolError) Error() string {
	return fmt.Sprintf("admin agent-tool: status=%d body=%s", e.Status, e.Body)
}

// FileMover is the seam the file tools (upload_file/download_file) call to
// move bytes between the agent's local workspace and the center, behind the
// daemon-side workspace path-containment guardrail. agentRoot + agentID are
// supplied by the handler from Config (process-fixed) — NEVER from tool args
// — so the model cannot move files for another agent or outside the
// workspace.
//
// internal/workerdaemon.*FileTransferClient satisfies this.
type FileMover interface {
	UploadFile(ctx context.Context, agentRoot, agentID, localPath, scope, scopeID string) (string, error)
	DownloadFile(ctx context.Context, agentRoot, agentID, ulidOrURI, destPath string) error
}

// Config carries everything NewServer needs. It is intentionally
// transport-agnostic (an AdminCaller + FileMover, not concrete HTTP/FS
// clients) so the server is testable with fakes.
type Config struct {
	// AgentID is the process-fixed operating agent. Injected into every
	// admin call body as agent_id; never read from tool args.
	AgentID string
	// Admin is the transport seam to the center's admin agent-tool
	// endpoints.
	Admin AdminCaller
	// AgentRoot is the agent's workspace root, passed to the FileMover for
	// path containment on every file tool. Process-fixed; never from args.
	// May be empty (file tools then fail containment with a clear error).
	AgentRoot string
	// Files is the byte-mover seam for the upload/download file tools. May
	// be nil if the host is built without file support (the tools then
	// return an IsError result explaining files are not wired).
	Files FileMover
	// TierTools (WS5, #issue-e346e5ec) enables tool TIERING: the default tool
	// set is the small high-frequency core; low-frequency management tools are
	// DEFERRED (removed from the default ListTools) and loaded on demand via the
	// search_tools meta-tool. Off (default) registers the FULL set — used by the
	// docs export and parity tests. The production per-agent host turns it ON.
	TierTools bool
	// Generation is the supervisor generation that launched this MCP host. It is
	// stamped onto the frozen plan-rule snapshot so audits can distinguish a
	// reused planning session from a fresh supervisor generation reload.
	Generation int

	planningRules *planningRuleCache
}

// NewServer builds the per-agent MCP server, registers the b3-i tools, and
// returns it WITHOUT running it. The caller runs it (srv.Run with a
// transport) so tests can attach an in-process transport.
func NewServer(cfg Config) *mcp.Server {
	if cfg.planningRules == nil {
		cfg.planningRules = newPlanningRuleCache(cfg)
	}
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "agent-center-mcp-host",
		Version: "0.1.0",
	}, nil)

	registerAllTools(srv, cfg)
	if !cfg.TierTools {
		// Full surface (docs export / parity tests) — no tiering.
		return srv
	}
	// WS5 (#issue-e346e5ec) tiering: shrink the DEFAULT agent tool set to the
	// high-frequency core (work + messages + core queries). Low-frequency
	// management tools are DEFERRED — removed from the default ListTools and
	// loaded on demand via search_tools (SDK tools/list_changed). Deferring is
	// NOT an authz gate: every secondary tool stays reachable via search_tools,
	// and the admin layer enforces authorization on the call regardless.
	srv.RemoveTools(secondaryToolNames()...)
	registerSearchTools(srv, cfg)
	return srv
}

// registerAllTools registers the FULL agent-facing tool surface on srv.
// NewServer calls it, then (when cfg.TierTools) defers the secondary tier.
func registerAllTools(srv *mcp.Server, cfg Config) {
	planningRules := cfg.planningRules
	if planningRules == nil {
		planningRules = newPlanningRuleCache(cfg)
	}

	// v2.14.0 I14/F5 §五/§13.A: the SINGLE "what do I have to do?" query in the Task
	// model — replaces get_my_work. Returns the agent's open/running tasks that are
	// RUNNABLE (blockedBy deps satisfied), each carrying lease_expires_at and any
	// legacy blocked_reason fields still present on old rows.
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_my_tasks",
		Description: "Your single \"what do I have to do?\" query. Returns the open/running tasks assigned to you that are runnable now (their dependencies are satisfied) — each with task_id, title, status, lease_expires_at, and any legacy blocked_reason fields. start_task one (by task_id) to begin it. Call it at the start of your loop and after finishing a task.",
	}, makeListMyTasks(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "fork_executor",
		Description: "Supervisor-only dispatch control: fork one of your assigned runnable tasks into this same Agent's isolated executor. Use after a work_available nudge when the task should run as code/tooling work outside the resident supervisor session. fork_executor performs start/admission for an open task; do NOT call start_task first (an already-running task is tolerated for compatibility). Returns accepted when the fork command is durably enqueued; inspect list_task_executions/get_task_execution for actual executor progress and judge the result before complete_task. Do not use for supervisor_inline/control tasks you should handle in this session.",
	}, makeForkExecutor(cfg))

	// v2.14.0 I14/F5 §五 (pull model on Task): the agent works its OWN tasks one at a
	// time — pick a runnable task_id from list_my_tasks, start_task it (open→running +
	// lease), heartbeat to renew the lease while it runs, then complete_task or
	// fail_task it before starting the next.
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "start_task",
		Description: "Start working on one of your runnable tasks (open→running, sets the execution lease). Pick a task_id from list_my_tasks. You may run up to your concurrency cap at once (1 by default — single-active; more if your profile opts into concurrency); start_task past the cap returns agent_busy until you complete_task, fail_task, or yield a running task. Returns task_not_runnable if the task's dependencies aren't satisfied yet.",
	}, makeStartTask(cfg))

	// T83: claim an OPEN assignment-pool task. Pool tasks are ownerless, so they are
	// claimed (assigned + started) rather than started directly.
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "claim_task",
		Description: "Claim an OPEN assignment-pool task (an ownerless pool task_id). Atomically assigns it to you and starts it (open→running). Only project-member agents may claim; you can hold at most a few claimed pool tasks at once. Returns already_claimed if another agent took it first, or pool_claim_limit_reached if you're at your cap. Once claimed it appears in list_my_tasks.",
	}, makeClaimTask(cfg))

	// v2.14.0 I14/F5 §五/§2.5: renew the execution lease on your running task. The
	// background lease-checker reclaims a running task whose lease lapses (the agent
	// presumed dead); heartbeat periodically while a long task runs so it is not
	// reclaimed. Lease-only — no status change; rejected unless the task is running.
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "heartbeat",
		Description: "Renew the execution lease on the task you are currently running so the background lease-checker does not reclaim it (it reclaims a running task whose lease lapses, presuming the agent died). Call it periodically during a long-running task. Lease-only: it does not change status, and is rejected unless the task is running.",
	}, makeHeartbeat(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_my_unread",
		Description: "List unread messages directed at you — every unread message in your DMs plus every unread @mention of you in channels you're in (excludes channel chatter you weren't mentioned in, and your own messages). Check this periodically and when you reach a stopping point. Each item carries actor_kind (human|agent|system) + reply_required: a HUMAN directed message you MUST answer (acknowledge + defer, handle now, or decline with a reason) — your reply IS your decision. An AGENT-authored mention is reply-optional (reply_required=false): judge by content — reply if it actually needs one, otherwise just mark_seen to silently acknowledge (no obligation to produce a message). Handle messages at a stopping point — they don't interrupt your current task. After you handle (or silently ack) a message, call mark_seen so it does not come back.",
	}, makeGetMyUnread(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "mark_seen",
		Description: "Mark a conversation read up to a message id — call this AFTER you reply to (or decide on) a message from get_my_unread, so it is not surfaced again. Pass the conversation_id and the id of the latest message you handled.",
	}, makeMarkSeen(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_messages",
		Description: "Read the chat history of ONE conversation you participate in — a DM, a channel, or a task/issue/plan conversation. This is how you catch up on context: get_my_unread only shows messages addressed to you (your DMs + @mentions), while list_messages returns the FULL message stream of a conversation, including messages you were never mentioned in or already marked seen. Pass conversation_id (from get_my_unread, find_org_channel, or a message you were given). Returns the most recent messages (limit, default 50, max 50) oldest→newest, plus has_more and next_before_message_id; to read OLDER history, call again with before_message_id = next_before_message_id instead of requesting a huge page. You must be an active participant — a channel you have not joined returns not_a_channel_member.",
	}, makeListMessages(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "post_message",
		Description: "Post a message to a DM/channel, a task, or an issue — ONE tool for all four, selected by target. Set target.type to \"conversation\" (a DM or channel, target.id = the conversation_id from the message you were given), \"task\" (target.id = task_id), or \"issue\" (target.id = issue_id). For the main answer to a message you were given, pass reply_to_message_id with that source message id; if replying in a thread also pass parent_message_id. Omit reply_to_message_id for side notifications/handoffs that should not count as answering the source. @mention a participant by name to notify them. Keep your text focused on what you're saying — to share a file, upload it with upload_file and pass the returned file_uri in attachments (the UI renders attachments as preview cards); do not paste raw file URIs into the text.",
	}, makePostMessage(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "start_dm",
		Description: "Start or reuse a same-org 1:1 DM with another agent and send the opening message. Use target_agent from find_org_agent's id or assignee_ref. For existing DMs this reuses the conversation; it does not create duplicates.",
	}, makeStartDM(cfg))

	// --- self / org-discovery tools (v2.7.1 #239) ----------------------------
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_my_profile",
		Description: "Get the calling agent's own profile: your display_name and agent_ref (the \"agent:<id>\" form others use to @mention you), your organization, the projects you belong to (with role + what you can do in each), and your capabilities. Call this at the start of a session to learn WHO YOU ARE — your display_name tells you which @mentions are actually for you. Several agents may share a conversation; never assume you are an agent whose name merely appears in a message — only your own display_name from this tool identifies you. In concurrent mode, your resident session is this same Agent's Supervisor control plane and forked executors are this same Agent's isolated execution units; process/workspace/MCP isolation does not make them external agents.",
	}, makeGetMyProfile(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "find_org_agent",
		Description: "Find agents in your organization by name (substring match; empty name lists all). Returns [{id, name, assignee_ref}] — pass an entry's assignee_ref straight to assign_task's assignee (it is the ready-to-use \"agent:<id>\" form; do not hand-build it).",
	}, makeFindOrgAgent(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "find_org_channel",
		Description: "Find channels in your organization by name (substring match; empty name lists all = available channels). Returns [{id, name}]. Use an entry's id directly as post_message's conversation_id (it is a plain channel id — do NOT add a prefix). An empty result means no such channel exists.",
	}, makeFindOrgChannel(cfg))

	// --- read tools (own-scope) ----------------------------------------------
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_task",
		Description: "Get a task the calling agent owns (it must hold a work item for the task).",
	}, makeGetTask(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_task_audit",
		Description: "Read a project-visible task's stable, paginated lifecycle audit history. Notes are bounded and secret-shaped content is redacted.",
	}, makeTaskRead(cfg, "get_task_audit"))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_task_executions",
		Description: "List stable executor runs linked to a project-visible task, reconstructed from persisted executor lifecycle events. Returns health_status, recovery_required, last_effective_activity_at, branch/SHA/pushed and non_delivery reasons.",
	}, makeTaskRead(cfg, "list_task_executions"))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_task_execution",
		Description: "Read one executor run linked to a project-visible task by execution_id, including CLI/model, health_status, recovery_required, branch/SHA/pushed and non_delivery reasons. If recovery_required=true, inspect audit/worktree evidence before complete_task.",
	}, makeTaskRead(cfg, "get_task_execution"))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_agent_runtime_effective_config",
		Description: "Read this agent's desired and observed effective non-sensitive runtime configuration. Secrets and environment values are never returned.",
	}, makeEffectiveConfig(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_tasks",
		Description: "List all tasks in a project (board overview), optionally filtered by status and/or assignee. Requires project_id; the caller must be a project member.",
	}, makeListTasks(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_issue",
		Description: "Get an issue in a project you are a member of (returns title, description, status, tags, created_by, org_ref).",
	}, makeGetIssue(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_issues",
		Description: "List the issues in a project you are a member of (board overview), optionally filtered by status and/or author. Requires project_id.",
	}, makeListIssues(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_tasks_of_issue",
		Description: "List the tasks derived from an issue (the reverse of create_task's derived_from_issue link) — see the executable work an issue spawned. You must be a member of the issue's project.",
	}, makeListTasksOfIssue(cfg))

	// --- workspace CodeRepo info tools (v2.18.4 BE-2) ------------------------
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_project_repos",
		Description: "List the code repositories a project references (label, description, url, provider, default_branch, is_primary). The standard repo-info surface — credentials are NEVER returned. You must be a member of the project.",
	}, makeListProjectRepos(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_repo_info",
		Description: "Get one referenced repo's standard info (omit repo_id for the project's primary repo). With live=true, also fetch recent remote commits + branches (no clone). Credentials are NEVER returned. You must be a member of the project.",
	}, makeGetRepoInfo(cfg))

	// --- pm write/passthrough tools ------------------------------------------
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_task",
		Description: "Create a task in a project the calling agent belongs to.",
	}, makeCreateTask(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "update_task",
		Description: "Update an existing task's card fields: title and/or description, or clear the description. Omitted fields are left unchanged. Description edits on running tasks are rejected with task_description_frozen.",
	}, makeUpdateTask(cfg))

	// --- issue management (v2.10.3 T170) -------------------------------------
	// The agent gets the full issue lifecycle a human has in the Web Console:
	// open → discuss (@/thread) → edit/close/reopen → derive tasks. Use an issue
	// (not a task) to carry a requirement/discussion; derive tasks from it with
	// create_task's derived_from_issue, then see them via list_tasks_of_issue.
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_issue",
		Description: "Open a new issue in a project you belong to — a discussion/requirement item (NOT an executable task). Provide a title and optional description/tags. Derive executable tasks from it later with create_task's derived_from_issue.",
	}, makeCreateIssue(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "update_issue",
		Description: "Edit an issue — set any of title, description, status, tags (omitted fields are left unchanged, applied all-or-none). status is one of open|in_progress|resolved|closed|discarded|reopened. You must be a member of the issue's project.",
	}, makeUpdateIssue(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "close_issue",
		Description: "Close an issue (sets status=closed). Convenience wrapper over update_issue; reopen it later with reopen_issue.",
	}, makeIssueID(cfg, "close_issue"))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "reopen_issue",
		Description: "Reopen a closed/resolved issue (sets status=open, the actionable state).",
	}, makeIssueID(cfg, "reopen_issue"))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "assign_task",
		Description: "Assign a task to an identity (e.g. agent:X or user:Y).",
	}, makeAssignTask(cfg, "assign_task"))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "reassign_task",
		Description: "Reassign a task to a different identity.",
	}, makeAssignTask(cfg, "reassign_task"))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "subscribe",
		Description: "Subscribe an identity (defaults to the calling agent) to a task.",
	}, makeSubscribe(cfg, "subscribe"))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "unsubscribe",
		Description: "Unsubscribe an identity (defaults to the calling agent) from a task.",
	}, makeSubscribe(cfg, "unsubscribe"))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "fail_task",
		Description: "Terminally mark the task you are running as failed when it cannot progress. A detailed reason is required; include what failed, what you tried, and what a next plan generation needs to know. Failed is terminal: do not expect the same task to resume.",
	}, makeFailTask(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "reset_task",
		Description: "Tier-3 recovery for a task stranded RUNNING under this Agent's dead executor (its workspace/worktree is gone or its node changed, so it will never make progress): reset it back to the pool (running→open, assignee/lease cleared). Reset does NOT fork or re-dispatch an executor: after the task is assigned and work_available wakes its supervisor, that supervisor must explicitly call fork_executor for code/tooling work (or handle supervisor_inline/control work itself). Only use when you've confirmed the executor is truly gone — a task whose lease is still live is rejected (a live agent is nudged, not reset). After repeated resets the center fails the task for evolution triage.",
	}, makeResetTask(cfg))

	// rerun_failed_node/resume_paused_node are the OPERATOR-RECOVERY half of the
	// pause/resume model (T200 WS4): an owner/PD un-sticks ANOTHER agent's plan node
	// — the cross-agent counterpart of resume_task (which only resumes YOUR OWN
	// paused task). Pick by the node's state: paused → resume_paused_node,
	// failed/undispatched → rerun_failed_node.
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "rerun_failed_node",
		Description: "Operator recovery for a FAILED/undispatched plan node: clear its dispatch record so the next plan advance re-dispatches it. Pair: use resume_paused_node instead when the node is merely paused (its agent set it aside and went idle).",
	}, makeRerunFailedNode(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "resume_paused_node",
		Description: "Operator recovery for a PAUSED plan node (the cross-agent counterpart of resume_task): a node whose agent paused its task and went idle shows `paused` — this resumes it and wakes that agent so it continues. Use rerun_failed_node instead for a failed/undispatched node.",
	}, makeResumePausedNode(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "complete_task",
		Description: "Optionally post a summary and move the task to completed. In concurrent mode, call this only after you, the Agent's Supervisor control plane, have judged that this same Agent's executor truly delivered; do not complete merely because an executor exited. If it returns task_non_delivery, read reason_codes and fix/register those exact missing delivery conditions before retrying. For a control-flow DECISION node, pass outcome=\"pass\"/\"reject\" to route its edges. For a REVIEW node, you MUST record your structured verdict via review_verdict=\"pass\"/\"reject\" (+ review_blocking) so the downstream Decision auto-decides — a non-blocking nit is review_verdict=\"pass\", review_blocking=false.",
	}, makeCompleteTask(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "report_manual_recovery_delivery",
		Description: "Register an already-pushed manual recovery delivery for a task stuck on task_non_delivery after a dead/stale/exhausted executor. This MCP tool does NOT commit or push; first recover/test/push in the retained worktree, then pass task_id, optional executor_id/worktree, reason, evidence, and git{branch,head_sha,pushed,probed,base_ref,base_known,ahead_of_base,dirty,push_error}. After it returns ok, call complete_task again.",
	}, makeReportManualRecoveryDelivery(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "discard_task",
		Description: "Terminally DISCARD a non-terminal task (open/running → discarded) — the right way to retire a superseded or mis-created task. A reason is required. Unlike complete_task it does not mark the work done (shows Discarded, not Completed); unlike fail_task it does not feed failure/evolution. A terminal task (completed/failed/discarded) is rejected.",
	}, makeDiscardTask(cfg))

	// T192: (re)set or clear a task's derived_from_issue AFTER creation.
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_task_issue",
		Description: "(Re)set or CLEAR a task's derived_from_issue link AFTER creation (previously the create-time link was the only chance to set it). Pass issue_id to link it (the issue must EXIST and belong to the task's project) or an empty string to clear. Authorized for the task's creator / a project member / its current worker — no work item required. Returns the resulting link.",
	}, makeSetTaskIssue(cfg))

	// --- reminder tools (T206, Cognition BC) ---------------------------------
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_reminder",
		Description: "Set a reminder that wakes a target agent and delivers a text. THREE trigger modes: (1) time — schedule once{once_at RFC3339}; (2) recurring — schedule cron{cron_expr, timezone} with optional end_condition (never|until|max_count); (3) EVENT-DRIVEN — pass on_event{entity_type: plan|task|issue, entity_id, event} to arm the reminder when a project entity transitions (plan: completed|failed|stopped; task: completed|failed|reopened|discarded; issue: closed|reopened), then fire ONCE after `delay` (e.g. 5m; default 0) and @-notify `target` (defaults to remindee_agent_id). The watched entity must be in your project. remindee/target must be in your project (owner may cross projects). skip_if_overlap (default true) drops a fire while the previous one is still being handled; deliver_as_creator (default true) delivers as your identity rather than the system identity (a self-reminder always wakes via the system identity).",
	}, makeCreateReminder(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_reminders",
		Description: "List reminders you created (default), or by remindee, optionally filtered by status (active|paused|completed|canceled). Shows next_run_at + fired_count.",
	}, makeListReminders(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_reminder",
		Description: "Read one reminder by id (you must be its creator, the remindee, or an owner).",
	}, makeGetReminder(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "update_reminder",
		Description: "Manage a reminder: action=pause | resume | cancel, or edit (pass a new schedule and/or content). Pausing stops it firing; resume recomputes the next run; cancel is terminal. Only the creator or an owner may update.",
	}, makeUpdateReminder(cfg))

	// --- plan tools (v2.9 P3 Stage C, #285) ----------------------------------
	// A PM-agent programmatically builds and runs plans: create a pending Plan,
	// add backlog tasks as nodes, wire depends_on edges into a DAG, then start
	// it (the center dispatches ready nodes as their dependencies complete).
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_plan",
		Description: "Create a new pending plan in a project you belong to. A plan is a DAG of tasks the center auto-dispatches once started. After creating, add tasks with add_task_to_plan, wire dependencies with add_plan_dependency, then start_plan. Optional target_date is RFC3339.",
	}, makeCreatePlan(cfg, planningRules))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "add_task_to_plan",
		Description: "DEPRECATED — prefer edit_plan_topology. Add an existing backlog task to a pending plan as a node. Once execution starts, completed history is immutable. Optional stage groups the task under a Plan Stage.",
	}, makePlanTask(cfg, "add_task_to_plan"))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "remove_task_from_plan",
		Description: "DEPRECATED — prefer edit_plan_topology (remove_node). Remove a task node from a pending plan and return it to the backlog. AssignmentPool is independent from Plan.",
	}, makePlanTask(cfg, "remove_task_from_plan"))

	// 2026-07-03 plan-stage-model §6: Stage authoring + read. A Stage groups a batch of
	// a plan's tasks into a sub-DAG bounded by a barrier + an optional acceptance gate;
	// stages themselves form a DAG (depends_on_stages) so batches can run in parallel.
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_stage",
		Description: "Create a Stage in a pending plan. Pass releases downstream; reject records evidence and appends an incremental remediation Stage without reopening completed work. Returns stage_id.",
	}, makeCreateStage(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_stage",
		Description: "Read a Stage's derived status (open/running/reopen/done), its member task nodes, and its current bounded-retry round. Status is projected from the member nodes — it is not a separately-tracked state.",
	}, makeGetStage(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "add_plan_dependency",
		Description: "DEPRECATED — prefer edit_plan_topology (add_edge). Add an edge to a pending Plan DAG. Both tasks must already be nodes in the Plan; self-edges and cycles are rejected.",
	}, makeAddPlanDep(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "remove_plan_dependency",
		Description: "DEPRECATED — prefer edit_plan_topology (remove_edge). Remove a depends_on edge from a pending Plan DAG. Idempotent when the edge is absent.",
	}, makePlanDep(cfg, "remove_plan_dependency"))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "edit_plan_topology",
		Description: "Atomically edit a PENDING plan's DAG with a batch of ops. Pass base_version (read from get_plan) for optimistic concurrency. ops is an ordered list of {op, ...}: add_node{task_id}, remove_node{task_id}, add_edge{from_task_id,to_task_id,kind?,when?,max_rounds?}, remove_edge{from_task_id,to_task_id}. Only the FINAL shape is validated. Once a plan is running or paused, use evolve_plan_generation instead.",
	}, makeEditPlanTopology(cfg, planningRules))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "evolve_plan_generation",
		Description: "Commit an immutable Plan Generation snapshot derived from the active generation. Requires parent_generation_id, base_version, idempotency_key, reason, evidence, and a diff with node_decisions plus new tasks/edges. New root tasks must have explicit dependency edges to prior execution unless detached=true; follows_task_id is lineage only and does not create an execution edge. Create new downstream tasks/edges instead of reusing already-dispatched old nodes. Optimistic concurrency rejects stale parent/version; idempotency returns the prior generation for the same key+payload. Running plans switch active generation and dispatch newly-ready nodes in the same transaction; paused plans switch without dispatch. If a plan is already done, call reopen_plan first; it reopens to paused so you can evolve, then resume_plan to dispatch.",
	}, makeEvolvePlanGeneration(cfg, planningRules))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "start_plan",
		Description: "Validate and start a pending plan (move it to running), atomically freezing its immutable G0 generation and exposing it through get_plan.active_generation_id. The center dispatches each node once its dependencies complete.",
	}, makePlanID(cfg, "start_plan"))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "pause_plan",
		Description: "Pause new dispatch while preserving the current frontier and immutable execution history.",
	}, makePlanID(cfg, "pause_plan"))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "resume_plan",
		Description: "Resume a paused plan from the same frontier.",
	}, makePlanID(cfg, "resume_plan"))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "reopen_plan",
		Description: "Reopen a done plan to paused so you can append a follow-up immutable generation with evolve_plan_generation. Completed task history stays immutable; after evolving, call resume_plan to dispatch newly-ready nodes. Archived or discarded plans cannot be reopened.",
	}, makePlanID(cfg, "reopen_plan"))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "complete_plan",
		Description: "Complete a running or paused plan when its effective nodes are settled. For an explicit audited override, set force=true and provide a non-empty reason; automatic completion never forces. Idempotent if already done. To add follow-up work to a done plan, use reopen_plan then evolve_plan_generation.",
	}, makeCompletePlan(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "discard_plan",
		Description: "Permanently abandon a pending, running, or paused plan. Non-terminal member tasks become discarded; completed history remains immutable.",
	}, makePlanID(cfg, "discard_plan"))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_plan",
		Description: "Read a plan's full detail: its nodes, dependency edges, per-node status, the ready_set, has_failed, and progress{done,total}. Scoped to the project you name (a plan in another project is not found).",
	}, makeGetPlan(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_plans",
		Description: "List a project's plans with a board summary each (status, progress, has_failed, node_count, and a capped nodes preview). Use this to find a plan_id to operate on.",
	}, makeListPlans(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "delete_plan",
		Description: "Hard-delete a never-started pending plan. Once a plan starts, preserve history and use discard_plan instead. Irreversible.",
	}, makePlanID(cfg, "delete_plan"))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "archive_plan",
		Description: "Archive a done or discarded plan without changing that lifecycle outcome. Returns the terminal plan detail with archived_at.",
	}, makePlanID(cfg, "archive_plan"))

	// --- plan shared findings (v2.10, ADR-0053 — DeLM shared context) --------
	// Record a compact finding (gist) back to your plan so sibling/downstream
	// agents build on it; read a plan's findings to see prior progress.
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "record_finding",
		Description: "Record a compact FINDING (gist) back to your plan's shared context so sibling and downstream agents can build on it instead of re-discovering it. Use it when you verify a fact, hit a dead end (failure — so others skip it), discover a binding constraint, or finish a change worth summarizing (patch_summary). You may only record a finding for a task you are/were assigned. Keep content short.",
	}, makeRecordFinding(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_findings",
		Description: "List the shared findings recorded in a plan so far (oldest-first): each has a kind (fact/failure/constraint/patch_summary), the source task, the author, and the gist. Read this before starting plan work to reuse what others already learned.",
	}, makeListFindings(cfg))

	// --- template tools ------------------------------------------------------
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_templates",
		Description: "LEGACY/DEPRECATED: list old org workflow templates. New plan/runtime guidance should live in Team Memory rules/ and be read with get_team_rules.",
	}, makeListTemplates(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_template",
		Description: "LEGACY/DEPRECATED: get an old workflow template by ID. Prefer migrated Team Memory rules/ plus get_team_rules for plan guidance.",
	}, makeGetTemplate(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_template",
		Description: "LEGACY/DEPRECATED compatibility path for old workflow templates. Prefer authoring Team Memory rules/ through team templates or migration.",
	}, makeCreateTemplate(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "update_template",
		Description: "LEGACY/DEPRECATED compatibility path for old workflow templates. Prefer updating Team Memory rules/.",
	}, makeUpdateTemplate(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "delete_template",
		Description: "LEGACY/DEPRECATED compatibility path for old workflow templates. Prefer deleting or reverting Team Memory rules/ files.",
	}, makeDeleteTemplate(cfg))

	// --- model catalog tools (issue-93dd8daa ①) ------------------------------
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_model_catalog_entry",
		Description: "List your organization's model catalog — the user-managed set of models agents may run (each with model_id, display_name, per-MTok input/output cost, context_window, and a free-text tier/capability description).",
	}, makeListModelCatalog(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_model_catalog_entry",
		Description: "Add a model to your organization's catalog. model_id must be unique in the org; costs and context_window must be >= 0; tier is a free-text capability/fit description.",
	}, makeCreateModelCatalog(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "update_model_catalog_entry",
		Description: "Update a model catalog entry by id (model_id, display_name, costs, context_window, tier).",
	}, makeUpdateModelCatalog(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "delete_model_catalog_entry",
		Description: "Delete a model catalog entry by id.",
	}, makeDeleteModelCatalog(cfg))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "import_model_catalog",
		Description: "Bulk-import the model catalog from a JSON array. mode=upsert (insert-or-update by model_id) or replace (swap the whole org catalog). Any invalid entry (bad schema, duplicate model_id, negative cost) rejects the WHOLE batch — nothing is half-applied.",
	}, makeImportModelCatalog(cfg))

	// --- team tools (Team Phase-1 wiring, design §4/§6/§7/§9) -----------------
	registerTeamTools(srv, cfg)

	// --- file tools ----------------------------------------------------------
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "upload_file",
		Description: "Upload a file from the agent's workspace to the center, optionally placing it in a scope.",
	}, makeUploadFile(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "download_file",
		Description: "Download a center file (ac://files/{ulid} or bare ulid) into the agent's workspace.",
	}, makeDownloadFile(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_files",
		Description: "List files and images attached to a task, issue, or conversation scope in the calling agent's own domain. Use the returned URI with download_file.",
	}, makeListFiles(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "attach_file",
		Description: "Attach an existing center file into a scope in the calling agent's own domain.",
	}, makeAttachFile(cfg))
}

// getMyProfileArgs is argless: get_my_profile is inherently self-scoped (the
// agent reads only its own org/projects/capabilities), and agent_id is
// process-fixed — nothing for the model to supply (v2.7.1 #239).
type getMyProfileArgs struct{}

// makeGetMyProfile returns the get_my_profile handler bound to cfg. The
// forwarded body carries ONLY the process-fixed agent_id (self-only scope).
func makeGetMyProfile(cfg Config) mcp.ToolHandlerFor[getMyProfileArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ getMyProfileArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{"agent_id": cfg.AgentID}
		return callAdmin(ctx, cfg, "get_my_profile", body)
	}
}

// findOrgAgentArgs is the typed input for find_org_agent. agent_id is process-
// fixed (injected from cfg, never the model) so the org scope can't be spoofed.
type findOrgAgentArgs struct {
	Name string `json:"name" jsonschema:"agent name to search for (substring, case-insensitive; empty lists all org agents)"`
}

// makeFindOrgAgent returns the find_org_agent handler bound to cfg. agent_id is
// injected from cfg; the org scope is derived center-side from that agent.
func makeFindOrgAgent(cfg Config) mcp.ToolHandlerFor[findOrgAgentArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args findOrgAgentArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{"agent_id": cfg.AgentID, "name": args.Name}
		return callAdmin(ctx, cfg, "find_org_agent", body)
	}
}

// findOrgChannelArgs is the typed input for find_org_channel (v2.7.1 #246).
// agent_id is process-fixed (org scope derived center-side, not spoofable).
type findOrgChannelArgs struct {
	Name string `json:"name" jsonschema:"channel name to search for (substring, case-insensitive; empty lists all org channels)"`
}

// makeFindOrgChannel returns the find_org_channel handler bound to cfg.
func makeFindOrgChannel(cfg Config) mcp.ToolHandlerFor[findOrgChannelArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args findOrgChannelArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{"agent_id": cfg.AgentID, "name": args.Name}
		return callAdmin(ctx, cfg, "find_org_channel", body)
	}
}

// postMessageAttachment is one already-uploaded file attached to a post_message
// (T44). uri is the ac://files/{ulid} returned by upload_file; the rest is
// display metadata rendered in the UI's attachment card.
type postMessageAttachment struct {
	URI      string `json:"uri" jsonschema:"the ac://files/{ulid} returned by upload_file"`
	Filename string `json:"filename" jsonschema:"display filename"`
	MimeType string `json:"mime_type" jsonschema:"the file's MIME type (drives image-preview vs file-chip rendering)"`
	Size     int64  `json:"size" jsonschema:"file size in bytes"`
}

// postMessageTarget is the T200 WS4 discriminated destination of post_message —
// it replaces the three former tools (DM/channel post_message, post_task_message,
// post_issue_message) with one target{type,id}. There is NO agent_id here; it is
// process-fixed and injected by the handler so the model cannot spoof which agent posts.
type postMessageTarget struct {
	Type string `json:"type" jsonschema:"one of: conversation (a DM or channel) | task | issue"`
	ID   string `json:"id" jsonschema:"the destination id — a conversation_id for a DM/channel, a task_id for a task, or an issue_id for an issue"`
}

// postMessageArgs is the typed input for post_message (v2.7 #185; T200 WS4). Like
// the former post_task_message there is NO agent_id field — it is process-fixed
// and injected by the handler so the model cannot spoof which agent posts.
type postMessageArgs struct {
	Target postMessageTarget `json:"target" jsonschema:"where to post: {type, id}. type=conversation (id=conversation_id of a DM/channel you received the message in), type=task (id=task_id), or type=issue (id=issue_id)"`
	Text   string            `json:"text" jsonschema:"the message text"`
	// ParentMessageID (v2.9.1 Thread F4): set to reply IN a thread — pass the thread
	// root message id the wake brief gave you. Omit for a normal top-level message.
	ParentMessageID string `json:"parent_message_id,omitempty" jsonschema:"to reply inside a thread, the thread root message id from the brief; omit for a top-level message"`
	// QuotedMessageID (引用): set to quote an earlier message — the UI renders a
	// preview card of it above your message. The quoted message must be in the SAME
	// conversation you are posting to. Orthogonal to a thread reply; omit for none.
	QuotedMessageID string `json:"quoted_message_id,omitempty" jsonschema:"to quote an earlier message (renders a preview card above yours), its message id in the SAME conversation; omit if not quoting"`
	// ReplyToMessageID marks this post as the primary reply closing a source message.
	// The center derives the legal destination from the source message. Omit for
	// side notifications/handoffs that should not count as answering the source.
	ReplyToMessageID string `json:"reply_to_message_id,omitempty" jsonschema:"source message id this is the main answer to; top-level sources may be answered top-level or with parent_message_id equal to the source id; threaded sources must use parent_message_id equal to the thread root; omit for side notifications/handoffs"`
	// Attachments (T44): files to share in the conversation, rendered as preview
	// cards. Upload each via upload_file first, then pass the returned file_uri here.
	Attachments []postMessageAttachment `json:"attachments,omitempty" jsonschema:"optional files to attach (upload each via upload_file first, then pass the returned file_uri as uri); the UI renders them as preview cards"`
	// MentionRefs (T460 ①): the reliable, typo-proof way to @mention agents — pass
	// each target's agent_ref ("agent:<id>", e.g. an assignee_ref from find_org_agent /
	// get_task) instead of (or alongside) an @display_name in the text. A ref never
	// fails silently the way a mistyped handle does. Prefer this for agent↔agent handoffs.
	MentionRefs []string `json:"mention_refs,omitempty" jsonschema:"reliable @mention of agents by ref ('agent:<id>', e.g. an assignee_ref) — never fails silently like a mistyped @display_name; prefer for agent-to-agent handoffs"`
}

// makePostMessage returns the post_message handler bound to cfg. agent_id is
// injected from cfg, NEVER from args. The model-facing arg is "text" (natural),
// but every admin post endpoint reads "content", so the value is forwarded under
// "content".
func makePostMessage(cfg Config) mcp.ToolHandlerFor[postMessageArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args postMessageArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"target": map[string]any{
				"type": args.Target.Type,
				"id":   args.Target.ID,
			},
			"content": args.Text,
		}
		if args.ParentMessageID != "" {
			body["parent_message_id"] = args.ParentMessageID
		}
		if args.QuotedMessageID != "" {
			body["quoted_message_id"] = args.QuotedMessageID
		}
		if args.ReplyToMessageID != "" {
			body["reply_to_message_id"] = args.ReplyToMessageID
		}
		if len(args.MentionRefs) > 0 {
			body["mention_refs"] = args.MentionRefs
		}
		if len(args.Attachments) > 0 {
			atts := make([]map[string]any, len(args.Attachments))
			for i, att := range args.Attachments {
				atts[i] = map[string]any{
					"uri":       att.URI,
					"filename":  att.Filename,
					"mime_type": att.MimeType,
					"size":      att.Size,
				}
			}
			body["attachments"] = atts
		}
		return callAdmin(ctx, cfg, "post_message", body)
	}
}

type startDMArgs struct {
	TargetAgent string `json:"target_agent" jsonschema:"target agent id, identity member id, or agent:<id>; use find_org_agent first when unsure"`
	Text        string `json:"text" jsonschema:"the opening message to send in the DM"`
	Reason      string `json:"reason,omitempty" jsonschema:"optional short reason for audit/context"`
}

func makeStartDM(cfg Config) mcp.ToolHandlerFor[startDMArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args startDMArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":     cfg.AgentID,
			"target_agent": args.TargetAgent,
			"content":      args.Text,
		}
		if args.Reason != "" {
			body["reason"] = args.Reason
		}
		return callAdmin(ctx, cfg, "start_dm", body)
	}
}

// callAdmin POSTs to the admin tool endpoint and maps the result onto the
// MCP wire shape:
//   - success → CallToolResult with the raw admin JSON as TextContent
//     passthrough (no OutputSchema; Out is any).
//   - *AdminToolError → CallToolResult{IsError: true} carrying the body, so
//     the model sees the failure and can self-correct.
//   - any other (transport) error → returned as the handler error.
func callAdmin(ctx context.Context, cfg Config, tool string, body any) (*mcp.CallToolResult, any, error) {
	var raw json.RawMessage
	if err := cfg.Admin.CallAgentTool(ctx, tool, body, &raw); err != nil {
		var adminErr *AdminToolError
		if errors.As(err, &adminErr) {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: adminErr.Body}},
			}, nil, nil
		}
		return nil, nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: adminToolResultText(raw)}},
	}, nil, nil
}

func adminToolResultText(raw json.RawMessage) string {
	if len(raw) <= maxAgentToolResultBytes {
		return string(raw)
	}
	omitted := len(raw) - maxAgentToolResultBytes
	env := map[string]any{
		"truncated":     true,
		"omitted_bytes": omitted,
		"prefix":        string(raw[:maxAgentToolResultBytes]),
	}
	b, err := json.Marshal(env)
	if err != nil {
		return fmt.Sprintf(`{"truncated":true,"omitted_bytes":%d}`, omitted)
	}
	return string(b)
}
