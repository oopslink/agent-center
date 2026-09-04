package agenttools

// Tool describes one agent-facing MCP tool.
type Tool struct {
	Name    string
	Summary string
}

// AgentFacingToolNames is the source-of-truth canonical set of MCP tool names
// the per-agent catalog exposes to the agent LLM.
var AgentFacingToolNames = []string{
	"add_plan_dependency",
	"add_task_to_plan",
	"edit_plan_topology",
	"evolve_plan_generation",
	"archive_plan",
	"assign_task",
	"attach_file",
	"fail_task",
	"heartbeat",
	"complete_task",
	"complete_plan",
	"report_manual_recovery_delivery",
	"discard_task",
	"set_task_issue",
	"create_reminder",
	"list_reminders",
	"get_reminder",
	"update_reminder",
	"create_plan",
	"create_stage",
	"get_stage",
	"reset_task",
	"rerun_failed_node",
	"resume_paused_node",
	"create_task",
	"update_task",
	"create_issue",
	"update_issue",
	"close_issue",
	"reopen_issue",
	"list_issues",
	"list_tasks_of_issue",
	"list_project_repos",
	"get_repo_info",
	"delete_plan",
	"download_file",
	"list_files",
	"find_org_agent",
	"find_org_channel",
	"fork_executor",
	"get_issue",
	"get_team_rule_index",
	"get_team_rule",
	"get_team_rules",
	"propose_team_memory_change",
	"list_team_memory_proposals",
	"get_team_memory_proposal",
	"review_team_memory_proposal",
	"get_my_profile",
	"get_my_unread",
	"list_my_tasks",
	"get_plan",
	"get_task",
	"get_task_audit",
	"list_task_executions",
	"get_task_execution",
	"get_agent_runtime_effective_config",
	"runtime_deploy_restart",
	"runtime_deploy_status",
	"list_findings",
	"list_plans",
	"list_tasks",
	"mark_seen",
	"list_messages",
	"post_message",
	"reassign_task",
	"record_finding",
	"remove_plan_dependency",
	"claim_task",
	"remove_task_from_plan",
	"start_dm",
	"start_plan",
	"start_task",
	"pause_plan",
	"reopen_plan",
	"resume_plan",
	"discard_plan",
	"subscribe",
	"unsubscribe",
	"upload_file",
	"list_templates",
	"get_template",
	"create_template",
	"update_template",
	"delete_template",
	"list_model_catalog_entry",
	"create_model_catalog_entry",
	"update_model_catalog_entry",
	"delete_model_catalog_entry",
	"import_model_catalog",
	"create_team",
	"update_team",
	"delete_team",
	"get_team",
	"list_teams",
	"add_member",
	"remove_member",
	"associate_project",
	"create_team_template",
	"curate_team_template",
	"export_team_template",
	"import_team_template",
	"instantiate_team",
	"extract_from_team",
	"assign_roles",
}

// FilesSeamTools are the agent-facing tools that move bytes through the
// worker-side file seam rather than proxying to an /admin/agent-tools/<name>
// endpoint.
var FilesSeamTools = []string{
	"download_file",
}

