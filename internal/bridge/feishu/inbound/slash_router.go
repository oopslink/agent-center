package inbound

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/conversation/identity"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

// SlashRouteContext carries the per-event context the SlashCommandRouter
// needs that's not in the SlashCommand itself.
type SlashRouteContext struct {
	// IdentityID is the resolved center identity for VendorUserID.
	IdentityID identity.IdentityID
	// VendorThreadKey is the inbound thread / chat id (used by
	// /track to find-or-create the current conversation).
	VendorThreadKey string
	// MessageContext narrows DM / group_adhoc / group_thread for the
	// /track find-or-create branch.
	MessageContext MessageContext
	// VendorMsgRef is forwarded into the trace Message (留痕) for the
	// rare case a duplicate slash arrives and we want to spot it via
	// vendor_msg_ref.
	VendorMsgRef string
}

// SlashRouter is the FeishuBridge slash command dispatcher
// (plan-7 § 3.3 + bridge/01 § 9.1). Its sole job is to translate a
// parsed SlashCommand + context into the appropriate Application
// Service call and write a 留痕 Message into the Conversation so the
// audit trail is complete.
//
// Reject paths (task / input_request not found, /dispatch, malformed)
// MUST NOT pollute Conversation Message timelines. They only surface
// via:
//
//   - emitted `bridge.slash_command_rejected` event (with reason +
//     message per conventions § 17)
//   - ephemeral reply via the SlashEphemeralReplier port (optional;
//     a no-op in tests / when the Bridge has no live vendor link)
type SlashRouter struct {
	db        *sql.DB
	clock     clock.Clock
	idgen     idgen.Generator
	sink      *observability.EventSink

	tasks     task.Repository
	execs     execution.Repository
	convs     conversation.ConversationRepository
	taskSvc   *trservice.TaskService
	irSvc     *trservice.InputRequestService
	irRepo    inputrequest.Repository
	msgWriter *convservice.MessageWriter

	ephemeral SlashEphemeralReplier
	actor     observability.Actor
}

// SlashEphemeralReplier is the optional outbound-side port the slash
// router uses to surface usage / reject feedback to the vendor user.
// The Bridge supplies a real implementation backed by the feishu client;
// tests inject a fake.
type SlashEphemeralReplier interface {
	// ReplyEphemeral sends a one-off reply visible only to the user
	// who issued the slash. Returns an error to keep "fire and forget"
	// off the table — callers decide whether to bubble or absorb (the
	// SlashRouter absorbs and emits `bridge.slash_command_rejected`
	// regardless of the reply's outcome).
	ReplyEphemeral(ctx context.Context, vendorThreadKey, vendorUserID, message string) error
}

// SlashRouterDeps wires the router.
type SlashRouterDeps struct {
	DB        *sql.DB
	Clock     clock.Clock
	IDGen     idgen.Generator
	Sink      *observability.EventSink
	Tasks     task.Repository
	Execs     execution.Repository
	Convs     conversation.ConversationRepository
	TaskSvc   *trservice.TaskService
	IRSvc     *trservice.InputRequestService
	IRRepo    inputrequest.Repository
	MsgWriter *convservice.MessageWriter
	Ephemeral SlashEphemeralReplier // optional
	Actor     observability.Actor
}

// NewSlashRouter constructs the router and validates deps.
func NewSlashRouter(deps SlashRouterDeps) (*SlashRouter, error) {
	if deps.DB == nil {
		return nil, errors.New("slash router: db required")
	}
	if deps.Sink == nil {
		return nil, errors.New("slash router: sink required")
	}
	if deps.IDGen == nil {
		return nil, errors.New("slash router: idgen required")
	}
	if deps.Tasks == nil {
		return nil, errors.New("slash router: task repo required")
	}
	if deps.Execs == nil {
		return nil, errors.New("slash router: execution repo required")
	}
	if deps.Convs == nil {
		return nil, errors.New("slash router: conversation repo required")
	}
	if deps.TaskSvc == nil {
		return nil, errors.New("slash router: task service required")
	}
	if deps.IRSvc == nil {
		return nil, errors.New("slash router: input request service required")
	}
	if deps.IRRepo == nil {
		return nil, errors.New("slash router: input request repo required")
	}
	if deps.MsgWriter == nil {
		return nil, errors.New("slash router: message writer required")
	}
	if err := deps.Actor.Validate(); err != nil {
		return nil, fmt.Errorf("slash router: actor: %w", err)
	}
	if deps.Clock == nil {
		deps.Clock = clock.SystemClock{}
	}
	return &SlashRouter{
		db:        deps.DB,
		clock:     deps.Clock,
		idgen:     deps.IDGen,
		sink:      deps.Sink,
		tasks:     deps.Tasks,
		execs:     deps.Execs,
		convs:     deps.Convs,
		taskSvc:   deps.TaskSvc,
		irSvc:     deps.IRSvc,
		irRepo:    deps.IRRepo,
		msgWriter: deps.MsgWriter,
		ephemeral: deps.Ephemeral,
		actor:     deps.Actor,
	}, nil
}

