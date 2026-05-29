package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
)

func newDB(t *testing.T) (*OutboxRepo, *AppliedRepo) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewOutboxRepo(db), NewAppliedRepo(db)
}

func mkEvent(t *testing.T) outbox.Event {
	t.Helper()
	return outbox.Event{ID: idgen.MustNewULID(), EventType: "pm.task.assigned", CreatedAt: time.Now().UTC()}
}

// countingProjector records how many times Project actually ran per event ID.
type countingProjector struct {
	name  string
	calls map[string]int
	fail  bool
}

func (p *countingProjector) Name() string { return p.name }
func (p *countingProjector) Project(_ context.Context, e outbox.Event) error {
	if p.fail {
		return errors.New("boom")
	}
	if p.calls == nil {
		p.calls = map[string]int{}
	}
	p.calls[e.ID]++
	return nil
}

func TestOutboxRepo_AppendFetchMarkProcessed(t *testing.T) {
	repo, _ := newDB(t)
	ctx := context.Background()
	e := mkEvent(t)
	if err := repo.Append(ctx, e); err != nil {
		t.Fatal(err)
	}
	un, err := repo.FetchUnprocessed(ctx, 10)
	if err != nil || len(un) != 1 || un[0].ID != e.ID {
		t.Fatalf("FetchUnprocessed = %+v, %v", un, err)
	}
	// Defaults applied: empty refs/payload become {}.
	if un[0].Refs != "{}" || un[0].Payload != "{}" {
		t.Fatalf("refs/payload defaults: %q %q", un[0].Refs, un[0].Payload)
	}
	if err := repo.MarkProcessed(ctx, e.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	un, _ = repo.FetchUnprocessed(ctx, 10)
	if len(un) != 0 {
		t.Fatalf("expected 0 unprocessed after MarkProcessed, got %d", len(un))
	}
}

// TestRelay_IdempotentAcrossRuns is the core OQ1 guarantee: re-running the
// relay (redelivery) applies each projector at most once per event_id.
func TestRelay_IdempotentAcrossRuns(t *testing.T) {
	repo, applied := newDB(t)
	ctx := context.Background()
	e := mkEvent(t)
	if err := repo.Append(ctx, e); err != nil {
		t.Fatal(err)
	}
	proj := &countingProjector{name: "participant-sync"}
	relay := outbox.NewRelay(repo, applied, clock.SystemClock{}, proj)

	n1, err := relay.RunOnce(ctx, 100)
	if err != nil || n1 != 1 {
		t.Fatalf("first RunOnce = %d, %v; want 1", n1, err)
	}
	// Second pass: event already processed AND projector already applied —
	// no double application.
	n2, err := relay.RunOnce(ctx, 100)
	if err != nil || n2 != 0 {
		t.Fatalf("second RunOnce = %d, %v; want 0", n2, err)
	}
	if proj.calls[e.ID] != 1 {
		t.Fatalf("projector applied %d times, want exactly 1 (idempotent)", proj.calls[e.ID])
	}
}

// TestRelay_FailingProjectorLeavesEventUnprocessed verifies a projector error
// keeps the event for retry instead of marking it done.
func TestRelay_FailingProjectorLeavesEventUnprocessed(t *testing.T) {
	repo, applied := newDB(t)
	ctx := context.Background()
	e := mkEvent(t)
	_ = repo.Append(ctx, e)

	failing := &countingProjector{name: "p", fail: true}
	relay := outbox.NewRelay(repo, applied, clock.SystemClock{}, failing)
	n, err := relay.RunOnce(ctx, 100)
	if err != nil || n != 0 {
		t.Fatalf("RunOnce with failing projector = %d, %v; want 0", n, err)
	}
	un, _ := repo.FetchUnprocessed(ctx, 10)
	if len(un) != 1 {
		t.Fatalf("failing projector should leave event unprocessed, got %d", len(un))
	}

	// Once the projector recovers, a later pass applies it exactly once.
	failing.fail = false
	if _, err := relay.RunOnce(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if failing.calls[e.ID] != 1 {
		t.Fatalf("recovered projector applied %d times, want 1", failing.calls[e.ID])
	}
	if un, _ := repo.FetchUnprocessed(ctx, 10); len(un) != 0 {
		t.Fatalf("event should be processed after recovery, %d remain", len(un))
	}
}

func TestAppliedStore_DedupByEventID(t *testing.T) {
	_, applied := newDB(t)
	ctx := context.Background()
	id := idgen.MustNewULID()
	if ok, _ := applied.IsApplied(ctx, "p", id); ok {
		t.Fatal("should not be applied yet")
	}
	if err := applied.MarkApplied(ctx, "p", id, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if ok, _ := applied.IsApplied(ctx, "p", id); !ok {
		t.Fatal("should be applied")
	}
	// Re-marking is a harmless no-op (PK dedup), not an error.
	if err := applied.MarkApplied(ctx, "p", id, time.Now().UTC()); err != nil {
		t.Fatalf("re-mark should be no-op, got %v", err)
	}
	// Different projector is independent.
	if ok, _ := applied.IsApplied(ctx, "other", id); ok {
		t.Fatal("other projector should be independent")
	}
}
