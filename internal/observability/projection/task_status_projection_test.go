package projection_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/observability/projection"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
)

type fakeWriter struct {
	called  bool
	gotInfo projection.TerminalTaskInfo
	err     error
}

func (f *fakeWriter) ApplyTerminal(_ context.Context, info projection.TerminalTaskInfo) error {
	f.called = true
	f.gotInfo = info
	return f.err
}

func TestStatusProjection_RequiresTx(t *testing.T) {
	w := &fakeWriter{}
	s := projection.NewTaskStatusProjectionService(w)
	err := s.OnExecutionTerminal(context.Background(), projection.TerminalTaskInfo{TaskExecutionID: "E-1", TerminalStatus: "completed"})
	if !errors.Is(err, projection.ErrNotInTransaction) {
		t.Fatalf("expected ErrNotInTransaction, got %v", err)
	}
	if w.called {
		t.Fatal("writer should NOT be called without tx")
	}
}

func TestStatusProjection_WithTx_InvokesWriter(t *testing.T) {
	w := &fakeWriter{}
	s := projection.NewTaskStatusProjectionService(w)
	// Use a real in-memory SQLite so we can make a real *sql.Tx.
	db, err := persistence.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	ctx := persistence.WithTx(context.Background(), tx)
	info := projection.TerminalTaskInfo{TaskExecutionID: taskruntime.TaskExecutionID("E-1"), TerminalStatus: "completed"}
	if err := s.OnExecutionTerminal(ctx, info); err != nil {
		t.Fatalf("OnExecutionTerminal: %v", err)
	}
	if !w.called {
		t.Fatal("writer should be called")
	}
	if w.gotInfo != info {
		t.Fatalf("info mismatch: %+v", w.gotInfo)
	}
}

func TestStatusProjection_WriterError_Wrapped(t *testing.T) {
	boom := errors.New("apply boom")
	w := &fakeWriter{err: boom}
	s := projection.NewTaskStatusProjectionService(w)
	db, _ := persistence.Open(t.TempDir() + "/test.db")
	defer db.Close()
	tx, _ := db.BeginTx(context.Background(), nil)
	defer tx.Rollback()
	ctx := persistence.WithTx(context.Background(), tx)
	err := s.OnExecutionTerminal(ctx, projection.TerminalTaskInfo{TaskExecutionID: "E-1", TerminalStatus: "failed"})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped writer err, got %v", err)
	}
}

func TestStatusProjection_Validation(t *testing.T) {
	s := projection.NewTaskStatusProjectionService(&fakeWriter{})
	db, _ := persistence.Open(t.TempDir() + "/test.db")
	defer db.Close()
	tx, _ := db.BeginTx(context.Background(), nil)
	defer tx.Rollback()
	ctx := persistence.WithTx(context.Background(), tx)
	if err := s.OnExecutionTerminal(ctx, projection.TerminalTaskInfo{}); err == nil {
		t.Fatal("expected validation error on empty info")
	}
	if err := s.OnExecutionTerminal(ctx, projection.TerminalTaskInfo{TaskExecutionID: "E-1"}); err == nil {
		t.Fatal("expected error on missing terminal_status")
	}
}

func TestStatusProjection_NilReceiver(t *testing.T) {
	var s *projection.TaskStatusProjectionService
	if err := s.OnExecutionTerminal(context.Background(), projection.TerminalTaskInfo{TaskExecutionID: "E-1", TerminalStatus: "completed"}); err == nil {
		t.Fatal("expected nil-receiver guard")
	}
}

// Sanity: confirm sql.Tx-based ctx mechanism works as expected.
var _ = sql.ErrNoRows
