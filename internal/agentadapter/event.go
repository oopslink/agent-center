package agentadapter

import (
	"encoding/json"
	"time"
)

// EventType is the normalised agent trace event type (05-agent-adapters
// § 3, 7 known kinds).
type EventType string

const (
	EventThinking     EventType = "thinking"
	EventToolCall     EventType = "tool_call"
	EventToolResult   EventType = "tool_result"
	EventTokensReport EventType = "tokens_report"
	EventTurnEnd      EventType = "turn_end"
	EventError        EventType = "error"
	EventUnknown      EventType = "unknown"
)

// IsValid checks enum membership.
func (t EventType) IsValid() bool {
	switch t {
	case EventThinking, EventToolCall, EventToolResult, EventTokensReport,
		EventTurnEnd, EventError, EventUnknown:
		return true
	}
	return false
}

// AgentTraceEvent is the normalised single-line agent stream-json event
// (05-agent-adapters § 3).
type AgentTraceEvent struct {
	Type       EventType       `json:"type"`
	Seq        int64           `json:"seq"`
	OccurredAt time.Time       `json:"occurred_at"`
	ToolName   string          `json:"tool_name,omitempty"`
	ToolInput  json.RawMessage `json:"tool_input,omitempty"`
	ToolOutput json.RawMessage `json:"tool_output,omitempty"`
	Thinking   string          `json:"thinking,omitempty"`
	TokensIn   int             `json:"tokens_in,omitempty"`
	TokensOut  int             `json:"tokens_out,omitempty"`
	Raw        json.RawMessage `json:"raw,omitempty"`
	// CliType is the original `type` field value from the CLI's JSONL.
	// Used by UnknownEventReporter to dedupe.
	CliType string `json:"cli_type,omitempty"`
}
