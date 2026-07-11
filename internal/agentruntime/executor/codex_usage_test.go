package executor

import (
	"strings"
	"testing"
)

// ParseCodexRunnerStream must extract the final agent_message, the thread_id (for
// tier-1 resume), and summed usage from a codex --json run.
func TestParseCodexRunnerStream_ResultThreadUsage(t *testing.T) {
	out := strings.Join([]string{
		`{"type":"thread.started","thread_id":"th_abc123"}`,
		`{"type":"item.completed","item":{"id":"m1","type":"agent_message","text":"first"}}`,
		`codex banner line (non-json, skipped)`,
		`{"type":"item.completed","item":{"id":"m2","type":"agent_message","text":"final answer"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":120,"output_tokens":34}}`,
	}, "\n")
	result, threadID, usage := ParseCodexRunnerStream(out)
	if result != "final answer" {
		t.Errorf("result = %q, want last agent_message 'final answer'", result)
	}
	if threadID != "th_abc123" {
		t.Errorf("threadID = %q, want th_abc123 (for resume)", threadID)
	}
	if usage.InputTokens != 120 || usage.OutputTokens != 34 {
		t.Errorf("usage = %+v, want in=120 out=34", usage)
	}
}

// A truncated stream (thread.started never arrived) yields an EMPTY threadID → the
// caller must degrade to tier-2 rerun (fail-loud), never resume-with-empty-id.
func TestParseCodexRunnerStream_NoThreadStarted_EmptyThreadID(t *testing.T) {
	out := `{"type":"item.completed","item":{"type":"agent_message","text":"partial"}}`
	result, threadID, _ := ParseCodexRunnerStream(out)
	if threadID != "" {
		t.Errorf("threadID = %q, want empty (no thread.started → tier-2 fallback)", threadID)
	}
	if result != "partial" {
		t.Errorf("result = %q, want 'partial'", result)
	}
}
