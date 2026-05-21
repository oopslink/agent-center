package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/oopslink/agent-center/internal/bridge/feishu/client"
	"github.com/oopslink/agent-center/internal/bridge/feishu/ledger"
	"github.com/oopslink/agent-center/internal/bridge/feishu/renderer"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/conversation/identity"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
)

// handleEvent routes one events-table row to the right handler.
func (s *Service) handleEvent(ctx context.Context, ev *observability.Event) error {
	switch ev.Type() {
	case "conversation.opened":
		return s.handleConversationOpened(ctx, ev)
	case "conversation.message_added":
		return s.handleMessageAdded(ctx, ev)
	case "input_request.responded":
		return s.auditInputRequestEvent(ctx, ev, "responded")
	case "input_request.timed_out":
		return s.auditInputRequestEvent(ctx, ev, "timed_out")
	case "input_request.canceled":
		return s.auditInputRequestEvent(ctx, ev, "canceled")
	}
	// Unknown event types are *expected* (events table is multi-BC). We
	// silently skip them — emitting a `bridge.event_ignored` for every
	// non-bridge event type would flood events. The cursor still advances
	// (see RunOnce), so the noisy path stays bounded.
	return nil
}

// auditInputRequestEvent records audit observability events for InputRequest
// state transitions without performing update_card (deferred to v2+, plan
// risk 2). Keeps the routing table complete + visible.
func (s *Service) auditInputRequestEvent(ctx context.Context, ev *observability.Event, kind string) error {
	_, err := s.deps.Sink.Emit(ctx, observability.EmitCommand{
		EventType: "channel.delivered",
		Refs: observability.EventRefs{
			ConversationID: ev.Refs().ConversationID,
			MessageID:      ev.Refs().MessageID,
			InputRequestID: ev.Refs().InputRequestID,
		},
		Actor: s.cfg.Actor,
		Payload: map[string]any{
			"channel":     s.cfg.Channel,
			"audit_kind":  kind,
			"source_event": ev.Type().String(),
			"note":        "input_request state audit (no update_card in v1; see plan-5 § 6 risk 2)",
		},
	})
	return err
}

// handleConversationOpened renders + delivers a root card for kind=task / kind=issue
// (other kinds skipped — root card only applies to task/issue per § 7.5).
func (s *Service) handleConversationOpened(ctx context.Context, ev *observability.Event) error {
	convIDRaw := ev.Refs().ConversationID
	if convIDRaw == "" {
		return s.emitRoutingFailed(ctx, ev, "missing_conversation_id", "conversation.opened event has empty refs.conversation_id")
	}
	convID := conversation.ConversationID(convIDRaw)
	conv, err := s.deps.Conversations.FindByID(ctx, convID)
	if err != nil {
		return s.emitRoutingFailed(ctx, ev, "conversation_not_found", fmt.Sprintf("FindByID(%q): %v", convID, err))
	}
	kind := string(conv.Kind())
	if kind != string(conversation.ConversationKindTask) &&
		kind != string(conversation.ConversationKindIssue) {
		// non-task/issue conversation.opened — not Bridge's concern.
		return nil
	}

	// Derive subject ref (Task #N / Issue #N).
	subject := kind + " " + string(conv.ID())
	title := conv.Title()
	if kind == "task" && s.deps.TaskByConversation != nil {
		if sref, ttl, err := s.deps.TaskByConversation(ctx, conv.ID()); err == nil && sref != "" {
			subject = sref
			if ttl != "" {
				title = ttl
			}
		}
	}
	if kind == "issue" && s.deps.IssueByConversation != nil {
		if sref, ttl, err := s.deps.IssueByConversation(ctx, conv.ID()); err == nil && sref != "" {
			subject = sref
			if ttl != "" {
				title = ttl
			}
		}
	}

	rendered, err := s.deps.Renderer.RenderRootCard(renderer.RootCardInput{
		Conversation: renderer.ConversationInput{
			ConversationID: string(conv.ID()),
			Kind:           kind,
			Title:          title,
		},
		SubjectRef: subject,
	})
	if err != nil {
		return s.emitDeliveryFailed(ctx, ev, "", string(conv.ID()), 0, "render_failed", err.Error())
	}
	target := client.Target{
		Channel:   s.cfg.Channel,
		ThreadKey: conv.PrimaryChannelThreadKey(),
	}
	if target.ThreadKey == "" {
		// fresh conversation: route to opener's preferred channel binding.
		target = s.fallbackTargetForActor(ctx, ev.Actor())
		target.Channel = s.cfg.Channel
	}
	rootMsgID := "root-card:" + string(conv.ID())
	return s.deliver(ctx, ev, deliveryRequest{
		MessageID:      rootMsgID,
		ConversationID: string(conv.ID()),
		Target:         target,
		Rendered:       rendered,
		WriteBackToConv: true,
	})
}

