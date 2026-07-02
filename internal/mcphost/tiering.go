package mcphost

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool tiering (WS5, #issue-e346e5ec). The default per-agent tool set is the
// small HIGH-FREQUENCY core — the tools an agent uses on nearly every loop:
// working its queue, messaging, and core task reads. LOW-FREQUENCY management
// tools (plan authoring, issue management, findings, files, subscriptions, org
// discovery, node recovery, link/assign admin) are DEFERRED: kept out of the
// default ListTools and loaded on demand via search_tools (SDK list_changed).
//
// This is a UX/context optimization, NOT an authorization boundary. A deferred
// tool is still reachable (search_tools loads it) and the admin layer enforces
// the same authorization on the call. Authz red-line tools therefore keep their
// reachability — they are simply one search_tools call away.
//
// secondaryTools is the source-of-truth DEFERRED manifest. Every agent-facing
// tool NOT listed here is core. `summary` powers search_tools matching + listing.
var secondaryTools = []struct{ name, summary string }{
	// org discovery
	{"find_org_agent", "find an agent in your organization by name"},
	{"find_org_channel", "find a channel in your organization by name"},
	// issue read + management
	{"get_issue", "read an issue"},
	{"list_issues", "list a project's issues"},
	{"list_tasks_of_issue", "list the tasks derived from an issue"},
	{"create_issue", "open a new issue in a project"},
	{"update_issue", "edit an issue (title/description/status/tags)"},
	{"close_issue", "close an issue"},
	{"reopen_issue", "reopen a closed issue"},
	// task link / assignment admin
	{"reassign_task", "reassign a task to a different identity"},
	{"set_task_issue", "(re)set or clear a task's derived_from_issue link"},
	// reminders (T206 Cognition) are CORE — kept OUT of this deferred manifest
	// (T252, issue-c438cde1). They were deferred, but that DOUBLE-hid them: in a
	// harness whose own tool-search only surfaces tools the MCP server has already
	// ADVERTISED, a tool deferred behind THIS server's search_tools is invisible to
	// the harness search until search_tools is called first. So an agent told to
	// "set a reminder" reached for the harness tool-search, found nothing (the
	// reminder tools were not yet advertised), and fell back to ad-hoc
	// ScheduleWakeup — exactly the I4 anti-pattern. Same reasoning as T247
	// promoting download_file/upload_file: a capability an agent is GUIDED to call
	// directly (I4 / follow-up T253 — "prefer reminder over ScheduleWakeup") must be
	// directly discoverable, not gated behind a discovery hop. create/list/get/
	// update are a tight family for the same proactive workflow, so all four are core.
	// subscriptions
	{"subscribe", "subscribe to a conversation or entity"},
	{"unsubscribe", "unsubscribe from a conversation or entity"},
	// plan node recovery
	{"rerun_failed_node", "rerun a failed plan node"},
	{"resume_paused_node", "resume a paused plan node"},
	// plan authoring / lifecycle
	{"create_plan", "create a draft plan (a DAG of tasks)"},
	{"add_task_to_plan", "add a backlog task as a node in a draft plan"},
	{"remove_task_from_plan", "remove a task node from a draft plan"},
	{"add_plan_dependency", "wire a depends_on edge between two plan nodes"},
	{"remove_plan_dependency", "remove a depends_on edge between plan nodes"},
	{"start_plan", "start a draft plan (the center dispatches ready nodes)"},
	{"stop_plan", "stop a running plan"},
	{"get_plan", "read a plan and its nodes"},
	{"list_plans", "list a project's plans"},
	{"delete_plan", "delete a draft plan"},
	{"archive_plan", "archive a finished plan"},
	// plan shared findings
	{"record_finding", "record a shared finding on a plan"},
	{"list_findings", "list a plan's shared findings"},
	// orchestration engine (P2-T2) — low-frequency graph authoring tools
	{"create_graph", "create an orchestration graph for a plan"},
	{"get_graph", "get graph detail with nodes and edges"},
	{"start_graph", "start an orchestration graph (draft to running)"},
	{"finish_graph", "finish an orchestration graph (running to done)"},
	{"add_graph_node", "add a node to an orchestration graph"},
	{"remove_graph_node", "remove a node from an orchestration graph"},
	{"update_graph_node", "update a graph node's title or metadata"},
	{"start_graph_node", "start a graph node (transition to running)"},
	{"complete_graph_node", "complete a graph node with an outcome"},
	{"discard_graph_node", "discard a graph node"},
	{"resolve_condition", "resolve a condition node (success/failure)"},
	{"add_graph_edge", "add a dependency edge between graph nodes"},
	{"remove_graph_edge", "remove a dependency edge between graph nodes"},
	{"list_graph_nodes", "list all nodes in an orchestration graph"},
	{"get_graph_node", "get a single graph node by ID"},
	{"get_ready_nodes", "get graph nodes whose dependencies are satisfied"},
	{"bind_task_to_node", "bind a task to a graph node"},
	{"unbind_task_from_node", "unbind the task from a graph node"},
	// files — T247 (issue-2dfd42a1): download_file + upload_file are CORE (kept
	// OUT of this deferred manifest). An agent that receives an image/file
	// attachment must be able to fetch it WITHOUT first discovering the tool via
	// search_tools — the wake-message hint tells it to call download_file
	// directly, and post_message attachments depend on upload_file. attach_file
	// (rarer — re-scoping an existing blob) stays deferred.
	{"attach_file", "attach an existing center file into a scope"},
}

// secondaryToolNames returns the deferred tool names (for RemoveTools on the
// tiered default set).
func secondaryToolNames() []string {
	names := make([]string, len(secondaryTools))
	for i, t := range secondaryTools {
		names[i] = t.name
	}
	return names
}

// searchToolsArgs is the (optional) query for search_tools.
type searchToolsArgs struct {
	Query string `json:"query,omitempty" jsonschema:"keywords to find deferred tools by name or purpose (space-separated, OR-matched, case-insensitive). Empty loads ALL deferred tools."`
}

// registerSearchTools registers the search_tools meta-tool on a tiered server.
// It is a mcphost-LOCAL tool (no admin route): it manipulates the live tool set.
func registerSearchTools(srv *mcp.Server, cfg Config) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search_tools",
		Description: "Find and load DEFERRED tools. Your default tool set is the high-frequency core; lower-frequency tools (plans, issues, findings, files, subscriptions, org discovery, node recovery) are loaded on demand. Call search_tools with keywords (e.g. \"plan\", \"issue\", \"file\") and the matching tools become callable immediately; an empty query loads ALL deferred tools. Common deferred read tools: get_issue (read a task's spec from its source issue) via \"issue\", get_plan via \"plan\", download_file (view a file/image someone sent) via \"file\". Discoverability is not absence — if a capability seems missing, search_tools here FIRST before concluding the tool does not exist. Replace semantics: each call loads exactly the tools matching your query (a later call changes the loaded set), so pass every group you need at once. Returns the loaded tool names + summaries.",
	}, makeSearchTools(srv, cfg))
}

