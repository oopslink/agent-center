// Package workerdaemon: AgentRunner wraps the agent-CLI subprocess
// spawn that the worker daemon's Runtime uses to execute a dispatched
// envelope (Phase C minimal viable path).
//
// Phase C scope: a thin spawn-and-stream-stdout wrapper. The full 11-step
// shim sequence (dispatch_loop.go + shim_supervisor.go) is the v2.3+
// path; here we exercise the end-to-end transport so v2.0 GA's dead
// dispatch path becomes alive. Specifically we:
//
//   - resolve the agent CLI binary (PATH lookup, or a config-mapped
//     override — `--fake-agent=<path>` flag substitutes fakeagent for
//     a real LLM in e2e tests),
//   - spawn with environment scrubbed to a small allow-list,
//   - optionally inject MCP secrets (delegates to MCPInjector — reused
//     from internal/workerdaemon/mcp_injection.go),
//   - stream the agent's stdout line-by-line, parsing fakeagent / shim-
//     compatible JSONL trace events, and emit a per-event callback so
//     Runtime can forward to the admin endpoint.
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
	"strings"
	"sync"
	"syscall"
	"time"
)

// AgentEvent is the parsed JSONL line emitted by the agent CLI on
// stdout. fakeagent uses this exact schema; real shim events are
// translated by their adapter into the same shape before reaching here
// (out of scope for Phase C — fakeagent path only).
//
// Recognised types: "start" / "progress" / "artifact" / "done" /
// "failed". Unknown types are forwarded as "progress" with kind=Type
// so we never drop trace data.
type AgentEvent struct {
	Type      string `json:"type"`
	Milestone string `json:"milestone,omitempty"`
	Content   string `json:"content,omitempty"`
	Text      string `json:"text,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Message   string `json:"message,omitempty"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	Kind      string `json:"kind,omitempty"` // for artifact events
}

// AgentEventHandler is invoked for every JSONL line. Returning a
// non-nil error stops the runner and surfaces the error as the spawn
// result.
type AgentEventHandler func(ctx context.Context, ev AgentEvent, raw []byte) error

// AgentRunnerConfig configures a runner.
type AgentRunnerConfig struct {
	// AgentCLI is the agent kind (claude-code / codex / opencode /
	// fakeagent). Looked up against AgentCLIOverrides first, then
	// falls back to exec.LookPath.
	AgentCLI string
	// CWD is the directory the subprocess is launched in. Empty =
	// inherit parent.
	CWD string
	// Prompt is piped to the subprocess's stdin.
	Prompt string
	// Args are appended to the resolved binary path. E.g. fakeagent
	// expects `--script=<path>`.
	Args []string
	// AgentCLIOverrides maps `agent_cli` → absolute path. Set by the
	// daemon main from `--fake-agent` / `--agent-bin` flags or the
	// config file. Lookup takes precedence over PATH.
	AgentCLIOverrides map[string]string
	// EnvAllowList is the set of env-var names propagated from the
	// parent. Empty = default minimal set.
	EnvAllowList []string
	// ExtraEnv is appended (KEY=VAL strings) after allow-list filtering.
	// Use for MCP_CONFIG / secrets / etc.
	ExtraEnv []string
	// MCPConfigPath is the absolute path of the per-execution
	// mcp_config.runtime.json written by MCPInjector. When non-empty
	// and AgentCLI is a real-agent kind, exposed to the subprocess via
	// MCP_CONFIG=<path> env so the agent picks up the resolved-secret
	// MCP server list (ADR-0027 § 7). fakeagent ignores it.
	MCPConfigPath string
}

// defaultEnvAllowList covers the minimum the agent CLIs need to run.
// Kept small for safety — secrets must come through ExtraEnv (MCP
// injection) not the parent env.
var defaultEnvAllowList = []string{
	"PATH", "HOME", "USER", "LOGNAME",
	"LANG", "LC_ALL", "LC_CTYPE", "TZ",
	"TMPDIR",
}

// AgentRunner spawns + supervises one agent subprocess.
type AgentRunner struct {
	cfg AgentRunnerConfig
}

// NewAgentRunner constructs a runner.
func NewAgentRunner(cfg AgentRunnerConfig) *AgentRunner {
	return &AgentRunner{cfg: cfg}
}

// ResolveBinary returns the absolute path to the agent CLI binary.
// Override map wins; then PATH lookup; then a useful error.
func (r *AgentRunner) ResolveBinary() (string, error) {
	if r.cfg.AgentCLI == "" {
		return "", errors.New("agent_runner: agent_cli required")
	}
	if r.cfg.AgentCLIOverrides != nil {
		if p, ok := r.cfg.AgentCLIOverrides[r.cfg.AgentCLI]; ok && strings.TrimSpace(p) != "" {
			abs, err := filepath.Abs(p)
			if err != nil {
				return "", fmt.Errorf("agent_runner: resolve override %q: %w", p, err)
			}
			return abs, nil
		}
	}
	bin, err := exec.LookPath(r.cfg.AgentCLI)
	if err != nil {
		return "", fmt.Errorf("agent_runner: lookup %q: %w (set --fake-agent or AgentCLIOverrides)", r.cfg.AgentCLI, err)
	}
	return bin, nil
}

// SpawnResult is the outcome of one Run call.
type SpawnResult struct {
	PID       int
	ExitCode  int
	Failed    bool   // true if Run aborted via handler error or non-zero exit
	FailedMsg string // failure reason summary
}

// procHandle is the in-memory handle Runtime keeps so it can SIGTERM
// the subprocess on a kill request.
type procHandle struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	started time.Time
}

