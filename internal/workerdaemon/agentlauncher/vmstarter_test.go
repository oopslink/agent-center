package agentlauncher

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTartGuestRuntimeArgs_RewritesSocketAndConfigPaths(t *testing.T) {
	configPath := filepath.Join("/host", "agent-center", "config.toml")
	got := tartGuestRuntimeArgs([]string{
		"--worker-id", "w1",
		"--sock-dir", "/tmp/host-socks",
		"--config", configPath,
		"--admin-target", "http://127.0.0.1:7300",
	}, "/tmp/guest-socks", configPath, "/host/state")
	wantConfig := filepath.Join("/Volumes/My Shared Files", "agent-center-config", "config.toml")
	if got[3] != "/tmp/guest-socks" {
		t.Fatalf("sock dir = %q", got[3])
	}
	if got[5] != wantConfig {
		t.Fatalf("config = %q, want %q", got[5], wantConfig)
	}
	if got[7] != "http://192.168.64.1:7300" {
		t.Fatalf("admin target = %q", got[7])
	}
	if got[len(got)-2] != "--agent-home-base" || got[len(got)-1] != filepath.Join("/Volumes/My Shared Files", "agent-center-state") {
		t.Fatalf("missing guest home base override: %v", got)
	}
}

func TestTartGuestAdminTargetOverride(t *testing.T) {
	t.Setenv("AC_SANDBOX_HOST_ADMIN_TARGET", "https://host.tart.internal:7300")
	if got := tartGuestAdminTarget("http://127.0.0.1:7300"); got != "https://host.tart.internal:7300" {
		t.Fatalf("admin target override = %q", got)
	}
}

func TestTartGuestMountPlanAndEnvMountsSkillsAndMemorySources(t *testing.T) {
	homeBase := t.TempDir()
	codexHome := t.TempDir()
	claudeConfig := t.TempDir()
	builtinSkills := t.TempDir()
	starter, err := NewTartVMStarter(TartVMStarterConfig{
		BinaryPath: filepath.Join(t.TempDir(), "agent-center"),
		BaseEnv: []string{
			"PATH=/usr/bin",
			"CODEX_HOME=" + codexHome,
			"CLAUDE_CONFIG_DIR=" + claudeConfig,
			"CLAUDE_BUILTIN_SKILLS_DIR=" + builtinSkills,
		},
		HomeBase: homeBase,
		SockDir:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewTartVMStarter: %v", err)
	}
	agentHome := filepath.Join(homeBase, "agents", "agent-1")
	plan := starter.guestMountPlan("agent-1", agentHome, starter.binaryPath, "")
	wantMounts := map[string]bool{
		"agent-center-state":                 true,
		"agent-center-bin":                   true,
		"agent-center-codex-source":          true,
		"agent-center-claude-config":         true,
		"agent-center-claude-builtin-skills": true,
	}
	for _, m := range plan.mounts {
		delete(wantMounts, m.name)
	}
	if len(wantMounts) != 0 {
		t.Fatalf("missing mounts: %v; got %+v", wantMounts, plan.mounts)
	}
	env := tartGuestEnv(starter.baseEnv, plan)
	got := map[string]string{}
	for _, entry := range env {
		k, v, ok := strings.Cut(entry, "=")
		if ok {
			got[k] = v
		}
	}
	if got["CODEX_HOME"] != tartGuestSharePath("agent-center-codex-source") {
		t.Fatalf("guest CODEX_HOME = %q", got["CODEX_HOME"])
	}
	if got["CLAUDE_CONFIG_DIR"] != tartGuestSharePath("agent-center-claude-config") {
		t.Fatalf("guest CLAUDE_CONFIG_DIR = %q", got["CLAUDE_CONFIG_DIR"])
	}
	if got["CLAUDE_BUILTIN_SKILLS_DIR"] != tartGuestSharePath("agent-center-claude-builtin-skills") {
		t.Fatalf("guest CLAUDE_BUILTIN_SKILLS_DIR = %q", got["CLAUDE_BUILTIN_SKILLS_DIR"])
	}
	if got["PATH"] != "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin" {
		t.Fatalf("guest PATH = %q", got["PATH"])
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
