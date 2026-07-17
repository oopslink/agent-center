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
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

// Progress phases (the ProgressEntry.Phase enum). Lifecycle phases always write
// status through; phaseRunning is the throttled one.
const (
	phaseStart   = "start"
	phaseRunning = "running"
	phaseDone    = "done"
	// phaseTool marks a per-tool-call record (I109 ②) — a lifecycle-neutral phase that
	// is NOT throttled into the file, so every tool call leaves a trace.
	phaseTool = "tool"
)

const (
	// runnerHeartbeatInterval throttles the LIVENESS heartbeat while a runner command
	// streams output with no tool calls (pure generation). Well below the watchdog
	// stall timeout so a long-but-active run keeps last_progress_at fresh.
	runnerHeartbeatInterval = 15 * time.Second
	// statusRefreshInterval throttles the STATUS file write for running-phase notes.
	// Matches the previous heartbeat cadence, so status/executor.progress behave exactly
	// as before now that progress.jsonl appends are no longer coupled to them (I109 ②).
	statusRefreshInterval = 15 * time.Second
	// maxRunnerLine bounds a single streamed line (claude stream-json lines carrying a
	// large tool result can be big) so bufio.Scanner does not error with "token too long".
	maxRunnerLine = 16 << 20 // 16 MiB
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
	//
	// tools carries the greppable identifiers of what a phaseTool note invoked
	// ("Bash", "git", "push"); it is variadic because every other phase has none. It
	// is a SEPARATE argument rather than something parsed back out of message because
	// message is length-clipped — see ProgressEntry.Tools (I109 ②).
	Progress func(phase, message string, tools ...string)
}

// RunResult is the Runner's success payload.
type RunResult struct {
	Result  string // full result written to output.json
	Summary string // one-line summary written to status (chat relay)
	// ThreadID is the codex thread_id parsed from a cli=codex --json run (T969), empty
	// for claude. Flows into Output.ThreadID → Record.SessionID for tier-1 resume.
	ThreadID string
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

	// progress appends EVERY note to progress.jsonl but throttles the STATUS write
	// (I109 ②). These two used to be welded together, which is what made the record
	// lossy: to keep status writes off the hot path the CALLER throttled itself to one
	// note per ~15s, so the file only ever saw a ~15s SAMPLE of the run and a tool call
	// landing between beats left no trace at all. Splitting them lets the file record
	// every tool call (an append is cheap and append-only) while status keeps exactly
	// its previous write cadence — so watchdog freshness and the executor.progress
	// event are unchanged, byte-for-byte, and only the record gets denser.
	var lastStatusWrite time.Time
	progress := func(phase, message string, tools ...string) {
		now := clk.Now()
		entry := ProgressEntry{At: now, Phase: phase, Message: message, Tools: tools}
		_ = fx.AppendProgress(in.ExecutorID, entry) // best-effort relay
		// A lifecycle note (start/done) always writes through; a running note writes at
		// most once per interval. Refreshing LastProgressAt keeps a long-but-live run from
		// being judged stalled, and Detail carries the note into the executor.progress
		// event so it says WHAT the runner is doing, not just that it is alive (T880).
		lifecycle := phase == phaseStart || phase == phaseDone
		if !lifecycle && now.Sub(lastStatusWrite) < statusRefreshInterval {
			return
		}
		lastStatusWrite = now
		st.LastProgressAt = now
		st.Detail = message
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
		ThreadID:   res.ThreadID, // T969: codex thread_id (empty for claude) → Record.SessionID
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
//
// runErr stays the ROOT-CAUSE signal (recoverable via errors.Is on the return), but a
// failure to PERSIST the error output/status is no longer swallowed (v2.34.0 dogfood
// cleanup): it is joined onto the returned error so it surfaces in the process's exit
// error instead of vanishing. A silent WriteOutput failure here means the orchestrator
// reads no output.json and can only guess from the exit code, so it must be visible.
func recordFailure(fx *FileExchange, in Input, st *Status, clk clock.Clock, runErr error) error {
	detail := &ErrorDetail{Kind: "runner_failed", Message: runErr.Error()}
	out := Output{
		ExecutorID: in.ExecutorID,
		Success:    false,
		Error:      detail,
		FinishedAt: clk.Now(),
	}
	err := runErr
	if werr := fx.WriteOutput(out); werr != nil {
		err = errors.Join(err, fmt.Errorf("executor: write failure output: %w", werr))
	}
	st.State = StateFailed
	st.Error = detail
	st.LastProgressAt = clk.Now()
	if serr := fx.WriteStatus(*st); serr != nil {
		err = errors.Join(err, fmt.Errorf("executor: write failed status: %w", serr))
	}
	return err
}

// CommandRunner is the default production Runner: it runs a command (the model
// -routed agent CLI the orchestrator chose) inside the executor's workspace and
// returns its combined output as the result. It is the F1 process-model default;
// F3 supplies the actual model-routed argv. os/exec is reached through a seam so
// the run loop is unit-testable without a real CLI.
type CommandRunner struct {
	cmd []string
	// run streams the command's output line-by-line via onLine (so Run can refresh the
	// watchdog progress timestamp mid-run) and returns the full combined output.
	run func(ctx context.Context, dir string, cmd []string, onLine func(line string)) (string, error)
}

// NewCommandRunner builds a CommandRunner for cmd (name + args). An empty cmd is
// allowed to construct but errors at Run time with a clear message — the
// process-model layer does not invent a command.
func NewCommandRunner(cmd []string) *CommandRunner {
	return &CommandRunner{cmd: cmd, run: execRun}
}

// execRun is the production exec seam: it streams the command's STDOUT line-by-line
// (invoking onLine per line, so the caller can refresh last_progress_at while a long
// runner — e.g. claude emitting stream-json throughout a multi-minute run — is still
// working, instead of being falsely stall-killed), captures STDERR, and returns the
// full combined output (stdout then stderr). Preserving stderr matters: a runner
// failure's diagnostic (e.g. claude's "Session ID … already in use") is on stderr and
// must survive into the returned output (matching the prior CombinedOutput contract).
// The executor process's own sanitized (mcp-free, no center creds) environment is
// inherited.
func execRun(ctx context.Context, dir string, cmd []string, onLine func(line string)) (string, error) {
	if len(cmd) == 0 {
		return "", errors.New("executor: empty runner command")
	}
	c := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	c.Dir = dir
	stdout, err := c.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderrBuf strings.Builder
	c.Stderr = &stderrBuf
	if err := c.Start(); err != nil {
		return "", err
	}
	var outBuf strings.Builder
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), maxRunnerLine)
	for sc.Scan() {
		line := sc.Text()
		outBuf.WriteString(line)
		outBuf.WriteByte('\n')
		if onLine != nil {
			onLine(line)
		}
	}
	// Read stdout to EOF above, THEN Wait (Wait closes the pipe + reaps + joins the
	// stderr copier goroutine). A scanner error (e.g. an over-long line) is non-fatal —
	// we still Wait for the authoritative exit status and return what we captured.
	werr := c.Wait()
	return outBuf.String() + stderrBuf.String(), werr
}

