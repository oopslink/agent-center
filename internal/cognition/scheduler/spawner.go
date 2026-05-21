package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

// SpawnerConfig captures the knobs.
type SpawnerConfig struct {
	// Binary is the path to the `agent-center` binary used to fork the
	// supervisor subprocess. Defaults to "agent-center" (resolved via
	// $PATH).
	Binary string
	// ExtraArgs are appended after the subcommand arguments (test injection).
	ExtraArgs []string
	// HomeOverride is set as $HOME for the subprocess (cognition/02 § 3.2).
	HomeOverride string
	// MemoryDir is set as $AGENT_CENTER_MEMORY_DIR for the subprocess.
	MemoryDir string
	// UsageDir is the directory in which the supervisor subprocess writes
	// the per-invocation usage JSON file before exit. Default: $HOME/.agent-center/invocations.
	UsageDir string
	// EnvAllowList is the set of host env vars passed through verbatim to
	// the supervisor subprocess (PATH is always included).
	EnvAllowList []string
}

// DefaultSpawnerConfig returns v1 defaults.
func DefaultSpawnerConfig() SpawnerConfig {
	return SpawnerConfig{
		Binary:       "agent-center",
		EnvAllowList: []string{"PATH"},
	}
}

// SpawnerDeps are the collaborators the Spawner needs.
type SpawnerDeps struct {
	DB           *sql.DB
	Repo         cognition.SupervisorInvocationRepository
	Sink         *observability.EventSink
	Clock        clock.Clock
	IDGen        idgen.Generator
}

// ProcessRunner is the port the Spawner uses to launch the supervisor
// subprocess. Default implementation is execRunner (real fork+exec); tests
// inject a fake that captures arguments without spawning.
type ProcessRunner interface {
	Start(ctx context.Context, cmd ProcessSpec, onExit func(exitCode int, err error, stderr string)) (ProcessHandle, error)
}

// ProcessSpec describes a fork+exec request.
type ProcessSpec struct {
	Binary  string
	Args    []string
	Env     []string
	WorkDir string
}

// ProcessHandle exposes minimal lifecycle controls. Implementations are
// expected to honour Signal SIGTERM → SIGKILL escalation via Kill.
type ProcessHandle interface {
	// PID returns the process id; -1 when not available.
	PID() int
	// Signal sends sig to the process.
	Signal(sig os.Signal) error
	// Kill force-terminates the process.
	Kill() error
	// Done returns a channel closed when the process has exited and the
	// onExit callback has been invoked.
	Done() <-chan struct{}
}

// execRunner is the real fork+exec implementation.
type execRunner struct{}

// NewExecProcessRunner returns the production ProcessRunner.
func NewExecProcessRunner() ProcessRunner { return execRunner{} }

func (execRunner) Start(ctx context.Context, spec ProcessSpec, onExit func(int, error, string)) (ProcessHandle, error) {
	cmd := exec.Command(spec.Binary, spec.Args...)
	cmd.Env = spec.Env
	cmd.Dir = spec.WorkDir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	cmd.Stdout = os.Stdout
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	h := &execProcessHandle{cmd: cmd, done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		exitCode := 0
		if err != nil {
			if exErr, ok := err.(*exec.ExitError); ok {
				exitCode = exErr.ExitCode()
			} else {
				exitCode = -1
			}
		}
		if onExit != nil {
			onExit(exitCode, err, stderr.String())
		}
		close(h.done)
	}()
	return h, nil
}

type execProcessHandle struct {
	cmd  *exec.Cmd
	done chan struct{}
}

func (h *execProcessHandle) PID() int {
	if h.cmd.Process == nil {
		return -1
	}
	return h.cmd.Process.Pid
}

func (h *execProcessHandle) Signal(sig os.Signal) error {
	if h.cmd.Process == nil {
		return errors.New("spawner: process not started")
	}
	return h.cmd.Process.Signal(sig)
}

func (h *execProcessHandle) Kill() error {
	if h.cmd.Process == nil {
		return nil
	}
	return h.cmd.Process.Kill()
}

func (h *execProcessHandle) Done() <-chan struct{} { return h.done }

// Spawner is the SupervisorSpawner domain service (plan-6 § 3.6).
type Spawner struct {
	cfg     SpawnerConfig
	deps    SpawnerDeps
	runner  ProcessRunner

	mu      sync.Mutex
	live    map[cognition.InvocationID]ProcessHandle
}

