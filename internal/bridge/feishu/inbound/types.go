// Package inbound hosts the Phase 7 FeishuBridge inbound path —
// vendor event → identity resolution → slash / card / direct
// dispatch → Conversation BC + TaskRuntime BC write-through.
//
// All types in this file are Bridge BC Value Objects (bridge/00 § 1.3 +
// plan-7 § 1.3): no identity, no persistence, `==` equates equivalence,
// JSON-friendly. They MAY be passed across goroutine boundaries.
//
// IMPORTANT (conventions § 9.y): this package is part of the Bridge BC.
// Vendor SDK imports are forbidden here; the SOLE vendor SDK leaf remains
// `internal/bridge/feishu/client/oapi_adapter.go`.
package inbound

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// VendorEventKind classifies the inbound vendor envelope. Mirrors the
// subset of feishu open-platform events the bridge subscribes to per
// bridge/01 § 8.
type VendorEventKind string

// VendorEventKind values.
const (
	// VendorEventMessageReceive corresponds to `im.message.receive_v1` —
	// either a DM or a group message containing text (possibly with
	// @bot).
	VendorEventMessageReceive VendorEventKind = "im.message.receive_v1"
	// VendorEventCardActionTrigger corresponds to `card.action.trigger`.
	VendorEventCardActionTrigger VendorEventKind = "card.action.trigger"
)

// IsValid reports whether the kind is one of the supported enums.
func (k VendorEventKind) IsValid() bool {
	switch k {
	case VendorEventMessageReceive, VendorEventCardActionTrigger:
		return true
	}
	return false
}

// String returns the underlying value.
func (k VendorEventKind) String() string { return string(k) }

// MessageContext narrows the channel surface where a message landed:
// DM (1:1), a group conversation root (no thread), or a group conversation
// reply-thread.
type MessageContext string

// MessageContext values.
const (
	// MessageContextDM is a 1:1 direct message.
	MessageContextDM MessageContext = "dm"
	// MessageContextGroupAdhoc is a group @bot message with NO thread
	// (an adhoc top-level message).
	MessageContextGroupAdhoc MessageContext = "group_adhoc"
	// MessageContextGroupThread is a message inside an existing thread.
	MessageContextGroupThread MessageContext = "group_thread"
)

// IsValid reports whether the context is one of the enums.
func (c MessageContext) IsValid() bool {
	switch c {
	case MessageContextDM, MessageContextGroupAdhoc, MessageContextGroupThread:
		return true
	}
	return false
}

// String returns the underlying value.
func (c MessageContext) String() string { return string(c) }

// VendorEvent is the normalised inbound envelope used inside the Bridge BC.
// The adapter (`internal/bridge/feishu/client/oapi_adapter.go`) is the
// SOLE place that translates raw SDK structs into this VO; downstream
// services (router / resolver / slash dispatch / card callback) consume
// only this struct.
type VendorEvent struct {
	// Kind is the envelope discriminator.
	Kind VendorEventKind
	// VendorMsgRef is the SDK-side unique id used for dedupe (bridge/00
	// invariant 6). For card.action.trigger it's the card_message_id
	// concatenated with the action delivery id.
	VendorMsgRef string
	// VendorThreadKey routes the event to the right Conversation:
	// vendor chat id for DM/group adhoc; thread id for group threads;
	// empty when an envelope synthesis is required.
	VendorThreadKey string
	// VendorUserID is the open_id of the sender (always present).
	VendorUserID string
	// Context narrows the channel surface (DM / group adhoc / group
	// thread). It is empty for VendorEventCardActionTrigger.
	Context MessageContext
	// Text carries the message body for VendorEventMessageReceive.
	Text string
	// CardAction is set for VendorEventCardActionTrigger only.
	CardAction CardActionEvent
	// ReceivedAt is the time the SDK delivered the event to the
	// bridge (post wall-clock). Used for dedupe TTL and audit only.
	ReceivedAt time.Time
}

// CardActionEvent is the normalised view of a `card.action.trigger`
// vendor event (a button click on an outbound interactive card).
type CardActionEvent struct {
	// CardMessageID is the vendor card id (feishu om_xxx for cards).
	CardMessageID string
	// ActionValue is the JSON-parseable map embedded in the button
	// (see renderer button() helper). Required fields per Phase 5
	// renderer:
	//   - action: e.g. "input_request_respond"
	//   - input_request_id: for input_request_* actions
	//   - option_id / option_text: for respond
	ActionValue map[string]any
}

