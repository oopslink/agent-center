package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/oopslink/agent-center/internal/agentsupervisor"
	"github.com/oopslink/agent-center/internal/workerdaemon"
)

// AgentSupervisorCommand is the v2.7 (D2-f s1) persistent per-agent SUPERVISOR
// entry. It is a thin, long-lived process that OWNS the agent's claude (claude
// == its child) so a worker-daemon crash/restart does NOT kill claude: the
// supervisor setsids into its own session/group to ESCAPE a killpg of the
// daemon's group, holds claude's stdin open, and continuously drains claude's
// stdout to a persistent offset cursor (events.jsonl).
//
// SCOPE: additive + NOT wired into the daemon (no socket, no attach/reattach —
// those are s2/s3). System-audience (operators do not invoke it directly), so
// it lives under the `worker` group alongside mcp-host / run / shim.
//
// MINIMAL KEY SURFACE: the supervisor receives only the daemon-generated
// mcp-config FILE PATH via --mcp-config-path. It NEVER takes or holds the
// worker token; the daemon generates the mcp-config (which carries the token)
// and the supervisor just points claude at the file.
func AgentSupervisorCommand() *Command {
	return &Command{
		Name:    "agent-supervisor",
		Summary: "Persistent per-agent supervisor that owns claude and survives the daemon (system; v2.7 D2-f)",
		LongHelp: "Owns ONE agent's claude as its child and survives a worker-daemon " +
			"crash/restart by setsid-detaching into its own process group (escaping a " +
			"killpg of the daemon group), holding claude's stdin open, and continuously " +
			"draining claude's stdout to <home>/events.jsonl. Receives only the " +
			"daemon-generated mcp-config FILE PATH (--mcp-config-path) — never the worker " +
			"token. Flags: --agent-id, --home-dir, --mcp-config-path, [--claude-bin], [--model].",
		Flags: func(fs *flag.FlagSet) Handler {
			agentID := fs.String("agent-id", "", "agent id this supervisor owns (required)")
			homeDir := fs.String("home-dir", "", "per-agent home directory for artifacts (required)")
			mcpConfigPath := fs.String("mcp-config-path", "", "path to the daemon-generated mcp-config (no token; optional)")
			claudeBin := fs.String("claude-bin", "", "override the claude binary path (default: claude on PATH)")
			model := fs.String("model", "", "optional claude --model override")
			return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
				return runAgentSupervisor(ctx, errw, *agentID, *homeDir, *mcpConfigPath, *claudeBin, *model)
			}
		},
	}
}

// runAgentSupervisor builds the claude streaming ChildCmd (reusing the
// workerdaemon argv pipeline), constructs the Supervisor, setsid-detaches THIS
// process, launches the child, and runs until SIGTERM/SIGINT. Diagnostics go to
// errw; the child's stdout is drained to events.jsonl and is NOT echoed to the
// supervisor's stdout.
func runAgentSupervisor(ctx context.Context, errw io.Writer, agentID, homeDir, mcpConfigPath, claudeBin, model string) ExitCode {
	agentID = strings.TrimSpace(agentID)
	homeDir = strings.TrimSpace(homeDir)
	if agentID == "" {
		fmt.Fprintln(errw, "Error: agent_supervisor: --agent-id is required")
		return ExitUsage
	}
	if homeDir == "" {
		fmt.Fprintln(errw, "Error: agent_supervisor: --home-dir is required")
		return ExitUsage
	}

	// Build the validated claude streaming argv via the workerdaemon pipeline
	// (BuildCommand + rewriteForStreamingInput + AgentSessionUUID + --mcp-config
	// <path>). The supervisor holds only the mcp-config PATH; no token here.
	// --model (if any) is appended as an argv flag below.
	childCmd, err := workerdaemon.BuildClaudeStreamingArgv(agentID, strings.TrimSpace(claudeBin), strings.TrimSpace(mcpConfigPath), nil)
	if err != nil {
		fmt.Fprintf(errw, "Error: agent_supervisor: build claude argv: %v\n", err)
		return ExitBusinessError
	}
	if m := strings.TrimSpace(model); m != "" {
		childCmd = append(childCmd, "--model", m)
	}

	sup, err := agentsupervisor.New(agentsupervisor.Config{
		AgentID:  agentID,
		HomeDir:  homeDir,
		ChildCmd: childCmd,
		Logger: func(msg string) {
			fmt.Fprintf(errw, "[agent-supervisor] %s\n", msg)
		},
	})
	if err != nil {
		fmt.Fprintf(errw, "Error: agent_supervisor: %v\n", err)
		return ExitUsage
	}

	// DETACH: setsid THIS process into its own session/group BEFORE launching
	// the child, so a later killpg of the daemon's group does not reach the
	// supervisor (the survival guarantee). EPERM (already a group leader) is
	// treated as already-detached inside DetachSession.
	if err := agentsupervisor.DetachSession(); err != nil {
		fmt.Fprintf(errw, "Error: agent_supervisor: detach session: %v\n", err)
		return ExitBusinessError
	}

	if err := sup.Start(); err != nil {
		fmt.Fprintf(errw, "Error: agent_supervisor: start: %v\n", err)
		return ExitBusinessError
	}
	fmt.Fprintf(errw, "[agent-supervisor] agent=%s instance=%s child_pid=%d home=%s\n",
		agentID, sup.InstanceID(), sup.ChildPID(), homeDir)

	// SIGINT/SIGTERM → graceful Stop (clean teardown). NOTE: this is the
	// operator-initiated shutdown path. A DAEMON death does NOT signal the
	// supervisor (it escaped the group), which is exactly how it survives.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sigCh:
		fmt.Fprintln(errw, "[agent-supervisor] signal received, stopping child")
		_ = sup.Stop(true /*graceful*/)
	case <-sup.Done():
		// Child exited on its own.
	case <-ctx.Done():
		_ = sup.Stop(true)
	}

	if err := sup.Wait(); err != nil {
		fmt.Fprintf(errw, "[agent-supervisor] child exited: %v\n", err)
		return ExitBusinessError
	}
	return ExitOK
}