// NewSpawner wires a Spawner. runner defaults to NewExecProcessRunner.
func NewSpawner(cfg SpawnerConfig, deps SpawnerDeps, runner ProcessRunner) (*Spawner, error) {
	if deps.DB == nil {
		return nil, errors.New("spawner: db required")
	}
	if deps.Repo == nil {
		return nil, errors.New("spawner: repo required")
	}
	if deps.Sink == nil {
		return nil, errors.New("spawner: sink required")
	}
	if deps.Clock == nil {
		deps.Clock = clock.SystemClock{}
	}
	if deps.IDGen == nil {
		deps.IDGen = idgen.NewGenerator(deps.Clock)
	}
	if cfg.Binary == "" {
		cfg.Binary = "agent-center"
	}
	if cfg.UsageDir == "" {
		// Default to $HOME (or memoryDir if Home empty)
		base := cfg.HomeOverride
		if base == "" {
			base = cfg.MemoryDir
		}
		if base != "" {
			cfg.UsageDir = filepath.Join(base, ".agent-center", "invocations")
		}
	}
	if runner == nil {
		runner = NewExecProcessRunner()
	}
	return &Spawner{cfg: cfg, deps: deps, runner: runner, live: map[cognition.InvocationID]ProcessHandle{}}, nil
}

// Spawn handles one InvocationRequest end-to-end:
//   1. INSERT invocation row (status=running) inside a tx + emit
//      supervisor.invocation_started.
//   2. fork+exec `agent-center supervisor --scope=... --invocation-id=...
//      --trigger-events=...`.
//   3. Async goroutine waits for exit → MarkSucceeded / MarkFailed /
//      MarkTimedOut + emit terminal event.
//
// Returns the new InvocationID. If the partial unique index trips
// (another running invocation in the same scope), returns
// ErrScopeKeyRunningExists — callers (Coalescer) treat this as benign.
func (s *Spawner) Spawn(ctx context.Context, req InvocationRequest) (cognition.InvocationID, error) {
	if req.Scope.IsZero() {
		return "", errors.New("spawner: scope required")
	}
	if req.TriggerEvents.Len() == 0 {
		return "", errors.New("spawner: trigger events required")
	}
	id := cognition.InvocationID(s.deps.IDGen.NewULID())
	now := s.deps.Clock.Now()
	inv, err := cognition.Spawn(cognition.SpawnInput{
		ID:            id,
		Scope:         req.Scope,
		TriggerEvents: req.TriggerEvents,
		StartedAt:     now,
	})
	if err != nil {
		return "", fmt.Errorf("spawner: build invocation: %w", err)
	}
	// Save + emit in tx.
	if err := persistence.RunInTx(ctx, s.deps.DB, func(txCtx context.Context) error {
		if err := s.deps.Repo.Save(txCtx, inv); err != nil {
			return err
		}
		_, err := s.deps.Sink.Emit(txCtx, observability.EmitCommand{
			EventType:     "supervisor.invocation_started",
			Refs:          refsForScope(req.Scope),
			Actor:         observability.Actor("supervisor:" + string(id)),
			Payload: map[string]any{
				"invocation_id":        string(id),
				"scope_kind":           string(req.Scope.Kind()),
				"scope_key":            req.Scope.Key(),
				"trigger_event_ids":    cognition.EventIDsAsStrings(req.TriggerEvents.IDs()),
				"hard_timeout_seconds": inv.HardTimeoutSeconds(),
				"started_at":           now.UTC().Format(time.RFC3339Nano),
			},
			CorrelationID: string(id),
		})
		return err
	}); err != nil {
		if errors.Is(err, cognition.ErrScopeKeyRunningExists) {
			return "", err
		}
		return "", fmt.Errorf("spawner: persist: %w", err)
	}
	// Build subprocess spec.
	args := []string{
		"supervisor",
		"--scope=" + req.Scope.Kind().String() + ":" + req.Scope.Key(),
		"--invocation-id=" + string(id),
		"--trigger-events=" + strings.Join(cognition.EventIDsAsStrings(req.TriggerEvents.IDs()), ","),
	}
	args = append(args, s.cfg.ExtraArgs...)
	env := s.buildEnv(id, req.Scope)
	if err := os.MkdirAll(s.cfg.UsageDir, 0o755); err != nil && s.cfg.UsageDir != "" {
		// Non-fatal — usage write will fail at the subprocess and we'll
		// proceed with zero usage.
		_ = err
	}
	handle, err := s.runner.Start(ctx, ProcessSpec{
		Binary:  s.cfg.Binary,
		Args:    args,
		Env:     env,
		WorkDir: s.cfg.MemoryDir,
	}, func(exitCode int, runErr error, stderr string) {
		s.finalize(context.Background(), id, exitCode, runErr, stderr)
	})
	if err != nil {
		// Mark failed synchronously with cli_command_error reason.
		s.markFailed(ctx, id, cognition.FailedReasonCLICommandError,
			fmt.Sprintf("fork+exec failed: %v", err))
		return id, fmt.Errorf("spawner: start: %w", err)
	}
	s.mu.Lock()
	s.live[id] = handle
	s.mu.Unlock()
	return id, nil
}

