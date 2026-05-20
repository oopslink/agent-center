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
