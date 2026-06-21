package mcphost

// AgentFacingToolNames is the SOURCE-OF-TRUTH canonical set of MCP tool names the
// per-agent catalog (NewServer) exposes to the agent LLM. It exists to anchor the
// full-parity guard (TestAgentFacingToolParity): the guard asserts the live
// ListTools name-set EQUALS this list, so a tool added to the registration without
// being added here (or vice versa) fails CI — forcing a DELIBERATE decision about
// whether a new capability should be agent-facing.
//
// This closes the whole CLASS of the #285/#299 seam (a plan/admin handler written
// but never registered in the agent catalog → the agent LLM can't see it). The
// per-tool integration guards (TestPlanToolsRegistered) catch specific families;
// this catches ANY drift in either direction.
//
// When adding/removing an agent-facing tool: update BOTH the NewServer registration
// AND this list (and FilesSeamTools below if it moves bytes via the FileMover seam
// instead of the /admin/agent-tools/<name> proxy). The guard will tell you if you
// miss one.
var AgentFacingToolNames = []string{
	"add_plan_dependency",
	"add_task_to_plan",
	"archive_plan",
	"assign_task",
	"attach_file",
	"block_task",
	"complete_task",
	"discard_task",   // T119: terminal-discard a superseded / mis-created task
	"set_task_issue", // T192: (re)set/clear a task's derived_from_issue after creation
	// T206 Cognition reminders
	"create_reminder",
	"list_reminders",
	"get_reminder",
	"update_reminder",
	"create_plan",
	// v2.9.1 P0 recovery tools (deliberately agent-facing: owner/PD recover a
	// task stuck blocked after a restart/stale-release).
	"unblock_task",
	"rerun_failed_node",
	"resume_paused_node",
	"create_task",
	// v2.10.3 T170 — full agent issue management (create/update/close/reopen/
	// comment/list/link-task). get_issue (relaxed to project-member scope) is
	// already listed below.
	"create_issue",
	"update_issue",
	"close_issue",
	"reopen_issue",
	// T200 WS4: post_issue_message merged into post_message (target type "issue").
	"list_issues",
	"list_tasks_of_issue",
	"delete_plan",
	"download_file",
	"fail_task",
	"find_org_agent",
	"find_org_channel",
	"get_issue",
	"get_my_profile",
	"get_my_unread",
	"get_my_work",
	"get_plan",
	"get_task",
	"list_findings",
	"list_plans",
	"list_tasks",
	"mark_seen",
	"pause_task",
	// T200 WS4: post_message is the single post tool (target = conversation|task|issue);
	// the former post_task_message / post_issue_message are gone.
	"post_message",
	"reassign_task",
	"record_finding",
	"remove_plan_dependency",
	"claim_task",
	"remove_task_from_plan",
	"request_input",
	"resume_task",
	"start_dm",
	"start_plan",
	"start_task",
	"stop_plan",
	"subscribe",
	"unsubscribe",
	"upload_file",
}

// FilesSeamTools are the agent-facing tools that move BYTES through the FileMover
// seam (NewServer's Files dep) rather than proxying to an /admin/agent-tools/<name>
// HTTP endpoint via callAdmin. They are the legitimate EXCEPTION to the
// reverse-lockstep half of the parity guard: every other AgentFacingToolNames entry
// maps 1:1 to a /admin/agent-tools/<name> admin route, but these do not (download_file
// proxies to GET /admin/files/{ulid}). Keep this list minimal and explicit.
var FilesSeamTools = []string{
	"download_file",
}