func (s *Spawner) buildEnv(id cognition.InvocationID, scope cognition.InvocationScope) []string {
	envMap := map[string]string{
		"AGENT_CENTER_INVOCATION_ID": string(id),
		"AGENT_CENTER_SCOPE":         scope.String(),
		"GIT_AUTHOR_NAME":            "supervisor:" + string(id),
		"GIT_AUTHOR_EMAIL":           "supervisor:" + string(id) + "@agent-center.local",
		"GIT_COMMITTER_NAME":         "supervisor:" + string(id),
		"GIT_COMMITTER_EMAIL":        "supervisor:" + string(id) + "@agent-center.local",
		"GIT_TERMINAL_PROMPT":        "0",
		"GIT_CONFIG_GLOBAL":          "/dev/null",
		"GIT_CONFIG_SYSTEM":          "/dev/null",
	}
	if s.cfg.HomeOverride != "" {
		envMap["HOME"] = s.cfg.HomeOverride
		envMap["XDG_CONFIG_HOME"] = s.cfg.HomeOverride
		envMap["CLAUDE_CONFIG_DIR"] = filepath.Join(s.cfg.HomeOverride, ".claude")
	}
	if s.cfg.MemoryDir != "" {
		envMap["AGENT_CENTER_MEMORY_DIR"] = s.cfg.MemoryDir
	}
	if s.cfg.UsageDir != "" {
		envMap["AGENT_CENTER_USAGE_DIR"] = s.cfg.UsageDir
	}
	// Allow-list pass-through.
	for _, k := range s.cfg.EnvAllowList {
		if v, ok := os.LookupEnv(k); ok {
			if _, set := envMap[k]; !set {
				envMap[k] = v
			}
		}
	}
	out := make([]string, 0, len(envMap))
	for k, v := range envMap {
		out = append(out, k+"="+v)
	}
	return out
}

// finalize is invoked when the subprocess exits. It loads the latest
// invocation row, applies the appropriate terminal transition based on
// exit code + stderr, persists the row, and emits the corresponding
// supervisor.* event.
func (s *Spawner) finalize(ctx context.Context, id cognition.InvocationID, exitCode int, runErr error, stderr string) {
	s.mu.Lock()
	delete(s.live, id)
	s.mu.Unlock()
	now := s.deps.Clock.Now()
	inv, err := s.deps.Repo.FindByID(ctx, id)
	if err != nil {
		return // best-effort — TimeoutHandler or CrashRecovery will pick it up
	}
	if inv.IsTerminal() {
		return // already finalised (e.g. by TimeoutHandler)
	}
	// Detect timeout / oom / non-zero variants.
	if exitCode == 0 {
		tu := s.readUsage(id)
		_ = inv.MarkSucceeded(now, tu, inv.DecisionsMade())
		s.persistTerminal(ctx, inv, "supervisor.invocation_succeeded", map[string]any{
			"invocation_id":  string(id),
			"token_usage":    tu,
			"decisions_made": inv.DecisionsMade(),
			"ended_at":       now.UTC().Format(time.RFC3339Nano),
		})
		return
	}
	reason := cognition.FailedReasonClaudeNonZero
	msg := fmt.Sprintf("exit_code=%d stderr=%s", exitCode, truncate(stderr, 512))
	// Heuristic: SIGKILL with oom hint → oom.
	if exitCode == 137 || strings.Contains(strings.ToLower(stderr), "out of memory") || strings.Contains(stderr, "oom") {
		reason = cognition.FailedReasonOOM
		msg = "OOM killed: " + truncate(stderr, 256)
	}
	if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
		reason = cognition.FailedReasonKilledByAdmin
		msg = "context cancelled: " + runErr.Error()
	}
	if err := inv.MarkFailed(reason, msg, now); err != nil {
		return
	}
	s.persistTerminal(ctx, inv, "supervisor.invocation_failed_alert", map[string]any{
		"invocation_id":  string(id),
		"reason":         string(reason),
		"message":        msg,
		"ended_at":       now.UTC().Format(time.RFC3339Nano),
	})
}

