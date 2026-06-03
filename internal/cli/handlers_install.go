// handlers_install.go — `agent-center install` subcommand family.
// v2.4-D-A1 (task #35): skeleton + version detection + branch routing.
// A2 (task #36) implements the systemd / launchd install path; A5 (#39)
// implements the upgrade flow. A1 ships:
//
//   - `agent-center install center [--prefix=...] [--user-mode]` — installs
//     server on local machine (host A).
//   - `agent-center install worker --bootstrap=... --token=... [...]` —
//     installs worker daemon on local machine (host B).
//
// Both commands detect existing installs and branch:
//
//   - **Fresh**: no prior install at prefix → A2 will write binaries, units,
//     start service.
//   - **SameVersion**: install dir for this exact version already exists →
//     idempotent no-op, exit 0 with "already installed" message.
//   - **Upgrade**: different version exists → A5 will atomic-swap symlink +
//     restart service + rollback on failure.
//
// A1 implements the routing + clear error messages. The real install /
// upgrade work is stubbed (returns "not implemented in A1; coming in A2/A5")
// so the CLI shape is observable + testable before the implementation lands.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// InstallState reflects the outcome of detectExistingInstall.
type InstallState int

const (
	InstallStateUnknown InstallState = iota
	// InstallStateFresh = no prior install at prefix; A2 will write
	// everything fresh.
	InstallStateFresh
	// InstallStateSameVersion = `<prefix>/current` already points at
	// this binary's version; the install is idempotent.
	InstallStateSameVersion
	// InstallStateUpgrade = `<prefix>/current` exists but points at a
	// different version; A5 will perform symlink swap.
	InstallStateUpgrade
)

// String for diagnostics.
func (s InstallState) String() string {
	switch s {
	case InstallStateFresh:
		return "fresh"
	case InstallStateSameVersion:
		return "same-version"
	case InstallStateUpgrade:
		return "upgrade"
	default:
		return "unknown"
	}
}

// InstallCommand is the parent group; printing help when invoked
// without a subcommand. Per the v2.4 first-mile spec, the operator
// types `agent-center install center` or `... install worker`.
func InstallCommand() *Command {
	return &Command{
		Name:    "install",
		Group:   "Admin",
		Summary: "Install or upgrade agent-center (center or worker) on this machine",
		LongHelp: "Use subcommands:\n" +
			"  agent-center install center [--prefix=...] [--user-mode]\n" +
			"      Install or upgrade the server on this host.\n" +
			"  agent-center install worker --bootstrap=<url> --token=<token>\n" +
			"      Install or upgrade a worker daemon that joins the server.\n",
	}
}

// InstallCenterCommand is the `install center` leaf — the operator's
// one command for "spin up an agent-center server on this machine".
func InstallCenterCommand() *Command {
	return &Command{
		Name:    "center",
		Summary: "Install the agent-center server on this machine (idempotent + upgrade-aware)",
		Flags:   installCenterHandler,
	}
}

// InstallWorkerCommand is the `install worker` leaf — the operator's
// one command for "join this machine to the cluster as a worker".
func InstallWorkerCommand() *Command {
	return &Command{
		Name:    "worker",
		Summary: "Install the worker daemon on this machine, enrolling against a running center (idempotent + upgrade-aware)",
		Flags:   installWorkerHandler,
	}
}

