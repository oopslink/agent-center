// Package workerdaemon: codex_session.go is the Codex CLI execution session —
// the codex counterpart to the claude-owning SupervisorSession.
//
// ARCHITECTURAL DIFFERENCE (why this is not a SupervisorSession):
// claude is driven as ONE long-lived process with a held-open stdin into which
// newline-delimited stream-json user messages are injected for the agent's whole
// life. Codex has no such mode: `codex exec --json` is ONE-SHOT — it takes the
// prompt as an argument, streams JSONL events on stdout, runs the turn to
// completion, and EXITS. Continuation is done by capturing the thread_id from the
// first turn's `thread.started` event and re-invoking `codex exec resume
// <thread_id> --json <next prompt>`.
//
// So a CodexSession is a long-lived LOGICAL handle whose underlying codex process
// comes and goes once per turn: each Inject runs a fresh `codex exec`/`exec
// resume`, maps its JSONL to claudestream.StreamEvent via OnEvent, and the
// process exiting at end-of-turn does NOT end the session (it stays ready for the
// next Inject). OnExit fires only on Stop / Detach / a fatal spawn failure.
//
// It satisfies the same Session control surface (Inject / Stop / Detach) the
// AgentController needs, and emits the SAME claudestream.StreamEvent type the
// controller's onEvent consumes — so it is drop-in for a future cli-aware
// sessionStarter (see IMPLEMENTATION_PLAN.md Stage 3). It is NOT yet wired into
// the controller (agent.cli is not plumbed to the worker today), so this file is
// exercised by unit tests + the env-gated real-codex integration test, not by a
// production path.
package agentruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/oopslink/agent-center/internal/claudestream"
)

// ErrCodexInvalidEvent is returned by mapCodexLine for a malformed JSONL line.
var ErrCodexInvalidEvent = errors.New("codex_session: invalid event JSON")

// codexProc is one running `codex exec` process. The real impl wraps os/exec;
// tests inject a fake that emits canned stdout lines. Codex takes its prompt as
// an argv, so — unlike the claude session — there is no stdin to write.
type codexProc interface {
	Stdout() io.Reader
	Wait() error
}

// codexLaunchSpec carries everything needed to assemble one `codex exec` (fresh)
// or `codex exec resume <threadID>` (continuation) invocation.
type codexLaunchSpec struct {
	TasksDir string
	Binary   string // "" → "codex" on PATH
	Model    string // "" → codex default
	ThreadID string // "" → fresh `exec`; else `exec resume <ThreadID>`
	Prompt   string
	Env      map[string]string
}

// codexLauncher builds + starts a codexProc. execCodexLauncher is the real impl;
// tests inject a fake.
type codexLauncher interface {
	Launch(ctx context.Context, spec codexLaunchSpec) (codexProc, error)
}

// buildCodexArgv assembles the argv for a fresh or resumed codex exec turn.
//
//   - fresh:  codex exec --json --skip-git-repo-check
//     --dangerously-bypass-approvals-and-sandbox [-m model] <prompt>
//   - resume: codex exec resume <threadID> --json --skip-git-repo-check
//     --dangerously-bypass-approvals-and-sandbox [-m model] <prompt>
//
// The working directory is set via cmd.Dir (codex `exec resume` does not accept
// -C), and --dangerously-bypass-approvals-and-sandbox is required for autonomous
// operation because the worker process is the isolation boundary (same model as
// claude, which has no internal sandbox).
func buildCodexArgv(spec codexLaunchSpec) []string {
	bin := spec.Binary
	if bin == "" {
		bin = "codex"
	}
	argv := []string{bin, "exec"}
	if spec.ThreadID != "" {
		argv = append(argv, "resume", spec.ThreadID)
	}
	argv = append(argv,
		"--json",
		"--skip-git-repo-check",
		"--dangerously-bypass-approvals-and-sandbox",
	)
	if spec.Model != "" {
		argv = append(argv, "-m", spec.Model)
	}
	argv = append(argv, spec.Prompt)
	return argv
}

// execCodexLauncher is the production codexLauncher.
type execCodexLauncher struct{}

func (execCodexLauncher) Launch(ctx context.Context, spec codexLaunchSpec) (codexProc, error) {
	argv := buildCodexArgv(spec)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if spec.TasksDir != "" {
		cmd.Dir = spec.TasksDir
	}
	env := os.Environ()
	for k, v := range spec.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	// Codex reads its prompt from argv; an OPEN stdin pipe makes it block on
	// "Reading additional input from stdin...". Leaving Stdin nil connects the
	// child to /dev/null → immediate EOF, so it proceeds with the argv prompt.
	cmd.Stdin = nil
	cmd.Stderr = os.Stderr
	// Own process group so a ctx-cancel kill reaches the whole tree.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Graceful cancel: SIGTERM the group, then SIGKILL after the wait delay.
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 5 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex_session: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("codex_session: start %s: %w", argv[0], err)
	}
	return &execCodexProc{cmd: cmd, stdout: stdout}, nil
}

