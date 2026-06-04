package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// v2.7.1 #234: detectExistingInstall disambiguates a same-version build by
// its git commit (the COMMIT marker) so a same-version-different-commit
// rebuild is correctly classified as an upgrade — the root-cause fix for the
// #217 dogfood bug where an in-place upgrade silently skipped the binary swap.

// withInstallBuildCommit sets the installerCommit() seam for the duration of a
// test, restoring it after.
func withInstallBuildCommit(t *testing.T, commit string) {
	t.Helper()
	prior := installBuildCommit
	installBuildCommit = func() string { return commit }
	t.Cleanup(func() { installBuildCommit = prior })
}

// seedInstall lays out <dir>/current → versions/<version> with VERSION and
// (optionally) a COMMIT marker.
func seedInstall(t *testing.T, dir, version, commit string, withCommit bool) {
	t.Helper()
	versionsDir := filepath.Join(dir, "versions", version)
	if err := os.MkdirAll(versionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionsDir, "VERSION"), []byte(version+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if withCommit {
		if err := os.WriteFile(filepath.Join(versionsDir, "COMMIT"), []byte(commit+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(versionsDir, filepath.Join(dir, "current")); err != nil {
		t.Fatal(err)
	}
}

func TestDetectExistingInstall_234_SameVersionSameCommit_IsNoOp(t *testing.T) {
	withInstallBuildCommit(t, "abc1234")
	dir := t.TempDir()
	seedInstall(t, dir, "v2.7.1", "abc1234", true)

	state, ver, err := detectExistingInstall(dir, "v2.7.1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if state != InstallStateSameVersion {
		t.Fatalf("state=%s, want same-version (same commit → idempotent no-op)", state)
	}
	if ver != "v2.7.1" {
		t.Fatalf("ver=%q, want v2.7.1", ver)
	}
}

func TestDetectExistingInstall_234_SameVersionDifferentCommit_IsUpgrade(t *testing.T) {
	withInstallBuildCommit(t, "def5678") // this build's commit
	dir := t.TempDir()
	seedInstall(t, dir, "v2.7.1", "abc1234", true) // installed at an older commit

	state, ver, err := detectExistingInstall(dir, "v2.7.1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if state != InstallStateUpgrade {
		t.Fatalf("state=%s, want upgrade (same version, different commit = the #234 fix)", state)
	}
	if ver != "v2.7.1" {
		t.Fatalf("ver=%q, want v2.7.1", ver)
	}
}

func TestDetectExistingInstall_234_SameVersionLegacyNoCommitFile_IsNoOp(t *testing.T) {
	withInstallBuildCommit(t, "def5678")
	dir := t.TempDir()
	seedInstall(t, dir, "v2.7.1", "", false) // legacy install: VERSION only, no COMMIT marker

	state, _, err := detectExistingInstall(dir, "v2.7.1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if state != InstallStateSameVersion {
		t.Fatalf("state=%s, want same-version (legacy no-COMMIT falls back to version-only)", state)
	}
}

func TestApplyForceState_234(t *testing.T) {
	if got := applyForceState(InstallStateSameVersion, true); got != InstallStateUpgrade {
		t.Fatalf("force same-version → %s, want upgrade", got)
	}
	if got := applyForceState(InstallStateSameVersion, false); got != InstallStateSameVersion {
		t.Fatalf("no-force same-version → %s, want same-version", got)
	}
	// Force never downgrades / alters the other classifications.
	for _, s := range []InstallState{InstallStateFresh, InstallStateUpgrade, InstallStateUnknown} {
		if got := applyForceState(s, true); got != s {
			t.Fatalf("force %s → %s, want unchanged", s, got)
		}
	}
}

// writeVersionFile must persist the COMMIT marker so the next
// detectExistingInstall can compare commits (round-trip).
func TestWriteVersionFile_234_PersistsCommit(t *testing.T) {
	withInstallBuildCommit(t, "abc1234")
	dir := t.TempDir()
	layout := newInstallLayout(dir, "v2.7.1")
	if err := os.MkdirAll(layout.VersionedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeVersionFile(layout); err != nil {
		t.Fatalf("writeVersionFile: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(layout.VersionedDir, "COMMIT"))
	if err != nil {
		t.Fatalf("read COMMIT: %v", err)
	}
	if string(got) != "abc1234\n" {
		t.Fatalf("COMMIT=%q, want abc1234\\n", string(got))
	}
}
