package inbound

import (
	"context"
	"errors"
	"fmt"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/conversation/identity"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

// CardCallback is the FeishuBridge interactive-card callback handler
// (plan-7 § 3.4 + bridge/01 § 9 D3). Today it routes only InputRequest
// responses; future card actions plug in via the `action` discriminator.
//
// Idempotency invariant: re-delivery of the same (card_message_id +
// action_value) on an already-terminal InputRequest is a silent ack —
// we record an audit event but do NOT error.
type CardCallback struct {
	clock     clock.Clock
	sink      *observability.EventSink
	irRepo    inputrequest.Repository
	irSvc     *trservice.InputRequestService
	execs     execution.Repository
	tasks     task.Repository
	msgWriter *convservice.MessageWriter
	actor     observability.Actor
}

// CardCallbackDeps wires the callback.
type CardCallbackDeps struct {
	Clock     clock.Clock
	Sink      *observability.EventSink
	IRRepo    inputrequest.Repository
	IRSvc     *trservice.InputRequestService
	Execs     execution.Repository
	Tasks     task.Repository
	MsgWriter *convservice.MessageWriter
	Actor     observability.Actor
}

// NewCardCallback constructs the callback handler with dep validation.
func NewCardCallback(deps CardCallbackDeps) (*CardCallback, error) {
	if deps.Sink == nil {
		return nil, errors.New("card callback: sink required")
	}
	if deps.IRRepo == nil {
		return nil, errors.New("card callback: ir repo required")
	}
	if deps.IRSvc == nil {
		return nil, errors.New("card callback: ir service required")
	}
	if deps.Execs == nil {
		return nil, errors.New("card callback: execution repo required")
	}
	if deps.Tasks == nil {
		return nil, errors.New("card callback: task repo required")
	}
	if deps.MsgWriter == nil {
		return nil, errors.New("card callback: message writer required")
	}
	if err := deps.Actor.Validate(); err != nil {
		return nil, fmt.Errorf("card callback: actor: %w", err)
	}
	if deps.Clock == nil {
		deps.Clock = clock.SystemClock{}
	}
	return &CardCallback{
		clock:     deps.Clock,
		sink:      deps.Sink,
		irRepo:    deps.IRRepo,
		irSvc:     deps.IRSvc,
		execs:     deps.Execs,
		tasks:     deps.Tasks,
		msgWriter: deps.MsgWriter,
		actor:     deps.Actor,
	}, nil
}

// Handle dispatches a normalised CardActionEvent. The signer (identity)
// MUST already have been resolved by the caller (FeishuInboundRouter).
func (c *CardCallback) Handle(ctx context.Context, ev CardActionEvent, identityID identity.IdentityID) (RouteDecision, error) {
	if ev.ActionValue == nil {
		return RouteDecision{}, fmt.Errorf("%w: nil action_value", ErrCardActionMalformed)
	}
	if err := identityID.Validate(); err != nil {
		return RouteDecision{}, fmt.Errorf("card callback: identity_id: %w", err)
	}
	// Audit receipt before dispatch.
	if _, err := c.sink.Emit(ctx, observability.EmitCommand{
		EventType: "bridge.card_action_received",
		Actor:     c.actor,
		Payload: map[string]any{
			"card_message_id":  ev.CardMessageID,
			"action":           ev.Action(),
			"input_request_id": ev.InputRequestID(),
			"identity_id":      string(identityID),
		},
	}); err != nil {
		return RouteDecision{}, fmt.Errorf("card callback: emit received: %w", err)
	}
	switch ev.Action() {
	case "input_request_respond", "input_request_respond_custom":
		return c.handleInputRequestRespond(ctx, ev, identityID)
	case "input_request_cancel":
		return c.handleInputRequestCancel(ctx, ev, identityID)
	default:
		return c.dropUnknownAction(ctx, ev)
	}
}

func (c *CardCallback) handleInputRequestRespond(ctx context.Context, ev CardActionEvent, identityID identity.IdentityID) (RouteDecision, error) {
	irID := ev.InputRequestID()
	if irID == "" {
		c.emitMalformed(ctx, ev, "input_request_id missing in action_value")
		return RouteDecision{
			Kind: RouteDecisionDropUnknown, Reason: "malformed_card_action",
			Message: "input_request_id missing",
		}, nil
	}
	ir, err := c.irRepo.FindByID(ctx, taskruntime.InputRequestID(irID))
	if err != nil {
		if errors.Is(err, inputrequest.ErrInputRequestNotFound) {
			c.emitAlreadyResolved(ctx, ev, "input_request_not_found",
				"input_request not found; likely garbage-collected card")
			return RouteDecision{
				Kind: RouteDecisionDropUnknown, Reason: "input_request_not_found",
				Message: "input_request not found",
			}, nil
		}
		return RouteDecision{}, fmt.Errorf("card callback: ir lookup: %w", err)
	}
	if ir.Status() != inputrequest.StatusPending {
		c.emitAlreadyResolved(ctx, ev, "already_responded",
			fmt.Sprintf("input_request %q is %s; ignoring duplicate card click", irID, ir.Status()))
		return RouteDecision{
			Kind:    RouteDecisionCardCallback,
			Reason:  "already_responded",
			Message: "silent ack — request is already terminal",
		}, nil
	}
	choice := ev.OptionText()
	if choice == "" {
		// fallback when the renderer didn't include option_text
		// (input_request_respond_custom path): we accept the action
		// name as the answer until the user types via /answer.
		choice = ev.Action()
	}
	if err := c.irSvc.Respond(ctx, trservice.RespondInput{
		InputRequestID: ir.ID(),
		Answer:         choice,
		DecidedBy:      string(identityID),
		Actor:          c.actor,
	}); err != nil {
		if errors.Is(err, inputrequest.ErrInputRequestAlreadyResolved) ||
			errors.Is(err, inputrequest.ErrInvalidTransition) {
			c.emitAlreadyResolved(ctx, ev, "race_already_responded", err.Error())
			return RouteDecision{
				Kind: RouteDecisionCardCallback, Reason: "race_already_responded",
				Message: err.Error(),
			}, nil
		}
		return RouteDecision{}, fmt.Errorf("card callback: respond: %w", err)
	}
	// 留痕 Message — link via input_request_ref for outbound update_card.
	if convID, err := c.conversationForExecution(ctx, ir.TaskExecutionID()); err == nil && convID != "" {
		if _, err := c.msgWriter.AddMessage(ctx, convservice.AddMessageCommand{
			ConversationID:   convID,
			SenderIdentityID: conversation.IdentityRef(identityID),
			ContentKind:      conversation.MessageContentText,
			Content:          choice,
			Direction:        conversation.DirectionInbound,
			VendorMsgRef:     ev.CardMessageID + ":" + ev.Action(),
			InputRequestRef:  irID,
			Actor:            c.actor,
		}); err != nil && !errors.Is(err, conversation.ErrMessageDuplicate) {
			return RouteDecision{}, fmt.Errorf("card callback: trace message: %w", err)
		}
		return RouteDecision{
			Kind:           RouteDecisionCardCallback,
			ConversationID: string(convID),
			TargetAction:   "input_request.respond",
		}, nil
	} else if err != nil {
		return RouteDecision{}, err
	}
	return RouteDecision{
		Kind:         RouteDecisionCardCallback,
		TargetAction: "input_request.respond",
	}, nil
}

func (c *CardCallback) handleInputRequestCancel(ctx context.Context, ev CardActionEvent, identityID identity.IdentityID) (RouteDecision, error) {
	irID := ev.InputRequestID()
	if irID == "" {
		c.emitMalformed(ctx, ev, "input_request_id missing in cancel action")
		return RouteDecision{
			Kind: RouteDecisionDropUnknown, Reason: "malformed_card_action",
			Message: "input_request_id missing",
		}, nil
	}
	// v1: treat cancel as "respond with literal answer 'cancel'" so the
	// downstream IR state machine remains uniform. Future v2 may add a
	// dedicated Cancel verb.
	ev.ActionValue["option_text"] = "cancel"
	return c.handleInputRequestRespond(ctx, ev, identityID)
}

func (c *CardCallback) dropUnknownAction(ctx context.Context, ev CardActionEvent) (RouteDecision, error) {
	c.emitMalformed(ctx, ev, fmt.Sprintf("unknown card action %q", ev.Action()))
	return RouteDecision{
		Kind: RouteDecisionDropUnknown, Reason: "unknown_card_action",
		Message: ev.Action(),
	}, nil
}

func (c *CardCallback) emitMalformed(ctx context.Context, ev CardActionEvent, msg string) {
	_, _ = c.sink.Emit(ctx, observability.EmitCommand{
		EventType: "bridge.parse_failed",
		Actor:     c.actor,
		Payload: map[string]any{
			"reason":          "malformed_card_action",
			"message":         msg,
			"vendor_kind":     "card.action.trigger",
			"card_message_id": ev.CardMessageID,
			"action":          ev.Action(),
		},
	})
}

func (c *CardCallback) emitAlreadyResolved(ctx context.Context, ev CardActionEvent, reason, msg string) {
	_, _ = c.sink.Emit(ctx, observability.EmitCommand{
		EventType: "bridge.card_action_received",
		Actor:     c.actor,
		Payload: map[string]any{
			"card_message_id":  ev.CardMessageID,
			"action":           ev.Action(),
			"input_request_id": ev.InputRequestID(),
			"reason":           reason,
			"message":          msg,
		},
	})
}

func (c *CardCallback) conversationForExecution(ctx context.Context, executionID taskruntime.TaskExecutionID) (conversation.ConversationID, error) {
	e, err := c.execs.FindByID(ctx, executionID)
	if err != nil {
		if errors.Is(err, execution.ErrTaskExecutionNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("card callback: execution lookup: %w", err)
	}
	t, err := c.tasks.FindByID(ctx, e.TaskID())
	if err != nil {
		if errors.Is(err, task.ErrTaskNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("card callback: task lookup: %w", err)
	}
	return conversation.ConversationID(t.ConversationID()), nil
}
