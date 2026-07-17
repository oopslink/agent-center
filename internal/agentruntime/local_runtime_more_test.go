package agentruntime

import (
	"context"
	"testing"
	"time"
)

// TestExecutorSurface_NoEngineNoop pins the 0c executor面 on a runtime with NO engine
// attached (the default single-active agent): every executor method is a safe,
// non-wedging no-op rather than a wired action, so a non-concurrent agent never forks.
func TestExecutorSurface_NoEngineNoop(t *testing.T) {
	rt, _ := newTestRuntime(t)
	ctx := context.Background()

	// No engine + no ToolCaller → SpawnExecutor logs + returns (nil, nil), never wedges.
	if err := rt.NotifyWorkAvailable(ctx, "wi-1"); err != nil {
		t.Errorf("NotifyWorkAvailable (no engine) = %v, want nil", err)
	}
	res, err := rt.SpawnExecutor(ctx, SpawnRequest{TaskID: "wi-1"})
	if res != nil || err != nil {
		t.Errorf("SpawnExecutor (no engine) = (%v, %v), want (nil, nil)", res, err)
	}
	if err := rt.Recover(ctx); err != nil {
		t.Errorf("Recover (no engine) = %v, want nil", err)
	}
	if snap := rt.SnapshotConcurrency(); snap != nil {
		t.Errorf("SnapshotConcurrency (no engine) = %v, want nil", snap)
	}
	if rt.HasExecutor() {
		t.Error("a runtime with no engine attached must report HasExecutor()=false")
	}
}

// TestAccessorsAndResumeNudge pins the trivial accessors + the resume-nudge default/
// override the daemon boot-relaunch path reads.
func TestAccessorsAndResumeNudge(t *testing.T) {
	rt, _ := newTestRuntime(t)

	// withState is the only door onto the SessionState, and it runs under r.mu.
	rt.withState(func(s *SessionState) {
		if s == nil {
			t.Error("withState must hand fn the runtime's SessionState, got nil")
		}
	})
	if rt.AgentID() != "agent-x" {
		t.Fatalf("AgentID = %q, want agent-x", rt.AgentID())
	}
	if got := rt.ResumeNudgeText(); got != DefaultResumeNudge {
		t.Fatalf("ResumeNudgeText default = %q, want %q", got, DefaultResumeNudge)
	}

	// Override via cfg.
	rt2 := NewLocalRuntime(LocalRuntimeConfig{AgentID: "a", Reporter: &nopReporter{}, ResumeNudge: "pick it back up"}, &SessionState{})
	if got := rt2.ResumeNudgeText(); got != "pick it back up" {
		t.Fatalf("ResumeNudgeText override = %q", got)
	}
}

// TestIsRunning_AttachDetach pins IsRunning + Attach (boot reattach installs a
// session) + Detach (survival: sets Detaching + detaches the session), all under the
// shared lock.
func TestIsRunning_AttachDetach(t *testing.T) {
	rt, _ := newTestRuntime(t)

	if rt.IsRunning() {
		t.Fatal("IsRunning must be false before a session is installed")
	}

	fs := &fakeSession{}
	rt.Attach(fs)
	if !rt.IsRunning() {
		t.Fatal("IsRunning must be true after Attach")
	}
	rt.withState(func(s *SessionState) {
		if s.Session == nil {
			t.Error("Attach must install the session into the shared state")
		}
	})
	// The reattach path wires the runtime's own reader-goroutine callbacks.
	if rt.OnEventCallback() == nil || rt.OnExitCallback() == nil {
		t.Fatal("Attach callbacks must be non-nil")
	}

	rt.Detach()
	rt.withState(func(s *SessionState) {
		if !s.Detaching {
			t.Error("Detach must set Detaching so onExit treats the nil exit as survival, not crash")
		}
	})
	if !fs.closed {
		t.Fatal("Detach must detach the underlying session")
	}
}

// TestStopReporting_NoSession pins the no-live-session stop path: it settles the
// "stopped" lifecycle once and returns nil (no panic on a nil session).
func TestStopReporting_NoSession(t *testing.T) {
	rt, _ := newTestRuntime(t)
	if err := rt.StopReporting(context.Background()); err != nil {
		t.Fatalf("StopReporting (no session) = %v, want nil", err)
	}
}

// TestTick_NoResumeIsNoop pins Tick's drain path when nothing is scheduled: a no-op
// that returns nil (the daemon fans this out over every live runtime each OnTick).
func TestTick_NoResumeIsNoop(t *testing.T) {
	rt, _ := newTestRuntime(t)
	if err := rt.Tick(context.Background(), time.Unix(1_000_000, 0)); err != nil {
		t.Fatalf("Tick (no resume) = %v, want nil", err)
	}
}

// TestAgentCLIMarker_RoundTrip pins the codex cli-marker persistence (boot-recovery
// reads it to route a codex agent away from the claude supervisor probe).
func TestAgentCLIMarker_RoundTrip(t *testing.T) {
	home := t.TempDir()
	if got := ReadAgentCLIMarker(home); got != "" {
		t.Fatalf("absent marker = %q, want empty", got)
	}
	if err := WriteAgentCLIMarker(home, CLICodex); err != nil {
		t.Fatalf("WriteAgentCLIMarker: %v", err)
	}
	if got := ReadAgentCLIMarker(home); got != CLICodex {
		t.Fatalf("marker roundtrip = %q, want %q", got, CLICodex)
	}
	if err := WriteAgentCLIMarker("", "x"); err == nil {
		t.Fatal("WriteAgentCLIMarker with empty home must error")
	}
}
