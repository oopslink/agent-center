package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
)

// ExecutorCommand is the F1 (agent-concurrent-execution §4/§11.2) per-task
// EXECUTOR entry. It is the forked, pure-compute worker the orchestrator (监工)
// spawns for ONE task: it reads its goal from <agent-root>/executors/<id>/input.json,
// runs the model-routed agent CLI inside its own isolated git worktree, streams
// progress, and writes output.json + status — all over the F2 file protocol.
//
// CRITICAL (the executor's defining property): it NEVER connects to the center or
// mcp and holds NO credentials. Unlike mcp-host / agent-supervisor it takes no
// admin URL, no worker token, and is launched (by executor.Spawner) with a
// sanitized, mcp-free environment. Binding is purely its --agent-root + --executor-id.
// System-audience (the orchestrator spawns it, operators do not), so it lives
// under the `worker` group alongside mcp-host / agent-supervisor.
func ExecutorCommand() *Command {
	return &Command{
		Name:    "executor",
		Summary: "Per-task isolated executor (system; spawned by the orchestrator, no mcp/credentials; F1)",
		LongHelp: "Runs ONE task as a pure-compute child of the agent's orchestrator. Reads " +
			"goal/model/context from <agent-root>/executors/<executor-id>/input.json, runs the " +
			"orchestrator-supplied model-routed command (--runner-cmd, a JSON argv array) inside " +
			"its isolated git worktree, and writes progress.jsonl / output.json / status back over " +
			"the file protocol. NEVER connects to the center or mcp and holds no credentials. " +
			"Flags: --agent-root, --executor-id, [--runner-cmd].",
		Flags: func(fs *flag.FlagSet) Handler {
			agentRoot := fs.String("agent-root", "", "per-agent home anchoring executors/<id>/ (required)")
			executorID := fs.String("executor-id", "", "the executor id whose directory this process operates in (required)")
			runnerCmd := fs.String("runner-cmd", "", "JSON array of the model-routed command to run in the workspace (e.g. [\"claude\",\"-p\",\"...\"])")
			return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
				return runExecutor(ctx, errw, *agentRoot, *executorID, *runnerCmd)
			}
		},
	}
}

// runExecutor decodes the flags and drives executor.RunExecutor. The process exit
// code IS the completion signal (design §9): 0 on success, non-zero on failure
// (RunExecutor having recorded output.json + status=failed for the detail).
func runExecutor(ctx context.Context, errw io.Writer, agentRoot, executorID, runnerCmdJSON string) ExitCode {
	agentRoot = strings.TrimSpace(agentRoot)
	executorID = strings.TrimSpace(executorID)
	if agentRoot == "" {
		fmt.Fprintln(errw, "Error: executor: --agent-root is required")
		return ExitUsage
	}
	if executorID == "" {
		fmt.Fprintln(errw, "Error: executor: --executor-id is required")
		return ExitUsage
	}

	var runnerCmd []string
	if s := strings.TrimSpace(runnerCmdJSON); s != "" {
		if err := json.Unmarshal([]byte(s), &runnerCmd); err != nil {
			fmt.Fprintf(errw, "Error: executor: bad --runner-cmd JSON: %v\n", err)
			return ExitUsage
		}
	}

	// SIGINT/SIGTERM cancels the run ctx so the runner command is killed and the
	// executor can record a failed status before exiting (mirrors mcp-host).
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-runCtx.Done():
		}
	}()

	err := executor.RunExecutor(runCtx, executor.RunConfig{
		AgentRoot:  agentRoot,
		ExecutorID: executorID,
		RunnerCmd:  runnerCmd,
	})
	if err != nil {
		fmt.Fprintf(errw, "Error: executor: %v\n", err)
		return ExitBusinessError
	}
	return ExitOK
}