// Route dispatches a parsed SlashCommand. Returns a RouteDecision summary
// (used by the caller for audit + tests). Errors returned represent
// infrastructure failures (DB unreachable etc) — domain rejections are
// surfaced via RouteDecision{Kind=RouteDecisionRejectSlash, ...} and
// a non-nil error (sentinel-typed via errors.Is friendly).
func (r *SlashRouter) Route(ctx context.Context, cmd *SlashCommand, rctx SlashRouteContext) (RouteDecision, error) {
	if cmd == nil {
		return RouteDecision{}, errors.New("slash router: nil command")
	}
	if err := rctx.IdentityID.Validate(); err != nil {
		return RouteDecision{}, fmt.Errorf("slash router: identity_id: %w", err)
	}
	// Audit the command receipt regardless of outcome.
	if _, err := r.sink.Emit(ctx, observability.EmitCommand{
		EventType: "bridge.slash_command_received",
		Actor:     r.actor,
		Payload: map[string]any{
			"verb":           string(cmd.Verb),
			"args":           strings.Join(cmd.Args, " "),
			"raw":            cmd.Raw,
			"vendor_user_id": rctx.VendorThreadKey,
			"identity_id":    string(rctx.IdentityID),
		},
	}); err != nil {
		return RouteDecision{}, fmt.Errorf("slash router: emit received: %w", err)
	}
	switch cmd.Verb {
	case SlashVerbTrack:
		return r.routeTrack(ctx, cmd, rctx)
	case SlashVerbAnswer:
		return r.routeAnswer(ctx, cmd, rctx)
	case SlashVerbDispatch:
		return r.rejectDeferred(ctx, cmd, rctx,
			"/dispatch is reserved for v2; please @bot to escalate"), nil
	default:
		return r.rejectUsage(ctx, cmd, rctx, ErrSlashUnknownVerb,
			"unknown command; supported: /track /answer /dispatch"), nil
	}
}

func (r *SlashRouter) routeTrack(ctx context.Context, cmd *SlashCommand, rctx SlashRouteContext) (RouteDecision, error) {
	taskID := cmd.TaskID()
	if taskID == "" {
		return r.rejectUsage(ctx, cmd, rctx, ErrSlashInsufficientArgs,
			"usage: /track <task_id>"), nil
	}
	tID := taskruntime.TaskID(taskID)
	t, err := r.tasks.FindByID(ctx, tID)
	if err != nil {
		if errors.Is(err, task.ErrTaskNotFound) {
			return r.rejectNotFound(ctx, cmd, rctx, "task_not_found",
				fmt.Sprintf("task %q not found", taskID)), nil
		}
		return RouteDecision{}, fmt.Errorf("slash router: task lookup: %w", err)
	}
	// Resolve current thread → conversation. If absent, create a
	// conversation matching the message_context.
	convID, err := r.findOrCreateThreadConversation(ctx, rctx)
	if err != nil {
		return RouteDecision{}, fmt.Errorf("slash router: thread conversation: %w", err)
	}
	// Bind task → conversation. If already bound to THIS conv, this is a
	// no-op at the domain layer; if bound elsewhere, task BC returns
	// ErrCannotUnbindConversation.
	if existing := t.ConversationID(); existing != "" && existing != string(convID) {
		return r.rejectInvariant(ctx, cmd, rctx, "task_already_bound",
			fmt.Sprintf("task %q is already bound to conversation %q", taskID, existing)), nil
	}
	if t.ConversationID() == "" {
		if _, err := r.taskSvc.BindConversation(ctx, trservice.BindConversationInput{
			TaskID:         tID,
			Mode:           "to",
			ExistingConvID: convID,
			ChannelHint:    "feishu",
			Actor:          r.actor,
		}); err != nil {
			if errors.Is(err, task.ErrCannotUnbindConversation) || errors.Is(err, conversation.ErrConversationClosed) {
				return r.rejectInvariant(ctx, cmd, rctx, "bind_rejected", err.Error()), nil
			}
			return RouteDecision{}, fmt.Errorf("slash router: bind: %w", err)
		}
	}
	// Write 留痕 Message (single tx with the AddMessage event emit).
	if _, err := r.msgWriter.AddMessage(ctx, convservice.AddMessageCommand{
		ConversationID:   convID,
		SenderIdentityID: conversation.IdentityRef(rctx.IdentityID),
		ContentKind:      conversation.MessageContentText,
		Content:          cmd.Raw,
		Direction:        conversation.DirectionInbound,
		VendorMsgRef:     rctx.VendorMsgRef,
		Actor:            r.actor,
	}); err != nil {
		// Duplicate ref (re-delivered slash) → treat as success; dedupe
		// upstream should catch it before we get here.
		if errors.Is(err, conversation.ErrMessageDuplicate) {
			return RouteDecision{
				Kind:           RouteDecisionSlashRoute,
				ConversationID: string(convID),
				TargetAction:   "task.bind_conversation",
				Reason:         "duplicate_trace_message",
				Message:        "leave the original trace message; new slash returned a dup vendor_msg_ref",
			}, nil
		}
		return RouteDecision{}, fmt.Errorf("slash router: trace message: %w", err)
	}
	return RouteDecision{
		Kind:           RouteDecisionSlashRoute,
		ConversationID: string(convID),
		TargetAction:   "task.bind_conversation",
	}, nil
}

