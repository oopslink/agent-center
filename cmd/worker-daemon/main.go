// Command worker-daemon is the v2.2 Phase C worker daemon binary.
//
// It is the missing v2.0 GA consumer of the dispatchq queue. Without
// this process, dispatched tasks land in the in-memory queue and sit
// there forever — the system has no executor. v2.2-C wires:
//
//	cmd/worker-daemon  →  AdminClient  →  /admin/dispatch/queue/pull
//	                                  →   /admin/kill/queue/pull
//	                                  →   /admin/taskruntime/exec/report-*
//
// Per conventions § 0.4 the daemon never opens the SQLite file directly;
// all state transitions go through the center AppService via the unix
// socket admin endpoint.
//
// Flags:
//
//	--config=<path>           Path to agent-center.yaml (re-uses the
//	                          CLI config loader).
//	--worker-id=<id>          Required if not derivable from config.
//	--fake-agent=<path>       Override map: "fakeagent" → <path>. Lets
//	                          e2e tests run cmd/fakeagent in place of a
//	                          real LLM agent.
//	--poll-interval=<dur>     Default 1s.
//
// Boot sequence:
//  1. Load config, resolve admin socket path.
//  2. Construct AdminClient.
//  3. Enroll self via /admin/workforce/worker/enroll.
//  4. Enter Runtime.Run main loop until SIGINT/SIGTERM.
//
// Graceful shutdown on SIGINT/SIGTERM: stop polling, wait for in-flight
// executions to drain (default 30s grace), then exit.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/oopslink/agent-center/internal/admin/clienttransport"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/workerdaemon"
)

// fsSkillLoader wraps os.DirFS so the worker daemon's prompt assembly
// can load skill files (worker-agent.md + any extra skills referenced by
// the envelope) from disk. Built only when --skills-dir is supplied;
// when omitted the daemon falls back to a StaticSkillLoader{} (empty),
// which AssemblePrompt tolerates by skipping missing skills.

