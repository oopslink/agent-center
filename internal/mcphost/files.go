// files.go (v2.7 b3-ii, ADR-0049) — the per-agent MCP file tools. Two of the
// three move BYTES (upload_file / download_file) through the FileMover seam
// (the daemon-side FileTransferClient, which enforces workspace path
// containment); attach_file is a pure JSON passthrough to the admin endpoint.
//
// Security spine (same as every other tool): agentRoot + agentID are
// process-fixed — taken from Config, NEVER from tool args — so the model can
// neither move files for another agent nor reach outside the agent's
// workspace. The args structs deliberately carry NO agent_id / agent_root.
//
// Error mapping: the FileMover returns PLAIN errors (workspace path-escape,
// not-reachable, admin non-2xx, etc.) rather than the typed *AdminToolError
// that callAdmin understands. To let claude see the actual reason (e.g. "path
// escapes workspace root") and self-correct rather than treating it as a
// transport failure, file-mover errors are surfaced as an IsError
// CallToolResult carrying err.Error() (see fileError). attach_file, being a
// callAdmin passthrough, reuses callAdmin's existing *AdminToolError → IsError
// mapping.
package mcphost

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// errFilesNotWired is returned (as an IsError result) when a file tool is
// invoked on a host built without a FileMover.
var errFilesNotWired = errors.New("file tools are not available: no file mover configured")

// fileError builds an IsError CallToolResult carrying err.Error(), so a
// FileMover failure (path escape / not reachable / admin error) is shown to
// the model as a tool error it can read, not a protocol-level failure.
func fileError(err error) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}, nil, nil
}

// --- upload_file -------------------------------------------------------------

type uploadFileArgs struct {
	Path    string `json:"path" jsonschema:"local workspace path of the file to upload"`
	Scope   string `json:"scope,omitempty" jsonschema:"optional placement scope (e.g. task, conversation)"`
	ScopeID string `json:"scope_id,omitempty" jsonschema:"optional id for the placement scope"`
}

// makeUploadFile uploads args.Path (resolved inside cfg.AgentRoot by the
// FileMover) for cfg.AgentID and returns the resulting file URI. agentRoot +
// agentID come from cfg, never from args.
func makeUploadFile(cfg Config) mcp.ToolHandlerFor[uploadFileArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args uploadFileArgs) (*mcp.CallToolResult, any, error) {
		if cfg.Files == nil {
			return fileError(errFilesNotWired)
		}
		fileURI, err := cfg.Files.UploadFile(ctx, cfg.AgentRoot, cfg.AgentID, args.Path, args.Scope, args.ScopeID)
		if err != nil {
			return fileError(err)
		}
		out, _ := json.Marshal(map[string]any{"file_uri": fileURI})
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(out)}},
		}, nil, nil
	}
}

// --- download_file -----------------------------------------------------------

type downloadFileArgs struct {
	File     string `json:"file" jsonschema:"the ac://files/{ulid} or bare ulid to download"`
	DestPath string `json:"dest_path" jsonschema:"workspace-relative destination path"`
}

// makeDownloadFile downloads args.File into args.DestPath (resolved inside
// cfg.AgentRoot by the FileMover) for cfg.AgentID. agentRoot + agentID come
// from cfg, never from args.
func makeDownloadFile(cfg Config) mcp.ToolHandlerFor[downloadFileArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args downloadFileArgs) (*mcp.CallToolResult, any, error) {
		if cfg.Files == nil {
			return fileError(errFilesNotWired)
		}
		if err := cfg.Files.DownloadFile(ctx, cfg.AgentRoot, cfg.AgentID, args.File, args.DestPath); err != nil {
			return fileError(err)
		}
		out, _ := json.Marshal(map[string]any{"ok": true, "dest": args.DestPath})
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(out)}},
		}, nil, nil
	}
}

// --- attach_file -------------------------------------------------------------

type attachFileArgs struct {
	FileURI string `json:"file_uri" jsonschema:"the ac://files/{ulid} to attach"`
	Scope   string `json:"scope" jsonschema:"placement scope (must be in the agent's own domain)"`
	ScopeID string `json:"scope_id" jsonschema:"the id for the placement scope"`
}

// makeAttachFile is a pure JSON passthrough to /admin/agent-tools/attach_file
// (no bytes move). agent_id is injected from cfg; callAdmin handles the
// *AdminToolError → IsError mapping.
func makeAttachFile(cfg Config) mcp.ToolHandlerFor[attachFileArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args attachFileArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"file_uri": args.FileURI,
			"scope":    args.Scope,
			"scope_id": args.ScopeID,
		}
		return callAdmin(ctx, cfg, "attach_file", body)
	}
}
