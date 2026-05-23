package timeoutscan

import (
	"context"
	"database/sql"
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
	"github.com/oopslink/agent-center/internal/taskruntime/kill"
	trsqlite "github.com/oopslink/agent-center/internal/taskruntime/sqlite"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

type harness struct {
	db       *sql.DB
	clk      *clock.FakeClock
	idgen    idgen.Generator
	sink     *observability.EventSink
	eventRepo *obsqlite.EventRepo
	taskRepo *trsqlite.TaskRepo
	execRepo *trsqlite.TaskExecutionRepo
	irRepo   *trsqlite.InputRequestRepo
	killCoord *kill.Coordinator
	scanner  *Scanner
}

func setupHarness(t *testing.T) *harness {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	_, _ = db.ExecContext(ctx, `INSERT INTO projects (id, name, created_at, updated_at, created_by_identity_id) VALUES ('P-1','P','2026-05-21T12:00:00Z','2026-05-21T12:00:00Z','user:hayang')`)
	_, _ = db.ExecContext(ctx, `INSERT INTO workers (id, status, capabilities_json, working_seconds, enrolled_at, created_at, updated_at) VALUES ('W-1','online','[]',0,'2026-05-21T12:00:00Z','2026-05-21T12:00:00Z','2026-05-21T12:00:00Z')`)
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
	killCoord := kill.NewCoordinator(db, execRepo, taskRepo, irRepo, sink, nil, clk)
	cfg := DefaultConfig()
	scanner := NewScanner(db, execRepo, taskRepo, irRepo, sink, killCoord, clk, cfg)
	return &harness{
		db: db, clk: clk, idgen: gen, sink: sink, eventRepo: er,
		taskRepo: taskRepo, execRepo: execRepo, irRepo: irRepo,
		killCoord: killCoord, scanner: scanner,
	}
}

func seedTaskAndExec(t *testing.T, h *harness, taskID taskruntime.TaskID, execID taskruntime.TaskExecutionID, status execution.Status, transitionTime time.Time) {
	t.Helper()
	tt, _ := task.New(task.NewInput{ID: taskID, ProjectID: "P-1", Title: "x", CreatedBy: "user:hayang", Now: h.clk.Now()})
	if err := h.taskRepo.Save(context.Background(), tt); err != nil {
		t.Fatal(err)
	}
	e, _ := execution.New(execution.NewInput{
		ID: execID, TaskID: taskID, WorkerID: "W-1", AgentCLI: "claude-code",
		WorkspaceMode: execution.WorkspaceWorktree, Now: h.clk.Now(),
	})
	if status == execution.StatusWorking {
		_ = e.AckDispatch(h.clk.Now())
		_ = e.StartWorking("/cwd", transitionTime)
	}
	if err := h.execRepo.Save(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	_ = tt.SetCurrentExecutionID(execID, h.clk.Now())
	if err := h.taskRepo.Update(context.Background(), tt); err != nil {
		t.Fatal(err)
	}
}

func TestTick_SubmittedTimeout(t *testing.T) {
	h := setupHarness(t)
	seedTaskAndExec(t, h, "T-1", "E-1", execution.StatusSubmitted, h.clk.Now())
	h.clk.Advance(6 * time.Minute)
	if err := h.scanner.Tick(context.Background(), "system"); err != nil {
		t.Fatal(err)
	}
	e, _ := h.execRepo.FindByID(context.Background(), "E-1")
	if e.Status() != execution.StatusFailed || e.FailedReason() != execution.FailedSubmittedTimeout {
		t.Fatalf("wrong: %s/%s", e.Status(), e.FailedReason())
	}
	tt, _ := h.taskRepo.FindByID(context.Background(), "T-1")
	if tt.HasActiveExecution() {
		t.Fatal("expected cleared current_execution")
	}
}

func TestTick_ExecutionTimeoutTriggersKill(t *testing.T) {
	h := setupHarness(t)
	seedTaskAndExec(t, h, "T-1", "E-1", execution.StatusWorking, h.clk.Now())
	h.clk.Advance(7 * time.Hour)
	if err := h.scanner.Tick(context.Background(), "system"); err != nil {
		t.Fatal(err)
	}
	e, _ := h.execRepo.FindByID(context.Background(), "E-1")
	if e.CancelRequestedAt() == nil {
		t.Fatal("expected cancel_requested_at set by kill coordinator")
	}
	// Now simulate worker confirm killed
	if err := h.killCoord.HandleKilled(context.Background(), "E-1", execution.KilledTimeoutKill, "exceeded", "worker:W-1"); err != nil {
		t.Fatal(err)
	}
	e, _ = h.execRepo.FindByID(context.Background(), "E-1")
	if e.Status() != execution.StatusKilled || e.KilledReason() != execution.KilledTimeoutKill {
		t.Fatalf("wrong: %s/%s", e.Status(), e.KilledReason())
	}
}

func TestTick_InputRequestT2(t *testing.T) {
	h := setupHarness(t)
	seedTaskAndExec(t, h, "T-1", "E-1", execution.StatusWorking, h.clk.Now())
	// Transition to input_required
	e, _ := h.execRepo.FindByID(context.Background(), "E-1")
	_ = e.EnterInputRequired("IR-1", h.clk.Now())
	if err := h.execRepo.Update(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	ir, _ := inputrequest.New(inputrequest.NewInput{
		ID: "IR-1", TaskExecutionID: "E-1", Question: "q?", Now: h.clk.Now(),
	})
	if err := h.irRepo.Save(context.Background(), ir); err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(25 * time.Hour)
	if err := h.scanner.Tick(context.Background(), "system"); err != nil {
		t.Fatal(err)
	}
	got, _ := h.irRepo.FindByID(context.Background(), "IR-1")
	if got.Status() != inputrequest.StatusTimedOut {
		t.Fatalf("expected timed_out: %s", got.Status())
	}
	e, _ = h.execRepo.FindByID(context.Background(), "E-1")
	if e.Status() != execution.StatusFailed || e.FailedReason() != execution.FailedInputTimeout {
		t.Fatalf("wrong: %s/%s", e.Status(), e.FailedReason())
	}
}

func TestTick_InputRequestT1Ping(t *testing.T) {
	h := setupHarness(t)
	seedTaskAndExec(t, h, "T-1", "E-1", execution.StatusWorking, h.clk.Now())
	e, _ := h.execRepo.FindByID(context.Background(), "E-1")
	_ = e.EnterInputRequired("IR-1", h.clk.Now())
	if err := h.execRepo.Update(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	ir, _ := inputrequest.New(inputrequest.NewInput{
		ID: "IR-1", TaskExecutionID: "E-1", Question: "q?", Now: h.clk.Now(),
	})
	if err := h.irRepo.Save(context.Background(), ir); err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(5 * time.Hour)
	if err := h.scanner.Tick(context.Background(), "system"); err != nil {
		t.Fatal(err)
	}
	// Expect ping_t1 event
	events, _ := h.eventRepo.Find(context.Background(), observability.EventQueryFilter{Limit: 200})
	found := false
	for _, ev := range events {
		if ev.Type() == "input_request.ping_t1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected ping_t1 event")
	}
}

func TestHandleWorkerOffline_FailsActives(t *testing.T) {
	h := setupHarness(t)
	seedTaskAndExec(t, h, "T-1", "E-1", execution.StatusWorking, h.clk.Now())
	if err := h.scanner.HandleWorkerOffline(context.Background(), "W-1", "system"); err != nil {
		t.Fatal(err)
	}
	e, _ := h.execRepo.FindByID(context.Background(), "E-1")
	if e.Status() != execution.StatusFailed || e.FailedReason() != execution.FailedWorkerLost {
		t.Fatalf("wrong: %s/%s", e.Status(), e.FailedReason())
	}
	tt, _ := h.taskRepo.FindByID(context.Background(), "T-1")
	if tt.HasActiveExecution() {
		t.Fatal("expected current_execution cleared")
	}
}

func TestActorValidation(t *testing.T) {
	h := setupHarness(t)
	if err := h.scanner.Tick(context.Background(), "BAD"); err == nil {
		t.Fatal("expected actor")
	}
	if err := h.scanner.HandleWorkerOffline(context.Background(), "W-1", "BAD"); err == nil {
		t.Fatal("expected actor")
	}
}

func TestTick_SkipsTerminalExecutions(t *testing.T) {
	h := setupHarness(t)
	seedTaskAndExec(t, h, "T-1", "E-1", execution.StatusSubmitted, h.clk.Now())
	// Mark terminal before scan
	e, _ := h.execRepo.FindByID(context.Background(), "E-1")
	_ = e.MarkFailed(execution.FailedAgentCrashed, "boom", h.clk.Now())
	if err := h.execRepo.Update(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(10 * time.Minute)
	if err := h.scanner.Tick(context.Background(), "system"); err != nil {
		t.Fatal(err)
	}
	// Should remain failed (not re-flipped)
	e2, _ := h.execRepo.FindByID(context.Background(), "E-1")
	if e2.Status() != execution.StatusFailed {
		t.Fatalf("status: %s", e2.Status())
	}
}

func TestHandleWorkerOffline_TerminalSkipped(t *testing.T) {
	h := setupHarness(t)
	seedTaskAndExec(t, h, "T-1", "E-1", execution.StatusWorking, h.clk.Now())
	// Mark terminal so HandleWorkerOffline shouldn't double-fail
	e, _ := h.execRepo.FindByID(context.Background(), "E-1")
	_ = e.MarkFailed(execution.FailedAgentCrashed, "boom", h.clk.Now())
	if err := h.execRepo.Update(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if err := h.scanner.HandleWorkerOffline(context.Background(), "W-1", "system"); err != nil {
		t.Fatal(err)
	}
}

func TestTick_ExecutionTimeoutSkipsNonWorking(t *testing.T) {
	h := setupHarness(t)
	seedTaskAndExec(t, h, "T-1", "E-1", execution.StatusSubmitted, h.clk.Now())
	// Submitted but past 6h won't trigger execution_timeout (only working
	// state matters)
	h.clk.Advance(7 * time.Hour)
	if err := h.scanner.Tick(context.Background(), "system"); err != nil {
		t.Fatal(err)
	}
	// Since submitted_timeout (5min) also triggers, exec → failed
	e, _ := h.execRepo.FindByID(context.Background(), "E-1")
	if e.Status() != execution.StatusFailed || e.FailedReason() != execution.FailedSubmittedTimeout {
		t.Fatalf("expected submitted_timeout, got %s/%s", e.Status(), e.FailedReason())
	}
}

func TestRun_ContextCancel(t *testing.T) {
	h := setupHarness(t)
	h.scanner.cfg.TickInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- h.scanner.Run(ctx, "system")
	}()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected context error")
		}
	case <-time.After(time.Second):
		t.Fatal("scanner.Run did not return after cancel")
	}
}
