package environment

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
)

// fakeEventRepo is an in-memory ControlEventRepository for the domain-core tests
// (the sqlite layer + its UNIQUE constraints land in the next D1 slice).
type fakeEventRepo struct {
	byWorker map[WorkerID][]*WorkerControlEvent
}

func newFakeEventRepo() *fakeEventRepo {
	return &fakeEventRepo{byWorker: map[WorkerID][]*WorkerControlEvent{}}
}

func (r *fakeEventRepo) Append(_ context.Context, e *WorkerControlEvent) error {
	r.byWorker[e.WorkerID()] = append(r.byWorker[e.WorkerID()], e)
	return nil
}

func (r *fakeEventRepo) MaxOffset(_ context.Context, w WorkerID) (int64, error) {
	var max int64
	for _, e := range r.byWorker[w] {
		if e.Offset() > max {
			max = e.Offset()
		}
	}
	return max, nil
}

func (r *fakeEventRepo) FindByIdempotencyKey(_ context.Context, w WorkerID, key string) (*WorkerControlEvent, error) {
	for _, e := range r.byWorker[w] {
		if e.IdempotencyKey() == key {
			return e, nil
		}
	}
	return nil, nil
}

func (r *fakeEventRepo) ListAfter(_ context.Context, w WorkerID, offset int64) ([]*WorkerControlEvent, error) {
	var out []*WorkerControlEvent
	for _, e := range r.byWorker[w] {
		if e.Offset() > offset {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Offset() < out[j].Offset() })
	return out, nil
}

func newControlLog() *ControlLog {
	clk := clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	return NewControlLog(newFakeEventRepo(), idgen.NewGenerator(clk), clk)
}

func TestControlLog_AssignsMonotonicOffsets(t *testing.T) {
	l := newControlLog()
	ctx := context.Background()
	for i, key := range []string{"k1", "k2", "k3"} {
		e, err := l.AppendCommand(ctx, AppendCommandInput{WorkerID: "W1", CommandType: "agent.start", IdempotencyKey: key})
		if err != nil {
			t.Fatal(err)
		}
		if e.Offset() != int64(i+1) {
			t.Fatalf("offset = %d, want %d", e.Offset(), i+1)
		}
	}
}

func TestControlLog_IdempotentAppend_NoDuplicateDestructive(t *testing.T) {
	l := newControlLog()
	ctx := context.Background()
	// A destructive command issued, then RE-issued with the same idempotency key
	// (e.g. the center retries) must NOT create a second stream entry.
	first, err := l.AppendCommand(ctx, AppendCommandInput{WorkerID: "W1", CommandType: "agent.reset", Payload: `{"scope":"all"}`, IdempotencyKey: "reset-once"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := l.AppendCommand(ctx, AppendCommandInput{WorkerID: "W1", CommandType: "agent.reset", Payload: `{"scope":"all"}`, IdempotencyKey: "reset-once"})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID() != first.ID() || again.Offset() != first.Offset() {
		t.Fatalf("re-issued command should be the same entry: first=%s/%d again=%s/%d",
			first.ID(), first.Offset(), again.ID(), again.Offset())
	}
	all, _ := l.CommandsAfter(ctx, "W1", 0)
	if len(all) != 1 {
		t.Fatalf("stream should hold exactly 1 entry, got %d", len(all))
	}
}

func TestControlLog_ReplayFromAckCursor_NoReDeliverProcessed(t *testing.T) {
	l := newControlLog()
	ctx := context.Background()
	for _, k := range []string{"a", "b", "c"} {
		if _, err := l.AppendCommand(ctx, AppendCommandInput{WorkerID: "W1", CommandType: "agent.start", IdempotencyKey: k}); err != nil {
			t.Fatal(err)
		}
	}
	// Worker processed + acked through offset 1, then reconnects.
	w, _ := NewWorker(NewWorkerInput{ID: "W1", OrganizationID: "org-1", CreatedAt: time.Unix(1, 0)})
	w.AckOffset(1, time.Unix(2, 0))
	replay, err := l.Replay(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if len(replay) != 2 || replay[0].Offset() != 2 || replay[1].Offset() != 3 {
		t.Fatalf("replay = %+v, want offsets [2,3] (already-acked 1 not re-delivered)", replay)
	}
	// Re-issuing one of the replayed destructive commands (same key) stays deduped.
	if _, err := l.AppendCommand(ctx, AppendCommandInput{WorkerID: "W1", CommandType: "agent.start", IdempotencyKey: "b"}); err != nil {
		t.Fatal(err)
	}
	all, _ := l.CommandsAfter(ctx, "W1", 0)
	if len(all) != 3 {
		t.Fatalf("stream should still hold 3 entries after re-issue, got %d", len(all))
	}
}

func TestControlLog_ValidatesInput(t *testing.T) {
	l := newControlLog()
	ctx := context.Background()
	if _, err := l.AppendCommand(ctx, AppendCommandInput{WorkerID: "W1", IdempotencyKey: "k"}); err != ErrEmptyCommandType {
		t.Fatalf("want ErrEmptyCommandType, got %v", err)
	}
	if _, err := l.AppendCommand(ctx, AppendCommandInput{WorkerID: "W1", CommandType: "agent.start"}); err != ErrEmptyIdempotencyKey {
		t.Fatalf("want ErrEmptyIdempotencyKey, got %v", err)
	}
}

// Per-worker isolation: offsets are independent across workers.
func TestControlLog_PerWorkerOffsets(t *testing.T) {
	l := newControlLog()
	ctx := context.Background()
	e1, _ := l.AppendCommand(ctx, AppendCommandInput{WorkerID: "W1", CommandType: "x", IdempotencyKey: "k"})
	e2, _ := l.AppendCommand(ctx, AppendCommandInput{WorkerID: "W2", CommandType: "x", IdempotencyKey: "k"})
	if e1.Offset() != 1 || e2.Offset() != 1 {
		t.Fatalf("each worker's stream starts at offset 1: W1=%d W2=%d", e1.Offset(), e2.Offset())
	}
}