func (r *SlashRouter) routeAnswer(ctx context.Context, cmd *SlashCommand, rctx SlashRouteContext) (RouteDecision, error) {
	irID := cmd.InputRequestID()
	choice := cmd.AnswerChoice()
	if irID == "" || choice == "" {
		return r.rejectUsage(ctx, cmd, rctx, ErrSlashInsufficientArgs,
			"usage: /answer <input_request_id> <choice>"), nil
	}
	ir, err := r.irRepo.FindByID(ctx, taskruntime.InputRequestID(irID))
	if err != nil {
		if errors.Is(err, inputrequest.ErrInputRequestNotFound) {
			return r.rejectNotFound(ctx, cmd, rctx, "input_request_not_found",
				fmt.Sprintf("input_request %q not found", irID)), nil
		}
		return RouteDecision{}, fmt.Errorf("slash router: ir lookup: %w", err)
	}
	if ir.Status() != inputrequest.StatusPending {
		return r.rejectInvariant(ctx, cmd, rctx,
			"input_request_already_resolved",
			fmt.Sprintf("input_request %q is %s; cannot accept new response",
				irID, ir.Status())), nil
	}
	// Resolve target conversation: task.conversation_id (set by IRSvc
	// when the request was created — see input_request_service.Create).
	// We rely on the IR having a task -> conversation reference; fall
	// back to the conv referenced via the task lookup.
	convID, err := r.conversationForExecution(ctx, ir.TaskExecutionID())
	if err != nil {
		return RouteDecision{}, err
	}
	// Drive the InputRequest application service (Respond). This same-
	// tx writes IR status + exec status + emit input_request.responded.
	if err := r.irSvc.Respond(ctx, trservice.RespondInput{
		InputRequestID: ir.ID(),
		Answer:         choice,
		DecidedBy:      string(rctx.IdentityID),
		Actor:          r.actor,
	}); err != nil {
		if errors.Is(err, inputrequest.ErrInputRequestAlreadyResolved) ||
			errors.Is(err, inputrequest.ErrInvalidTransition) {
			return r.rejectInvariant(ctx, cmd, rctx,
				"input_request_already_resolved", err.Error()), nil
		}
		return RouteDecision{}, fmt.Errorf("slash router: respond: %w", err)
	}
	// 留痕 Message into the same conversation. Same direction + sender
	// rules as /track. Use input_request_ref so the dispatcher can update
	// the right card.
	if convID != "" {
		if _, err := r.msgWriter.AddMessage(ctx, convservice.AddMessageCommand{
			ConversationID:   convID,
			SenderIdentityID: conversation.IdentityRef(rctx.IdentityID),
			ContentKind:      conversation.MessageContentText,
			Content:          cmd.Raw,
			Direction:        conversation.DirectionInbound,
			VendorMsgRef:     rctx.VendorMsgRef,
			InputRequestRef:  irID,
			Actor:            r.actor,
		}); err != nil && !errors.Is(err, conversation.ErrMessageDuplicate) {
			return RouteDecision{}, fmt.Errorf("slash router: trace message: %w", err)
		}
	}
	return RouteDecision{
		Kind:           RouteDecisionSlashRoute,
		ConversationID: string(convID),
		TargetAction:   "input_request.respond",
	}, nil
}

