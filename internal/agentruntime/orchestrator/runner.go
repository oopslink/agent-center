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
	// and goal prompt. model and prompt are both required. sessionID, when non-empty,
	// is the orchestrator-allocated session identifier the runner should bind so the
	// executor's LLM conversation can later be `--resume`d for crash recovery (design
	// §4.3). A builder whose CLI cannot pre-assign a session id (e.g. codex mints its
	// own thread) ignores it — recovery then degrades that executor to a plain rerun.
	Build(model, prompt, sessionID string) ([]string, error)
}

// resumeSessionArgv rewrites a persisted fresh-launch argv into its --resume form:
// it swaps the `--session-id <sid>` flag pair for `--resume <sid>`, keeping every
// other flag (model, prompt, permissions) byte-for-byte. This is how the recovery
// ladder's tier-1 (full-context resume, design §4.3) reconstructs the resume
// invocation from the durable Record.RunnerCmd WITHOUT re-deriving model/prompt.
//
// It returns (resumeArgv, true) when a `--session-id <sid>` pair is present, or
// (argv, false) when it is not — the signal that this argv cannot be session-
// resumed (a session-less CLI like codex, or a pre-session record), so the caller
// falls back to a plain rerun (tier-2).
func resumeSessionArgv(argv []string) ([]string, bool) {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "--session-id" {
			out := make([]string, 0, len(argv))
			out = append(out, argv[:i]...)
			out = append(out, "--resume", argv[i+1])
			out = append(out, argv[i+2:]...)
			return out, true
		}
	}
	return argv, false
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
//	fresh:  codex exec [resume <threadID>] --json --skip-git-repo-check
//	        --dangerously-bypass-approvals-and-sandbox -m <model> <executor-framed prompt>
//
// --json (T969): unlike the pre-T969 plain-output executor, the codex executor now
// streams its JSONL event stream into output.json so the orchestrator can (a) extract
// the final result via the codex parser (ParseCodexRunnerStream → mapCodexLine) and
// (b) capture the thread_id (thread.started) → persist into Record.SessionID for tier-1
// resume. NO mcp (codex carries none by default).
//
// sessionID is the codex thread_id captured from a PRIOR run: when non-empty, Build
// emits `resume <threadID>` (tier-1 recovery, full-context continuation, mirroring
// codex_session.go buildCodexArgv); when empty (a fresh run OR thread_id was never
// captured) it is a plain `exec` and recovery falls to the ladder's tier-2 rerun.
// codex resume relies on the executor process cwd for the workspace (no -C), same as
// the resident session.
func (b *CodexRunnerBuilder) Build(model, prompt, sessionID string) ([]string, error) {
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("orchestrator: runner model required")
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, errors.New("orchestrator: runner prompt required")
	}
	argv := []string{b.binary, "exec"}
	if tid := strings.TrimSpace(sessionID); tid != "" {
		// tier-1 resume: continue the captured codex thread with full context.
		argv = append(argv, "resume", tid)
	}
	argv = append(argv,
		"--json",
		// Auth + autonomous permissions — same rationale + flags as the resident
		// cli=codex session (codex_session.go buildCodexArgv): the worker/workspace is
		// the isolation boundary, not codex's approval prompt or internal sandbox.
		"--skip-git-repo-check",
		"--dangerously-bypass-approvals-and-sandbox",
		"-m", model,
		// codex exec has no --append-system-prompt: the executor framing rides in the prompt.
		executorSystemPrompt+"\n\n"+prompt,
	)
	return argv, nil
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
// NO --mcp-config (executor isolation: no center tools/credentials).
//
// v2.34.0 (T845, §4.3): when sessionID is non-empty the argv binds `--session-id
// <sessionID>` so the executor's claude conversation is durably identified and can
// later be `--resume`d for full-context crash recovery (the orchestrator persists
// the same id into Record.SessionID). An empty sessionID keeps the old ephemeral
// one-shot argv byte-for-byte (no --session-id), so a caller that opts out is
// unchanged.
//
// v2.20.1 (T622, issue-47fe2a78 reopen): the executor runs in stream-json mode so
// each turn's `result` line carries the token `usage` the orchestrator relays to
// report_usage. The earlier plain `-p` text mode emitted NO usage, so
// ParseRunnerUsage returned 0 in production and usage_event was永远 empty. claude
// requires --verbose to emit stream-json under -p. The CommandRunner extracts the
// final TEXT result from the stream (executor.ParseRunnerStream) for output.json /
// chat relay — the raw JSON is never written back as the task result.
func (b *ClaudeRunnerBuilder) Build(model, prompt, sessionID string) ([]string, error) {
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
	// §4.3: bind the orchestrator-allocated session id so this executor's claude
	// conversation is durably resumable. Appended last so resumeSessionArgv can
	// locate the `--session-id <sid>` pair when reconstructing the resume argv.
	if sid := strings.TrimSpace(sessionID); sid != "" {
		args = append(args, "--session-id", sid)
	}
	return args, nil
}
