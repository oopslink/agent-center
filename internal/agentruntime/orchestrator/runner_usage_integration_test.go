package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
)

// fakeClaudeScript writes an executable shell script that emulates `claude`: it
// emits claude stream-json (a result line carrying usage) ONLY when the real
// ClaudeRunnerBuilder argv asked for it (--output-format stream-json); otherwise it
// prints plain text with no usage. This makes the test fail if the builder ever
// regresses back to plain `-p` text mode (the T622 production bug), because then the
// fake prints no stream-json and ParseRunnerStream finds zero usage.
func fakeClaudeScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.sh")
	// Scan argv for the literal "stream-json" token (the builder passes
	// `--output-format stream-json` as two args). Emit usage-bearing stream-json
	// when present; plain text otherwise.
	script := `#!/bin/sh
for arg in "$@"; do
  if [ "$arg" = "stream-json" ]; then
    printf '%s\n' '{"type":"system","subtype":"init"}'
    printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"All done."}]}}'
    printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"All done.","usage":{"input_tokens":321,"output_tokens":88,"cache_read_input_tokens":9,"cache_creation_input_tokens":3}}'
    exit 0
  fi
done
printf '%s\n' 'plain text result, no usage emitted'
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestIntegration_ClaudeBuilderArgvProducesParseableUsage closes the T622 gap: the
// per-component unit tests fed stream-json straight to ParseRunnerUsage and so never
// caught that the REAL ClaudeRunnerBuilder argv ran claude in plain `-p` text mode
// (no stream-json → no usage → usage_event empty in production).
//
// This drives the real chain: ClaudeRunnerBuilder.Build → executor.CommandRunner
// runs a genuine subprocess (the fake claude above, which only emits stream-json
// because the argv requested it) → ParseRunnerStream. It asserts BOTH that usage is
// non-zero (the builder really makes claude emit usage) AND that the final result
// TEXT is extracted from the stream (not the raw JSON relayed as the task result).
func TestIntegration_ClaudeBuilderArgvProducesParseableUsage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake claude is a POSIX shell script")
	}
	bin := fakeClaudeScript(t)

	argv, err := NewClaudeRunnerBuilder(bin).Build("claude-opus-4-8", "do the thing", "")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// The REAL CommandRunner (production execRun) forks the argv as a subprocess.
	res, err := executor.NewCommandRunner(argv).Run(context.Background(), executor.RunContext{
		WorkspaceDir: t.TempDir(),
		Progress: func(string, string, ...string) {},
	})
	if err != nil {
		t.Fatalf("CommandRunner.Run: %v", err)
	}

	// Usage parsed from the genuine subprocess output — non-zero proves the builder
	// argv actually produced stream-json usage (the production bug was zero here).
	want := executor.TokenUsage{InputTokens: 321, OutputTokens: 88, CacheReadTokens: 9, CacheWriteTokens: 3}
	if res.Usage != want {
		t.Errorf("parsed usage = %+v, want %+v (builder argv must yield stream-json usage)", res.Usage, want)
	}
	if res.Usage.IsZero() {
		t.Fatal("usage is zero — the real runner argv did not produce parseable usage (T622 regression)")
	}

	// The relayed result is the EXTRACTED final text, not the raw JSON stream.
	if res.Result != "All done." {
		t.Errorf("result = %q, want extracted text %q", res.Result, "All done.")
	}
	if strings.Contains(res.Result, "input_tokens") || strings.Contains(res.Result, `"type"`) {
		t.Errorf("result must be the final text, not raw stream-json: %q", res.Result)
	}
	if res.Summary != "All done." {
		t.Errorf("summary = %q, want %q", res.Summary, "All done.")
	}
}

// TestIntegration_NonStreamRunnerRelaysRawResult guards the fallback: a runner whose
// output is NOT claude stream-json (e.g. the codex plain-text executor, or a builder
// regression) must still relay its plain result text — never an empty result — and
// simply report no usage.
func TestIntegration_NonStreamRunnerRelaysRawResult(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' 'codex finished the task'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := executor.NewCommandRunner([]string{path}).Run(context.Background(), executor.RunContext{
		WorkspaceDir: t.TempDir(),
		Progress: func(string, string, ...string) {},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Usage.IsZero() {
		t.Errorf("non-stream output must yield zero usage, got %+v", res.Usage)
	}
	if !strings.Contains(res.Result, "codex finished the task") {
		t.Errorf("plain result must be relayed raw, got %q", res.Result)
	}
}
