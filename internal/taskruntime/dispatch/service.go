package dispatch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

// DispatchConfig captures dispatch-related config knobs (04-configuration
// § 7.6).
type DispatchConfig struct {
	MaxExecutionsPerTask  int           // default 3
	DispatchAckTimeout    time.Duration // default 30s
}

// DefaultConfig returns DispatchConfig with v1 defaults.
func DefaultConfig() DispatchConfig {
	return DispatchConfig{
		MaxExecutionsPerTask: 3,
		DispatchAckTimeout:   30 * time.Second,
	}
}

// Service is the DispatchService domain service (00-overview § 3.1).
type Service struct {
	db        *sql.DB
	taskRepo  task.Repository
	execRepo  execution.Repository
	sink      *observability.EventSink
	sender    EnvelopeSender
	clock     clock.Clock
	idgen     idgen.Generator
	cfg       DispatchConfig
}

// NewService constructs a DispatchService.
func NewService(
	db *sql.DB,
	taskRepo task.Repository,
	execRepo execution.Repository,
	sink *observability.EventSink,
	sender EnvelopeSender,
	clk clock.Clock,
	gen idgen.Generator,
	cfg DispatchConfig,
) *Service {
	if sender == nil {
		sender = NoopSender{}
	}
	if clk == nil {
		clk = clock.SystemClock{}
	}
	if cfg.MaxExecutionsPerTask == 0 {
		cfg.MaxExecutionsPerTask = 3
	}
	if cfg.DispatchAckTimeout == 0 {
		cfg.DispatchAckTimeout = 30 * time.Second
	}
	return &Service{
		db:       db,
		taskRepo: taskRepo,
		execRepo: execRepo,
		sink:     sink,
		sender:   sender,
		clock:    clk,
		idgen:    gen,
		cfg:      cfg,
	}
}

// DispatchInput is the input for Service.Dispatch.
type DispatchInput struct {
	TaskID                   taskruntime.TaskID
	WorkerID                 string
	AgentCLI                 string
	BaseBranch               string
	ExecutionTimeoutOverride *time.Duration
	ExtraSkillFiles          []string
	Actor                    observability.Actor
}

// DispatchResult is the result of a successful dispatch.
type DispatchResult struct {
	ExecutionID  taskruntime.TaskExecutionID
	Envelope     DispatchEnvelope
}