// installCenterHandler binds flags + dispatches the install center
// action by InstallState.
func installCenterHandler(fs *flag.FlagSet) Handler {
	prefix := fs.String("prefix", "", "install prefix (default: ~/.agent-center on Mac + Linux user mode; /opt/agent-center on Linux system mode)")
	userMode := fs.Bool("user-mode", isMacRuntime(), "install under the current user (no sudo). Mac default true, linux default false (use system mode + sudo).")
	port := fs.Int("port", 7100, "Web Console listen port (loopback only)")
	// v2.4-D-F4 fix: default-on so the Web Console's Add Worker Modal
	// can mint a usable install command without requiring the operator
	// to know they have to enable TCP. Pass --tcp-listen="" to disable
	// (unix-socket-only deployments).
	tcpListen := fs.String("tcp-listen", "0.0.0.0:7300", "admin TCP listener address (e.g. 0.0.0.0:7300). Pass --tcp-listen= to disable (unix-only).")
	service := fs.Bool("service", false, "register + start a launchd/systemd background service that auto-starts on boot. Default: foreground — install only drops files + config; run `agent-center server --config=<path>` yourself (logs to stdout).")
	bootstrapPublicURL := fs.String("bootstrap-public-url", "", "externally-reachable admin host:port for the Web Console Add Worker command (v2.7 #200), independent of --tcp-listen. Set when remote workers must dial a public DNS/LB/NAT address. Empty = derive from the bind address.")
	dryRun := fs.Bool("dry-run", false, "print planned actions without mutating state")

	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		_ = args // install center takes no positional args

		resolvedPrefix := *prefix
		if resolvedPrefix == "" {
			resolvedPrefix = defaultInstallPrefix(*userMode)
		}
		version := installerVersion()

		state, currentVersion, derr := detectExistingInstall(resolvedPrefix, version)
		if derr != nil {
			return PrintError(errw, FormatText, "install_detect_failed", derr.Error(), ExitBusinessError)
		}

		fmt.Fprintf(out, "agent-center install center:\n")
		fmt.Fprintf(out, "  prefix:        %s\n", resolvedPrefix)
		fmt.Fprintf(out, "  user-mode:     %v\n", *userMode)
		fmt.Fprintf(out, "  web port:      %d\n", *port)
		if *tcpListen != "" {
			fmt.Fprintf(out, "  admin tcp:     %s\n", *tcpListen)
		}
		fmt.Fprintf(out, "  state:         %s", state)
		if currentVersion != "" {
			fmt.Fprintf(out, " (current=%s, this=%s)", currentVersion, version)
		}
		fmt.Fprintln(out)

		if *dryRun {
			fmt.Fprintln(out, "[dry-run] no changes made")
			return ExitOK
		}

		switch state {
		case InstallStateSameVersion:
			fmt.Fprintf(out, "✓ AgentCenter %s already installed at %s, no changes\n", version, resolvedPrefix)
			return ExitOK
		case InstallStateFresh:
			return installCenterFresh(out, errw, installContext{
				Prefix:             resolvedPrefix,
				UserMode:           *userMode,
				Port:               *port,
				TCPListen:          *tcpListen,
				BootstrapPublicURL: strings.TrimSpace(*bootstrapPublicURL),
				Version:            version,
				Service:            *service,
			})
		case InstallStateUpgrade:
			return installCenterUpgrade(out, errw, installContext{
				Prefix:         resolvedPrefix,
				UserMode:       *userMode,
				Port:           *port,
				TCPListen:      *tcpListen,
				Version:        version,
				CurrentVersion: currentVersion,
				Service:        *service,
			})
		default:
			return PrintError(errw, FormatText, "install_state_unknown",
				"could not classify existing install — try --prefix=<empty-dir> or remove the old install manually",
				ExitBusinessError)
		}
	}
}

