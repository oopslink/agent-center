// Package workerdaemon: ClaudeSession is the long-lived per-agent claude
// process primitive for the v2.7 agent-execution path (slice D2-c-ii-A).
//
// Unlike AgentRunner (the one-shot path in agent_runner.go — spawn, write the
// prompt to stdin ONCE, stream stdout, wait for exit), ClaudeSession keeps a
// SINGLE claude process alive across many injected user messages: stdin stays
// OPEN and is fed newline-delimited stream-json user messages via Inject, and
// stdout is streamed line-by-line through adapter.ParseEvent for the lifetime
// of the process. The session-id is the AGENT id (persistent), NOT a
// per-execution id.
//
// This layer is intentionally pure: it knows nothing about control commands,
// AgentWorkItem, lifecycle feedback, or the admin API. It only exposes two
// callbacks (OnEvent / OnExit) plus Inject / Stop. The AgentController that
// wires those callbacks to admin feedback is the NEXT slice (D2-c-ii-B).
//
// Testability: the process is reached only through the procLauncher /
// sessionProc seam, so unit tests inject a fake that records stdin + emits
// canned stdout lines — no real claude binary required.
package workerdaemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/oopslink/agent-center/internal/agentadapter"
	"github.com/oopslink/agent-center/internal/agentadapter/claudecode"
)

// ErrSessionClosed is returned by Inject when the session has stopped (or its
// process has exited), so callers (c-ii-B) get a clear, typed signal rather
// than a write-to-closed-pipe panic.
var ErrSessionClosed = errors.New("claude_session: session closed")

// sessionProc is one running claude process. The real impl (execSessionProc)
// wraps os/exec; tests inject a fake that records stdin + emits canned stdout
// lines. The session owns the stdout scanner goroutine, so the seam exposes
// Stdout as a plain io.Reader.
type sessionProc interface {
	Stdin() io.Writer           // newline-delimited stream-json user messages
	Stdout() io.Reader          // newline-delimited stream-json events
	Wait() error                // blocks until the process exits
	Signal(sig os.Signal) error // graceful stop (SIGTERM)
	Kill() error                // hard stop (SIGKILL)
}

// procLauncher builds + starts a sessionProc. execLauncher is the real impl;
// tests inject a fake.
type procLauncher interface {
	Launch(ctx context.Context, spec ClaudeLaunchSpec) (sessionProc, error)
}

// ClaudeLaunchSpec carries everything needed to assemble the long-lived
// `claude` invocation. AgentID becomes the persistent --session-id; MCPConfig
// is the absolute path passed via --mcp-config; WorkspaceDir is the process
// cwd; Env is merged into the child env.
type ClaudeLaunchSpec struct {
	AgentID       string
	WorkspaceDir  string
	MCPConfigPath string
	Env           map[string]string
	// Binary overrides the claude binary path (empty = resolved from $PATH as
	// "claude" by the adapter).
	Binary string
}

// ---------------------------------------------------------------------------
// Real launcher (kept behind the seam; NOT exercised by unit tests).
// ---------------------------------------------------------------------------

// execLauncher is the production procLauncher. It reuses the claudecode
// adapter's BuildCommand to assemble argv + env, then drives a long-lived
// process via exec.CommandContext with stdin/stdout pipes.
//
// NOTE: adapter.BuildCommand (as of D2-c-ii-A) appends `-p <prompt>` and
// `--output-format stream-json --session-id <id>` but does NOT add
// `--input-format stream-json` / `--verbose`, and it REQUIRES a non-empty
// Prompt. For a long-lived session we want: KEEP `--print`/`-p` as a FLAG (it
// is required for stream-json input/output) but DROP the positional prompt
// (messages arrive on stdin), and ADD `--input-format stream-json` +
// `--verbose`. We therefore call BuildCommand with a sentinel prompt to satisfy
// its validation + reuse its env/skill/session-id assembly, then rewrite the
// args via rewriteForStreamingInput. This keeps the adapter as the single
// source of truth for the env + session-id wiring while adapting the argv for
// streaming input.
//
// VALIDATED (D2-c-ii-D, real claude 2.1.156): the long-lived flag set is
// `--print --input-format stream-json --output-format stream-json --verbose
// --session-id <agentID> --mcp-config <path>`. `--input-format` /
// `--output-format stream-json` only work WITH `--print`, and full event output
// needs `--verbose`. This is no longer a guess — it is a captured round-trip.
type execLauncher struct{}

