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
	"path/filepath"
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

	// v2.4-D-X1 fix B5: long-term worker token persistence.
	//
	// Path A — token file exists at <dataDir>/worker-token: load it,
	// swap bearer, skip the initial enroll (worker already exists on
	// the center).
	//
	// Path B — token file missing: the --token flag carries the
	// one-time enroll bearer; call enroll, capture the long-term
	// token from the response, persist it (mode 0600 atomic write),
	// swap bearer.
	//
	// Either way the runtime loop runs with the long-term bearer +
	// SkipInitialEnroll=true so we never re-burn the enroll token
	// and never re-mint orphan long-term tokens.
	tokenPath := workerTokenFilePath(cfg)
	enrollNeeded := true
	if existing, rerr := readWorkerTokenFile(tokenPath); rerr == nil && existing != "" {
		client = client.WithToken(existing)
		enrollNeeded = false
		logger("loaded long-term token from " + tokenPath)
	} else if !os.IsNotExist(rerr) && rerr != nil {
		logger(fmt.Sprintf("warning: read %s: %v — will try enroll", tokenPath, rerr))
	}

	if enrollNeeded {
		ctxEnroll, cancelEnroll := context.WithTimeout(context.Background(), 30*time.Second)
		enrollResp, eerr := client.EnrollWithExchange(ctxEnroll, *workerID, parseCaps(*capsFlag))
		cancelEnroll()
		if eerr != nil {
			fmt.Fprintf(os.Stderr, "[worker] enroll failed: %v\n", eerr)
			os.Exit(1)
		}
		if enrollResp.AdminTokenError != "" {
			fmt.Fprintf(os.Stderr, "[worker] enroll succeeded but server failed to mint long-term token: %s\n", enrollResp.AdminTokenError)
			os.Exit(1)
		}
		if enrollResp.AdminToken == "" {
			fmt.Fprintf(os.Stderr, "[worker] enroll succeeded but server returned no admin_token (older v2.x? check center version)\n")
			os.Exit(1)
		}
		if werr := writeWorkerTokenFile(tokenPath, enrollResp.AdminToken); werr != nil {
			fmt.Fprintf(os.Stderr, "[worker] failed to persist long-term token to %s: %v\n", tokenPath, werr)
			os.Exit(1)
		}
		client = client.WithToken(enrollResp.AdminToken)
		logger("enrolled + persisted long-term token to " + tokenPath)
	}

	// Build Runtime config.
	overrides := map[string]string{}
	if strings.TrimSpace(*fakeAgent) != "" {
		overrides["fakeagent"] = *fakeAgent
	}
	caps := parseCaps(*capsFlag)
	rtCfg := workerdaemon.RuntimeConfig{
		WorkerID:          *workerID,
		Capabilities:      caps,
		PollInterval:      *pollInterval,
		AgentCLIOverrides: overrides,
		Logger:            logger,
		// main.go already ran the enroll-or-load dance above, so the
		// runtime loop must not re-enroll (that would either burn a
		// fresh enroll token we don't have, or churn an orphan long-
		// term token).
		SkipInitialEnroll: true,
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

// parseCaps splits the comma-separated --capabilities flag into a
// trimmed slice. Empty inputs yield nil (the enroll handler tolerates
// nil but not [""]).
func parseCaps(csv string) []string {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil
	}
	var out []string
	for _, c := range strings.Split(csv, ",") {
		c = strings.TrimSpace(c)
		if c != "" {
			out = append(out, c)
		}
	}
	return out
}

// workerTokenFilePath returns the on-disk path the worker daemon
// uses to persist its long-term admin token across restarts. Lives
// next to the worker's SQLite DB so backup boundaries match.
func workerTokenFilePath(cfg config.Config) string {
	sqlitePath := strings.TrimSpace(cfg.Server.SqlitePath)
	if sqlitePath == "" {
		return "worker-token"
	}
	return filepath.Join(filepath.Dir(sqlitePath), "worker-token")
}

// readWorkerTokenFile reads a previously-persisted long-term token.
// Returns the trimmed token plaintext or the underlying os error
// (callers use os.IsNotExist to distinguish "first-boot" from real
// IO failures).
func readWorkerTokenFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// writeWorkerTokenFile atomically writes the token plaintext to path
// with mode 0600. Uses tmp-then-rename so a crash mid-write leaves
// either the old contents or the new — never half a token.
func writeWorkerTokenFile(path, token string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(token+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
