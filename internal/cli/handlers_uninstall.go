// handlers_uninstall.go — `agent-center uninstall center|worker`.
//
// v2.5.1 patch (#agent-center:5f6288e6, @oopslink ask msg=74fb3fa6).
// Inverts `install center|worker`: stop + unload the service unit,
// remove the install artefacts, leave the operator's data alone by
// default. `--purge` opts in to wiping `var/` + `logs/` and the
// install prefix itself.
//
// Default-preserve rationale: var/ holds the sqlite database +
// master_key + worker-token + bootstrap_token — wiping those by
// accident is hard to undo. The expected reinstall-on-preserved-var
// path (uninstall → reinstall same prefix → existing data is reused;
// see ensureMasterKeyFile + sqlite Open auto-migration) is
// verified end-to-end in v2.5.1.
package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// UninstallCommand is the parent group for `uninstall center|worker`.
func UninstallCommand() *Command {
	return &Command{
		Name:    "uninstall",
		Group:   "Admin",
		Summary: "Remove agent-center (center or worker) from this machine",
		LongHelp: "Use subcommands:\n" +
			"  agent-center uninstall center [--prefix=...] [--purge] [--yes]\n" +
			"      Stop + unload the center service, remove install artefacts.\n" +
			"      Preserves var/ + logs/ unless --purge.\n" +
			"  agent-center uninstall worker --worker-id=<id> [--prefix=...] [--purge] [--yes]\n" +
			"      Same, scoped to a single worker subtree.\n",
	}
}

// UninstallCenterCommand removes a center install. Mirrors the
// install center flag surface so operators don't have to think about
// where things landed.
func UninstallCenterCommand() *Command {
	return &Command{
		Name:    "center",
		Summary: "Uninstall the agent-center server from this machine (preserves data by default)",
		Flags:   uninstallCenterHandler,
	}
}

// UninstallWorkerCommand removes a worker install. --worker-id is
// required since multi-worker installs share a parent prefix.
func UninstallWorkerCommand() *Command {
	return &Command{
		Name:    "worker",
		Summary: "Uninstall a worker daemon from this machine (preserves data by default)",
		Flags:   uninstallWorkerHandler,
	}
}

func uninstallCenterHandler(fs *flag.FlagSet) Handler {
	prefix := fs.String("prefix", "", "install prefix (default: same as install center)")
	userMode := fs.Bool("user-mode", isMacRuntime(), "if true, target user-mode service paths (default: same as install center)")
	purge := fs.Bool("purge", false, "ALSO delete the data directory (var/) + logs/ + the whole prefix. NOT REVERSIBLE.")
	yes := fs.Bool("yes", false, "skip the interactive confirm prompt when --purge is set")
	dryRun := fs.Bool("dry-run", false, "print planned actions without mutating state")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		_ = args
		resolved := *prefix
		if resolved == "" {
			resolved = defaultInstallPrefix(*userMode)
		}
		home, _ := os.UserHomeDir()
		sp, perr := platformPaths(runtimeOS(), *userMode, home)
		if perr != nil {
			return PrintError(errw, FormatText, "install_platform_unsupported", perr.Error(), ExitBusinessError)
		}
		layout := newInstallLayout(resolved, "")
		plan := newUninstallPlan(layout, *purge)
		plan.addServiceTeardown(sp, sp.CenterUnitPath, sp.CenterServiceID)
		return runUninstall(ctx, plan, *purge, *yes, *dryRun, out, errw, "center")
	}
}

func uninstallWorkerHandler(fs *flag.FlagSet) Handler {
	prefix := fs.String("prefix", "", "install prefix (default: ~/.agent-center/workers/<worker-id>/)")
	userMode := fs.Bool("user-mode", isMacRuntime(), "if true, target user-mode service paths")
	workerID := fs.String("worker-id", "", "worker identifier to uninstall (required)")
	purge := fs.Bool("purge", false, "ALSO delete the data directory + the worker subtree. NOT REVERSIBLE.")
	yes := fs.Bool("yes", false, "skip the interactive confirm prompt when --purge is set")
	dryRun := fs.Bool("dry-run", false, "print planned actions without mutating state")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		_ = args
		if strings.TrimSpace(*workerID) == "" {
			return PrintError(errw, FormatText, "uninstall_worker_missing_id",
				"--worker-id is required (the Fleet row's id, or whatever you passed to install worker)",
				ExitUsage)
		}
		resolved := *prefix
		if resolved == "" {
			resolved = defaultWorkerInstallPrefix(*userMode, *workerID)
		}
		home, _ := os.UserHomeDir()
		base, perr := platformPaths(runtimeOS(), *userMode, home)
		if perr != nil {
			return PrintError(errw, FormatText, "install_platform_unsupported", perr.Error(), ExitBusinessError)
		}
		sp := applyWorkerIDToServicePaths(base, *workerID)
		layout := newInstallLayout(resolved, "")
		plan := newUninstallPlan(layout, *purge)
		plan.addServiceTeardown(sp, sp.WorkerUnitPath, sp.WorkerServiceID)
		return runUninstall(ctx, plan, *purge, *yes, *dryRun, out, errw, "worker "+*workerID)
	}
}

