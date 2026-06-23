package service

import (
	"context"
	"testing"
	"time"
)

// With no SweepCandidates wired (the fixture default), ReconcileOnce is dormant — it
// returns nil, enqueues nothing, and never panics. (The session-heal sweep behavior
// when candidates ARE wired is covered in wake_sweep_test.go.)
func TestReconcileOnce_NoOp(t *testing.T) {
	f := newWakeFixture(t)
	if err := f.proj.ReconcileOnce(f.ctx); err != nil {
		t.Fatalf("ReconcileOnce must be a graceful no-op, got %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("no-op reconcile must enqueue nothing, got %d", len(cmds))
	}
}

// Nil deps → ReconcileOnce no-ops gracefully (test-fixture / e-i-only build).
func TestReconcileOnce_NilDeps_NoOp(t *testing.T) {
	p := NewWakeProjector(WakeProjectorDeps{}) // no repos wired
	if err := p.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("nil-deps ReconcileOnce must be a graceful no-op, got %v", err)
	}
}

// --- WakeReconcileLoop lifecycle --------------------------------------------

// The loop runs boot-once + on a fast ticker and returns cleanly when ctx times
// out (no leak). Each sweep is a cheap no-op.
func TestWakeReconcileLoop_TicksThenStops(t *testing.T) {
	f := newWakeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	loop := NewWakeReconcileLoop(f.proj, 20*time.Millisecond, nil)
	go func() { loop.Run(ctx); close(done) }()

	select {
	case <-done:
		// Run returned cleanly after the ctx deadline (boot sweep + ticks ran).
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not return after ctx timeout (goroutine leak)")
	}
}
