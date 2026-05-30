package workerdaemon

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureLines reads the genuine claude 2.1.156 stream fixture, returning its
// non-empty lines (system / assistant / result).
func fixtureLines(t *testing.T) [][]byte {
	t.Helper()
	path := filepath.Join("testdata", "claude_2.1.156_stream.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	var lines [][]byte
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		b := bytes.TrimSpace(sc.Bytes())
		if len(b) == 0 {
			continue
		}
		cp := make([]byte, len(b))
		copy(cp, b)
		lines = append(lines, cp)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixture: %v", err)
	}
	return lines
}

// Faithful Anthropic-shaped tool lines (the simple PONG run used no tools).
// assistant tool_use block inside message.content[]; user tool_result block
// inside message.content[].
const (
	toolUseLine    = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_x","name":"Bash","input":{"command":"echo hi"}}]},"session_id":"s"}`
	toolResultLine = `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_x","content":"hi\n"}]},"session_id":"s"}`
	// assistantTextLine is a faithful Anthropic text-block assistant line. The
	// genuine PONG run emitted only a thinking block in its assistant message
	// (the visible "PONG" came through the result line), so this exercises the
	// text→assistant_text path with the standard content-block shape.
	assistantTextLine = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"PONG"}]},"session_id":"s"}`
)

func TestParseStreamLine_RealFixture(t *testing.T) {
	lines := fixtureLines(t)
	if len(lines) != 3 {
		t.Fatalf("want 3 fixture lines (system/assistant/result), got %d", len(lines))
	}

	// Line 0: system / init.
	evs, err := ParseStreamLine(lines[0])
	if err != nil {
		t.Fatalf("system line: %v", err)
	}
	if len(evs) != 1 || evs[0].Type != "system" || evs[0].Subtype != "init" {
		t.Fatalf("system line → %+v", evs)
	}
	if len(evs[0].Raw) == 0 {
		t.Fatal("system event missing Raw")
	}

	// Line 1: the genuine assistant message → a single thinking block (the
	// PONG run emitted no separate text block; the visible PONG came via the
	// result line below).
	evs, err = ParseStreamLine(lines[1])
	if err != nil {
		t.Fatalf("assistant line: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("assistant line want 1 event (thinking), got %d: %+v", len(evs), evs)
	}
	if evs[0].Type != "thinking" {
		t.Fatalf("assistant block[0] → %+v (want thinking)", evs[0])
	}
	if !strings.Contains(evs[0].Text, "PONG") {
		t.Fatalf("thinking text missing content: %q", evs[0].Text)
	}

	// A faithful text-block assistant line → assistant_text "PONG".
	tevs, err := ParseStreamLine([]byte(assistantTextLine))
	if err != nil {
		t.Fatalf("assistant text line: %v", err)
	}
	if len(tevs) != 1 || tevs[0].Type != "assistant_text" || tevs[0].Text != "PONG" {
		t.Fatalf("assistant text line → %+v (want assistant_text PONG)", tevs)
	}

	// Line 2: result / success.
	evs, err = ParseStreamLine(lines[2])
	if err != nil {
		t.Fatalf("result line: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("result line want 1 event, got %d", len(evs))
	}
	r := evs[0]
	if r.Type != "result" || r.Subtype != "success" || r.Result != "PONG" || r.StopReason != "end_turn" {
		t.Fatalf("result event → %+v", r)
	}
	if r.CostUSD == 0 {
		t.Fatal("result event missing total_cost_usd")
	}
	if r.TokensIn != 10 {
		t.Fatalf("result tokens_in = %d, want 10", r.TokensIn)
	}

	// None of the genuine shapes may be "unknown".
	for _, l := range lines {
		got, err := ParseStreamLine(l)
		if err != nil {
			t.Fatalf("parse %s: %v", l, err)
		}
		for _, e := range got {
			if e.Type == "unknown" {
				t.Fatalf("genuine line parsed as unknown: %s", l)
			}
		}
	}
}

func TestParseStreamLine_ToolUse(t *testing.T) {
	evs, err := ParseStreamLine([]byte(toolUseLine))
	if err != nil {
		t.Fatalf("tool_use line: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("want 1 event, got %d: %+v", len(evs), evs)
	}
	e := evs[0]
	if e.Type != "tool_use" || e.ToolName != "Bash" || e.ToolUseID != "toolu_x" {
		t.Fatalf("tool_use event → %+v", e)
	}
	if !strings.Contains(string(e.ToolInput), "echo hi") {
		t.Fatalf("tool_use input missing command: %s", e.ToolInput)
	}
}

func TestParseStreamLine_ToolResult(t *testing.T) {
	evs, err := ParseStreamLine([]byte(toolResultLine))
	if err != nil {
		t.Fatalf("tool_result line: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("want 1 event, got %d: %+v", len(evs), evs)
	}
	e := evs[0]
	if e.Type != "tool_result" || e.ToolUseID != "toolu_x" {
		t.Fatalf("tool_result event → %+v", e)
	}
	if !strings.Contains(string(e.ToolResult), "hi") {
		t.Fatalf("tool_result content missing: %s", e.ToolResult)
	}
}

func TestParseStreamLine_RateLimit(t *testing.T) {
	evs, err := ParseStreamLine([]byte(`{"type":"rate_limit_event","retry_after":5}`))
	if err != nil {
		t.Fatalf("rate_limit line: %v", err)
	}
	if len(evs) != 1 || evs[0].Type != "rate_limit" {
		t.Fatalf("rate_limit → %+v", evs)
	}
}

func TestParseStreamLine_UnknownTopLevel(t *testing.T) {
	evs, err := ParseStreamLine([]byte(`{"type":"some_future_thing","foo":1}`))
	if err != nil {
		t.Fatalf("unknown top-level must not error: %v", err)
	}
	if len(evs) != 1 || evs[0].Type != "unknown" {
		t.Fatalf("unknown top-level → %+v", evs)
	}
}

func TestParseStreamLine_UnknownContentBlock(t *testing.T) {
	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"future_block","x":1}]}}`
	evs, err := ParseStreamLine([]byte(line))
	if err != nil {
		t.Fatalf("unknown block must not error: %v", err)
	}
	if len(evs) != 1 || evs[0].Type != "unknown" {
		t.Fatalf("unknown block → %+v", evs)
	}
}

func TestParseStreamLine_Malformed(t *testing.T) {
	if _, err := ParseStreamLine([]byte(`{not json`)); err == nil {
		t.Fatal("malformed line must return an error")
	}
	if _, err := ParseStreamLine(nil); err == nil {
		t.Fatal("empty line must return an error")
	}
}

func TestParseStreamLine_MissingFieldsDegrade(t *testing.T) {
	// assistant with no message → single unknown (degrade, not panic/error).
	evs, err := ParseStreamLine([]byte(`{"type":"assistant"}`))
	if err != nil {
		t.Fatalf("assistant w/o message: %v", err)
	}
	if len(evs) != 1 || evs[0].Type != "unknown" {
		t.Fatalf("assistant w/o message → %+v", evs)
	}
	// result with missing fields → result event with empty result, no error.
	evs, err = ParseStreamLine([]byte(`{"type":"result","subtype":"error_max_turns"}`))
	if err != nil {
		t.Fatalf("sparse result: %v", err)
	}
	if len(evs) != 1 || evs[0].Type != "result" || evs[0].Subtype != "error_max_turns" {
		t.Fatalf("sparse result → %+v", evs)
	}
}