// installWorkerHandler binds flags + dispatches the install worker
// action by InstallState.
func installWorkerHandler(fs *flag.FlagSet) Handler {
	prefix := fs.String("prefix", "", "install prefix (default: same defaults as install center)")
	userMode := fs.Bool("user-mode", isMacRuntime(), "install under the current user (no sudo)")
	bootstrap := fs.String("bootstrap", "", "admin endpoint URL the worker dials, e.g. tcp://host:7300 or unix:/path/admin.sock (required)")
	token := fs.String("token", "", "one-time enrollment bearer token from the Web Console / mint-enroll endpoint (required)")
	// v2.4-D-F4 X1 fix: explicit fingerprint flag — workers MUST pin
	// the server's TLS cert (v2.3-7b client pinning). Required when
	// --bootstrap is tcp://; optional for unix:/ sockets.
	fingerprint := fs.String("server-fingerprint", "", "pinned server TLS cert sha256 fingerprint (sha256:HH:HH:...); required when --bootstrap is tcp://")
	workerID := fs.String("worker-id", "", "worker identifier (REQUIRED; no hostname default). Generated by the Web Console Add Worker Modal — copy it from the generated command.")
	workerName := fs.String("worker-name", "", "operator-facing friendly label (v2.4-D-X1 @oopslink); typed in the Web Console Modal. Empty falls back to worker-id.")
	service := fs.Bool("service", false, "register + start a launchd/systemd background service that auto-starts on boot. Default: foreground — install only drops files + config; run `agent-center worker run ...` yourself (logs to stdout).")
	dryRun := fs.Bool("dry-run", false, "print planned actions without mutating state")

	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		_ = args
		if strings.TrimSpace(*bootstrap) == "" {
			return PrintError(errw, FormatText, "install_worker_missing_bootstrap",
				"--bootstrap is required (e.g. tcp://<fp>@host:7300 from the Web Console enroll Modal)",
				ExitUsage)
		}
		if strings.TrimSpace(*token) == "" {
			return PrintError(errw, FormatText, "install_worker_missing_token",
				"--token is required (mint via Web Console / agent-center admintoken mint-enroll)",
				ExitUsage)
		}
		// Pinning gate: tcp:// MUST come with a fingerprint per v2.3-7b.
		if strings.HasPrefix(strings.TrimSpace(*bootstrap), "tcp://") && strings.TrimSpace(*fingerprint) == "" {
			return PrintError(errw, FormatText, "install_worker_missing_fingerprint",
				"--server-fingerprint is required when --bootstrap is tcp:// (copy from the Web Console enroll Modal)",
				ExitUsage)
		}

		// v2.7 #171 (@oopslink): --worker-id is REQUIRED — no hostname
		// fallback. Two bare `install worker` calls on one machine used
		// to both default to the same hostname → same prefix + launchd
		// label → the second silently clobbered the first. Forcing an
		// explicit id (the Web Console mints a unique one per worker)
		// makes multi-worker-per-machine collision-free by construction.
		resolvedWorkerID := strings.TrimSpace(*workerID)
		if resolvedWorkerID == "" {
			return PrintError(errw, FormatText, "install_worker_missing_id",
				"--worker-id is required (no hostname default). Copy it from the Web Console Add Worker command, which mints a unique id per worker.",
				ExitUsage)
		}
		resolvedPrefix := *prefix
		if resolvedPrefix == "" {
			resolvedPrefix = defaultWorkerInstallPrefix(*userMode, resolvedWorkerID)
		}
		version := installerVersion()

		state, currentVersion, derr := detectExistingInstall(resolvedPrefix, version)
		if derr != nil {
			return PrintError(errw, FormatText, "install_detect_failed", derr.Error(), ExitBusinessError)
		}

		fmt.Fprintf(out, "agent-center install worker:\n")
		fmt.Fprintf(out, "  prefix:        %s\n", resolvedPrefix)
		fmt.Fprintf(out, "  user-mode:     %v\n", *userMode)
		fmt.Fprintf(out, "  worker-id:     %s\n", resolvedWorkerID)
		fmt.Fprintf(out, "  bootstrap:     %s\n", *bootstrap)
		fmt.Fprintf(out, "  state:         %s", state)
		if currentVersion != "" {
			fmt.Fprintf(out, " (current=%s, this=%s)", currentVersion, version)
		}
		fmt.Fprintln(out)

		if *dryRun {
			fmt.Fprintln(out, "[dry-run] no changes made")
			return ExitOK
		}

		switch state {
		case InstallStateSameVersion:
			fmt.Fprintf(out, "✓ Worker %s already installed at %s, no changes\n", version, resolvedPrefix)
			return ExitOK
		case InstallStateFresh:
			return installWorkerFresh(out, errw, installContext{
				Prefix:      resolvedPrefix,
				UserMode:    *userMode,
				WorkerID:    resolvedWorkerID,
				WorkerName:  strings.TrimSpace(*workerName),
				Bootstrap:   *bootstrap,
				Token:       *token,
				Fingerprint: strings.TrimSpace(*fingerprint),
				Version:     version,
				Service:     *service,
			})
		case InstallStateUpgrade:
			return installWorkerUpgrade(out, errw, installContext{
				Prefix:         resolvedPrefix,
				UserMode:       *userMode,
				WorkerID:       resolvedWorkerID,
				WorkerName:     strings.TrimSpace(*workerName),
				Bootstrap:      *bootstrap,
				Token:          *token,
				Fingerprint:    strings.TrimSpace(*fingerprint),
				Version:        version,
				CurrentVersion: currentVersion,
				Service:        *service,
			})
		default:
			return PrintError(errw, FormatText, "install_state_unknown",
				"could not classify existing install — try --prefix=<empty-dir> or remove the old install manually",
				ExitBusinessError)
		}
	}
}

// installContext bundles the resolved flag values for the install/
// upgrade implementations (filled in by A2 / A5).
type installContext struct {
	Prefix    string
	UserMode  bool
	Port      int
	TCPListen string
	// BootstrapPublicURL (v2.7 #200, center only) — externally-reachable admin
	// host:port the Web Console Add Worker command advertises, written into the
	// center config. Empty → enroll wiring derives the host from admin_tcp_listen.
	BootstrapPublicURL string
	WorkerID           string
	WorkerName         string
	Bootstrap          string
	Token              string
	Fingerprint        string
	Version            string
	CurrentVersion     string
	// Service, when true, registers + starts a launchd/systemd background service
	// (the pre-v2.7 behavior). Default false (v2.7 #199): install only drops files
	// + config; the operator runs the foreground command (server / worker run,
	// logs to stdout). The unit file's presence on disk is the source of truth for
	// service-vs-foreground in uninstall/upgrade.
	Service bool
}

// --- A1 stubs: real impl in A2/A5 -------------------------------------

