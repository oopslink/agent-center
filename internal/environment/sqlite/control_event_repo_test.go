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

func mustForkEvent(t *testing.T, id string, offset int64, payload string, status string) *env.WorkerControlEvent {
	t.Helper()
	e, err := env.NewWorkerControlEvent(env.NewWorkerControlEventInput{
		ID: id, WorkerID: "w1", Offset: offset, IdempotencyKey: id,
		CommandType: "agent.fork_executor", Payload: payload, Status: status,
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

func TestControlEventRepo_MaxOffsetUsesAckCursorAfterGCFullyPrunesStream(t *testing.T) {
	ctx, db := newTestDB(t)
	repo := NewControlEventRepo(db)
	wrepo := NewWorkerRepo(db)

	saveWorkerAck(t, ctx, wrepo, "w1", 9821)

	// Simulates the production failure mode: every acked control row has been
	// retention-GCed, but the worker still reconnects with last_acked_offset=9821.
	off, err := repo.MaxOffset(ctx, "w1")
	if err != nil {
		t.Fatalf("MaxOffset: %v", err)
	}
	if off != 9821 {
		t.Fatalf("MaxOffset must preserve worker ack high-water mark: got %d want 9821", off)
	}

	next := mustEvent(t, "e9822", "w1", off+1, "k9822", "agent.reconcile")
	if err := repo.Append(ctx, next); err != nil {
		t.Fatalf("Append after ack-only max: %v", err)
	}
	replay, err := repo.ListAfter(ctx, "w1", 9821)
	if err != nil {
		t.Fatalf("ListAfter: %v", err)
	}
	if len(replay) != 1 || replay[0].Offset() != 9822 {
		t.Fatalf("new command must be visible after ack cursor: got %+v", replay)
	}
}

func TestControlEventRepo_MaxOffsetUsesLargerRemainingStreamOffset(t *testing.T) {
	ctx, db := newTestDB(t)
	repo := NewControlEventRepo(db)
	wrepo := NewWorkerRepo(db)

	saveWorkerAck(t, ctx, wrepo, "w1", 7)
	if err := repo.Append(ctx, mustEvent(t, "e8", "w1", 8, "k8", "agent.reconcile")); err != nil {
		t.Fatalf("Append: %v", err)
	}

	off, err := repo.MaxOffset(ctx, "w1")
	if err != nil {
		t.Fatalf("MaxOffset: %v", err)
	}
	if off != 8 {
		t.Fatalf("MaxOffset: got %d want remaining stream max 8", off)
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

func TestControlEventRepo_CommandStatusTracksLegacyForkRows(t *testing.T) {
	ctx, db := newTestDB(t)
	repo := NewControlEventRepo(db)

	legacy := mustForkEvent(t, "cmd-legacy", 1, `{"agent_id":"agent-1","task_id":"task-1"}`, "")
	if err := repo.Append(ctx, legacy); err != nil {
		t.Fatalf("Append legacy: %v", err)
	}
	if err := repo.Append(ctx, mustForkEvent(t, "cmd-other", 2, `{"agent_id":"agent-1","task_id":"task-2"}`, "")); err != nil {
		t.Fatalf("Append other: %v", err)
	}

	list, err := repo.ListByAgentTask(ctx, "w1", "agent.fork_executor", "agent-1", "task-1")
	if err != nil {
		t.Fatalf("ListByAgentTask: %v", err)
	}
	if len(list) != 1 || list[0].ID() != "cmd-legacy" || list[0].AgentID() != "agent-1" || list[0].TaskID() != "task-1" {
		t.Fatalf("legacy command lookup = %+v", list)
	}
	latest, err := repo.LatestNonTerminalByAgentTask(ctx, "w1", "agent.fork_executor", "agent-1", "task-1")
	if err != nil {
		t.Fatalf("LatestNonTerminalByAgentTask: %v", err)
	}
	if latest == nil || latest.ID() != "cmd-legacy" {
		t.Fatalf("latest legacy command = %+v", latest)
	}

	updatedAt := time.Date(2026, 5, 29, 12, 15, 0, 0, time.UTC)
	updated, err := repo.UpdateStatus(ctx, env.UpdateCommandStatusInput{
		WorkerID: "w1", CommandID: "cmd-legacy", AgentID: "agent-1", TaskID: "task-1",
		Status: env.CommandStatusExpired, StatusReason: "runtime_command_timeout",
		StatusUpdatedAt: updatedAt,
	})
	if err != nil {
		t.Fatalf("UpdateStatus legacy: %v", err)
	}
	if updated.Status() != env.CommandStatusExpired || updated.AgentID() != "agent-1" || updated.TaskID() != "task-1" ||
		!updated.StatusUpdatedAt().Equal(updatedAt) {
		t.Fatalf("updated legacy command = %+v", updated)
	}
	latest, err = repo.LatestNonTerminalByAgentTask(ctx, "w1", "agent.fork_executor", "agent-1", "task-1")
	if err != nil {
		t.Fatalf("Latest after terminal: %v", err)
	}
	if latest != nil {
		t.Fatalf("terminal command must not remain non-terminal: %+v", latest)
	}
}
