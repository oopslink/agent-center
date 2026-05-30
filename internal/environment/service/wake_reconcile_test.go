package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
)

// Red-line self-heal (C1-noloss): a waiting_input WorkItem whose task conversation
// has unread qualifying messages — with NO awaiting_input/message_added event ever
// processed — ReconcileOnce recomputes from the cursor alone and enqueues exactly
// ONE agent.wake with the merged unread + key agent.wake:{wi}:batch:{lastId}.
func TestReconcileOnce_SelfHeal_NoEventEverProcessed(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "AG1", "W1")
	f.saveTaskConv(t, "conv-1", "T1")
	f.saveWorkItem(t, "wi-1", "AG1", "pm://tasks/T1", agent.WorkItemWaitingInput)

	m1 := f.addMsg(t, "conv-1", "user:bob", "first ask")
	last := f.addMsg(t, "conv-1", "user:carol", "second ask")
	// cursor at m1 → only the second message is unread.
	f.setCursor(t, "AG1", "conv-1", m1)

	if err := f.proj.ReconcileOnce(f.ctx); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}

	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 1 {
		t.Fatalf("self-heal must enqueue exactly 1 wake, got %d", len(cmds))
	}
	c := cmds[0]
	if c.IdempotencyKey() != "agent.wake:wi-1:batch:"+last {
		t.Fatalf("idempotency_key = %q (want batch:%s)", c.IdempotencyKey(), last)
	}
	if !strings.Contains(c.Payload(), "second ask") {
		t.Fatalf("merged unread missing: %s", c.Payload())
	}
	if strings.Contains(c.Payload(), "first ask") {
		t.Fatalf("already-seen message must not be re-delivered: %s", c.Payload())
	}
}

// Convergence: after the cursor advanced past all messages (push already delivered
// + mark-seen) ReconcileOnce enqueues NOTHING.
func TestReconcileOnce_CursorAdvanced_NoEnqueue(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "AG1", "W1")
	f.saveTaskConv(t, "conv-1", "T1")
	f.saveWorkItem(t, "wi-1", "AG1", "pm://tasks/T1", agent.WorkItemWaitingInput)

	f.addMsg(t, "conv-1", "user:bob", "old")
	last := f.addMsg(t, "conv-1", "user:carol", "also old")
	f.setCursor(t, "AG1", "conv-1", last) // cursor at the newest → nothing unread

	if err := f.proj.ReconcileOnce(f.ctx); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("advanced cursor must enqueue nothing, got %d", len(cmds))
	}
}

// No double-deliver: calling ReconcileOnce twice with the same unread → the second
// is idempotent (same batch key → ControlLog dedups). Also asserts a push-enqueued
// batch and a poll-enqueued batch converge to ONE command (same key).
func TestReconcileOnce_Twice_Idempotent(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "AG1", "W1")
	f.saveTaskConv(t, "conv-1", "T1")
	f.saveWorkItem(t, "wi-1", "AG1", "pm://tasks/T1", agent.WorkItemWaitingInput)
	f.addMsg(t, "conv-1", "user:bob", "go")

	if err := f.proj.ReconcileOnce(f.ctx); err != nil {
		t.Fatalf("ReconcileOnce 1: %v", err)
	}
	if err := f.proj.ReconcileOnce(f.ctx); err != nil {
		t.Fatalf("ReconcileOnce 2: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 1 {
		t.Fatalf("double sweep must dedup to 1 command, got %d", len(cmds))
	}
}

// Convergence with push: the e-ii push path and the poll path use the SAME batch
// key, so a push-enqueued batch followed by a reconcile sweep yields ONE command.
func TestReconcileOnce_ConvergesWithPush(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "AG1", "W1")
	f.saveTaskConv(t, "conv-1", "T1")
	f.saveWorkItem(t, "wi-1", "AG1", "pm://tasks/T1", agent.WorkItemWaitingInput)
	f.addMsg(t, "conv-1", "user:bob", "go")

	// push path (e-ii) enqueues first.
	if err := f.proj.Project(f.ctx, awaitingInputEvent("EV1", "AG1", "wi-1", "T1", "conv-1")); err != nil {
		t.Fatalf("push Project: %v", err)
	}
	// poll path runs later; cursor not yet advanced (command unconsumed) → same key.
	if err := f.proj.ReconcileOnce(f.ctx); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 1 {
		t.Fatalf("push+poll must converge to 1 command (same batch key), got %d", len(cmds))
	}
}

