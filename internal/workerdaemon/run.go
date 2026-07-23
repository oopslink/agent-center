package workerdaemon

// run.go — the SINGLE-SOURCE worker-daemon bootstrap (v2.7 (b) cutover). Both
// `agent-center worker run` (the unified-CLI entry) and cmd/worker-daemon (the
// thin standalone wrapper, retiring) parse their flags into RunOptions and call
// RunDaemon. Extracting the bootstrap here is what lets the daemon run under the
// UNIFIED `agent-center` binary: RunDaemon sets AgentControllerConfig.BinaryPath
// = os.Executable(), so when the process is `agent-center` it can route the
// `worker agent-supervisor` and `worker mcp-host` subcommands (the spawn-bug fix —
// the standalone `agent-center-worker-daemon` is flag-only and cannot).
//
// Per conventions § 0.4 the daemon never opens the SQLite file directly; all state
// goes through the center AppService via the admin endpoint (AdminClient). cfg is
// read only for paths (admin socket, token-file / agent-home location).

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/oopslink/agent-center/internal/admin/clienttransport"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/workforce"
)

// RunOptions carries the resolved worker-daemon launch parameters. The caller
// (CLI handler or standalone main) owns flag parsing and maps the values here.
type RunOptions struct {
	ConfigPath        string
	WorkerID          string
	WorkerName        string
	FakeAgent         string
	PollInterval      time.Duration
	AdminToken        string
	AdminTarget       string
	ServerFingerprint string
	SkillsDir         string
	// DisableControlStream forces the control loop onto the pure-poll path.
	// The SSE down-push stream is DEFAULT-ON for v2.7 (D5 slice-2); this is the
	// operator escape hatch. Poll keeps the identical delivery contract.
	DisableControlStream bool
	// AgentCenterVersion / WorkerVersion carry the binary's build identity
	// (T752 Worker Profile), threaded from the CLI build seams. Reported to the
	// center on every online so the Profile page shows real versions. Empty in
	// unversioned / `go run` builds — the Profile page then omits the field.
	AgentCenterVersion string
	WorkerVersion      string
}

