package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/workerdaemon"
)

// WorkerRunCommand is the `worker run` daemon entry (v2.7 (b) cutover). The worker
// daemon now ships INSIDE the unified `agent-center` binary so its os.Executable()
// can route the `worker agent-supervisor` and `worker mcp-host` subcommands the
// daemon spawns (the spawn-bug fix; the retired standalone `agent-center-worker-
// daemon` was flag-only and could not route them).
//
// § 0.4 is honored by construction: the daemon talks to the center ONLY via the
// admin endpoint (AdminClient) and never opens the SQLite file — so this CLI
// subcommand holds no DB handle. The flag set is kept STRICTLY in parity with the
// (retiring) standalone binary so operator behavior and Tester runbooks are
// unchanged; the real bootstrap lives in workerdaemon.RunDaemon (single source,
// shared with the thin standalone wrapper).
func WorkerRunCommand() *Command {
	return &Command{
		Name:    "run",
		Summary: "Run the worker daemon (control-stream executor; v2.7 (b) unified binary)",
		LongHelp: "Runs the worker daemon in THIS unified agent-center binary so the daemon's " +
			"os.Executable() can route the worker agent-supervisor / mcp-host subcommands it " +
			"spawns. Talks to the center only over the admin endpoint (never opens SQLite, " +
			"§ 0.4). Enrolls (or loads the persisted long-term token), then runs the " +
			"control-stream execution path. Graceful drain on SIGINT/SIGTERM.",
		Flags: func(fs *flag.FlagSet) Handler {
			cfgPath := fs.String("config", "", "path to agent-center.yaml")
			workerID := fs.String("worker-id", "", "worker identity (required)")
			workerName := fs.String("worker-name", "",
				"operator-facing friendly label set at enroll time (v2.4-D-X1); blank defaults to worker-id server-side")
			fakeAgent := fs.String("fake-agent", "", "override path for the 'fakeagent' agent_cli (e2e tests)")
			pollInterval := fs.Duration("poll-interval", 1*time.Second, "queue poll interval")
			adminToken := fs.String("admin-token", "",
				"admin bearer token (required by v2.3-3a auth); falls back to AGENT_CENTER_ADMIN_TOKEN env")
			adminTarget := fs.String("admin-target", "",
				"admin endpoint, e.g. unix:/run/admin.sock or tcp://host:7300 (default: cfg.server.admin_socket_path)")
			// v2.7 FINDING-P (#204): accept the SAME friendly flag vocabulary as
			// `install worker` / the Web Console Add-Worker command so an operator can
			// copy-paste either way without `flag provided but not defined: -bootstrap`.
			// --bootstrap aliases --admin-target (the admin endpoint URL) and --token
			// aliases --admin-token (the bearer/enroll token); the legacy
			// --admin-target/--admin-token stay for back-compat. The friendly flag wins
			// when both are set (it is the one the operator copied from the UI).
			bootstrap := fs.String("bootstrap", "",
				"admin endpoint URL the worker dials, e.g. tcp://host:7300 (alias of --admin-target; matches `install worker`)")
			token := fs.String("token", "",
				"admin bearer / enroll token (alias of --admin-token; matches `install worker`)")
			serverFingerprint := fs.String("server-fingerprint", "",
				"sha256:HH:HH:... pinned server cert fingerprint (required with --admin-target=tcp://...); falls back to AGENT_CENTER_SERVER_FINGERPRINT env")
			skillsDir := fs.String("skills-dir", "",
				"directory containing worker-agent.md + extra skills (real-agent dispatch)")
			disableControlStream := fs.Bool("disable-control-stream", false,
				"force the pure-poll control path (the SSE down-push stream is default-on for v2.7; poll keeps the identical delivery contract)")
			return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
				// v2.7.1 #249: config.yaml is the single source of truth for the
				// worker's enrollment identity. Resolve each field flag-first,
				// falling back to the worker: section — so `worker run --config=<p>`
				// (the installed command) needs no secrets on the CLI, while an
				// explicit flag still overrides (back-compat + advanced use).
				cfgPath := resolveWorkerConfigPath(*cfgPath, *workerID)
				cfg, _ := config.Load(config.LoadOptions{Path: cfgPath}) // best-effort; RunDaemon re-loads + validates
				workerIDv := firstNonEmptyWorker(*workerID, cfg.Worker.WorkerID)
				if strings.TrimSpace(workerIDv) == "" {
					fmt.Fprintln(errw, "Error: worker run: worker_id is required (pass --worker-id or set worker.worker_id in --config)")
					return ExitUsage
				}
				logf := func(msg string) { fmt.Fprintf(errw, "[worker] %s\n", msg) }
				err := workerdaemon.RunDaemon(ctx, workerdaemon.RunOptions{
					ConfigPath:           cfgPath,
					WorkerID:             workerIDv,
					WorkerName:           firstNonEmptyWorker(*workerName, cfg.Worker.WorkerName),
					FakeAgent:            *fakeAgent,
					PollInterval:         *pollInterval,
					AdminToken:           firstNonEmptyWorker(coalesceWorkerFlag(*token, *adminToken), cfg.Worker.Token),
					AdminTarget:          firstNonEmptyWorker(coalesceWorkerFlag(*bootstrap, *adminTarget), cfg.Worker.Bootstrap),
					ServerFingerprint:    firstNonEmptyWorker(*serverFingerprint, cfg.Worker.ServerFingerprint),
					SkillsDir:            *skillsDir,
					DisableControlStream: *disableControlStream,
				}, logf)
				if err != nil {
					if workerdaemon.IsShutdownError(err) {
						logf(err.Error())
						return ExitOK
					}
					fmt.Fprintf(errw, "Error: worker run: %v\n", err)
					return ExitBusinessError
				}
				logf("shutdown complete")
				return ExitOK
			}
		},
	}
}

