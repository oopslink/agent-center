package agentruntime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSandboxBindingComputerUseStatusRequiresRunningVM(t *testing.T) {
	b := SandboxBinding{State: SandboxStateReady, ComputerUseEndpoint: "/tmp/cua.sock"}
	if got := b.ComputerUseStatus(); got != "unavailable" {
		t.Fatalf("ready-but-stopped computer use status = %q, want unavailable", got)
	}
	b.State = SandboxStateRunning
	if got := b.ComputerUseStatus(); got != "ready" {
		t.Fatalf("running computer use status = %q, want ready", got)
	}
}

func TestTartStateFromJSON(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{name: "running flag wins", raw: `{"Running":true,"State":"suspended"}`, want: SandboxStateRunning, ok: true},
		{name: "running state", raw: `{"Running":false,"State":"running"}`, want: SandboxStateRunning, ok: true},
		{name: "suspended", raw: `{"Running":false,"State":"suspended"}`, want: SandboxStateSuspended, ok: true},
		{name: "stopped", raw: `{"Running":false,"State":"stopped"}`, want: SandboxStateReady, ok: true},
		{name: "unknown", raw: `{"Running":false,"State":"paused"}`, want: "", ok: false},
		{name: "bad json", raw: `{`, want: "", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tartStateFromJSON([]byte(tt.raw))
			if got != tt.want || ok != tt.ok {
				t.Fatalf("tartStateFromJSON() = %q, %v; want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestTartNotRunningError(t *testing.T) {
	if !tartNotRunningError(`VM "VM "ac-agent-20328d5c454c" is not running" is not running`) {
		t.Fatal("expected nested Tart not-running error to match")
	}
	if tartNotRunningError("tart suspend failed: permission denied") {
		t.Fatal("unexpected not-running match")
	}
}

func TestTartRestoreFailed(t *testing.T) {
	errText := `Error Domain=VZErrorDomain Code=12 "The virtual machine failed to restore with error invalid argument."`
	if !tartRestoreFailed(errText) {
		t.Fatal("expected Tart restore failure to match")
	}
	if tartRestoreFailed("VM is not running") {
		t.Fatal("unexpected restore-failure match")
	}
}

func TestMaterializeSandboxResourcesCreatesStandardRuntimeFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AC_SANDBOX_RUNTIME_PLACEMENT", SandboxRuntimePlacementVMRuntime)
	b, err := materializeSandboxResources(home, SandboxBinding{AgentID: "agent-1"})
	if err != nil {
		t.Fatalf("materialize sandbox resources: %v", err)
	}
	if b.RunDir != filepath.Join(home, "sandbox", "run") {
		t.Fatalf("run dir = %q", b.RunDir)
	}
	if b.VNCPasswordFile != filepath.Join(b.RunDir, "vnc_password") {
		t.Fatalf("vnc password file = %q", b.VNCPasswordFile)
	}
	if b.RuntimePlacement != SandboxRuntimePlacementVMRuntime {
		t.Fatalf("runtime placement = %q", b.RuntimePlacement)
	}
	if b.HostMountPath != home {
		t.Fatalf("host mount path = %q, want %q", b.HostMountPath, home)
	}
	if !strings.HasPrefix(b.GuestMountPath, "/Volumes/My Shared Files/agent-home-") {
		t.Fatalf("guest mount path = %q", b.GuestMountPath)
	}
	raw, err := os.ReadFile(b.VNCPasswordFile)
	if err != nil {
		t.Fatalf("read vnc password: %v", err)
	}
	if len(strings.TrimSpace(string(raw))) != 8 {
		t.Fatalf("generated vnc password length = %d, want 8", len(strings.TrimSpace(string(raw))))
	}
	row := b.Row()
	if row.RunDir != b.RunDir || row.VNCPasswordFile != b.VNCPasswordFile {
		t.Fatalf("row did not project standard sandbox resources: %+v", row)
	}
}

func TestSandboxBrowserCommandUsesAgentSSHKey(t *testing.T) {
	runDir := t.TempDir()
	key := filepath.Join(runDir, "id_ed25519")
	if err := os.WriteFile(key, []byte("key"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	b := SandboxBinding{RunDir: runDir}
	got := sandboxBrowserCommandForBinding(b, "192.168.64.2")
	if !strings.Contains(got, "-i "+key) {
		t.Fatalf("browser command %q does not include sandbox ssh key %q", got, key)
	}
	if !strings.Contains(got, "admin@192.168.64.2") || !strings.Contains(got, "/usr/bin/open -a Safari") {
		t.Fatalf("browser command missing target/open command: %q", got)
	}
}

func TestSandboxHostCodexCLIPathHonorsEnv(t *testing.T) {
	codex := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(codex, []byte("codex"), 0o755); err != nil {
		t.Fatalf("write codex: %v", err)
	}
	t.Setenv("AC_SANDBOX_CODEX_BINARY", codex)
	if got := sandboxHostCodexCLIPath(); got != codex {
		t.Fatalf("codex path = %q, want %q", got, codex)
	}
}

func TestSandboxHostCodexCodeModeHostPathHonorsEnv(t *testing.T) {
	codeMode := filepath.Join(t.TempDir(), "codex-code-mode-host")
	if err := os.WriteFile(codeMode, []byte("code mode"), 0o755); err != nil {
		t.Fatalf("write code mode host: %v", err)
	}
	t.Setenv("AC_SANDBOX_CODE_MODE_HOST_BINARY", codeMode)
	if got := sandboxHostCodexCodeModeHostPath("/missing/codex"); got != codeMode {
		t.Fatalf("code mode host path = %q, want %q", got, codeMode)
	}
}

func TestSandboxHostCodexCodeModeHostPathFindsResolvedCodexSibling(t *testing.T) {
	caskBin := filepath.Join(t.TempDir(), "Caskroom", "codex", "0.151.0", "bin")
	if err := os.MkdirAll(caskBin, 0o755); err != nil {
		t.Fatalf("mkdir cask bin: %v", err)
	}
	realCodex := filepath.Join(caskBin, "codex")
	codeMode := filepath.Join(caskBin, "codex-code-mode-host")
	if err := os.WriteFile(realCodex, []byte("codex"), 0o755); err != nil {
		t.Fatalf("write codex: %v", err)
	}
	if err := os.WriteFile(codeMode, []byte("code mode"), 0o755); err != nil {
		t.Fatalf("write code mode host: %v", err)
	}
	homebrewBin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(homebrewBin, 0o755); err != nil {
		t.Fatalf("mkdir homebrew bin: %v", err)
	}
	codexSymlink := filepath.Join(homebrewBin, "codex")
	if err := os.Symlink(realCodex, codexSymlink); err != nil {
		t.Fatalf("symlink codex: %v", err)
	}
	got := sandboxHostCodexCodeModeHostPath(codexSymlink)
	gotInfo, gotErr := os.Stat(got)
	wantInfo, wantErr := os.Stat(codeMode)
	if gotErr != nil || wantErr != nil || !os.SameFile(gotInfo, wantInfo) {
		t.Fatalf("code mode host path = %q, want same file as %q (gotErr=%v wantErr=%v)", got, codeMode, gotErr, wantErr)
	}
}

func TestEnsureAgentSandboxInsideVMOverlaysWithoutHostResourceMaterialization(t *testing.T) {
	home := t.TempDir()
	hostHome := filepath.Join(t.TempDir(), "host-agent-home")
	if err := os.MkdirAll(filepath.Join(home, "sandbox"), 0o700); err != nil {
		t.Fatalf("mkdir sandbox: %v", err)
	}
	if err := writeSandboxBinding(filepath.Join(home, "sandbox", "binding.json"), SandboxBinding{
		SandboxID:        "sbx-existing",
		AgentID:          "agent-1",
		WorkerID:         "worker-1",
		Provider:         SandboxProviderTartMacOSVM,
		RuntimePlacement: SandboxRuntimePlacementHostEndpoint,
		VMName:           "ac-agent-existing",
		State:            SandboxStateDegraded,
		HostMountPath:    hostHome,
		VNCPasswordFile:  filepath.Join(hostHome, "sandbox", "run", "vnc_password"),
		LastError:        "host-side error",
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}
	cua := filepath.Join(home, "computeruse.sock")
	if err := os.WriteFile(cua, []byte("sock"), 0o600); err != nil {
		t.Fatalf("write cua placeholder: %v", err)
	}
	t.Setenv("AC_SANDBOX_RUNTIME_INSIDE_VM", "1")
	t.Setenv("AC_SANDBOX_COMPUTER_USE_ENDPOINT", cua)
	m := NewLocalSandboxManager(func() time.Time {
		return time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)
	})
	b, err := m.EnsureAgentSandbox(context.Background(), SandboxEnsureRequest{
		AgentID:  "agent-1",
		WorkerID: "worker-2",
		HomeDir:  home,
		Config:   SandboxConfig{Enabled: true, Provider: SandboxProviderTartMacOSVM},
	})
	if err != nil {
		t.Fatalf("EnsureAgentSandbox: %v", err)
	}
	if b.RuntimePlacement != SandboxRuntimePlacementVMRuntime {
		t.Fatalf("runtime placement = %q", b.RuntimePlacement)
	}
	if b.State != SandboxStateRunning || b.LastError != "" {
		t.Fatalf("state/error = %q/%q", b.State, b.LastError)
	}
	if b.HostMountPath != hostHome {
		t.Fatalf("host mount path changed to %q, want %q", b.HostMountPath, hostHome)
	}
	if b.ComputerUseEndpoint != cua {
		t.Fatalf("cua endpoint = %q, want %q", b.ComputerUseEndpoint, cua)
	}
	if _, err := os.Stat(filepath.Join(hostHome, "sandbox", "run", "vnc_password")); !os.IsNotExist(err) {
		t.Fatalf("unexpected host vnc password materialization err=%v", err)
	}
	persisted, ok, err := readSandboxBinding(filepath.Join(home, "sandbox", "binding.json"))
	if err != nil || !ok {
		t.Fatalf("read persisted binding: ok=%v err=%v", ok, err)
	}
	if persisted.State != SandboxStateDegraded {
		t.Fatalf("inside VM overlay should not rewrite shared host binding, got %q", persisted.State)
	}
}

func TestHealthInsideVMDoesNotRequireHostTart(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "sandbox"), 0o700); err != nil {
		t.Fatalf("mkdir sandbox: %v", err)
	}
	if err := writeSandboxBinding(filepath.Join(home, "sandbox", "binding.json"), SandboxBinding{
		SandboxID:        "sbx-existing",
		AgentID:          "agent-1",
		WorkerID:         "worker-1",
		Provider:         SandboxProviderTartMacOSVM,
		RuntimePlacement: SandboxRuntimePlacementHostEndpoint,
		VMName:           "ac-agent-existing",
		State:            SandboxStateDegraded,
		LastError:        "tart CLI not found on worker host",
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}
	cua := filepath.Join(home, "computeruse.sock")
	if err := os.WriteFile(cua, []byte("sock"), 0o600); err != nil {
		t.Fatalf("write cua placeholder: %v", err)
	}
	t.Setenv("AC_SANDBOX_RUNTIME_INSIDE_VM", "1")
	t.Setenv("AC_SANDBOX_COMPUTER_USE_ENDPOINT", cua)
	m := NewLocalSandboxManager(func() time.Time {
		return time.Date(2026, 9, 13, 13, 0, 0, 0, time.UTC)
	})
	b, err := m.Health(context.Background(), SandboxEnsureRequest{
		AgentID:  "agent-1",
		WorkerID: "worker-2",
		HomeDir:  home,
		Config:   SandboxConfig{Enabled: true, Provider: SandboxProviderTartMacOSVM},
	})
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if b.State != SandboxStateRunning || b.LastError != "" {
		t.Fatalf("inside VM health state/error = %q/%q", b.State, b.LastError)
	}
	persisted, ok, err := readSandboxBinding(filepath.Join(home, "sandbox", "binding.json"))
	if err != nil || !ok {
		t.Fatalf("read persisted binding: ok=%v err=%v", ok, err)
	}
	if persisted.State != SandboxStateDegraded {
		t.Fatalf("inside VM health should not rewrite shared host binding, got %q", persisted.State)
	}
}

func TestHealthInsideVMRestoresMissingComputerUseEndpoint(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "sandbox"), 0o700); err != nil {
		t.Fatalf("mkdir sandbox: %v", err)
	}
	if err := writeSandboxBinding(filepath.Join(home, "sandbox", "binding.json"), SandboxBinding{
		SandboxID: "sbx-existing",
		AgentID:   "agent-1",
		WorkerID:  "worker-1",
		Provider:  SandboxProviderTartMacOSVM,
		State:     SandboxStateRunning,
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}
	cua := filepath.Join(home, "computeruse.sock")
	appPath := filepath.Join(home, "Codex Computer Use.app")
	if err := os.MkdirAll(appPath, 0o755); err != nil {
		t.Fatalf("mkdir app: %v", err)
	}
	bin := filepath.Join(home, "fake-open")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n: > \"$TEST_CUA_ENDPOINT\"\n"), 0o755); err != nil {
		t.Fatalf("write fake open: %v", err)
	}
	oldOpen := sandboxComputerUseOpenBinary
	oldApp := sandboxComputerUseAppPath
	sandboxComputerUseOpenBinary = bin
	sandboxComputerUseAppPath = appPath
	t.Cleanup(func() {
		sandboxComputerUseOpenBinary = oldOpen
		sandboxComputerUseAppPath = oldApp
	})
	t.Setenv("TEST_CUA_ENDPOINT", cua)
	t.Setenv("AC_SANDBOX_RUNTIME_INSIDE_VM", "1")
	t.Setenv("AC_SANDBOX_COMPUTER_USE_ENDPOINT", cua)
	m := NewLocalSandboxManager(func() time.Time {
		return time.Date(2026, 9, 14, 4, 0, 0, 0, time.UTC)
	})
	b, err := m.Health(context.Background(), SandboxEnsureRequest{
		AgentID:  "agent-1",
		WorkerID: "worker-2",
		HomeDir:  home,
		Config:   SandboxConfig{Enabled: true, Provider: SandboxProviderTartMacOSVM},
	})
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if b.ComputerUseEndpoint != cua || b.LastError != "" {
		t.Fatalf("endpoint/error = %q/%q, want %q/empty", b.ComputerUseEndpoint, b.LastError, cua)
	}
}