// RunDaemon boots and runs the worker daemon until ctx is cancelled or a SIGINT/
// SIGTERM arrives, then stops the worker control loop. Agent-runtime processes are
// intentionally left running so the next worker incarnation can re-adopt them from
// the durable pid store; explicit StopAgent/reset remains the path that terminates
// an agent runtime. RunDaemon is the single source of truth for the daemon bootstrap
// (config → AdminClient → token enroll-or-load → Runtime + AgentController → run).
// Returns nil on clean shutdown; IsShutdownError reports the benign "shutdown grace
// exceeded" flavor.
//
// logf may be nil (defaults to a no-op).
func RunDaemon(ctx context.Context, opts RunOptions, logf func(string)) error {
	if logf == nil {
		logf = func(string) {}
	}
	if strings.TrimSpace(opts.WorkerID) == "" {
		return fmt.Errorf("worker daemon: worker-id is required")
	}

	cfg, err := config.Load(config.LoadOptions{Path: opts.ConfigPath})
	if err != nil {
		var b strings.Builder
		for _, r := range config.AsErrorList(err) {
			fmt.Fprintf(&b, "%s; ", r)
		}
		return fmt.Errorf("worker daemon: config: %s", strings.TrimSpace(b.String()))
	}

	// Resolve admin target: --admin-target overrides cfg.Server.AdminSocketPath.
	targetSpec := strings.TrimSpace(opts.AdminTarget)
	if targetSpec == "" {
		sock := strings.TrimSpace(cfg.Server.AdminSocketPath)
		if sock == "" {
			return fmt.Errorf("worker daemon: either --admin-target (e.g. unix:/path or tcp://host:port) or server.admin_socket_path is required")
		}
		targetSpec = "unix:" + sock
	}
	parsedTarget, err := clienttransport.ParseTarget(targetSpec)
	if err != nil {
		return fmt.Errorf("worker daemon: %w", err)
	}
	fingerprint := strings.TrimSpace(opts.ServerFingerprint)
	if fingerprint == "" {
		fingerprint = strings.TrimSpace(os.Getenv("AGENT_CENTER_SERVER_FINGERPRINT"))
	}
	token := strings.TrimSpace(opts.AdminToken)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("AGENT_CENTER_ADMIN_TOKEN"))
	}
	client, err := NewAdminClientFromTarget(parsedTarget, fingerprint, 30*time.Second)
	if err != nil {
		return fmt.Errorf("worker daemon: admin client: %w", err)
	}
	client = client.WithToken(token)

	// v2.7 #147: auto-discover installed agent CLIs on every online (no manual
	// --capabilities flag). The rich probe result feeds both the enroll seed
	// (names) and the authoritative capability report (rich, below); the
	// Runtime advertises the detected set.
	probed := ProbeAllAdapters(ctx, nil)
	caps := detectedCLINames(probed)

	// T752: gather the worker host + build identity once at startup; reported
	// alongside the capability upload below so the Worker Profile page shows
	// real host facts instead of "Coming in v2.9" placeholders.
	sysInfo := collectSystemInfo(opts.AgentCenterVersion, opts.WorkerVersion)

	// Long-term worker token persistence (v2.4-D-X1): load an existing token and
	// skip enroll, else enroll-with-exchange and persist the minted long-term token.
	tokenPath := workerTokenFilePath(cfg, opts.ConfigPath, opts.WorkerID)
	enrollNeeded := true
	if existing, rerr := readWorkerTokenFile(tokenPath); rerr == nil && existing != "" {
		client = client.WithToken(existing)
		token = existing
		enrollNeeded = false
		logf("loaded long-term token from " + tokenPath)
	} else if rerr != nil && !os.IsNotExist(rerr) {
		logf(fmt.Sprintf("warning: read %s: %v — will try enroll", tokenPath, rerr))
	}
	if enrollNeeded {
		ctxEnroll, cancelEnroll := context.WithTimeout(ctx, 30*time.Second)
		enrollResp, eerr := client.EnrollWithExchange(ctxEnroll, opts.WorkerID, opts.WorkerName, caps)
		cancelEnroll()
		if eerr != nil {
			return fmt.Errorf("worker daemon: enroll failed: %w", eerr)
		}
		if enrollResp.AdminTokenError != "" {
			return fmt.Errorf("worker daemon: enroll succeeded but server failed to mint long-term token: %s", enrollResp.AdminTokenError)
		}
		if enrollResp.AdminToken == "" {
			return fmt.Errorf("worker daemon: enroll succeeded but server returned no admin_token (older center? check version)")
		}
		if werr := writeWorkerTokenFile(tokenPath, enrollResp.AdminToken); werr != nil {
			return fmt.Errorf("worker daemon: failed to persist long-term token to %s: %w", tokenPath, werr)
		}
		client = client.WithToken(enrollResp.AdminToken)
		token = enrollResp.AdminToken
		logf("enrolled + persisted long-term token to " + tokenPath)
	}

	// v2.7 #147: report probed capabilities on EVERY online (including the
	// enroll-skipped path) so a CLI installed after first enroll is discovered.
	// Non-fatal: a failed report just means stale caps until the next online.
	{
		ctxRep, cancelRep := context.WithTimeout(ctx, 30*time.Second)
		if rerr := client.ReportCapabilities(ctxRep, opts.WorkerID, probed, sysInfo); rerr != nil {
			logf(fmt.Sprintf("warning: capability report failed (will retry next online): %v", rerr))
		} else {
			logf(fmt.Sprintf("reported %d probed capabilities", len(probed)))
		}
		cancelRep()
	}

	// Build Runtime config.
	overrides := map[string]string{}
	if strings.TrimSpace(opts.FakeAgent) != "" {
		overrides["fakeagent"] = opts.FakeAgent
	}
	pollInterval := opts.PollInterval
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	rtCfg := RuntimeConfig{
		WorkerID:          opts.WorkerID,
		Capabilities:      caps,
		PollInterval:      pollInterval,
		AgentCLIOverrides: overrides,
		Logger:            logf,
		// main already ran enroll-or-load above → the runtime loop must not re-enroll.
		SkipInitialEnroll: true,
		// #107 slice-2: the control-stream path is the unconditional execution
		// path. Always wire ControlClient so Run starts the control loop.
		ControlClient: client,
		// #108 D5 slice-2: STREAM-FIRST by default. The same *AdminClient serves
		// the SSE down-push (StreamCommands) — ride the same bearer/transport.
		// Poll (ControlClient) remains the always-available fallback. Operators
		// can opt out via --disable-control-stream.
		ControlStreamClient:  client,
		DisableControlStream: opts.DisableControlStream,
	}

	// T854 D6 §4.5 (Ship, piece ③): the worker is a launcher/controller — it launches one
	// `worker agent-runtime` process per agent and PROXIES control commands to them
	// (cursor-gated), instead of hosting N runtimes in-process. This is the SOLE path;
	// the pre-D6 in-process AgentController path was removed after the §6 real-deploy
	// acceptance validated the controller model.
	wctrl, werr := buildWorkerController(opts, targetSpec, token, fingerprint, client, logf)
	if werr != nil {
		return fmt.Errorf("worker daemon: controller: %w", werr)
	}
	reconcileControllerFromResumeState(ctx, wctrl, client, opts.WorkerID, logf)
	rtCfg.ControlHandler = controllerHandler{
		ctrl:     wctrl,
		reporter: client,
		homeBase: agentHomeBase(cfg, opts.ConfigPath, opts.WorkerID),
		poster:   client,
		log:      logf,
	}
	logf("worker running in controller mode (process-per-agent)")

	rt := NewRuntime(rtCfg, client)

	// Signal-aware context (SIGINT/SIGTERM → cancel worker control loop). Agent
	// runtime processes are independent units and survive for re-adoption.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case s := <-sigCh:
			logf(fmt.Sprintf("signal %s received; cancelling", s))
			cancel()
		case <-runCtx.Done():
		}
	}()

	logf(fmt.Sprintf("starting: worker_id=%s target=%s poll=%s overrides=%d",
		opts.WorkerID, targetSpec, pollInterval, len(overrides)))
	err = rt.Run(runCtx)
	logf("worker exiting; agent-runtime processes left running for next-worker adoption")
	return err
}