func (s *Service) handleMessageAdded(ctx context.Context, ev *observability.Event) error {
	// Only outbound direction is Bridge's concern.
	direction, _ := ev.Payload()["direction"].(string)
	if direction != string(conversation.DirectionOutbound) {
		return nil
	}
	convIDRaw := ev.Refs().ConversationID
	msgIDRaw := ev.Refs().MessageID
	if convIDRaw == "" || msgIDRaw == "" {
		return s.emitRoutingFailed(ctx, ev, "missing_refs",
			"conversation.message_added event missing conversation_id / message_id refs")
	}
	convID := conversation.ConversationID(convIDRaw)
	msgID := conversation.MessageID(msgIDRaw)

	msg, err := s.deps.Messages.FindByID(ctx, msgID)
	if err != nil {
		return s.emitRoutingFailed(ctx, ev, "message_not_found",
			fmt.Sprintf("MessageRepository.FindByID(%q): %v", msgID, err))
	}
	conv, err := s.deps.Conversations.FindByID(ctx, convID)
	if err != nil {
		return s.emitRoutingFailed(ctx, ev, "conversation_not_found",
			fmt.Sprintf("ConversationRepository.FindByID(%q): %v", convID, err))
	}
	// Resolve InputRequest when the Message points at one (agent_finding +
	// input_request_ref). Cross-BC read is allowed (conventions § 9.z).
	var ir *renderer.InputRequestInput
	if string(msg.ContentKind()) == renderer.ContentKindAgentFinding && msg.InputRequestRef() != "" {
		if s.deps.InputRequests == nil {
			return s.emitDeliveryFailed(ctx, ev, string(msgID), string(convID), 0,
				"input_request_repo_missing",
				"agent_finding message has input_request_ref but dispatcher has no InputRequestRepository wired")
		}
		req, err := s.deps.InputRequests.FindByID(ctx, taskruntime.InputRequestID(msg.InputRequestRef()))
		if err != nil {
			return s.emitDeliveryFailed(ctx, ev, string(msgID), string(convID), 0,
				"input_request_not_found",
				fmt.Sprintf("InputRequestRepository.FindByID(%q): %v", msg.InputRequestRef(), err))
		}
		ir = &renderer.InputRequestInput{
			ID:       string(req.ID()),
			Question: req.Question(),
			Options:  req.Options(),
		}
	}
	rendered, err := s.deps.Renderer.RenderMessage(renderer.MessageInput{
		MessageID:       string(msg.ID()),
		ContentKind:     string(msg.ContentKind()),
		Content:         msg.Content(),
		Sender:          string(msg.SenderIdentityID()),
		InputRequestRef: msg.InputRequestRef(),
	}, ir)
	if err != nil {
		return s.emitDeliveryFailed(ctx, ev, string(msgID), string(convID), 0,
			"render_failed", err.Error())
	}
	target := client.Target{
		Channel:   s.cfg.Channel,
		ThreadKey: conv.PrimaryChannelThreadKey(),
	}
	if target.ThreadKey == "" {
		target = s.fallbackTargetForActor(ctx, ev.Actor())
		target.Channel = s.cfg.Channel
	}
	return s.deliver(ctx, ev, deliveryRequest{
		MessageID:      string(msgID),
		ConversationID: string(convID),
		Target:         target,
		Rendered:       rendered,
	})
}

// fallbackTargetForActor resolves a preferred channel binding for the
// event actor. Returns empty Target if no binding (caller decides what to
// do).
func (s *Service) fallbackTargetForActor(ctx context.Context, actor observability.Actor) client.Target {
	if s.deps.Bindings == nil {
		return client.Target{}
	}
	binding, err := s.deps.Bindings.FindPreferred(ctx, identity.IdentityID(string(actor)), identity.Channel(s.cfg.Channel))
	if err != nil {
		return client.Target{}
	}
	return client.Target{
		Channel:      s.cfg.Channel,
		VendorUserID: binding.VendorUserID(),
	}
}

// deliveryRequest groups arguments to the deliver pipeline.
type deliveryRequest struct {
	MessageID       string
	ConversationID  string
	Target          client.Target
	Rendered        renderer.RenderedCard
	WriteBackToConv bool // true for root card → update conversation.primary_channel_thread_key
}

