package agentruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