// uninstallPlan captures every action the uninstall handler will run,
// in declared order. Phase 1 stops the service + removes the unit
// file. Phase 2 removes the install artefacts (versions/, current,
// etc/). Phase 3 (only when --purge) wipes var/, logs/, and the
// prefix itself.
type uninstallPlan struct {
	prefix string
	purge  bool

	serviceSteps []activationStep
	unitPath     string
	serviceID    string
	serviceMgr   string

	installPaths []string // versions/, current, etc/, bin/
	dataPaths    []string // var/, logs/, prefix root
}

func newUninstallPlan(layout installLayout, purge bool) *uninstallPlan {
	p := &uninstallPlan{prefix: layout.Prefix, purge: purge}
	// install-side artefacts always come down
	p.installPaths = []string{
		filepath.Join(layout.Prefix, "versions"),
		layout.CurrentLink,
		layout.ConfigDir, // etc/
		filepath.Join(layout.Prefix, "bin"), // legacy v2.4 layout
	}
	if purge {
		// Data side: var/, logs/, then the prefix root last.
		p.dataPaths = []string{
			layout.DataDir,
			layout.LogsDir,
			layout.Prefix,
		}
	}
	return p
}

func (p *uninstallPlan) addServiceTeardown(sp servicePaths, unitPath, serviceID string) {
	p.unitPath = unitPath
	p.serviceID = serviceID
	p.serviceMgr = sp.ServiceManager
	p.serviceSteps = serviceTeardownCmds(sp, serviceID)
}

// serviceTeardownCmds reverses serviceActivateCmds. launchd uses
// `bootout gui/<uid> <plist>` (modern API — kills daemon AND
// deregisters from SMAppService so the entry leaves the operator's
// System Settings → Login Items → Allow in Background list); systemd
// disable + stop + daemon-reload is three. All steps tolerate
// non-zero exits — operator wanted everything gone, so a service
// that's already stopped isn't a failure.
//
// v2.5.17 fix (#72): pre-v2.5.17 launchd path used `launchctl unload`
// which on macOS Ventura+ does NOT remove the SMAppService
// registration; operator was left with a stale ON toggle in
// Background Items even though daemon was stopped and plist file was
// removed. `bootout gui/<uid>` is the documented modern replacement
// (since macOS 10.10) and clears both layers in one call.
func serviceTeardownCmds(sp servicePaths, serviceID string) []activationStep {
	switch sp.ServiceManager {
	case "launchd":
		domain := launchdGUIDomain()
		return []activationStep{
			{Cmd: "launchctl bootout " + domain + " " + sp.unitPathFor(serviceID), Tolerate: true},
		}
	case "systemd":
		if sp.UserMode {
			return []activationStep{
				{Cmd: "systemctl --user stop " + serviceID, Tolerate: true},
				{Cmd: "systemctl --user disable " + serviceID, Tolerate: true},
				{Cmd: "systemctl --user daemon-reload"},
			}
		}
		return []activationStep{
			{Cmd: "systemctl stop " + serviceID, Tolerate: true},
			{Cmd: "systemctl disable " + serviceID, Tolerate: true},
			{Cmd: "systemctl daemon-reload"},
		}
	}
	return nil
}