// makeSearchTools backs search_tools. It re-registers the full surface
// (idempotent — AddTool is keyed by name) so the matched deferred tools come
// back, then removes the deferred tools that did NOT match. Core tools and
// search_tools itself are never removed.
func makeSearchTools(srv *mcp.Server, cfg Config) mcp.ToolHandlerFor[searchToolsArgs, any] {
	return func(_ context.Context, _ *mcp.CallToolRequest, args searchToolsArgs) (*mcp.CallToolResult, any, error) {
		terms := strings.Fields(strings.ToLower(args.Query))
		type loaded struct {
			Name    string `json:"name"`
			Summary string `json:"summary"`
		}
		matched := make([]loaded, 0)
		unmatched := make([]string, 0)
		for _, t := range secondaryTools {
			if toolMatches(t.name, t.summary, terms) {
				matched = append(matched, loaded{Name: t.name, Summary: t.summary})
			} else {
				unmatched = append(unmatched, t.name)
			}
		}
		// Re-add the full surface (idempotent), then drop the non-matching
		// deferred tools — leaving core + search_tools + the matched tools.
		registerAllTools(srv, cfg)
		srv.RemoveTools(unmatched...)
		return nil, map[string]any{
			"loaded": matched,
			"note":   "These tools are now callable directly. Call search_tools again to load a different set.",
		}, nil
	}
}

// toolMatches reports whether a deferred tool matches the query terms. No terms
// (empty query) matches everything; otherwise any term that is a substring of
// the name or summary matches (OR semantics).
func toolMatches(name, summary string, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	hay := strings.ToLower(name + " " + summary)
	for _, term := range terms {
		if strings.Contains(hay, term) {
			return true
		}
	}
	return false
}
