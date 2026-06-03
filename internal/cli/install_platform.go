// install_platform.go — generate systemd unit (Linux) / launchd plist
// (Mac) for the center + worker services. v2.4-D-A2 (task #36).
//
// Mac launchd path is the only one that must actually work for v2.4 PM
// acceptance (mac arm64 only per @oopslink 2026-05-26 scope narrow);
// systemd path is implemented + has unit tests for the unit-file
// rendering but is not validated end-to-end. Marked clearly so future
// iterations can validate on Linux without re-deriving the layout.
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// servicePaths bundles the OS-specific paths where service definitions
// live. system mode → root-owned paths; user mode → per-user.
type servicePaths struct {
	OS              string // "darwin" or "linux"
	UserMode        bool
	CenterUnitPath  string // where the center service unit lives
	WorkerUnitPath  string // where the worker service unit lives
	CenterServiceID string // systemd service name or launchd plist Label
	WorkerServiceID string
	ServiceManager  string // "systemd" or "launchd"
}

// sanitizeWorkerIDForLabel returns a worker_id form that's safe for
// embedding in a launchd Label / systemd unit name / filesystem path.
// Lowercases, replaces every non-[a-z0-9] char with '-', collapses
// repeated dashes, trims leading/trailing dashes. Empty input or
// fully-non-alphanumeric input returns "default".
func sanitizeWorkerIDForLabel(id string) string {
	if id == "" {
		return "default"
	}
	var b strings.Builder
	b.Grow(len(id))
	prevDash := false
	for _, r := range strings.ToLower(id) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "default"
	}
	return s
}

// applyWorkerIDToServicePaths returns sp with the worker label + unit
// path scoped by worker-id, so multiple workers can coexist on the
// same machine without trampling each other's launchd / systemd
// registration. @oopslink 2026-05-26 ask in #agent-center:0c9f6bb7
// (multi-tenancy / per-tenant testing).
func applyWorkerIDToServicePaths(sp servicePaths, workerID string) servicePaths {
	safe := sanitizeWorkerIDForLabel(workerID)
	switch sp.ServiceManager {
	case "launchd":
		sp.WorkerServiceID = "com.agent-center.worker." + safe
		sp.WorkerUnitPath = strings.Replace(sp.WorkerUnitPath, "com.agent-center.worker.plist", sp.WorkerServiceID+".plist", 1)
	case "systemd":
		sp.WorkerServiceID = "agent-center-worker-" + safe + ".service"
		sp.WorkerUnitPath = strings.Replace(sp.WorkerUnitPath, "agent-center-worker.service", sp.WorkerServiceID, 1)
	}
	return sp
}

// platformPaths picks the install target dirs for a given OS + mode.
// homeDir is needed for user-mode paths; pass os.UserHomeDir().
func platformPaths(osName string, userMode bool, homeDir string) (servicePaths, error) {
	switch osName {
	case "darwin":
		// On Mac we always go user mode in v2.4 (system-wide LaunchDaemon
		// would need root + /Library — deferred to v3 deployment-as-product).
		// Operator opts out of user mode by setting --prefix to a system
		// path — but the launchd plist still goes under their LaunchAgents.
		if homeDir == "" {
			return servicePaths{}, fmt.Errorf("install platform: empty home dir on darwin")
		}
		return servicePaths{
			OS:              "darwin",
			UserMode:        true, // forced on Mac
			CenterUnitPath:  homeDir + "/Library/LaunchAgents/com.agent-center.center.plist",
			WorkerUnitPath:  homeDir + "/Library/LaunchAgents/com.agent-center.worker.plist",
			CenterServiceID: "com.agent-center.center",
			WorkerServiceID: "com.agent-center.worker",
			ServiceManager:  "launchd",
		}, nil
	case "linux":
		if userMode {
			if homeDir == "" {
				return servicePaths{}, fmt.Errorf("install platform: empty home dir on linux user mode")
			}
			return servicePaths{
				OS:              "linux",
				UserMode:        true,
				CenterUnitPath:  homeDir + "/.config/systemd/user/agent-center.service",
				WorkerUnitPath:  homeDir + "/.config/systemd/user/agent-center-worker.service",
				CenterServiceID: "agent-center.service",
				WorkerServiceID: "agent-center-worker.service",
				ServiceManager:  "systemd",
			}, nil
		}
		return servicePaths{
			OS:              "linux",
			UserMode:        false,
			CenterUnitPath:  "/etc/systemd/system/agent-center.service",
			WorkerUnitPath:  "/etc/systemd/system/agent-center-worker.service",
			CenterServiceID: "agent-center.service",
			WorkerServiceID: "agent-center-worker.service",
			ServiceManager:  "systemd",
		}, nil
	}
	return servicePaths{}, fmt.Errorf("install platform: unsupported OS %q (only darwin + linux supported in v2.4)", osName)
}

