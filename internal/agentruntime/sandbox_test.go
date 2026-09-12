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
