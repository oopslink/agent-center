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
}

// toolDomain maps each agent-facing tool to its docs domain. Curated on purpose
// (the registry carries no domain tag); a tool absent here → "uncategorized".
var toolDomain = map[string]string{
	// identity / discovery
	"get_my_profile": "identity", "find_org_agent": "identity", "find_org_channel": "identity",
	// work queue
	"get_my_work": "work_queue", "get_my_active_work": "work_queue", "start_work": "work_queue",
	"fail_work": "work_queue", "pause_work": "work_queue", "resume_paused_work": "work_queue",
	"list_my_paused_work": "work_queue", "claim_task": "work_queue", "list_assignment_pool": "work_queue",
	// tasks & issues
	"get_task": "tasks_issues", "list_tasks": "tasks_issues", "create_task": "tasks_issues",
	"assign_task": "tasks_issues", "reassign_task": "tasks_issues", "complete_task": "tasks_issues",
	"discard_task": "tasks_issues", "block_task": "tasks_issues", "unblock_task": "tasks_issues",
	"get_issue": "tasks_issues", "list_issues": "tasks_issues", "create_issue": "tasks_issues",
	"update_issue": "tasks_issues", "close_issue": "tasks_issues", "reopen_issue": "tasks_issues",
	"list_tasks_of_issue": "tasks_issues", "post_issue_message": "tasks_issues",
	// plan / orchestration
	"create_plan": "plan", "add_task_to_plan": "plan", "remove_task_from_plan": "plan",
	"add_plan_dependency": "plan", "remove_plan_dependency": "plan", "start_plan": "plan",
	"stop_plan": "plan", "get_plan": "plan", "list_plans": "plan", "delete_plan": "plan",
	"archive_plan": "plan", "rerun_failed_node": "plan", "resume_paused_node": "plan",
	// conversations / messaging
	"get_my_unread": "conversations", "mark_seen": "conversations", "post_task_message": "conversations",
	"post_message": "conversations", "subscribe": "conversations", "unsubscribe": "conversations",
	"request_input": "conversations",
	// files
	"upload_file": "files", "download_file": "files", "attach_file": "files",
	// findings
	"record_finding": "findings", "list_findings": "findings",
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
	// Build the real per-agent catalog. Handlers are never invoked (we only list),
	// so the seams can be nil.
	srv := mcphost.NewServer(mcphost.Config{AgentID: "doc-export"})
	serverT, clientT := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		return fmt.Errorf("server connect: %w", err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "mcp-tools-export", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		return fmt.Errorf("client connect: %w", err)
	}
	defer cs.Close()

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}

	byDomain := map[string][]Tool{}
	for _, t := range res.Tools {
		dom := toolDomain[t.Name]
		if dom == "" {
			dom = "uncategorized"
			fmt.Fprintf(os.Stderr, "warning: tool %q has no domain mapping (add it to toolDomain)\n", t.Name)
		}
		byDomain[dom] = append(byDomain[dom], Tool{
			Name:        t.Name,
			Summary:     firstSentence(t.Description),
			Description: t.Description,
			Params:      params(t),
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
		"note":    "GENERATED by `make gen-mcp-docs` (cmd/mcp-tools-export) from internal/mcphost — do not edit by hand.",
		"total":   total,
		"domains": domains,
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
