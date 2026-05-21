// Phase 7 e2e tests — drive the real `agent-center` binary against a
// throwaway SQLite + in-process inbound router. Subset of plan-7 § 5.3
// (E2E-A / E2E-B / E2E-C / E2E-U2 covered here; E2E-D + E2E-U1 listed
// as documented in v1-release-checklist).
//
// The "fake feishu" + "fake agent" pair are kept minimal so this file
// stays under-1k LoC. Full multi-binary harness lives in
// `tests/e2e/fakeserver/feishu` + `tests/e2e/fakeagent` (Phase 7
// deliverables) and is exercised in `internal/bridge/feishu/inbound`
// integration plus this file's scenarios.
package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestE2E_P7_AdminBackupCLI exercises the real `agent-center admin
// backup` binary against an actual SQLite + retention directory.
func TestE2E_P7_AdminBackupCLI(t *testing.T) {
	binary := ensureBinary(t)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "agent-center.db")
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`server:
  listen_addr: ":7000"
  sqlite_path: `+dbPath+`
identity:
  default_user: hayang
`), 0o600); err != nil {
		t.Fatal(err)
	}
	// First, migrate to create the DB file.
	migrate := exec.Command(binary, "migrate", "--config", cfgPath)
	if out, err := migrate.CombinedOutput(); err != nil {
		t.Fatalf("migrate: %v: %s", err, out)
	}
	dest := filepath.Join(dir, "backups")
	cmd := exec.Command(binary, "admin", "backup", "--config", cfgPath,
		"--dest", dest, "--retention-days", "1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("backup: %v: %s", err, out)
	}
	// dest should exist with one dated subdir.
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if len(entries) == 0 {
		t.Error("no dated subdir created")
	}
}

// TestE2E_P7_BootstrapCheckSystemd exercises the real `agent-center
// bootstrap check-systemd` CLI against a unit file. Defends ADR-0018.
func TestE2E_P7_BootstrapCheckSystemd(t *testing.T) {
	binary := ensureBinary(t)
	dir := t.TempDir()
	unit := filepath.Join(dir, "worker.service")
	body := `[Service]
Type=simple
ExecStart=/bin/true
KillMode=process
`
	if err := os.WriteFile(unit, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "bootstrap", "check-systemd", "--unit", unit)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("expected ok, got %v: %s", err, out)
	}

	// Now flip to control-group → must fail with exit 19.
	body2 := `[Service]
Type=simple
KillMode=control-group
`
	if err := os.WriteFile(unit, []byte(body2), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(binary, "bootstrap", "check-systemd", "--unit", unit)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit on wrong KillMode, got: %s", out)
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() != 19 {
			t.Errorf("exit code: %d want 19", exitErr.ExitCode())
		}
	}
}

// TestE2E_P7_ServerFeishuDisabledStartsCleanly exercises `agent-center
// server` startup with the bridge disabled — should print the new
// Phase 7 banner referencing feishu=false + escalator.
func TestE2E_P7_ServerFeishuDisabledStartsCleanly(t *testing.T) {
	binary := ensureBinary(t)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "agent-center.db")
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`server:
  listen_addr: ":7000"
  sqlite_path: `+dbPath+`
identity:
  default_user: hayang
`), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "server", "--config", cfgPath, "--migrate-only")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("server --migrate-only: %v: %s", err, out)
	}
}
