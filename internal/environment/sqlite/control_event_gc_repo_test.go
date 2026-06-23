package sqlite

import (
	"context"
	"strconv"
	"testing"
	"time"

	env "github.com/oopslink/agent-center/internal/environment"
)

// mustEventAt builds a control event with an explicit created_at (the GC tests need
// to straddle the retention cutoff).
func mustEventAt(t *testing.T, id, workerID string, offset int64, key, cmd string, createdAt time.Time) *env.WorkerControlEvent {
	t.Helper()
	e, err := env.NewWorkerControlEvent(env.NewWorkerControlEventInput{
		ID: id, WorkerID: env.WorkerID(workerID), Offset: offset,
		IdempotencyKey: key, CommandType: cmd, Payload: `{}`,
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("NewWorkerControlEvent: %v", err)
	}
	return e
}

// saveWorkerAck seeds an env_workers row with a specific last_acked_offset so the GC
// ack-gate has something to join against.
func saveWorkerAck(t *testing.T, ctx context.Context, repo *WorkerRepo, id string, lastAcked int64) {
	t.Helper()
	w, err := env.RehydrateWorker(env.RehydrateWorkerInput{
		ID: env.WorkerID(id), Name: id, Status: env.WorkerOffline,
		LastAckedOffset: lastAcked,
		CreatedAt:       time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Version:         1,
	})
	if err != nil {
		t.Fatalf("RehydrateWorker: %v", err)
	}
	if err := repo.Save(ctx, w); err != nil {
		t.Fatalf("Save worker %s: %v", id, err)
	}
}

func remainingIDs(t *testing.T, ctx context.Context, repo *ControlEventRepo, workerID string) map[string]bool {
	t.Helper()
	list, err := repo.ListAfter(ctx, env.WorkerID(workerID), 0)
	if err != nil {
		t.Fatalf("ListAfter: %v", err)
	}
	out := map[string]bool{}
	for _, e := range list {
		out[e.ID()] = true
	}
	return out
}

// Core semantics: GC deletes acked rows older than the cutoff, KEEPS un-acked old rows
// (offset > last_acked → the offline-worker replay safety guard) and KEEPS recent rows.
func TestControlEventGC_DeleteAckedBefore_AckGate(t *testing.T) {
	ctx, db := newTestDB(t)
	repo := NewControlEventRepo(db)
	wrepo := NewWorkerRepo(db)

	old := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)    // before cutoff
	recent := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC) // after cutoff
	cutoff := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)

	// Worker acked through offset 2.
	saveWorkerAck(t, ctx, wrepo, "w1", 2)

	// e1/e2: old + acked → DELETE. e3: old but UN-acked (offset 3 > 2) → KEEP
	// (the offline-worker replay guard). e4: recent + acked-range → KEEP (within retention).
	mustAppend(t, ctx, repo, mustEventAt(t, "e1", "w1", 1, "k1", "agent.reconcile", old))
	mustAppend(t, ctx, repo, mustEventAt(t, "e2", "w1", 2, "k2", "agent.work_available", old))
	mustAppend(t, ctx, repo, mustEventAt(t, "e3", "w1", 3, "k3", "agent.converse", old))
	mustAppend(t, ctx, repo, mustEventAt(t, "e4", "w1", 4, "k4", "agent.reconcile", recent))

	n, err := repo.DeleteAckedBefore(ctx, cutoff, 500)
	if err != nil {
		t.Fatalf("DeleteAckedBefore: %v", err)
	}
	if n != 2 {
		t.Fatalf("deleted = %d, want 2 (e1,e2)", n)
	}
	got := remainingIDs(t, ctx, repo, "w1")
	if got["e1"] || got["e2"] {
		t.Fatalf("acked old rows should be gone, remaining=%v", got)
	}
	if !got["e3"] {
		t.Fatalf("un-acked old row e3 MUST survive (offline-worker replay guard), remaining=%v", got)
	}
	if !got["e4"] {
		t.Fatalf("recent row e4 MUST survive (within retention), remaining=%v", got)
	}
}

