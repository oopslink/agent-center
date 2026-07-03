package agentruntime

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestLocalRuntime_WiredNotifiesForwardToHooks proves the Phase 0a contract: the
// three wired signals forward VERBATIM to the injected hooks (this is what makes
// routing a command through the runtime behavior-identical to the direct path),
// and the hook's error is propagated unchanged.
func TestLocalRuntime_WiredNotifiesForwardToHooks(t *testing.T) {
	var gotWork WorkRequest
	var gotWake WakeRequest
	var gotConverse ConverseRequest
	sentinel := errors.New("boom")

	rt := NewLocalRuntime("agent:x", Hooks{
		NotifyWork:     func(_ context.Context, req WorkRequest) error { gotWork = req; return nil },
		NotifyWake:     func(_ context.Context, req WakeRequest) error { gotWake = req; return sentinel },
		NotifyConverse: func(_ context.Context, req ConverseRequest) error { gotConverse = req; return nil },
	})

	if rt.AgentID() != "agent:x" {
		t.Fatalf("AgentID = %q, want agent:x", rt.AgentID())
	}

	wantWork := WorkRequest{AgentID: "agent:x", TaskID: "T1", TaskRef: "T#1", Brief: "do it"}
	if err := rt.NotifyWork(context.Background(), wantWork); err != nil {
		t.Fatalf("NotifyWork err = %v, want nil", err)
	}
	if gotWork != wantWork {
		t.Fatalf("work hook got %+v, want %+v", gotWork, wantWork)
	}

	wantWake := WakeRequest{AgentID: "agent:x", TaskID: "T1", ConversationID: "c1", MessageID: "m1", MessageText: "hi", RootMessageID: "r1"}
	if err := rt.NotifyWake(context.Background(), wantWake); !errors.Is(err, sentinel) {
		t.Fatalf("NotifyWake err = %v, want sentinel propagated", err)
	}
	if gotWake != wantWake {
		t.Fatalf("wake hook got %+v, want %+v", gotWake, wantWake)
	}

	wantConv := ConverseRequest{AgentID: "agent:x", ConversationID: "c1", ConvKind: "dm", SenderRef: "u1", MessageID: "m1", MessageText: "yo", AttachmentCount: 2, OwnerRef: "pm://tasks/T1"}
	if err := rt.NotifyConverse(context.Background(), wantConv); err != nil {
		t.Fatalf("NotifyConverse err = %v, want nil", err)
	}
	if gotConverse != wantConv {
		t.Fatalf("converse hook got %+v, want %+v", gotConverse, wantConv)
	}
}

// TestLocalRuntime_UnwiredMethodsReturnNotWired proves the not-yet-migrated methods
// fail loudly with ErrNotWired (0b/0c targets) rather than silently no-op'ing — so
// a routing mistake in a later phase surfaces instead of dropping a signal.
func TestLocalRuntime_UnwiredMethodsReturnNotWired(t *testing.T) {
	rt := NewLocalRuntime("agent:x", Hooks{})
	ctx := context.Background()

	if err := rt.NotifyWorkAvailable(ctx, "T1"); !errors.Is(err, ErrNotWired) {
		t.Errorf("NotifyWorkAvailable = %v, want ErrNotWired", err)
	}
	if err := rt.Start(ctx, StartSpec{}); !errors.Is(err, ErrNotWired) {
		t.Errorf("Start = %v, want ErrNotWired", err)
	}
	if err := rt.Stop(ctx); !errors.Is(err, ErrNotWired) {
		t.Errorf("Stop = %v, want ErrNotWired", err)
	}
	if _, err := rt.SpawnExecutor(ctx, SpawnRequest{}); !errors.Is(err, ErrNotWired) {
		t.Errorf("SpawnExecutor = %v, want ErrNotWired", err)
	}
	if err := rt.Tick(ctx, time.Time{}); !errors.Is(err, ErrNotWired) {
		t.Errorf("Tick = %v, want ErrNotWired", err)
	}
	if err := rt.Recover(ctx); !errors.Is(err, ErrNotWired) {
		t.Errorf("Recover = %v, want ErrNotWired", err)
	}
	if rt.IsRunning() {
		t.Errorf("IsRunning = true, want false (session state not owned in 0a)")
	}
	if snap := rt.SnapshotConcurrency(); snap != nil {
		t.Errorf("SnapshotConcurrency = %v, want nil", snap)
	}
}

// TestLocalRuntime_NilHookReturnsNotWired ensures a wired-signal method with no hook
// installed returns ErrNotWired rather than panicking on a nil call.
func TestLocalRuntime_NilHookReturnsNotWired(t *testing.T) {
	rt := NewLocalRuntime("agent:x", Hooks{})
	ctx := context.Background()
	if err := rt.NotifyWork(ctx, WorkRequest{}); !errors.Is(err, ErrNotWired) {
		t.Errorf("NotifyWork (nil hook) = %v, want ErrNotWired", err)
	}
	if err := rt.NotifyWake(ctx, WakeRequest{}); !errors.Is(err, ErrNotWired) {
		t.Errorf("NotifyWake (nil hook) = %v, want ErrNotWired", err)
	}
	if err := rt.NotifyConverse(ctx, ConverseRequest{}); !errors.Is(err, ErrNotWired) {
		t.Errorf("NotifyConverse (nil hook) = %v, want ErrNotWired", err)
	}
}