// SecondaryTools is the source-of-truth deferred manifest. Every agent-facing
// tool not listed here is part of the directly advertised core catalog.
var SecondaryTools = []Tool{
	{Name: "find_org_agent", Summary: "find an agent in your organization by name"},
	{Name: "find_org_channel", Summary: "find a channel in your organization by name"},
	{Name: "update_task", Summary: "update a task's title or description card fields"},
	{Name: "reassign_task", Summary: "reassign a task to a different identity"},
	{Name: "set_task_issue", Summary: "(re)set or clear a task's derived_from_issue link"},
	{Name: "get_task_audit", Summary: "read a task's redacted lifecycle audit history"},
	{Name: "list_task_executions", Summary: "list executor runs linked to a task"},
	{Name: "get_task_execution", Summary: "inspect one executor run linked to a task"},
	{Name: "get_agent_runtime_effective_config", Summary: "compare desired and effective runtime configuration without secrets"},
	{Name: "subscribe", Summary: "subscribe to a conversation or entity"},
	{Name: "unsubscribe", Summary: "unsubscribe from a conversation or entity"},
	{Name: "rerun_failed_node", Summary: "rerun a failed plan node"},
	{Name: "resume_paused_node", Summary: "resume a paused plan node"},
	{Name: "reset_task", Summary: "reset a dead-executor task back to the pool for a fresh executor"},
	{Name: "report_manual_recovery_delivery", Summary: "register an already-pushed manual recovery delivery after task_non_delivery; MCP counterpart to recover-delivery without commit/push"},
	{Name: "create_plan", Summary: "create a pending plan (a DAG of tasks)"},
	{Name: "edit_plan_topology", Summary: "atomically edit a pending plan's DAG (add/remove nodes+edges)"},
	{Name: "evolve_plan_generation", Summary: "commit an immutable generation diff for a running or paused plan; reopen a done plan first"},
	{Name: "add_task_to_plan", Summary: "add a backlog task as a node in a pending plan"},
	{Name: "remove_task_from_plan", Summary: "remove a task node from a pending plan"},
	{Name: "add_plan_dependency", Summary: "wire a plan edge: seq depends_on, or a conditional/loopback control-flow edge (Decision/cycle authoring)"},
	{Name: "remove_plan_dependency", Summary: "remove a depends_on edge between plan nodes"},
	{Name: "start_plan", Summary: "start a pending plan (the center dispatches ready nodes)"},
	{Name: "pause_plan", Summary: "pause new dispatch without rewriting history"},
	{Name: "resume_plan", Summary: "resume a paused plan from the same frontier"},
	{Name: "reopen_plan", Summary: "reopen a done plan to paused so a follow-up generation can be evolved"},
	{Name: "complete_plan", Summary: "complete a plan whose current effective nodes are all settled"},
	{Name: "discard_plan", Summary: "permanently abandon an active plan while preserving terminal history"},
	{Name: "get_plan", Summary: "read a plan and its nodes"},
	{Name: "list_plans", Summary: "list a project's plans"},
	{Name: "delete_plan", Summary: "delete a never-started pending plan"},
	{Name: "archive_plan", Summary: "archive a finished plan"},
	{Name: "record_finding", Summary: "record a shared finding on a plan"},
	{Name: "list_findings", Summary: "list a plan's shared finding"},
	{Name: "attach_file", Summary: "attach an existing center file into a scope"},
	{Name: "list_templates", Summary: "legacy/deprecated workflow templates; prefer get_team_rules"},
	{Name: "get_template", Summary: "legacy/deprecated workflow template content; prefer Team Memory rules"},
	{Name: "create_template", Summary: "legacy/deprecated workflow template creation; prefer Team Memory rules"},
	{Name: "update_template", Summary: "legacy/deprecated workflow template update; prefer Team Memory rules"},
	{Name: "delete_template", Summary: "legacy/deprecated workflow template delete; prefer Team Memory rules"},
	{Name: "list_model_catalog_entry", Summary: "list the org's model catalog"},
	{Name: "create_model_catalog_entry", Summary: "add a model to the org catalog"},
	{Name: "update_model_catalog_entry", Summary: "update a model catalog entry"},
	{Name: "delete_model_catalog_entry", Summary: "delete a model catalog entry"},
	{Name: "import_model_catalog", Summary: "bulk import the model catalog from JSON (upsert|replace)"},
	{Name: "create_team", Summary: "create a team with its template-declared roles"},
	{Name: "update_team", Summary: "update a team's name/description"},
	{Name: "delete_team", Summary: "delete a team"},
	{Name: "get_team", Summary: "read a team and its roles"},
	{Name: "list_teams", Summary: "list your organization's teams"},
	{Name: "propose_team_memory_change", Summary: "propose a controlled Team Memory change"},
	{Name: "list_team_memory_proposals", Summary: "list controlled Team Memory proposals"},
	{Name: "get_team_memory_proposal", Summary: "inspect a Team Memory proposal with target hash and diff"},
	{Name: "review_team_memory_proposal", Summary: "curator-only promote/reject for Team Memory proposals"},
	{Name: "add_member", Summary: "add an agent/human member to a team under a declared role"},
	{Name: "remove_member", Summary: "remove a member from a team"},
	{Name: "associate_project", Summary: "associate a project with a team"},
	{Name: "create_team_template", Summary: "author + validate a reusable team template"},
	{Name: "curate_team_template", Summary: "mark a team template curated after manual review (export gate)"},
	{Name: "export_team_template", Summary: "export a curated team template to a shareable JSON document (cross-org)"},
	{Name: "import_team_template", Summary: "import a team template from an exported JSON document into your org"},
	{Name: "instantiate_team", Summary: "instantiate a team template into your org (creates team + agents + memory; project-independent)"},
	{Name: "extract_from_team", Summary: "snapshot a live team into a draft template (roles + portable experiences, scrub highlights)"},
	{Name: "assign_roles", Summary: "resolve plan-node roles to concrete agents off a team's roster"},
}

// CoreToolNames returns the directly advertised agent-facing tool names. It does
// not include server-local discovery tools such as search_tools.
func CoreToolNames() []string {
	deferred := map[string]bool{}
	for _, t := range SecondaryTools {
		deferred[t.Name] = true
	}
	out := make([]string, 0, len(AgentFacingToolNames)-len(deferred))
	for _, name := range AgentFacingToolNames {
		if !deferred[name] {
			out = append(out, name)
		}
	}
	return out
}

// SecondaryToolNames returns the deferred tool names.
func SecondaryToolNames() []string {
	names := make([]string, len(SecondaryTools))
	for i, t := range SecondaryTools {
		names[i] = t.Name
	}
	return names
}
