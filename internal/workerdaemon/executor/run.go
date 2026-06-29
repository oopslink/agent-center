package executor

// run.go — F1 (process model) executor-side entrypoint loop (design §11.2).
//
// This is what the forked `worker executor` process runs. It is PURE COMPUTE: it
// talks to the orchestrator EXCLUSIVELY through the F2 file protocol under its own
// directory and never opens a center / mcp connection. The state machine:
//
//	SPAWNED → ReadInput → WriteStatus(running) → Runner.Run (streaming progress,
//	          refreshing last_progress_at for the watchdog)
//	        → success: WriteOutput(result) + WriteStatus(done)  → return nil (exit 0)
//	        → failure: WriteOutput(error)  + WriteStatus(failed) → return err (exit !=0)
//
// The actual compute (which agent CLI / model) is behind the Runner port: the
// orchestrator (F3) decides the model-routed command and passes it via the spawn
// argv; the default production CommandRunner just runs that command inside the
// executor's isolated workspace. Tests inject a fake Runner.

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/oopslink/agent-center/internal/clock"
)

// Runner performs the executor's compute inside its isolated workspace and
// returns a result string plus a one-line summary (for the status file the
// orchestrator relays to chat). It must not connect to the center.
type Runner interface {
	Run(ctx context.Context, rc RunContext) (RunResult, error)
}

// RunContext is what the Runner receives: the resolved Input, the absolute
// workspace path it must confine its file edits to, and a Progress sink that
// appends to progress.jsonl and refreshes the watchdog timestamp.
type RunContext struct {
	Input        Input
	WorkspaceDir string
	// Progress streams a progress line. Errors appending are non-fatal to the run
	// (best-effort relay) and are swallowed by the provided closure.
	Progress func(phase, message string)
}

// RunResult is the Runner's success payload.
type RunResult struct {
	Result  string // full result written to output.json
	Summary string // one-line summary written to status (chat relay)
	// Usage is the run's aggregate token usage parsed from the runner stream
	// (v2.20.0 F2 / T613). The zero value means none observed — recordSuccess then
	// leaves output.json's usage nil. The orchestrator relays a non-zero usage to
	// the center's report_usage, tagged with input.json's Source.TaskRef.
	Usage TokenUsage
}

// RunConfig configures RunExecutor (the entrypoint).
type RunConfig struct {
	// AgentRoot anchors the FileExchange Layout (the per-agent home).
	AgentRoot string
	// ExecutorID is this executor's id (its directory under executors/).
	ExecutorID string
	// Runner does the compute. Nil → a CommandRunner built from RunnerCmd.
	Runner Runner
	// RunnerCmd is the default-runner command (the model-routed agent CLI passed by
	// the orchestrator after `--`). Used only when Runner is nil.
	RunnerCmd []string
	// Clock is injected for deterministic tests. Nil → SystemClock.
	Clock clock.Clock
}

// RunExecutor executes the entrypoint state machine. The returned error is the
// process exit signal: nil → exit 0 (success), non-nil → exit non-zero (failure).
// Even on failure it makes a best effort to record output.json + status=failed so
// the orchestrator's dual completion signal (design §9) has the detail.
func RunExecutor(ctx context.Context, cfg RunConfig) error {
	clk := cfg.Clock
	if clk == nil {
		clk = clock.SystemClock{}
	}
	layout, err := NewLayout(cfg.AgentRoot)
	if err != nil {
		return err
	}
	fx, err := NewFileExchange(layout, clk)
	if err != nil {
		return err
	}

	in, err := fx.ReadInput(cfg.ExecutorID)
	if err != nil {
		// Without input we cannot run; surface it. There is no valid status to write
		// (started_at/model unknown), so we just fail — the orchestrator sees the
		// process exit non-zero with no status==running and treats it as a crash (§9).
		return fmt.Errorf("executor: read input: %w", err)
	}

	st := Status{
		ExecutorID:     in.ExecutorID,
		State:          StateRunning,
		Model:          in.Model,
		StartedAt:      clk.Now(),
		LastProgressAt: clk.Now(),
	}
	if err := fx.WriteStatus(st); err != nil {
		return fmt.Errorf("executor: write running status: %w", err)
	}

	wsDir, err := layout.WorkspaceDir(in.ExecutorID)
	if err != nil {
		return err
	}

	runner := cfg.Runner
	if runner == nil {
		runner = NewCommandRunner(cfg.RunnerCmd)
	}

	progress := func(phase, message string) {
		entry := ProgressEntry{At: clk.Now(), Phase: phase, Message: message}
		_ = fx.AppendProgress(in.ExecutorID, entry) // best-effort relay
		// Refresh the watchdog timestamp so a long but live run is not judged stalled.
		st.LastProgressAt = clk.Now()
		_ = fx.WriteStatus(st)
	}

	res, runErr := runner.Run(ctx, RunContext{Input: in, WorkspaceDir: wsDir, Progress: progress})
	if runErr != nil {
		return recordFailure(fx, in, &st, clk, runErr)
	}
	return recordSuccess(fx, in, &st, clk, res)
}

