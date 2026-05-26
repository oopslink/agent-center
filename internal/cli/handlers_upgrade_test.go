package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUpgradeCenter_RefusesFresh covers the key behaviour difference
// vs `install center`: with no install at the prefix, install walks
// the fresh path; upgrade refuses with a clear error pointing the
// operator at `install center` for fresh.
func TestUpgradeCenter_RefusesFresh(t *testing.T) {
	cmd := UpgradeCenterCommand()
	prefix := t.TempDir() // empty — never installed
	_, stderr, code := runHandler(t, cmd, []string{"--prefix=" + prefix, "--dry-run"})
	if code != ExitBusinessError {
		t.Fatalf("code=%d, want ExitBusinessError", code)
	}
	if !strings.Contains(stderr, "upgrade_no_install") {
		t.Errorf("stderr missing error code: %s", stderr)
	}
	if !strings.Contains(stderr, "install center") {
		t.Errorf("stderr missing recovery hint pointing at install center: %s", stderr)
	}
}

// TestUpgradeCenter_NoOpOnSameVersion — when current==this, both
// install and upgrade walk the "already installed" idempotent path.
func TestUpgradeCenter_NoOpOnSameVersion(t *testing.T) {
	prefix := t.TempDir()
	// Seed the layout for the installer's own version so detection
	// classifies the prefix as SameVersion.
	verDir := filepath.Join(prefix, "versions", installerVersion())
	if err := os.MkdirAll(verDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(verDir, "VERSION"), []byte(installerVersion()+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(verDir, filepath.Join(prefix, "current")); err != nil {
		t.Fatal(err)
	}
	cmd := UpgradeCenterCommand()
	stdout, _, code := runHandler(t, cmd, []string{"--prefix=" + prefix})
	if code != ExitOK {
		t.Fatalf("code=%d stdout=%s", code, stdout)
	}
	if !strings.Contains(stdout, "already installed") {
		t.Errorf("expected no-op message; got: %s", stdout)
	}
}

// TestUpgradeCenter_DryRun shows the planned state without mutating.
func TestUpgradeCenter_DryRun_SameVersion(t *testing.T) {
	prefix := t.TempDir()
	verDir := filepath.Join(prefix, "versions", installerVersion())
	_ = os.MkdirAll(verDir, 0o755)
	_ = os.WriteFile(filepath.Join(verDir, "VERSION"), []byte(installerVersion()+"\n"), 0o644)
	_ = os.Symlink(verDir, filepath.Join(prefix, "current"))
	cmd := UpgradeCenterCommand()
	stdout, _, code := runHandler(t, cmd, []string{"--prefix=" + prefix, "--dry-run"})
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(stdout, "[dry-run]") {
		t.Errorf("missing dry-run marker: %s", stdout)
	}
	if !strings.Contains(stdout, "state:") {
		t.Errorf("missing state line: %s", stdout)
	}
}

func TestUpgradeWorker_RequiresID(t *testing.T) {
	cmd := UpgradeWorkerCommand()
	_, stderr, code := runHandler(t, cmd, []string{"--dry-run"})
	if code != ExitUsage {
		t.Fatalf("code=%d, want ExitUsage", code)
	}
	if !strings.Contains(stderr, "worker-id") {
		t.Errorf("stderr missing worker-id hint: %s", stderr)
	}
}

func TestUpgradeCommand_HasSubcommands(t *testing.T) {
	root := UpgradeCommand()
	if root.Name != "upgrade" {
		t.Fatalf("name=%q", root.Name)
	}
	if root.Run != nil {
		t.Fatal("upgrade group should not have a Run (subcommand dispatch)")
	}
}
