package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

func mkTaskInDB(t *testing.T, db *sql.DB, id taskruntime.TaskID) {
	t.Helper()
	repo := NewTaskRepo(db)
	tt, err := task.New(task.NewInput{
		ID:        id,
		ProjectID: "P-1",
		Title:     "x",
		CreatedBy: "user:hayang",
		Now:       refTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(context.Background(), tt); err != nil {
		t.Fatal(err)
	}
}

func mkExec(t *testing.T, id taskruntime.TaskExecutionID, taskID taskruntime.TaskID) *execution.TaskExecution {
	t.Helper()
	e, err := execution.New(execution.NewInput{
		ID:            id,
		TaskID:        taskID,
		WorkerID:      "W-1",
		AgentCLI:      "claude-code",
		WorkspaceMode: execution.WorkspaceWorktree,
		BaseBranch:    "main",
		Now:           refTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestExecRepo_SaveFindRoundtrip(t *testing.T) {
	db := openTestDB(t)
	mkTaskInDB(t, db, "T-1")
	repo := NewTaskExecutionRepo(db)
	ctx := context.Background()
	e := mkExec(t, "E-1", "T-1")
	if err := repo.Save(ctx, e); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := repo.FindByID(ctx, "E-1")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.Status() != execution.StatusSubmitted || got.DispatchState() != execution.DispatchPendingAck {
		t.Fatalf("wrong: %+v", got)
	}
}

func TestExecRepo_NotFoundAndNilGuards(t *testing.T) {
	db := openTestDB(t)
	repo := NewTaskExecutionRepo(db)
	if _, err := repo.FindByID(context.Background(), "E-NOPE"); !errors.Is(err, execution.ErrTaskExecutionNotFound) {
		t.Fatalf("expected not_found: %v", err)
	}
	if err := repo.Save(context.Background(), nil); err == nil {
		t.Fatal("expected nil error")
	}
	if err := repo.Update(context.Background(), nil); err == nil {
		t.Fatal("expected nil error")
	}
}

func TestExecRepo_Update_HappyAndConflict(t *testing.T) {
	db := openTestDB(t)
	mkTaskInDB(t, db, "T-1")
	repo := NewTaskExecutionRepo(db)
	ctx := context.Background()
	e := mkExec(t, "E-1", "T-1")
	if err := repo.Save(ctx, e); err != nil {
		t.Fatal(err)
	}
	_ = e.AckDispatch(refTime.Add(time.Second))
	if err := repo.Update(ctx, e); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := repo.FindByID(ctx, "E-1")
	if got.DispatchState() != execution.DispatchAcked {
		t.Fatalf("dispatch: %s", got.DispatchState())
	}
	// CAS conflict: load again, mutate twice
	stale, _ := repo.FindByID(ctx, "E-1")
	_ = e.StartWorking("/cwd", refTime.Add(2*time.Second))
	if err := repo.Update(ctx, e); err != nil {
		t.Fatal(err)
	}
	_ = stale.StartWorking("/cwd-stale", refTime.Add(2*time.Second))
	if err := repo.Update(ctx, stale); !errors.Is(err, execution.ErrTaskExecutionVersionConflict) {
		t.Fatalf("expected version conflict: %v", err)
	}
}

func TestExecRepo_Update_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewTaskExecutionRepo(db)
	e := mkExec(t, "E-1", "T-1")
	_ = e.AckDispatch(refTime)
	if err := repo.Update(context.Background(), e); !errors.Is(err, execution.ErrTaskExecutionNotFound) {
		t.Fatalf("expected not_found: %v", err)
	}
}

func TestExecRepo_FindActiveAndByWorker(t *testing.T) {
	db := openTestDB(t)
	mkTaskInDB(t, db, "T-1")
	repo := NewTaskExecutionRepo(db)
	ctx := context.Background()
	e1 := mkExec(t, "E-1", "T-1")
	e2 := mkExec(t, "E-2", "T-1")
	if err := repo.Save(ctx, e1); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, e2); err != nil {
		t.Fatal(err)
	}
	// Terminate e1
	_ = e1.MarkFailed(execution.FailedAgentCrashed, "boom", refTime.Add(time.Second))
	if err := repo.Update(ctx, e1); err != nil {
		t.Fatal(err)
	}
	active, err := repo.FindActive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID() != "E-2" {
		t.Fatalf("active: %+v", active)
	}
	all, err := repo.FindByTaskID(ctx, "T-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("by task: %d", len(all))
	}
	worker, err := repo.FindByWorkerID(ctx, "W-1", execution.StatusSubmitted)
	if err != nil {
		t.Fatal(err)
	}
	if len(worker) != 1 {
		t.Fatalf("worker: %d", len(worker))
	}
	worker, err = repo.FindByWorkerID(ctx, "W-1") // all statuses
	if err != nil {
		t.Fatal(err)
	}
	if len(worker) != 2 {
		t.Fatalf("worker all: %d", len(worker))
	}
}

func TestExecRepo_FindPendingAndSubmitted(t *testing.T) {
	db := openTestDB(t)
	mkTaskInDB(t, db, "T-1")
	repo := NewTaskExecutionRepo(db)
	ctx := context.Background()
	e := mkExec(t, "E-1", "T-1")
	if err := repo.Save(ctx, e); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindPendingAckOlderThan(ctx, refTime.Add(time.Hour).Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("pending_ack: %d", len(got))
	}
	got, err = repo.FindSubmittedOlderThan(ctx, refTime.Add(time.Hour).Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("submitted: %d", len(got))
	}
}

func TestExecRepo_AllFailedReasonsRoundtrip(t *testing.T) {
	db := openTestDB(t)
	mkTaskInDB(t, db, "T-1")
	repo := NewTaskExecutionRepo(db)
	ctx := context.Background()
	reasons := []execution.FailedReason{
		execution.FailedAgentExitNonzero,
		execution.FailedAgentReported,
		execution.FailedAgentCrashed,
		execution.FailedExecutionTimeout,
		execution.FailedShimNoHello,
		execution.DispatchNack(execution.NackWorkerAtCapacity),
	}
	for i, r := range reasons {
		id := taskruntime.TaskExecutionID("E-" + string(rune('A'+i)))
		e := mkExec(t, id, "T-1")
		if err := repo.Save(ctx, e); err != nil {
			t.Fatal(err)
		}
		if err := e.MarkFailed(r, "x", refTime.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		if err := repo.Update(ctx, e); err != nil {
			t.Fatal(err)
		}
		got, _ := repo.FindByID(ctx, id)
		if got.FailedReason() != r {
			t.Fatalf("reason mismatch: %s != %s", got.FailedReason(), r)
		}
	}
}
