package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/oopslink/agent-center/internal/agentsupervisor"
	"github.com/oopslink/agent-center/internal/claudestream"
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
			"token. Flags: --agent-id, --home-dir, --mcp-config-path, [--workspace-dir], [--claude-bin], [--model], [--display-name], [--reset-epoch].",
		Flags: func(fs *flag.FlagSet) Handler {
			agentID := fs.String("agent-id", "", "agent id this supervisor owns (required)")
			homeDir := fs.String("home-dir", "", "per-agent home directory for artifacts (required)")
			mcpConfigPath := fs.String("mcp-config-path", "", "path to the daemon-generated mcp-config (no token; optional)")
			workspaceDir := fs.String("workspace-dir", "", "claude's working directory (the agent workspace; default: inherit)")
			claudeBin := fs.String("claude-bin", "", "override the claude binary path (default: claude on PATH)")
			model := fs.String("model", "", "optional claude --model override")
			displayName := fs.String("display-name", "", "agent human-readable display_name; injected as GIT_{AUTHOR,COMMITTER}_NAME via the ② AgentEnv seam (overrides the ULID default; empty → ULID). EMAIL stays <agent-id>@agent-center (T469)")
			resetEpoch := fs.Int("reset-epoch", 0, "per-agent reset epoch; derives claude --session-id via SessionUUIDGen(agent-id, epoch, generation). 0 = initial; the daemon bumps it on a clean-slate reset and re-passes the durable value on a crash-relaunch (system; v2.7 D2-f)")
			generation := fs.Int("generation", 0, "per-agent crash-relaunch fork generation; derives claude --session-id via SessionUUIDGen(agent-id, epoch, generation). 0 = pre-fix id (initial/normal start); the daemon bumps it per Mode-B relaunch (system; v2.7 GATE-7)")
			resumeFrom := fs.String("resume-from", "", "Mode-B fork: prior session-id to --resume + --fork-session from (the killed session whose lock blocks re-use). Empty = plain start, no fork (system; v2.7 GATE-7)")
			return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
				return runAgentSupervisor(ctx, errw, *agentID, *homeDir, *mcpConfigPath, *workspaceDir, *claudeBin, *model, *displayName, *resetEpoch, *generation, *resumeFrom)
			}
		},
	}
}

// runAgentSupervisor builds the claude streaming ChildCmd (reusing the
// workerdaemon argv pipeline), constructs the Supervisor, setsid-detaches THIS
// process, launches the child, and runs until SIGTERM/SIGINT. Diagnostics go to
// errw; the child's stdout is drained to events.jsonl and is NOT echoed to the
// supervisor's stdout.
func runAgentSupervisor(ctx context.Context, errw io.Writer, agentID, homeDir, mcpConfigPath, workspaceDir, claudeBin, model, displayName string, resetEpoch, generation int, resumeFrom string) ExitCode {
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

	// Build the validated claude streaming argv via the claudestream pipeline
	// (BuildCommand + rewriteForStreamingInput + SessionUUID + --mcp-config
	// <path>). The supervisor holds only the mcp-config PATH; no token here.
	// --model (if any) is appended as an argv flag below.
	childCmd, err := claudestream.BuildStreamingArgv(agentID, strings.TrimSpace(claudeBin), strings.TrimSpace(mcpConfigPath), resetEpoch, generation, strings.TrimSpace(resumeFrom), nil)
	if err != nil {
		fmt.Fprintf(errw, "Error: agent_supervisor: build claude argv: %v\n", err)
		return ExitBusinessError
	}
	if m := strings.TrimSpace(model); m != "" {
		childCmd = append(childCmd, "--model", m)
	}

	sup, err := agentsupervisor.New(agentsupervisor.Config{
		AgentID:      agentID,
		HomeDir:      homeDir,
		SockPath:     agentsupervisor.SockPath(agentID),
		ChildCmd:     childCmd,
		WorkspaceDir: strings.TrimSpace(workspaceDir),
		// T469: inject the human-readable display_name into git author/committer NAME
		// via the ② AgentEnv seam. mergeGitIdentity overlays this OVER the AgentID
		// (ULID) default, keeping EMAIL=<agent-id>@agent-center. Empty display_name →
		// nil → NAME falls back to the ULID default (no empty-author injection).
		AgentEnv: agentsupervisor.DisplayNameEnv(displayName),
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

	// SERVE the reconnectable unix-socket RPC (s2) so a returning daemon can
	// re-attach (hello/inject/read/ack). It runs alongside the drain; on
	// serveCtx cancel (signal/child-exit) Serve closes the listener and removes
	// the socket. Serving is best-effort: a listen failure is logged but does
	// NOT bring down the survival core (s1 behavior stays intact).
	serveCtx, cancelServe := context.WithCancel(ctx)
	defer cancelServe()
	// v2.7 #178: serve on the short temp-dir socket (not under the deeply-nested
	// agent home, which overflowed macOS's 104B sun_path limit). Best-effort
	// clean a stale pre-#178 socket left in the home on upgrade.
	sockPath := agentsupervisor.SockPath(agentID)
	_ = os.Remove(filepath.Join(homeDir, agentsupervisor.DefaultSocketName))
	go func() {
		if err := sup.Serve(serveCtx, sockPath); err != nil {
			fmt.Fprintf(errw, "[agent-supervisor] serve: %v\n", err)
		}
	}()

	// SIGINT/SIGTERM → graceful Stop (clean teardown). NOTE: this is the
	// operator-initiated shutdown path. A DAEMON death does NOT signal the
	// supervisor (it escaped the group), which is exactly how it survives.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sigCh:
		fmt.Fprintln(errw, "[agent-supervisor] signal received, stopping child")
		cancelServe()
		_ = sup.Stop(true /*graceful*/)
	case <-sup.Done():
		// Child exited on its own; stop serving the socket.
		cancelServe()
	case <-ctx.Done():
		cancelServe()
		_ = sup.Stop(true)
	}

	if err := sup.Wait(); err != nil {
		fmt.Fprintf(errw, "[agent-supervisor] child exited: %v\n", err)
		return ExitBusinessError
	}
	return ExitOK
}
