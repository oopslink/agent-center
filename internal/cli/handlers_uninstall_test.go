package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUninstallCenter_DryRun renders a full plan without touching the
// filesystem. Verifies the default (non-purge) plan lists every
// install artefact but skips var/+logs/+prefix, and that the --purge
// extension adds those three rm -rf lines.
func TestUninstallCenter_DryRun_Default(t *testing.T) {
	cmd := UninstallCenterCommand()
	prefix := t.TempDir()
	stdout, _, code := runHandler(t, cmd, []string{
		"--prefix=" + prefix, "--dry-run",
	})
	if code != ExitOK {
		t.Fatalf("code=%d stdout=%s", code, stdout)
	}
	for _, want := range []string{
		"rm -rf " + filepath.Join(prefix, "versions"),
		"rm -rf " + filepath.Join(prefix, "current"),
		"rm -rf " + filepath.Join(prefix, "etc"),
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing planned step %q in:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "--purge") {
		t.Errorf("default plan should not mention --purge artefacts:\n%s", stdout)
	}
}

func TestUninstallCenter_DryRun_Purge(t *testing.T) {
	cmd := UninstallCenterCommand()
	prefix := t.TempDir()
	stdout, _, code := runHandler(t, cmd, []string{
		"--prefix=" + prefix, "--purge", "--yes", "--dry-run",
	})
	if code != ExitOK {
		t.Fatalf("code=%d stdout=%s", code, stdout)
	}
	for _, want := range []string{
		"rm -rf " + filepath.Join(prefix, "var") + "  (--purge)",
		"rm -rf " + filepath.Join(prefix, "logs") + "  (--purge)",
		"rm -rf " + prefix + "  (--purge)",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing --purge step %q in:\n%s", want, stdout)
		}
	}
}

