package observability

import (
	"context"
	"errors"
	"time"
)

// EventRepository is the Observability BC repository (observability/00 § 5.1).
//
// Append-only by design: no Update or Delete methods. Only INSERT and read
// access.
type EventRepository interface {
	// Append inserts a single Event row. If a row with the same id already
	// exists, returns ErrEventAlreadyExists (caller-side retry).
	Append(ctx context.Context, e *Event) error

	// FindByID returns the event with the given id. Returns ErrEventNotFound
	// if absent.
	FindByID(ctx context.Context, id EventID) (*Event, error)

	// Find returns events matching filter, ordered by id ascending (ULID
	// lexicographic ordering ≈ time ordering). Empty filter returns latest
	// page (up to limit).
	Find(ctx context.Context, filter EventQueryFilter) ([]*Event, error)
}

// EventQueryFilter is the parameter set for EventRepository.Find. Per
// observability/00 § 5.1 + 02-persistence § 8.2.2 + plan-4 § 3.5.
type EventQueryFilter struct {
	// EventType matches exactly when set.
	EventType *EventType
	// EventTypePrefix matches prefix when set (e.g. "task." matches
	// "task.created" / "task.priority_changed"). Used by `query events
	// --type=task.` per plan-4 § 3.5.
	EventTypePrefix *string
	// Refs matches conjunctively when set (any non-empty field must be a
	// substring of the JSON refs column).
	Refs EventRefsFilter
	// CorrelationID matches exactly when non-nil.
	CorrelationID *string
	// DecisionID matches exactly when non-nil.
	DecisionID *string
	// Actor matches exactly when non-nil.
	Actor *string
	// Since lower bound on occurred_at.
	Since *time.Time
	// Until upper bound on occurred_at (exclusive).
	Until *time.Time
	// Cursor for pagination (events with id > Cursor are returned).
	Cursor *EventID
	// Limit caps the page size. <=0 → DefaultEventQueryLimit. Caller may
	// not exceed MaxEventQueryLimit; implementations should return an
	// explicit error rather than silently truncate (conventions § 17).
	Limit int
}

// DefaultEventQueryLimit caps Find when caller passes Limit <= 0.
const DefaultEventQueryLimit = 100

// MaxEventQueryLimit is the hard cap an EventRepository.Find caller may
// request. Above this implementations return ErrEventQueryLimitTooLarge
// (plan-4 § 3.5 DoD "limit 超过上限 1000 → 报 explicit error，不静默截断").
const MaxEventQueryLimit = 1000

// EventRefsFilter is a subset of EventRefs used as a Find filter; empty
// fields are wildcards.
type EventRefsFilter struct {
	WorkerID       string
	ProjectID      string
	ProposalID     string
	MappingID      string
	ConversationID string
	MessageID      string
	TaskID         string
	ExecutionID    string
	InputRequestID string
	IssueID        string
}

// IsEmpty reports whether all fields are zero.
func (f EventRefsFilter) IsEmpty() bool {
	return f == EventRefsFilter{}
}

// Observability BC sentinel errors. Use errors.Is to test.
var (
	ErrEventNotFound           = errors.New("observability: event not found")
	ErrEventAlreadyExists      = errors.New("observability: event id already exists")
	ErrEventImmutable          = errors.New("observability: events table is append-only, cannot modify/delete")
	ErrEventQueryLimitTooLarge = errors.New("observability: event query limit exceeds MaxEventQueryLimit")
)
