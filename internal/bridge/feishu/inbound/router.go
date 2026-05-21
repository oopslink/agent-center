package inbound

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/conversation/identity"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

// Router is the FeishuInboundRouter — sole entry for all vendor inbound
// events (plan-7 § 3.5).
//
// Six dispatch branches:
//
//	dedupe drop / unknown drop / panic isolate
//	    └── audit-only outcomes
//	direct_add_message  — DM / @bot / group_thread plain text
//	slash_route          — /track /answer /dispatch parser → SlashRouter
//	card_callback        — interactive card button click → CardCallback
//
// Hard invariants (bridge/00 § 2):
//
//   - The Router never invokes vendor SDK code (Bridge BC has exactly
//     ONE leaf importing the vendor SDK — `client/oapi_adapter.go`).
//   - The Router never makes domain decisions; it only translates the
//     vendor envelope to the matching application service call.
//   - Slash + card paths do NOT trigger Cognition wake (per ADR-0017
//     § 6); free-text inbound paths emit `conversation.message_added`
//     which the Phase 6 WakeScheduler picks up via its whitelist.
//   - The handler is panic-isolated (defer/recover at the top of
//     OnVendorEvent) to keep WebSocket long-conn alive on bugs.
type Router struct {
	clock     clock.Clock
	idgen     idgen.Generator
	sink      *observability.EventSink
	dedupe    *Dedupe
	resolver  *IdentityResolver
	parser    *SlashCommandParser
	slash     *SlashRouter
	card      *CardCallback
	db        *sql.DB
	convs     conversation.ConversationRepository
	msgWriter *convservice.MessageWriter
	actor     observability.Actor
}

// RouterDeps wires the router.
type RouterDeps struct {
	Clock     clock.Clock
	IDGen     idgen.Generator
	Sink      *observability.EventSink
	Dedupe    *Dedupe
	Resolver  *IdentityResolver
	Parser    *SlashCommandParser
	Slash     *SlashRouter
	Card      *CardCallback
	DB        *sql.DB
	Convs     conversation.ConversationRepository
	MsgWriter *convservice.MessageWriter
	Actor     observability.Actor
}

// NewRouter constructs the inbound Router with dep validation.
func NewRouter(deps RouterDeps) (*Router, error) {
	if deps.Sink == nil {
		return nil, errors.New("inbound router: sink required")
	}
	if deps.Dedupe == nil {
		return nil, errors.New("inbound router: dedupe required")
	}
	if deps.Resolver == nil {
		return nil, errors.New("inbound router: resolver required")
	}
	if deps.Parser == nil {
		return nil, errors.New("inbound router: parser required")
	}
	if deps.Slash == nil {
		return nil, errors.New("inbound router: slash router required")
	}
	if deps.Card == nil {
		return nil, errors.New("inbound router: card callback required")
	}
	if deps.DB == nil {
		return nil, errors.New("inbound router: db required")
	}
	if deps.IDGen == nil {
		return nil, errors.New("inbound router: idgen required")
	}
	if deps.Convs == nil {
		return nil, errors.New("inbound router: conversation repo required")
	}
	if deps.MsgWriter == nil {
		return nil, errors.New("inbound router: message writer required")
	}
	if err := deps.Actor.Validate(); err != nil {
		return nil, fmt.Errorf("inbound router: actor: %w", err)
	}
	if deps.Clock == nil {
		deps.Clock = clock.SystemClock{}
	}
	return &Router{
		clock:     deps.Clock,
		idgen:     deps.IDGen,
		sink:      deps.Sink,
		dedupe:    deps.Dedupe,
		resolver:  deps.Resolver,
		parser:    deps.Parser,
		slash:     deps.Slash,
		card:      deps.Card,
		db:        deps.DB,
		convs:     deps.Convs,
		msgWriter: deps.MsgWriter,
		actor:     deps.Actor,
	}, nil
}

