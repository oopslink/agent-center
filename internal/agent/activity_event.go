package agent

import (
	"errors"
	"strings"
	"time"
)

// Activity event_type values (v2.7.1 #216). Standardized subtypes the Web Console
// AgentDetail Activity stream renders (badge + content preview); the worker daemon
// emitter (agent_controller.go streamActivityPayload) uses these instead of string
// literals so producer + consumer agree. Payload schema per subtype (omitempty —
// a field is absent when the claude stream doesn't carry it):
//
//	EventTypeSystemInit    {model?, session_id?, mcp_servers?}
//	EventTypeAssistantText {text}
//	EventTypeThinking      {text}
//	EventTypeToolUse       {tool_name, args?, tool_use_id?}
//	EventTypeToolResult    {tool_name?, ok, tool_use_id?, tool_result?}
//	EventTypeResult        {subtype, is_error, result?, stop_reason?, cost_usd?, tokens_in?, tokens_out?}
//	EventTypeLifecycle     {event, ...}
//
// NOTE (v2.7.1 #216): no "status_change" constant — WorkItem lifecycle is already
// shown via the work-item row status badge; duplicating it into the activity stream
// would be two paths for one source (no-middle-state). A "defined but never emitted"
// constant is itself misleading, so it is intentionally absent until a future task
// actually emits it.
const (
	EventTypeSystemInit    = "system_init"
	EventTypeAssistantText = "assistant_text"
	EventTypeThinking      = "thinking"
	EventTypeToolUse       = "tool_use"
	EventTypeToolResult    = "tool_result"
	EventTypeResult        = "result"
	EventTypeLifecycle     = "lifecycle"
	EventTypeRateLimit     = "rate_limit"
	EventTypeUnknown       = "unknown"
	// v2.15 message-consumption activity (docs/design/features/agent-message-consumption-activity.md):
	// EventTypeMessageDelivered — worker daemon 在 sess.Inject() 后埋点，表示一条入站消息
	// 真正进入 agent 上下文（payload: {conversation_id, message_id, sender_ref, sender_display,
	// content_preview, attachments_count}）。
	// EventTypeMessageAcknowledged — agent 主动调 mark_seen 工具（PULL 路径）后由 ack projector
	// 追加，表示 agent 确认已读（payload: {conversation_id, message_id, previous_last_seen_message_id}）。
	EventTypeMessageDelivered    = "message_delivered"
	EventTypeMessageAcknowledged = "message_acknowledged"
)

// AgentActivityEvent is one entry in the append-only observation stream
// (ADR-0049 §2). Because there is no AgentRun, "what the agent did" is
// reconstructed from this stream. The verbose ClaudeCode stdout (assistant
// text, tool-use) is parsed into these events and does NOT auto-post to the
// Conversation (plan §2.6) — only explicit agent messages reach humans.
//
// TaskRef/InteractionRef are optional: they let the UI group activity by
// Task segment and by interaction (logical turn) within it (plan §2.4).
type AgentActivityEvent struct {
	id             string
	agentID        AgentID
	taskRef        string // optional
	interactionRef string // optional
	eventType      string // e.g. "assistant_text" | "tool_use" | "status" | "lifecycle"
	payload        string // JSON
	occurredAt     time.Time
}

// NewActivityEventInput captures constructor args.
type NewActivityEventInput struct {
	ID             string
	AgentID        AgentID
	TaskRef        string
	InteractionRef string
	EventType      string
	Payload        string
	OccurredAt     time.Time
}

// NewActivityEvent constructs an append-only activity event.
func NewActivityEvent(in NewActivityEventInput) (*AgentActivityEvent, error) {
	if strings.TrimSpace(in.ID) == "" {
		return nil, errors.New("agent: activity event id required")
	}
	if strings.TrimSpace(string(in.AgentID)) == "" {
		return nil, errors.New("agent: activity event agent id required")
	}
	if strings.TrimSpace(in.EventType) == "" {
		return nil, errors.New("agent: activity event type required")
	}
	if in.OccurredAt.IsZero() {
		return nil, errors.New("agent: occurred_at required")
	}
	payload := in.Payload
	if strings.TrimSpace(payload) == "" {
		payload = "{}"
	}
	return &AgentActivityEvent{
		id: in.ID, agentID: in.AgentID, taskRef: in.TaskRef,
		interactionRef: in.InteractionRef, eventType: in.EventType,
		payload: payload, occurredAt: in.OccurredAt.UTC(),
	}, nil
}

// Getters.
func (e *AgentActivityEvent) ID() string             { return e.id }
func (e *AgentActivityEvent) AgentID() AgentID       { return e.agentID }
func (e *AgentActivityEvent) TaskRef() string        { return e.taskRef }
func (e *AgentActivityEvent) InteractionRef() string { return e.interactionRef }
func (e *AgentActivityEvent) EventType() string      { return e.eventType }
func (e *AgentActivityEvent) Payload() string        { return e.payload }
func (e *AgentActivityEvent) OccurredAt() time.Time  { return e.occurredAt }
