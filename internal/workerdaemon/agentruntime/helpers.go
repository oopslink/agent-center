package agentruntime

// helpers.go — pure, stateless helpers moved down with the session面 (brief/payload
// rendering, mcp-config + cli-marker file writes). workerdaemon re-exports the ones
// its remaining code / tests still call.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oopslink/agent-center/internal/claudestream"
	"github.com/oopslink/agent-center/internal/conversation"
)

// mcpConfigFileName is the file Start writes the --mcp-config document to under
// HomeDir. MUST match the name the supervisor reads.
const mcpConfigFileName = "mcp_config.runtime.json"

// WriteMCPConfig writes the config bytes under homeDir and returns the path. Returns
// ("", nil) when there is nothing to write.
func WriteMCPConfig(homeDir string, b []byte) (string, error) {
	if len(b) == 0 {
		return "", nil
	}
	if homeDir == "" {
		return "", errors.New("claude_session: home_dir required to write mcp-config")
	}
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		return "", fmt.Errorf("claude_session: mkdir home_dir: %w", err)
	}
	path := filepath.Join(homeDir, mcpConfigFileName)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", fmt.Errorf("claude_session: write mcp-config: %w", err)
	}
	return path, nil
}

// WriteAgentCLIMarker writes the per-agent-home cli marker (codex start).
func WriteAgentCLIMarker(home, cli string) error {
	if home == "" {
		return errors.New("agent_controller: home required for cli marker")
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(home, AgentCLIMarkerFile), []byte(cli), 0o600)
}