// Dispatch creates a new TaskExecution, writes events, and asynchronously
// (post-commit) sends the DispatchEnvelope to the worker.
//
// Single-active invariant + max_executions_per_task enforced inside tx.
func (s *Service) Dispatch(ctx context.Context, in DispatchInput) (*DispatchResult, error) {
	if err := in.Actor.Validate(); err != nil {
		return nil, err
	}
	now := s.clock.Now()
	var res DispatchResult
	var (
		limitErr       error
		limitMessage   string
		limitTaskID    string
		limitProjectID string
	)
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		t, err := s.taskRepo.FindByID(txCtx, in.TaskID)
		if err != nil {
			return err
		}
		if t.IsTerminal() {
			return fmt.Errorf("%w: task %s is %s", execution.ErrInvalidTransition, t.ID(), t.Status())
		}
		if t.HasActiveExecution() {
			return execution.ErrSingleActiveViolation
		}
		// max_executions_per_task check
		execs, err := s.execRepo.FindByTaskID(txCtx, t.ID())
		if err != nil {
			return err
		}
		if len(execs) >= s.cfg.MaxExecutionsPerTask {
			limitErr = fmt.Errorf("dispatch: max_executions_per_task=%d reached for task %s", s.cfg.MaxExecutionsPerTask, t.ID())
			limitMessage = fmt.Sprintf("task has %d executions, limit %d", len(execs), s.cfg.MaxExecutionsPerTask)
			limitTaskID = string(t.ID())
			limitProjectID = t.ProjectID()
			return limitErr
		}
		ws := execution.WorkspaceDirect
		if t.RequiresWorktree() {
			ws = execution.WorkspaceWorktree
		}
		agentCLI := in.AgentCLI
		if agentCLI == "" {
			agentCLI = "claude-code"
		}
		e, err := execution.New(execution.NewInput{
			ID:                       taskruntime.TaskExecutionID(s.idgen.NewULID()),
			TaskID:                   t.ID(),
			WorkerID:                 in.WorkerID,
			AgentCLI:                 agentCLI,
			WorkspaceMode:            ws,
			BaseBranch:               in.BaseBranch,
			Priority:                 t.Priority().String(),
			EtaAt:                    t.EtaAt(),
			ExecutionTimeoutOverride: in.ExecutionTimeoutOverride,
			Now:                      now,
		})
		if err != nil {
			return err
		}
		if err := s.execRepo.Save(txCtx, e); err != nil {
			return err
		}
		if err := t.SetCurrentExecutionID(e.ID(), now); err != nil {
			return err
		}
		if err := s.taskRepo.Update(txCtx, t); err != nil {
			return err
		}
		// emit task_execution.submitted
		if _, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "task_execution.submitted",
			Refs: observability.EventRefs{
				TaskID:      string(t.ID()),
				ExecutionID: string(e.ID()),
				WorkerID:    e.WorkerID(),
				ProjectID:   t.ProjectID(),
			},
			Actor: in.Actor,
			Payload: map[string]any{
				"execution_id": string(e.ID()),
				"task_id":      string(t.ID()),
				"worker_id":    e.WorkerID(),
				"agent_cli":    e.AgentCLI(),
				"workspace_mode": string(e.WorkspaceMode()),
			},
		}); err != nil {
			return err
		}
		// emit task_execution.dispatched
		if _, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "task_execution.dispatched",
			Refs: observability.EventRefs{
				TaskID:      string(t.ID()),
				ExecutionID: string(e.ID()),
				WorkerID:    e.WorkerID(),
				ProjectID:   t.ProjectID(),
			},
			Actor: in.Actor,
			Payload: map[string]any{
				"execution_id": string(e.ID()),
				"task_id":      string(t.ID()),
				"worker_id":    e.WorkerID(),
			},
		}); err != nil {
			return err
		}
		// Build envelope
		var timeoutOverrideSecs *int64
		if in.ExecutionTimeoutOverride != nil {
			v := int64(in.ExecutionTimeoutOverride.Seconds())
			timeoutOverrideSecs = &v
		}
		env := DispatchEnvelope{
			EnvelopeVersion:          EnvelopeVersionV1,
			ExecutionID:              e.ID(),
			TaskID:                   t.ID(),
			WorkerID:                 e.WorkerID(),
			ProjectID:                t.ProjectID(),
			ConversationID:           t.ConversationID(),
			AgentCLI:                 e.AgentCLI(),
			WorkspaceMode:            e.WorkspaceMode(),
			BaseBranch:               e.BaseBranch(),
			TaskTitle:                t.Title(),
			TaskDescription:          t.Description(),
			TaskDescriptionBlobRef:   t.DescriptionBlobRef(),
			FromIssueID:              t.FromIssueID(),
			ParentTaskID:             t.ParentTaskID(),
			DependsOnTaskIDs:         t.DependsOnTaskIDs(),
			Priority:                 t.Priority().String(),
			EtaAt:                    t.EtaAt(),
			ExecutionTimeoutOverride: timeoutOverrideSecs,
			ExtraSkillFiles:          append([]string(nil), in.ExtraSkillFiles...),
		}
		res.ExecutionID = e.ID()
		res.Envelope = env
		return nil
	})
	if err != nil {
		if limitErr != nil {
			// Emit dispatch_limit_reached AFTER the failing tx so the audit
			// event is preserved (it's not part of the rolled-back state
			// changes; it's a separate audit signal).
			_, _ = s.sink.Emit(ctx, observability.EmitCommand{
				EventType: "task.dispatch_limit_reached",
				Refs:      observability.EventRefs{TaskID: limitTaskID, ProjectID: limitProjectID},
				Actor:     in.Actor,
				Payload: map[string]any{
					"task_id": limitTaskID,
					"reason":  "dispatch_limit_reached",
					"message": limitMessage,
					"limit":   s.cfg.MaxExecutionsPerTask,
				},
			})
		}
		return nil, err
	}
	// Post-commit: hand envelope to transport (best-effort; failure is
	// captured via 30s no-ack timeout scan).
	if sendErr := s.sender.Send(ctx, res.Envelope); sendErr != nil {
		// Emit observable degrade — but do not roll back commit (already
		// committed). The TimeoutScanner will pick up no-ack 30s later.
		_, _ = s.sink.Emit(ctx, observability.EmitCommand{
			EventType: "task_execution.dispatch_send_failed",
			Refs: observability.EventRefs{
				ExecutionID: string(res.ExecutionID),
			},
			Actor: in.Actor,
			Payload: map[string]any{
				"reason":  "dispatch_send_failed",
				"message": sendErr.Error(),
			},
		})
	}
	return &res, nil
}

// HandleAck records the ACK from a worker on a specific execution.
func (s *Service) HandleAck(ctx context.Context, ack DispatchAck, actor observability.Actor) error {
	if err := ack.Validate(); err != nil {
		return err
	}
	if err := actor.Validate(); err != nil {
		return err
	}
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		e, err := s.execRepo.FindByID(txCtx, ack.ExecutionID)
		if err != nil {
			return err
		}
		if err := e.AckDispatch(s.clock.Now()); err != nil {
			return err
		}
		if err := s.execRepo.Update(txCtx, e); err != nil {
			return err
		}
		_, err = s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "task_execution.acked",
			Refs: observability.EventRefs{
				TaskID:      string(e.TaskID()),
				ExecutionID: string(e.ID()),
				WorkerID:    e.WorkerID(),
			},
			Actor: actor,
			Payload: map[string]any{
				"execution_id": string(e.ID()),
			},
		})
		return err
	})
}

