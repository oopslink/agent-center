package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
)

func TestWorkerRepo_SaveAndFindByID(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	w := newWorker(t, "W-1")
	if err := repo.Save(context.Background(), w); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.FindByID(context.Background(), "W-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.ID() != "W-1" || got.Status() != workforce.WorkerOffline {
		t.Fatalf("got: id=%s status=%s", got.ID(), got.Status())
	}
	if got.Version() != 1 {
		t.Fatalf("version: %d", got.Version())
	}
	if caps := got.Capabilities(); len(caps) != 1 || caps[0] != "claude-code" {
		t.Fatalf("capabilities: %v", caps)
	}
}

func TestWorkerRepo_Save_Duplicate(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	w := newWorker(t, "W-1")
	if err := repo.Save(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	w2 := newWorker(t, "W-1")
	err := repo.Save(context.Background(), w2)
	if !errors.Is(err, workforce.ErrWorkerAlreadyExists) {
		t.Fatalf("expected ErrWorkerAlreadyExists, got %v", err)
	}
}

func TestWorkerRepo_FindByID_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	_, err := repo.FindByID(context.Background(), "W-MISSING")
	if !errors.Is(err, workforce.ErrWorkerNotFound) {
		t.Fatalf("expected ErrWorkerNotFound, got %v", err)
	}
}

func TestWorkerRepo_UpdateStatus_CASSuccess(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	w := newWorker(t, "W-1")
	_ = repo.Save(context.Background(), w)
	err := repo.UpdateStatus(context.Background(), "W-1", workforce.WorkerOffline, workforce.WorkerOnline, 1)
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	got, _ := repo.FindByID(context.Background(), "W-1")
	if got.Status() != workforce.WorkerOnline {
		t.Fatalf("status: %s", got.Status())
	}
	if got.Version() != 2 {
		t.Fatalf("version: %d", got.Version())
	}
}

func TestWorkerRepo_UpdateStatus_FromMismatch(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	_ = repo.Save(context.Background(), newWorker(t, "W-1"))
	// "from" is online but worker is offline → CAS fails
	err := repo.UpdateStatus(context.Background(), "W-1", workforce.WorkerOnline, workforce.WorkerOffline, 1)
	if !errors.Is(err, workforce.ErrWorkerVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

func TestWorkerRepo_UpdateStatus_VersionMismatch(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	_ = repo.Save(context.Background(), newWorker(t, "W-1"))
	err := repo.UpdateStatus(context.Background(), "W-1", workforce.WorkerOffline, workforce.WorkerOnline, 99)
	if !errors.Is(err, workforce.ErrWorkerVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

func TestWorkerRepo_UpdateStatus_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	err := repo.UpdateStatus(context.Background(), "W-NEVER", workforce.WorkerOffline, workforce.WorkerOnline, 1)
	if !errors.Is(err, workforce.ErrWorkerNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestWorkerRepo_UpdateStatus_InvalidStatus(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	err := repo.UpdateStatus(context.Background(), "W-1", "bogus", workforce.WorkerOnline, 1)
	if !errors.Is(err, workforce.ErrWorkerInvalidStatus) {
		t.Fatalf("expected invalid status, got %v", err)
	}
}

func TestWorkerRepo_UpdateLastHeartbeatAt(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	_ = repo.Save(context.Background(), newWorker(t, "W-1"))
	at := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	if err := repo.UpdateLastHeartbeatAt(context.Background(), "W-1", at, 60); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.FindByID(context.Background(), "W-1")
	if got.LastHeartbeatAt() == nil || !got.LastHeartbeatAt().Equal(at) {
		t.Fatalf("heartbeat: %v", got.LastHeartbeatAt())
	}
	if got.WorkingSeconds() != 60 {
		t.Fatalf("working_seconds: %d", got.WorkingSeconds())
	}
}

func TestWorkerRepo_UpdateLastHeartbeatAt_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	err := repo.UpdateLastHeartbeatAt(context.Background(), "W-NEVER", time.Now(), 1)
	if !errors.Is(err, workforce.ErrWorkerNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestWorkerRepo_FindByStatus(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	w1 := newWorker(t, "W-1")
	w2 := newWorker(t, "W-2")
	_ = repo.Save(context.Background(), w1)
	_ = repo.Save(context.Background(), w2)
	_ = repo.UpdateStatus(context.Background(), "W-1", workforce.WorkerOffline, workforce.WorkerOnline, 1)

	got, err := repo.FindByStatus(context.Background(), workforce.WorkerOnline)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID() != "W-1" {
		t.Fatalf("FindByStatus online: %v", got)
	}
	got, _ = repo.FindByStatus(context.Background(), workforce.WorkerOffline)
	if len(got) != 1 || got[0].ID() != "W-2" {
		t.Fatalf("FindByStatus offline: %v", got)
	}
}

func TestWorkerRepo_FindByStatus_Invalid(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	_, err := repo.FindByStatus(context.Background(), "bogus")
	if !errors.Is(err, workforce.ErrWorkerInvalidStatus) {
		t.Fatalf("expected invalid status, got %v", err)
	}
}

func TestWorkerRepo_FindAll(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	for _, id := range []workforce.WorkerID{"W-1", "W-2", "W-3"} {
		_ = repo.Save(context.Background(), newWorker(t, id))
	}
	got, err := repo.FindAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("FindAll: %d workers", len(got))
	}
}

func TestWorkerRepo_Save_RespectsTx(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	w := newWorker(t, "W-1")
	tx, _ := db.BeginTx(context.Background(), nil)
	ctx := persistence.WithTx(context.Background(), tx)
	if err := repo.Save(ctx, w); err != nil {
		t.Fatal(err)
	}
	// outside tx not visible
	if _, err := repo.FindByID(context.Background(), "W-1"); !errors.Is(err, workforce.ErrWorkerNotFound) {
		t.Fatalf("expected not visible: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindByID(context.Background(), "W-1"); !errors.Is(err, workforce.ErrWorkerNotFound) {
		t.Fatal("expected gone after rollback")
	}
}

func TestWorkerRepo_Save_NilWorker(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	if err := repo.Save(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil worker")
	}
}
