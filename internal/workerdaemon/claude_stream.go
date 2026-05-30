// Package workerdaemon: claude_stream.go is the D2-c-ii-D stream-json OUTPUT
// parser for the long-lived ClaudeSession path (claude_session.go). It is the
// CORRECT, validated successor to claudecode.Adapter.ParseEvent for the
// streaming path: ParseEvent switches on FLAT top-level types
// (thinking/tool_use/tool_result/usage/end_turn) which real claude 2.1.156
// NEVER emits at the top level — those are CONTENT BLOCKS nested inside
// `assistant.message.content[]` / `user.message.content[]`.
//
// VALIDATED against a genuine claude 2.1.156 round-trip (the testdata fixture
// internal/workerdaemon/testdata/claude_2.1.156_stream.jsonl). The real
// top-level event types are: "system" (with a subtype, e.g. "init"),
// "assistant", "user", "result", "rate_limit_event". Text / thinking / tool_use
// live in assistant.message.content[]; tool_result lives in
// user.message.content[].
//
// This parser does NOT touch claudecode.adapter — the legacy one-shot path
// still uses ParseEvent and retires with #107.
package workerdaemon

import (
	"encoding/json"
	"fmt"
)

// StreamEvent is the D2 normalised view of ONE thing claude said on stdout. A
// single stream-json line can expand to MANY StreamEvents (an assistant message
// with N content blocks → N events), so ParseStreamLine returns a slice.
type StreamEvent struct {
	// Type is the normalised kind. One of:
	//   "system" | "assistant_text" | "thinking" | "tool_use" |
	//   "tool_result" | "result" | "rate_limit" | "unknown"
	Type string

	// Text carries the block text for "assistant_text" and "thinking".
	Text string

	// ToolName / ToolInput / ToolUseID describe a "tool_use" block.
	ToolName  string
	ToolInput json.RawMessage
	ToolUseID string

	// ToolResult carries the result content for a "tool_result" block;
	// ToolUseID correlates it back to the originating tool_use.
	ToolResult json.RawMessage

	// Subtype is the top-level subtype for "system" (e.g. "init") and "result"
	// (e.g. "success").
	Subtype string

	// Result / StopReason / CostUSD / TokensIn / TokensOut are populated for a
	// "result" event (the terminal summary line).
	Result     string
	StopReason string
	CostUSD    float64
	TokensIn   int
	TokensOut  int

	// Raw is the originating bytes for the activity payload: the whole line for
	// top-level events (system/result/rate_limit/unknown), or the marshaled
	// content block for per-block events (assistant_text/thinking/tool_use/
	// tool_result).
	Raw json.RawMessage
}

