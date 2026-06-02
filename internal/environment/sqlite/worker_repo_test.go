package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	env "github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/persistence"
)

func newTestDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return ctx, db
}

func mustWorker(t *testing.T, id, name string) *env.Worker {
	t.Helper()
	w, err := env.NewWorker(env.NewWorkerInput{
		ID: env.WorkerID(id), Name: name,
		CreatedAt: time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	return w
}

func TestWorkerRepo_RoundTrip(t *testing.T) {
	ctx, db := newTestDB(t)
	repo := NewWorkerRepo(db)

	w := mustWorker(t, "w1", "alpha")
	if err := repo.Save(ctx, w); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByID(ctx, "w1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.ID() != "w1" || got.Name() != "alpha" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.Status() != env.WorkerOffline {
		t.Fatalf("status: got %q want offline", got.Status())
	}
	if got.LastAckedOffset() != 0 || got.Version() != 1 {
		t.Fatalf("offset/version: got %d/%d", got.LastAckedOffset(), got.Version())
	}
	if !got.CreatedAt().Equal(w.CreatedAt()) {
		t.Fatalf("created_at: got %v want %v", got.CreatedAt(), w.CreatedAt())
	}

	// Update: connect + ack advances status/heartbeat/offset/version.
	at := time.Date(2026, 5, 29, 13, 0, 0, 0, time.UTC)
	w.Connect(at)
	w.AckOffset(5, at)
	if err := repo.Update(ctx, w); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = repo.FindByID(ctx, "w1")
	if err != nil {
		t.Fatalf("FindByID after update: %v", err)
	}
	if got.Status() != env.WorkerOnline {
		t.Fatalf("status after connect: got %q", got.Status())
	}
	if got.LastAckedOffset() != 5 {
		t.Fatalf("offset after ack: got %d want 5", got.LastAckedOffset())
	}
	if !got.LastHeartbeatAt().Equal(at) {
		t.Fatalf("heartbeat: got %v want %v", got.LastHeartbeatAt(), at)
	}
	if got.Version() < 2 {
		t.Fatalf("version not bumped: %d", got.Version())
	}
}

func TestWorkerRepo_FindByID_NotFound(t *testing.T) {
	ctx, db := newTestDB(t)
	repo := NewWorkerRepo(db)
	_, err := repo.FindByID(ctx, "missing")
	if !errors.Is(err, env.ErrWorkerNotFound) {
		t.Fatalf("got %v want ErrWorkerNotFound", err)
	}
}

func TestWorkerRepo_Update_NotFound(t *testing.T) {
	ctx, db := newTestDB(t)
	repo := NewWorkerRepo(db)
	w := mustWorker(t, "ghost", "x")
	if err := repo.Update(ctx, w); !errors.Is(err, env.ErrWorkerNotFound) {
		t.Fatalf("got %v want ErrWorkerNotFound", err)
	}
}

func TestWorkerRepo_Save_Duplicate(t *testing.T) {
	ctx, db := newTestDB(t)
	repo := NewWorkerRepo(db)
	w := mustWorker(t, "w1", "alpha")
	if err := repo.Save(ctx, w); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	if err := repo.Save(ctx, w); !errors.Is(err, env.ErrWorkerExists) {
		t.Fatalf("duplicate Save: got %v want ErrWorkerExists", err)
	}
}

// v2.7 #140 step-3: TestWorkerRepo_ListByOrg removed — ListByOrg is gone (org is
// no longer stored on the control-channel Worker; the Environment page worker
// list reads canonical workforce.Worker via the webconsole handler).