// TestUninstallCenter_PreservesData_NoPurge actually runs the
// uninstall against a pre-seeded prefix. The install artefacts
// (versions/, current, etc/, bin/) should be gone; var/ + logs/
// stay put.
func TestUninstallCenter_PreservesData_NoPurge(t *testing.T) {
	prefix := t.TempDir()
	// Seed the layout the way `install center` lays it down.
	seedFiles := map[string]string{
		"versions/v2.5.0/VERSION":  "v2.5.0\n",
		"versions/v2.5.0/bin/x":    "noop",
		"etc/config.yaml":          "stub\n",
		"var/agent-center.db":      "sqlite-stub",
		"var/master.key":           "base64-secret",
		"var/worker-token":         "acat_stub",
		"logs/com.test.x.err.log":  "old logs",
	}
	for rel, body := range seedFiles {
		full := filepath.Join(prefix, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(prefix, "versions/v2.5.0"),
		filepath.Join(prefix, "current")); err != nil {
		t.Fatal(err)
	}
	cmd := UninstallCenterCommand()
	stdout, _, code := runHandler(t, cmd, []string{"--prefix=" + prefix})
	if code != ExitOK {
		t.Fatalf("code=%d stdout=%s", code, stdout)
	}
	// Install side gone.
	for _, rel := range []string{"versions", "current", "etc"} {
		if _, err := os.Stat(filepath.Join(prefix, rel)); !os.IsNotExist(err) {
			t.Errorf("install path %q survived uninstall", rel)
		}
	}
	// Data side preserved verbatim — same checksum.
	for rel, want := range map[string]string{
		"var/agent-center.db":     "sqlite-stub",
		"var/master.key":          "base64-secret",
		"var/worker-token":        "acat_stub",
		"logs/com.test.x.err.log": "old logs",
	} {
		got, err := os.ReadFile(filepath.Join(prefix, rel))
		if err != nil {
			t.Errorf("data path %q vanished: %v", rel, err)
			continue
		}
		if string(got) != want {
			t.Errorf("data path %q mutated: got %q want %q", rel, string(got), want)
		}
	}
}

// TestUninstallCenter_PurgeWipesEverything covers the destructive
// path: --purge --yes drops the whole prefix including the data.
func TestUninstallCenter_PurgeWipesEverything(t *testing.T) {
	prefix := t.TempDir()
	for _, rel := range []string{
		"versions/v2.5.0/VERSION", "etc/config.yaml",
		"var/agent-center.db", "var/master.key", "logs/x.err.log",
	} {
		full := filepath.Join(prefix, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("seed"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := UninstallCenterCommand()
	stdout, _, code := runHandler(t, cmd, []string{
		"--prefix=" + prefix, "--purge", "--yes",
	})
	if code != ExitOK {
		t.Fatalf("code=%d stdout=%s", code, stdout)
	}
	if _, err := os.Stat(prefix); !os.IsNotExist(err) {
		t.Fatalf("prefix %q survived --purge", prefix)
	}
}

// TestUninstallWorker_RequiresID surfaces the operator-friendly
// missing-arg error before doing anything dangerous.
func TestUninstallWorker_RequiresID(t *testing.T) {
	cmd := UninstallWorkerCommand()
	_, stderr, code := runHandler(t, cmd, []string{"--dry-run"})
	if code != ExitUsage {
		t.Fatalf("code=%d, want ExitUsage", code)
	}
	if !strings.Contains(stderr, "worker-id") {
		t.Errorf("stderr missing --worker-id hint: %s", stderr)
	}
}

// v2.5.17 (#72): launchd teardown uses `launchctl bootout
// gui/<uid> <plist>` rather than the deprecated `launchctl unload
// <plist>`. The deprecated path leaves a stale SMAppService entry in
// the operator's System Settings → Login Items → Allow in Background
// list on macOS Ventura+ even after the daemon stops and the plist
// is deleted; bootout removes both layers.
func TestServiceTeardownCmds_LaunchdUsesBootout(t *testing.T) {
	prevDomain := launchdGUIDomain
	defer func() { launchdGUIDomain = prevDomain }()
	launchdGUIDomain = func() string { return "gui/501" }
	sp := servicePaths{
		OS:              "darwin",
		ServiceManager:  "launchd",
		CenterUnitPath:  "/Users/x/Library/LaunchAgents/com.agent-center.center.plist",
		CenterServiceID: "com.agent-center.center",
	}
	got := serviceTeardownCmds(sp, sp.CenterServiceID)
	if len(got) != 1 {
		t.Fatalf("len(got)=%d want 1, steps=%v", len(got), got)
	}
	wantCmd := "launchctl bootout gui/501 /Users/x/Library/LaunchAgents/com.agent-center.center.plist"
	if got[0].Cmd != wantCmd {
		t.Fatalf("cmd=%q\nwant=%q", got[0].Cmd, wantCmd)
	}
	if !got[0].Tolerate {
		t.Fatalf("teardown step must tolerate non-zero exit (service may already be stopped)")
	}
}

func TestServiceTeardownCmds_SystemdUnchanged(t *testing.T) {
	sp := servicePaths{
		OS:              "linux",
		ServiceManager:  "systemd",
		UserMode:        true,
		CenterServiceID: "agent-center.service",
	}
	got := serviceTeardownCmds(sp, sp.CenterServiceID)
	if len(got) != 3 {
		t.Fatalf("len(got)=%d want 3", len(got))
	}
	wantCmds := []string{
		"systemctl --user stop agent-center.service",
		"systemctl --user disable agent-center.service",
		"systemctl --user daemon-reload",
	}
	for i, want := range wantCmds {
		if got[i].Cmd != want {
			t.Errorf("step %d cmd=%q want %q", i, got[i].Cmd, want)
		}
	}
}

// TestUninstallCommand_HasSubcommands sanity-check on the command tree.
func TestUninstallCommand_HasSubcommands(t *testing.T) {
	root := UninstallCommand()
	if root.Name != "uninstall" {
		t.Fatalf("name=%q", root.Name)
	}
	if root.Run != nil {
		t.Fatal("uninstall group should not have a Run (subcommand dispatch)")
	}
}
