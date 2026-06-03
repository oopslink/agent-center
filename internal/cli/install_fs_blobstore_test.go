package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oopslink/agent-center/internal/config"
)

// v2.7 #159: the installed center config MUST wire a blob_store root, else
// FilesSvc is nil and every /api/files upload returns 501 (#133/#142 file
// attachments broken on a fresh install). This was #142's acceptance blind-spot
// (tests used a custom config, not the install-generated one). Parse the
// generated config and assert the blob root resolves under the data dir (and
// that the YAML loads with no unknown-key rejection).
func TestCenterConfigYAML_WiresBlobStoreRoot(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "var")
	yaml := centerConfigYAML(dataDir, 7100, "", "", filepath.Join(dataDir, "master.key"))
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.LoadOptions{
		Path: path,
		Env:  func(string) (string, bool) { return "", false },
	})
	if err != nil {
		t.Fatalf("generated install config failed to load: %v", err)
	}
	want := dataDir + "/blobs"
	if cfg.BlobStore.Root != want {
		t.Fatalf("BlobStore.Root = %q, want %q (empty/unwritable → FilesSvc nil → /api/files 501)", cfg.BlobStore.Root, want)
	}

	// v2.7 #161: server must not default to :7000 (macOS AirPlay Receiver holds
	// 7000 → center can't bind on a fresh Mac install), and must differ from the
	// web console port (separate listeners).
	if cfg.Server.ListenAddr == ":7000" {
		t.Fatalf("Server.ListenAddr is :7000 — collides with macOS AirPlay Receiver; center won't bind")
	}
	if cfg.Server.ListenAddr == cfg.WebConsole.ListenAddr {
		t.Fatalf("Server.ListenAddr (%q) must not equal WebConsole.ListenAddr (%q) — separate listeners",
			cfg.Server.ListenAddr, cfg.WebConsole.ListenAddr)
	}
}
