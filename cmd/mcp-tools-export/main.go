// Command mcp-tools-export introspects the per-agent MCP catalog
// (internal/mcphost.NewServer) over an in-memory MCP transport and exports the
// full tool list as a structured JS data island consumed by the sites docs page
// (sites/dev/<ver>/mcp-tools.gen.js → window.__MCP_TOOLS__).
//
// The tool NAMES, one-line SUMMARIES and PARAMETERS come straight from the live
// registry (ListTools → Name/Description/InputSchema), so the docs never drift
// from code — re-run after changing a tool and the page updates. The only curated
// bit is the domain grouping (toolDomain below); a tool missing from it lands in
// "Uncategorized" and prints a warning so a newly-added tool is noticed.
//
//	go run ./cmd/mcp-tools-export -out sites/dev/v2.10.3/mcp-tools.gen.js
//
// or via `make gen-mcp-docs`.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/oopslink/agent-center/internal/mcphost"
)

// Domain is one logical group of tools in the docs table.
type Domain struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Tools []Tool `json:"tools"`
}

// Tool is one exported MCP tool row.
type Tool struct {
	Name        string  `json:"name"`
	Summary     string  `json:"summary"`
	Description string  `json:"description"`
	Params      []Param `json:"params"`
	// Tier is the WS5 tool-tiering bucket: "core" = in the default agent tool set;
	// "secondary" = deferred (out of the default ListTools, loaded on demand via
	// search_tools). Derived by diffing the full vs tiered catalog — never hand-set.
	Tier string `json:"tier"`
}

// Param is one input-schema property (name + whether it is required).
type Param struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
}

// domainOrder fixes the section order in the docs page.
var domainOrder = []struct{ key, title string }{
	{"identity", "身份 / 发现 · Identity & Discovery"},
	{"work_queue", "工作队列 · Work Queue"},
	{"tasks_issues", "任务 / Issue · Tasks & Issues"},
	{"plan", "Plan / 编排 · Plan & Orchestration"},
	{"conversations", "会话 / 消息 · Conversations & Messaging"},
	{"files", "文件 · Files"},
	{"findings", "Findings"},
	{"reminders", "提醒 · Reminders"},
}

// toolDomain maps each agent-facing tool to its docs domain. Curated on purpose
// (the registry carries no domain tag); a tool absent here → "uncategorized".
var toolDomain = map[string]string{
	// identity / discovery
	"get_my_profile": "identity", "find_org_agent": "identity", "find_org_channel": "identity",
	// work queue
	"get_my_work": "work_queue", "start_task": "work_queue", "fail_task": "work_queue",
	"pause_task": "work_queue", "resume_task": "work_queue", "claim_task": "work_queue",
	// tasks & issues
	"get_task": "tasks_issues", "list_tasks": "tasks_issues", "create_task": "tasks_issues",
	"assign_task": "tasks_issues", "reassign_task": "tasks_issues", "complete_task": "tasks_issues",
	"discard_task": "tasks_issues", "set_task_issue": "tasks_issues", "block_task": "tasks_issues", "unblock_task": "tasks_issues",
	"get_issue": "tasks_issues", "list_issues": "tasks_issues", "create_issue": "tasks_issues",
	"update_issue": "tasks_issues", "close_issue": "tasks_issues", "reopen_issue": "tasks_issues",
	"list_tasks_of_issue": "tasks_issues",
	// reminders (T206 Cognition)
	"create_reminder": "reminders", "list_reminders": "reminders", "get_reminder": "reminders", "update_reminder": "reminders",
	// plan / orchestration
	"create_plan": "plan", "add_task_to_plan": "plan", "remove_task_from_plan": "plan",
	"add_plan_dependency": "plan", "remove_plan_dependency": "plan", "start_plan": "plan",
	"stop_plan": "plan", "get_plan": "plan", "list_plans": "plan", "delete_plan": "plan",
	"archive_plan": "plan", "rerun_failed_node": "plan", "resume_paused_node": "plan",
	// conversations / messaging
	"get_my_unread": "conversations", "mark_seen": "conversations",
	"post_message": "conversations", "start_dm": "conversations", "subscribe": "conversations", "unsubscribe": "conversations",
	"request_input": "conversations",
	// files
	"upload_file": "files", "download_file": "files", "attach_file": "files",
	// findings
	"record_finding": "findings", "list_findings": "findings",
	// WS5 tiering meta-tool: discovers + loads the deferred (secondary) tools on
	// demand. Only present in the tiered (production) catalog.
	"search_tools": "identity",
}

func main() {
	out := flag.String("out", "", "output .js path (window.__MCP_TOOLS__ data island)")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "mcp-tools-export: -out is required")
		os.Exit(2)
	}
	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, "mcp-tools-export:", err)
		os.Exit(1)
	}
}

