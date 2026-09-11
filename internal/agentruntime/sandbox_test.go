package agentruntime

import "testing"

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
