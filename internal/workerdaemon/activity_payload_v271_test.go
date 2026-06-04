package workerdaemon

import (
	"encoding/json"
	"testing"

	"github.com/oopslink/agent-center/internal/claudestream"
)

// v2.7.1 #216: the activity payload follows the standardized per-subtype schema the
// Web Console Activity stream reads.
func TestStreamActivityPayload_Schema(t *testing.T) {
	t.Run("assistant_text", func(t *testing.T) {
		p := streamActivityPayload(claudestream.StreamEvent{Type: "assistant_text", Text: "hello"}, "")
		if p["type"] != "assistant_text" || p["text"] != "hello" {
			t.Fatalf("assistant_text payload = %+v", p)
		}
	})
	t.Run("tool_use carries tool_name + args", func(t *testing.T) {
		p := streamActivityPayload(claudestream.StreamEvent{
			Type: "tool_use", ToolName: "Read", ToolUseID: "tu-1",
			ToolInput: json.RawMessage(`{"file":"x"}`),
		}, "")
		if p["tool_name"] != "Read" || p["tool_use_id"] != "tu-1" {
			t.Fatalf("tool_use payload = %+v", p)
		}
		if _, ok := p["args"]; !ok {
			t.Fatalf("tool_use must carry args: %+v", p)
		}
	})
	t.Run("tool_result carries tool_name (correlated) + ok", func(t *testing.T) {
		ok := streamActivityPayload(claudestream.StreamEvent{
			Type: "tool_result", ToolUseID: "tu-1", ToolResult: json.RawMessage(`{"content":"done"}`),
		}, "Read")
		if ok["tool_name"] != "Read" {
			t.Fatalf("tool_result tool_name not correlated: %+v", ok)
		}
		if ok["ok"] != true {
			t.Fatalf("tool_result ok should be true (no is_error): %+v", ok)
		}
		bad := streamActivityPayload(claudestream.StreamEvent{
			Type: "tool_result", ToolUseID: "tu-2", ToolResult: json.RawMessage(`{"is_error":true,"content":"boom"}`),
		}, "Bash")
		if bad["ok"] != false {
			t.Fatalf("tool_result ok should be false on is_error: %+v", bad)
		}
	})
	t.Run("system init carries model/session_id/mcp_servers", func(t *testing.T) {
		raw := json.RawMessage(`{"type":"system","subtype":"init","model":"claude-opus-4-8","session_id":"sess-abc","mcp_servers":[{"name":"x"}]}`)
		p := streamActivityPayload(claudestream.StreamEvent{Type: "system", Subtype: "init", Raw: raw}, "")
		if p["model"] != "claude-opus-4-8" || p["session_id"] != "sess-abc" {
			t.Fatalf("system_init payload = %+v", p)
		}
		if _, ok := p["mcp_servers"]; !ok {
			t.Fatalf("system_init must carry mcp_servers: %+v", p)
		}
	})
}

// v2.7.1 #216: the claude session-init system line maps to the "system_init"
// activity event_type; everything else keeps its stream type.
func TestActivityEventType(t *testing.T) {
	cases := []struct {
		typ, sub, want string
	}{
		{"system", "init", "system_init"},
		{"system", "other", "system"},
		{"assistant_text", "", "assistant_text"},
		{"tool_use", "", "tool_use"},
		{"tool_result", "", "tool_result"},
		{"result", "success", "result"},
	}
	for _, c := range cases {
		if got := activityEventType(claudestream.StreamEvent{Type: c.typ, Subtype: c.sub}); got != c.want {
			t.Errorf("activityEventType(%q,%q)=%q want %q", c.typ, c.sub, got, c.want)
		}
	}
}

func TestToolResultIsError(t *testing.T) {
	if toolResultIsError(json.RawMessage(`{"is_error":true}`)) != true {
		t.Error("is_error:true → true")
	}
	if toolResultIsError(json.RawMessage(`{"is_error":false}`)) != false {
		t.Error("is_error:false → false")
	}
	if toolResultIsError(nil) != false {
		t.Error("empty → false")
	}
	if toolResultIsError(json.RawMessage(`not json`)) != false {
		t.Error("unparseable → false")
	}
}