// recordSuccess writes output.json (success) + status=done and returns nil.
func recordSuccess(fx *FileExchange, in Input, st *Status, clk clock.Clock, res RunResult) error {
	out := Output{
		ExecutorID: in.ExecutorID,
		Success:    true,
		Result:     res.Result,
		FinishedAt: clk.Now(),
	}
	// Record the run's token usage when the runner observed any (v2.20.0 F2 / T613).
	// Zero stays nil — nothing for the orchestrator's writeback to report.
	if !res.Usage.IsZero() {
		u := res.Usage
		out.Usage = &u
	}
	if err := fx.WriteOutput(out); err != nil {
		return fmt.Errorf("executor: write output: %w", err)
	}
	st.State = StateDone
	st.Summary = res.Summary
	st.LastProgressAt = clk.Now()
	if err := fx.WriteStatus(*st); err != nil {
		return fmt.Errorf("executor: write done status: %w", err)
	}
	return nil
}

// recordFailure writes output.json (error) + status=failed and returns runErr so
// the process exits non-zero (the orchestrator's exit-code signal, design §9).
func recordFailure(fx *FileExchange, in Input, st *Status, clk clock.Clock, runErr error) error {
	detail := &ErrorDetail{Kind: "runner_failed", Message: runErr.Error()}
	out := Output{
		ExecutorID: in.ExecutorID,
		Success:    false,
		Error:      detail,
		FinishedAt: clk.Now(),
	}
	// Best-effort: even if writing output/status fails, return the original runErr.
	_ = fx.WriteOutput(out)
	st.State = StateFailed
	st.Error = detail
	st.LastProgressAt = clk.Now()
	_ = fx.WriteStatus(*st)
	return runErr
}

// CommandRunner is the default production Runner: it runs a command (the model
// -routed agent CLI the orchestrator chose) inside the executor's workspace and
// returns its combined output as the result. It is the F1 process-model default;
// F3 supplies the actual model-routed argv. os/exec is reached through a seam so
// the run loop is unit-testable without a real CLI.
type CommandRunner struct {
	cmd []string
	run func(ctx context.Context, dir string, name string, args ...string) (string, error)
}

// NewCommandRunner builds a CommandRunner for cmd (name + args). An empty cmd is
// allowed to construct but errors at Run time with a clear message — the
// process-model layer does not invent a command.
func NewCommandRunner(cmd []string) *CommandRunner {
	return &CommandRunner{cmd: cmd, run: execRun}
}

// execRun is the production exec seam: run name+args under dir, returning combined
// output. The executor process's own (already-sanitized, mcp-free) environment is
// inherited — no center credentials are reachable to pass down.
func execRun(ctx context.Context, dir string, name string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	return string(out), err
}

// Run executes the configured command in rc.WorkspaceDir.
func (r *CommandRunner) Run(ctx context.Context, rc RunContext) (RunResult, error) {
	if len(r.cmd) == 0 {
		return RunResult{}, errors.New("executor: no runner command configured (orchestrator must supply the model-routed agent CLI)")
	}
	rc.Progress("start", "running "+r.cmd[0])
	out, err := r.run(ctx, rc.WorkspaceDir, r.cmd[0], r.cmd[1:]...)
	if err != nil {
		return RunResult{}, fmt.Errorf("runner command %q: %w: %s", r.cmd[0], err, strings.TrimSpace(out))
	}
	rc.Progress("done", "runner command completed")
	// Parse the run's per-turn token usage from the captured stream (v2.20.0 F2 /
	// T613). Best-effort: a runner whose output carries no parseable result line
	// yields a zero usage, which recordSuccess omits from output.json.
	return RunResult{Result: out, Summary: summarize(out), Usage: ParseRunnerUsage(out)}, nil
}

// summarize takes the first non-empty line of out (trimmed) as the chat-relay
// summary, bounded to a sane length.
func summarize(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 200 {
			return line[:200]
		}
		return line
	}
	return ""
}
