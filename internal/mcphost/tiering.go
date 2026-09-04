package mcphost

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/oopslink/agent-center/internal/agenttools"
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
var secondaryTools = agenttools.SecondaryTools

// secondaryToolNames returns the deferred tool names (for RemoveTools on the
// tiered default set).
func secondaryToolNames() []string {
	return agenttools.SecondaryToolNames()
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
			if toolMatches(t.Name, t.Summary, terms) {
				matched = append(matched, loaded{Name: t.Name, Summary: t.Summary})
			} else {
				unmatched = append(unmatched, t.Name)
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