// resolveWorkerConfigPath mirrors the system-command config resolution (see
// loadConfigForCLI): prefer the subcommand --config, else fall back to the GLOBAL
// --config / AGENT_CENTER_CONFIG that BuildRouter captured in globalConfigPath.
//
// This is REQUIRED, not cosmetic: the unified CLI's global layer (extractConfigFlag
// + StripGlobalFlags) consumes/strips --config (the ONLY global-layer flag) BEFORE
// the subcommand FlagSet parses, so the subcommand's own --config is always empty in
// real routing. Without this fallback `worker run` ignores the operator config and
// silently uses the default install config (/var/lib) — diverging from the standalone
// daemon across the WHOLE config surface (sqlite/admin-socket/token path/...). Caught
// by Tester's runtime parity check (msg 601b01a3); flag-parity unit tests + the
// "both entrypoints call RunDaemon" structural argument did not cover this seam.
func resolveWorkerConfigPath(flagVal, workerID string) string {
	if v := strings.TrimSpace(flagVal); v != "" {
		return v
	}
	if g := GlobalConfigPath(); g != "" {
		return g
	}
	// v2.7 FINDING-O (#203): symmetric to #90's server-side discovery — bare
	// `worker run --worker-id=X` (no --config) discovers the worker-mode install
	// config (~/.agent-center/workers/<X>/etc/config.yaml) instead of falling back
	// to the system /var/lib defaults (where the long-term token / state would be
	// unwritable in user mode). Missing → "" → DefaultConfig (zero-install dev
	// fallback, same as #90; XDG second-tier fallback deferred to v2.8).
	if id := strings.TrimSpace(workerID); id != "" {
		p := filepath.Join(defaultWorkerInstallPrefix(true, id), "etc", "config.yaml")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// coalesceWorkerFlag picks the value for a `worker run` option that has two
// spellings (v2.7 FINDING-P #204): the friendly flag (--bootstrap / --token,
// matching `install worker` + the Web Console) and the legacy back-compat alias
// (--admin-target / --admin-token). The friendly value wins when both are set —
// it is the one the operator copy-pasted from the UI; otherwise the non-empty
// one is used. Empty/empty leaves the daemon's own defaults (config /
// AGENT_CENTER_ADMIN_TOKEN env) intact.
func coalesceWorkerFlag(friendly, legacy string) string {
	if v := strings.TrimSpace(friendly); v != "" {
		return v
	}
	return strings.TrimSpace(legacy)
}

// firstNonEmptyWorker returns the flag value when set, else the config value
// (v2.7.1 #249: flag overrides config, config is the fallback source of truth).
func firstNonEmptyWorker(flagVal, cfgVal string) string {
	if v := strings.TrimSpace(flagVal); v != "" {
		return v
	}
	return strings.TrimSpace(cfgVal)
}