type execCodexProc struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
}

func (p *execCodexProc) Stdout() io.Reader { return p.stdout }
func (p *execCodexProc) Wait() error       { return p.cmd.Wait() }

// ---------------------------------------------------------------------------
// codex JSONL → claudestream.StreamEvent mapping.
// ---------------------------------------------------------------------------

// mapCodexLine maps ONE `codex exec --json` JSONL line to zero or more
// claudestream.StreamEvent (the type the AgentController consumes) and extracts
// the thread_id when the line is a thread.started event.
//
// codex schema (validated on codex-cli 0.137.0):
//   - thread.started{thread_id}                 → threadID set (no StreamEvent)
//   - turn.started                              → (no StreamEvent)
//   - item.started{command_execution}           → tool_use   (ToolName=shell)
//   - item.completed{command_execution}         → tool_result
//   - item.completed{agent_message,text}        → assistant_text  (the reply)
//   - item.completed{reasoning,text}            → thinking
//   - turn.completed{usage}                     → result (success, tokens)
//   - turn.failed                               → result (IsError=true) [TERMINAL]
//   - error                                     → error/transient (IsError=true) [NON-terminal,
//     issue-c7a1fe3e: a reconnect blip must not false-kill the turn]
//   - anything else                             → unknown (forward-compatible)
func mapCodexLine(line []byte) (events []claudestream.StreamEvent, threadID string, err error) {
	if len(line) == 0 {
		return nil, "", fmt.Errorf("%w: empty line", ErrCodexInvalidEvent)
	}
	var top struct {
		Type     string          `json:"type"`
		ThreadID string          `json:"thread_id"`
		Item     json.RawMessage `json:"item"`
		Usage    *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(line, &top); err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrCodexInvalidEvent, err)
	}

	switch top.Type {
	case "thread.started":
		return nil, top.ThreadID, nil
	case "turn.started":
		return nil, "", nil
	case "turn.completed":
		ev := claudestream.StreamEvent{
			Type:       "result",
			Subtype:    "success",
			StopReason: "end_turn",
			Raw:        cloneRaw(line),
		}
		if top.Usage != nil {
			ev.TokensIn = top.Usage.InputTokens
			ev.TokensOut = top.Usage.OutputTokens
		}
		return []claudestream.StreamEvent{ev}, "", nil
	case "turn.failed":
		// TERMINAL: the turn genuinely failed → a result/error event ends the turn (the
		// session's sawResult gate + the events.go turn-end handler both key on
		// Type=="result").
		msg := top.Message
		if top.Error != nil && top.Error.Message != "" {
			msg = top.Error.Message
		}
		return []claudestream.StreamEvent{{
			Type:    "result",
			Subtype: "error",
			IsError: true,
			Result:  msg,
			Raw:     cloneRaw(line),
		}}, "", nil
	case "error":
		// issue-c7a1fe3e: a bare `error` event is codex reporting a TRANSIENT / recoverable
		// condition mid-stream (a reconnect blip under network jitter), NOT a terminal turn
		// failure — codex keeps going and still emits turn.completed. The old code mapped it
		// to a terminal `result`(IsError) event, so a transient blip FALSE-KILLED the session
		// (assumed dead) even though the turn later completed. Emit it as a NON-"result"
		// diagnostic: visible/logged (never swallowed), but the session's sawResult gate and
		// the events.go turn-end handler both ignore non-"result" types, so consumption
		// continues. Genuine terminality still comes from turn.failed (above) OR the stream
		// closing without a turn.completed (the synthesize-failed path below) — a truly fatal
		// error therefore STILL fails loud; only a transient one no longer false-kills.
		msg := top.Message
		if top.Error != nil && top.Error.Message != "" {
			msg = top.Error.Message
		}
		return []claudestream.StreamEvent{{
			Type:    "error", // NON-terminal (not "result"): sawResult / turn-end handler ignore it
			Subtype: "transient",
			IsError: true,
			Result:  msg,
			Raw:     cloneRaw(line),
		}}, "", nil
	case "item.started", "item.completed":
		ev, ok := mapCodexItem(top.Type, top.Item, line)
		if !ok {
			return nil, "", nil
		}
		return []claudestream.StreamEvent{ev}, "", nil
	default:
		return []claudestream.StreamEvent{{Type: "unknown", Raw: cloneRaw(line)}}, "", nil
	}
}