func installCenterFresh(out, errw io.Writer, ic installContext) ExitCode {
	layout := newInstallLayout(ic.Prefix, ic.Version)
	home, _ := os.UserHomeDir()
	sp, err := platformPaths(runtimeOS(), ic.UserMode, home)
	if err != nil {
		return PrintError(errw, FormatText, "install_platform_unsupported", err.Error(), ExitBusinessError)
	}
	// v2.4-D-A6: pre-flight port check for the Web Console port.
	webAddr := fmt.Sprintf("127.0.0.1:%d", ic.Port)
	if err := preflightPortAvailable(webAddr); err != nil {
		return PrintError(errw, FormatText, "install_port_in_use",
			renderInstallError(err, installErrorContext{Operation: "bind_port", Port: webAddr}),
			ExitBusinessError)
	}
	if ic.TCPListen != "" {
		if err := preflightPortAvailable(ic.TCPListen); err != nil {
			return PrintError(errw, FormatText, "install_port_in_use",
				renderInstallError(err, installErrorContext{Operation: "bind_port", Port: ic.TCPListen}),
				ExitBusinessError)
		}
	}

	if _, _, err := copyBinaries(layout); err != nil {
		return PrintError(errw, FormatText, "install_copy_binaries_failed",
			renderInstallError(err, installErrorContext{Operation: "write_binary", Path: layout.BinDir, Prefix: layout.Prefix}),
			ExitBusinessError)
	}
	if err := writeVersionFile(layout); err != nil {
		return PrintError(errw, FormatText, "install_write_version_failed", err.Error(), ExitBusinessError)
	}
	if err := writeCenterConfig(layout, ic.Port, ic.TCPListen, ic.BootstrapPublicURL); err != nil {
		return PrintError(errw, FormatText, "install_write_config_failed", err.Error(), ExitBusinessError)
	}
	if err := atomicSymlinkSwap(layout); err != nil {
		return PrintError(errw, FormatText, "install_symlink_swap_failed", err.Error(), ExitBusinessError)
	}
	if err := os.MkdirAll(layout.LogsDir, 0o755); err != nil {
		return PrintError(errw, FormatText, "install_mkdir_logs_failed", err.Error(), ExitBusinessError)
	}
	currentBin := filepath.Join(layout.CurrentBinDir, "agent-center")
	// v2.7 #199: only register + start a background service when --service is
	// given. Default is foreground — drop files + config, the operator runs
	// `server` themselves (logs to stdout). The unit file's presence is the
	// source of truth for service-vs-foreground in uninstall/upgrade.
	if ic.Service {
		unitBody := renderCenterServiceUnit(sp, currentBin, layout.ConfigPath, layout.LogsDir)
		if err := writeUnitFile(sp.CenterUnitPath, unitBody); err != nil {
			return PrintError(errw, FormatText, "install_write_unit_failed",
				renderInstallError(err, installErrorContext{Operation: "write_unit", Path: sp.CenterUnitPath}),
				ExitBusinessError)
		}
		// Activate service; failure prints commands for manual activation.
		if err := activateService(sp, sp.CenterServiceID, out, !installShouldActivate(sp)); err != nil {
			fmt.Fprintf(errw, "warning: service activation failed: %v\n", err)
			fmt.Fprintln(errw, "  Service unit written but not started. Run activation commands manually.")
		}
	}
	fmt.Fprintf(out, "\n✓ AgentCenter %s installed\n", ic.Version)
	fmt.Fprintf(out, "  prefix:    %s\n", layout.Prefix)
	if ic.Service {
		fmt.Fprintf(out, "  service:   %s (%s)\n", sp.CenterServiceID, sp.ServiceManager)
	}
	fmt.Fprintf(out, "  config:    %s\n", layout.ConfigPath)
	fmt.Fprintf(out, "  data:      %s\n", layout.DataDir)
	fmt.Fprintf(out, "  Web Console: http://127.0.0.1:%d/\n", ic.Port)
	if ic.Service {
		fmt.Fprintln(out, "  (next: open the URL above; first-time setup will mint the bootstrap admin token)")
	} else {
		fmt.Fprintf(out, "  run (foreground): %s server --config=%s\n", currentBin, layout.ConfigPath)
		fmt.Fprintln(out, "  (or re-run with --service to register a launchd/systemd background service)")
		fmt.Fprintln(out, "  (then open the Web Console URL; first-time setup mints the bootstrap admin token)")
	}
	return ExitOK
}

