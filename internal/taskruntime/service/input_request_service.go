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
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

// InputRequestService wraps InputRequest CRUD with cross-aggregate writes
// (execution status, conversation message — ADR-0017 + 03-input-request).
type InputRequestService struct {
	db       *sql.DB
	irRepo   inputrequest.Repository
	execRepo execution.Repository
	taskRepo task.Repository
	convRepo conversation.ConversationRepository
	msgRepo  conversation.MessageRepository
	sink     *observability.EventSink
	idgen    idgen.Generator
	clock    clock.Clock
	// DefaultChannel is `notification.default_channel` per ADR-0017 § 10.4.
	DefaultChannel string
}

// NewInputRequestService constructs the service.
func NewInputRequestService(
	db *sql.DB,
	irRepo inputrequest.Repository,
	execRepo execution.Repository,
	taskRepo task.Repository,
	convRepo conversation.ConversationRepository,
	msgRepo conversation.MessageRepository,
	sink *observability.EventSink,
	gen idgen.Generator,
	clk clock.Clock,
	defaultChannel string,
) *InputRequestService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &InputRequestService{
		db: db, irRepo: irRepo, execRepo: execRepo, taskRepo: taskRepo,
		convRepo: convRepo, msgRepo: msgRepo, sink: sink, idgen: gen,
		clock: clk, DefaultChannel: defaultChannel,
	}
}

// CreateInput is the input for agent → CLI request-input.
type CreateInput struct {
	ExecutionID taskruntime.TaskExecutionID
	Question    string
	Options     []string
	Urgency     inputrequest.Urgency
	Actor       observability.Actor
}

// CreateResult bundles the created IR id + the conversation it was
// written to (used by waiting callers).
type CreateResult struct {
	InputRequestID taskruntime.InputRequestID
	ConversationID conversation.ConversationID
}

// Create writes the IR row + transitions execution → input_required +
// writes Message(agent_finding) into task.conversation_id. Falls back to
// the configured default_channel if conversation_id is null per ADR-0017
// § 10.4. Returns ErrNoInputChannel when fallback isn't configured (the
// caller transitions execution → failed(no_input_channel)).
func (s *InputRequestService) Create(ctx context.Context, in CreateInput) (*CreateResult, error) {
	if err := in.Actor.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(in.ExecutionID)) == "" {
		return nil, errors.New("input_request: execution_id required")
	}
	if strings.TrimSpace(in.Question) == "" {
		return nil, errors.New("input_request: question required")
	}
	now := s.clock.Now()
	res := &CreateResult{}
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		e, err := s.execRepo.FindByID(txCtx, in.ExecutionID)
		if err != nil {
			return err
		}
		t, err := s.taskRepo.FindByID(txCtx, e.TaskID())
		if err != nil {
			return err
		}
		convID := conversation.ConversationID(t.ConversationID())
		if convID == "" {
			// Fallback to default_channel
			if strings.TrimSpace(s.DefaultChannel) == "" {
				return errNoInputChannel
			}
			conv, err := conversation.NewConversation(conversation.NewConversationInput{
				ID:                 conversation.ConversationID(s.idgen.NewULID()),
				Kind:               conversation.ConversationKindTask,
				Title:              t.Title(),
				PrimaryChannelHint: s.DefaultChannel,
				OpenedAt:           now,
			})
			if err != nil {
				return err
			}
			if err := s.convRepo.Save(txCtx, conv); err != nil {
				return err
			}
			convID = conv.ID()
			if err := t.BindConversation(string(convID), now); err != nil {
				return err
			}
			if err := s.taskRepo.Update(txCtx, t); err != nil {
				return err
			}
			if _, err := s.sink.Emit(txCtx, observability.EmitCommand{
				EventType: "conversation.opened",
				Refs:      observability.EventRefs{ConversationID: string(convID)},
				Actor:     in.Actor,
				Payload: map[string]any{
					"conversation_id": string(convID),
					"kind":            "task",
					"reason":          "default_channel_fallback",
					"message":         "auto-bind via notification.default_channel",
				},
			}); err != nil {
				return err
			}
		}
		ir, err := inputrequest.New(inputrequest.NewInput{
			ID:              taskruntime.InputRequestID(s.idgen.NewULID()),
			TaskExecutionID: in.ExecutionID,
			Question:        in.Question,
			Options:         in.Options,
			Urgency:         urgencyOrDefault(in.Urgency),
			Now:             now,
		})
		if err != nil {
			return err
		}
		if err := s.irRepo.Save(txCtx, ir); err != nil {
			return err
		}
		if err := e.EnterInputRequired(ir.ID(), now); err != nil {
			return err
		}
		if err := s.execRepo.Update(txCtx, e); err != nil {
			return err
		}
		// Write Message(agent_finding, input_request_ref=*) to conversation
		msg, err := conversation.NewMessage(conversation.NewMessageInput{
			ID:               conversation.MessageID(s.idgen.NewULID()),
			ConversationID:   convID,
			SenderIdentityID: conversation.IdentityRef("agent:" + string(in.ExecutionID)),
			ContentKind:      conversation.MessageContentAgentFinding,
			Content:          in.Question,
			Direction:        conversation.DirectionOutbound,
			InputRequestRef:  string(ir.ID()),
			PostedAt:         now,
		})
		if err != nil {
			return err
		}
		if err := s.msgRepo.Append(txCtx, msg); err != nil {
			return err
		}
		// Emit events
		if _, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "input_request.requested",
			Refs: observability.EventRefs{
				ExecutionID:    string(in.ExecutionID),
				InputRequestID: string(ir.ID()),
				ConversationID: string(convID),
			},
			Actor: in.Actor,
			Payload: map[string]any{
				"input_request_id": string(ir.ID()),
				"execution_id":     string(in.ExecutionID),
				"question":         in.Question,
				"urgency":          string(urgencyOrDefault(in.Urgency)),
			},
		}); err != nil {
			return err
		}
		if _, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "task_execution.input_required",
			Refs: observability.EventRefs{
				ExecutionID: string(in.ExecutionID),
				TaskID:      string(t.ID()),
			},
			Actor: in.Actor,
			Payload: map[string]any{
				"execution_id":     string(in.ExecutionID),
				"input_request_id": string(ir.ID()),
			},
		}); err != nil {
			return err
		}
		if _, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "conversation.message_added",
			Refs: observability.EventRefs{
				ConversationID: string(convID),
				MessageID:      string(msg.ID()),
				InputRequestID: string(ir.ID()),
			},
			Actor: in.Actor,
			Payload: map[string]any{
				"conversation_id":   string(convID),
				"message_id":        string(msg.ID()),
				"content_kind":      "agent_finding",
				"input_request_ref": string(ir.ID()),
			},
		}); err != nil {
			return err
		}
		res.InputRequestID = ir.ID()
		res.ConversationID = convID
		return nil
	})
	if err != nil {
		// no_input_channel: also fail execution.
		if errors.Is(err, errNoInputChannel) {
			s.failExecutionNoInputChannel(ctx, in.ExecutionID, in.Actor)
		}
		return nil, err
	}
	return res, nil
}

