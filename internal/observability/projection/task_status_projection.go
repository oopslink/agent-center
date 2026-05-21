package projection

import (
	"context"
	"errors"
	"fmt"

	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
)

// ErrNotInTransaction is returned by TaskStatusProjectionService helpers
// when the caller did not put a *sql.Tx into ctx — these helpers are
// strict same-tx double writers (plan-4 § 3.2 DoD) and refuse to run
// outside the caller's BC tx (otherwise an in-flight failure on the
// caller side cannot roll back the projection side, violating ADR-0014
// § 2 / conventions § 17).
var ErrNotInTransaction = errors.New("observability/projection: TaskStatusProjection helper requires an active tx in ctx")

// TerminalTaskInfo summarises the fields a caller-BC tx hands the helper
// in order to update the caller's own task / task_execution row(s).
//
// Per plan-4 § 3.2: the helper writes the caller-BC's tables (e.g.
// `task_executions.<terminal flag>`), it does NOT touch the Observability
// projection table. This keeps the BC ownership clear (conventions § 9.z).
type TerminalTaskInfo struct {
	TaskExecutionID taskruntime.TaskExecutionID
	TerminalStatus  string // completed | failed | killed
}

// TaskStatusProjectionService is the same-tx helper TaskRuntime / Discussion
// invoke when an execution reaches a terminal state. It mutates the caller's
// own state tables (passed via the CallerWriter port) inside the caller's
// existing tx; it does NOT touch task_execution_projections.
//
// Per observability/00-overview § 1.4 + plan-4 § 3.2: this is the helper-
// shaped representation of "TaskStatusProjection physical form = task /
// task_execution main tables themselves".
type TaskStatusProjectionService struct {
	writer CallerWriter
}

// CallerWriter is the port the caller BC implements to apply the projection
// fields to its own tables (e.g. `tasks.last_execution_status`). Each BC
// owns its writer; the helper is agnostic.
type CallerWriter interface {
	ApplyTerminal(ctx context.Context, info TerminalTaskInfo) error
}

// NewTaskStatusProjectionService wires the helper. writer must not be nil.
func NewTaskStatusProjectionService(writer CallerWriter) *TaskStatusProjectionService {
	return &TaskStatusProjectionService{writer: writer}
}

// OnExecutionTerminal applies the projection update to the caller BC's
// state tables inside the caller's tx. Returns ErrNotInTransaction if ctx
// lacks a tx.
func (s *TaskStatusProjectionService) OnExecutionTerminal(ctx context.Context, info TerminalTaskInfo) error {
	if s == nil || s.writer == nil {
		return errors.New("task status projection: nil receiver / writer")
	}
	if info.TaskExecutionID == "" {
		return errors.New("task status projection: task_execution_id required")
	}
	if info.TerminalStatus == "" {
		return errors.New("task status projection: terminal_status required")
	}
	if _, ok := persistence.TxFromCtx(ctx); !ok {
		return ErrNotInTransaction
	}
	if err := s.writer.ApplyTerminal(ctx, info); err != nil {
		return fmt.Errorf("task status projection: writer: %w", err)
	}
	return nil
}
