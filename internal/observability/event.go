// Package observability hosts the Observability BC tactical types:
// Event AR + VO, EventRepository interface + sentinel errors, EventSink.
//
// Per observability/00-overview § 1: single AR, append-only, no Entity. Per
// ADR-0014 § 2: events INSERT and state UPDATE share one tx via context.
package observability

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/idgen"
)

// EventID is the ULID string identifying an Event.
type EventID string

// String returns the underlying ULID.
func (id EventID) String() string { return string(id) }

// EventType is the BC-prefixed event name (`<bc>.<entity>.<action>`).
type EventType string

// String returns the type.
func (t EventType) String() string { return string(t) }

// Validate checks t is non-empty and follows the `<bc>.<entity>.<action>` or
// `<bc>.<action>` shape (at least one dot, lowercase letters / digits /
// underscores). Closed enum is owned by callers; this is structural only.
func (t EventType) Validate() error {
	s := string(t)
	if s == "" {
		return errors.New("event_type: required")
	}
	if !strings.Contains(s, ".") {
		return fmt.Errorf("event_type %q: must contain '.' separator (<bc>.<action>)", s)
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '_' || c == '.':
		default:
			return fmt.Errorf("event_type %q: contains invalid character %q", s, c)
		}
	}
	return nil
}

// Actor categorises who triggered the event. Per observability/00 § 1.1:
// formalised string with prefix.
type Actor string

// String returns the underlying string.
func (a Actor) String() string { return string(a) }

// Validate enforces the prefix vocabulary. `system` is the only un-prefixed
// allowed value.
func (a Actor) Validate() error {
	s := string(a)
	if s == "" {
		return errors.New("actor: required")
	}
	if s == "system" {
		return nil
	}
	allowed := []string{"user:", "supervisor:", "worker:", "agent:"}
	for _, p := range allowed {
		if strings.HasPrefix(s, p) && len(s) > len(p) {
			return nil
		}
	}
	return fmt.Errorf("actor %q: must be 'system' or one of %v with non-empty suffix", s, allowed)
}

// EventRefs is the JSON-marshallable set of cross-BC references attached to
// an event. Per observability/00 § 1.1: optional keys ({task_id?,
// worker_id?, ...}).
//
// Add fields here as Phase 2+ needs them. JSON marshaller is omitempty-
// safe: empty struct serialises to `{}`.
type EventRefs struct {
	IdentityID       string `json:"identity_id,omitempty"`
	OrganizationID   string `json:"organization_id,omitempty"`
	MemberID         string `json:"member_id,omitempty"`
	WorkerID         string `json:"worker_id,omitempty"`
	ProjectID        string `json:"project_id,omitempty"`
	ProposalID       string `json:"proposal_id,omitempty"`
	MappingID        string `json:"mapping_id,omitempty"`
	ConversationID   string `json:"conversation_id,omitempty"`
	MessageID        string `json:"message_id,omitempty"`
	TaskID           string `json:"task_id,omitempty"`
	ExecutionID      string `json:"execution_id,omitempty"`
	InputRequestID   string `json:"input_request_id,omitempty"`
	IssueID          string `json:"issue_id,omitempty"`
}

// MarshalJSON serialises EventRefs with stable key ordering (Go encoding/json
// already sorts struct fields by declaration order, which matches our schema
// ordering).
func (r EventRefs) MarshalJSON() ([]byte, error) {
	type alias EventRefs
	return json.Marshal(alias(r))
}

// Event is the AR for a single domain event row.
//
// Immutable by construction: all fields are unexported and only set through
// the NewEvent constructor; getters expose read-only access.
type Event struct {
	id            EventID
	occurredAt    time.Time
	seq           int64
	eventType     EventType
	refs          EventRefs
	actor         Actor
	payload       map[string]any
	correlationID string
	decisionID    string
	createdAt     time.Time
}

