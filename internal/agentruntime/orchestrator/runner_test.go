package orchestrator

import (
	"strings"
	"testing"
)

func TestClaudeRunnerBuilder_Build(t *testing.T) {
	b := NewClaudeRunnerBuilder("")
	argv, err := b.Build("claude-haiku-4-5", "do the thing", "")
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
	if !strings.Contains(joined, "execution unit of the same Agent") ||
		!strings.Contains(joined, "not an external agent or separate accountable party") ||
		!strings.Contains(joined, "Supervisor relays and judges") {
		t.Errorf("executor system prompt should bind executor to same-agent responsibility: %q", joined)
	}
}

func TestCodexRunnerBuilder_Build(t *testing.T) {
	b := NewCodexRunnerBuilder("")
	argv, err := b.Build("gpt-5.5", "do the thing", "")
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
	// T969: the codex executor now streams --json so the orchestrator can extract the
	// result + capture the thread_id (thread.started) for tier-1 resume.
	if !strings.Contains(joined, "--json") {
		t.Errorf("codex executor runner must stream --json (T969): %q", joined)
	}
	// A fresh run is a plain `exec` (no resume subcommand).
	if strings.Contains(joined, "resume") {
		t.Errorf("fresh codex run must not carry a resume subcommand: %q", joined)
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
	if !strings.Contains(last, "execution unit of the same Agent") ||
		!strings.Contains(last, "not an external agent or separate accountable party") ||
		!strings.Contains(last, "Supervisor relays and judges") {
		t.Errorf("codex prompt should bind executor to same-agent responsibility: %q", last)
	}
	if !strings.Contains(last, "do the thing") {
		t.Errorf("codex prompt should contain the goal: %q", last)
	}
}

// TestCodexRunnerBuilder_Resume covers the T969 tier-1 resume argv: a non-empty
// sessionID (a codex thread_id captured from a prior run) makes Build emit
// `codex exec resume <threadID> --json ...`; an empty one is a plain `exec` (tier-2).
func TestCodexRunnerBuilder_Resume(t *testing.T) {
	b := NewCodexRunnerBuilder("")
	argv, err := b.Build("gpt-5.5", "continue the work", "th_abc123")
	if err != nil {
		t.Fatalf("Build resume: %v", err)
	}
	if argv[0] != "codex" || argv[1] != "exec" || argv[2] != "resume" || argv[3] != "th_abc123" {
		t.Fatalf("resume argv head = %q, want [codex exec resume th_abc123]", argv[:min(4, len(argv))])
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "--json") || !strings.Contains(joined, "-m gpt-5.5") {
		t.Errorf("resume argv must keep --json + -m: %q", joined)
	}
}

func TestCodexRunnerBuilder_CustomBinaryAndValidation(t *testing.T) {
	b := NewCodexRunnerBuilder("/opt/codex")
	argv, err := b.Build("gpt-5.5", "p", "")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if argv[0] != "/opt/codex" {
		t.Errorf("binary = %q, want /opt/codex", argv[0])
	}
	if _, err := b.Build("", "p", ""); err == nil {
		t.Error("empty model must error")
	}
	if _, err := b.Build("m", "  ", ""); err == nil {
		t.Error("empty prompt must error")
	}
}

func TestClaudeRunnerBuilder_SessionID(t *testing.T) {
	b := NewClaudeRunnerBuilder("")
	sid := "abcabcab-1111-5222-8333-444444444444"

	// A non-empty session id binds --session-id <sid> for durable/resumable identity.
	argv, err := b.Build("m", "p", sid)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "--session-id "+sid) {
		t.Errorf("argv %q missing --session-id %s", joined, sid)
	}

	// An empty session id keeps the old ephemeral one-shot argv (no --session-id).
	bare, err := b.Build("m", "p", "")
	if err != nil {
		t.Fatalf("Build bare: %v", err)
	}
	if strings.Contains(strings.Join(bare, " "), "--session-id") {
		t.Errorf("empty session id must not add --session-id: %q", bare)
	}
}

// (TestCodexRunnerBuilder_IgnoresSessionID removed in T969: codex now USES sessionID as
// the captured thread_id to build a `resume` argv — see TestCodexRunnerBuilder_Resume.)

func TestResumeSessionArgv(t *testing.T) {
	// A claude fresh argv rewrites --session-id <sid> → --resume <sid>, preserving
	// every other flag in order.
	sid := "abcabcab-1111-5222-8333-444444444444"
	fresh, err := NewClaudeRunnerBuilder("claude").Build("m", "the goal", sid)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	resumed, ok := resumeSessionArgv(fresh)
	if !ok {
		t.Fatalf("resumeSessionArgv did not find --session-id in %q", fresh)
	}
	joined := strings.Join(resumed, " ")
	if !strings.Contains(joined, "--resume "+sid) {
		t.Errorf("resumed argv %q missing --resume %s", joined, sid)
	}
	if strings.Contains(joined, "--session-id") {
		t.Errorf("resumed argv must not keep --session-id: %q", joined)
	}
	// The goal prompt + model survive the rewrite (same conversation, resumed).
	if !strings.Contains(joined, "-p the goal") || !strings.Contains(joined, "--model m") {
		t.Errorf("resumed argv dropped prompt/model: %q", joined)
	}
	if len(resumed) != len(fresh) {
		t.Errorf("resume rewrite changed argv length: %d → %d", len(fresh), len(resumed))
	}

	// A session-less argv (no --session-id, e.g. codex) reports not-resumable.
	if _, ok := resumeSessionArgv([]string{"codex", "exec", "-m", "x", "prompt"}); ok {
		t.Error("session-less argv must report ok=false")
	}
}

func TestClaudeRunnerBuilder_CustomBinaryAndValidation(t *testing.T) {
	b := NewClaudeRunnerBuilder("/opt/claude")
	argv, err := b.Build("m", "p", "")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if argv[0] != "/opt/claude" {
		t.Errorf("binary = %q, want /opt/claude", argv[0])
	}
	if _, err := b.Build("", "p", ""); err == nil {
		t.Error("empty model must error")
	}
	if _, err := b.Build("m", "  ", ""); err == nil {
		t.Error("empty prompt must error")
	}
}
