package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

// ExecutionService handles agent-facing CLI writes that mutate executions
// (report-progress / report-failure).
type ExecutionService struct {
	db       *sql.DB
	execRepo execution.Repository
	taskRepo task.Repository
	convRepo conversation.ConversationRepository
	msgRepo  conversation.MessageRepository
	sink     *observability.EventSink
	idgen    idgen.Generator
	clock    clock.Clock
}

// NewExecutionService constructs the service.
func NewExecutionService(db *sql.DB, execRepo execution.Repository, taskRepo task.Repository, convRepo conversation.ConversationRepository, msgRepo conversation.MessageRepository, sink *observability.EventSink, gen idgen.Generator, clk clock.Clock) *ExecutionService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &ExecutionService{
		db: db, execRepo: execRepo, taskRepo: taskRepo, convRepo: convRepo,
		msgRepo: msgRepo, sink: sink, idgen: gen, clock: clk,
	}
}

// ReportProgressInput is the input for `report-progress` (agent → CLI).
type ReportProgressInput struct {
	ExecutionID taskruntime.TaskExecutionID
	Kind        string
	Content     string
	Actor       observability.Actor
}

// ReportProgress writes Message(agent_finding, no input_request_ref) to
// task.conversation_id. Skips silently when conversation_id == "" (only
// IR triggers fallback per ADR-0017).
func (s *ExecutionService) ReportProgress(ctx context.Context, in ReportProgressInput) error {
	if err := in.Actor.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(string(in.ExecutionID)) == "" {
		return errors.New("report-progress: execution_id required")
	}
	if strings.TrimSpace(in.Content) == "" {
		return errors.New("report-progress: content required")
	}
	now := s.clock.Now()
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		e, err := s.execRepo.FindByID(txCtx, in.ExecutionID)
		if err != nil {
			return err
		}
		t, err := s.taskRepo.FindByID(txCtx, e.TaskID())
		if err != nil {
			return err
		}
		if t.ConversationID() == "" {
			return nil // silent skip per ADR-0017
		}
		msg, err := conversation.NewMessage(conversation.NewMessageInput{
			ID:               conversation.MessageID(s.idgen.NewULID()),
			ConversationID:   conversation.ConversationID(t.ConversationID()),
			SenderIdentityID: conversation.IdentityRef("agent:" + string(in.ExecutionID)),
			ContentKind:      conversation.MessageContentAgentFinding,
			Content:          in.Content,
			Direction:        conversation.DirectionOutbound,
			PostedAt:         now,
		})
		if err != nil {
			return err
		}
		if err := s.msgRepo.Append(txCtx, msg); err != nil {
			return err
		}
		_, err = s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "conversation.message_added",
			Refs: observability.EventRefs{
				ConversationID: t.ConversationID(),
				MessageID:      string(msg.ID()),
				ExecutionID:    string(in.ExecutionID),
			},
			Actor: in.Actor,
			Payload: map[string]any{
				"conversation_id": t.ConversationID(),
				"message_id":      string(msg.ID()),
				"content_kind":    "agent_finding",
				"kind":            in.Kind,
			},
		})
		return err
	})
}

// NotifyWorkingInput flips an execution submitted→working (v2.2 Phase D
// gap #1). The worker daemon calls this from defaultAgentSpawner right
// after PullDispatches accepted an envelope but before — or in parallel
// to — spawning the actual agent subprocess. The center is the authority
// on execution state (conventions § 0.4); the worker only owns the
// subprocess.
type NotifyWorkingInput struct {
	ExecutionID taskruntime.TaskExecutionID
	CWD         string // worker-reported working directory; informational
	BranchName  string // worker-reported branch (worktree mode); informational
	Actor       observability.Actor
}

// NotifyWorking transitions a submitted execution → working and also
// records the ACK (dispatch_state=acked) so the no-ACK timeout scanner
// stops watching it. Idempotent on the working state — repeat calls on
// an already-working execution return nil (the daemon may retry across
// poll cycles after transient transport errors).
func (s *ExecutionService) NotifyWorking(ctx context.Context, in NotifyWorkingInput) error {
	if err := in.Actor.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(string(in.ExecutionID)) == "" {
		return errors.New("notify-working: execution_id required")
	}
	now := s.clock.Now()
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		e, err := s.execRepo.FindByID(txCtx, in.ExecutionID)
		if err != nil {
			return err
		}
		if e.IsTerminal() {
			// Race: kill arrived before notify-working. Don't error — just
			// no-op so the worker can finish its cleanup.
			return nil
		}
		if e.Status() == execution.StatusWorking {
			return nil // idempotent
		}
		// Record ACK first if still pending_ack (AckDispatch bumps version
		// on its own; we need a separate Update so the CAS WHERE clause
		// matches on-disk state). StartWorking then bumps again with its
		// own Update.
		if e.DispatchState() == execution.DispatchPendingAck {
			if err := e.AckDispatch(now); err != nil {
				return err
			}
			if err := s.execRepo.Update(txCtx, e); err != nil {
				return err
			}
		}
		if err := e.StartWorking(in.CWD, now); err != nil {
			return err
		}
		if in.BranchName != "" {
			e.SetBranchName(in.BranchName)
		}
		if err := s.execRepo.Update(txCtx, e); err != nil {
			return err
		}
		_, err = s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "task_execution.working",
			Refs: observability.EventRefs{
				ExecutionID: string(e.ID()),
				TaskID:      string(e.TaskID()),
				WorkerID:    e.WorkerID(),
			},
			Actor: in.Actor,
			Payload: map[string]any{
				"execution_id": string(e.ID()),
				"cwd":          in.CWD,
				"branch":       in.BranchName,
			},
		})
		return err
	})
}