// deliver is the unified vendor-send pipeline: ledger append → SendXxx →
// ledger update + observability emit + (optional) conversation/message
// write-back, with all errors emitted explicitly (conventions § 17).
func (s *Service) deliver(ctx context.Context, ev *observability.Event, req deliveryRequest) error {
	// Tx1 — Bridge BC: append pending ledger row.
	now := s.deps.Clock.Now()
	ledgerRow, err := ledger.NewLedger(ledger.NewLedgerInput{
		ID:             s.deps.IDGen.NewULID(),
		MessageID:      req.MessageID,
		ConversationID: req.ConversationID,
		Channel:        s.cfg.Channel,
		ThreadKey:      req.Target.ThreadKey,
		CreatedAt:      now,
	})
	if err != nil {
		return s.emitDeliveryFailed(ctx, ev, req.MessageID, req.ConversationID, 0,
			"ledger_invalid", err.Error())
	}
	if err := s.deps.Ledger.Append(ctx, ledgerRow); err != nil {
		// Duplicate ledger row → idempotency hit; treat as a benign skip
		// (this event was already processed in a previous batch).
		if errors.Is(err, ledger.ErrLedgerDuplicate) {
			return nil
		}
		return s.emitDeliveryFailed(ctx, ev, req.MessageID, req.ConversationID, 0,
			"ledger_append_failed", err.Error())
	}

	// Vendor send (no agent-center tx — vendor IO is at-least-once).
	var sendErr error
	var result client.SendResult
	if req.Rendered.MessageKind == renderer.MessageKindText {
		result, sendErr = s.deps.Client.SendTextMessage(ctx, req.Target, extractTextPayload(req.Rendered.CardJSON))
	} else {
		result, sendErr = s.deps.Client.SendInteractiveCard(ctx, req.Target, req.Rendered.CardJSON)
	}
	if sendErr != nil {
		reason, message := classifyVendorError(sendErr)
		if mfErr := s.deps.Ledger.MarkFailed(ctx, ledgerRow.ID(), ledgerRow.Version(), sendErr.Error()); mfErr != nil {
			_, _ = s.deps.Sink.Emit(ctx, observability.EmitCommand{
				EventType: "bridge.feishu.ledger_mark_failed_failed",
				Actor:     s.cfg.Actor,
				Payload: map[string]any{
					"reason":  "ledger_mark_failed_failed",
					"message": mfErr.Error(),
				},
			})
		}
		return s.emitDeliveryFailed(ctx, ev, req.MessageID, req.ConversationID, 1, reason, message)
	}

	// Tx2 — Bridge BC: MarkDelivered + emit channel.delivered.
	err = persistence.RunInTx(ctx, s.deps.DB, func(txCtx context.Context) error {
		if err := s.deps.Ledger.MarkDelivered(txCtx, ledgerRow.ID(), ledgerRow.Version(),
			result.VendorMsgRef, result.CardMessageID, result.ThreadKey); err != nil {
			return err
		}
		_, err := s.deps.Sink.Emit(txCtx, observability.EmitCommand{
			EventType: "channel.delivered",
			Refs: observability.EventRefs{
				MessageID:      req.MessageID,
				ConversationID: req.ConversationID,
			},
			Actor: s.cfg.Actor,
			Payload: map[string]any{
				"channel":         s.cfg.Channel,
				"vendor_msg_ref":  result.VendorMsgRef,
				"card_message_id": result.CardMessageID,
				"thread_key":      result.ThreadKey,
			},
		})
		return err
	})
	if err != nil {
		return s.emitDeliveryFailed(ctx, ev, req.MessageID, req.ConversationID, 0,
			"ledger_mark_delivered_failed", err.Error())
	}

	// Tx3 — Conversation BC: write-back vendor_msg_ref + (optional)
	// primary_channel_thread_key. We deliberately use a separate tx to
	// keep BC physical isolation (§ 9.z); the dispatcher remains the
	// orchestrator across BCs.
	wbErr := persistence.RunInTx(ctx, s.deps.DB, func(txCtx context.Context) error {
		if !req.WriteBackToConv {
			if result.VendorMsgRef != "" {
				if err := s.deps.Messages.UpdateVendorMsgRef(txCtx,
					conversation.MessageID(req.MessageID), result.VendorMsgRef); err != nil {
					// Message.UpdateVendorMsgRef is idempotent (returns
					// ErrMessageImmutable when already set); treat that as
					// success.
					if !errors.Is(err, conversation.ErrMessageImmutable) {
						return err
					}
				}
			}
			return nil
		}
		// Root card path: backfill primary_channel_thread_key on the conv.
		conv, err := s.deps.Conversations.FindByID(txCtx, conversation.ConversationID(req.ConversationID))
		if err != nil {
			return err
		}
		threadKey := result.ThreadKey
		if threadKey == "" {
			threadKey = req.Target.ThreadKey
		}
		if threadKey == "" {
			return errors.New("dispatcher: vendor returned no thread_key for root card")
		}
		// Skip if already set (idempotent for retry path).
		if conv.PrimaryChannelThreadKey() != "" {
			return nil
		}
		return s.deps.Conversations.UpdatePrimaryChannel(txCtx, conv.ID(),
			s.cfg.Channel, threadKey, conv.Version(), s.deps.Clock.Now())
	})
	if wbErr != nil {
		_, _ = s.deps.Sink.Emit(ctx, observability.EmitCommand{
			EventType: "bridge.callback_failed",
			Refs: observability.EventRefs{
				ConversationID: req.ConversationID,
				MessageID:      req.MessageID,
			},
			Actor: s.cfg.Actor,
			Payload: map[string]any{
				"reason":  "callback_writeback_failed",
				"message": wbErr.Error(),
			},
		})
		return wbErr
	}
	return nil
}

