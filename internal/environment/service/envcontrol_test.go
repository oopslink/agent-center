package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/environment"
	envsql "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
)

func newTestSvc(t *testing.T) (context.Context, *sql.DB, *EnvControl) {
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
	clk := clock.NewFakeClock(time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC))
	svc := New(Deps{
		DB:      db,
		Workers: envsql.NewWorkerRepo(db),
		Events:  envsql.NewControlEventRepo(db),
		IDGen:   idgen.NewGenerator(clk),
		Clock:   clk,
	})
	return ctx, db, svc
}

func TestEnvControl_ConnectWorker_CreateThenConnect(t *testing.T) {
	ctx, _, svc := newTestSvc(t)

	// First connect auto-creates the Worker AR (offline → online),
	// last_acked_offset 0. v2.7 #140 step-3: no org stored on the AR.
	w, err := svc.ConnectWorker(ctx, "w1")
	if err != nil {
		t.Fatalf("ConnectWorker(create): %v", err)
	}
	if w.Status() != environment.WorkerOnline {
		t.Fatalf("status = %q, want online", w.Status())
	}
	if w.LastAckedOffset() != 0 {
		t.Fatalf("last_acked_offset = %d, want 0", w.LastAckedOffset())
	}

	// Second connect loads the existing AR (does not error / re-create) and
	// stays online — the ack cursor is preserved (control-channel state intact).
	w2, err := svc.ConnectWorker(ctx, "w1")
	if err != nil {
		t.Fatalf("ConnectWorker(reconnect): %v", err)
	}
	if w2.Status() != environment.WorkerOnline {
		t.Fatalf("status = %q, want online", w2.Status())
	}
}

func TestEnvControl_AckWorker_Monotonic(t *testing.T) {
	ctx, _, svc := newTestSvc(t)
	if _, err := svc.ConnectWorker(ctx, "w1"); err != nil {
		t.Fatalf("connect: %v", err)
	}

	w, err := svc.AckWorker(ctx, "w1", 3)
	if err != nil {
		t.Fatalf("ack 3: %v", err)
	}
	if w.LastAckedOffset() != 3 {
		t.Fatalf("offset = %d, want 3", w.LastAckedOffset())
	}

	// Stale ack (offset <= current) is a tolerated no-op, not an error.
	w, err = svc.AckWorker(ctx, "w1", 2)
	if err != nil {
		t.Fatalf("stale ack: %v", err)
	}
	if w.LastAckedOffset() != 3 {
		t.Fatalf("offset = %d, want 3 (unchanged)", w.LastAckedOffset())
	}

	// Forward ack advances.
	w, err = svc.AckWorker(ctx, "w1", 5)
	if err != nil {
		t.Fatalf("ack 5: %v", err)
	}
	if w.LastAckedOffset() != 5 {
		t.Fatalf("offset = %d, want 5", w.LastAckedOffset())
	}
}

func TestEnvControl_CommandsAfter(t *testing.T) {
	ctx, _, svc := newTestSvc(t)
	if _, err := svc.ConnectWorker(ctx, "w1"); err != nil {
		t.Fatalf("connect: %v", err)
	}

	for _, ik := range []string{"k1", "k2", "k3"} {
		if _, err := svc.EnqueueCommand(ctx, environment.AppendCommandInput{
			WorkerID:       "w1",
			CommandType:    "agent.start",
			IdempotencyKey: ik,
		}); err != nil {
			t.Fatalf("enqueue %s: %v", ik, err)
		}
	}

	all, err := svc.CommandsAfter(ctx, "w1", 0)
	if err != nil {
		t.Fatalf("CommandsAfter(0): %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("len = %d, want 3", len(all))
	}
	// Ascending offsets 1,2,3.
	for i, c := range all {
		if c.Offset() != int64(i+1) {
			t.Fatalf("cmd[%d] offset = %d, want %d", i, c.Offset(), i+1)
		}
	}

	after2, err := svc.CommandsAfter(ctx, "w1", 2)
	if err != nil {
		t.Fatalf("CommandsAfter(2): %v", err)
	}
	if len(after2) != 1 || after2[0].Offset() != 3 {
		t.Fatalf("after2 = %+v, want only offset 3", after2)
	}
}

func TestEnvControl_New_DefaultsClock(t *testing.T) {
	// nil clock must fall back to SystemClock (no panic on Now()).
	svc := New(Deps{})
	if svc.clock == nil {
		t.Fatal("clock not defaulted")
	}
}

func TestEnvControl_AckWorker_UnknownWorker(t *testing.T) {
	ctx, _, svc := newTestSvc(t)
	if _, err := svc.AckWorker(ctx, "nope", 1); err == nil {
		t.Fatal("AckWorker(unknown): want error, got nil")
	}
}

func TestEnvControl_EnqueueCommand_Idempotent(t *testing.T) {
	ctx, _, svc := newTestSvc(t)
	if _, err := svc.ConnectWorker(ctx, "w1"); err != nil {
		t.Fatalf("connect: %v", err)
	}
	first, err := svc.EnqueueCommand(ctx, environment.AppendCommandInput{
		WorkerID: "w1", CommandType: "agent.reset", IdempotencyKey: "k",
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	again, err := svc.EnqueueCommand(ctx, environment.AppendCommandInput{
		WorkerID: "w1", CommandType: "agent.reset", IdempotencyKey: "k",
	})
	if err != nil {
		t.Fatalf("enqueue again: %v", err)
	}
	if first.Offset() != again.Offset() {
		t.Fatalf("re-issue created new offset %d != %d (not deduped)", again.Offset(), first.Offset())
	}
	all, err := svc.CommandsAfter(ctx, "w1", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("len = %d, want 1 (deduped)", len(all))
	}
}

func TestEnvControl_Heartbeat(t *testing.T) {
	ctx, _, svc := newTestSvc(t)
	if _, err := svc.ConnectWorker(ctx, "w1"); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := svc.Heartbeat(ctx, "w1"); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	// Heartbeat on an unknown worker surfaces ErrWorkerNotFound.
	if err := svc.Heartbeat(ctx, "nope"); err == nil {
		t.Fatalf("heartbeat(unknown): want error, got nil")
	}
}