func installCenterUpgrade(out, errw io.Writer, ic installContext) ExitCode {
	layout := newInstallLayout(ic.Prefix, ic.Version)
	home, _ := os.UserHomeDir()
	sp, err := platformPaths(runtimeOS(), ic.UserMode, home)
	if err != nil {
		return PrintError(errw, FormatText, "install_platform_unsupported", err.Error(), ExitBusinessError)
	}
	fmt.Fprintf(out, "\nUpgrading center: %s → %s\n", ic.CurrentVersion, ic.Version)

	// Steps 1-3: copy binaries + write VERSION. Config + unit file NOT
	// rewritten on upgrade (preserve operator edits; symlink swap
	// makes <prefix>/current/bin/* refer to the new version).
	if _, _, err := copyBinaries(layout); err != nil {
		return PrintError(errw, FormatText, "install_copy_binaries_failed", err.Error(), ExitBusinessError)
	}
	if err := writeVersionFile(layout); err != nil {
		return PrintError(errw, FormatText, "install_write_version_failed", err.Error(), ExitBusinessError)
	}

	// v2.7 #199: a foreground install (no unit file) upgrades by swapping the
	// symlink only — there is no service to restart; the operator restarts their
	// foreground process. A service install (unit present) keeps the restart +
	// rollback path. Unit-file presence is the source of truth.
	if !unitFileExists(sp.CenterUnitPath) {
		if err := atomicSymlinkSwap(layout); err != nil {
			return PrintError(errw, FormatText, "install_symlink_swap_failed", err.Error(), ExitBusinessError)
		}
		fmt.Fprintf(out, "\n✓ Upgrade complete (foreground): %s → %s\n", ic.CurrentVersion, ic.Version)
		fmt.Fprintf(out, "  current:   %s → %s\n", layout.CurrentLink, layout.VersionedDir)
		fmt.Fprintln(out, "  (restart your foreground process to pick up the new binary; or use --service for a managed service)")
		return ExitOK
	}
	if err := upgradeService(out, errw, layout, sp, sp.CenterServiceID, centerHealthProbe(layout)); err != nil {
		// upgradeService returns post-rollback. The new versioned dir
		// stays on disk (rollback only swapped the symlink) — operator
		// can inspect it or `rm -rf` to retry from clean.
		return PrintError(errw, FormatText, "install_upgrade_failed", err.Error(), ExitBusinessError)
	}

	fmt.Fprintf(out, "\n✓ Upgrade complete: %s → %s\n", ic.CurrentVersion, ic.Version)
	fmt.Fprintf(out, "  service:   %s (%s)\n", sp.CenterServiceID, sp.ServiceManager)
	fmt.Fprintf(out, "  current:   %s → %s\n", layout.CurrentLink, layout.VersionedDir)
	fmt.Fprintf(out, "  Web Console: http://127.0.0.1:%d/\n", ic.Port)
	return ExitOK
}

// unitFileExists reports whether a service unit file exists on disk — the v2.7
// #199 source of truth for service-vs-foreground deployment (no extra state).
func unitFileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func installWorkerFresh(out, errw io.Writer, ic installContext) ExitCode {
	layout := newInstallLayout(ic.Prefix, ic.Version)
	home, _ := os.UserHomeDir()
	sp, err := platformPaths(runtimeOS(), ic.UserMode, home)
	if err != nil {
		return PrintError(errw, FormatText, "install_platform_unsupported", err.Error(), ExitBusinessError)
	}
	// v2.4-D-X1 fix (@oopslink ask): scope launchd label + unit path
	// by worker-id so multiple workers can coexist on one machine
	// (per-tenant testing / dev sandbox). When two installs reuse
	// the same --worker-id, the second is treated as an update of
	// the first by launchd, which is the intended re-enroll path.
	sp = applyWorkerIDToServicePaths(sp, ic.WorkerID)
	if _, _, err := copyBinaries(layout); err != nil {
		return PrintError(errw, FormatText, "install_copy_binaries_failed", err.Error(), ExitBusinessError)
	}
	if err := writeVersionFile(layout); err != nil {
		return PrintError(errw, FormatText, "install_write_version_failed", err.Error(), ExitBusinessError)
	}
	if err := writeWorkerConfig(layout); err != nil {
		return PrintError(errw, FormatText, "install_write_config_failed", err.Error(), ExitBusinessError)
	}
	if err := atomicSymlinkSwap(layout); err != nil {
		return PrintError(errw, FormatText, "install_symlink_swap_failed", err.Error(), ExitBusinessError)
	}
	if err := os.MkdirAll(layout.LogsDir, 0o755); err != nil {
		return PrintError(errw, FormatText, "install_mkdir_logs_failed", err.Error(), ExitBusinessError)
	}
	// v2.7 (b) cutover: the worker runs as the UNIFIED `agent-center` binary
	// (`agent-center worker run ...`), not the retired standalone
	// agent-center-worker-daemon — so its os.Executable() can route the
	// worker agent-supervisor / mcp-host subcommands it spawns.
	currentBin := filepath.Join(layout.CurrentBinDir, "agent-center")
	// A2: bootstrap URL is passed straight through; fingerprint is
	// embedded in the URL (tcp://<fp>@host:port). A3 (#37) will burn
	// the token on first enroll. Until then the worker daemon will
	// 401 on a stale token, which is the right behavior.
	// #175: pass an augmented PATH so the launchd/systemd-started daemon can
	// find user-installed agent CLIs (claude/codex/opencode) that live
	// outside the service manager's minimal default PATH.
	// v2.7 #199: only register + start a background service when --service is
	// given; default is foreground (drop files + config, operator runs
	// `worker run` themselves → stdout logs). Unit-file presence is the
	// service-vs-foreground source of truth for uninstall/upgrade.
	if ic.Service {
		unitBody := renderWorkerServiceUnit(sp, currentBin, layout.ConfigPath,
			ic.WorkerID, ic.WorkerName, ic.Bootstrap, ic.Token, ic.Fingerprint, layout.LogsDir,
			resolveWorkerPATH(home))
		if err := writeUnitFile(sp.WorkerUnitPath, unitBody); err != nil {
			return PrintError(errw, FormatText, "install_write_unit_failed", err.Error(), ExitBusinessError)
		}
		if err := activateService(sp, sp.WorkerServiceID, out, !installShouldActivate(sp)); err != nil {
			fmt.Fprintf(errw, "warning: service activation failed: %v\n", err)
			fmt.Fprintln(errw, "  Service unit written but not started. Run activation commands manually.")
		}
		// v2.4-D-X1 fix B9: wait for the daemon to actually enroll (or
		// hit a clear failure) before claiming the install succeeded.
		// Previously we declared ✓ installed before the daemon had even
		// started, hiding burned-token / fingerprint-mismatch / unreachable-
		// center failures from the operator. They had to hunt the launchd
		// stderr log themselves to find out the worker wasn't connected.
		if installShouldActivate(sp) && sp.ServiceManager == "launchd" {
			tokenFile := workerTokenFile(layout)
			logPath := launchdLogPath(layout.LogsDir, sp.WorkerServiceID, "err")
			if err := waitForWorkerEnroll(logPath, 30*time.Second, out); err != nil {
				fmt.Fprintln(errw, "")
				fmt.Fprintln(errw, renderWorkerEnrollFailure(err, ic, layout, sp, tokenFile, logPath))
				return ExitBusinessError
			}
		}
		fmt.Fprintf(out, "\n✓ AgentCenter worker %s installed + connected\n", ic.Version)
		fmt.Fprintf(out, "  worker-id: %s\n", ic.WorkerID)
		fmt.Fprintf(out, "  bootstrap: %s\n", ic.Bootstrap)
		fmt.Fprintf(out, "  service:   %s (%s)\n", sp.WorkerServiceID, sp.ServiceManager)
		fmt.Fprintf(out, "  config:    %s\n", layout.ConfigPath)
		fmt.Fprintln(out, "  Visible now in the Web Console Fleet view.")
		return ExitOK
	}
	fmt.Fprintf(out, "\n✓ AgentCenter worker %s installed\n", ic.Version)
	fmt.Fprintf(out, "  worker-id: %s\n", ic.WorkerID)
	fmt.Fprintf(out, "  bootstrap: %s\n", ic.Bootstrap)
	fmt.Fprintf(out, "  config:    %s\n", layout.ConfigPath)
	fmt.Fprintf(out, "  run (foreground): %s\n", workerRunCommand(currentBin, layout.ConfigPath, ic))
	fmt.Fprintln(out, "  (or re-run with --service to register a launchd/systemd background service)")
	fmt.Fprintln(out, "  (the worker appears in the Web Console Fleet view once it connects)")
	return ExitOK
}