// IsShutdownError reports whether err is the benign "shutdown grace exceeded"
// flavor (a clean enough shutdown → exit 0) versus a fatal transport/config error.
func IsShutdownError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "shutdown grace exceeded")
}

// detectedCLINames returns the agent-CLI names that were detected during probe
// (v2.7 #147). Used to seed enroll + advertise the Runtime's executable set.
// Empty input yields nil (the enroll handler tolerates nil but not [""]).
func detectedCLINames(caps []workforce.Capability) []string {
	var out []string
	for _, c := range caps {
		if c.Detected {
			out = append(out, c.AgentCLI)
		}
	}
	return out
}

// workerStateDir resolves the directory the worker persists its OWN runtime state
// under (the long-term token + the per-agent home layout).
//
// v2.7 FINDING-Q (#205): the worker never opens SQLite (§ 0.4), so deriving its
// state dir from cfg.Server.SqlitePath is only correct when a config FILE actually
// provided that path:
//   - configFile loaded (installed worker / #203-discovered config / explicit
//     --config): use filepath.Dir(cfg.Server.SqlitePath) — the install's
//     user-writable data dir (<prefix>/var) — so state shares the install backup
//     boundary. UNCHANGED behavior.
//   - NO config file: cfg is the built-in DefaultConfig whose sqlite_path is the
//     SYSTEM /var/lib path (a server default, meaningless for a worker, and
//     UNWRITABLE in the #199 user-mode foreground run). Fall back to a
//     user-writable, worker-id-keyed dir that mirrors the install layout #203
//     probes (~/.agent-center/workers/<id>/var) instead of failing with
//     `mkdir /var/lib/agent-center: permission denied`.
//
// Returns "" only when there is no config file AND no resolvable HOME; callers then
// use a cwd-relative name (never the unwritable system default).
func workerStateDir(cfg config.Config, configPath, workerID string) string {
	if strings.TrimSpace(configPath) != "" {
		if sqlitePath := strings.TrimSpace(cfg.Server.SqlitePath); sqlitePath != "" {
			return filepath.Dir(sqlitePath)
		}
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		if id := sanitizeWorkerStateID(workerID); id != "" {
			return filepath.Join(home, ".agent-center", "workers", id, "var")
		}
	}
	return ""
}

// sanitizeWorkerStateID makes a worker-id safe as a single path segment for the
// FINDING-Q user-writable fallback dir (no separators / traversal). It need not
// byte-match the installer's label sanitizer: this path is only used when NO
// install config exists (so there is no pre-existing token to line up with) — it
// just has to be stable + user-writable.
func sanitizeWorkerStateID(workerID string) string {
	id := strings.TrimSpace(workerID)
	if id == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), ".")
	if out == "" {
		return ""
	}
	return out
}

// workerTokenFilePath returns the path the daemon persists its long-term admin
// token to (under the worker state dir; see workerStateDir for FINDING-Q #205).
func workerTokenFilePath(cfg config.Config, configPath, workerID string) string {
	if dir := workerStateDir(cfg, configPath, workerID); dir != "" {
		return filepath.Join(dir, "worker-token")
	}
	return "worker-token"
}

// agentHomeBase returns the per-agent layout root — the worker STATE DIR itself,
// under which agentPaths builds agents/<agent_id>/ (→ workers/<wid>/var/agents/
// <ULID>/).
//
// v2.7 #209: dropped the redundant "agent-homes" wrapper that used to sit between
// var/ and agents/. var/'s only child was agent-homes/, whose only child was
// agents/ — a meaningless nesting level accumulated over time (same cleanup
// spirit as #179's double-workers/<wid> dedup). Fresh-install only (no migration;
// v2.6→v2.7 requires reinstall).
//
// dir resolution is workerStateDir (config → <sqlite_dir>=worker var; no-config →
// user-writable ~/.agent-center/workers/<id>/var, FINDING-Q #205). Falls back to
// "." (cwd) only when neither a config sqlite_path nor HOME exists, so
// agentPaths' non-empty-base guard still holds → agents/<id> resolves cwd-relative.
func agentHomeBase(cfg config.Config, configPath, workerID string) string {
	if dir := workerStateDir(cfg, configPath, workerID); dir != "" {
		return dir
	}
	return "."
}

// readWorkerTokenFile reads a previously-persisted long-term token (trimmed).
// Callers use os.IsNotExist on the error to distinguish first-boot from IO failure.
func readWorkerTokenFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// writeWorkerTokenFile atomically writes the token (mode 0600, tmp-then-rename so
// a crash mid-write never leaves half a token).
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
