package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/config"
)

// v2.7.1 #251: upgrading a pre-#141 worker (identity in the unit args, token
// leaking via ps/plist) backfills the config's worker: section (from the old
// unit args + var/worker-token) and rewrites the unit to the #249 --config-only
// form so the token leaves the plist. Fill-missing / no-clobber.

func seedOldWorkerInstall(t *testing.T, dir string) (installLayout, servicePaths) {
	t.Helper()
	layout := newInstallLayout(dir, "v2.7.1")
	if err := os.MkdirAll(layout.ConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(layout.DataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-#141 worker config: ONLY sqlite_path, no worker section, 0644.
	if err := os.WriteFile(layout.ConfigPath, []byte("server:\n  sqlite_path: \"/x/worker.db\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Persisted long-term token (0600) — the source the daemon actually uses.
	if err := os.WriteFile(workerTokenFile(layout), []byte("ltoken-xyz\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Old systemd unit carrying the identity + token in args (the leak).
	sp := servicePaths{
		ServiceManager:  "systemd",
		WorkerServiceID: "agent-center-worker.service",
		WorkerUnitPath:  filepath.Join(dir, "unit"),
		UserMode:        true,
	}
	oldUnit := "[Service]\nExecStart=/opt/bin/agent-center worker run --config=" + layout.ConfigPath +
		" --worker-id=w-1 --admin-target=tcp://host:7300 --admin-token=enroll-tok --server-fingerprint=sha256:AA:BB\n"
	if err := os.WriteFile(sp.WorkerUnitPath, []byte(oldUnit), 0o644); err != nil {
		t.Fatal(err)
	}
	return layout, sp
}

func TestMigrateWorkerConfigOnUpgrade_251_BackfillAndRewrite(t *testing.T) {
	dir := t.TempDir()
	layout, sp := seedOldWorkerInstall(t, dir)

	// ic empty → forces recovery from the old unit args + var/worker-token.
	if err := migrateWorkerConfigOnUpgrade(layout, sp, installContext{}, "/home/u"); err != nil {
		t.Fatal(err)
	}

	// Config now has a worker: section + 0600, sqlite_path preserved (no clobber).
	fi, _ := os.Stat(layout.ConfigPath)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("migrated config perms = %o, want 0600", fi.Mode().Perm())
	}
	cfg, err := config.Load(config.LoadOptions{Path: layout.ConfigPath})
	if err != nil {
		t.Fatalf("config.Load after migrate: %v", err)
	}
	if cfg.Server.SqlitePath != "/x/worker.db" {
		t.Errorf("sqlite_path clobbered: %q", cfg.Server.SqlitePath)
	}
	w := cfg.Worker
	if w.WorkerID != "w-1" || w.Bootstrap != "tcp://host:7300" || w.ServerFingerprint != "sha256:AA:BB" {
		t.Fatalf("worker fields from unit args wrong: %+v", w)
	}
	// token MUST come from var/worker-token (the long-term token), not the unit's
	// enroll token.
	if w.Token != "ltoken-xyz" {
		t.Fatalf("token = %q, want ltoken-xyz (from var/worker-token)", w.Token)
	}

	// Unit rewritten to --config-only — token GONE from the plist/unit.
	unit, _ := os.ReadFile(sp.WorkerUnitPath)
	us := string(unit)
	if !strings.Contains(us, "worker run --config="+layout.ConfigPath) {
		t.Fatalf("unit not rewritten to --config-only form:\n%s", us)
	}
	for _, leak := range []string{"--admin-token", "enroll-tok", "ltoken-xyz", "--worker-id", "--admin-target"} {
		if strings.Contains(us, leak) {
			t.Fatalf("unit still leaks %q after rewrite:\n%s", leak, us)
		}
	}
}

// No-clobber: a config that already has a worker: section (#141+ install) is
// left untouched.
func TestMigrateWorkerConfigOnUpgrade_251_NoClobber(t *testing.T) {
	dir := t.TempDir()
	layout, sp := seedOldWorkerInstall(t, dir)
	// Replace with a #141-style config that already has worker:.
	existing := "server:\n  sqlite_path: \"/x/worker.db\"\nworker:\n  worker_id: \"already-set\"\n  bootstrap: \"tcp://keep:1\"\n"
	if err := os.WriteFile(layout.ConfigPath, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := migrateWorkerConfigOnUpgrade(layout, sp, installContext{WorkerID: "should-not-apply"}, "/home/u"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(layout.ConfigPath)
	if string(got) != existing {
		t.Fatalf("config with existing worker: must be untouched (no clobber), got:\n%s", got)
	}
}

func TestParseWorkerUnitArgs_251(t *testing.T) {
	// launchd plist form (each arg wrapped in <string>).
	plist := "<array><string>worker</string><string>run</string>" +
		"<string>--worker-id=w-9</string><string>--admin-target=tcp://h:7300</string>" +
		"<string>--admin-token=tok9</string><string>--server-fingerprint=sha256:CC</string></array>"
	got := parseWorkerUnitArgs(plist)
	if got["--worker-id"] != "w-9" || got["--admin-target"] != "tcp://h:7300" ||
		got["--admin-token"] != "tok9" || got["--server-fingerprint"] != "sha256:CC" {
		t.Fatalf("plist parse = %+v", got)
	}
	// systemd ExecStart form (space-separated).
	systemd := "ExecStart=/bin/agent-center worker run --config=/c --worker-id=w-7 --admin-target=unix:/run/a.sock --admin-token=tok7\n"
	g2 := parseWorkerUnitArgs(systemd)
	if g2["--worker-id"] != "w-7" || g2["--admin-target"] != "unix:/run/a.sock" || g2["--admin-token"] != "tok7" {
		t.Fatalf("systemd parse = %+v", g2)
	}
}
