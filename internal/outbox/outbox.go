// Package outbox is the cross-bounded-context reliability seam for v2.7
// (plan §10 OQ1). A producing AppService writes its own BC state AND an
// outbox event in ONE local transaction; an asynchronous, idempotent
// (dedup by event_id), replayable relay then applies the cross-BC effect via
// registered projectors.
//
// This is one of three SEPARATE reliable channels (OQ1): (1) this DB outbox
// for cross-BC state projection, (2) the Worker control flow (phase D, its
// own ack/idempotency), (3) the observation/SSE stream (AgentActivityEvent).
// They are intentionally NOT one table.
//
// A0 ships the table + repo + idempotent relay skeleton only; it has no
// cross-BC producers yet. The first real producer is ProjectManager's
// subscriber→participant projection in phase B (ADR-0052).
package outbox

import (
	"context"
	"errors"
	"time"
)

// ErrEventNotFound is returned when an event id is absent.
var ErrEventNotFound = errors.New("outbox: event not found")

// Event is one outbox record. ID is the dedup key (a ULID): projectors are
// applied at-least-once and MUST be idempotent on ID. Payload/Refs are opaque
// JSON the producer writes and the projector interprets.
type Event struct {
	ID          string // event_id (ULID) — the idempotency key
	EventType   string // e.g. "pm.task.assigned" (<bc>.<entity>.<action>)
	Refs        string // JSON of cross-BC references (task_id, agent_id, ...)
	Payload     string // JSON payload
	CreatedAt   time.Time
	ProcessedAt *time.Time // nil = not yet fully relayed
}

// IsProcessed reports whether the relay has finished this event.
func (e Event) IsProcessed() bool { return e.ProcessedAt != nil }

// Repository persists outbox events. Append is called inside the producer's
// own transaction (via persistence.ExecutorFromCtx) so the state write and
// the event are atomic.
type Repository interface {
	// Append writes one event. Producers call this in their own tx.
	Append(ctx context.Context, e Event) error
	// FetchUnprocessed returns up to limit events with processed_at IS NULL,
	// oldest first (by id, which is time-ordered).
	FetchUnprocessed(ctx context.Context, limit int) ([]Event, error)
	// MarkProcessed sets processed_at on an event (idempotent).
	MarkProcessed(ctx context.Context, id string, t time.Time) error
}

// AppliedStore records which (projector, event_id) pairs a projector has
// already applied, so redelivery is a no-op. This is the concrete dedup-by-
// event_id mechanism behind the "idempotent projector" requirement (OQ1):
// even a non-idempotent projector body runs at most once per event.
type AppliedStore interface {
	// IsApplied reports whether projector has already applied eventID.
	IsApplied(ctx context.Context, projector, eventID string) (bool, error)
	// MarkApplied records that projector applied eventID (idempotent insert).
	MarkApplied(ctx context.Context, projector, eventID string, t time.Time) error
}