// mapCodexItem maps an item.started/item.completed event by inner item type.
// ok=false means the item carries no StreamEvent (e.g. an item.started for a
// text item, which only carries text once completed).
func mapCodexItem(topType string, itemRaw json.RawMessage, line []byte) (claudestream.StreamEvent, bool) {
	if len(itemRaw) == 0 {
		return claudestream.StreamEvent{}, false
	}
	var item struct {
		ID               string `json:"id"`
		Type             string `json:"type"`
		Text             string `json:"text"`
		Command          string `json:"command"`
		AggregatedOutput string `json:"aggregated_output"`
	}
	if err := json.Unmarshal(itemRaw, &item); err != nil {
		return claudestream.StreamEvent{}, false
	}
	switch item.Type {
	case "command_execution":
		if topType == "item.completed" {
			out, _ := json.Marshal(item.AggregatedOutput)
			return claudestream.StreamEvent{
				Type:       "tool_result",
				ToolUseID:  item.ID,
				ToolResult: out,
				Raw:        cloneRaw(line),
			}, true
		}
		in, _ := json.Marshal(struct {
			Command string `json:"command"`
		}{Command: item.Command})
		return claudestream.StreamEvent{
			Type:      "tool_use",
			ToolName:  "shell",
			ToolUseID: item.ID,
			ToolInput: in,
			Raw:       cloneRaw(line),
		}, true
	case "agent_message":
		if topType != "item.completed" {
			return claudestream.StreamEvent{}, false
		}
		return claudestream.StreamEvent{
			Type: "assistant_text",
			Text: item.Text,
			Raw:  cloneRaw(line),
		}, true
	case "reasoning":
		if topType != "item.completed" {
			return claudestream.StreamEvent{}, false
		}
		return claudestream.StreamEvent{
			Type: "thinking",
			Text: item.Text,
			Raw:  cloneRaw(line),
		}, true
	default:
		return claudestream.StreamEvent{Type: "unknown", Raw: cloneRaw(line)}, true
	}
}

func cloneRaw(line []byte) json.RawMessage {
	return append(json.RawMessage(nil), line...)
}

// ---------------------------------------------------------------------------
// CodexSession.
// ---------------------------------------------------------------------------

// CodexSessionConfig configures StartCodexSession.
type CodexSessionConfig struct {
	// AgentID identifies the agent (for logging).
	AgentID string
	// TasksDir is the codex working root (cmd.Dir for every turn).
	TasksDir string
	// Binary overrides the codex binary path ("" → "codex" on PATH).
	Binary string
	// Model is an optional `codex -m` override.
	Model string
	// Env is merged into each turn's child environment.
	Env map[string]string
	// Launcher starts each turn's process. Defaults to execCodexLauncher when nil.
	Launcher codexLauncher
	// OnEvent is invoked for every mapped StreamEvent, in order, from the
	// run-loop goroutine. Must not block indefinitely.
	OnEvent func(ev claudestream.StreamEvent)
	// OnExit is invoked EXACTLY ONCE when the session ends (Stop / Detach / fatal
	// spawn failure). err is nil for a clean Stop/Detach, non-nil for a fatal
	// spawn failure.
	OnExit func(err error)
	// Logger receives one-line ops messages. nil → discard.
	Logger func(msg string)
	// InjectBuffer bounds the pending-inject queue (0 → 64).
	InjectBuffer int
}

// CodexSession is a long-lived logical handle that runs one `codex exec` turn per
// injected message, threading the captured thread_id across turns. Safe for
// concurrent Inject / Stop / Detach.
type CodexSession struct {
	cfg      CodexSessionConfig
	launcher codexLauncher
	cancel   context.CancelFunc
	injectCh chan string

	mu       sync.Mutex
	closed   bool
	threadID string

	stopOnce sync.Once
	exitOnce sync.Once
	done     chan struct{}
}

// compile-time: CodexSession satisfies the controller's Session contract,
// so a future cli-aware sessionStarter can return it interchangeably with the
// claude-owning SupervisorSession.
var _ Session = (*CodexSession)(nil)

// StartCodexSession starts the run-loop and returns an immediately-usable
// session. It does not spawn codex until the first Inject.
func StartCodexSession(ctx context.Context, cfg CodexSessionConfig) (*CodexSession, error) {
	if cfg.AgentID == "" {
		return nil, errors.New("codex_session: agent_id required")
	}
	if cfg.Launcher == nil {
		cfg.Launcher = execCodexLauncher{}
	}
	if cfg.Logger == nil {
		cfg.Logger = func(string) {}
	}
	if cfg.OnEvent == nil {
		cfg.OnEvent = func(claudestream.StreamEvent) {}
	}
	if cfg.InjectBuffer <= 0 {
		cfg.InjectBuffer = 64
	}
	loopCtx, cancel := context.WithCancel(context.Background())
	s := &CodexSession{
		cfg:      cfg,
		launcher: cfg.Launcher,
		cancel:   cancel,
		injectCh: make(chan string, cfg.InjectBuffer),
		done:     make(chan struct{}),
	}
	go s.runLoop(loopCtx)
	return s, nil
}