// launchdGUIDomain returns the launchctl domain-target for the current
// user's GUI session, e.g. "gui/501". Indirected so tests can stub it
// out without forking a real launchctl call. macOS-only — callers
// guard on sp.ServiceManager == "launchd" before invoking.
var launchdGUIDomain = func() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func runUninstall(ctx context.Context, plan *uninstallPlan, purge, yes, dryRun bool, out, errw io.Writer, label string) ExitCode {
	_ = ctx
	// Show the plan first so the operator knows what's about to happen.
	fmt.Fprintf(out, "agent-center uninstall %s:\n", label)
	fmt.Fprintf(out, "  prefix:        %s\n", plan.prefix)
	fmt.Fprintf(out, "  purge:         %v\n", purge)
	if plan.serviceID != "" {
		fmt.Fprintf(out, "  service:       %s (%s)\n", plan.serviceID, plan.serviceMgr)
	}

	if purge && !yes && !dryRun {
		if !confirmPurge(out, errw, plan.prefix) {
			fmt.Fprintln(out, "[abort] --purge confirm declined; no changes made")
			return ExitOK
		}
	}

	if dryRun {
		fmt.Fprintln(out, "  Plan:")
		for _, s := range plan.serviceSteps {
			fmt.Fprintf(out, "    - %s\n", s.Cmd)
		}
		if plan.unitPath != "" {
			fmt.Fprintf(out, "    - rm %s  (service unit file)\n", plan.unitPath)
		}
		for _, p := range plan.installPaths {
			fmt.Fprintf(out, "    - rm -rf %s\n", p)
		}
		for _, p := range plan.dataPaths {
			fmt.Fprintf(out, "    - rm -rf %s  (--purge)\n", p)
		}
		fmt.Fprintln(out, "[dry-run] no changes made")
		return ExitOK
	}

	// Phase 1: stop + unload service. Best-effort. v2.5.17 (#72) —
	// stdout/stderr are now surfaced to the operator instead of being
	// io.Discard'd, so a `launchctl bootout` that complains about
	// "service target does not exist" or "permission denied" is visible
	// at the terminal alongside the rest of the uninstall plan.
	for _, s := range plan.serviceSteps {
		runShellTolerant(out, s)
	}
	if plan.unitPath != "" {
		if err := os.Remove(plan.unitPath); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(errw, "warning: remove unit file %s: %v\n", plan.unitPath, err)
		}
	}

	// Phase 2: install artefacts.
	for _, p := range plan.installPaths {
		if err := os.RemoveAll(p); err != nil {
			fmt.Fprintf(errw, "warning: rm -rf %s: %v\n", p, err)
		}
	}

	// Phase 3: --purge data side.
	if purge {
		for _, p := range plan.dataPaths {
			if err := os.RemoveAll(p); err != nil {
				fmt.Fprintf(errw, "warning: rm -rf %s: %v\n", p, err)
			}
		}
	}

	// Summary tells the user *what survived* + how to reinstall /
	// re-purge — see PD's polish #1 in #agent-center:c691d9da.
	fmt.Fprintf(out, "\n✓ AgentCenter %s uninstalled\n", label)
	if purge {
		fmt.Fprintf(out, "  All data wiped. Prefix %s removed.\n", plan.prefix)
		return ExitOK
	}
	fmt.Fprintf(out, "  Data preserved at:\n")
	fmt.Fprintf(out, "    %s/var/   (sqlite + master.key + tokens)\n", plan.prefix)
	fmt.Fprintf(out, "    %s/logs/  (service stdout/stderr)\n", plan.prefix)
	fmt.Fprintf(out, "  Reinstall with `./install %s` at the same prefix will reuse them.\n", reinstallLabel(label))
	fmt.Fprintf(out, "  To remove all data: rerun with --purge\n")
	return ExitOK
}

// reinstallLabel maps the human-friendly label back to the install
// subcommand the operator should run if they want to come back.
// "worker <id>" → "worker"; "center" → "center".
func reinstallLabel(label string) string {
	if strings.HasPrefix(label, "worker") {
		return "worker"
	}
	return label
}

// confirmPurge reads y/N from stdin. Returns true only on explicit "y" / "yes".
// When stdin isn't a TTY (piped input), returns false unless the
// caller passed --yes (handled at the call site).
func confirmPurge(out, errw io.Writer, prefix string) bool {
	fmt.Fprintf(out, "\n  --purge will DELETE %s and everything under it.\n", prefix)
	fmt.Fprintf(out, "  This includes the sqlite database, master.key, and worker tokens.\n")
	fmt.Fprintf(out, "  This is NOT REVERSIBLE.\n\n")
	fmt.Fprintf(out, "  Type \"yes\" to continue, anything else to abort: ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		fmt.Fprintf(errw, "\n[abort] could not read confirmation: %v\n", err)
		return false
	}
	answer := strings.TrimSpace(strings.ToLower(line))
	return answer == "yes" || answer == "y"
}

// runShellTolerant invokes one of the prebuilt teardown steps; never
// returns an error because the operator wanted everything gone (a
// service that's already stopped is not a failure).
//
// v2.5.17 (#72): stdout + stderr now flow to `out` so the operator can
// see what the service manager actually said. Pre-v2.5.17 both streams
// were io.Discard, which hid `launchctl unload` no-ops on modern macOS
// (where the modern bootout/bootstrap API is required) and made the
// uninstall feel like it "did nothing" when in fact the unload command
// had silently failed.
func runShellTolerant(out io.Writer, step activationStep) {
	parts := splitSpaces(step.Cmd)
	if len(parts) == 0 {
		return
	}
	fmt.Fprintf(out, "  $ %s\n", step.Cmd)
	cmd := exec.Command(parts[0], parts[1:]...) //nolint:gosec
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil && !step.Tolerate {
		fmt.Fprintf(out, "    (exit error: %v)\n", err)
	}
}

// ensure linux import resolves when building cross-platform; the
// platform-routing helpers in install_fs.go already gate on GOOS so
// we don't need to do anything here, but keep the symbol referenced
// to silence a possible future unused-import warning.
var _ = runtime.GOOS

// errNoSuchInstall is a sentinel returned by future stricter checks
// (e.g. uninstall on a prefix that was never installed). Today the
// handler simply removes anything it finds and prints what it did;
// future iterations can promote this into a clearer error message.
var errNoSuchInstall = errors.New("uninstall: no install detected at prefix")
