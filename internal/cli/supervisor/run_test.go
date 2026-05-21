package supervisor_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/cli/supervisor"
	"github.com/oopslink/agent-center/internal/observability"
)

// writeFakeClaudeScript writes a tiny shell script that emulates the
// `claude` CLI's stream-json output. The body argument is interpreted as
// the stdout the script should emit.
func writeFakeClaudeScript(t *testing.T, body, exit string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake_claude.sh")
	content := "#!/bin/sh\ncat <<'__EOF__'\n" + body + "\n__EOF__\nexit " + exit + "\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func TestRun_HappyPath(t *testing.T) {
	memDir := t.TempDir()
	usageDir := t.TempDir()
	script := writeFakeClaudeScript(t, `{"type":"thinking","text":"I should dispatch T-1"}
{"type":"tool_use","name":"Bash","input":{"command":"agent-center dispatch T-1 --worker=W-1 --rationale='W-1 idle'"}}
{"type":"tool_result","tool_use_id":"x","content":"OK"}
{"type":"usage","input_tokens":100,"output_tokens":50}
{"type":"end_turn"}`, "0")
	var stdout, stderr bytes.Buffer
	res, err := supervisor.Run(context.Background(), supervisor.RunInput{
		Scope:        "task:T-1",
		InvocationID: "INV1",
		TriggerCSV:   "01HEZX",
		MemoryDir:    memDir,
		UsageDir:     usageDir,
		ClaudeBinary: script,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v stderr=%s", err, stderr.String())
	}
	if res.ExitCode != 0 {
		t.Errorf("exit = %d", res.ExitCode)
	}
	if res.Token.Input != 100 || res.Token.Output != 50 {
		t.Errorf("tokens = %+v", res.Token)
	}
	if res.Decisions != 1 {
		t.Errorf("decisions = %d", res.Decisions)
	}
	// usage file written
	if _, err := os.Stat(filepath.Join(usageDir, "INV1.usage.json")); err != nil {
		t.Errorf("usage file missing: %v", err)
	}
}

func TestRun_FailedExit(t *testing.T) {
	memDir := t.TempDir()
	usageDir := t.TempDir()
	script := writeFakeClaudeScript(t, `{"type":"error","message":"boom"}`, "1")
	var stdout, stderr bytes.Buffer
	res, err := supervisor.Run(context.Background(), supervisor.RunInput{
		Scope:        "task:T-1",
		InvocationID: "INV2",
		TriggerCSV:   "01HEY",
		MemoryDir:    memDir,
		UsageDir:     usageDir,
		ClaudeBinary: script,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.ExitCode != 1 {
		t.Errorf("exit = %d", res.ExitCode)
	}
}

func TestRun_NoTrigger(t *testing.T) {
	memDir := t.TempDir()
	_, err := supervisor.Run(context.Background(), supervisor.RunInput{
		Scope:        "task:T-1",
		InvocationID: "I",
		TriggerCSV:   "",
		MemoryDir:    memDir,
	}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "trigger-events") {
		t.Errorf("expected trigger err, got %v", err)
	}
}

func TestRun_ScopeMissing(t *testing.T) {
	_, err := supervisor.Run(context.Background(), supervisor.RunInput{
		InvocationID: "I",
		TriggerCSV:   "X",
		MemoryDir:    t.TempDir(),
	}, nil, nil)
	if err == nil {
		t.Error("expected scope err")
	}
}

func TestRun_InvocationIDRequired(t *testing.T) {
	_, err := supervisor.Run(context.Background(), supervisor.RunInput{
		Scope:      "task:T-1",
		TriggerCSV: "X",
		MemoryDir:  t.TempDir(),
	}, nil, nil)
	if err == nil {
		t.Error("expected invocation id err")
	}
}

func TestRun_MemoryDirRequired(t *testing.T) {
	_, err := supervisor.Run(context.Background(), supervisor.RunInput{
		Scope: "task:T-1", InvocationID: "I", TriggerCSV: "X",
	}, nil, nil)
	if err == nil {
		t.Error("expected memorydir err")
	}
}

func TestRun_EventLookupWired(t *testing.T) {
	memDir := t.TempDir()
	usageDir := t.TempDir()
	script := writeFakeClaudeScript(t, `{"type":"end_turn"}`, "0")
	called := 0
	res, err := supervisor.Run(context.Background(), supervisor.RunInput{
		Scope:        "issue:I-9",
		InvocationID: "INV3",
		TriggerCSV:   "01HEX,01HEY",
		MemoryDir:    memDir,
		UsageDir:     usageDir,
		ClaudeBinary: script,
		EventLookup: func(_ context.Context, id observability.EventID) (*observability.Event, error) {
			called++
			return nil, observability.ErrEventNotFound
		},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if called != 2 {
		t.Errorf("EventLookup called %d times", called)
	}
	_ = res
}

func TestParseScopeFlag_Variants(t *testing.T) {
	// global aliases
	for _, s := range []string{"global", "global:", "global:_global_"} {
		out, err := supervisor.ExportParseScope(s)
		if err != nil {
			t.Errorf("global %q: %v", s, err)
		}
		if out.Key() != "_global_" {
			t.Errorf("key = %q", out.Key())
		}
	}
	if _, err := supervisor.ExportParseScope("badformat"); err == nil {
		t.Error("expected err")
	}
	if _, err := supervisor.ExportParseScope(""); err == nil {
		t.Error("expected empty err")
	}
}