// Run launches the subprocess, streams stdout JSONL through handler,
// and returns when the subprocess exits or handler returns an error.
//
// The procHandle is exposed back through `started` channel before the
// subprocess actually runs, so the caller can install it in its
// in-memory map and SIGTERM it on demand.
func (r *AgentRunner) Run(ctx context.Context, handler AgentEventHandler, started chan<- *procHandle) (SpawnResult, error) {
	bin, err := r.ResolveBinary()
	if err != nil {
		return SpawnResult{Failed: true, FailedMsg: err.Error()}, err
	}
	cmd := exec.CommandContext(ctx, bin, r.cfg.Args...)
	if r.cfg.CWD != "" {
		cmd.Dir = r.cfg.CWD
	}
	extraEnv := r.cfg.ExtraEnv
	if strings.TrimSpace(r.cfg.MCPConfigPath) != "" {
		// Pass the per-execution mcp_config.runtime.json to the agent via
		// the standard MCP_CONFIG env (per ADR-0027 § 7). Appended so the
		// caller's ExtraEnv wins if it already set MCP_CONFIG.
		extraEnv = append([]string(nil), r.cfg.ExtraEnv...)
		extraEnv = append(extraEnv, "MCP_CONFIG="+r.cfg.MCPConfigPath)
	}
	cmd.Env = buildEnv(r.cfg.EnvAllowList, extraEnv)
	// Place the child in its own process group so we can SIGTERM the
	// whole tree (agent CLIs frequently fork helpers).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return SpawnResult{Failed: true, FailedMsg: err.Error()},
			fmt.Errorf("agent_runner: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return SpawnResult{Failed: true, FailedMsg: err.Error()},
			fmt.Errorf("agent_runner: stdout pipe: %w", err)
	}
	// stderr forwarded to parent stderr for ops visibility — prefixed
	// in Runtime via its logger.
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return SpawnResult{Failed: true, FailedMsg: err.Error()},
			fmt.Errorf("agent_runner: start %s: %w", bin, err)
	}
	handle := &procHandle{cmd: cmd, started: time.Now()}
	if started != nil {
		// Non-blocking handoff; Runtime always allocates a buffered
		// chan so this is safe.
		select {
		case started <- handle:
		default:
		}
	}

	// Pipe prompt to stdin then close.
	go func() {
		defer stdin.Close()
		if r.cfg.Prompt != "" {
			_, _ = io.WriteString(stdin, r.cfg.Prompt)
			if !strings.HasSuffix(r.cfg.Prompt, "\n") {
				_, _ = io.WriteString(stdin, "\n")
			}
		}
	}()

	// Stream stdout line-by-line.
	var handlerErr error
	scanner := bufio.NewScanner(stdout)
	// Allow longer lines than the default 64KB cap; agent JSON can be
	// long (especially artifacts).
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		// Copy because scanner.Bytes() is reused next iteration.
		raw := make([]byte, len(line))
		copy(raw, line)
		trimmed := strings.TrimSpace(string(raw))
		if trimmed == "" {
			continue
		}
		ev := AgentEvent{}
		if err := json.Unmarshal(raw, &ev); err != nil {
			// Non-JSON line — forward as a progress event with the raw
			// text as content so trace replay still shows it.
			ev = AgentEvent{Type: "progress", Milestone: "stdout_text", Content: trimmed}
		}
		if handler != nil {
			if err := handler(ctx, ev, raw); err != nil {
				handlerErr = err
				// Stop reading further; we'll kill the process.
				break
			}
		}
		if ev.Type == "done" || ev.Type == "failed" {
			// Drain rest after a terminal event so the subprocess can
			// exit cleanly, but don't block forever on it.
			break
		}
	}
	if err := scanner.Err(); err != nil && handlerErr == nil {
		handlerErr = err
	}

	// If handler aborted, SIGTERM the subprocess.
	if handlerErr != nil {
		_ = terminate(handle.cmd, 2*time.Second)
	}

	waitErr := cmd.Wait()
	exitCode := 0
	if exitErr, ok := waitErr.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if waitErr != nil && handlerErr == nil {
		handlerErr = waitErr
	}
	res := SpawnResult{
		PID:      cmd.Process.Pid,
		ExitCode: exitCode,
	}
	if handlerErr != nil {
		res.Failed = true
		res.FailedMsg = handlerErr.Error()
		return res, handlerErr
	}
	if exitCode != 0 {
		res.Failed = true
		res.FailedMsg = fmt.Sprintf("agent exited non-zero: %d", exitCode)
	}
	return res, nil
}

// Signal sends sig to the process group (negative pid). Safe on a
// finished process — returns the os.Signal error verbatim, which the
// caller is expected to log + ignore.
func (h *procHandle) Signal(sig os.Signal) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cmd == nil || h.cmd.Process == nil {
		return errors.New("agent_runner: no process")
	}
	// Negative PID = signal the group (matches Setpgid above).
	pgid := h.cmd.Process.Pid
	if s, ok := sig.(syscall.Signal); ok {
		return syscall.Kill(-pgid, s)
	}
	return h.cmd.Process.Signal(sig)
}

// PID returns the subprocess PID (0 if not started).
func (h *procHandle) PID() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cmd == nil || h.cmd.Process == nil {
		return 0
	}
	return h.cmd.Process.Pid
}

// terminate sends SIGTERM, waits up to grace, then SIGKILL.
func terminate(cmd *exec.Cmd, grace time.Duration) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pgid := cmd.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(grace):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-done
		return nil
	}
}

// buildEnv assembles the env slice from the parent allowlist + extras.
func buildEnv(allow []string, extra []string) []string {
	if len(allow) == 0 {
		allow = defaultEnvAllowList
	}
	out := make([]string, 0, len(allow)+len(extra))
	for _, name := range allow {
		if v, ok := os.LookupEnv(name); ok {
			out = append(out, name+"="+v)
		}
	}
	out = append(out, extra...)
	return out
}