// workerRunCommand renders the foreground `worker run` command line (v2.7 #199),
// mirroring the args the service unit would otherwise carry. v2.7 FINDING-P
// (#204): emit the friendly --bootstrap/--token spelling so the whole chain
// (Web Console Add-Worker → install worker → worker run) shares ONE vocabulary;
// `worker run` accepts these as aliases of --admin-target/--admin-token.
func workerRunCommand(bin, configPath string, ic installContext) string {
	cmd := fmt.Sprintf("%s worker run --config=%s --worker-id=%s --bootstrap=%s --token=%s",
		bin, configPath, ic.WorkerID, ic.Bootstrap, ic.Token)
	if ic.WorkerName != "" {
		cmd += " --worker-name=" + ic.WorkerName
	}
	if ic.Fingerprint != "" {
		cmd += " --server-fingerprint=" + ic.Fingerprint
	}
	return cmd
}

func installWorkerUpgrade(out, errw io.Writer, ic installContext) ExitCode {
	layout := newInstallLayout(ic.Prefix, ic.Version)
	home, _ := os.UserHomeDir()
	sp, err := platformPaths(runtimeOS(), ic.UserMode, home)
	if err != nil {
		return PrintError(errw, FormatText, "install_platform_unsupported", err.Error(), ExitBusinessError)
	}
	sp = applyWorkerIDToServicePaths(sp, ic.WorkerID)
	fmt.Fprintf(out, "\nUpgrading worker: %s → %s\n", ic.CurrentVersion, ic.Version)

	if _, _, err := copyBinaries(layout); err != nil {
		return PrintError(errw, FormatText, "install_copy_binaries_failed", err.Error(), ExitBusinessError)
	}
	if err := writeVersionFile(layout); err != nil {
		return PrintError(errw, FormatText, "install_write_version_failed", err.Error(), ExitBusinessError)
	}

	// v2.7 #199: foreground install (no unit) → swap symlink only; service
	// install (unit present) → restart + rollback path.
	if !unitFileExists(sp.WorkerUnitPath) {
		if err := atomicSymlinkSwap(layout); err != nil {
			return PrintError(errw, FormatText, "install_symlink_swap_failed", err.Error(), ExitBusinessError)
		}
		fmt.Fprintf(out, "\n✓ Upgrade complete (foreground): %s → %s\n", ic.CurrentVersion, ic.Version)
		fmt.Fprintf(out, "  worker-id: %s\n", ic.WorkerID)
		fmt.Fprintln(out, "  (restart your foreground `worker run` process to pick up the new binary; or use --service for a managed service)")
		return ExitOK
	}
	if err := upgradeService(out, errw, layout, sp, sp.WorkerServiceID, workerHealthProbe(sp, sp.WorkerServiceID)); err != nil {
		return PrintError(errw, FormatText, "install_upgrade_failed", err.Error(), ExitBusinessError)
	}

	fmt.Fprintf(out, "\n✓ Upgrade complete: %s → %s\n", ic.CurrentVersion, ic.Version)
	fmt.Fprintf(out, "  service:   %s (%s)\n", sp.WorkerServiceID, sp.ServiceManager)
	return ExitOK
}

