package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
)

func mkExecInDB(t *testing.T) {
	t.Helper()
	// Helper to set up minimum needed for FK constraints.
}

func TestInputRequestRepo_SaveFindRoundtrip(t *testing.T) {
	db := openTestDB(t)
	mkTaskInDB(t, db, "T-1")
	execRepo := NewTaskExecutionRepo(db)
	if err := execRepo.Save(context.Background(), mkExec(t, "E-1", "T-1")); err != nil {
		t.Fatal(err)
	}
	repo := NewInputRequestRepo(db)
	ctx := context.Background()
	ir, err := inputrequest.New(inputrequest.NewInput{
		ID:              "IR-1",
		TaskExecutionID: "E-1",
		Question:        "proceed?",
		Options:         []string{"yes", "no"},
		Urgency:         inputrequest.UrgencyNormal,
		Now:             refTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, ir); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := repo.FindByID(ctx, "IR-1")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.Status() != inputrequest.StatusPending {
		t.Fatalf("status: %s", got.Status())
	}
	if len(got.Options()) != 2 {
		t.Fatalf("options: %+v", got.Options())
	}
}

func TestInputRequestRepo_NotFoundAndNilGuard(t *testing.T) {
	db := openTestDB(t)
	repo := NewInputRequestRepo(db)
	if _, err := repo.FindByID(context.Background(), "X"); !errors.Is(err, inputrequest.ErrInputRequestNotFound) {
		t.Fatalf("expected not_found: %v", err)
	}
	if err := repo.Save(context.Background(), nil); err == nil {
		t.Fatal("expected nil error")
	}
	if err := repo.Update(context.Background(), nil); err == nil {
		t.Fatal("expected nil error")
	}
}

func TestInputRequestRepo_Update_HappyAndConflict(t *testing.T) {
	db := openTestDB(t)
	mkTaskInDB(t, db, "T-1")
	if err := NewTaskExecutionRepo(db).Save(context.Background(), mkExec(t, "E-1", "T-1")); err != nil {
		t.Fatal(err)
	}
	repo := NewInputRequestRepo(db)
	ctx := context.Background()
	ir, _ := inputrequest.New(inputrequest.NewInput{
		ID: "IR-1", TaskExecutionID: "E-1", Question: "q", Now: refTime,
	})
	if err := repo.Save(ctx, ir); err != nil {
		t.Fatal(err)
	}
	if err := ir.Respond(inputrequest.InputResponse{
		Answer: "yes", DecidedBy: "user:hayang", DecidedAt: refTime.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(ctx, ir); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := repo.FindByID(ctx, "IR-1")
	if got.Status() != inputrequest.StatusResponded {
		t.Fatalf("status: %s", got.Status())
	}
	// CAS conflict
	stale, _ := repo.FindByID(ctx, "IR-1")
	_ = stale.MarkCanceled("kill", "abandoned", refTime.Add(2*time.Second))
	// stale is now in canceled but already responded in DB; update should
	// version conflict (since IR is responded in db, stale version=2)
	if err := repo.Update(ctx, stale); err == nil {
		t.Fatal("expected error")
	}
}

func TestInputRequestRepo_Update_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewInputRequestRepo(db)
	ir, _ := inputrequest.New(inputrequest.NewInput{
		ID: "IR-NF", TaskExecutionID: "E-1", Question: "q", Now: refTime,
	})
	_ = ir.MarkCanceled("r", "m", refTime.Add(time.Second))
	if err := repo.Update(context.Background(), ir); !errors.Is(err, inputrequest.ErrInputRequestNotFound) {
		t.Fatalf("expected not_found: %v", err)
	}
}

func TestInputRequestRepo_FindByExecAndPending(t *testing.T) {
	db := openTestDB(t)
	mkTaskInDB(t, db, "T-1")
	if err := NewTaskExecutionRepo(db).Save(context.Background(), mkExec(t, "E-1", "T-1")); err != nil {
		t.Fatal(err)
	}
	repo := NewInputRequestRepo(db)
	ctx := context.Background()
	for i, opts := range [][]string{nil, {"a", "b"}} {
		_ = i
		_ = opts
	}
	ir, _ := inputrequest.New(inputrequest.NewInput{
		ID: "IR-1", TaskExecutionID: "E-1", Question: "q1", Now: refTime,
	})
	if err := repo.Save(ctx, ir); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindByTaskExecutionID(ctx, "E-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != "IR-1" {
		t.Fatalf("by exec: %s", got.ID())
	}
	if _, err := repo.FindByTaskExecutionID(ctx, "E-NONE"); !errors.Is(err, inputrequest.ErrInputRequestNotFound) {
		t.Fatalf("expected not_found: %v", err)
	}
	pending, err := repo.FindPending(ctx, refTime.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending: %d", len(pending))
	}
}