// OnVendorEvent is the WebSocket-callback entry point. The router
// emits structured audit events on every path; callers (the WS reader
// goroutine) only observe the returned decision for metrics + tests.
//
// Errors are returned only for INFRASTRUCTURE problems (DB unreachable,
// event sink down). Domain rejections surface as RouteDecision.Kind
// values (RejectSlash / DropUnknown / DropDedupe).
func (r *Router) OnVendorEvent(ctx context.Context, ev VendorEvent) (dec RouteDecision, err error) {
	// Panic isolation: any unhandled panic inside the dispatch path is
	// converted into a structured `bridge.parse_failed { reason=panic }`
	// event so we do NOT crash the WS reader goroutine.
	defer func() {
		if rec := recover(); rec != nil {
			_, _ = r.sink.Emit(ctx, observability.EmitCommand{
				EventType: "bridge.parse_failed",
				Actor:     r.actor,
				Payload: map[string]any{
					"reason":         "panic",
					"message":        fmt.Sprintf("panic during inbound dispatch: %v", rec),
					"vendor_kind":    string(ev.Kind),
					"vendor_msg_ref": ev.VendorMsgRef,
				},
			})
			dec = RouteDecision{Kind: RouteDecisionDropPanic, Reason: "panic",
				Message: fmt.Sprintf("panic during inbound dispatch: %v", rec)}
			err = nil
		}
	}()

	// Structural validation. We CAN NOT proceed without these fields.
	if vErr := ev.Validate(); vErr != nil {
		r.emitParseFailed(ctx, "malformed_payload", vErr.Error(), ev)
		return RouteDecision{
			Kind: RouteDecisionDropUnknown, Reason: "malformed_payload",
			Message: vErr.Error(),
		}, nil
	}

	// Dedupe.
	if r.dedupe.SeenBefore(ev.VendorMsgRef) {
		_, _ = r.sink.Emit(ctx, observability.EmitCommand{
			EventType: "bridge.inbound_dedupe_drop",
			Actor:     r.actor,
			Payload: map[string]any{
				"vendor_msg_ref": ev.VendorMsgRef,
				"vendor_kind":    string(ev.Kind),
			},
		})
		return RouteDecision{Kind: RouteDecisionDropDedupe,
			Reason: "duplicate_vendor_msg_ref", Message: ev.VendorMsgRef}, nil
	}

	// Resolve identity FIRST — both slash + free-text + card need it.
	identityID, rerr := r.resolver.Resolve(ctx, ev.VendorUserID)
	if rerr != nil {
		// Resolver already emitted bridge.parse_failed for 0/>1 cases.
		return RouteDecision{
			Kind:    RouteDecisionDropUnknown,
			Reason:  "identity_resolution_failed",
			Message: rerr.Error(),
		}, rerr
	}

	switch ev.Kind {
	case VendorEventCardActionTrigger:
		decision, e := r.card.Handle(ctx, ev.CardAction, identityID)
		if e != nil {
			return RouteDecision{}, fmt.Errorf("inbound router: card: %w", e)
		}
		r.auditRouted(ctx, ev, decision)
		return decision, nil
	case VendorEventMessageReceive:
		// Slash detection: only on text content. Empty body falls
		// through to direct_add_message.
		cmd, perr := r.parser.Parse(ev.Text)
		switch {
		case perr == nil && cmd != nil:
			decision, e := r.slash.Route(ctx, cmd, SlashRouteContext{
				IdentityID:      identityID,
				VendorThreadKey: ev.VendorThreadKey,
				MessageContext:  ev.Context,
				VendorMsgRef:    ev.VendorMsgRef,
			})
			if e != nil {
				return RouteDecision{}, fmt.Errorf("inbound router: slash: %w", e)
			}
			r.auditRouted(ctx, ev, decision)
			return decision, nil
		case errors.Is(perr, ErrSlashFeatureDeferred):
			// /dispatch — parser returned both cmd + deferred error;
			// the slash router emits the reject event.
			decision, e := r.slash.Route(ctx, cmd, SlashRouteContext{
				IdentityID:      identityID,
				VendorThreadKey: ev.VendorThreadKey,
				MessageContext:  ev.Context,
				VendorMsgRef:    ev.VendorMsgRef,
			})
			if e != nil {
				return RouteDecision{}, fmt.Errorf("inbound router: slash defer: %w", e)
			}
			r.auditRouted(ctx, ev, decision)
			return decision, nil
		case errors.Is(perr, ErrSlashUnknownVerb),
			errors.Is(perr, ErrSlashInsufficientArgs),
			errors.Is(perr, ErrSlashEmpty):
			// Use slash router reject path for consistent
			// bridge.slash_command_rejected emission.
			fakeCmd := &SlashCommand{Raw: ev.Text}
			if cmd != nil {
				fakeCmd = cmd
			}
			decision, _ := r.slash.Route(ctx, fakeCmd, SlashRouteContext{
				IdentityID:      identityID,
				VendorThreadKey: ev.VendorThreadKey,
				MessageContext:  ev.Context,
				VendorMsgRef:    ev.VendorMsgRef,
			})
			r.auditRouted(ctx, ev, decision)
			return decision, nil
		}
		// Free text → direct add-message path.
		decision, e := r.directAddMessage(ctx, ev, identityID)
		if e != nil {
			return RouteDecision{}, fmt.Errorf("inbound router: direct: %w", e)
		}
		r.auditRouted(ctx, ev, decision)
		return decision, nil
	default:
		r.emitParseFailed(ctx, "unknown_event_kind",
			fmt.Sprintf("unknown vendor kind %q", ev.Kind), ev)
		return RouteDecision{Kind: RouteDecisionDropUnknown,
			Reason: "unknown_event_kind", Message: string(ev.Kind)}, nil
	}
}

