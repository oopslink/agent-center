package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// v2.4-D-A1 tests for install subcommand skeleton.

func TestDetectExistingInstall_NoPrefix(t *testing.T) {
	_, _, err := detectExistingInstall("", "v2.4.0")
	if err == nil {
		t.Fatal("expected error on empty prefix")
	}
}

func TestDetectExistingInstall_MissingPrefix_IsFresh(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "no-such-prefix")
	state, ver, err := detectExistingInstall(dir, "v2.4.0")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if state != InstallStateFresh {
		t.Fatalf("state=%s, want fresh", state)
	}
	if ver != "" {
		t.Fatalf("version=%q, want empty", ver)
	}
}

func TestDetectExistingInstall_SameVersion(t *testing.T) {
	dir := t.TempDir()
	versionsDir := filepath.Join(dir, "versions", "v2.4.0")
	if err := os.MkdirAll(versionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionsDir, "VERSION"), []byte("v2.4.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(versionsDir, filepath.Join(dir, "current")); err != nil {
		t.Fatal(err)
	}
	state, ver, err := detectExistingInstall(dir, "v2.4.0")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if state != InstallStateSameVersion {
		t.Fatalf("state=%s, want same-version", state)
	}
	if ver != "v2.4.0" {
		t.Fatalf("ver=%q, want v2.4.0", ver)
	}
}

func TestDetectExistingInstall_Upgrade(t *testing.T) {
	dir := t.TempDir()
	versionsDir := filepath.Join(dir, "versions", "v2.3.5")
	if err := os.MkdirAll(versionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionsDir, "VERSION"), []byte("v2.3.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(versionsDir, filepath.Join(dir, "current")); err != nil {
		t.Fatal(err)
	}
	state, ver, err := detectExistingInstall(dir, "v2.4.0")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if state != InstallStateUpgrade {
		t.Fatalf("state=%s, want upgrade", state)
	}
	if ver != "v2.3.5" {
		t.Fatalf("ver=%q, want v2.3.5", ver)
	}
}

func TestDetectExistingInstall_CurrentNoVersionFile_Errors(t *testing.T) {
	dir := t.TempDir()
	versionsDir := filepath.Join(dir, "versions", "v2.4.0")
	if err := os.MkdirAll(versionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// VERSION file deliberately missing.
	if err := os.Symlink(versionsDir, filepath.Join(dir, "current")); err != nil {
		t.Fatal(err)
	}
	_, _, err := detectExistingInstall(dir, "v2.4.0")
	if err == nil || !strings.Contains(err.Error(), "no VERSION file") {
		t.Fatalf("expected no-VERSION error, got %v", err)
	}
}

func TestDefaultInstallPrefix_Linux_System(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	if got := defaultInstallPrefix(false); got != "/opt/agent-center" {
		t.Errorf("linux system mode default = %q", got)
	}
}

func TestDefaultInstallPrefix_Linux_User(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	got := defaultInstallPrefix(true)
	if !strings.HasSuffix(got, "/.local/share/agent-center") {
		t.Errorf("linux user mode default = %q, want suffix .local/share/agent-center", got)
	}
}

func TestDefaultInstallPrefix_Mac(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("mac-only")
	}
	got := defaultInstallPrefix(true)
	if !strings.Contains(got, "Library/Application Support/agent-center") {
		t.Errorf("mac default = %q", got)
	}
	// Mac always returns Library dir regardless of userMode flag (per
	// commented spec).
	got2 := defaultInstallPrefix(false)
	if got != got2 {
		t.Errorf("mac userMode flag should be no-op: %q vs %q", got, got2)
	}
}

func TestInstallCommand_HasSubcommands(t *testing.T) {
	root := InstallCommand()
	if root.Name != "install" {
		t.Fatalf("name=%q", root.Name)
	}
	if root.Run != nil {
		t.Fatal("install group should not have a Run (subcommand dispatch)")
	}
}

func TestInstallCenterCommand_RequiresNothing(t *testing.T) {
	cmd := InstallCenterCommand()
	if cmd.Flags == nil {
		t.Fatal("install center should have Flags")
	}
}

func TestInstallWorkerCommand_RejectsMissingBootstrap(t *testing.T) {
	cmd := InstallWorkerCommand()
	_, stderr, code := runHandler(t, cmd, []string{"--token=abc"})
	if code != ExitUsage {
		t.Fatalf("missing --bootstrap should be ExitUsage, got %d", code)
	}
	if !strings.Contains(stderr, "bootstrap") {
		t.Errorf("stderr should mention bootstrap: %q", stderr)
	}
}

func TestInstallWorkerCommand_RejectsMissingToken(t *testing.T) {
	cmd := InstallWorkerCommand()
	_, stderr, code := runHandler(t, cmd, []string{"--bootstrap=tcp://x@y:7300"})
	if code != ExitUsage {
		t.Fatalf("missing --token should be ExitUsage, got %d", code)
	}
	if !strings.Contains(stderr, "token") {
		t.Errorf("stderr should mention token: %q", stderr)
	}
}

func TestInstallCenter_DryRun(t *testing.T) {
	cmd := InstallCenterCommand()
	prefix := t.TempDir()
	stdout, _, code := runHandler(t, cmd, []string{"--prefix=" + prefix, "--dry-run"})
	if code != ExitOK {
		t.Fatalf("dry-run on fresh prefix should ExitOK, got %d", code)
	}
	if !strings.Contains(stdout, "[dry-run]") || !strings.Contains(stdout, "state:") {
		t.Errorf("dry-run output missing key lines: %s", stdout)
	}
}

func TestInstallWorker_DryRun(t *testing.T) {
	cmd := InstallWorkerCommand()
	prefix := t.TempDir()
	stdout, _, code := runHandler(t, cmd, []string{
		"--prefix=" + prefix, "--dry-run",
		"--bootstrap=tcp://x@y:7300", "--token=abc",
	})
	if code != ExitOK {
		t.Fatalf("dry-run on fresh prefix should ExitOK, got %d", code)
	}
	if !strings.Contains(stdout, "[dry-run]") || !strings.Contains(stdout, "worker-id:") {
		t.Errorf("dry-run output missing key lines: %s", stdout)
	}
}

func TestInstallCenter_SameVersion_NoOp(t *testing.T) {
	// Set up an existing same-version install.
	prefix := t.TempDir()
	version := installerVersion() // matches what handler will use
	versionsDir := filepath.Join(prefix, "versions", version)
	if err := os.MkdirAll(versionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionsDir, "VERSION"), []byte(version+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(versionsDir, filepath.Join(prefix, "current")); err != nil {
		t.Fatal(err)
	}

	cmd := InstallCenterCommand()
	stdout, _, code := runHandler(t, cmd, []string{"--prefix=" + prefix})
	if code != ExitOK {
		t.Fatalf("same-version should ExitOK, got %d", code)
	}
	if !strings.Contains(stdout, "already installed") {
		t.Errorf("stdout should say already installed: %s", stdout)
	}
}

func TestInstallerVersion_Fallback(t *testing.T) {
	// Default installBuildVersion is the empty closure → "dev"
	if got := installerVersion(); got != "dev" {
		t.Errorf("default version = %q, want dev", got)
	}
	// Override
	prior := installBuildVersion
	installBuildVersion = func() string { return "v9.9.9-test" }
	defer func() { installBuildVersion = prior }()
	if got := installerVersion(); got != "v9.9.9-test" {
		t.Errorf("overridden version = %q", got)
	}
}