func main() {
	var (
		cfgPath      = flag.String("config", "", "path to agent-center.yaml")
		workerID     = flag.String("worker-id", "", "worker identity (required)")
		fakeAgent    = flag.String("fake-agent", "", "override path for the 'fakeagent' agent_cli (e2e tests)")
		pollInterval = flag.Duration("poll-interval", 1*time.Second, "queue poll interval")
		capsFlag     = flag.String("capabilities", "", "comma-separated capability list")
		adminToken   = flag.String("admin-token", "",
			"admin bearer token (required by v2.3-3a auth); falls back to AGENT_CENTER_ADMIN_TOKEN env")
		// v2.3-7b (task #27): cross-host worker support. Either flag
		// alone overrides cfg.Server.AdminSocketPath; --server-
		// fingerprint is REQUIRED when --admin-target is tcp:// (no
		// silent fallback to TLS-without-verify; pinning is the
		// trust anchor).
		adminTarget = flag.String("admin-target", "",
			"admin endpoint, e.g. unix:/run/admin.sock or tcp://host:7300 (default: cfg.server.admin_socket_path)")
		serverFingerprint = flag.String("server-fingerprint", "",
			"sha256:HH:HH:... pinned server cert fingerprint (required with --admin-target=tcp://...); falls back to AGENT_CENTER_SERVER_FINGERPRINT env")
		skillsDir = flag.String("skills-dir", "",
			"directory containing worker-agent.md + extra skills (real-agent dispatch)")
	)
	flag.Parse()

	if strings.TrimSpace(*workerID) == "" {
		fmt.Fprintln(os.Stderr, "[worker] --worker-id is required")
		os.Exit(2)
	}

	cfg, err := config.Load(config.LoadOptions{Path: *cfgPath})
	if err != nil {
		for _, r := range config.AsErrorList(err) {
			fmt.Fprintf(os.Stderr, "[worker] config: %s\n", r)
		}
		os.Exit(2)
	}

	// Build AdminClient. v2.3-7b: --admin-target overrides
	// cfg.Server.AdminSocketPath; if neither set, fail.
	targetSpec := strings.TrimSpace(*adminTarget)
	if targetSpec == "" {
		sock := strings.TrimSpace(cfg.Server.AdminSocketPath)
		if sock == "" {
			fmt.Fprintln(os.Stderr, "[worker] config error: either --admin-target (e.g. unix:/path or tcp://host:port) or server.admin_socket_path is required")
			os.Exit(2)
		}
		targetSpec = "unix:" + sock
	}
	parsedTarget, err := clienttransport.ParseTarget(targetSpec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[worker] config error: %v\n", err)
		os.Exit(2)
	}
	fingerprint := strings.TrimSpace(*serverFingerprint)
	if fingerprint == "" {
		fingerprint = strings.TrimSpace(os.Getenv("AGENT_CENTER_SERVER_FINGERPRINT"))
	}
	// v2.3-3a (task #28): admin endpoint requires a bearer token on
	// every request. Resolution order: --admin-token flag, then
	// AGENT_CENTER_ADMIN_TOKEN env. Empty permitted at construct-time;
	// server will 401 if no header reaches it.
	token := strings.TrimSpace(*adminToken)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("AGENT_CENTER_ADMIN_TOKEN"))
	}
	client, err := workerdaemon.NewAdminClientFromTarget(parsedTarget, fingerprint, 30*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[worker] admin client: %v\n", err)
		os.Exit(2)
	}
	client = client.WithToken(token)
	logger := func(msg string) { fmt.Fprintf(os.Stderr, "[worker] %s\n", msg) }

	// Build Runtime config.
	overrides := map[string]string{}
	if strings.TrimSpace(*fakeAgent) != "" {
		overrides["fakeagent"] = *fakeAgent
	}
	var caps []string
	if strings.TrimSpace(*capsFlag) != "" {
		for _, c := range strings.Split(*capsFlag, ",") {
			c = strings.TrimSpace(c)
			if c != "" {
				caps = append(caps, c)
			}
		}
	}
	rtCfg := workerdaemon.RuntimeConfig{
		WorkerID:          *workerID,
		Capabilities:      caps,
		PollInterval:      *pollInterval,
		AgentCLIOverrides: overrides,
		Logger:            logger,
	}
	// v2.3-3b (task #29): wire the real-agent dispatch chain so real
	// agents (claude-code / codex / opencode) — not just fakeagent —
	// pick up assembled prompts + MCP runtime config. fakeagent path
	// skips both (handled inside defaultAgentSpawner).
	var skillLoader workerdaemon.SkillLoader
	if strings.TrimSpace(*skillsDir) != "" {
		skillLoader = workerdaemon.FSSkillLoader{FS: os.DirFS(*skillsDir)}
	}
	injector := workerdaemon.NewMCPInjector(workerdaemon.NewAdminClientSecretResolver(client))
	rt := workerdaemon.NewRuntimeWithDeps(rtCfg, client, nil, workerdaemon.RuntimeDeps{
		SkillLoader: skillLoader,
		MCPInjector: injector,
	})

	// Signal-aware context.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-sigCh
		logger(fmt.Sprintf("signal %s received; cancelling", s))
		cancel()
	}()

	logger(fmt.Sprintf("starting: worker_id=%s target=%s poll=%s overrides=%d",
		*workerID, targetSpec, *pollInterval, len(overrides)))

	if err := rt.Run(ctx); err != nil {
		// Two flavors:
		//   - initial enroll failure → exit 1 (transport / config issue)
		//   - shutdown grace exceeded → log + exit 0 (we did our best)
		if isShutdownErr(err) {
			logger(err.Error())
			os.Exit(0)
		}
		logger("fatal: " + err.Error())
		os.Exit(1)
	}
	logger("shutdown complete")
}

func isShutdownErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "shutdown grace exceeded")
}