// ConcludeSuccessInput marks an execution → completed(agent_reported_success)
// (v2.2 Phase D gap #1). Driven by the worker daemon when the agent
// subprocess exits with code 0. The companion to NotifyWorking — closes
// the state machine submitted → working → completed.
type ConcludeSuccessInput struct {
	ExecutionID taskruntime.TaskExecutionID
	Message     string
	Actor       observability.Actor
}

// ConcludeSuccess transitions working|input_required → completed and also
// flips the parent Task → done. Idempotent on already-completed
// executions.
func (s *ExecutionService) ConcludeSuccess(ctx context.Context, in ConcludeSuccessInput) error {
	if err := in.Actor.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(string(in.ExecutionID)) == "" {
		return errors.New("conclude: execution_id required")
	}
	msg := strings.TrimSpace(in.Message)
	if msg == "" {
		msg = "agent exited cleanly"
	}
	now := s.clock.Now()
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		e, err := s.execRepo.FindByID(txCtx, in.ExecutionID)
		if err != nil {
			return err
		}
		if e.IsTerminal() {
			return nil // idempotent — repeat conclude is a no-op
		}
		// Permit conclude from submitted as a defensive fallback (the
		// daemon may emit `done` before it manages to NotifyWorking on a
		// very fast agent). Each version-bumping method needs its own
		// Update so the per-row CAS WHERE clause matches on-disk state.
		if e.Status() == execution.StatusSubmitted {
			if e.DispatchState() == execution.DispatchPendingAck {
				if err := e.AckDispatch(now); err != nil {
					return err
				}
				if err := s.execRepo.Update(txCtx, e); err != nil {
					return err
				}
			}
			if err := e.StartWorking("", now); err != nil {
				return err
			}
			if err := s.execRepo.Update(txCtx, e); err != nil {
				return err
			}
		}
		if err := e.MarkCompleted(execution.CompletedAgentReportedSuccess, msg, now); err != nil {
			return err
		}
		if err := s.execRepo.Update(txCtx, e); err != nil {
			return err
		}
		// Flip parent Task → done.
		t, err := s.taskRepo.FindByID(txCtx, e.TaskID())
		if err != nil {
			return err
		}
		if !t.IsTerminal() {
			if string(t.CurrentExecutionID()) != "" {
				t.ClearCurrentExecutionID(now)
				if err := s.taskRepo.Update(txCtx, t); err != nil {
					return err
				}
			}
			if err := t.MarkDone(now); err != nil {
				return err
			}
			if err := s.taskRepo.Update(txCtx, t); err != nil {
				return err
			}
		}
		if _, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "task_execution.completed",
			Refs: observability.EventRefs{
				ExecutionID: string(e.ID()),
				TaskID:      string(e.TaskID()),
				WorkerID:    e.WorkerID(),
			},
			Actor: in.Actor,
			Payload: map[string]any{
				"execution_id": string(e.ID()),
				"reason":       string(execution.CompletedAgentReportedSuccess),
				"message":      msg,
			},
		}); err != nil {
			return err
		}
		_, err = s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "task.done",
			Refs: observability.EventRefs{
				TaskID:    string(t.ID()),
				ProjectID: t.ProjectID(),
			},
			Actor: in.Actor,
			Payload: map[string]any{
				"task_id":      string(t.ID()),
				"execution_id": string(e.ID()),
			},
		})
		return err
	})
}

// ReportFailureInput is the input for `report-failure` (agent → CLI).
type ReportFailureInput struct {
	ExecutionID taskruntime.TaskExecutionID
	Reason      string // human-readable; CLI maps to FailedReason via Validate
	Message     string
	Actor       observability.Actor
}

// ReportFailure transitions execution → failed(agent_reported_failure).
func (s *ExecutionService) ReportFailure(ctx context.Context, in ReportFailureInput) error {
	if err := in.Actor.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(string(in.ExecutionID)) == "" {
		return errors.New("report-failure: execution_id required")
	}
	if strings.TrimSpace(in.Message) == "" {
		return errors.New("report-failure: message required")
	}
	now := s.clock.Now()
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		e, err := s.execRepo.FindByID(txCtx, in.ExecutionID)
		if err != nil {
			return err
		}
		// Use agent_reported_failure reason; embed agent's reason in message.
		msg := in.Message
		if strings.TrimSpace(in.Reason) != "" {
			msg = "[" + in.Reason + "] " + msg
		}
		if err := e.MarkFailed(execution.FailedAgentReported, msg, now); err != nil {
			return err
		}
		if err := s.execRepo.Update(txCtx, e); err != nil {
			return err
		}
		t, err := s.taskRepo.FindByID(txCtx, e.TaskID())
		if err == nil && string(t.CurrentExecutionID()) != "" {
			t.ClearCurrentExecutionID(now)
			if err := s.taskRepo.Update(txCtx, t); err != nil {
				return err
			}
		}
		_, err = s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "task_execution.failed",
			Refs: observability.EventRefs{
				ExecutionID: string(e.ID()),
				TaskID:      string(e.TaskID()),
			},
			Actor: in.Actor,
			Payload: map[string]any{
				"execution_id": string(e.ID()),
				"reason":       string(execution.FailedAgentReported),
				"message":      msg,
			},
		})
		return err
	})
}
