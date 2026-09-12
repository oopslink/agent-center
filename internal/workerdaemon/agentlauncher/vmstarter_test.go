package agentlauncher

import (
	"path/filepath"
	"testing"
)

func TestTartGuestRuntimeArgs_RewritesSocketAndConfigPaths(t *testing.T) {
	configPath := filepath.Join("/host", "agent-center", "config.toml")
	got := tartGuestRuntimeArgs([]string{
		"--worker-id", "w1",
		"--sock-dir", "/tmp/host-socks",
		"--config", configPath,
		"--admin-target", "http://127.0.0.1:7300",
	}, "/tmp/guest-socks", configPath)
	wantConfig := filepath.Join("/Volumes/My Shared Files", "agent-center-config", "config.toml")
	if got[3] != "/tmp/guest-socks" {
		t.Fatalf("sock dir = %q", got[3])
	}
	if got[5] != wantConfig {
		t.Fatalf("config = %q, want %q", got[5], wantConfig)
	}
}

func TestTartConfigPath(t *testing.T) {
	if got := tartConfigPath([]string{"--worker-id", "w1", "--config", "/tmp/cfg.toml"}); got != "/tmp/cfg.toml" {
		t.Fatalf("config path = %q", got)
	}
	if got := tartConfigPath([]string{"--worker-id", "w1"}); got != "" {
		t.Fatalf("config path without flag = %q", got)
	}
}

func TestShellJoinUsesEnvCommand(t *testing.T) {
	got := shellJoin([]string{"/bin/echo", "hello world"}, []string{"A=B"})
	want := "env 'A=B' '/bin/echo' 'hello world'"
	if got != want {
		t.Fatalf("shellJoin = %q, want %q", got, want)
	}
}