func (execLauncher) Launch(ctx context.Context, spec ClaudeLaunchSpec) (sessionProc, error) {
	adapter := claudecode.New(spec.Binary)
	req := agentadapter.SpawnRequest{
		ExecutionID: spec.AgentID, // persistent agent id, NOT per-execution
		Prompt:      longLivedSentinelPrompt,
		WorkingDir:  spec.WorkspaceDir,
		Env:         spec.Env,
	}
	cmdSpec, err := adapter.BuildCommand(req)
	if err != nil {
		return nil, fmt.Errorf("claude_session: build command: %w", err)
	}
	args := rewriteForStreamingInput(cmdSpec.Args)
	if spec.MCPConfigPath != "" {
		mcp, err := adapter.BuildMCPConfigArg(spec.MCPConfigPath)
		if err != nil {
			return nil, fmt.Errorf("claude_session: mcp-config arg: %w", err)
		}
		args = append(args, mcp.Args...)
	}

	cmd := exec.CommandContext(ctx, cmdSpec.Binary, args...)
	if spec.WorkspaceDir != "" {
		cmd.Dir = spec.WorkspaceDir
	}
	cmd.Env = cmdSpec.Env
	// Own process group so Stop can signal the whole tree (claude forks MCP
	// helpers) — matches AgentRunner's Setpgid behaviour.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("claude_session: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("claude_session: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("claude_session: start %s: %w", cmdSpec.Binary, err)
	}
	return &execSessionProc{cmd: cmd, stdin: stdin, stdout: stdout}, nil
}

// longLivedSentinelPrompt satisfies BuildCommand's non-empty-prompt validation;
// rewriteForStreamingInput strips the resulting `-p <sentinel>` pair.
const longLivedSentinelPrompt = "__ac_streaming_input__"

// rewriteForStreamingInput converts the one-shot argv produced by
// adapter.BuildCommand (`... -p <prompt>`) into the long-lived streaming-input
// argv VALIDATED against real claude 2.1.156:
//
//	--print --input-format stream-json --output-format stream-json --verbose
//	--session-id <agentID>   (+ --mcp-config <path>, appended by the caller)
//
// It KEEPS `--print`/`-p` as a FLAG (rewriting `-p` → the canonical `--print`)
// but DROPS the positional prompt value (the sentinel), then ENSURES
// `--input-format stream-json`, `--output-format stream-json`, and `--verbose`
// are all present exactly once (adding any that are missing; never
// duplicating). `--session-id <id>` and other flags pass through unchanged.
func rewriteForStreamingInput(in []string) []string {
	out := make([]string, 0, len(in)+4)
	hasPrint := false
	hasInputFormat := false
	hasOutputFormat := false
	hasVerbose := false

	for i := 0; i < len(in); i++ {
		switch in[i] {
		case "-p", "--print":
			// Keep the flag (canonicalised to --print) but DROP the positional
			// prompt value that follows `-p`.
			if in[i] == "-p" && i+1 < len(in) {
				i++ // skip the sentinel prompt value
			}
			if !hasPrint {
				out = append(out, "--print")
				hasPrint = true
			}
		case "--input-format":
			out = append(out, in[i])
			if i+1 < len(in) {
				i++
				out = append(out, in[i])
			}
			hasInputFormat = true
		case "--output-format":
			out = append(out, in[i])
			if i+1 < len(in) {
				i++
				out = append(out, in[i])
			}
			hasOutputFormat = true
		case "--verbose":
			if !hasVerbose {
				out = append(out, in[i])
				hasVerbose = true
			}
		default:
			out = append(out, in[i])
		}
	}

	if !hasPrint {
		out = append(out, "--print")
	}
	if !hasInputFormat {
		out = append(out, "--input-format", "stream-json")
	}
	if !hasOutputFormat {
		out = append(out, "--output-format", "stream-json")
	}
	if !hasVerbose {
		out = append(out, "--verbose")
	}
	return out
}

// execSessionProc wraps a real *exec.Cmd as a sessionProc.
type execSessionProc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

func (p *execSessionProc) Stdin() io.Writer  { return p.stdin }
func (p *execSessionProc) Stdout() io.Reader { return p.stdout }
func (p *execSessionProc) Wait() error       { return p.cmd.Wait() }

func (p *execSessionProc) Signal(sig os.Signal) error {
	if p.cmd.Process == nil {
		return errors.New("claude_session: no process")
	}
	// Negative PID signals the whole group (matches Setpgid above).
	if s, ok := sig.(syscall.Signal); ok {
		return syscall.Kill(-p.cmd.Process.Pid, s)
	}
	return p.cmd.Process.Signal(sig)
}

func (p *execSessionProc) Kill() error {
	if p.cmd.Process == nil {
		return errors.New("claude_session: no process")
	}
	return syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
}

// ---------------------------------------------------------------------------
// stream-json INPUT encoding.
// ---------------------------------------------------------------------------