// Predicate: only-self unread → no enqueue.
func TestReconcileOnce_OnlySelfUnread_NoEnqueue(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "AG1", "W1")
	f.saveTaskConv(t, "conv-1", "T1")
	f.saveWorkItem(t, "wi-1", "AG1", "pm://tasks/T1", agent.WorkItemWaitingInput)
	f.addMsg(t, "conv-1", "agent:AG1", "self only")

	if err := f.proj.ReconcileOnce(f.ctx); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("only-self unread must not enqueue, got %d", len(cmds))
	}
}

// Predicate: ListByStatus(waiting_input) excludes non-waiting items, so an active
// WorkItem with unread is never swept.
func TestReconcileOnce_ActiveWorkItemExcluded(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "AG1", "W1")
	f.saveTaskConv(t, "conv-1", "T1")
	f.saveWorkItem(t, "wi-1", "AG1", "pm://tasks/T1", agent.WorkItemActive)
	f.addMsg(t, "conv-1", "user:bob", "unread but item is active")

	if err := f.proj.ReconcileOnce(f.ctx); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("active WorkItem must be excluded by ListByStatus, got %d", len(cmds))
	}
}

// Predicate: agent with no worker binding → skip + log, no error, sweep continues.
func TestReconcileOnce_AgentNoWorker_SkipNoError(t *testing.T) {
	f := newWakeFixture(t)
	// no agent saved → FindByID fails → skip; no command, no error.
	f.saveTaskConv(t, "conv-1", "T1")
	f.saveWorkItem(t, "wi-1", "AG1", "pm://tasks/T1", agent.WorkItemWaitingInput)
	f.addMsg(t, "conv-1", "user:bob", "go")

	if err := f.proj.ReconcileOnce(f.ctx); err != nil {
		t.Fatalf("ReconcileOnce must not fail on unresolved agent: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("unresolved agent must enqueue nothing, got %d", len(cmds))
	}
}

// Sweep resilience: two waiting_input WorkItems, one with an unresolvable
// conversation (no conv saved) → the other is still processed.
func TestReconcileOnce_SweepContinuesPastBadItem(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "AG1", "W1")
	f.saveAgent(t, "AG2", "W2")
	// T1 has NO conversation saved → unresolvable, must be skipped.
	f.saveWorkItem(t, "wi-bad", "AG1", "pm://tasks/T1", agent.WorkItemWaitingInput)
	// T2 is healthy with an unread message.
	f.saveTaskConv(t, "conv-2", "T2")
	f.saveWorkItem(t, "wi-ok", "AG2", "pm://tasks/T2", agent.WorkItemWaitingInput)
	last := f.addMsg(t, "conv-2", "user:bob", "process me")

	if err := f.proj.ReconcileOnce(f.ctx); err != nil {
		t.Fatalf("ReconcileOnce must tolerate per-item failures: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("unresolvable-conv item must enqueue nothing, got %d", len(cmds))
	}
	cmds := f.commandsFor(t, "W2")
	if len(cmds) != 1 {
		t.Fatalf("healthy item must still be processed, got %d commands", len(cmds))
	}
	if cmds[0].IdempotencyKey() != "agent.wake:wi-ok:batch:"+last {
		t.Fatalf("idempotency_key = %q", cmds[0].IdempotencyKey())
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

// The loop runs an initial ReconcileOnce on boot then stops cleanly on ctx
// cancel (no goroutine leak). We assert the boot sweep enqueued the pending wake
// then cancel.
func TestWakeReconcileLoop_BootSweepThenCancel(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "AG1", "W1")
	f.saveTaskConv(t, "conv-1", "T1")
	f.saveWorkItem(t, "wi-1", "AG1", "pm://tasks/T1", agent.WorkItemWaitingInput)
	last := f.addMsg(t, "conv-1", "user:bob", "wake me")

	ctx, cancel := context.WithCancel(context.Background())
	loop := NewWakeReconcileLoop(f.proj, time.Hour, nil) // long tick → only boot sweep runs
	done := make(chan struct{})
	go func() { loop.Run(ctx); close(done) }()

	// Wait for the boot sweep to enqueue the wake.
	deadline := time.After(2 * time.Second)
	for {
		if cmds := f.commandsFor(t, "W1"); len(cmds) == 1 {
			if cmds[0].IdempotencyKey() != "agent.wake:wi-1:batch:"+last {
				t.Fatalf("boot sweep key = %q", cmds[0].IdempotencyKey())
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("boot sweep did not enqueue within deadline")
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-done:
		// clean return
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not return on ctx cancel (goroutine leak)")
	}
}

// The loop runs boot-once + on a fast ticker and returns cleanly when ctx times
// out (no leak). With no waiting_input items each sweep is a cheap no-op.
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