func (s *Service) emitRoutingFailed(ctx context.Context, ev *observability.Event, reason, message string) error {
	_, err := s.deps.Sink.Emit(ctx, observability.EmitCommand{
		EventType: "bridge.routing_failed",
		Refs:      ev.Refs(),
		Actor:     s.cfg.Actor,
		Payload: map[string]any{
			"reason":       reason,
			"message":      message,
			"source_event": ev.Type().String(),
			"source_id":    string(ev.ID()),
		},
	})
	return err
}

func (s *Service) emitDeliveryFailed(ctx context.Context, ev *observability.Event, messageID, conversationID string, retryCount int, reason, message string) error {
	refs := observability.EventRefs{
		ConversationID: conversationID,
		MessageID:      messageID,
	}
	_, err := s.deps.Sink.Emit(ctx, observability.EmitCommand{
		EventType: "channel.delivery_failed",
		Refs:      refs,
		Actor:     s.cfg.Actor,
		Payload: map[string]any{
			"reason":      reason,
			"message":     message,
			"channel":     s.cfg.Channel,
			"retry_count": retryCount,
		},
	})
	return err
}

// classifyVendorError maps a Client error into (reason, message) per § 16.
func classifyVendorError(err error) (string, string) {
	switch {
	case errors.Is(err, client.ErrAuthFailed):
		return "auth_failed", err.Error()
	case errors.Is(err, client.ErrPermanentFailure):
		return "4xx_permanent", err.Error()
	case errors.Is(err, client.ErrTransientFailure):
		return "5xx_exhausted", err.Error()
	case errors.Is(err, client.ErrNotConnected):
		return "connect_lost", err.Error()
	}
	return "vendor_error", err.Error()
}

// extractTextPayload pulls the "text" field from the rendered envelope so
// the FeishuClient SendTextMessage path receives just the markdown body.
func extractTextPayload(envelopeJSON string) string {
	// envelope is {"text":"..."}; do a cheap parse without bringing in
	// encoding/json to keep the hot path light. The dispatcher already
	// knows the renderer produced this shape.
	const prefix = `{"text":"`
	if len(envelopeJSON) < len(prefix)+2 || envelopeJSON[:len(prefix)] != prefix {
		return envelopeJSON
	}
	// strip closing `"}`
	if envelopeJSON[len(envelopeJSON)-2:] != `"}` {
		return envelopeJSON
	}
	// unescape minimally (the renderer used json.Marshal so quote / NL /
	// tab / backslash are the relevant escapes).
	body := envelopeJSON[len(prefix) : len(envelopeJSON)-2]
	return unescapeJSONStringContent(body)
}

func unescapeJSONStringContent(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case '"':
				out = append(out, '"')
			case '\\':
				out = append(out, '\\')
			case 'n':
				out = append(out, '\n')
			case 't':
				out = append(out, '\t')
			case 'r':
				out = append(out, '\r')
			default:
				out = append(out, s[i+1])
			}
			i++
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}

// unused helper kept for future fine-grained backoff tuning.
var _ = time.Second