// encodeUserMessage encodes a plain user message as one newline-terminated
// stream-json line for claude's `--input-format stream-json`.
//
// FLAG (D2-g): this schema is a documented BEST GUESS — no stream-json INPUT
// encoding exists in-repo (the adapter only encodes OUTPUT). It mirrors the
// Anthropic Messages content-block shape:
//
//	{"type":"user","message":{"role":"user","content":[{"type":"text","text":"<msg>"}]}}\n
//
// It is deliberately isolated in this one function so D2-g can correct it
// against the real claude binary without touching the session lifecycle.
func encodeUserMessage(msg string) ([]byte, error) {
	type textBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type innerMessage struct {
		Role    string      `json:"role"`
		Content []textBlock `json:"content"`
	}
	type userEnvelope struct {
		Type    string       `json:"type"`
		Message innerMessage `json:"message"`
	}
	env := userEnvelope{
		Type: "user",
		Message: innerMessage{
			Role:    "user",
			Content: []textBlock{{Type: "text", Text: msg}},
		},
	}
	b, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("claude_session: encode user message: %w", err)
	}
	return append(b, '\n'), nil
}

// ---------------------------------------------------------------------------
// ClaudeSession.
// ---------------------------------------------------------------------------

// ClaudeSessionConfig configures StartClaudeSession.
type ClaudeSessionConfig struct {
	// AgentID is the persistent claude --session-id.
	AgentID string
	// HomeDir is the per-agent home directory; the --mcp-config file is
	// written under it.
	HomeDir string
	// WorkspaceDir is the process cwd (empty = inherit).
	WorkspaceDir string
	// Launcher starts the process. Defaults to execLauncher when nil.
	Launcher procLauncher
	// MCPConfigBytes is the pre-generated --mcp-config document content. When
	// non-empty it is written under HomeDir and passed via --mcp-config.
	MCPConfigBytes []byte
	// Env is merged into the child process environment.
	Env map[string]string
	// Binary overrides the claude binary path (empty = "claude" on PATH).
	Binary string
	// OnEvent is invoked for every parsed stdout StreamEvent, in order, from the
	// reader goroutine. One stream-json line can yield MULTIPLE StreamEvents (an
	// assistant message with N content blocks), and OnEvent fires once per
	// StreamEvent. Must not block indefinitely.
	OnEvent func(ev StreamEvent)
	// OnExit is invoked EXACTLY ONCE when the process exits / stdout closes /
	// Stop completes. err is the process exit error (nil on clean exit).
	OnExit func(err error)
	// Logger receives one-line ops messages (matches the daemon idiom).
	Logger func(msg string)
	// StopGrace is how long graceful Stop waits after SIGTERM before SIGKILL.
	// Defaults to 5s when zero.
	StopGrace time.Duration
}

// ClaudeSession is a long-lived claude process with an injectable stdin and a
// streamed stdout. Safe for concurrent Inject/Stop.
type ClaudeSession struct {
	cfg  ClaudeSessionConfig
	proc sessionProc

	stdinMu sync.Mutex // guards writes + closed
	closed  bool       // set once Stop begins / process exited; blocks Inject

	stopOnce sync.Once
	exitOnce sync.Once
	done     chan struct{} // closed after OnExit fires
}

// StartClaudeSession writes the mcp-config (if any), launches the process, and
// starts the stdout reader goroutine. The returned session is immediately
// usable for Inject/Stop.
func StartClaudeSession(ctx context.Context, cfg ClaudeSessionConfig) (*ClaudeSession, error) {
	if cfg.AgentID == "" {
		return nil, errors.New("claude_session: agent_id required")
	}
	if cfg.Launcher == nil {
		cfg.Launcher = execLauncher{}
	}
	if cfg.Logger == nil {
		cfg.Logger = func(string) {}
	}
	if cfg.StopGrace <= 0 {
		cfg.StopGrace = 5 * time.Second
	}

	mcpPath, err := writeMCPConfig(cfg.HomeDir, cfg.MCPConfigBytes)
	if err != nil {
		return nil, err
	}

	spec := ClaudeLaunchSpec{
		AgentID:       cfg.AgentID,
		WorkspaceDir:  cfg.WorkspaceDir,
		MCPConfigPath: mcpPath,
		Env:           cfg.Env,
		Binary:        cfg.Binary,
	}
	proc, err := cfg.Launcher.Launch(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("claude_session: launch: %w", err)
	}

	s := &ClaudeSession{
		cfg:  cfg,
		proc: proc,
		done: make(chan struct{}),
	}
	go s.readLoop()
	return s, nil
}

// mcpConfigFileName is the file the session writes the --mcp-config document
// to under HomeDir.
const mcpConfigFileName = "mcp_config.runtime.json"

