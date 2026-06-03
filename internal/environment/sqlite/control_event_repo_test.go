package sqlite

import (
	"errors"
	"testing"
	"time"

	env "github.com/oopslink/agent-center/internal/environment"
)

func mustEvent(t *testing.T, id, workerID string, offset int64, key, cmd string) *env.WorkerControlEvent {
	t.Helper()
	e, err := env.NewWorkerControlEvent(env.NewWorkerControlEventInput{
		ID: id, WorkerID: env.WorkerID(workerID), Offset: offset,
		IdempotencyKey: key, CommandType: cmd, Payload: `{}`,
		CreatedAt: time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewWorkerControlEvent: %v", err)
	}
	return e
}

func TestControlEventRepo_AppendAndMaxOffset(t *testing.T) {
	ctx, db := newTestDB(t)
	repo := NewControlEventRepo(db)

	if off, err := repo.MaxOffset(ctx, "w1"); err != nil || off != 0 {
		t.Fatalf("MaxOffset empty: got %d err=%v want 0", off, err)
	}

	if err := repo.Append(ctx, mustEvent(t, "e1", "w1", 1, "k1", "stop")); err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	if err := repo.Append(ctx, mustEvent(t, "e2", "w1", 2, "k2", "reset")); err != nil {
		t.Fatalf("Append 2: %v", err)
	}

	off, err := repo.MaxOffset(ctx, "w1")
	if err != nil {
		t.Fatalf("MaxOffset: %v", err)
	}
	if off != 2 {
		t.Fatalf("MaxOffset: got %d want 2", off)
	}

	// Stored offset round-trips via FindByIdempotencyKey.
	got, err := repo.FindByIdempotencyKey(ctx, "w1", "k2")
	if err != nil {
		t.Fatalf("FindByIdempotencyKey: %v", err)
	}
	if got == nil || got.Offset() != 2 || got.CommandType() != "reset" {
		t.Fatalf("FindByIdempotencyKey hit mismatch: %+v", got)
	}
}

func TestControlEventRepo_FindByIdempotencyKey_Miss(t *testing.T) {
	ctx, db := newTestDB(t)
	repo := NewControlEventRepo(db)
	got, err := repo.FindByIdempotencyKey(ctx, "w1", "nope")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		t.Fatalf("miss should return nil, got %+v", got)
	}
}

func TestControlEventRepo_ListAfter_Ordering(t *testing.T) {
	ctx, db := newTestDB(t)
	repo := NewControlEventRepo(db)

	// Insert out of order to prove ascending ordering on read.
	if err := repo.Append(ctx, mustEvent(t, "e3", "w1", 3, "k3", "stop")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Append(ctx, mustEvent(t, "e1", "w1", 1, "k1", "stop")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Append(ctx, mustEvent(t, "e2", "w1", 2, "k2", "stop")); err != nil {
		t.Fatal(err)
	}
	// Another worker's events must not leak.
	if err := repo.Append(ctx, mustEvent(t, "x1", "w2", 1, "kx", "stop")); err != nil {
		t.Fatal(err)
	}

	list, err := repo.ListAfter(ctx, "w1", 1)
	if err != nil {
		t.Fatalf("ListAfter: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListAfter(>1): got %d want 2", len(list))
	}
	if list[0].Offset() != 2 || list[1].Offset() != 3 {
		t.Fatalf("ordering: got %d,%d want 2,3", list[0].Offset(), list[1].Offset())
	}

	// offset 0 returns the whole stream for w1.
	all, err := repo.ListAfter(ctx, "w1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("ListAfter(>0): got %d want 3", len(all))
	}
}

func TestControlEventRepo_Append_DuplicateIdempotencyKey(t *testing.T) {
	ctx, db := newTestDB(t)
	repo := NewControlEventRepo(db)

	if err := repo.Append(ctx, mustEvent(t, "e1", "w1", 1, "k1", "stop")); err != nil {
		t.Fatalf("first Append: %v", err)
	}
	// Same (worker, key) at a different offset/id → ErrDuplicateIdempotencyKey.
	err := repo.Append(ctx, mustEvent(t, "e2", "w1", 2, "k1", "stop"))
	if !errors.Is(err, env.ErrDuplicateIdempotencyKey) {
		t.Fatalf("duplicate key: got %v want ErrDuplicateIdempotencyKey", err)
	}
}

func TestControlEventRepo_Append_DuplicateOffset(t *testing.T) {
	ctx, db := newTestDB(t)
	repo := NewControlEventRepo(db)

	if err := repo.Append(ctx, mustEvent(t, "e1", "w1", 1, "k1", "stop")); err != nil {
		t.Fatalf("first Append: %v", err)
	}
	// Same (worker, offset) with a distinct key → a UNIQUE error that is NOT
	// the idempotency-key sentinel.
	err := repo.Append(ctx, mustEvent(t, "e2", "w1", 1, "k2", "stop"))
	if err == nil {
		t.Fatal("duplicate offset should error")
	}
	if errors.Is(err, env.ErrDuplicateIdempotencyKey) {
		t.Fatalf("offset clash must not map to ErrDuplicateIdempotencyKey, got %v", err)
	}
}