// markFailed is a sync convenience for the "could not even start
// subprocess" path. Different from finalize because the invocation row
// is freshly inserted (version=1) at this point.
func (s *Spawner) markFailed(ctx context.Context, id cognition.InvocationID, reason cognition.InvocationFailedReason, message string) {
	now := s.deps.Clock.Now()
	inv, err := s.deps.Repo.FindByID(ctx, id)
	if err != nil {
		return
	}
	if inv.IsTerminal() {
		return
	}
	_ = inv.MarkFailed(reason, message, now)
	s.persistTerminal(ctx, inv, "supervisor.invocation_failed_alert", map[string]any{
		"invocation_id": string(id),
		"reason":        string(reason),
		"message":       message,
		"ended_at":      now.UTC().Format(time.RFC3339Nano),
	})
}

// persistTerminal commits the terminal status + emits the matching event
// in one tx.
func (s *Spawner) persistTerminal(ctx context.Context, inv *cognition.SupervisorInvocation, eventType observability.EventType, payload map[string]any) {
	_ = persistence.RunInTx(ctx, s.deps.DB, func(txCtx context.Context) error {
		if err := s.deps.Repo.UpdateStatusToTerminal(txCtx, inv); err != nil {
			return err
		}
		_, err := s.deps.Sink.Emit(txCtx, observability.EmitCommand{
			EventType:     eventType,
			Refs:          refsForScope(inv.Scope()),
			Actor:         observability.Actor("supervisor:" + string(inv.ID())),
			Payload:       payload,
			CorrelationID: string(inv.ID()),
		})
		return err
	})
}

// readUsage reads the per-invocation usage JSON file written by the
// subprocess before exit. Missing / malformed files yield zero usage
// (best-effort; failure to read does not block terminal write).
func (s *Spawner) readUsage(id cognition.InvocationID) cognition.TokenUsage {
	if s.cfg.UsageDir == "" {
		return cognition.TokenUsage{}
	}
	p := filepath.Join(s.cfg.UsageDir, string(id)+".usage.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return cognition.TokenUsage{}
	}
	var usage cognition.TokenUsage
	if err := json.Unmarshal(b, &usage); err != nil {
		return cognition.TokenUsage{}
	}
	return usage
}

// SignalAndKill signals the live process for an invocation; used by
// TimeoutHandler to escalate SIGTERM → SIGKILL.
func (s *Spawner) SignalAndKill(id cognition.InvocationID, grace time.Duration, clk clock.Clock) {
	s.mu.Lock()
	h, ok := s.live[id]
	s.mu.Unlock()
	if !ok || h == nil {
		return
	}
	_ = h.Signal(syscall.SIGTERM)
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-h.Done():
		return
	case <-timer.C:
		_ = h.Kill()
	}
}

// LiveCount returns the number of currently-spawned processes (test).
func (s *Spawner) LiveCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.live)
}

// LiveProcessHandle returns the handle for an invocation_id if live (test).
func (s *Spawner) LiveProcessHandle(id cognition.InvocationID) (ProcessHandle, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.live[id]
	return h, ok
}

func refsForScope(scope cognition.InvocationScope) observability.EventRefs {
	switch scope.Kind() {
	case cognition.ScopeTask:
		return observability.EventRefs{TaskID: scope.Key()}
	case cognition.ScopeIssue:
		return observability.EventRefs{IssueID: scope.Key()}
	case cognition.ScopeConversation:
		return observability.EventRefs{ConversationID: scope.Key()}
	case cognition.ScopeWorker:
		return observability.EventRefs{WorkerID: scope.Key()}
	}
	return observability.EventRefs{}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}