// writeMCPConfig writes the config bytes under homeDir and returns the path.
// Returns ("", nil) when there is nothing to write.
func writeMCPConfig(homeDir string, b []byte) (string, error) {
	if len(b) == 0 {
		return "", nil
	}
	if homeDir == "" {
		return "", errors.New("claude_session: home_dir required to write mcp-config")
	}
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		return "", fmt.Errorf("claude_session: mkdir home_dir: %w", err)
	}
	path := filepath.Join(homeDir, mcpConfigFileName)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", fmt.Errorf("claude_session: write mcp-config: %w", err)
	}
	return path, nil
}

// readLoop scans stdout line-by-line, parses each line via ParseStreamLine
// (the D2 claude 2.1.156 stream-json parser), and dispatches to OnEvent ONCE
// PER parsed StreamEvent (one line can carry multiple content-block events). It
// always terminates when stdout closes or the process exits, then fires OnExit
// exactly once. This is the sole goroutine the session spawns; joining it =
// OnExit fired.
func (s *ClaudeSession) readLoop() {
	scanner := bufio.NewScanner(s.proc.Stdout())
	// Match AgentRunner's larger line cap; agent JSON can be long.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		raw := make([]byte, len(line))
		copy(raw, line)
		if len(raw) == 0 {
			continue
		}
		events, err := ParseStreamLine(raw)
		if err != nil {
			// Don't drop the stream on a single malformed line; log + skip.
			s.cfg.Logger(fmt.Sprintf("[worker] claude_session: parse stream line: %v", err))
			continue
		}
		if s.cfg.OnEvent != nil {
			for _, ev := range events {
				s.cfg.OnEvent(ev)
			}
		}
	}
	scanErr := scanner.Err()

	// stdout has closed → the process is exiting (or already gone). Wait for
	// the exit status and surface it through OnExit. Stop() may have already
	// signalled; Wait still returns the terminal status here.
	waitErr := s.proc.Wait()
	exitErr := waitErr
	if exitErr == nil {
		exitErr = scanErr
	}
	s.fireExit(exitErr)
}

// fireExit marks the session closed and invokes OnExit exactly once, then
// closes done. Concurrency-safe; idempotent.
func (s *ClaudeSession) fireExit(err error) {
	s.exitOnce.Do(func() {
		s.stdinMu.Lock()
		s.closed = true
		s.stdinMu.Unlock()
		if s.cfg.OnExit != nil {
			s.cfg.OnExit(err)
		}
		close(s.done)
	})
}

// Inject encodes userMessage as a stream-json user line and writes it to the
// (still-open) stdin. Concurrency-safe. Returns ErrSessionClosed if the
// session has stopped or its process has exited.
func (s *ClaudeSession) Inject(ctx context.Context, userMessage string) error {
	line, err := encodeUserMessage(userMessage)
	if err != nil {
		return err
	}
	s.stdinMu.Lock()
	defer s.stdinMu.Unlock()
	if s.closed {
		return ErrSessionClosed
	}
	if _, err := s.proc.Stdin().Write(line); err != nil {
		return fmt.Errorf("claude_session: write stdin: %w", err)
	}
	return nil
}

// Stop terminates the process. graceful=true: SIGTERM, wait up to StopGrace,
// then SIGKILL if still alive. graceful=false: SIGKILL immediately. It blocks
// until the reader goroutine has joined and OnExit has fired (exactly once).
// Idempotent.
func (s *ClaudeSession) Stop(ctx context.Context, graceful bool) error {
	s.stopOnce.Do(func() {
		// Block further Injects immediately.
		s.stdinMu.Lock()
		s.closed = true
		s.stdinMu.Unlock()

		if graceful {
			if err := s.proc.Signal(syscall.SIGTERM); err != nil {
				s.cfg.Logger(fmt.Sprintf("[worker] claude_session: SIGTERM: %v", err))
			}
			select {
			case <-s.done:
				return // process exited within grace; reader joined
			case <-time.After(s.cfg.StopGrace):
				s.cfg.Logger("[worker] claude_session: grace expired, SIGKILL")
			case <-ctx.Done():
				s.cfg.Logger("[worker] claude_session: stop ctx cancelled, SIGKILL")
			}
		}
		if err := s.proc.Kill(); err != nil {
			s.cfg.Logger(fmt.Sprintf("[worker] claude_session: SIGKILL: %v", err))
		}
	})
	// Always wait for the reader goroutine to finish + OnExit to fire so the
	// caller has a clean join point with no goroutine leak.
	select {
	case <-s.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// Done returns a channel closed after the process has exited and OnExit has
// fired. Useful for callers that want to await termination without Stop.
func (s *ClaudeSession) Done() <-chan struct{} { return s.done }

// Wait blocks until the process has exited and OnExit has fired.
func (s *ClaudeSession) Wait() { <-s.done }