// ReadAgentCLIMarker returns the persisted cli for an agent home, or "" if absent.
func ReadAgentCLIMarker(home string) string {
	b, err := os.ReadFile(filepath.Join(home, AgentCLIMarkerFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// messageDeliveredPayload renders the message_delivered activity JSON payload.
func messageDeliveredPayload(req ConverseRequest) string {
	preview := req.MessageText
	if r := []rune(preview); len(r) > 200 {
		preview = string(r[:200])
	}
	b, err := json.Marshal(map[string]any{
		"conversation_id":   req.ConversationID,
		"message_id":        req.MessageID,
		"sender_ref":        req.SenderRef,
		"sender_display":    req.SenderDisplay,
		"content_preview":   preview,
		"attachments_count": req.AttachmentCount,
	})
	if err != nil {
		return "{}"
	}
	return string(b)
}

// BuildConverseBrief renders the stdin brief injected for an agent.converse.
func BuildConverseBrief(req ConverseRequest) string {
	sender := strings.TrimSpace(req.SenderDisplay)
	if sender == "" {
		sender = strings.TrimSpace(req.SenderRef)
	}
	var header string
	var anchorNote string
	showConvNote := true
	if oc, ok := conversation.ResolveOwnerContext(req.OwnerRef); ok && oc.Anchored {
		oc.Name = strings.TrimSpace(req.ConvName)
		if oc.Name != "" {
			header = fmt.Sprintf("[%s chat — %q (%s=%s)] %s mentioned you:", oc.Label, oc.Name, oc.IDField, oc.ID, sender)
		} else {
			header = fmt.Sprintf("[%s chat (%s=%s)] %s mentioned you:", oc.Label, oc.IDField, oc.ID, sender)
		}
		anchorNote = fmt.Sprintf("(This message belongs to %s=%s. When it refers to \"this %s\" — e.g. completing, archiving, or editing it — act on THAT %s, not any other %s you may also be in.)", oc.IDField, oc.ID, string(oc.Kind), oc.IDField, string(oc.Kind))
		if oc.Kind != conversation.OwnerKindPlan {
			showConvNote = false
		}
	} else if req.ConvKind == "channel" {
		where := strings.TrimSpace(req.ConvName)
		if where == "" {
			where = "a channel"
		}
		header = fmt.Sprintf("[Channel #%s] %s mentioned you:", where, sender)
	} else {
		header = fmt.Sprintf("[Direct message from %s]:", sender)
	}
	convNote := ""
	if showConvNote {
		convNote = " This is a conversation, not a task — there is no work item to complete."
	}
	replyHint := fmt.Sprintf("(To reply, use the post_message tool with conversation_id=%q.%s)", req.ConversationID, convNote)
	if root := strings.TrimSpace(req.RootMessageID); root != "" {
		replyHint = fmt.Sprintf("(You were mentioned INSIDE a thread. To reply IN that thread, use the post_message tool with conversation_id=%q AND parent_message_id=%q — do not omit parent_message_id, or your reply will land outside the thread.)", req.ConversationID, root)
	}
	if anchorNote != "" {
		replyHint = anchorNote + "\n" + replyHint
	}
	body := req.MessageText
	if req.AttachmentCount > 0 {
		noun := "attachment"
		if req.AttachmentCount > 1 {
			noun = "attachments"
		}
		body = fmt.Sprintf("%s\n\n[This message has %d file %s. Call get_my_unread to get their file_uri(s), then download_file each into your workspace and read the saved file (images included) to view them.]", body, req.AttachmentCount, noun)
	}
	return fmt.Sprintf("%s\n%s\n\n%s", header, body, replyHint)
}

// converseErrorSummary builds a short failure summary from the result event.
func converseErrorSummary(ev claudestream.StreamEvent) string {
	s := strings.TrimSpace(ev.Subtype)
	if s == "" {
		s = "error"
	}
	if r := strings.TrimSpace(ev.Result); r != "" {
		const max = 200
		if len(r) > max {
			r = r[:max] + "…"
		}
		s = s + ": " + r
	}
	return s
}

// StreamActivityPayload builds the JSON activity payload for a StreamEvent.
func StreamActivityPayload(ev claudestream.StreamEvent, toolName string) map[string]any {
	p := map[string]any{"type": ev.Type}
	switch ev.Type {
	case "assistant_text", "thinking":
		p["text"] = ev.Text
	case "tool_use":
		p["tool_name"] = ev.ToolName
		p["tool_use_id"] = ev.ToolUseID
		if len(ev.ToolInput) > 0 {
			p["args"] = ev.ToolInput
			p["tool_input"] = ev.ToolInput
		}
	case "tool_result":
		p["tool_use_id"] = ev.ToolUseID
		if toolName != "" {
			p["tool_name"] = toolName
		}
		p["ok"] = !ToolResultIsError(ev.ToolResult)
		if len(ev.ToolResult) > 0 {
			p["tool_result"] = ev.ToolResult
		}
	case "system":
		p["subtype"] = ev.Subtype
		if ev.Subtype == "init" {
			MergeSystemInitFields(p, ev.Raw)
		}
	case "result":
		p["subtype"] = ev.Subtype
		p["result"] = ev.Result
		p["stop_reason"] = ev.StopReason
		p["is_error"] = ev.IsError
		p["cost_usd"] = ev.CostUSD
		p["tokens_in"] = ev.TokensIn
		p["tokens_out"] = ev.TokensOut
	}
	if len(ev.Raw) > 0 {
		p["raw"] = ev.Raw
	}
	return p
}

// ActivityEventType maps a StreamEvent to the standardized activity event_type.
func ActivityEventType(ev claudestream.StreamEvent) string {
	if ev.Type == "system" && ev.Subtype == "init" {
		return "system_init"
	}
	return ev.Type
}

// ToolResultIsError reports whether a claude tool_result content block carries an
// is_error flag.
func ToolResultIsError(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var probe struct {
		IsError bool `json:"is_error"`
	}
	_ = json.Unmarshal(raw, &probe)
	return probe.IsError
}

// MergeSystemInitFields extracts {model, session_id, mcp_servers} from the raw
// system-init line into the payload.
func MergeSystemInitFields(p map[string]any, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var probe struct {
		Model      string          `json:"model"`
		SessionID  string          `json:"session_id"`
		MCPServers json.RawMessage `json:"mcp_servers"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return
	}
	if probe.Model != "" {
		p["model"] = probe.Model
	}
	if probe.SessionID != "" {
		p["session_id"] = probe.SessionID
	}
	if len(probe.MCPServers) > 0 {
		p["mcp_servers"] = probe.MCPServers
	}
}