// NewEventInput captures the constructor arguments for NewEvent.
type NewEventInput struct {
	ID            EventID
	OccurredAt    time.Time
	Seq           int64
	EventType     EventType
	Refs          EventRefs
	Actor         Actor
	Payload       map[string]any
	CorrelationID string
	DecisionID    string
	CreatedAt     time.Time
}

// NewEvent constructs a validated Event. Returns the validation error if any
// invariant is violated.
//
// Validation rules per observability/00 § 2:
//   - event_type non-empty + structural form
//   - actor non-empty + prefix valid
//   - occurred_at non-zero
//   - payload reason/message pair: if "reason" present, "message" required
//     and non-empty (conventions § 16)
//   - id, seq, occurredAt must be set
func NewEvent(in NewEventInput) (*Event, error) {
	if in.ID == "" {
		return nil, errors.New("event: id required")
	}
	if !idgen.IsValid(string(in.ID)) {
		return nil, fmt.Errorf("event: id %q not a valid ULID", in.ID)
	}
	if in.OccurredAt.IsZero() {
		return nil, errors.New("event: occurred_at required")
	}
	if in.Seq <= 0 {
		return nil, fmt.Errorf("event: seq must be positive, got %d", in.Seq)
	}
	if err := in.EventType.Validate(); err != nil {
		return nil, err
	}
	if err := in.Actor.Validate(); err != nil {
		return nil, err
	}
	payload := in.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	if err := validateReasonMessage(payload); err != nil {
		return nil, err
	}
	createdAt := in.CreatedAt
	if createdAt.IsZero() {
		createdAt = in.OccurredAt
	}
	return &Event{
		id:            in.ID,
		occurredAt:    in.OccurredAt.UTC(),
		seq:           in.Seq,
		eventType:     in.EventType,
		refs:          in.Refs,
		actor:         in.Actor,
		payload:       payload,
		correlationID: in.CorrelationID,
		decisionID:    in.DecisionID,
		createdAt:     createdAt.UTC(),
	}, nil
}

// ID returns the event ULID.
func (e *Event) ID() EventID { return e.id }

// OccurredAt returns the business time.
func (e *Event) OccurredAt() time.Time { return e.occurredAt }

// Seq returns the monotonic sequence number.
func (e *Event) Seq() int64 { return e.seq }

// Type returns the event type.
func (e *Event) Type() EventType { return e.eventType }

// Refs returns a copy of the event refs.
func (e *Event) Refs() EventRefs { return e.refs }

// Actor returns the triggering actor.
func (e *Event) Actor() Actor { return e.actor }

// Payload returns a copy of the payload map.
func (e *Event) Payload() map[string]any {
	out := make(map[string]any, len(e.payload))
	for k, v := range e.payload {
		out[k] = v
	}
	return out
}

// CorrelationID returns the optional correlation id.
func (e *Event) CorrelationID() string { return e.correlationID }

// DecisionID returns the optional decision id.
func (e *Event) DecisionID() string { return e.decisionID }

// CreatedAt returns the INSERT-time timestamp.
func (e *Event) CreatedAt() time.Time { return e.createdAt }

// PayloadJSON returns the JSON-encoded payload (deterministic key ordering
// is not guaranteed; persistence layer is responsible for canonicalising if
// required).
func (e *Event) PayloadJSON() ([]byte, error) {
	return json.Marshal(e.payload)
}

// RefsJSON returns the JSON-encoded refs.
func (e *Event) RefsJSON() ([]byte, error) {
	return json.Marshal(e.refs)
}

func validateReasonMessage(payload map[string]any) error {
	reason, hasReason := payload["reason"]
	msg, hasMsg := payload["message"]
	if !hasReason {
		return nil
	}
	rs, ok := reason.(string)
	if !ok || rs == "" {
		return errors.New("event: payload.reason must be a non-empty string")
	}
	if !hasMsg {
		return errors.New("event: payload.reason set requires payload.message (conventions § 16)")
	}
	ms, ok := msg.(string)
	if !ok || ms == "" {
		return errors.New("event: payload.message must be a non-empty string when reason is set")
	}
	return nil
}