// renderCenterServiceUnit returns the systemd unit body (linux) or
// launchd plist body (mac) for the center service. binaryPath is the
// fully-resolved path to agent-center under <prefix>/current/bin/
// (we use the `current` symlink so upgrades swap-without-touching
// the service unit — that's the whole point of A5's atomic swap).
func renderCenterServiceUnit(sp servicePaths, binaryPath, configPath, logsDir string) string {
	switch sp.ServiceManager {
	case "launchd":
		// The center neither probes CLIs nor spawns agent CLIs, so it
		// needs no augmented PATH (empty pathEnv = no EnvironmentVariables).
		return renderLaunchdPlist(sp.CenterServiceID, binaryPath, []string{
			"server", "--config=" + configPath,
		}, logsDir, "")
	case "systemd":
		return renderSystemdUnit(systemdUnit{
			Description:  "agent-center server",
			ExecStart:    binaryPath + " server --config=" + configPath,
			After:        "network-online.target",
			Wants:        "network-online.target",
			KillMode:     "", // server uses default (control-group); only worker needs KillMode=process
			UserMode:     sp.UserMode,
			WantedByUser: "default.target",
			WantedBySys:  "multi-user.target",
		})
	}
	return ""
}

// renderWorkerServiceUnit ditto for worker.
//
// v2.7 (b) cutover: the worker now runs as `agent-center worker run` (the unified
// binary — so the daemon's os.Executable() can route the worker agent-supervisor /
// mcp-host subcommands it spawns). binaryPath is therefore `agent-center` and the
// args are PREFIXED with the `worker run` sub-command path. This REVERSES the
// v2.4-D-F4 X1 fix (which dropped the prefix because the OLD standalone
// `agent-center-worker-daemon` used flag.Parse() and treated `worker run` as a
// non-flag terminator): the unified CLI router consumes the sub-command path
// first, then `worker run` flag-parses the remainder — so the prefix is correct
// and required.
func renderWorkerServiceUnit(sp servicePaths, binaryPath, configPath, workerID, workerName, bootstrap, token, fingerprint, logsDir, pathEnv string) string {
	args := []string{
		"worker", "run",
		"--config=" + configPath,
		"--worker-id=" + workerID,
		"--admin-target=" + bootstrap,
		"--admin-token=" + token,
	}
	if workerName != "" {
		args = append(args, "--worker-name="+workerName)
	}
	if fingerprint != "" {
		args = append(args, "--server-fingerprint="+fingerprint)
	}
	// v2.7 #147: no --capabilities — the daemon auto-probes installed CLIs on
	// every online and reports them to center.
	switch sp.ServiceManager {
	case "launchd":
		return renderLaunchdPlist(sp.WorkerServiceID, binaryPath, args, logsDir, pathEnv)
	case "systemd":
		return renderSystemdUnit(systemdUnit{
			Description:  "agent-center worker daemon",
			ExecStart:    binaryPath + " " + strings.Join(args, " "),
			After:        "network-online.target",
			KillMode:     "process", // ADR-0018 (per-execution shim outlives daemon)
			UserMode:     sp.UserMode,
			WantedByUser: "default.target",
			WantedBySys:  "multi-user.target",
			PathEnv:      pathEnv,
		})
	}
	return ""
}

// resolveWorkerPATH builds the PATH the worker daemon's service unit
// should run with. v2.7 #175 (acceptance FINDING-C sub-3): launchd (and
// systemd) start the daemon with a minimal PATH, so the daemon's CLI
// probe (ProbeAllAdapters) — and the agent CLIs it later spawns — could
// not find user-installed binaries (claude/codex/opencode) that live in
// homebrew / cargo / npm-global / etc. We union the installing user's own
// PATH (captured at install time — catches volta/nvm/asdf/custom layouts
// that no hardcoded list would) with a set of well-known user CLI dirs as
// a backstop (covers the case where the install itself ran from a reduced
// env). Order is preserved with the live PATH first; duplicates dropped.
func resolveWorkerPATH(home string) string {
	var ordered []string
	seen := map[string]struct{}{}
	add := func(dir string) {
		if dir == "" {
			return
		}
		if _, ok := seen[dir]; ok {
			return
		}
		seen[dir] = struct{}{}
		ordered = append(ordered, dir)
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		add(dir)
	}
	wellKnown := []string{
		"/usr/local/bin",
		"/opt/homebrew/bin",
		"/usr/bin",
		"/bin",
		"/usr/sbin",
		"/sbin",
	}
	if home != "" {
		wellKnown = append(wellKnown,
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".cargo", "bin"),
			filepath.Join(home, ".npm-global", "bin"),
			filepath.Join(home, ".volta", "bin"),
		)
	}
	for _, dir := range wellKnown {
		add(dir)
	}
	return strings.Join(ordered, string(os.PathListSeparator))
}

