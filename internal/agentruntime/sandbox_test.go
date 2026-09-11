package agentruntime

import "testing"

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
