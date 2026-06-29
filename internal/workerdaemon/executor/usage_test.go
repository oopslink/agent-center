package executor

import (
	"strings"
	"testing"
	"time"
)

// claudeResult builds a claude 2.1.156 stream-json `result` line with the given
// token counts (the shape claudestream.ParseStreamLine decodes).
func claudeResult(in, out, cacheRead, cacheWrite int) string {
	return `{"type":"result","subtype":"success","is_error":false,"result":"done","usage":{` +
		`"input_tokens":` + itoa(in) +
		`,"output_tokens":` + itoa(out) +
		`,"cache_read_input_tokens":` + itoa(cacheRead) +
		`,"cache_creation_input_tokens":` + itoa(cacheWrite) +
		`}}`
}

func TestParseRunnerUsage_SumsAcrossResultLines(t *testing.T) {
	// Two turns: the run aggregate is the per-field sum across both result lines.
	out := strings.Join([]string{
		`claude starting…`, // banner — skipped
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`,
		claudeResult(100, 50, 20, 10),
		`{"type":"assistant","message":{"content":[{"type":"text","text":"more"}]}}`,
		claudeResult(7, 3, 1, 2),
	}, "\n")

	got := ParseRunnerUsage(out)
	want := TokenUsage{InputTokens: 107, OutputTokens: 53, CacheReadTokens: 21, CacheWriteTokens: 12}
	if got != want {
		t.Fatalf("ParseRunnerUsage = %+v, want %+v", got, want)
	}
}

func TestParseRunnerUsage_NoResultLineIsZero(t *testing.T) {
	// A non-stream runner (codex plain text) or output with no result line → zero.
	for _, out := range []string{
		"",
		"plain codex output\nwith no json\n",
		`{"type":"assistant","message":{"content":[{"type":"text","text":"x"}]}}`,
		"{ not valid json\n}{also broken",
	} {
		if got := ParseRunnerUsage(out); !got.IsZero() {
			t.Errorf("ParseRunnerUsage(%q) = %+v, want zero", out, got)
		}
	}
}

func TestParseRunnerUsage_ResultWithoutUsageIsZero(t *testing.T) {
	// A result line that omits `usage` contributes zero (defaults), not an error.
	out := `{"type":"result","subtype":"success","is_error":false,"result":"done"}`
	if got := ParseRunnerUsage(out); !got.IsZero() {
		t.Errorf("ParseRunnerUsage = %+v, want zero for usage-less result", got)
	}
}

func TestParseRunnerUsage_SkipsInterleavedStderr(t *testing.T) {
	// CombinedOutput interleaves stderr; non-JSON lines must not derail the parse.
	out := "WARN: deprecation notice\n" + claudeResult(5, 6, 0, 0) + "\nERROR trailing noise"
	got := ParseRunnerUsage(out)
	if got.InputTokens != 5 || got.OutputTokens != 6 {
		t.Errorf("ParseRunnerUsage = %+v, want input=5 output=6", got)
	}
}

func TestOutput_ValidateUsage(t *testing.T) {
	base := Output{ExecutorID: "exec-1", Success: true, FinishedAt: time.Unix(1700000000, 0)}
	// Valid usage passes.
	base.Usage = &TokenUsage{InputTokens: 10, OutputTokens: 5}
	if err := base.Validate(); err != nil {
		t.Errorf("valid output+usage: %v", err)
	}
	// Negative usage fails the output validation.
	base.Usage = &TokenUsage{InputTokens: -1}
	if err := base.Validate(); err == nil {
		t.Error("output with negative usage must fail Validate")
	}
	// nil usage is fine (the common no-usage case).
	base.Usage = nil
	if err := base.Validate(); err != nil {
		t.Errorf("output with nil usage: %v", err)
	}
}

func TestTokenUsage_IsZeroAndValidate(t *testing.T) {
	if !(TokenUsage{}).IsZero() {
		t.Error("zero value must be IsZero")
	}
	if (TokenUsage{OutputTokens: 1}).IsZero() {
		t.Error("non-zero output must not be IsZero")
	}
	if err := (TokenUsage{InputTokens: 1, OutputTokens: 2}).Validate(); err != nil {
		t.Errorf("valid usage: %v", err)
	}
	for _, bad := range []TokenUsage{
		{InputTokens: -1},
		{OutputTokens: -1},
		{CacheReadTokens: -1},
		{CacheWriteTokens: -1},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("negative usage %+v must fail Validate", bad)
		}
	}
}
