package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// v2.7 #199 follow-up: bare `agent-center server` (no --config) must pick up the
// user-mode install config (~/.agent-center/etc/config.yaml) instead of falling
// back to the system /var/lib defaults that need root.
func TestDiscoverDefaultConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got := discoverDefaultConfigPath(); got != "" {
		t.Fatalf("no install config → want empty, got %q", got)
	}

	etc := filepath.Join(home, ".agent-center", "etc")
	if err := os.MkdirAll(etc, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(etc, "config.yaml")
	if err := os.WriteFile(p, []byte("server: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := discoverDefaultConfigPath(); got != p {
		t.Fatalf("install config present → want %q, got %q", p, got)
	}
}

// loadConfigForCLI with no --config / env discovers the user-install config and
// uses ITS sqlite_path (user-writable) rather than the system default.
func TestLoadConfigForCLI_DiscoversUserInstallConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	SetGlobalConfigPath("")
	t.Cleanup(func() { SetGlobalConfigPath("") })

	// No install config → built-in system default db path.
	cfg, err := loadConfigForCLI("", nil)
	if err != nil {
		t.Fatalf("default load: %v", err)
	}
	if cfg.Server.SqlitePath != "/var/lib/agent-center/agent-center.db" {
		t.Fatalf("no install → want system default db path, got %q", cfg.Server.SqlitePath)
	}

	// Write a user install config → bare load discovers it.
	etc := filepath.Join(home, ".agent-center", "etc")
	if err := os.MkdirAll(etc, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(home, ".agent-center", "var", "agent-center.db")
	body := "server:\n  sqlite_path: \"" + dbPath + "\"\n  admin_socket_path: \"" +
		filepath.Join(home, ".agent-center", "var", "admin.sock") + "\"\n"
	if err := os.WriteFile(filepath.Join(etc, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg2, err := loadConfigForCLI("", nil)
	if err != nil {
		t.Fatalf("discover load: %v", err)
	}
	if cfg2.Server.SqlitePath != dbPath {
		t.Fatalf("install present → want user db path %q, got %q", dbPath, cfg2.Server.SqlitePath)
	}
}