// Respond writes the user/supervisor response and transitions IR →
// responded + execution → working.
type RespondInput struct {
	InputRequestID taskruntime.InputRequestID
	Answer         string
	DecidedBy      string
	Actor          observability.Actor
}

// Respond applies a response.
func (s *InputRequestService) Respond(ctx context.Context, in RespondInput) error {
	if err := in.Actor.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(string(in.InputRequestID)) == "" {
		return errors.New("respond: input_request_id required")
	}
	if strings.TrimSpace(in.Answer) == "" {
		return errors.New("respond: answer required")
	}
	if strings.TrimSpace(in.DecidedBy) == "" {
		return errors.New("respond: decided_by required")
	}
	now := s.clock.Now()
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		ir, err := s.irRepo.FindByID(txCtx, in.InputRequestID)
		if err != nil {
			return err
		}
		if err := ir.Respond(inputrequest.InputResponse{
			Answer: in.Answer, DecidedBy: in.DecidedBy, DecidedAt: now,
		}); err != nil {
			return err
		}
		if err := s.irRepo.Update(txCtx, ir); err != nil {
			return err
		}
		e, err := s.execRepo.FindByID(txCtx, ir.TaskExecutionID())
		if err != nil {
			return err
		}
		if e.Status() == execution.StatusInputRequired {
			if err := e.LeaveInputRequired(now); err != nil {
				return err
			}
			if err := s.execRepo.Update(txCtx, e); err != nil {
				return err
			}
		}
		_, err = s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "input_request.responded",
			Refs: observability.EventRefs{
				ExecutionID:    string(ir.TaskExecutionID()),
				InputRequestID: string(ir.ID()),
			},
			Actor: in.Actor,
			Payload: map[string]any{
				"input_request_id": string(ir.ID()),
				"answer":           in.Answer,
				"decided_by":       in.DecidedBy,
			},
		})
		return err
	})
}

func (s *InputRequestService) failExecutionNoInputChannel(ctx context.Context, executionID taskruntime.TaskExecutionID, actor observability.Actor) {
	now := s.clock.Now()
	_ = persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		e, err := s.execRepo.FindByID(txCtx, executionID)
		if err != nil {
			return err
		}
		if e.IsTerminal() {
			return nil
		}
		if err := e.MarkFailed(execution.FailedNoInputChannel,
			"agent requested input but no conversation bound and no default_channel configured", now); err != nil {
			return err
		}
		if err := s.execRepo.Update(txCtx, e); err != nil {
			return err
		}
		_, err = s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "task_execution.failed",
			Refs: observability.EventRefs{
				ExecutionID: string(executionID),
				TaskID:      string(e.TaskID()),
			},
			Actor: actor,
			Payload: map[string]any{
				"execution_id": string(executionID),
				"reason":       string(execution.FailedNoInputChannel),
				"message":      "no input channel available",
			},
		})
		return err
	})
}

// ErrNoInputChannel is exposed for callers (CLI handler) that want to
// detect this specific fallback failure for exit-code mapping.
var ErrNoInputChannel = errNoInputChannel

var errNoInputChannel = errors.New("taskruntime: no_input_channel — conversation_id null and default_channel unconfigured")

func urgencyOrDefault(u inputrequest.Urgency) inputrequest.Urgency {
	if u == "" {
		return inputrequest.UrgencyNormal
	}
	return u
}
