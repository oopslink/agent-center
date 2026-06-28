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
		"--setting-sources user,project",
		"--permission-mode bypassPermissions",
		"--dangerously-skip-permissions",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv %q missing %q", joined, want)
		}
	}
	// The executor must NEVER get an mcp config or streaming output (isolation +
	// one-shot result capture).
	if strings.Contains(joined, "mcp-config") || strings.Contains(joined, "--mcp") {
		t.Errorf("executor runner must not carry an mcp config: %q", joined)
	}
	if strings.Contains(joined, "stream-json") {
		t.Errorf("executor runner must be one-shot text, not stream-json: %q", joined)
	}
	// The system prompt must frame the executor as having NO center/mcp access.
	if !strings.Contains(joined, "NO access to the agent-center") {
		t.Errorf("executor system prompt should forbid center access: %q", joined)
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