// Action returns the "action" key as a normalised string ("" if missing).
func (e CardActionEvent) Action() string {
	if e.ActionValue == nil {
		return ""
	}
	if v, ok := e.ActionValue["action"].(string); ok {
		return v
	}
	return ""
}

// InputRequestID extracts the input_request_id field, if present.
func (e CardActionEvent) InputRequestID() string {
	if e.ActionValue == nil {
		return ""
	}
	if v, ok := e.ActionValue["input_request_id"].(string); ok {
		return v
	}
	return ""
}

// OptionText extracts the option_text field, if present.
func (e CardActionEvent) OptionText() string {
	if e.ActionValue == nil {
		return ""
	}
	if v, ok := e.ActionValue["option_text"].(string); ok {
		return v
	}
	return ""
}

// Validate enforces the VO invariants. Returns ErrVendorEventMalformed on
// failure (with a descriptive wrap message).
func (v VendorEvent) Validate() error {
	if !v.Kind.IsValid() {
		return fmt.Errorf("%w: unknown kind %q", ErrVendorEventMalformed, v.Kind)
	}
	if strings.TrimSpace(v.VendorMsgRef) == "" {
		return fmt.Errorf("%w: vendor_msg_ref required", ErrVendorEventMalformed)
	}
	if strings.TrimSpace(v.VendorUserID) == "" {
		return fmt.Errorf("%w: vendor_user_id required", ErrVendorEventMalformed)
	}
	switch v.Kind {
	case VendorEventMessageReceive:
		if !v.Context.IsValid() {
			return fmt.Errorf("%w: message_receive requires context", ErrVendorEventMalformed)
		}
		if strings.TrimSpace(v.VendorThreadKey) == "" {
			return fmt.Errorf("%w: message_receive requires vendor_thread_key", ErrVendorEventMalformed)
		}
	case VendorEventCardActionTrigger:
		if strings.TrimSpace(v.CardAction.CardMessageID) == "" {
			return fmt.Errorf("%w: card action requires card_message_id", ErrVendorEventMalformed)
		}
		if v.CardAction.ActionValue == nil {
			return fmt.Errorf("%w: card action requires action_value", ErrVendorEventMalformed)
		}
		if v.CardAction.Action() == "" {
			return fmt.Errorf("%w: card action requires action field", ErrVendorEventMalformed)
		}
	}
	return nil
}

// SlashVerb is the closed enum of supported slash command verbs.
type SlashVerb string

// SlashVerb enum.
const (
	SlashVerbTrack    SlashVerb = "track"
	SlashVerbAnswer   SlashVerb = "answer"
	SlashVerbDispatch SlashVerb = "dispatch" // v1 stub: always rejects
)

// IsValid reports verb membership.
func (v SlashVerb) IsValid() bool {
	switch v {
	case SlashVerbTrack, SlashVerbAnswer, SlashVerbDispatch:
		return true
	}
	return false
}

// String returns the underlying value.
func (v SlashVerb) String() string { return string(v) }

// SlashCommand is the parsed slash representation. Per bridge/01 § 9.1.
type SlashCommand struct {
	Verb SlashVerb
	// Args is the positional argument list AFTER the verb. Empty slice
	// means "no args".
	Args []string
	// Raw is the original message text (for audit + retry).
	Raw string
}

// RouteDecisionKind classifies the outcome of FeishuInboundRouter
// (plan-7 § 1.3 / 3.5). Audit-only — caller has already executed the
// routing action.
type RouteDecisionKind int

