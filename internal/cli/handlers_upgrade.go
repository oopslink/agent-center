// handlers_upgrade.go — `agent-center upgrade center|worker`.
//
// v2.5.2 patch (@oopslink ask in #agent-center msg=8e5ea457). The
// upgrade path itself was already wired in v2.4-D-A5 (atomic symlink
// swap + health probe + auto-rollback under `install center` auto-
// detect); this file just exposes it as an explicit subcommand so
// operators can say "I want to upgrade" out loud instead of relying
// on the install handler's silent fresh-vs-upgrade branch.
//
// Behaviour difference from `install center`:
//   - Fresh prefix     → `install` walks the fresh path.
//                         `upgrade` rejects with "no existing install
//                         at <prefix>; run `install center` first".
//   - Same version     → both walk the idempotent no-op path.
//   - Different ver    → both walk the atomic-swap upgrade path.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
)

// UpgradeCommand is the parent group for `upgrade center|worker`.
func UpgradeCommand() *Command {
	return &Command{
		Name:    "upgrade",
		Group:   "Admin",
		Summary: "Upgrade an existing agent-center install (center or worker) to the current binary's version",
		LongHelp: "Use subcommands:\n" +
			"  agent-center upgrade center [--prefix=...] [--user-mode]\n" +
			"      Upgrade the server install at <prefix> to this binary's\n" +
			"      version. Refuses with an error if no install exists\n" +
			"      (use `install center` for fresh installs).\n" +
			"  agent-center upgrade worker --worker-id=<id> [--bootstrap=...] [--token=...] [...]\n" +
			"      Upgrade a worker install at <prefix>/workers/<id>/.\n",
	}
}

// UpgradeCenterCommand is the explicit upgrade-only entry point for
// the center. Shares the install-center flag surface to keep
// muscle memory + the existing flag docs unchanged.
func UpgradeCenterCommand() *Command {
	return &Command{
		Name:    "center",
		Summary: "Upgrade an existing agent-center server install (fails if no install present)",
		Flags:   upgradeCenterHandler,
	}
}

// UpgradeWorkerCommand mirrors UpgradeCenterCommand for the worker
// install. Requires --worker-id so the right worker subtree is
// targeted on multi-worker hosts.
func UpgradeWorkerCommand() *Command {
	return &Command{
		Name:    "worker",
		Summary: "Upgrade an existing worker install (fails if no install present)",
		Flags:   upgradeWorkerHandler,
	}
}

func upgradeCenterHandler(fs *flag.FlagSet) Handler {
	prefix := fs.String("prefix", "", "install prefix (default: same as install center)")
	userMode := fs.Bool("user-mode", isMacRuntime(), "user-mode service paths (default: same as install center)")
	port := fs.Int("port", 7100, "Web Console listen port (reserved; existing config is preserved on upgrade)")
	tcpListen := fs.String("tcp-listen", "0.0.0.0:7300", "admin TCP listener address (reserved; existing config preserved)")
	dryRun := fs.Bool("dry-run", false, "print planned actions without mutating state")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		_ = args
		_ = ctx
		resolved := *prefix
		if resolved == "" {
			resolved = defaultInstallPrefix(*userMode)
		}
		version := installerVersion()
		state, currentVersion, derr := detectExistingInstall(resolved, version)
		if derr != nil {
			return PrintError(errw, FormatText, "install_detect_failed", derr.Error(), ExitBusinessError)
		}
		if err := requireUpgradeState(state, resolved, errw); err != nil {
			return ExitBusinessError
		}
		fmt.Fprintf(out, "agent-center upgrade center:\n")
		fmt.Fprintf(out, "  prefix:    %s\n", resolved)
		fmt.Fprintf(out, "  state:     %s (current=%s, this=%s)\n", state, currentVersion, version)
		if *dryRun {
			fmt.Fprintln(out, "[dry-run] no changes made")
			return ExitOK
		}
		if state == InstallStateSameVersion {
			fmt.Fprintf(out, "✓ AgentCenter %s already installed at %s, no changes\n", version, resolved)
			return ExitOK
		}
		return installCenterUpgrade(out, errw, installContext{
			Prefix:         resolved,
			UserMode:       *userMode,
			Port:           *port,
			TCPListen:      *tcpListen,
			Version:        version,
			CurrentVersion: currentVersion,
		})
	}
}

func upgradeWorkerHandler(fs *flag.FlagSet) Handler {
	prefix := fs.String("prefix", "", "install prefix (default: ~/.agent-center/workers/<worker-id>/)")
	userMode := fs.Bool("user-mode", isMacRuntime(), "user-mode service paths")
	workerID := fs.String("worker-id", "", "worker identifier to upgrade (required)")
	bootstrap := fs.String("bootstrap", "", "admin endpoint URL (preserved across upgrade; only honoured on Fresh, which upgrade refuses)")
	token := fs.String("token", "", "enroll token (preserved across upgrade)")
	fingerprint := fs.String("server-fingerprint", "", "pinned server fingerprint (preserved across upgrade)")
	caps := fs.String("capabilities", "", "comma-separated capabilities (preserved across upgrade)")
	dryRun := fs.Bool("dry-run", false, "print planned actions without mutating state")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		_ = args
		_ = ctx
		if *workerID == "" {
			return PrintError(errw, FormatText, "upgrade_worker_missing_id",
				"--worker-id is required (the id you passed to `install worker`)",
				ExitUsage)
		}
		resolved := *prefix
		if resolved == "" {
			resolved = defaultWorkerInstallPrefix(*userMode, *workerID)
		}
		version := installerVersion()
		state, currentVersion, derr := detectExistingInstall(resolved, version)
		if derr != nil {
			return PrintError(errw, FormatText, "install_detect_failed", derr.Error(), ExitBusinessError)
		}
		if err := requireUpgradeState(state, resolved, errw); err != nil {
			return ExitBusinessError
		}
		fmt.Fprintf(out, "agent-center upgrade worker:\n")
		fmt.Fprintf(out, "  prefix:    %s\n", resolved)
		fmt.Fprintf(out, "  worker-id: %s\n", *workerID)
		fmt.Fprintf(out, "  state:     %s (current=%s, this=%s)\n", state, currentVersion, version)
		if *dryRun {
			fmt.Fprintln(out, "[dry-run] no changes made")
			return ExitOK
		}
		if state == InstallStateSameVersion {
			fmt.Fprintf(out, "✓ Worker %s already installed at %s, no changes\n", version, resolved)
			return ExitOK
		}
		return installWorkerUpgrade(out, errw, installContext{
			Prefix:         resolved,
			UserMode:       *userMode,
			WorkerID:       *workerID,
			Bootstrap:      *bootstrap,
			Token:          *token,
			Fingerprint:    *fingerprint,
			Caps:           *caps,
			Version:        version,
			CurrentVersion: currentVersion,
		})
	}
}

// requireUpgradeState refuses the Fresh path. SameVersion + Upgrade
// are both fine — SameVersion turns into a no-op message at the call
// site, Upgrade walks the real swap.
func requireUpgradeState(state InstallState, prefix string, errw io.Writer) error {
	if state != InstallStateFresh {
		return nil
	}
	PrintError(errw, FormatText, "upgrade_no_install",
		fmt.Sprintf("no existing install at %s — run `install center` first for fresh installs", prefix),
		ExitBusinessError)
	return os.ErrNotExist
}