// Safety-gate regression (the load-bearing one): a worker offline PAST retention still
// replays every un-acked command on reconnect — GC removed only the acked tail.
func TestControlEventGC_OfflineWorkerStillReplaysUnacked(t *testing.T) {
	ctx, db := newTestDB(t)
	repo := NewControlEventRepo(db)
	wrepo := NewWorkerRepo(db)

	old := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	cutoff := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)

	// Worker acked offset 1, then went offline; offsets 2 (a converse) and 3 are
	// undelivered and now older than retention.
	saveWorkerAck(t, ctx, wrepo, "w1", 1)
	mustAppend(t, ctx, repo, mustEventAt(t, "e1", "w1", 1, "k1", "agent.reconcile", old))
	mustAppend(t, ctx, repo, mustEventAt(t, "e2", "w1", 2, "k2", "agent.converse", old))
	mustAppend(t, ctx, repo, mustEventAt(t, "e3", "w1", 3, "k3", "agent.reconcile", old))

	if _, err := repo.DeleteAckedBefore(ctx, cutoff, 500); err != nil {
		t.Fatalf("DeleteAckedBefore: %v", err)
	}

	// Reconnect replay = CommandsAfter(last_acked=1): must still return offsets 2,3.
	replay, err := repo.ListAfter(ctx, "w1", 1)
	if err != nil {
		t.Fatalf("ListAfter: %v", err)
	}
	if len(replay) != 2 || replay[0].Offset() != 2 || replay[1].Offset() != 3 {
		t.Fatalf("offline worker lost undelivered state: replay=%+v want offsets [2,3]", replay)
	}
}

// Orphan rows (worker no longer in env_workers) are pruned by time alone.
func TestControlEventGC_OrphanWorkerPrunedByTimeOnly(t *testing.T) {
	ctx, db := newTestDB(t)
	repo := NewControlEventRepo(db)

	old := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	cutoff := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)

	// No env_workers row for "ghost".
	mustAppend(t, ctx, repo, mustEventAt(t, "g1", "ghost", 1, "k1", "agent.reconcile", old))
	mustAppend(t, ctx, repo, mustEventAt(t, "g2", "ghost", 2, "k2", "agent.reconcile", recent))

	n, err := repo.DeleteAckedBefore(ctx, cutoff, 500)
	if err != nil {
		t.Fatalf("DeleteAckedBefore: %v", err)
	}
	if n != 1 {
		t.Fatalf("deleted = %d, want 1 (old orphan only)", n)
	}
	got := remainingIDs(t, ctx, repo, "ghost")
	if got["g1"] {
		t.Fatalf("old orphan row should be pruned, remaining=%v", got)
	}
	if !got["g2"] {
		t.Fatalf("recent orphan row should survive (within retention), remaining=%v", got)
	}
}

// Batching: the LIMIT bounds one DELETE; a second call drains the rest.
func TestControlEventGC_DeleteAckedBefore_BatchLimit(t *testing.T) {
	ctx, db := newTestDB(t)
	repo := NewControlEventRepo(db)
	wrepo := NewWorkerRepo(db)

	old := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	cutoff := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)

	saveWorkerAck(t, ctx, wrepo, "w1", 10)
	for i := int64(1); i <= 5; i++ {
		mustAppend(t, ctx, repo, mustEventAt(t, idFor(i), "w1", i, keyFor(i), "agent.reconcile", old))
	}

	first, err := repo.DeleteAckedBefore(ctx, cutoff, 2)
	if err != nil {
		t.Fatalf("batch 1: %v", err)
	}
	if first != 2 {
		t.Fatalf("batch 1 deleted = %d, want 2 (LIMIT)", first)
	}
	second, err := repo.DeleteAckedBefore(ctx, cutoff, 2)
	if err != nil {
		t.Fatalf("batch 2: %v", err)
	}
	if second != 2 {
		t.Fatalf("batch 2 deleted = %d, want 2", second)
	}
	third, err := repo.DeleteAckedBefore(ctx, cutoff, 2)
	if err != nil {
		t.Fatalf("batch 3: %v", err)
	}
	if third != 1 {
		t.Fatalf("batch 3 deleted = %d, want 1 (drain)", third)
	}
	if len(remainingIDs(t, ctx, repo, "w1")) != 0 {
		t.Fatalf("all 5 acked old rows should be gone")
	}
}

func mustAppend(t *testing.T, ctx context.Context, repo *ControlEventRepo, e *env.WorkerControlEvent) {
	t.Helper()
	if err := repo.Append(ctx, e); err != nil {
		t.Fatalf("Append %s: %v", e.ID(), err)
	}
}

func idFor(i int64) string  { return "e" + strconv.FormatInt(i, 10) }
func keyFor(i int64) string { return "k" + strconv.FormatInt(i, 10) }
