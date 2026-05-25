// Package kill hosts the KillCoordinator domain service (00-overview §
// 3.5).
package kill

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

// KillSender abstracts the transport that pushes kill_requested events to
// the worker daemon (which then SIGTERMs the agent).
type KillSender interface {
	SendKill(ctx context.Context, executionID taskruntime.TaskExecutionID, reason execution.KilledReason, message string) error
}

// NoopKillSender does nothing.
type NoopKillSender struct{}

// SendKill no-op.
func (NoopKillSender) SendKill(context.Context, taskruntime.TaskExecutionID, execution.KilledReason, string) error {
	return nil
}

// Coordinator is the KillCoordinator domain service.
type Coordinator struct {
	db       *sql.DB
	execRepo execution.Repository
	taskRepo task.Repository
	irRepo   inputrequest.Repository
	sink     *observability.EventSink
	sender   KillSender
	clock    clock.Clock
}

// NewCoordinator constructs a KillCoordinator.
func NewCoordinator(
	db *sql.DB,
	execRepo execution.Repository,
	taskRepo task.Repository,
	irRepo inputrequest.Repository,
	sink *observability.EventSink,
	sender KillSender,
	clk clock.Clock,
) *Coordinator {
	if sender == nil {
		// FIXME(prod-wiring): noop fallback — production callers MUST
		// pass a real KillSender (e.g. dispatchq.KillSender). Reaching
		// this branch silently drops every kill request. See
		// conventions § 0.4 enforce mechanism #2.
		sender = NoopKillSender{}
	}
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &Coordinator{
		db:       db,
		execRepo: execRepo,
		taskRepo: taskRepo,
		irRepo:   irRepo,
		sink:     sink,
		sender:   sender,
		clock:    clk,
	}
}

