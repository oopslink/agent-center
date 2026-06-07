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

	// v2.8.1 #278 D (pull model): the agent works its OWN queue one item at a
	// time — pick a queued item from get_my_work, start_work it (mark running),
	// do it, complete_task it, then start_work the next. Only one work item may
	// be running at a time (start_work returns agent_busy if one is already
	// active). fail_work reports the running item as failed.
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "start_work",
		Description: "Start working on one of your queued work items (mark it running). Pick a work_item_id from get_my_work. Only ONE work item can be running at a time — finish (complete_task) or fail (fail_work) the current one before starting the next. Returns agent_busy if you already have a running item.",
	}, makeStartWork(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "fail_work",
		Description: "Report that the work item you are currently running has failed (cannot be completed). Frees you to start the next queued item.",
	}, makeFailWork(cfg))

	// v2.8.1 #278 PR4 scheduling autonomy: pause the current task to switch, then
	// optionally resume it later.
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "pause_work",
		Description: "Pause your currently-running work item to switch to another (sets it aside, freeing you to start_work a different item). Resume it later with resume_paused_work. Use only when scheduling needs it — by default finish your current task first.",
	}, makePauseWork(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "resume_paused_work",
		Description: "Resume a previously paused work item (pick its id from list_my_paused_work) — marks it running again. Only ONE work item can be running at a time, so finish or pause your current one first (returns agent_busy otherwise).",
	}, makeResumeWork(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_my_active_work",
		Description: "List your currently-RUNNING work item(s). Call this at the start of your loop (on wake / after finishing a task): if you have an active item, resume/continue it (do NOT start a new one); if empty, call get_my_work to pick the next queued item.",
	}, makeGetMyActiveWork(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_my_paused_work",
		Description: "List your paused work items — the candidates you can resume_paused_work later.",
	}, makeListMyPausedWork(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_my_unread",
		Description: "List unread messages directed at you — every unread message in your DMs plus every unread @mention of you in channels you're in (excludes channel chatter you weren't mentioned in, and your own messages). Check this periodically and when you reach a stopping point. You MUST reply to each one (acknowledge + defer, handle now, or decline with a reason) — your reply IS your decision.",
	}, makeGetMyUnread(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "post_task_message",
		Description: "Post a message into a task the calling agent participates in.",
	}, makePostTaskMessage(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "post_message",
		Description: "Reply in a DM or channel the calling agent participates in (e.g. when a human messages or @mentions the agent). Use the conversation_id from the message you were given.",
	}, makePostMessage(cfg))

	// --- self / org-discovery tools (v2.7.1 #239) ----------------------------
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_my_profile",
		Description: "Get the calling agent's own profile: organization, the projects it belongs to (with role + what it can do in each), and its capabilities. Call this at the start to learn who you are and where you can act — no need to ask a human.",
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
		Name:        "get_issue",
		Description: "Get an issue the calling agent's own task derives from (own-scope).",
	}, makeGetIssue(cfg))

	// --- pm write/passthrough tools ------------------------------------------
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_task",
		Description: "Create a task in a project the calling agent belongs to.",
	}, makeCreateTask(cfg))

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
		Name:        "request_input",
		Description: "Post a question to a task and park the calling agent's work item waiting for input.",
	}, makeRequestInput(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "block_task",
		Description: "Post a reason and move the task to blocked.",
	}, makeBlockTask(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "complete_task",
		Description: "Optionally post a summary and move the task to completed.",
	}, makeCompleteTask(cfg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "verify_task",
		Description: "Verify a completed task (the calling agent may not verify its own completion).",
	}, makeVerifyTask(cfg))

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
		Name:        "attach_file",
		Description: "Attach an existing center file into a scope in the calling agent's own domain.",
	}, makeAttachFile(cfg))

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
		// The model-facing arg is "text" (natural), but the admin
		// post_task_message endpoint reads "content" (and 400s on
		// missing_content), so forward the value under "content".
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"task_id":  args.TaskID,
			"content":  args.Text,
		}
		return callAdmin(ctx, cfg, "post_task_message", body)
	}
}

// postMessageArgs is the typed input for post_message (v2.7 #185). Like
// post_task_message there is NO agent_id field — it is process-fixed and
// injected by the handler so the model cannot spoof which agent posts.
type postMessageArgs struct {
	ConversationID string `json:"conversation_id" jsonschema:"the conversation to reply in (use the conversation_id from the message you received)"`
	Text           string `json:"text" jsonschema:"the message text"`
}

// makePostMessage returns the post_message handler bound to cfg. agent_id is
// injected from cfg, NEVER from args.
func makePostMessage(cfg Config) mcp.ToolHandlerFor[postMessageArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args postMessageArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":        cfg.AgentID,
			"conversation_id": args.ConversationID,
			"content":         args.Text,
		}
		return callAdmin(ctx, cfg, "post_message", body)
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
