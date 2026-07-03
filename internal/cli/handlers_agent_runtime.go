package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/workerdaemon"
)

// AgentRuntimeCommand is the `worker agent-runtime` entry (T854 D6, design §4.5/§5):
// ONE agent's self-contained runtime, run as its own OS process. The worker's
// launcher fork/execs this subcommand (os.Executable() worker agent-runtime
// --agent-id X --sock-dir D) per agent and rebuilds it on exit.
//
// It shares the worker's config/token bootstrap (it does NOT re-enroll — it loads the
// token the worker persisted), self-builds its center client, runs Boot self-recovery,
// then serves control commands the worker proxies over the unix socket in --sock-dir.
// The flag surface mirrors `worker run` so an operator/launcher can pass the same
// admin-target/token/fingerprint, plus --agent-id and --sock-dir.
func AgentRuntimeCommand() *Command {
	return &Command{
		Name:    "agent-runtime",
		Summary: "Run ONE agent's self-contained runtime process (system; spawned by the worker launcher)",
		LongHelp: "Runs a single agent's runtime in its own process (design §4.5 worker=controller): " +
			"self-builds its center client, runs Boot self-recovery BEFORE accepting commands, serves " +
			"worker-proxied control commands over a unix socket, and drains on SIGINT/SIGTERM. Normally " +
			"spawned by the worker daemon's launcher, not run by hand.",
		Flags: func(fs *flag.FlagSet) Handler {
			cfgPath := fs.String("config", "", "path to agent-center.yaml")
			workerID := fs.String("worker-id", "", "worker identity (required)")
			agentID := fs.String("agent-id", "", "the agent this process serves (required)")
			sockDir := fs.String("sock-dir", "", "short per-worker runtime dir the control socket binds in (required)")
			tickInterval := fs.Duration("tick-interval", time.Second, "Tick + watchdog interval")
			adminToken := fs.String("admin-token", "", "admin bearer token; falls back to AGENT_CENTER_ADMIN_TOKEN env")
			adminTarget := fs.String("admin-target", "", "admin endpoint (default: cfg.server.admin_socket_path)")
			bootstrap := fs.String("bootstrap", "", "admin endpoint URL (alias of --admin-target)")
			token := fs.String("token", "", "admin bearer/enroll token (alias of --admin-token)")
			serverFingerprint := fs.String("server-fingerprint", "", "pinned server cert fingerprint; falls back to AGENT_CENTER_SERVER_FINGERPRINT env")
			return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
				cfgPathV := resolveWorkerConfigPath(*cfgPath, *workerID)
				cfg, _ := config.Load(config.LoadOptions{Path: cfgPathV}) // best-effort; RunAgentRuntime re-loads
				workerIDv := firstNonEmptyWorker(*workerID, cfg.Worker.WorkerID)
				if strings.TrimSpace(workerIDv) == "" {
					fmt.Fprintln(errw, "Error: agent-runtime: worker_id is required (--worker-id or worker.worker_id)")
					return ExitUsage
				}
				if strings.TrimSpace(*agentID) == "" {
					fmt.Fprintln(errw, "Error: agent-runtime: --agent-id is required")
					return ExitUsage
				}
				if strings.TrimSpace(*sockDir) == "" {
					fmt.Fprintln(errw, "Error: agent-runtime: --sock-dir is required")
					return ExitUsage
				}
				logf := func(msg string) { fmt.Fprintf(errw, "[agent-runtime %s] %s\n", *agentID, msg) }
				err := workerdaemon.RunAgentRuntime(ctx, workerdaemon.AgentRuntimeOptions{
					AgentID:      *agentID,
					SockDir:      *sockDir,
					TickInterval: *tickInterval,
					Run: workerdaemon.RunOptions{
						ConfigPath:        cfgPathV,
						WorkerID:          workerIDv,
						AdminToken:        firstNonEmptyWorker(coalesceWorkerFlag(*token, *adminToken), cfg.Worker.Token),
						AdminTarget:       firstNonEmptyWorker(coalesceWorkerFlag(*bootstrap, *adminTarget), cfg.Worker.Bootstrap),
						ServerFingerprint: firstNonEmptyWorker(*serverFingerprint, cfg.Worker.ServerFingerprint),
					},
				}, logf)
				if err != nil {
					if workerdaemon.IsShutdownError(err) {
						logf(err.Error())
						return ExitOK
					}
					fmt.Fprintf(errw, "Error: agent-runtime: %v\n", err)
					return ExitBusinessError
				}
				logf("shutdown complete")
				return ExitOK
			}
		},
	}
}
