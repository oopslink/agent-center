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
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/oopslink/agent-center/internal/admin/clienttransport"
	"github.com/oopslink/agent-center/internal/mcphost"
	"github.com/oopslink/agent-center/internal/workerdaemon"
)

// MCPHostCommand is the per-agent stdio MCP server entry (v2.7 b3-i,
// ADR-0049). One process == one agent: it bridges MCP tool calls from a
// claude process to the center's admin agent-tool endpoints. The daemon
// spawns it via --mcp-config with per-server env; it is system-audience
// (operators do not invoke it directly), so it lives under the `worker`
// group alongside the other daemon-internal entries (run / shim).
//
// Binding comes ENTIRELY from env (process-fixed):
//   - AC_MCP_AGENT_ID    operating agent id, injected into every admin
//     call body; NEVER taken from tool args (required).
//   - AC_MCP_ADMIN_URL   admin endpoint: unix:/path or tcp://host:port
//     (required).
//   - AC_MCP_WORKER_TOKEN worker bearer token (owner worker:<id>).
//   - AC_MCP_SERVER_FINGERPRINT  pinned cert fingerprint, required when
//     AC_MCP_ADMIN_URL is tcp://...
func MCPHostCommand() *Command {
	return &Command{
		Name:    "mcp-host",
		Summary: "Per-agent stdio MCP server (system; spawned by the worker daemon, v2.7 b3-i)",
		LongHelp: "Bridges MCP tool calls from one claude process to the center's admin " +
			"agent-tool endpoints. Bound to ONE agent via AC_MCP_AGENT_ID; that agent_id " +
			"is injected into every admin call and is never taken from tool args. " +
			"Reads AC_MCP_AGENT_ID, AC_MCP_ADMIN_URL, AC_MCP_WORKER_TOKEN " +
			"(+ AC_MCP_SERVER_FINGERPRINT for tcp://) from the environment.",
		Flags: func(fs *flag.FlagSet) Handler {
			return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
				return runMCPHost(ctx, errw)
			}
		},
	}
}

// runMCPHost builds the AdminClient from env, constructs the per-agent MCP
// server, and runs it over stdio until SIGINT/SIGTERM (mirroring
// ServerCommand's signal handling). Diagnostics go to errw; stdout/stdin
// are the MCP transport and must not be polluted.
func runMCPHost(ctx context.Context, errw io.Writer) ExitCode {
	agentID := strings.TrimSpace(os.Getenv("AC_MCP_AGENT_ID"))
	if agentID == "" {
		fmt.Fprintln(errw, "Error: mcp_host: AC_MCP_AGENT_ID is required")
		return ExitUsage
	}
	adminURL := strings.TrimSpace(os.Getenv("AC_MCP_ADMIN_URL"))
	if adminURL == "" {
		fmt.Fprintln(errw, "Error: mcp_host: AC_MCP_ADMIN_URL is required (unix:/path or tcp://host:port)")
		return ExitUsage
	}
	token := strings.TrimSpace(os.Getenv("AC_MCP_WORKER_TOKEN"))
	fingerprint := strings.TrimSpace(os.Getenv("AC_MCP_SERVER_FINGERPRINT"))

	target, err := clienttransport.ParseTarget(adminURL)
	if err != nil {
		fmt.Fprintf(errw, "Error: mcp_host: bad AC_MCP_ADMIN_URL: %v\n", err)
		return ExitUsage
	}
	adminClient, err := workerdaemon.NewAdminClientFromTarget(target, fingerprint, 30*time.Second)
	if err != nil {
		fmt.Fprintf(errw, "Error: mcp_host: build admin client: %v\n", err)
		return ExitBusinessError
	}
	adminClient.WithToken(token)

	srv := mcphost.NewServer(mcphost.Config{
		AgentID: agentID,
		Admin:   adminClient,
	})

	// SIGINT/SIGTERM cancels the run ctx so Server.Run closes the stdio
	// connection and returns (mirror ServerCommand).
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

	if err := srv.Run(runCtx, &mcp.StdioTransport{}); err != nil {
		// A canceled ctx is the normal shutdown path, not an error.
		if runCtx.Err() != nil {
			return ExitOK
		}
		fmt.Fprintf(errw, "Error: mcp_host: %v\n", err)
		return ExitBusinessError
	}
	return ExitOK
}