// runLoop processes injected messages one turn at a time until the loop ctx is
// cancelled (Stop/Detach) or a turn hits a fatal spawn failure.
func (s *CodexSession) runLoop(ctx context.Context) {
	var fatal error
	for {
		select {
		case <-ctx.Done():
			s.fireExit(nil)
			return
		case msg := <-s.injectCh:
			if err := s.runTurn(ctx, msg); err != nil {
				if ctx.Err() != nil {
					// Cancelled mid-turn (Stop/Detach) — clean end.
					s.fireExit(nil)
					return
				}
				fatal = err
				s.fireExit(fatal)
				return
			}
		}
	}
}

// runTurn runs ONE codex turn for msg: spawn fresh/resume, stream+map stdout to
// OnEvent, capture thread_id. A non-zero process exit is NOT fatal to the session
// (a synthetic error result is emitted so the controller never sits silently);
// only a spawn failure returns a non-nil (fatal) error.
func (s *CodexSession) runTurn(ctx context.Context, msg string) error {
	s.mu.Lock()
	thread := s.threadID
	s.mu.Unlock()

	proc, err := s.launcher.Launch(ctx, codexLaunchSpec{
		TasksDir: s.cfg.TasksDir,
		Binary:   s.cfg.Binary,
		Model:    s.cfg.Model,
		ThreadID: thread,
		Prompt:   msg,
		Env:      s.cfg.Env,
	})
	if err != nil {
		return fmt.Errorf("codex_session: launch turn: %w", err)
	}

	scanner := bufio.NewScanner(proc.Stdout())
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	sawResult := false
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		raw := make([]byte, len(line))
		copy(raw, line)
		events, tid, perr := mapCodexLine(raw)
		if perr != nil {
			s.cfg.Logger(fmt.Sprintf("[worker] codex_session: parse line: %v", perr))
			continue
		}
		if tid != "" {
			s.mu.Lock()
			s.threadID = tid
			s.mu.Unlock()
		}
		for _, ev := range events {
			switch {
			case ev.Type == "result":
				sawResult = true
			case ev.Type == "error" && ev.Subtype == "transient":
				// issue-c7a1fe3e: surface the transient codex error LOUD (never silent),
				// but do NOT terminate — the turn continues to its real turn.completed /
				// turn.failed. Fail-loud, not false-death.
				s.cfg.Logger(fmt.Sprintf("[worker] codex_session: transient codex error (non-terminal, continuing): %s", ev.Result))
			}
			s.cfg.OnEvent(ev)
		}
	}
	waitErr := proc.Wait()

	if !sawResult {
		// No terminal result line (crash / killed). Synthesize a failed result so
		// the controller fails the in-flight work item (no silent failure). When
		// the turn was cancelled (Stop/Detach), skip — runLoop ends the session.
		if ctx.Err() == nil {
			s.cfg.OnEvent(claudestream.StreamEvent{
				Type:    "result",
				Subtype: "error",
				IsError: true,
				Result:  fmt.Sprintf("codex exited without completing the turn: %v", waitErr),
			})
		}
	}
	return nil
}

// fireExit invokes OnExit exactly once, marks the session closed, then closes
// done. Concurrency-safe; idempotent.
func (s *CodexSession) fireExit(err error) {
	s.exitOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		if s.cfg.OnExit != nil {
			s.cfg.OnExit(err)
		}
		close(s.done)
	})
}

// Inject queues userMessage for the next turn. Concurrency-safe. Returns
// ErrSessionClosed once Stop/Detach has begun.
func (s *CodexSession) Inject(ctx context.Context, userMessage string) error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return ErrSessionClosed
	}
	select {
	case s.injectCh <- userMessage:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return ErrSessionClosed
	}
}

// Stop terminates the session: it blocks new injects, cancels the run-loop (which
// kills any in-flight codex turn via the launch ctx), and waits for OnExit to
// fire exactly once. Idempotent.
func (s *CodexSession) Stop(ctx context.Context) error {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		s.cancel()
	})
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Detach ends the session. Codex has no cross-restart survival model (there is no
// persistent process between turns), so Detach is a clean stop that fires
// OnExit(nil); a future daemon re-dispatches a fresh turn (resuming via the
// persisted thread_id is a future enhancement — see IMPLEMENTATION_PLAN.md).
func (s *CodexSession) Detach() {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		s.cancel()
	})
}

// ThreadID returns the captured codex thread_id (empty before the first turn's
// thread.started). Used by resume + by tests.
func (s *CodexSession) ThreadID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.threadID
}

// Done returns a channel closed after OnExit has fired.
func (s *CodexSession) Done() <-chan struct{} { return s.done }
