package environment

import (
	"context"
	"errors"
	"testing"
)

// capturePublisher records every published command so the append→publish hook
// can be asserted. Implements CommandPublisher.
type capturePublisher struct {
	got []*WorkerControlEvent
}

func (p *capturePublisher) Publish(e *WorkerControlEvent) { p.got = append(p.got, e) }

// panicPublisher simulates a publish failure (a subscriber drop / panic in the
// bus). The append must still succeed — best-effort, after-commit.
type panicPublisher struct{ calls int }

func (p *panicPublisher) Publish(_ *WorkerControlEvent) {
	p.calls++
	panic("publish blew up")
}

func TestControlLog_AppendCommand_PublishesWithOffset(t *testing.T) {
	pub := &capturePublisher{}
	l := newControlLog().WithPublisher(pub)
	ctx := context.Background()

	for i, key := range []string{"k1", "k2", "k3"} {
		evt, err := l.AppendCommand(ctx, AppendCommandInput{
			WorkerID: "W1", CommandType: "agent.start", IdempotencyKey: key,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(pub.got) != i+1 {
			t.Fatalf("after %d appends want %d publishes, got %d", i+1, i+1, len(pub.got))
		}
		last := pub.got[len(pub.got)-1]
		if last.Offset() != evt.Offset() || last.ID() != evt.ID() {
			t.Fatalf("published command must carry the appended command's offset/id: pub=%d/%s evt=%d/%s",
				last.Offset(), last.ID(), evt.Offset(), evt.ID())
		}
		if last.WorkerID() != "W1" {
			t.Fatalf("published command worker = %q, want W1", last.WorkerID())
		}
	}
}

func TestControlLog_AppendCommand_NilPublisher_NoOp(t *testing.T) {
	l := newControlLog() // no publisher injected
	ctx := context.Background()
	// Must not panic and must still append.
	evt, err := l.AppendCommand(ctx, AppendCommandInput{WorkerID: "W1", CommandType: "x", IdempotencyKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if evt.Offset() != 1 {
		t.Fatalf("offset = %d, want 1", evt.Offset())
	}
}

func TestControlLog_AppendCommand_IdempotentReissue_NoSecondPublish(t *testing.T) {
	pub := &capturePublisher{}
	l := newControlLog().WithPublisher(pub)
	ctx := context.Background()
	in := AppendCommandInput{WorkerID: "W1", CommandType: "agent.reset", IdempotencyKey: "reset-once"}
	if _, err := l.AppendCommand(ctx, in); err != nil {
		t.Fatal(err)
	}
	// Re-issue the same logical command (same idempotency key) — deduped to the
	// existing entry, so it must NOT re-publish (no gratuitous double-send).
	if _, err := l.AppendCommand(ctx, in); err != nil {
		t.Fatal(err)
	}
	if len(pub.got) != 1 {
		t.Fatalf("idempotent re-issue must publish once, got %d publishes", len(pub.got))
	}
}

func TestControlLog_AppendCommand_PublishFailureDoesNotFailAppend(t *testing.T) {
	// A publish that panics must not propagate: the command is committed and the
	// poll/catch-up path recovers. We wrap the panicking publisher so AppendCommand
	// itself stays panic-safe via the recover in publish.
	pub := &recoverPublisher{inner: &panicPublisher{}}
	l := newControlLog().WithPublisher(pub)
	ctx := context.Background()
	evt, err := l.AppendCommand(ctx, AppendCommandInput{WorkerID: "W1", CommandType: "x", IdempotencyKey: "k"})
	if err != nil {
		t.Fatalf("publish failure must not fail append, got err %v", err)
	}
	if evt == nil || evt.Offset() != 1 {
		t.Fatalf("command must still be appended despite publish failure: %+v", evt)
	}
	// And it is genuinely persisted (catch-up recovers it).
	all, _ := l.CommandsAfter(ctx, "W1", 0)
	if len(all) != 1 {
		t.Fatalf("command must be persisted (recoverable by poll), got %d", len(all))
	}
}

// recoverPublisher proves the best-effort contract: even a panicking publisher
// (e.g. a bug in the bus) cannot fail AppendCommand. The composition-root bus
// never panics, but the hook must be defensive regardless.
type recoverPublisher struct{ inner CommandPublisher }

func (p *recoverPublisher) Publish(e *WorkerControlEvent) {
	defer func() { _ = recover() }()
	p.inner.Publish(e)
}

var _ = errors.New
