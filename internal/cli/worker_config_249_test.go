package cli

import (
	"os"
	"testing"

	"github.com/oopslink/agent-center/internal/config"
)

// v2.7.1 #249: writeWorkerConfig writes the worker enrollment identity into
// config.yaml at 0600, and config.Load round-trips it (which also proves the
// known-keys allowlist covers worker.* — an omission would reject the file with
// "unknown YAML key", the #211 lesson).
func TestWriteWorkerConfig_249_PermsAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	layout := newInstallLayout(dir, "v2.7.1")
	ic := installContext{
		WorkerID: "w-1", WorkerName: "My Worker", Bootstrap: "tcp://host:7300",
		Token: "acat_secret_token", Fingerprint: "sha256:AA:BB",
	}
	if err := writeWorkerConfig(layout, ic); err != nil {
		t.Fatal(err)
	}

	// 0600 — the file holds the token; it must not be world/group readable.
	fi, err := os.Stat(layout.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("config perms = %o, want 0600 (#249 token protection)", fi.Mode().Perm())
	}

	// config.Load parses the worker section (errors here would mean the
	// known-keys allowlist is missing worker.* → #211 regression).
	cfg, err := config.Load(config.LoadOptions{Path: layout.ConfigPath})
	if err != nil {
		t.Fatalf("config.Load (known-keys must cover worker.*): %v", err)
	}
	w := cfg.Worker
	if w.WorkerID != "w-1" || w.WorkerName != "My Worker" || w.Bootstrap != "tcp://host:7300" ||
		w.Token != "acat_secret_token" || w.ServerFingerprint != "sha256:AA:BB" {
		t.Fatalf("worker config round-trip mismatch: %+v", w)
	}
}

func TestFirstNonEmptyWorker_249(t *testing.T) {
	if got := firstNonEmptyWorker("flagval", "cfgval"); got != "flagval" {
		t.Fatalf("flag must override config: got %q", got)
	}
	if got := firstNonEmptyWorker("", "cfgval"); got != "cfgval" {
		t.Fatalf("empty flag must fall back to config: got %q", got)
	}
	if got := firstNonEmptyWorker("  ", "cfgval"); got != "cfgval" {
		t.Fatalf("whitespace flag must fall back to config: got %q", got)
	}
}
