package orchestrator

import (
	"strings"
	"testing"
)

func TestClaudeRunnerBuilder_Build(t *testing.T) {
	b := NewClaudeRunnerBuilder("")
	argv, err := b.Build("claude-haiku-4-5", "do the thing")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if argv[0] != "claude" {
		t.Errorf("binary = %q, want claude (default)", argv[0])
	}
	joined := strings.Join(argv, " ")
	for _, want := range []string{
		"-p do the thing",
		"--model claude-haiku-4-5",
		"--append-system-prompt",
		// T622: stream-json + --verbose so each result line carries token usage.
		"--output-format stream-json",
		"--verbose",
		"--setting-sources user,project",
		"--permission-mode bypassPermissions",
		"--dangerously-skip-permissions",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv %q missing %q", joined, want)
		}
	}
	// The executor must NEVER get an mcp config (isolation: no center tools/creds).
	if strings.Contains(joined, "mcp-config") || strings.Contains(joined, "--mcp") {
		t.Errorf("executor runner must not carry an mcp config: %q", joined)
	}
	// The system prompt must frame the executor as having NO center/mcp access.
	if !strings.Contains(joined, "NO access to the agent-center") {
		t.Errorf("executor system prompt should forbid center access: %q", joined)
	}
}

func TestCodexRunnerBuilder_Build(t *testing.T) {
	b := NewCodexRunnerBuilder("")
	argv, err := b.Build("gpt-5.5", "do the thing")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if argv[0] != "codex" || argv[1] != "exec" {
		t.Errorf("argv[0:2] = %q, want [codex exec]", argv[0:2])
	}
	joined := strings.Join(argv, " ")
	// Auth/permission flags mirror the resident cli=codex session (buildCodexArgv).
	for _, want := range []string{
		"--skip-git-repo-check",
		"--dangerously-bypass-approvals-and-sandbox",
		"-m gpt-5.5",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv %q missing %q", joined, want)
		}
	}
	// One-shot text capture: NO --json (the executor harvests plain combined output).
	if strings.Contains(joined, "--json") {
		t.Errorf("codex executor runner must be plain text, not --json: %q", joined)
	}
	// No mcp / center access.
	if strings.Contains(joined, "mcp") {
		t.Errorf("codex executor runner must not carry mcp config: %q", joined)
	}
	// The prompt argument (last) carries both the executor framing and the goal.
	last := argv[len(argv)-1]
	if !strings.Contains(last, "NO access to the agent-center") {
		t.Errorf("codex prompt should prepend the executor framing: %q", last)
	}
	if !strings.Contains(last, "do the thing") {
		t.Errorf("codex prompt should contain the goal: %q", last)
	}
}

func TestCodexRunnerBuilder_CustomBinaryAndValidation(t *testing.T) {
	b := NewCodexRunnerBuilder("/opt/codex")
	argv, err := b.Build("gpt-5.5", "p")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if argv[0] != "/opt/codex" {
		t.Errorf("binary = %q, want /opt/codex", argv[0])
	}
	if _, err := b.Build("", "p"); err == nil {
		t.Error("empty model must error")
	}
	if _, err := b.Build("m", "  "); err == nil {
		t.Error("empty prompt must error")
	}
}

func TestClaudeRunnerBuilder_CustomBinaryAndValidation(t *testing.T) {
	b := NewClaudeRunnerBuilder("/opt/claude")
	argv, err := b.Build("m", "p")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if argv[0] != "/opt/claude" {
		t.Errorf("binary = %q, want /opt/claude", argv[0])
	}
	if _, err := b.Build("", "p"); err == nil {
		t.Error("empty model must error")
	}
	if _, err := b.Build("m", "  "); err == nil {
		t.Error("empty prompt must error")
	}
}