// systemdUnit captures the systemd-unit-file fields we care about.
type systemdUnit struct {
	Description  string
	ExecStart    string
	After        string
	Wants        string
	KillMode     string
	UserMode     bool
	WantedByUser string
	WantedBySys  string
	PathEnv      string // #175: Environment=PATH= so the daemon finds user CLIs
}

func renderSystemdUnit(u systemdUnit) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=" + u.Description + "\n")
	if u.After != "" {
		b.WriteString("After=" + u.After + "\n")
	}
	if u.Wants != "" {
		b.WriteString("Wants=" + u.Wants + "\n")
	}
	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")
	if u.PathEnv != "" {
		b.WriteString("Environment=PATH=" + u.PathEnv + "\n")
	}
	b.WriteString("ExecStart=" + u.ExecStart + "\n")
	b.WriteString("Restart=on-failure\n")
	b.WriteString("RestartSec=5s\n")
	b.WriteString("StandardOutput=journal\n")
	b.WriteString("StandardError=journal\n")
	if u.KillMode != "" {
		b.WriteString("KillMode=" + u.KillMode + "\n")
	}
	if !u.UserMode {
		// Security hardening only applies to system-mode units;
		// user units run with the user's own permissions.
		b.WriteString("NoNewPrivileges=true\n")
		b.WriteString("PrivateTmp=true\n")
	}
	b.WriteString("\n[Install]\n")
	if u.UserMode {
		b.WriteString("WantedBy=" + u.WantedByUser + "\n")
	} else {
		b.WriteString("WantedBy=" + u.WantedBySys + "\n")
	}
	return b.String()
}

// renderLaunchdPlist returns a minimal LaunchAgent plist that runs the
// given binary with args, restarts on crash, and writes stdout/stderr
// under logsDir (per-install `<prefix>/logs/`). v2.4.1 moved these off
// `/tmp/` so a `~/.agent-center/` install keeps everything in one tree
// and reboots don't wipe the daemon log (operator finds last-failure
// context where the rest of the install lives).
func renderLaunchdPlist(label, binaryPath string, args []string, logsDir, pathEnv string) string {
	var argsXML strings.Builder
	argsXML.WriteString("\t\t<string>" + xmlEscape(binaryPath) + "</string>\n")
	for _, a := range args {
		argsXML.WriteString("\t\t<string>" + xmlEscape(a) + "</string>\n")
	}
	outPath := launchdLogPath(logsDir, label, "out")
	errPath := launchdLogPath(logsDir, label, "err")
	// #175: launchd starts daemons with a minimal PATH. When pathEnv is
	// set (worker), inject an EnvironmentVariables dict so the daemon's CLI
	// probe + the agent CLIs it spawns can find user-installed binaries.
	envXML := ""
	if pathEnv != "" {
		envXML = `	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>` + xmlEscape(pathEnv) + `</string>
	</dict>
`
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + xmlEscape(label) + `</string>
	<key>ProgramArguments</key>
	<array>
` + argsXML.String() + `	</array>
` + envXML + `	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>StandardOutPath</key>
	<string>` + xmlEscape(outPath) + `</string>
	<key>StandardErrorPath</key>
	<string>` + xmlEscape(errPath) + `</string>
</dict>
</plist>
`
}

// launchdLogPath returns `<logsDir>/<label>.<kind>.log`, falling back to
// /tmp when logsDir is empty (test fixtures still pass "" through
// renderLaunchdPlist directly).
func launchdLogPath(logsDir, label, kind string) string {
	if logsDir == "" {
		return "/tmp/" + label + "." + kind + ".log"
	}
	return logsDir + "/" + label + "." + kind + ".log"
}

// xmlEscape is a tiny escaper for the strings we put in plist values.
// Paths + flags don't generally contain XML-sensitive chars but a
// defensive escape costs nothing.
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
