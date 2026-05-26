// install_fs.go — filesystem + service activation for `install center` /
// `install worker`. v2.4-D-A2 (task #36).
//
// Responsibility split:
//   - A2 (this file): write versioned binaries + service unit + config +
//     atomic symlink swap (current → versions/<v>); activate service.
//   - A5 (next ST): wrap the same flow with upgrade semantics — DB
//     migration apply + service restart + health probe + auto-rollback.
//
// On install success we print the operator-facing summary + URL +
// (for center) the bootstrap token. The version selection is the
// running binary's own version (installerVersion()).
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// activationStep is one shell command in the activation sequence,
// plus a flag for whether a non-zero exit should be tolerated (e.g.
// launchctl unload of a service that isn't currently loaded).
type activationStep struct {
	Cmd      string
	Tolerate bool
}

// activateService runs the platform-appropriate command to enable +
// start the service. If skipActivate is true, prints the command the
// operator would run manually instead. Returns nil on success or a
// descriptive error.
func activateService(sp servicePaths, serviceID string, out io.Writer, skipActivate bool) error {
	steps := serviceActivateCmds(sp, serviceID)
	if skipActivate {
		fmt.Fprintln(out, "  service activation skipped — run these to activate:")
		for _, s := range steps {
			fmt.Fprintf(out, "    %s\n", s.Cmd)
		}
		return nil
	}
	for _, s := range steps {
		// Tokenize on space — safe because we built these strings, no
		// user input goes in.
		parts := splitSpaces(s.Cmd)
		cmd := exec.Command(parts[0], parts[1:]...) //nolint:gosec
		if s.Tolerate {
			// Suppress output so the operator doesn't see "Unload
			// failed: 5: Input/output error" on a clean install.
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
			_ = cmd.Run()
			continue
		}
		cmd.Stdout = out
		cmd.Stderr = out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("activate service via %q: %w", s.Cmd, err)
		}
	}
	return nil
}

// serviceActivateCmds returns the shell-level commands to enable +
// start the service. Built strings, no operator input.
func serviceActivateCmds(sp servicePaths, serviceID string) []activationStep {
	switch sp.ServiceManager {
	case "launchd":
		return []activationStep{
			{Cmd: "launchctl unload " + sp.unitPathFor(serviceID), Tolerate: true},
			{Cmd: "launchctl load " + sp.unitPathFor(serviceID)},
		}
	case "systemd":
		if sp.UserMode {
			return []activationStep{
				{Cmd: "systemctl --user daemon-reload"},
				{Cmd: "systemctl --user enable " + serviceID},
				{Cmd: "systemctl --user restart " + serviceID},
			}
		}
		return []activationStep{
			{Cmd: "systemctl daemon-reload"},
			{Cmd: "systemctl enable " + serviceID},
			{Cmd: "systemctl restart " + serviceID},
		}
	}
	return nil
}

func (sp servicePaths) unitPathFor(serviceID string) string {
	if serviceID == sp.CenterServiceID {
		return sp.CenterUnitPath
	}
	if serviceID == sp.WorkerServiceID {
		return sp.WorkerUnitPath
	}
	return ""
}

// splitSpaces is a tiny tokenizer for the activation commands we
// build ourselves. Doesn't handle quoted strings — none of our
// commands need them.
func splitSpaces(s string) []string {
	var out []string
	current := ""
	for _, r := range s {
		if r == ' ' {
			if current != "" {
				out = append(out, current)
				current = ""
			}
			continue
		}
		current += string(r)
	}
	if current != "" {
		out = append(out, current)
	}
	return out
}

// installLayout captures the directories we create under <prefix> for a
// versioned install.
type installLayout struct {
	Prefix        string // <prefix>
	Version       string // e.g. "v2.4.0"
	VersionedDir  string // <prefix>/versions/<version>
	BinDir        string // <prefix>/versions/<version>/bin
	ConfigDir     string // <prefix>/etc
	ConfigPath    string // <prefix>/etc/config.yaml
	DataDir       string // <prefix>/var
	CurrentLink   string // <prefix>/current
	CurrentBinDir string // <prefix>/current/bin (stable across upgrades)
}

func newInstallLayout(prefix, version string) installLayout {
	versionedDir := filepath.Join(prefix, "versions", version)
	currentLink := filepath.Join(prefix, "current")
	return installLayout{
		Prefix:        prefix,
		Version:       version,
		VersionedDir:  versionedDir,
		BinDir:        filepath.Join(versionedDir, "bin"),
		ConfigDir:     filepath.Join(prefix, "etc"),
		ConfigPath:    filepath.Join(prefix, "etc", "config.yaml"),
		DataDir:       filepath.Join(prefix, "var"),
		CurrentLink:   currentLink,
		CurrentBinDir: filepath.Join(currentLink, "bin"),
	}
}