// Run executes the configured command in rc.WorkspaceDir.
func (r *CommandRunner) Run(ctx context.Context, rc RunContext) (RunResult, error) {
	if len(r.cmd) == 0 {
		return RunResult{}, errors.New("executor: no runner command configured (orchestrator must supply the model-routed agent CLI)")
	}
	rc.Progress(phaseStart, "running "+r.cmd[0])
	// Two distinct signals ride the stream, and conflating them is what made the record
	// lossy (I109 ②):
	//
	//   - EVERY TOOL CALL gets its own entry, unthrottled. A tool call is a discrete,
	//     low-rate event worth one line each; sampling it on a 15s beat meant a call
	//     between beats vanished, and a later `grep -c push` read that absence as proof
	//     the push never happened. The status write stays throttled inside rc.Progress,
	//     so this costs an append, not a status write.
	//
	//   - A LIVENESS HEARTBEAT, still throttled, for stretches with no tool calls at
	//     all (pure generation, a long think). Without it a legitimately busy run has
	//     last_progress_at frozen and gets falsely stall-killed, which is the reason the
	//     heartbeat exists in the first place (T880).
	var lastBeat time.Time
	var lastActivity string
	onLine := func(line string) {
		detail, tools, isTool := streamLineActivity([]byte(line))
		if detail != "" {
			lastActivity = detail
		}
		now := time.Now()
		if isTool {
			// Record the call itself, and count it as a beat: it already refreshed
			// last_progress_at, so the heartbeat has nothing to add.
			lastBeat = now
			rc.Progress(phaseTool, detail, tools...)
			return
		}
		if now.Sub(lastBeat) >= runnerHeartbeatInterval {
			lastBeat = now
			msg := lastActivity
			if msg == "" {
				msg = "executor active (streaming)"
			}
			rc.Progress(phaseRunning, msg)
		}
	}
	out, err := r.run(ctx, rc.WorkspaceDir, r.cmd, onLine)
	if err != nil {
		return RunResult{}, fmt.Errorf("runner command %q: %w: %s", r.cmd[0], err, strings.TrimSpace(out))
	}
	rc.Progress(phaseDone, "runner command completed")
	// Extract the run's final TEXT result + per-turn token usage from the captured
	// stream (T613 usage; T622 result extraction). In production the runner is claude
	// --output-format stream-json --verbose, so `out` is JSON lines — relaying it raw
	// would post a wall of JSON as the task result. ParseRunnerStream pulls the final
	// answer text + the usage; when the output is NOT stream-json (a codex plain run,
	// an error transcript) result comes back empty and we relay the raw output so a
	// non-claude runner's result is never lost.
	// cli branch (T969): a codex --json run emits codex's own JSONL event stream, NOT
	// claude stream-json, so it needs the codex parser (which also captures thread_id
	// for tier-1 resume). The claude path is byte-identical to before — an empty
	// ThreadID and the same ParseRunnerStream result/usage.
	var (
		result, threadID string
		usage            TokenUsage
	)
	if isCodexRunnerCmd(r.cmd) {
		result, threadID, usage = ParseCodexRunnerStream(out)
	} else {
		result, usage = ParseRunnerStream(out)
	}
	if strings.TrimSpace(result) == "" {
		result = out
	}
	return RunResult{Result: result, Summary: summarize(result), Usage: usage, ThreadID: threadID}, nil
}

// isCodexRunnerCmd reports whether a runner argv is a `codex exec ...` invocation, so
// CommandRunner.Run picks the codex JSONL parser (T969). Keyed on the binary basename
// (NewCodexRunnerBuilder defaults to "codex", but a deployment may set an absolute
// path) + the `exec` subcommand, so it never mis-fires on the claude runner.
func isCodexRunnerCmd(cmd []string) bool {
	return len(cmd) >= 2 && filepath.Base(cmd[0]) == "codex" && cmd[1] == "exec"
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