// RequestKill is stage 1 of the two-phase kill protocol:
//   - writes cancel_requested_at + reason + message
//   - emits task_execution.kill_requested
//   - notifies worker via sender
//
// For executions in submitted state (no agent yet spawned), this also
// performs stage 2 inline (transition → killed) since there's nothing to
// SIGTERM. Per 02-task-execution § 4 边界.
func (c *Coordinator) RequestKill(ctx context.Context, executionID taskruntime.TaskExecutionID, reason execution.KilledReason, message string, actor observability.Actor) error {
	if err := reason.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(message) == "" {
		return errors.New("kill: message required (conventions § 16)")
	}
	if err := actor.Validate(); err != nil {
		return err
	}
	now := c.clock.Now()
	var directKilled bool
	err := persistence.RunInTx(ctx, c.db, func(txCtx context.Context) error {
		e, err := c.execRepo.FindByID(txCtx, executionID)
		if err != nil {
			return err
		}
		if e.IsTerminal() {
			// Idempotent no-op: kill on terminal — refuse explicitly so
			// caller can return ExitInvalidTransition (CLI semantics).
			return execution.ErrTaskExecutionAlreadyTerminated
		}
		// Snapshot pre-call version to detect idempotent no-op.
		preVersion := e.Version()
		if err := e.RequestKill(string(reason), message, now); err != nil {
			return err
		}
		if e.Version() != preVersion {
			if err := c.execRepo.Update(txCtx, e); err != nil {
				return err
			}
		}
		// Emit kill_requested
		if _, err := c.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "task_execution.kill_requested",
			Refs: observability.EventRefs{
				TaskID:      string(e.TaskID()),
				ExecutionID: string(e.ID()),
				WorkerID:    e.WorkerID(),
			},
			Actor: actor,
			Payload: map[string]any{
				"execution_id": string(e.ID()),
				"reason":       string(reason),
				"message":      message,
			},
		}); err != nil {
			return err
		}
		// If submitted, perform stage 2 inline.
		if e.Status() == execution.StatusSubmitted {
			directKilled = true
			return c.markKilledInTx(txCtx, e, reason, message, actor, now)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !directKilled {
		// Notify worker; failure logs but doesn't block (worker may be
		// offline; timeout scanner picks up via worker_lost).
		if sendErr := c.sender.SendKill(ctx, executionID, reason, message); sendErr != nil {
			_, _ = c.sink.Emit(ctx, observability.EmitCommand{
				EventType: "task_execution.kill_send_failed",
				Refs:      observability.EventRefs{ExecutionID: string(executionID)},
				Actor:     actor,
				Payload: map[string]any{
					"reason":  "kill_send_failed",
					"message": sendErr.Error(),
				},
			})
		}
	}
	return nil
}

// HandleKilled is stage 2 of the protocol: worker has confirmed agent is
// dead. Transitions execution → killed + emits task_execution.killed.
// Also cancels any pending InputRequest (input_request.canceled).
func (c *Coordinator) HandleKilled(ctx context.Context, executionID taskruntime.TaskExecutionID, reason execution.KilledReason, message string, actor observability.Actor) error {
	if err := reason.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(message) == "" {
		return errors.New("kill: message required (conventions § 16)")
	}
	if err := actor.Validate(); err != nil {
		return err
	}
	now := c.clock.Now()
	return persistence.RunInTx(ctx, c.db, func(txCtx context.Context) error {
		e, err := c.execRepo.FindByID(txCtx, executionID)
		if err != nil {
			return err
		}
		if e.IsTerminal() {
			return execution.ErrTaskExecutionAlreadyTerminated
		}
		return c.markKilledInTx(txCtx, e, reason, message, actor, now)
	})
}

func (c *Coordinator) markKilledInTx(txCtx context.Context, e *execution.TaskExecution, reason execution.KilledReason, message string, actor observability.Actor, now time.Time) error {
	if err := e.MarkKilled(reason, message, now); err != nil {
		return err
	}
	if err := c.execRepo.Update(txCtx, e); err != nil {
		return err
	}
	// Clear task.current_execution_id if needed.
	//
	// Per conventions § 9.w + § 17: schema declares no FOREIGN KEY, but
	// the application-layer invariant is that an execution's Task exists
	// for the duration of any kill flow (executions only reach this point
	// because they were just loaded from the same tx via FindByID). If
	// the task is missing, that's a real bug — panic rather than silently
	// no-op.
	t, err := c.taskRepo.FindByID(txCtx, e.TaskID())
	if err != nil {
		if errors.Is(err, task.ErrTaskNotFound) {
			panic(fmt.Sprintf("invariant violated: task %s missing in markKilledInTx (execution refers to it)", e.TaskID()))
		}
		return err
	}
	if string(t.CurrentExecutionID()) != "" {
		t.ClearCurrentExecutionID(now)
		if err := c.taskRepo.Update(txCtx, t); err != nil {
			return err
		}
	}
	// Cancel pending IR if any.
	//
	// Per § 9.w + § 17: when execution.pending_input_request_id is set,
	// the IR must exist (it was created in the same domain flow). If
	// it's missing, that's a real bug — panic.
	if string(e.PendingInputRequestID()) != "" {
		ir, irErr := c.irRepo.FindByID(txCtx, e.PendingInputRequestID())
		if irErr != nil {
			if errors.Is(irErr, inputrequest.ErrInputRequestNotFound) {
				panic(fmt.Sprintf("invariant violated: input_request %s missing in markKilledInTx (execution refers to it)", e.PendingInputRequestID()))
			}
			return irErr
		}
		if ir.Status() == inputrequest.StatusPending {
			if err := ir.MarkCanceled("kill_precondition", "execution killed: "+message, now); err != nil {
				return err
			}
			if err := c.irRepo.Update(txCtx, ir); err != nil {
				return err
			}
			if _, err := c.sink.Emit(txCtx, observability.EmitCommand{
				EventType: "input_request.canceled",
				Refs: observability.EventRefs{
					ExecutionID:    string(e.ID()),
					InputRequestID: string(ir.ID()),
				},
				Actor: actor,
				Payload: map[string]any{
					"input_request_id": string(ir.ID()),
					"reason":           "kill_precondition",
					"message":          "execution killed: " + message,
				},
			}); err != nil {
				return err
			}
		}
	}
	// emit killed
	if _, err := c.sink.Emit(txCtx, observability.EmitCommand{
		EventType: "task_execution.killed",
		Refs: observability.EventRefs{
			TaskID:      string(e.TaskID()),
			ExecutionID: string(e.ID()),
			WorkerID:    e.WorkerID(),
		},
		Actor: actor,
		Payload: map[string]any{
			"execution_id": string(e.ID()),
			"reason":       string(reason),
			"message":      message,
		},
	}); err != nil {
		return err
	}
	// abandon/suspend precondition continues here per § 1.7 — Task
	// transitions handled by caller (CLI handler that's wrapping
	// abandon-task / suspend-task knows what to do; KillCoordinator only
	// signals readiness).
	switch reason {
	case execution.KilledAbandonPrecondition:
		if t, err := c.taskRepo.FindByID(txCtx, e.TaskID()); err == nil && !t.IsTerminal() {
			if err := t.Abandon("abandon_after_kill", message, now); err == nil {
				if err := c.taskRepo.Update(txCtx, t); err != nil {
					return err
				}
				if _, err := c.sink.Emit(txCtx, observability.EmitCommand{
					EventType: "task.abandoned",
					Refs:      observability.EventRefs{TaskID: string(t.ID()), ProjectID: t.ProjectID()},
					Actor:     actor,
					Payload: map[string]any{
						"task_id": string(t.ID()),
						"reason":  "abandon_after_kill",
						"message": message,
					},
				}); err != nil {
					return err
				}
			}
		}
	case execution.KilledSuspendPrecondition:
		if t, err := c.taskRepo.FindByID(txCtx, e.TaskID()); err == nil && t.Status() == task.StatusOpen {
			if err := t.Suspend(now); err == nil {
				if err := c.taskRepo.Update(txCtx, t); err != nil {
					return err
				}
				if _, err := c.sink.Emit(txCtx, observability.EmitCommand{
					EventType: "task.suspended",
					Refs:      observability.EventRefs{TaskID: string(t.ID()), ProjectID: t.ProjectID()},
					Actor:     actor,
					Payload: map[string]any{
						"task_id": string(t.ID()),
					},
				}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