// directAddMessage handles the @bot / DM / group_thread free-text branch.
// Finds-or-creates the conversation matching (channel=feishu,
// vendor_thread_key) then writes an inbound Message via MessageWriter.
func (r *Router) directAddMessage(ctx context.Context, ev VendorEvent, identityID identity.IdentityID) (RouteDecision, error) {
	convID, err := r.findOrCreateConversation(ctx, ev)
	if err != nil {
		return RouteDecision{}, err
	}
	if _, err := r.msgWriter.AddMessage(ctx, convservice.AddMessageCommand{
		ConversationID:   convID,
		SenderIdentityID: conversation.IdentityRef(identityID),
		ContentKind:      conversation.MessageContentText,
		Content:          ev.Text,
		Direction:        conversation.DirectionInbound,
		VendorMsgRef:     ev.VendorMsgRef,
		Actor:            r.actor,
	}); err != nil {
		if errors.Is(err, conversation.ErrMessageDuplicate) {
			return RouteDecision{
				Kind:           RouteDecisionDirectAddMessage,
				ConversationID: string(convID),
				TargetAction:   "conversation.add_message",
				Reason:         "duplicate_message",
				Message:        "vendor_msg_ref already written",
			}, nil
		}
		return RouteDecision{}, fmt.Errorf("add message: %w", err)
	}
	return RouteDecision{
		Kind:           RouteDecisionDirectAddMessage,
		ConversationID: string(convID),
		TargetAction:   "conversation.add_message",
	}, nil
}

func (r *Router) findOrCreateConversation(ctx context.Context, ev VendorEvent) (conversation.ConversationID, error) {
	if ev.VendorThreadKey != "" {
		if existing, err := r.convs.FindByChannelAndThreadKey(ctx, "feishu", ev.VendorThreadKey); err == nil {
			return existing.ID(), nil
		} else if !errors.Is(err, conversation.ErrConversationNotFound) {
			return "", err
		}
	}
	kind := conversation.ConversationKindAdhoc
	switch ev.Context {
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
		PrimaryChannelThreadKey: ev.VendorThreadKey,
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
				"origin":          "feishu.inbound",
			},
		})
		return err
	}); err != nil {
		return "", err
	}
	return conv.ID(), nil
}

func (r *Router) emitParseFailed(ctx context.Context, reason, msg string, ev VendorEvent) {
	_, _ = r.sink.Emit(ctx, observability.EmitCommand{
		EventType: "bridge.parse_failed",
		Actor:     r.actor,
		Payload: map[string]any{
			"reason":         reason,
			"message":        msg,
			"vendor_kind":    string(ev.Kind),
			"vendor_msg_ref": ev.VendorMsgRef,
		},
	})
}

func (r *Router) auditRouted(ctx context.Context, ev VendorEvent, dec RouteDecision) {
	payload := map[string]any{
		"vendor_kind":     string(ev.Kind),
		"vendor_msg_ref":  ev.VendorMsgRef,
		"route_decision":  dec.Kind.String(),
		"conversation_id": dec.ConversationID,
		"target_action":   dec.TargetAction,
	}
	// reason/message pair must be either both empty (omitted) or both
	// populated (conventions § 16 + observability validateReasonMessage).
	if dec.Reason != "" && dec.Message != "" {
		payload["reason"] = dec.Reason
		payload["message"] = dec.Message
	}
	if _, err := r.sink.Emit(ctx, observability.EmitCommand{
		EventType: "bridge.inbound_routed",
		Actor:     r.actor,
		Payload:   payload,
	}); err != nil {
		// Audit failure is non-fatal but must not be swallowed —
		// fall back to a parse_failed emit (which only fails for the
		// same reasons, so we accept the double error as a last
		// resort). conventions § 17.
		_, _ = r.sink.Emit(ctx, observability.EmitCommand{
			EventType: "bridge.parse_failed",
			Actor:     r.actor,
			Payload: map[string]any{
				"reason":  "audit_emit_failed",
				"message": fmt.Sprintf("audit emit: %v", err),
			},
		})
	}
}

// suppress unused-import lint when idgen isn't directly referenced in
// some builds.
var _ = idgen.NewGenerator