// HandleNack records a NACK and transitions execution → failed.
func (s *Service) HandleNack(ctx context.Context, nack DispatchNack, actor observability.Actor) error {
	if err := nack.Validate(); err != nil {
		return err
	}
	if err := actor.Validate(); err != nil {
		return err
	}
	now := s.clock.Now()
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		e, err := s.execRepo.FindByID(txCtx, nack.ExecutionID)
		if err != nil {
			return err
		}
		failedReason := nack.FailedReason()
		if err := e.MarkFailed(failedReason, nack.Message, now); err != nil {
			return err
		}
		if err := s.execRepo.Update(txCtx, e); err != nil {
			return err
		}
		// Clear task.current_execution_id
		if err := s.clearTaskCurrent(txCtx, e.TaskID(), now); err != nil {
			return err
		}
		// emit nacked event + failed event
		if _, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "task_execution.nacked",
			Refs: observability.EventRefs{
				TaskID:      string(e.TaskID()),
				ExecutionID: string(e.ID()),
				WorkerID:    e.WorkerID(),
			},
			Actor: actor,
			Payload: map[string]any{
				"execution_id": string(e.ID()),
				"reason":       string(nack.Reason),
				"message":      nack.Message,
			},
		}); err != nil {
			return err
		}
		return s.emitFailed(txCtx, e, actor)
	})
}

// ScanPendingAck scans for executions stuck in dispatch_state=pending_ack
// older than the ACK timeout and marks them failed(dispatch_no_ack).
func (s *Service) ScanPendingAck(ctx context.Context, actor observability.Actor) (int, error) {
	if err := actor.Validate(); err != nil {
		return 0, err
	}
	cutoff := s.clock.Now().Add(-s.cfg.DispatchAckTimeout).UTC().Format(time.RFC3339Nano)
	overdues, err := s.execRepo.FindPendingAckOlderThan(ctx, cutoff)
	if err != nil {
		return 0, err
	}
	now := s.clock.Now()
	count := 0
	for _, e := range overdues {
		if e.IsTerminal() {
			continue
		}
		txErr := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
			fresh, err := s.execRepo.FindByID(txCtx, e.ID())
			if err != nil {
				return err
			}
			if fresh.IsTerminal() {
				return nil
			}
			if fresh.DispatchState() != execution.DispatchPendingAck {
				return nil
			}
			if err := fresh.MarkFailed(execution.FailedDispatchNoAck,
				fmt.Sprintf("no ACK received within %s", s.cfg.DispatchAckTimeout), now); err != nil {
				return err
			}
			if err := s.execRepo.Update(txCtx, fresh); err != nil {
				return err
			}
			if err := s.clearTaskCurrent(txCtx, fresh.TaskID(), now); err != nil {
				return err
			}
			return s.emitFailed(txCtx, fresh, actor)
		})
		if txErr != nil {
			return count, txErr
		}
		count++
	}
	return count, nil
}

func (s *Service) clearTaskCurrent(txCtx context.Context, taskID taskruntime.TaskID, now time.Time) error {
	t, err := s.taskRepo.FindByID(txCtx, taskID)
	if err != nil {
		// Per conventions § 9.w + § 17: the schema no longer declares a
		// FOREIGN KEY for task_executions.task_id, but the application-layer
		// invariant is that every TaskExecution we're working with here was
		// just loaded from the same tx — its parent Task must exist. If it
		// doesn't, that's a genuine bug (concurrent delete / data
		// corruption) and we panic rather than silently swallow.
		if errors.Is(err, task.ErrTaskNotFound) {
			panic(fmt.Sprintf("invariant violated: task %s missing in clearTaskCurrent (execution refers to it)", taskID))
		}
		return err
	}
	if string(t.CurrentExecutionID()) == "" {
		return nil
	}
	t.ClearCurrentExecutionID(now)
	return s.taskRepo.Update(txCtx, t)
}

func (s *Service) emitFailed(txCtx context.Context, e *execution.TaskExecution, actor observability.Actor) error {
	_, err := s.sink.Emit(txCtx, observability.EmitCommand{
		EventType: "task_execution.failed",
		Refs: observability.EventRefs{
			TaskID:      string(e.TaskID()),
			ExecutionID: string(e.ID()),
			WorkerID:    e.WorkerID(),
		},
		Actor: actor,
		Payload: map[string]any{
			"execution_id": string(e.ID()),
			"reason":       string(e.FailedReason()),
			"message":      e.FailedMessage(),
		},
	})
	return err
}