// copyBinaries copies the executables found alongside the running
// binary (assumed layout: tarball/bin/{agent-center, agent-center-worker-
// daemon, fakeagent}) into layout.BinDir. Returns the binary paths
// under the new versioned dir.
func copyBinaries(layout installLayout) (centerBin, workerBin string, err error) {
	srcDir, err := selfBinDir()
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(layout.BinDir, 0o755); err != nil {
		return "", "", fmt.Errorf("mkdir %s: %w", layout.BinDir, err)
	}
	for _, name := range []string{"agent-center", "agent-center-worker-daemon", "fakeagent"} {
		src := filepath.Join(srcDir, name)
		if _, err := os.Stat(src); err != nil {
			if os.IsNotExist(err) {
				if name == "fakeagent" {
					// fakeagent is optional in prod tarballs.
					continue
				}
				return "", "", fmt.Errorf("required binary missing in tarball: %s", src)
			}
			return "", "", fmt.Errorf("stat %s: %w", src, err)
		}
		dst := filepath.Join(layout.BinDir, name)
		if err := copyFileMode0755(src, dst); err != nil {
			return "", "", fmt.Errorf("copy %s → %s: %w", src, dst, err)
		}
	}
	return filepath.Join(layout.BinDir, "agent-center"),
		filepath.Join(layout.BinDir, "agent-center-worker-daemon"),
		nil
}

// selfBinDir returns the directory containing the running binary.
// Tarball layout assumes binaries are siblings.
func selfBinDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("os.Executable: %w", err)
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		real = exe // best-effort
	}
	return filepath.Dir(real), nil
}

// copyFileMode0755 copies src → dst with mode 0755 (executable).
func copyFileMode0755(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// writeVersionFile writes the VERSION marker that detectExistingInstall
// reads on next invocation.
func writeVersionFile(layout installLayout) error {
	return os.WriteFile(filepath.Join(layout.VersionedDir, "VERSION"),
		[]byte(layout.Version+"\n"), 0o644)
}

// atomicSymlinkSwap creates <prefix>/current → versionedDir using a
// tmp + rename (POSIX-atomic). Existing symlink is replaced; missing
// symlink is created.
func atomicSymlinkSwap(layout installLayout) error {
	// Ensure the symlink's parent exists.
	if err := os.MkdirAll(filepath.Dir(layout.CurrentLink), 0o755); err != nil {
		return err
	}
	tmp := layout.CurrentLink + ".new"
	_ = os.Remove(tmp) // tolerate stale
	if err := os.Symlink(layout.VersionedDir, tmp); err != nil {
		return fmt.Errorf("create new symlink %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, layout.CurrentLink); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s → %s: %w", tmp, layout.CurrentLink, err)
	}
	return nil
}

// writeCenterConfig writes a minimal default config.yaml for the
// center. The operator can edit later. Defaults align with v2.2
// single-host guide.
func writeCenterConfig(layout installLayout, port int, tcpListen string) error {
	if err := os.MkdirAll(layout.ConfigDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(layout.DataDir, 0o755); err != nil {
		return err
	}
	yaml := centerConfigYAML(layout.DataDir, port, tcpListen)
	return os.WriteFile(layout.ConfigPath, []byte(yaml), 0o644)
}

// centerConfigYAML returns the YAML body for the default center config.
// Kept as a pure function so tests can assert content.
func centerConfigYAML(dataDir string, port int, tcpListen string) string {
	yaml := `# agent-center — installed by v2.4-D-A2 install command.
# Edit this file then ` + "`systemctl --user restart agent-center`" + ` (or launchctl) to apply.

server:
  listen_addr: ":7000"
  sqlite_path: "` + dataDir + `/agent-center.db"
  admin_socket_path: "` + dataDir + `/admin.sock"
`
	if tcpListen != "" {
		yaml += `  admin_tcp_listen: "` + tcpListen + `"
`
	}
	yaml += fmt.Sprintf(`
identity:
  default_user: hayang

web_console:
  enabled: true
  listen_addr: "127.0.0.1:%d"
`, port)
	return yaml
}

// writeWorkerConfig writes a minimal default config.yaml for a worker.
func writeWorkerConfig(layout installLayout) error {
	if err := os.MkdirAll(layout.ConfigDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(layout.DataDir, 0o755); err != nil {
		return err
	}
	yaml := `# agent-center worker — installed by v2.4-D-A2 install worker command.
# The worker daemon's flags (--admin-target / --server-fingerprint /
# --admin-token / --worker-id) live in the service unit, not this file;
# this file holds local-only paths.

server:
  sqlite_path: "` + layout.DataDir + `/worker.db"
`
	return os.WriteFile(layout.ConfigPath, []byte(yaml), 0o644)
}

// writeUnitFile writes the systemd unit / launchd plist content to the
// platform-chosen path.
func writeUnitFile(unitPath, body string) error {
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(unitPath, []byte(body), 0o644)
}

// runtimeOS returns runtime.GOOS as a string. Wrapped so tests can
// stub platform-specific behavior.
var runtimeOS = func() string { return runtime.GOOS }

// errInstallSkipped is the sentinel A2 returns when a platform path
// hasn't been validated yet. Currently unused (we activate on both
// systemd + launchd) but reserved.
var errInstallSkipped = errors.New("install: platform path not yet validated")

// installShouldActivate gates the actual systemctl/launchctl invocation.
// Returns true (= activate the service) unless overridden. Overrides:
//   - env AGENT_CENTER_INSTALL_SKIP_ACTIVATE=1 (test harness)
//   - sp.ServiceManager unknown (no commands to run)
//
// Production install: returns true → service actually starts.
var installShouldActivate = func(sp servicePaths) bool {
	if os.Getenv("AGENT_CENTER_INSTALL_SKIP_ACTIVATE") == "1" {
		return false
	}
	if sp.ServiceManager == "" {
		return false
	}
	return true
}
