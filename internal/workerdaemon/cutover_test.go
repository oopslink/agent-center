package workerdaemon

import (
	"context"
	"testing"
)

// fakeControlClient is a no-op ControlClient — only its non-nil-ness matters for
// the D2-f cutover XOR gate.
type fakeControlClient struct{}

func (fakeControlClient) ConnectControl(ctx context.Context, workerID string) (int64, error) {
	return 0, nil
}
func (fakeControlClient) PullCommands(ctx context.Context, workerID string, after int64) ([]ControlCommand, error) {
	return nil, nil
}
func (fakeControlClient) AckControl(ctx context.Context, workerID string, offset int64) error {
	return nil
}

// TestCutover_LegacyDispatchXOR asserts the v2.7 D2-f cutover switch: the legacy
// taskruntime dispatch poll and the new control-stream loop are MUTUALLY
// EXCLUSIVE on ControlClient — exactly one execution path is enabled, never both
// or neither. Both key off the SAME value (fixed at construction), so there is no
// "both/neither" window and an agent is never double-run.
func TestCutover_LegacyDispatchXOR(t *testing.T) {
	fc := &fakeCenter{}

	// Default wiring (ControlClient nil): legacy dispatch ENABLED, control loop
	// OFF — the unchanged pre-D2-f behavior (additive land, flag default off).
	rtOff := NewRuntime(RuntimeConfig{WorkerID: "w-1"}, fc, nil)
	if rtOff.cfg.ControlClient != nil {
		t.Fatal("default RuntimeConfig must leave ControlClient nil")
	}
	if !rtOff.legacyDispatchEnabled() {
		t.Fatal("ControlClient nil → legacy dispatch must be ENABLED (default/dormant)")
	}

	// Cutover ON (ControlClient set): legacy dispatch DISABLED, control loop ON.
	rtOn := NewRuntime(RuntimeConfig{WorkerID: "w-1", ControlClient: fakeControlClient{}}, fc, nil)
	if rtOn.legacyDispatchEnabled() {
		t.Fatal("ControlClient set → legacy dispatch must be DISABLED (new path active)")
	}

	// XOR: the control loop starts iff ControlClient != nil, which is exactly
	// !legacyDispatchEnabled — so the two are never simultaneously on or off.
	for _, rt := range []*Runtime{rtOff, rtOn} {
		controlLoopOn := rt.cfg.ControlClient != nil
		if controlLoopOn == rt.legacyDispatchEnabled() {
			t.Fatalf("legacy dispatch and control loop must be mutually exclusive (XOR); "+
				"controlLoopOn=%v legacyEnabled=%v", controlLoopOn, rt.legacyDispatchEnabled())
		}
	}
}
