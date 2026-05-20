package kill

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	trsqlite "github.com/oopslink/agent-center/internal/taskruntime/sqlite"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

type harness struct {
	db        *sql.DB
	clk       *clock.FakeClock
	idgen     idgen.Generator
	taskRepo  *trsqlite.TaskRepo
	execRepo  *trsqlite.TaskExecutionRepo
	irRepo    *trsqlite.InputRequestRepo
	sink      *observability.EventSink
	eventRepo *obsqlite.EventRepo
	coord     *Coordinator
	sender    *recordingSender
}

type recordingSender struct {
	calls []struct {
		ID      taskruntime.TaskExecutionID
		Reason  execution.KilledReason
		Message string
	}
	wantFail bool
}

func (r *recordingSender) SendKill(_ context.Context, id taskruntime.TaskExecutionID, reason execution.KilledReason, msg string) error {
	r.calls = append(r.calls, struct {
		ID      taskruntime.TaskExecutionID
		Reason  execution.KilledReason
		Message string
	}{id, reason, msg})
	if r.wantFail {
		return errors.New("simulated kill transport failure")
	}
	return nil
}

func setupKill(t *testing.T) *harness {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO projects (id, name, created_at, updated_at, created_by_identity_id) VALUES ('P-1','P','2026-05-21T12:00:00Z','2026-05-21T12:00:00Z','user:hayang')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workers (id, status, capabilities, working_seconds, enrolled_at, created_at, updated_at) VALUES ('W-1','online','[]',0,'2026-05-21T12:00:00Z','2026-05-21T12:00:00Z','2026-05-21T12:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	er, err := obsqlite.NewEventRepo(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	sink := observability.NewEventSink(er, er, gen, clk)
	taskRepo := trsqlite.NewTaskRepo(db)
	execRepo := trsqlite.NewTaskExecutionRepo(db)
	irRepo := trsqlite.NewInputRequestRepo(db)
	sender := &recordingSender{}
	c := NewCoordinator(db, execRepo, taskRepo, irRepo, sink, sender, clk)
	return &harness{
		db: db, clk: clk, idgen: gen, taskRepo: taskRepo, execRepo: execRepo,
		irRepo: irRepo, sink: sink, eventRepo: er, coord: c, sender: sender,
	}
}

func seed(t *testing.T, h *harness, status execution.Status) (taskruntime.TaskID, taskruntime.TaskExecutionID) {
	t.Helper()
	tt, _ := task.New(task.NewInput{ID: "T-1", ProjectID: "P-1", Title: "x", CreatedBy: "user:hayang", Now: h.clk.Now()})
	if err := h.taskRepo.Save(context.Background(), tt); err != nil {
		t.Fatal(err)
	}
	e, _ := execution.New(execution.NewInput{
		ID: "E-1", TaskID: tt.ID(), WorkerID: "W-1", AgentCLI: "claude-code",
		WorkspaceMode: execution.WorkspaceWorktree, Now: h.clk.Now(),
	})
	switch status {
	case execution.StatusWorking:
		_ = e.AckDispatch(h.clk.Now())
		_ = e.StartWorking("/x", h.clk.Now())
	case execution.StatusInputRequired:
		_ = e.AckDispatch(h.clk.Now())
		_ = e.StartWorking("/x", h.clk.Now())
		_ = e.EnterInputRequired("IR-1", h.clk.Now())
	}
	if err := h.execRepo.Save(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	// Update task.current_execution_id
	_ = tt.SetCurrentExecutionID(e.ID(), h.clk.Now())
	if err := h.taskRepo.Update(context.Background(), tt); err != nil {
		t.Fatal(err)
	}
	return tt.ID(), e.ID()
}

func TestRequestKill_WorkingTwoPhase(t *testing.T) {
	h := setupKill(t)
	_, execID := seed(t, h, execution.StatusWorking)
	if err := h.coord.RequestKill(context.Background(), execID, execution.KilledUserRequest, "stop now", "user:hayang"); err != nil {
		t.Fatal(err)
	}
	e, _ := h.execRepo.FindByID(context.Background(), execID)
	if e.CancelRequestedAt() == nil {
		t.Fatal("expected cancel_requested_at")
	}
	if e.Status() != execution.StatusWorking {
		t.Fatalf("expected still working: %s", e.Status())
	}
	if len(h.sender.calls) != 1 {
		t.Fatalf("expected sender invoked: %+v", h.sender.calls)
	}
	if err := h.coord.HandleKilled(context.Background(), execID, execution.KilledUserRequest, "done", "worker:W-1"); err != nil {
		t.Fatal(err)
	}
	e, _ = h.execRepo.FindByID(context.Background(), execID)
	if e.Status() != execution.StatusKilled || e.KilledReason() != execution.KilledUserRequest {
		t.Fatalf("wrong: %s/%s", e.Status(), e.KilledReason())
	}
	tt, _ := h.taskRepo.FindByID(context.Background(), "T-1")
	if tt.HasActiveExecution() {
		t.Fatal("expected cleared current_execution")
	}
}

func TestRequestKill_SubmittedDirectKilled(t *testing.T) {
	h := setupKill(t)
	_, execID := seed(t, h, execution.StatusSubmitted)
	if err := h.coord.RequestKill(context.Background(), execID, execution.KilledUserRequest, "stop", "user:hayang"); err != nil {
		t.Fatal(err)
	}
	e, _ := h.execRepo.FindByID(context.Background(), execID)
	if e.Status() != execution.StatusKilled {
		t.Fatalf("expected killed: %s", e.Status())
	}
	if len(h.sender.calls) != 0 {
		t.Fatalf("expected no sender call (direct kill): %+v", h.sender.calls)
	}
}

func TestRequestKill_OnInputRequired_CancelsIR(t *testing.T) {
	h := setupKill(t)
	taskID, execID := seed(t, h, execution.StatusInputRequired)
	// Persist IR-1 pending
	ir, _ := inputrequest.New(inputrequest.NewInput{
		ID: "IR-1", TaskExecutionID: execID, Question: "q?", Now: h.clk.Now(),
	})
	if err := h.irRepo.Save(context.Background(), ir); err != nil {
		t.Fatal(err)
	}
	if err := h.coord.RequestKill(context.Background(), execID, execution.KilledUserRequest, "stop", "user:hayang"); err != nil {
		t.Fatal(err)
	}
	if err := h.coord.HandleKilled(context.Background(), execID, execution.KilledUserRequest, "done", "worker:W-1"); err != nil {
		t.Fatal(err)
	}
	// IR canceled
	got, _ := h.irRepo.FindByID(context.Background(), "IR-1")
	if got.Status() != inputrequest.StatusCanceled {
		t.Fatalf("expected canceled, got %s", got.Status())
	}
	_ = taskID
}

func TestRequestKill_TerminalRejected(t *testing.T) {
	h := setupKill(t)
	_, execID := seed(t, h, execution.StatusWorking)
	_ = h.coord.RequestKill(context.Background(), execID, execution.KilledUserRequest, "stop", "user:hayang")
	_ = h.coord.HandleKilled(context.Background(), execID, execution.KilledUserRequest, "done", "worker:W-1")
	if err := h.coord.RequestKill(context.Background(), execID, execution.KilledUserRequest, "again", "user:hayang"); !errors.Is(err, execution.ErrTaskExecutionAlreadyTerminated) {
		t.Fatalf("expected terminated: %v", err)
	}
	if err := h.coord.HandleKilled(context.Background(), execID, execution.KilledUserRequest, "again", "worker:W-1"); !errors.Is(err, execution.ErrTaskExecutionAlreadyTerminated) {
		t.Fatalf("expected terminated: %v", err)
	}
}

func TestRequestKill_IdempotentInGrace(t *testing.T) {
	h := setupKill(t)
	_, execID := seed(t, h, execution.StatusWorking)
	if err := h.coord.RequestKill(context.Background(), execID, execution.KilledUserRequest, "stop", "user:hayang"); err != nil {
		t.Fatal(err)
	}
	// Second is no-op since cancel_requested_at already set; service still
	// records kill_requested event again (this is fine: idempotency at AR
	// layer means cancel_requested_at not overwritten; event is duplicated
	// audit signal).
	if err := h.coord.RequestKill(context.Background(), execID, execution.KilledUserRequest, "stop", "user:hayang"); err != nil {
		t.Fatal(err)
	}
}

func TestRequestKill_AbandonPrecondition(t *testing.T) {
	h := setupKill(t)
	_, execID := seed(t, h, execution.StatusWorking)
	_ = h.coord.RequestKill(context.Background(), execID, execution.KilledAbandonPrecondition, "abandon", "user:hayang")
	_ = h.coord.HandleKilled(context.Background(), execID, execution.KilledAbandonPrecondition, "done", "worker:W-1")
	tt, _ := h.taskRepo.FindByID(context.Background(), "T-1")
	if tt.Status() != task.StatusAbandoned {
		t.Fatalf("expected abandoned: %s", tt.Status())
	}
}

func TestRequestKill_SuspendPrecondition(t *testing.T) {
	h := setupKill(t)
	_, execID := seed(t, h, execution.StatusWorking)
	_ = h.coord.RequestKill(context.Background(), execID, execution.KilledSuspendPrecondition, "suspend", "user:hayang")
	_ = h.coord.HandleKilled(context.Background(), execID, execution.KilledSuspendPrecondition, "done", "worker:W-1")
	tt, _ := h.taskRepo.FindByID(context.Background(), "T-1")
	if tt.Status() != task.StatusSuspended {
		t.Fatalf("expected suspended: %s", tt.Status())
	}
}

func TestRequestKill_TimeoutKill(t *testing.T) {
	h := setupKill(t)
	_, execID := seed(t, h, execution.StatusWorking)
	_ = h.coord.RequestKill(context.Background(), execID, execution.KilledTimeoutKill, "exceeded", "system")
	_ = h.coord.HandleKilled(context.Background(), execID, execution.KilledTimeoutKill, "done", "worker:W-1")
	e, _ := h.execRepo.FindByID(context.Background(), execID)
	if e.KilledReason() != execution.KilledTimeoutKill {
		t.Fatalf("reason: %s", e.KilledReason())
	}
}

func TestRequestKill_NotFoundAndValidation(t *testing.T) {
	h := setupKill(t)
	if err := h.coord.RequestKill(context.Background(), "E-NONE", execution.KilledUserRequest, "x", "user:hayang"); !errors.Is(err, execution.ErrTaskExecutionNotFound) {
		t.Fatalf("expected not_found: %v", err)
	}
	if err := h.coord.RequestKill(context.Background(), "E", "BAD_REASON", "x", "user:hayang"); !errors.Is(err, execution.ErrUnknownReason) {
		t.Fatal("expected unknown reason")
	}
	if err := h.coord.RequestKill(context.Background(), "E", execution.KilledUserRequest, "", "user:hayang"); err == nil {
		t.Fatal("expected message error")
	}
	if err := h.coord.RequestKill(context.Background(), "E", execution.KilledUserRequest, "x", "BAD"); err == nil {
		t.Fatal("expected actor error")
	}
	if err := h.coord.HandleKilled(context.Background(), "E", "BAD_REASON", "x", "user:hayang"); !errors.Is(err, execution.ErrUnknownReason) {
		t.Fatal("expected unknown")
	}
	if err := h.coord.HandleKilled(context.Background(), "E", execution.KilledUserRequest, "", "user:hayang"); err == nil {
		t.Fatal("expected message")
	}
	if err := h.coord.HandleKilled(context.Background(), "E", execution.KilledUserRequest, "x", "BAD"); err == nil {
		t.Fatal("expected actor")
	}
}

func TestRequestKill_PendingIRMissingIgnored(t *testing.T) {
	h := setupKill(t)
	_, execID := seed(t, h, execution.StatusInputRequired)
	// IR-1 is referenced by exec but never persisted (simulates race
	// where IR was deleted/never created).
	if err := h.coord.RequestKill(context.Background(), execID, execution.KilledUserRequest, "stop", "user:hayang"); err != nil {
		t.Fatal(err)
	}
	if err := h.coord.HandleKilled(context.Background(), execID, execution.KilledUserRequest, "done", "worker:W-1"); err != nil {
		t.Fatal(err)
	}
	e, _ := h.execRepo.FindByID(context.Background(), execID)
	if e.Status() != execution.StatusKilled {
		t.Fatalf("status: %s", e.Status())
	}
}

func TestNewCoordinator_DefaultsApplied(t *testing.T) {
	// nil sender / nil clock should default
	c := NewCoordinator(nil, nil, nil, nil, nil, nil, nil)
	if c.sender == nil {
		t.Fatal("expected default sender")
	}
	if c.clock == nil {
		t.Fatal("expected default clock")
	}
}

func TestRequestKill_SendFailureEmitsEvent(t *testing.T) {
	h := setupKill(t)
	h.sender.wantFail = true
	_, execID := seed(t, h, execution.StatusWorking)
	if err := h.coord.RequestKill(context.Background(), execID, execution.KilledUserRequest, "x", "user:hayang"); err != nil {
		t.Fatal(err)
	}
	// Look for kill_send_failed event
	got, _ := h.eventRepo.Find(context.Background(), observability.EventQueryFilter{Limit: 200})
	found := false
	for _, e := range got {
		if e.Type() == "task_execution.kill_send_failed" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected kill_send_failed event")
	}
}