// --- helpers ---------------------------------------------------------

// detectExistingInstall reads `<prefix>/current/VERSION` (a one-line
// text file containing the installed version string) to classify the
// install state. A missing prefix or missing VERSION file = Fresh.
//
// The VERSION file is written by A2's installer (for now, A1 returns
// Fresh on any layout we don't recognise, so the stub branches work).
func detectExistingInstall(prefix, thisVersion string) (InstallState, string, error) {
	if prefix == "" {
		return InstallStateUnknown, "", errors.New("detectExistingInstall: empty prefix")
	}
	currentLink := filepath.Join(prefix, "current")
	info, err := os.Lstat(currentLink)
	if err != nil {
		if os.IsNotExist(err) {
			return InstallStateFresh, "", nil
		}
		return InstallStateUnknown, "", fmt.Errorf("stat %s: %w", currentLink, err)
	}
	if info.Mode()&os.ModeSymlink == 0 && !info.IsDir() {
		// Something weird at <prefix>/current — bail.
		return InstallStateUnknown, "", fmt.Errorf("%s is neither a symlink nor a directory", currentLink)
	}
	versionFile := filepath.Join(currentLink, "VERSION")
	data, err := os.ReadFile(versionFile)
	if err != nil {
		if os.IsNotExist(err) {
			// Symlink/dir exists but no VERSION marker → treat as
			// unrecognised existing install, fail loudly.
			return InstallStateUnknown, "", fmt.Errorf("%s has no VERSION file — manual cleanup required", currentLink)
		}
		return InstallStateUnknown, "", fmt.Errorf("read %s: %w", versionFile, err)
	}
	currentVersion := strings.TrimSpace(string(data))
	if currentVersion == thisVersion {
		return InstallStateSameVersion, currentVersion, nil
	}
	return InstallStateUpgrade, currentVersion, nil
}

