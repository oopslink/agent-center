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

// Config carries everything NewServer needs. It is intentionally
// transport-agnostic (an AdminCaller, not a concrete HTTP client) so the
// server is testable with a fake.
type Config struct {
	// AgentID is the process-fixed operating agent. Injected into every
	// admin call body as agent_id; never read from tool args.
	AgentID string
	// Admin is the transport seam to the center's admin agent-tool
	// endpoints.
	Admin AdminCaller
}

// NewServer builds the per-agent MCP server, registers the b3-i tools, and
// returns it WITHOUT running it. The caller runs it (srv.Run with a
// transport) so tests can attach an in-process transport.
func NewServer(cfg Config) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "agent-center-mcp-host",
		Version: "0.1.0",
	}, nil)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_my_work",
		Description: "List the calling agent's own work items (its task queue + history).",
	}, makeGetMyWork(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "post_task_message",
		Description: "Post a message into a task the calling agent participates in.",
	}, makePostTaskMessage(cfg))

	return srv
}

// getMyWorkArgs is argless: get_my_work is inherently own-scoped (the
// agent reads only its own queue), and agent_id is process-fixed, so there
// is nothing for the model to supply.
type getMyWorkArgs struct{}

// makeGetMyWork returns the get_my_work handler bound to cfg. The forwarded
// body carries ONLY the process-fixed agent_id.
func makeGetMyWork(cfg Config) mcp.ToolHandlerFor[getMyWorkArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ getMyWorkArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{"agent_id": cfg.AgentID}
		return callAdmin(ctx, cfg, "get_my_work", body)
	}
}

// postTaskMessageArgs is the typed input for post_task_message. Note there
// is deliberately NO agent_id field: it is process-fixed and injected by
// the handler, so the model cannot spoof which agent posts.
type postTaskMessageArgs struct {
	TaskID string `json:"task_id" jsonschema:"the task to post into"`
	Text   string `json:"text" jsonschema:"the message text"`
}

// makePostTaskMessage returns the post_task_message handler bound to cfg.
// agent_id is injected from cfg, NEVER from args.
func makePostTaskMessage(cfg Config) mcp.ToolHandlerFor[postTaskMessageArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args postTaskMessageArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"task_id":  args.TaskID,
			"text":     args.Text,
		}
		return callAdmin(ctx, cfg, "post_task_message", body)
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
		Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}},
	}, nil, nil
}
