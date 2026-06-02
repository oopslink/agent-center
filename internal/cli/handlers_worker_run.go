package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

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
			capsFlag := fs.String("capabilities", "", "comma-separated capability list")
			adminToken := fs.String("admin-token", "",
				"admin bearer token (required by v2.3-3a auth); falls back to AGENT_CENTER_ADMIN_TOKEN env")
			adminTarget := fs.String("admin-target", "",
				"admin endpoint, e.g. unix:/run/admin.sock or tcp://host:7300 (default: cfg.server.admin_socket_path)")
			serverFingerprint := fs.String("server-fingerprint", "",
				"sha256:HH:HH:... pinned server cert fingerprint (required with --admin-target=tcp://...); falls back to AGENT_CENTER_SERVER_FINGERPRINT env")
			skillsDir := fs.String("skills-dir", "",
				"directory containing worker-agent.md + extra skills (real-agent dispatch)")
			disableControlStream := fs.Bool("disable-control-stream", false,
				"force the pure-poll control path (the SSE down-push stream is default-on for v2.7; poll keeps the identical delivery contract)")
			return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
				if strings.TrimSpace(*workerID) == "" {
					fmt.Fprintln(errw, "Error: worker run: --worker-id is required")
					return ExitUsage
				}
				logf := func(msg string) { fmt.Fprintf(errw, "[worker] %s\n", msg) }
				err := workerdaemon.RunDaemon(ctx, workerdaemon.RunOptions{
					ConfigPath:        resolveWorkerConfigPath(*cfgPath),
					WorkerID:          *workerID,
					WorkerName:        *workerName,
					FakeAgent:         *fakeAgent,
					PollInterval:      *pollInterval,
					CapabilitiesCSV:   *capsFlag,
					AdminToken:        *adminToken,
					AdminTarget:       *adminTarget,
					ServerFingerprint:    *serverFingerprint,
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
func resolveWorkerConfigPath(flagVal string) string {
	if v := strings.TrimSpace(flagVal); v != "" {
		return v
	}
	return GlobalConfigPath()
}
