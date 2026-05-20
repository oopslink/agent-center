package reconcile

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	trsqlite "github.com/oopslink/agent-center/internal/taskruntime/sqlite"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

func newDB(t *testing.T) (*sql.DB, *clock.FakeClock) {
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
	if _, err := db.ExecContext(ctx, `INSERT INTO workers (id, status, capabilities, working_seconds, enrolled_at, created_at, updated_at) VALUES ('W-2','online','[]',0,'2026-05-21T12:00:00Z','2026-05-21T12:00:00Z','2026-05-21T12:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	return db, clk
}

func seedTaskAndExec(t *testing.T, db *sql.DB, clk *clock.FakeClock, taskID taskruntime.TaskID, execID taskruntime.TaskExecutionID, workerID string, status execution.Status) {
	t.Helper()
	taskRepo := trsqlite.NewTaskRepo(db)
	execRepo := trsqlite.NewTaskExecutionRepo(db)
	tt, err := task.New(task.NewInput{
		ID: taskID, ProjectID: "P-1", Title: "x", CreatedBy: "user:hayang", Now: clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := taskRepo.Save(context.Background(), tt); err != nil {
		t.Fatal(err)
	}
	e, err := execution.New(execution.NewInput{
		ID: execID, TaskID: taskID, WorkerID: workerID, AgentCLI: "claude-code",
		WorkspaceMode: execution.WorkspaceWorktree, Now: clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if status != execution.StatusSubmitted {
		// Transition manually
		_ = e.AckDispatch(clk.Now())
		_ = e.StartWorking("/x", clk.Now())
		if status == execution.StatusFailed {
			_ = e.MarkFailed(execution.FailedAgentCrashed, "boom", clk.Now())
		}
	}
	if err := execRepo.Save(context.Background(), e); err != nil {
		t.Fatal(err)
	}
}

func TestReconcile_ThreeWayClassification(t *testing.T) {
	db, clk := newDB(t)
	execRepo := trsqlite.NewTaskExecutionRepo(db)
	svc := NewService(execRepo)

	// E-1: both worker + center active
	seedTaskAndExec(t, db, clk, "T-1", "E-1", "W-1", execution.StatusWorking)
	// E-2: worker claims active but center says terminal (stale)
	seedTaskAndExec(t, db, clk, "T-2", "E-2", "W-1", execution.StatusFailed)
	// E-3: worker claims unknown (no such execution)
	// E-4: assigned to W-2 but worker W-1 claims it (unknown)
	seedTaskAndExec(t, db, clk, "T-4", "E-4", "W-2", execution.StatusWorking)

	req := Request{
		WorkerID: "W-1",
		LocalActives: []LocalActiveExecution{
			{ExecutionID: "E-1", Status: "working"},
			{ExecutionID: "E-2", Status: "working"},
			{ExecutionID: "E-3", Status: "working"},
			{ExecutionID: "E-4", Status: "working"},
		},
	}
	resp, err := svc.Handle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !containsID(resp.Active, "E-1") {
		t.Fatalf("active should contain E-1: %+v", resp.Active)
	}
	if !containsID(resp.Stale, "E-2") {
		t.Fatalf("stale should contain E-2: %+v", resp.Stale)
	}
	if !containsID(resp.Unknown, "E-3") {
		t.Fatalf("unknown should contain E-3: %+v", resp.Unknown)
	}
	if !containsID(resp.Unknown, "E-4") {
		t.Fatalf("unknown should contain E-4: %+v", resp.Unknown)
	}
}

func TestReconcile_CenterActiveNotInWorker(t *testing.T) {
	db, clk := newDB(t)
	execRepo := trsqlite.NewTaskExecutionRepo(db)
	svc := NewService(execRepo)
	// Center has E-1 active for W-1 but worker doesn't claim
	seedTaskAndExec(t, db, clk, "T-1", "E-1", "W-1", execution.StatusWorking)
	resp, err := svc.Handle(context.Background(), Request{WorkerID: "W-1"})
	if err != nil {
		t.Fatal(err)
	}
	// E-1 ends up in Active (center authoritative)
	if !containsID(resp.Active, "E-1") {
		t.Fatalf("expected E-1 in active: %+v", resp)
	}
}

func TestReconcile_Validation(t *testing.T) {
	db, _ := newDB(t)
	svc := NewService(trsqlite.NewTaskExecutionRepo(db))
	if _, err := svc.Handle(context.Background(), Request{}); err == nil {
		t.Fatal("expected worker_id error")
	}
}

func containsID(xs []taskruntime.TaskExecutionID, v taskruntime.TaskExecutionID) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