// RouteDecisionKind enum.
const (
	// RouteDecisionUnspecified is the zero value (programmer error).
	RouteDecisionUnspecified RouteDecisionKind = iota
	// RouteDecisionDirectAddMessage means the router wrote an inbound
	// Message into Conversation (DM / @bot / group thread free text).
	// The wake decision is deferred to Phase 6 WakeScheduler — Bridge
	// does NOT trigger directly.
	RouteDecisionDirectAddMessage
	// RouteDecisionSlashRoute means a /verb command was parsed and
	// dispatched to the matching application service.
	RouteDecisionSlashRoute
	// RouteDecisionCardCallback means a card.action.trigger was routed
	// to the matching application service (e.g. InputRequest.respond).
	RouteDecisionCardCallback
	// RouteDecisionDropDedupe means the vendor_msg_ref was already
	// seen and the envelope was silently dropped.
	RouteDecisionDropDedupe
	// RouteDecisionDropUnknown means the envelope did not match any
	// known dispatch path; emitted `bridge.parse_failed`.
	RouteDecisionDropUnknown
	// RouteDecisionDropPanic means the dispatch panicked and the
	// router contained the failure; emitted `bridge.parse_failed
	// reason=panic`.
	RouteDecisionDropPanic
	// RouteDecisionRejectSlash means a malformed / forbidden slash
	// command was rejected (ephemeral reply via Bridge outbound).
	RouteDecisionRejectSlash
)

// String returns a stable identifier for events / logs.
func (k RouteDecisionKind) String() string {
	switch k {
	case RouteDecisionDirectAddMessage:
		return "direct_add_message"
	case RouteDecisionSlashRoute:
		return "slash_route"
	case RouteDecisionCardCallback:
		return "card_callback"
	case RouteDecisionDropDedupe:
		return "drop_dedupe"
	case RouteDecisionDropUnknown:
		return "drop_unknown"
	case RouteDecisionDropPanic:
		return "drop_panic"
	case RouteDecisionRejectSlash:
		return "reject_slash"
	default:
		return "unspecified"
	}
}

// RouteDecision is the audit-only routing outcome.
type RouteDecision struct {
	Kind RouteDecisionKind
	// ConversationID is set when the routed write landed in a
	// Conversation (Direct / Slash track-create / Card callback
	// where we wrote a 留痕 Message).
	ConversationID string
	// TargetAction is a human-readable verb describing what the router
	// did (e.g. "conversation.add_message", "task.bind_conversation",
	// "input_request.respond"). Used by audit events.
	TargetAction string
	// Reason is the machine-readable reason; populated for drop /
	// reject decisions. Empty for happy paths.
	Reason string
	// Message is the human-readable reason; required when Reason set.
	Message string
}

// IsSuccessful reports whether the decision corresponds to a positive
// outcome (used by tests + observability dashboards).
func (d RouteDecision) IsSuccessful() bool {
	switch d.Kind {
	case RouteDecisionDirectAddMessage, RouteDecisionSlashRoute,
		RouteDecisionCardCallback:
		return true
	}
	return false
}

// Sentinel errors used across the Phase 7 inbound subsystem. Callers
// pattern-match via `errors.Is`.
var (
	// ErrVendorEventMalformed is returned by VendorEvent.Validate when
	// any structural invariant is violated.
	ErrVendorEventMalformed = errors.New("bridge: vendor event malformed")
	// ErrSlashUnknownVerb is returned by SlashCommandParser when the
	// verb is not in the white-list.
	ErrSlashUnknownVerb = errors.New("bridge: slash unknown verb")
	// ErrSlashInsufficientArgs is returned by SlashCommandParser when
	// arg count is below the per-verb minimum.
	ErrSlashInsufficientArgs = errors.New("bridge: slash insufficient args")
	// ErrSlashEmpty is returned by SlashCommandParser when the input
	// starts with '/' but is just whitespace after the prefix.
	ErrSlashEmpty = errors.New("bridge: slash empty")
	// ErrSlashFeatureDeferred is returned when /dispatch (or other
	// v2 verbs) is invoked under v1.
	ErrSlashFeatureDeferred = errors.New("bridge: slash feature deferred to v2")
	// ErrNoUserIdentity is returned by FeishuInboundIdentityResolver
	// when no user identity exists and we cannot auto-bind.
	ErrNoUserIdentity = errors.New("bridge: no user identity to bind")
	// ErrAmbiguousUserIdentity is returned when more than one user
	// identity exists — manual binding required (v2 introduces an
	// interactive enroll flow).
	ErrAmbiguousUserIdentity = errors.New("bridge: multiple user identities; cannot auto-bind")
	// ErrCardActionMalformed is returned by the card callback when the
	// action_value cannot be parsed.
	ErrCardActionMalformed = errors.New("bridge: card action_value malformed")
)
