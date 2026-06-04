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

	"github.com/oopslink/agent-center/internal/secretmgmt"
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
		// Modern domain-target API (bootout/bootstrap), NOT the deprecated
		// load/unload: `launchctl load` fails with "Load failed: 5: Input/output
		// error" on Darwin 25.1.0+ (macOS 26), breaking install + upgrade. This
		// mirrors the #72 teardown migration to `bootout gui/<uid>` — the
		// activate/restart path had been missed. bootout (tolerated: the service
		// may not be loaded yet on a fresh install, and on upgrade it removes the
		// running old service) → bootstrap loads the plist, which starts the
		// service via RunAtLoad=true. domain = gui/<uid> (same helper as teardown).
		domain := launchdGUIDomain()
		return []activationStep{
			{Cmd: "launchctl bootout " + domain + " " + sp.unitPathFor(serviceID), Tolerate: true},
			{Cmd: "launchctl bootstrap " + domain + " " + sp.unitPathFor(serviceID)},
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
	LogsDir       string // <prefix>/logs (launchd stdout/stderr land here)
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
		LogsDir:       filepath.Join(prefix, "logs"),
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
	// v2.7 (b) cutover: the worker is now the unified `agent-center` binary
	// (`agent-center worker run`); the standalone agent-center-worker-daemon is
	// retired and no longer copied/deployed.
	for _, name := range []string{"agent-center", "fakeagent"} {
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
	// workerBin is the same unified binary (worker runs as `agent-center worker run`).
	agentCenterBin := filepath.Join(layout.BinDir, "agent-center")
	return agentCenterBin, agentCenterBin, nil
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

// writeVersionFile writes the VERSION + COMMIT markers that
// detectExistingInstall reads on next invocation. v2.7.1 #234: the
// COMMIT marker (build git sha) lets a same-version rebuild be told
// apart from a true no-op so the binary swap is not silently skipped.
func writeVersionFile(layout installLayout) error {
	if err := os.WriteFile(filepath.Join(layout.VersionedDir, "VERSION"),
		[]byte(layout.Version+"\n"), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(layout.VersionedDir, "COMMIT"),
		[]byte(installerCommit()+"\n"), 0o644)
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
// center + provisions a master_key file (v2.5 X1 polish — see
// #agent-center:3f970c5d). The operator can edit either later.
// Defaults align with v2.2 single-host guide.
//
// v2.5 first-boot master_key: SecretManagement BC's AES-256 master
// key is treated as a first-class first-boot asset alongside
// bootstrap_token — we generate `<prefix>/var/master.key` (mode
// 0600) and reference it from the config. Without this, B2's
// install-command re-display always returns 503 on fresh installs
// since the AdminToken service has no master key to encrypt the
// enroll-token plaintext with. UserSecret BC also stays disabled
// until the operator manually provisions one. Auto-provisioning
// matches v2.4-D-A2's bootstrap_token approach: opinionated
// defaults so the happy path works out of the box.
func writeCenterConfig(layout installLayout, port, serverPort int, tcpListen, bootstrapPublicURL, instance string) error {
	if err := os.MkdirAll(layout.ConfigDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(layout.DataDir, 0o755); err != nil {
		return err
	}
	masterKeyPath := filepath.Join(layout.DataDir, "master.key")
	if err := ensureMasterKeyFile(masterKeyPath); err != nil {
		return err
	}
	yaml := centerConfigYAML(layout.DataDir, port, serverPort, tcpListen, bootstrapPublicURL, masterKeyPath, instance)
	return os.WriteFile(layout.ConfigPath, []byte(yaml), 0o644)
}

// ensureMasterKeyFile creates the AES-256 master key on first install
// (idempotent: re-installs keep the existing key so the encrypted
// UserSecret + enroll-token plaintext payloads survive). Mode 0600,
// owner-only.
func ensureMasterKeyFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil // preserve existing key on re-install / upgrade
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat master key: %w", err)
	}
	mk, err := secretmgmt.GenerateMasterKey()
	if err != nil {
		return fmt.Errorf("generate master key: %w", err)
	}
	// secretmgmt.LoadMasterKey accepts base64 (std + URL); write base64.
	body := mk.Base64() + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write master key: %w", err)
	}
	return nil
}

// centerConfigYAML returns the YAML body for the default center config.
// Kept as a pure function so tests can assert content.
func centerConfigYAML(dataDir string, port, serverPort int, tcpListen, bootstrapPublicURL, masterKeyPath, instance string) string {
	if serverPort == 0 {
		serverPort = 7050
	}
	if instance == "" {
		instance = "default"
	}
	yaml := `# agent-center — installed by v2.4-D-A2 install command.
# Edit this file then ` + "`systemctl --user restart agent-center`" + ` (or launchctl) to apply.

server:
  # v2.7.1 #211: names this center deployment (multi-center coexistence on one host).
  instance: "` + instance + `"
  # v2.7 #161: default off :7000 — macOS AirPlay Receiver (AirTunes) listens on
  # 7000 by default, so :7000 fails to bind on a fresh Mac install and the center
  # never starts. :7050 avoids AirPlay (7000) and the web console (7100), and
  # keeps the 70xx/73xx numbering. (server and web_console are separate listeners
  # and must not share a port.) v2.7.1 #211: --server-port makes this configurable.
  listen_addr: "` + fmt.Sprintf(":%d", serverPort) + `"
  sqlite_path: "` + dataDir + `/agent-center.db"
  admin_socket_path: "` + dataDir + `/admin.sock"
`
	if tcpListen != "" {
		yaml += `  admin_tcp_listen: "` + tcpListen + `"
`
	}
	// v2.7 #200: externally-reachable host:port for the Web Console Add Worker
	// command, independent of the bind address. Set when remote workers must dial
	// a public DNS / LB / NAT address rather than the bind addr.
	if bootstrapPublicURL != "" {
		yaml += `  bootstrap_public_url: "` + bootstrapPublicURL + `"
`
	}
	yaml += fmt.Sprintf(`
secret_management:
  # v2.5 X1 polish: auto-generated at install time (mode 0600).
  # UserSecret BC + Add Worker Show install command depend on this
  # being present; without it Show install command returns 503.
  master_key_file: "%s"

web_console:
  enabled: true
  listen_addr: "127.0.0.1:%d"
`, masterKeyPath, port)
	// v2.7 #159: the install config MUST set a writable blob_store root, else
	// FilesSvc is never wired and every /api/files upload returns 501 (channel/
	// conversation file attachments — #133/#142 — fully broken on a fresh
	// install). The DefaultConfig fallback ("/var/lib/agent-center/blobs") is a
	// Linux-system path that MkdirAll cannot create under a macOS user-mode
	// prefix. Anchor it under the install data dir (<prefix>/var) like sqlite.
	yaml += `
blob_store:
  kind: "local"
  root: "` + dataDir + `/blobs"
`
	return yaml
}

// writeWorkerConfig writes the worker config.yaml. v2.7.1 #249: config is the
// single source of truth for the worker's enrollment identity — worker_id /
// bootstrap / token / fingerprint live here (not on the `worker run` command
// line), so the token never appears in `ps` / the launchd plist / the systemd
// unit. The file is 0600 because it contains the token (same protection model
// as the center's bootstrap_token file).
func writeWorkerConfig(layout installLayout, ic installContext) error {
	if err := os.MkdirAll(layout.ConfigDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(layout.DataDir, 0o755); err != nil {
		return err
	}
	yaml := "# agent-center worker config (v2.7.1 #249: single source of truth).\n" +
		"# Holds the worker's enrollment identity so `agent-center worker run --config=<this>`\n" +
		"# needs no secrets on the command line. File mode 0600 — it contains the token.\n\n" +
		"server:\n" +
		fmt.Sprintf("  sqlite_path: %q\n", layout.DataDir+"/worker.db") +
		workerConfigBlock(ic)
	return os.WriteFile(layout.ConfigPath, []byte(yaml), 0o600)
}

// workerConfigBlock renders the `worker:` YAML section (v2.7.1 #249). Shared by
// the fresh install (writeWorkerConfig) and the #251 upgrade migration (appended
// to a pre-#141 config). %q quotes each value safely for a YAML double-quoted
// scalar (bootstrap "tcp://...", token, fingerprint "sha256:..." all carry ':'/'/').
func workerConfigBlock(ic installContext) string {
	b := "worker:\n" +
		fmt.Sprintf("  worker_id: %q\n", ic.WorkerID) +
		fmt.Sprintf("  bootstrap: %q\n", ic.Bootstrap)
	if ic.WorkerName != "" {
		b += fmt.Sprintf("  worker_name: %q\n", ic.WorkerName)
	}
	if ic.Token != "" {
		b += fmt.Sprintf("  token: %q\n", ic.Token)
	}
	if ic.Fingerprint != "" {
		b += fmt.Sprintf("  server_fingerprint: %q\n", ic.Fingerprint)
	}
	return b
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
