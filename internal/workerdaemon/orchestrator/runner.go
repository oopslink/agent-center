// Package orchestrator is W1 of agent-concurrent-execution phase 2
// (docs/design/features/agent-concurrent-execution.md §4/§5/§8/§11.1): the
// production wiring that chains the v2.17.0 foundations — F4 consistency routing →
// F3 model routing → F2 file protocol → F1 process-model Pool — so the resident
// orchestrator (监工) really forks executors for incoming work.
//
// This package owns the orchestration LOGIC (the Engine) plus the production
// "real runner" the forked executor runs (a no-mcp claude invocation under the
// F3-selected model). It deliberately depends only on the executor (F1/F2/F4) and
// modelrouter (F3) packages plus the claude adapter — never on the worker daemon —
// so it stays unit-testable and the daemon wires it in at the work() seam.
package orchestrator

import (
	"errors"
	"strings"
)

// RunnerCmdBuilder builds the argv the forked executor runs inside its isolated
// workspace (executor.LaunchSpec.RunnerCmd). It is a PORT so tests inject a fake
// and the production default binds the real, model-routed claude invocation (PD
// ruling, W1: the executor must really run a model — no placeholder runner).
type RunnerCmdBuilder interface {
	// Build returns the full argv ([binary, args...]) for the given resolved model
	// and goal prompt. model and prompt are both required.
	Build(model, prompt string) ([]string, error)
}

// executorSystemPrompt is the executor's persistent --append-system-prompt: it
// frames the forked claude as a focused, isolated worker. CRITICAL framing — the
// executor has NO mcp / center tools (it is launched without --mcp-config), so it
// must NOT try to talk to the center; it does the task with its built-in tools in
// its own workspace and reports the result as its final message (the orchestrator
// harvests output.json and does all center writeback — design §3/§4).
//
// ExecutorSystemPrompt returns the executor framing prompt so callers (e.g. the
// daemon's launchExecutor) can persist it to the executor's SYSTEM.md for runtime
// inspection without duplicating the constant.
func ExecutorSystemPrompt() string { return executorSystemPrompt }

const executorSystemPrompt = "You are an isolated executor working a single task in your current working directory. " +
	"You have NO access to the agent-center / MCP tools and NO network credentials for the center — do not attempt to message any chat, update any task, or call center tools. " +
	"Use your built-in tools (read/edit files, run commands) to complete the task entirely within this workspace. " +
	"When finished, your final message must be a concise report of what you did and the outcome; that message is the result the orchestrator relays."

// CodexRunnerBuilder builds the production executor runner for the codex CLI: a
// ONE-SHOT `codex exec` invocation under the F3-selected model (v2.18.1 BE-2,
// issue-8746a5b9 — the cross-CLI executor: a claude-code supervisor may dispatch a
// codex executor for a given task). It mirrors the auth/permission flags of the
// resident cli=codex session (workerdaemon/codex_session.go buildCodexArgv:
// --skip-git-repo-check + --dangerously-bypass-approvals-and-sandbox), because the
// worker process / workspace cwd is the real isolation boundary (same model as the
// claude executor — codex has no internal sandbox we rely on).
//
// It deliberately DIFFERS from the resident session in two ways, matching the
// ClaudeRunnerBuilder's one-shot-executor contract:
//   - NO --json: the resident session maps codex's JSONL event stream to
//     StreamEvents, but the executor's CommandRunner captures the command's combined
//     output verbatim into output.json and relays it — so we want codex's plain,
//     human-readable final transcript, not a wall of JSONL events (the codex analogue
//     of the claude executor's bare `-p`).
//   - the executor framing is PREPENDED to the prompt: codex exec has no
//     --append-system-prompt flag (cf. the claude builder), so the same "no center /
//     mcp access, report your result as the final message" framing rides in the prompt.
//
// codex exec carries no center credentials or mcp config (none is passed; codex has
// no mcp by default), so the executor isolation holds.
//
// TODO(T622 / issue-47fe2a78): codex usage 上报未打通. The claude executor emits
// stream-json whose `result` lines carry token usage (executor.ParseRunnerStream),
// but `codex exec` here runs in plain-text mode (no --json) so ParseRunnerStream
// finds no parseable usage → a codex executor's usage_event is empty (its result
// text still relays fine via the raw-output fallback). Wiring codex usage needs a
// codex-specific token parser over its JSONL event stream (a different shape from
// claude's) — deferred; this fix gets claude-code's source-bound usage landing first.
type CodexRunnerBuilder struct {
	binary string
}