func run(out string) error {
	ctx := context.Background()
	// Two catalogs from the SAME source of truth (internal/mcphost.NewServer):
	//   - FULL  (TierTools off): every agent-facing tool — what the docs enumerate.
	//   - TIERED (TierTools on): the small high-frequency DEFAULT set + search_tools;
	//     the deferred (secondary) tools are removed from it.
	// A tool's tier is derived by diffing the two (in tiered ⇒ "core", else
	// "secondary"), so the page's default-vs-on-demand split never drifts from WS5.
	// Handlers are never invoked (we only ListTools), so the seams can be nil.
	full, err := listTools(ctx, mcphost.Config{AgentID: "doc-export"})
	if err != nil {
		return fmt.Errorf("list full catalog: %w", err)
	}
	tiered, err := listTools(ctx, mcphost.Config{AgentID: "doc-export", TierTools: true})
	if err != nil {
		return fmt.Errorf("list tiered catalog: %w", err)
	}
	coreNames := map[string]bool{}
	for _, t := range tiered {
		coreNames[t.Name] = true
	}
	tierOf := func(name string) string {
		if coreNames[name] {
			return "core"
		}
		return "secondary"
	}

	// Enumerate the FULL catalog, plus any tool present ONLY in the tiered catalog
	// (search_tools — the on-demand loader, which the full set does not register).
	allTools := append([]*mcp.Tool{}, full...)
	fullNames := map[string]bool{}
	for _, t := range full {
		fullNames[t.Name] = true
	}
	for _, t := range tiered {
		if !fullNames[t.Name] {
			allTools = append(allTools, t)
		}
	}

	byDomain := map[string][]Tool{}
	coreTotal, secondaryTotal := 0, 0
	for _, t := range allTools {
		dom := toolDomain[t.Name]
		if dom == "" {
			dom = "uncategorized"
			fmt.Fprintf(os.Stderr, "warning: tool %q has no domain mapping (add it to toolDomain)\n", t.Name)
		}
		tier := tierOf(t.Name)
		if tier == "core" {
			coreTotal++
		} else {
			secondaryTotal++
		}
		byDomain[dom] = append(byDomain[dom], Tool{
			Name:        t.Name,
			Summary:     firstSentence(t.Description),
			Description: t.Description,
			Params:      params(t),
			Tier:        tier,
		})
	}

	order := append([]struct{ key, title string }{}, domainOrder...)
	if len(byDomain["uncategorized"]) > 0 {
		order = append(order, struct{ key, title string }{"uncategorized", "Uncategorized"})
	}

	var domains []Domain
	total := 0
	for _, d := range order {
		tools := byDomain[d.key]
		if len(tools) == 0 {
			continue
		}
		sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
		total += len(tools)
		domains = append(domains, Domain{Key: d.key, Title: d.title, Tools: tools})
	}

	payload := map[string]any{
		"note":            "GENERATED by `make gen-mcp-docs` (cmd/mcp-tools-export) from internal/mcphost — do not edit by hand.",
		"total":           total,
		"core_total":      coreTotal,
		"secondary_total": secondaryTotal,
		"domains":         domains,
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	js := "// @generated by cmd/mcp-tools-export — DO NOT EDIT. Run `make gen-mcp-docs`.\n" +
		"window.__MCP_TOOLS__ = " + string(body) + ";\n"
	if err := os.WriteFile(out, []byte(js), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%d tools across %d domains)\n", out, total, len(domains))
	return nil
}

// listTools builds a per-agent catalog for cfg over an in-memory transport and
// returns its tools (ListTools). The server's handler seams stay nil — we only
// enumerate, never invoke.
func listTools(ctx context.Context, cfg mcphost.Config) ([]*mcp.Tool, error) {
	srv := mcphost.NewServer(cfg)
	serverT, clientT := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		return nil, fmt.Errorf("server connect: %w", err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "mcp-tools-export", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		return nil, fmt.Errorf("client connect: %w", err)
	}
	defer cs.Close()
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}
	return res.Tools, nil
}

// firstSentence returns the leading sentence of a description as a compact summary.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ". "); i > 0 {
		return s[:i+1]
	}
	return s
}

// params decodes a tool's InputSchema into a sorted (required-first, then alpha)
// list of property names.
func params(t *mcp.Tool) []Param {
	raw, err := json.Marshal(t.InputSchema)
	if err != nil {
		return nil
	}
	var schema struct {
		Properties map[string]any `json:"properties"`
		Required   []string       `json:"required"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil
	}
	req := map[string]bool{}
	for _, r := range schema.Required {
		req[r] = true
	}
	out := make([]Param, 0, len(schema.Properties))
	for name := range schema.Properties {
		out = append(out, Param{Name: name, Required: req[name]})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Required != out[j].Required {
			return out[i].Required // required first
		}
		return out[i].Name < out[j].Name
	})
	return out
}
