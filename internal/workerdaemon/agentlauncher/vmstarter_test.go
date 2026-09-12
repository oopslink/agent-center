package agentlauncher

import (
	"os"
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

func TestTartGuestAdminTargetRewritesTCPLocalhost(t *testing.T) {
	if got := tartGuestAdminTarget("tcp://localhost:7300"); got != "tcp://192.168.64.1:7300" {
		t.Fatalf("tcp localhost admin target = %q", got)
	}
	if got := tartGuestAdminTarget("tcp://127.0.0.1:7300"); got != "tcp://192.168.64.1:7300" {
		t.Fatalf("tcp 127 admin target = %q", got)
	}
}

func TestTartVMStarterSSHIdentityFile(t *testing.T) {
	homeBase := t.TempDir()
	starter, err := NewTartVMStarter(TartVMStarterConfig{
		BinaryPath: filepath.Join(t.TempDir(), "agent-center"),
		BaseEnv:    []string{"PATH=/usr/bin"},
		HomeBase:   homeBase,
		SockDir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewTartVMStarter: %v", err)
	}
	agentID := "agent-1"
	runDirKey := filepath.Join(homeBase, "agents", agentID, "sandbox", "run", "id_ed25519")
	if err := os.MkdirAll(filepath.Dir(runDirKey), 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	if err := os.WriteFile(runDirKey, []byte("key"), 0o600); err != nil {
		t.Fatalf("write run dir key: %v", err)
	}
	if got := starter.sshIdentityFile(agentID); got != runDirKey {
		t.Fatalf("run dir identity = %q, want %q", got, runDirKey)
	}

	agentEnvKey := filepath.Join(t.TempDir(), "agent-env-key")
	if err := os.WriteFile(agentEnvKey, []byte("key"), 0o600); err != nil {
		t.Fatalf("write agent env key: %v", err)
	}
	starter.baseEnv = append(starter.baseEnv, "AC_SANDBOX_SSH_KEY_FILE_AGENT_1="+agentEnvKey)
	if got := starter.sshIdentityFile(agentID); got != agentEnvKey {
		t.Fatalf("agent env identity = %q, want %q", got, agentEnvKey)
	}

	globalKey := filepath.Join(t.TempDir(), "global-key")
	if err := os.WriteFile(globalKey, []byte("key"), 0o600); err != nil {
		t.Fatalf("write global key: %v", err)
	}
	starter.baseEnv = append(starter.baseEnv, "AC_TART_SSH_IDENTITY_FILE="+globalKey)
	if got := starter.sshIdentityFile(agentID); got != globalKey {
		t.Fatalf("global identity = %q, want %q", got, globalKey)
	}
}

func TestSSHBaseArgsIncludesIdentityFile(t *testing.T) {
	got := sshBaseArgs("192.168.64.2", "/tmp/id_ed25519", "true")
	want := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=5",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-i", "/tmp/id_ed25519",
		"admin@192.168.64.2",
		"true",
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("ssh args = %#v, want %#v", got, want)
	}
}

func TestSSHShellCommandQuotesRemoteCommandAsSingleArg(t *testing.T) {
	got := sshBaseArgs("192.168.64.2", "/tmp/id_ed25519", sshShellCommand("mkdir -p '/tmp/a b' && echo ok"))
	if got[len(got)-1] != "sh -lc 'mkdir -p '\\''/tmp/a b'\\'' && echo ok'" {
		t.Fatalf("remote command arg = %q", got[len(got)-1])
	}
}

func TestTartGuestMountPlanAndEnvMountsSkillsAndMemorySources(t *testing.T) {
	homeBase := t.TempDir()
	codexHome := t.TempDir()
	claudeConfig := t.TempDir()
	builtinSkills := t.TempDir()
	agentSkills := t.TempDir()
	starter, err := NewTartVMStarter(TartVMStarterConfig{
		BinaryPath: filepath.Join(t.TempDir(), "agent-center"),
		BaseEnv: []string{
			"PATH=/usr/bin",
			"CODEX_HOME=" + codexHome,
			"CLAUDE_CONFIG_DIR=" + claudeConfig,
			"CLAUDE_BUILTIN_SKILLS_DIR=" + builtinSkills,
			"AC_AGENT_SKILLS_DIR=" + agentSkills,
			"AC_SANDBOX_COMPUTER_USE_ENDPOINT_AGENT_1=/tmp/host-cua.sock",
			"AC_SANDBOX_VNC_PASSWORD_FILE_AGENT_1=/tmp/host-vnc-password",
			"NODE_REPL_SANDBOX_ALLOWED_UNIX_SOCKETS=/tmp/host-cua.sock",
			"SKY_CUA_SERVICE_NATIVE_PIPE_PATH=/tmp/host-cua.sock",
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
		"agent-center-agent-skills":          true,
	}
	for _, m := range plan.mounts {
		delete(wantMounts, m.name)
	}
	if len(wantMounts) != 0 {
		t.Fatalf("missing mounts: %v; got %+v", wantMounts, plan.mounts)
	}
	t.Setenv("AC_SANDBOX_GUEST_COMPUTER_USE_ENDPOINT_AGENT_1", "/tmp/guest-cua.sock")
	env := tartGuestEnv(starter.baseEnv, plan, "agent-1")
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
	if got["AC_AGENT_SKILLS_DIR"] != tartGuestSharePath("agent-center-agent-skills") {
		t.Fatalf("guest AC_AGENT_SKILLS_DIR = %q", got["AC_AGENT_SKILLS_DIR"])
	}
	if got["PATH"] != "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin" {
		t.Fatalf("guest PATH = %q", got["PATH"])
	}
	if got["AC_SANDBOX_RUNTIME_INSIDE_VM"] != "1" {
		t.Fatalf("inside VM flag = %q", got["AC_SANDBOX_RUNTIME_INSIDE_VM"])
	}
	if got["AC_SANDBOX_RUNTIME_PLACEMENT"] != "vm_runtime" {
		t.Fatalf("runtime placement = %q", got["AC_SANDBOX_RUNTIME_PLACEMENT"])
	}
	if got["AC_SANDBOX_COMPUTER_USE_ENDPOINT"] != "/tmp/guest-cua.sock" {
		t.Fatalf("guest CUA endpoint = %q", got["AC_SANDBOX_COMPUTER_USE_ENDPOINT"])
	}
	if got["SKY_CUA_SERVICE_NATIVE_PIPE_PATH"] != "/tmp/guest-cua.sock" {
		t.Fatalf("guest CUA pipe = %q", got["SKY_CUA_SERVICE_NATIVE_PIPE_PATH"])
	}
	for key := range got {
		if strings.Contains(got[key], "host-cua") || strings.Contains(got[key], "host-vnc") {
			t.Fatalf("guest env leaked host sandbox value %s=%q", key, got[key])
		}
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
