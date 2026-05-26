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
	prefix := fs.String("prefix", "", "install prefix (default: /opt/agent-center linux system mode, ~/Library/Application Support/agent-center on Mac, ~/.local/share/agent-center on linux user mode)")
	userMode := fs.Bool("user-mode", isMacRuntime(), "install under the current user (no sudo). Mac default true, linux default false (use system mode + sudo).")
	port := fs.Int("port", 7100, "Web Console listen port (loopback only)")
	tcpListen := fs.String("tcp-listen", "", "admin TCP listener address (e.g. 0.0.0.0:7300). Empty = unix-only.")
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
				Prefix:    resolvedPrefix,
				UserMode:  *userMode,
				Port:      *port,
				TCPListen: *tcpListen,
				Version:   version,
			})
		case InstallStateUpgrade:
			return installCenterUpgrade(out, errw, installContext{
				Prefix:         resolvedPrefix,
				UserMode:       *userMode,
				Port:           *port,
				TCPListen:      *tcpListen,
				Version:        version,
				CurrentVersion: currentVersion,
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
	bootstrap := fs.String("bootstrap", "", "admin endpoint URL the worker dials, e.g. tcp://<fingerprint>@host:7300 or unix:/path/admin.sock (required)")
	token := fs.String("token", "", "one-time enrollment bearer token from the Web Console / mint-enroll endpoint (required)")
	workerID := fs.String("worker-id", "", "worker identifier; default = OS hostname")
	caps := fs.String("capabilities", "", "comma-separated agent capabilities advertised by this worker (e.g. claudecode,fakeagent)")
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

		resolvedPrefix := *prefix
		if resolvedPrefix == "" {
			resolvedPrefix = defaultInstallPrefix(*userMode)
		}
		resolvedWorkerID := strings.TrimSpace(*workerID)
		if resolvedWorkerID == "" {
			resolvedWorkerID, _ = os.Hostname()
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
		if *caps != "" {
			fmt.Fprintf(out, "  capabilities:  %s\n", *caps)
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
			fmt.Fprintf(out, "✓ Worker %s already installed at %s, no changes\n", version, resolvedPrefix)
			return ExitOK
		case InstallStateFresh:
			return installWorkerFresh(out, errw, installContext{
				Prefix:    resolvedPrefix,
				UserMode:  *userMode,
				WorkerID:  resolvedWorkerID,
				Bootstrap: *bootstrap,
				Token:     *token,
				Caps:      *caps,
				Version:   version,
			})
		case InstallStateUpgrade:
			return installWorkerUpgrade(out, errw, installContext{
				Prefix:         resolvedPrefix,
				UserMode:       *userMode,
				WorkerID:       resolvedWorkerID,
				Bootstrap:      *bootstrap,
				Token:          *token,
				Caps:           *caps,
				Version:        version,
				CurrentVersion: currentVersion,
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
	Prefix         string
	UserMode       bool
	Port           int
	TCPListen      string
	WorkerID       string
	Bootstrap      string
	Token          string
	Caps           string
	Version        string
	CurrentVersion string
}

// --- A1 stubs: real impl in A2/A5 -------------------------------------

func installCenterFresh(out, errw io.Writer, ic installContext) ExitCode {
	fmt.Fprintf(out, "[a1-stub] fresh center install → A2 (#36) will:\n")
	fmt.Fprintf(out, "  1. mkdir %s/versions/%s + copy binaries\n", ic.Prefix, ic.Version)
	fmt.Fprintf(out, "  2. write systemd unit / launchd plist\n")
	fmt.Fprintf(out, "  3. atomic ln -sf %s/versions/%s %s/current\n", ic.Prefix, ic.Version, ic.Prefix)
	fmt.Fprintf(out, "  4. start service\n")
	fmt.Fprintf(out, "  5. mint bootstrap admin token + print Web Console URL\n")
	return ExitNotImplemented
}

func installCenterUpgrade(out, errw io.Writer, ic installContext) ExitCode {
	fmt.Fprintf(out, "[a1-stub] upgrade center install → A5 (#39) will:\n")
	fmt.Fprintf(out, "  1. mkdir %s/versions/%s (current=%s)\n", ic.Prefix, ic.Version, ic.CurrentVersion)
	fmt.Fprintf(out, "  2. copy binaries; apply DB migrations\n")
	fmt.Fprintf(out, "  3. atomic swap %s/current → versions/%s + restart service\n", ic.Prefix, ic.Version)
	fmt.Fprintf(out, "  4. health probe; rollback on failure (swap back to versions/%s)\n", ic.CurrentVersion)
	return ExitNotImplemented
}

func installWorkerFresh(out, errw io.Writer, ic installContext) ExitCode {
	fmt.Fprintf(out, "[a1-stub] fresh worker install → A2 (#36) will:\n")
	fmt.Fprintf(out, "  1. mkdir %s/versions/%s + copy worker binary\n", ic.Prefix, ic.Version)
	fmt.Fprintf(out, "  2. write systemd unit / launchd plist with --admin-target=%s\n", ic.Bootstrap)
	fmt.Fprintf(out, "  3. enroll via one-time token (A3 #37)\n")
	fmt.Fprintf(out, "  4. start service\n")
	return ExitNotImplemented
}

func installWorkerUpgrade(out, errw io.Writer, ic installContext) ExitCode {
	fmt.Fprintf(out, "[a1-stub] upgrade worker install → A5 (#39) will:\n")
	fmt.Fprintf(out, "  1. mkdir %s/versions/%s (current=%s)\n", ic.Prefix, ic.Version, ic.CurrentVersion)
	fmt.Fprintf(out, "  2. atomic swap %s/current → versions/%s + restart\n", ic.Prefix, ic.Version)
	return ExitNotImplemented
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
func defaultInstallPrefix(userMode bool) string {
	if runtime.GOOS == "darwin" {
		// Mac: always user mode in v2.4 (launchd user agent).
		// Explicit user-mode=false on Mac falls back to user dir too,
		// because Mac system-wide install needs root + /Library/...
		// flow we explicitly defer.
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "agent-center")
	}
	if userMode {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "agent-center")
	}
	// Linux system mode default.
	return "/opt/agent-center"
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
	// In test builds this returns "". The real CLI binary sets
	// installBuildVersion to a closure capturing main.buildVersion in
	// init() (added in v2.4-D-A2).
	return ""
}
