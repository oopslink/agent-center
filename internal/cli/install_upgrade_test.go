package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// v2.4-D-A5 tests for upgrade orchestration helpers.

func TestRollbackSymlink_RestoresTarget(t *testing.T) {
	prefix := t.TempDir()
	prevVer := filepath.Join(prefix, "versions", "v2.4.0")
	newVer := filepath.Join(prefix, "versions", "v2.4.1")
	if err := os.MkdirAll(prevVer, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newVer, 0o755); err != nil {
		t.Fatal(err)
	}
	currentLink := filepath.Join(prefix, "current")
	if err := os.Symlink(newVer, currentLink); err != nil {
		t.Fatal(err)
	}
	// Use a dummy ServiceManager so installShouldActivate returns false
	sp := servicePaths{ServiceManager: ""}
	var out, errw bytes.Buffer
	if err := rollbackSymlink(&out, &errw, currentLink, prevVer, sp, "x"); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(currentLink)
	if err != nil {
		t.Fatal(err)
	}
	if target != prevVer {
		t.Fatalf("after rollback symlink → %q, want %q", target, prevVer)
	}
	if !strings.Contains(errw.String(), "rollback complete") {
		t.Errorf("rollback message missing: %s", errw.String())
	}
}

func TestPollHealth_SucceedsImmediately(t *testing.T) {
	calls := 0
	probe := func() error {
		calls++
		return nil
	}
	if err := pollHealth(probe, 1*time.Second); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestPollHealth_TimesOut(t *testing.T) {
	probe := func() error { return errors.New("not ready") }
	start := time.Now()
	err := pollHealth(probe, 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "not ready") {
		t.Errorf("err = %v", err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Errorf("timed out too fast: %s", elapsed)
	}
}

func TestPollHealth_EventuallySucceeds(t *testing.T) {
	calls := 0
	probe := func() error {
		calls++
		if calls < 3 {
			return errors.New("retry")
		}
		return nil
	}
	if err := pollHealth(probe, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if calls < 3 {
		t.Errorf("expected ≥3 calls, got %d", calls)
	}
}

func TestUpgradeService_SymlinkSwapHappens(t *testing.T) {
	t.Setenv("AGENT_CENTER_INSTALL_SKIP_ACTIVATE", "1")
	prefix := t.TempDir()
	prevVer := filepath.Join(prefix, "versions", "v2.4.0")
	newVer := filepath.Join(prefix, "versions", "v2.4.1")
	if err := os.MkdirAll(prevVer, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newVer, 0o755); err != nil {
		t.Fatal(err)
	}
	currentLink := filepath.Join(prefix, "current")
	if err := os.Symlink(prevVer, currentLink); err != nil {
		t.Fatal(err)
	}
	layout := newInstallLayout(prefix, "v2.4.1")
	sp := servicePaths{ServiceManager: "launchd", CenterServiceID: "x", CenterUnitPath: "/tmp/x.plist"}
	var out, errw bytes.Buffer
	err := upgradeService(&out, &errw, layout, sp, sp.CenterServiceID, nil)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := os.Readlink(currentLink)
	if target != newVer {
		t.Fatalf("symlink target = %q, want %q", target, newVer)
	}
	if !strings.Contains(out.String(), "rollback target") {
		t.Errorf("expected rollback target log: %s", out.String())
	}
}

func TestCenterHealthProbe_NonExistentSocket(t *testing.T) {
	layout := newInstallLayout("/nonexistent-prefix", "v0")
	probe := centerHealthProbe(layout)
	if err := probe(); err == nil {
		t.Fatal("expected error for missing socket")
	}
}

func TestWorkerHealthProbe_Always(t *testing.T) {
	// v0 worker probe is a no-op (deferred per A5 doc); just verify
	// it returns nil so upgrade flow doesn't artificially fail.
	probe := workerHealthProbe(servicePaths{}, "x")
	if err := probe(); err != nil {
		t.Errorf("worker probe should be no-op for v0, got %v", err)
	}
}