// conversationForExecution resolves execution_id → task → conversation_id.
// Returns "" + nil when no conversation is bound (the IR still completes
// via Respond — only the 留痕 trail is skipped).
func (r *SlashRouter) conversationForExecution(ctx context.Context, executionID taskruntime.TaskExecutionID) (conversation.ConversationID, error) {
	e, err := r.execs.FindByID(ctx, executionID)
	if err != nil {
		if errors.Is(err, execution.ErrTaskExecutionNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("slash router: execution lookup: %w", err)
	}
	t, err := r.tasks.FindByID(ctx, e.TaskID())
	if err != nil {
		if errors.Is(err, task.ErrTaskNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("slash router: task lookup: %w", err)
	}
	return conversation.ConversationID(t.ConversationID()), nil
}

// findOrCreateThreadConversation resolves (channel=feishu, thread_key) →
// Conversation, creating a kind=dm / group_thread / adhoc conversation
// when missing. The newly-created conversation lands in a single tx with
// its conversation.opened event.
func (r *SlashRouter) findOrCreateThreadConversation(ctx context.Context, rctx SlashRouteContext) (conversation.ConversationID, error) {
	if rctx.VendorThreadKey != "" {
		if existing, err := r.convs.FindByChannelAndThreadKey(ctx, "feishu", rctx.VendorThreadKey); err == nil {
			return existing.ID(), nil
		} else if !errors.Is(err, conversation.ErrConversationNotFound) {
			return "", err
		}
	}
	kind := conversation.ConversationKindAdhoc
	switch rctx.MessageContext {
	case MessageContextDM:
		kind = conversation.ConversationKindDM
	case MessageContextGroupThread:
		kind = conversation.ConversationKindGroupThread
	case MessageContextGroupAdhoc:
		kind = conversation.ConversationKindAdhoc
	}
	now := r.clock.Now()
	conv, err := conversation.NewConversation(conversation.NewConversationInput{
		ID:                      conversation.ConversationID(r.idgen.NewULID()),
		Kind:                    kind,
		Title:                   "feishu " + string(kind),
		PrimaryChannelHint:      "feishu",
		PrimaryChannelThreadKey: rctx.VendorThreadKey,
		OpenedAt:                now,
	})
	if err != nil {
		return "", err
	}
	if err := persistence.RunInTx(ctx, r.db, func(txCtx context.Context) error {
		if err := r.convs.Save(txCtx, conv); err != nil {
			return err
		}
		_, err := r.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "conversation.opened",
			Refs:      observability.EventRefs{ConversationID: string(conv.ID())},
			Actor:     r.actor,
			Payload: map[string]any{
				"conversation_id": string(conv.ID()),
				"kind":            string(conv.Kind()),
				"origin":          "feishu.inbound.slash",
			},
		})
		return err
	}); err != nil {
		return "", err
	}
	return conv.ID(), nil
}

func (r *SlashRouter) rejectUsage(ctx context.Context, cmd *SlashCommand, rctx SlashRouteContext, sentinel error, msg string) RouteDecision {
	r.emitRejected(ctx, "usage_error", msg, cmd)
	r.tryEphemeral(ctx, rctx, msg)
	_ = sentinel
	return RouteDecision{
		Kind:    RouteDecisionRejectSlash,
		Reason:  "usage_error",
		Message: msg,
	}
}

func (r *SlashRouter) rejectNotFound(ctx context.Context, cmd *SlashCommand, rctx SlashRouteContext, reason, msg string) RouteDecision {
	r.emitRejected(ctx, reason, msg, cmd)
	r.tryEphemeral(ctx, rctx, msg)
	return RouteDecision{
		Kind:    RouteDecisionRejectSlash,
		Reason:  reason,
		Message: msg,
	}
}

func (r *SlashRouter) rejectInvariant(ctx context.Context, cmd *SlashCommand, rctx SlashRouteContext, reason, msg string) RouteDecision {
	r.emitRejected(ctx, reason, msg, cmd)
	r.tryEphemeral(ctx, rctx, msg)
	return RouteDecision{
		Kind:    RouteDecisionRejectSlash,
		Reason:  reason,
		Message: msg,
	}
}

func (r *SlashRouter) rejectDeferred(ctx context.Context, cmd *SlashCommand, rctx SlashRouteContext, msg string) RouteDecision {
	r.emitRejected(ctx, "feature_deferred", msg, cmd)
	r.tryEphemeral(ctx, rctx, msg)
	return RouteDecision{
		Kind:    RouteDecisionRejectSlash,
		Reason:  "feature_deferred",
		Message: msg,
	}
}

func (r *SlashRouter) emitRejected(ctx context.Context, reason, msg string, cmd *SlashCommand) {
	_, _ = r.sink.Emit(ctx, observability.EmitCommand{
		EventType: "bridge.slash_command_rejected",
		Actor:     r.actor,
		Payload: map[string]any{
			"reason":  reason,
			"message": msg,
			"verb":    string(cmd.Verb),
			"args":    strings.Join(cmd.Args, " "),
			"raw":     cmd.Raw,
		},
	})
}

func (r *SlashRouter) tryEphemeral(ctx context.Context, rctx SlashRouteContext, msg string) {
	if r.ephemeral == nil {
		return
	}
	// Best-effort; failures are visible via bridge.slash_command_rejected
	// (already emitted) — no need to bubble.
	_ = r.ephemeral.ReplyEphemeral(ctx, rctx.VendorThreadKey, string(rctx.IdentityID), msg)
}