// defaultInstallPrefix picks the install prefix when --prefix is empty.
//
// v2.4.1 (@oopslink ask, #agent-center msg=68b04496): unified to
// `~/.agent-center` across Mac + Linux user-mode. The previous
// per-OS defaults (`~/Library/Application Support/agent-center` on
// Mac; `~/.local/share/agent-center` on Linux user-mode) scattered
// the install across three different conventions and were hard to
// find from a terminal — `~/.agent-center` is one short, predictable
// path. Hard break: no auto-migration from the old paths (v2.4.0
// only saw single-user dogfood); CHANGELOG documents the manual
// move recipe.
//
// Linux system-mode default stays at `/opt/agent-center` since that
// path is rooted (`~/.agent-center` resolves to root's home, which
// is wrong for a service running as a system daemon).
func defaultInstallPrefix(userMode bool) string {
	if runtime.GOOS == "linux" && !userMode {
		return "/opt/agent-center"
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agent-center")
}

// defaultWorkerInstallPrefix is the worker-equivalent of
// defaultInstallPrefix. v2.4-D-X1 multi-worker per machine:
// worker installs go under `<base>/workers/<id>/` so the center
// install at `<base>/{bin,etc,var,logs}/` and each worker at
// `<base>/workers/<id>/{bin,etc,var,logs}/` coexist cleanly under
// one tree. The user can still --prefix to a custom path.
func defaultWorkerInstallPrefix(userMode bool, workerID string) string {
	base := defaultInstallPrefix(userMode)
	return filepath.Join(base, "workers", sanitizeWorkerIDForLabel(workerID))
}

// isMacRuntime reports whether the current OS is macOS. Used to set the
// --user-mode default (Mac defaults to user-mode true because system-wide
// install requires the operator to manage /Library permissions manually).
func isMacRuntime() bool { return runtime.GOOS == "darwin" }

// installerVersion returns the binary's own build version (overridden
// at link time via -X main.buildVersion). Falls back to "dev" so the
// install command works in `go run` / unversioned builds.
func installerVersion() string {
	if buildVersion := installBuildVersion(); buildVersion != "" {
		return buildVersion
	}
	return "dev"
}

// installBuildVersion is a hook so tests can inject a version without
// touching the real build-time variable. Production reads the override
// via the binary's main.buildVersion through this seam.
var installBuildVersion = func() string {
	// In test builds this returns "". The real CLI binary calls
	// SetInstallBuildVersion(main.buildVersion) in main() (added in
	// v2.4-D-X1 fix so `install center` prints the real tag).
	return ""
}

// SetInstallBuildVersion lets the binary's main() thread the linker-
// injected buildVersion into the install command. Called only when
// buildVersion is non-empty and not the "dev" sentinel; the empty
// case stays "dev" for `go run` / unversioned builds. Tests don't
// call this — they mutate installBuildVersion directly with a
// restore-on-defer pattern.
func SetInstallBuildVersion(v string) {
	if v == "" || v == "dev" {
		return
	}
	bv := v
	installBuildVersion = func() string { return bv }
}

// ResolvedBuildVersion returns the linker-injected version string if
// main() called SetInstallBuildVersion, otherwise "dev". Mirrors
// installerVersion() but exported so other server-side surfaces
// (e.g. /api/health) can echo the same value the install command
// printed. v2.4-D-X1 fix B10.
func ResolvedBuildVersion() string {
	return installerVersion()
}

// workerTokenFile returns the on-disk path the worker daemon
// persists its long-term token at (v2.4-D B5 fix). Kept here so the
// install handler's failure renderer can name it without duplicating
// the path logic. The daemon main.go owns the actual read/write.
func workerTokenFile(layout installLayout) string {
	return filepath.Join(layout.DataDir, "worker-token")
}

// waitForWorkerEnroll tails the launchd stderr log file for the
// worker daemon, polling for the success marker the daemon prints
// after it has either loaded a persisted token or completed enroll.
// Returns nil on success, an error containing the tail of the log
// on failure or timeout.
//
// Why tail launchd's redirected stderr instead of e.g. asking the
// admin endpoint? Because the install command runs ON the worker
// machine and (for cross-host setups) has no direct path back to
// the center, and we want the answer locally rather than chase a
// network round-trip just to confirm a process started.
func waitForWorkerEnroll(logPath string, timeout time.Duration, out io.Writer) error {
	deadline := time.Now().Add(timeout)
	fmt.Fprintln(out, "  waiting for daemon to connect...")
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		body, _ := os.ReadFile(logPath)
		s := string(body)
		// Daemon prints one of these two as soon as it has a usable
		// bearer (see cmd/worker-daemon/main.go).
		if strings.Contains(s, "loaded long-term token") || strings.Contains(s, "enrolled + persisted long-term token") {
			return nil
		}
		// Hard failures the daemon prints before exiting.
		if strings.Contains(s, "enroll failed:") || strings.Contains(s, "[worker] fatal:") {
			return fmt.Errorf("worker daemon failed to enroll:\n%s", tailLines(s, 12))
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("worker daemon did not enroll within %s. Last log lines:\n%s", timeout, tailLines(s, 12))
		}
		<-ticker.C
	}
}

// tailLines returns the last n non-empty lines of s, indented with
// 4 spaces for embedding in CLI error output.
func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	var b strings.Builder
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		b.WriteString("    ")
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
}

// renderWorkerEnrollFailure produces the operator-facing failure
// message after a daemon-start watch failed. Includes a concrete
// "to retry" recipe (PD's ask in #agent-center:0c9f6bb7): the user
// shouldn't be left guessing which file to remove.
func renderWorkerEnrollFailure(err error, ic installContext, layout installLayout, sp servicePaths, tokenFile, logPath string) string {
	var b strings.Builder
	b.WriteString("Error: worker installed but daemon failed to come up\n\n")
	b.WriteString(err.Error())
	b.WriteString("\nWhat to try:\n")
	b.WriteString("  - Inspect the full launchd log at " + logPath + "\n")
	b.WriteString("  - Common causes:\n")
	b.WriteString("      • enroll token already used / expired (mint a new one from the Web Console)\n")
	b.WriteString("      • center unreachable (check --bootstrap host:port + firewall)\n")
	b.WriteString("      • fingerprint mismatch (cert rotated; copy the new fingerprint from the Web Console)\n")
	b.WriteString("\nTo retry from scratch:\n")
	// v2.7 #150: modern API (bootout), not the deprecated `launchctl unload`
	// which fails on Darwin 25.1.0+ (macOS 26) — keep operator guidance
	// consistent with the activate/teardown paths.
	b.WriteString("  launchctl bootout " + launchdGUIDomain() + " " + sp.WorkerUnitPath + "\n")
	b.WriteString("  rm -f " + tokenFile + "\n")
	b.WriteString("  # mint a fresh enroll token from the Web Console, then:\n")
	b.WriteString("  ./install worker --bootstrap=" + ic.Bootstrap + " --server-fingerprint=" + ic.Fingerprint + " --token=<NEW>\n")
	return b.String()
}
