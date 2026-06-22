package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
)

// TaskInputConversationPort posts the F6 input_request / input_reply messages
// into a Task's bound Conversation (v2.14.0 I14, issue §13.A/§13.C). It is the
// Conversation-side adapter the TaskInputConversationProjector drives so the pm
// BC stays decoupled from Conversation (ADR-0052) — mirroring PlanDispatcher. The
// production wiring adapts MessageWriter. Both methods run reentrant in the
// caller's tx (AddMessage's RunInTx is reentrant), returning the new message id.
//
//   - PostInputRequest posts an input_request (kind=input_request, sender=agentRef)
//     when a task agent blocks needing a user reply.
//   - PostInputReply posts the user's input_reply (kind=input_reply, sender=actorRef)
//     threaded under the original input_request (inputRequestMessageID; "" ⇒ top-level).
//
// It is OPTIONAL on the projector (nil ⇒ the projector MarkApplies + no-ops, so a
// build without conversation wiring still drains the events cleanly).
type TaskInputConversationPort interface {
	PostInputRequest(ctx context.Context, conversationID, agentRef, reason string) (messageID string, err error)
	PostInputReply(ctx context.Context, conversationID, actorRef, comment, inputRequestMessageID string) (messageID string, err error)
}

// TaskInputConversationProjector is the F6 outbox projector that surfaces a task's
// input_required block (and its reply) into the task's bound Conversation
// (ADR-0052 outbox purity: the pm BlockTask/UnblockTask AppServices write ONLY pm
// state + an outbox event; THIS projector — the sole Conversation writer for these
// events — performs the cross-BC message write). It is a SIBLING of the
// ParticipantProjector / PlanOrchestratorProjector (same contract: Name + Project
// consuming events in ONE tx + IsApplied/MarkApplied idempotency) and must be
// registered on the production relay or input_request/input_reply are silently
// never posted.
//
// It consumes EvtTaskInputRequested → input_request message, and
// EvtTaskInputReplied → input_reply message; every other event type is a no-op.
type TaskInputConversationProjector struct {
	db       *sql.DB
	convRepo conversation.ConversationRepository
	port     TaskInputConversationPort
	applied  outbox.AppliedStore
	clock    clock.Clock
}

// NewTaskInputConversationProjector wires the projector. port is OPTIONAL (nil ⇒
// the projector MarkApplies + no-ops rather than failing — a build without
// Conversation wiring still drains the events).
func NewTaskInputConversationProjector(db *sql.DB, convRepo conversation.ConversationRepository, port TaskInputConversationPort, applied outbox.AppliedStore, clk clock.Clock) *TaskInputConversationProjector {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &TaskInputConversationProjector{db: db, convRepo: convRepo, port: port, applied: applied, clock: clk}
}

// Name is the AppliedStore key (distinct from the sibling projectors).
func (p *TaskInputConversationProjector) Name() string { return "pm-task-input-conversation" }

// Project posts the input_request / input_reply message for one F6 task-input
// event. Irrelevant event types are a no-op. The side effect + MarkApplied run in
// ONE tx so redelivery is a true no-op (IsApplied short-circuit).
func (p *TaskInputConversationProjector) Project(ctx context.Context, e outbox.Event) error {
	switch e.EventType {
	case EvtTaskInputRequested, EvtTaskInputReplied:
		// handled below
	default:
		return nil
	}
	var pl taskInputEventPayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return err
	}
	now := p.clock.Now()
	return persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
		// Same-tx idempotency: if already applied, this event is done.
		if done, err := p.applied.IsApplied(txCtx, p.Name(), e.ID); err != nil {
			return err
		} else if done {
			return nil
		}
		if err := p.post(txCtx, e, pl); err != nil {
			return err
		}
		return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
	})
}

// post resolves the task's bound Conversation (by OwnerRef → NewTaskOwnerRef) and
// posts the input_request / input_reply via the port. No conversation, or no port
// wired, is a no-op (the caller still MarkApplies so the event is not retried
// forever).
func (p *TaskInputConversationProjector) post(txCtx context.Context, e outbox.Event, pl taskInputEventPayload) error {
	if p.port == nil {
		return nil // no Conversation wiring → drain the event without posting
	}
	taskID := taskIDFromOwnerRef(pl.OwnerRef)
	if taskID == "" {
		return nil // malformed owner_ref → nothing to resolve (no-op)
	}
	conv, err := p.convRepo.FindByOwnerRef(txCtx, conversation.NewTaskOwnerRef(taskID))
	if err != nil {
		if errors.Is(err, conversation.ErrConversationNotFound) {
			return nil // no bound conversation yet → no-op (MarkApplied)
		}
		return err
	}
	convID := string(conv.ID())
	switch e.EventType {
	case EvtTaskInputRequested:
		_, perr := p.port.PostInputRequest(txCtx, convID, pl.AgentRef, pl.Reason)
		return perr
	case EvtTaskInputReplied:
		_, perr := p.port.PostInputReply(txCtx, convID, pl.ActorRef, pl.Comment, pl.InputRequestMessageID)
		return perr
	}
	return nil
}

// taskIDFromOwnerRef extracts the task id from a pm://tasks/{id} owner_ref ("" if
// it is not a task owner_ref).
func taskIDFromOwnerRef(ownerRef string) string {
	const prefix = "pm://tasks/"
	if !strings.HasPrefix(ownerRef, prefix) {
		return ""
	}
	return strings.TrimPrefix(ownerRef, prefix)
}

var _ outbox.Projector = (*TaskInputConversationProjector)(nil)