// NewCodexRunnerBuilder builds a CodexRunnerBuilder. An empty binary defaults to
// "codex" on PATH (matching the codex session default).
func NewCodexRunnerBuilder(binary string) *CodexRunnerBuilder {
	if strings.TrimSpace(binary) == "" {
		binary = "codex"
	}
	return &CodexRunnerBuilder{binary: binary}
}

// Build assembles the executor's codex argv:
//
//	codex exec --skip-git-repo-check --dangerously-bypass-approvals-and-sandbox
//	           -m <model> <executor-framed prompt>
//
// NO --json (plain combined output captured into output.json), NO mcp (codex carries
// none by default), NO resume thread (each executor is an ephemeral one-shot).
func (b *CodexRunnerBuilder) Build(model, prompt string) ([]string, error) {
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("orchestrator: runner model required")
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, errors.New("orchestrator: runner prompt required")
	}
	return []string{
		b.binary, "exec",
		// Auth + autonomous permissions — same rationale + flags as the resident
		// cli=codex session (codex_session.go buildCodexArgv): the worker/workspace is
		// the isolation boundary, not codex's approval prompt or internal sandbox.
		"--skip-git-repo-check",
		"--dangerously-bypass-approvals-and-sandbox",
		"-m", model,
		// codex exec has no --append-system-prompt: the executor framing rides in the prompt.
		executorSystemPrompt + "\n\n" + prompt,
	}, nil
}

// ClaudeRunnerBuilder builds the production executor runner: a ONE-SHOT, no-mcp
// claude invocation under the F3-selected model (design §4/§5). It mirrors the
// supervisor's auth/permission flags (claudestream.rewriteForStreamingInput) so
// the headless executor authenticates and runs non-interactively, but it
// deliberately omits BOTH the streaming-input rewrite AND any --mcp-config: the
// executor is a one-shot `claude -p` that prints its final result (captured into
// output.json by the executor's CommandRunner) and carries NO center credentials.
type ClaudeRunnerBuilder struct {
	binary string
}

// NewClaudeRunnerBuilder builds a ClaudeRunnerBuilder. An empty binary defaults to
// "claude" on PATH (matching the adapter default).
func NewClaudeRunnerBuilder(binary string) *ClaudeRunnerBuilder {
	if strings.TrimSpace(binary) == "" {
		binary = "claude"
	}
	return &ClaudeRunnerBuilder{binary: binary}
}

// Build assembles the executor's claude argv:
//
//	claude -p <prompt> --model <model> --append-system-prompt <executor framing>
//	       --output-format stream-json --verbose
//	       --setting-sources user,project
//	       --allow-dangerously-skip-permissions --dangerously-skip-permissions
//	       --permission-mode bypassPermissions
//
// NO --mcp-config (executor isolation: no center tools/credentials), NO --session-id
// (each executor is an ephemeral one-shot).
//
// v2.20.1 (T622, issue-47fe2a78 reopen): the executor runs in stream-json mode so
// each turn's `result` line carries the token `usage` the orchestrator relays to
// report_usage. The earlier plain `-p` text mode emitted NO usage, so
// ParseRunnerUsage returned 0 in production and usage_event was永远 empty. claude
// requires --verbose to emit stream-json under -p. The CommandRunner extracts the
// final TEXT result from the stream (executor.ParseRunnerStream) for output.json /
// chat relay — the raw JSON is never written back as the task result.
func (b *ClaudeRunnerBuilder) Build(model, prompt string) ([]string, error) {
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("orchestrator: runner model required")
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, errors.New("orchestrator: runner prompt required")
	}
	args := []string{
		b.binary,
		"-p", prompt,
		"--model", model,
		"--append-system-prompt", executorSystemPrompt,
		// stream-json so each result line carries per-turn usage (T622); --verbose is
		// REQUIRED by claude to emit stream-json under -p.
		"--output-format", "stream-json",
		"--verbose",
		// Auth + non-interactive permissions — same rationale as the supervisor's
		// streaming argv (claudestream): "user" setting-source loads keychain /login
		// auth (+ "project" the workspace's own .claude); bypassPermissions is the
		// deterministic headless tool-permission group. The executor's OS sandbox +
		// workspace cwd are the real boundary, not claude's interactive prompt.
		"--setting-sources", "user,project",
		"--allow-dangerously-skip-permissions",
		"--dangerously-skip-permissions",
		"--permission-mode", "bypassPermissions",
	}
	return args, nil
}