// contentBlock is one entry in an assistant/user message's content[] array.
// Only the fields the parser needs are decoded; unknown fields are ignored.
type contentBlock struct {
	Type string `json:"type"`
	// text block
	Text string `json:"text"`
	// thinking block
	Thinking string `json:"thinking"`
	// tool_use block
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
	// tool_result block
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

// messageEnvelope is the `message` object inside an assistant/user line.
type messageEnvelope struct {
	Content []contentBlock `json:"content"`
}

// ParseStreamLine parses ONE claude 2.1.156 stream-json OUTPUT line into zero or
// more StreamEvents. Dispatch is on the top-level `type`:
//
//   - "system"           → one {Type:"system", Subtype, Raw:line}
//   - "assistant"        → one event per message.content[] block:
//     text→assistant_text, thinking→thinking, tool_use→tool_use,
//     any other block type→unknown (never fails)
//   - "user"             → one event per message.content[] block:
//     tool_result→tool_result; text→assistant_text (rare); other→skipped
//   - "result"           → one {Type:"result", Subtype, Result, StopReason, ...}
//   - "rate_limit_event" → one {Type:"rate_limit", Raw:line}
//   - any other type     → one {Type:"unknown", Raw:line} (forward-compatible)
//
// Malformed JSON returns an error (the caller logs + skips the line). Missing
// fields degrade gracefully (empty strings / unknown), never panic.
func ParseStreamLine(line []byte) ([]StreamEvent, error) {
	if len(line) == 0 {
		return nil, fmt.Errorf("claude_stream: empty line")
	}
	var head struct {
		Type    string          `json:"type"`
		Subtype string          `json:"subtype"`
		Message json.RawMessage `json:"message"`
		// result-line fields
		Result       string  `json:"result"`
		StopReason   string  `json:"stop_reason"`
		TotalCostUSD float64 `json:"total_cost_usd"`
		Usage        *usage  `json:"usage"`
	}
	if err := json.Unmarshal(line, &head); err != nil {
		return nil, fmt.Errorf("claude_stream: decode line: %w", err)
	}

	switch head.Type {
	case "system":
		return []StreamEvent{{
			Type:    "system",
			Subtype: head.Subtype,
			Raw:     cloneRaw(line),
		}}, nil

	case "assistant":
		return parseContentBlocks(head.Message, false /*user*/), nil

	case "user":
		return parseContentBlocks(head.Message, true /*user*/), nil

	case "result":
		ev := StreamEvent{
			Type:       "result",
			Subtype:    head.Subtype,
			Result:     head.Result,
			StopReason: head.StopReason,
			CostUSD:    head.TotalCostUSD,
			Raw:        cloneRaw(line),
		}
		if head.Usage != nil {
			ev.TokensIn = head.Usage.InputTokens
			ev.TokensOut = head.Usage.OutputTokens
		}
		return []StreamEvent{ev}, nil

	case "rate_limit_event":
		return []StreamEvent{{
			Type: "rate_limit",
			Raw:  cloneRaw(line),
		}}, nil

	default:
		// Forward-compatible: a top-level type this version doesn't model maps
		// to a single unknown event carrying the raw line — never fails.
		return []StreamEvent{{
			Type: "unknown",
			Raw:  cloneRaw(line),
		}}, nil
	}
}

// usage decodes the input/output token counts from a result line's usage object.
type usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// parseContentBlocks expands an assistant/user message's content[] into one
// StreamEvent per block. fromUser selects the user-line block mapping
// (tool_result) vs the assistant-line mapping (text/thinking/tool_use). A
// missing/unparsable message degrades to a single unknown event rather than
// failing the whole stream.
func parseContentBlocks(rawMsg json.RawMessage, fromUser bool) []StreamEvent {
	if len(rawMsg) == 0 {
		return []StreamEvent{{Type: "unknown", Raw: cloneRaw(rawMsg)}}
	}
	var msg messageEnvelope
	if err := json.Unmarshal(rawMsg, &msg); err != nil {
		// Degrade, don't fail: surface the whole message as unknown.
		return []StreamEvent{{Type: "unknown", Raw: cloneRaw(rawMsg)}}
	}

	out := make([]StreamEvent, 0, len(msg.Content))
	for i := range msg.Content {
		blk := msg.Content[i]
		blkRaw, _ := json.Marshal(blk)
		if fromUser {
			switch blk.Type {
			case "tool_result":
				out = append(out, StreamEvent{
					Type:       "tool_result",
					ToolUseID:  blk.ToolUseID,
					ToolResult: blk.Content,
					Raw:        blkRaw,
				})
			case "text":
				// Rare: a user line carrying plain text. Map it like assistant
				// text so the controller still surfaces it as activity.
				out = append(out, StreamEvent{
					Type: "assistant_text",
					Text: blk.Text,
					Raw:  blkRaw,
				})
			default:
				// Other user content (rare) → skip; not an error.
			}
			continue
		}
		switch blk.Type {
		case "text":
			out = append(out, StreamEvent{
				Type: "assistant_text",
				Text: blk.Text,
				Raw:  blkRaw,
			})
		case "thinking":
			out = append(out, StreamEvent{
				Type: "thinking",
				Text: blk.Thinking,
				Raw:  blkRaw,
			})
		case "tool_use":
			out = append(out, StreamEvent{
				Type:      "tool_use",
				ToolName:  blk.Name,
				ToolInput: blk.Input,
				ToolUseID: blk.ID,
				Raw:       blkRaw,
			})
		default:
			// Unknown block type → unknown event (forward-compatible).
			out = append(out, StreamEvent{Type: "unknown", Raw: blkRaw})
		}
	}
	return out
}

// cloneRaw returns a defensive copy of b so the parsed event does not alias the
// scanner's reused buffer.
func cloneRaw(b []byte) json.RawMessage {
	if len(b) == 0 {
		return nil
	}
	out := make(json.RawMessage, len(b))
	copy(out, b)
	return out
}
