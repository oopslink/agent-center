package observability

import (
	"context"
	"errors"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
)

// EmitCommand is the payload an Application / Domain Service hands to the
// EventSink. The Sink owns id / seq / occurred_at construction; the caller
// owns the tx (ctx) and the semantic content.
type EmitCommand struct {
	EventType     EventType
	Refs          EventRefs
	Actor         Actor
	Payload       map[string]any
	CorrelationID string
	DecisionID    string
	// OccurredAt is optional; defaults to clock.Now().
	OccurredAt time.Time
}

// SeqAllocator hands out monotonic sequence numbers. The SQLite EventRepo
// implements it.
type SeqAllocator interface {
	NextSeq() int64
}

// EventSink is the Domain Service that all BCs use to emit domain events
// inside the same tx as their state UPDATEs (ADR-0014 § 2).
//
// Construction:
//
//	sink := observability.NewEventSink(repo, repo, idGen, clk)
//
// Usage from an application service:
//
//	persistence.RunInTx(ctx, db, func(txCtx context.Context) error {
//	    // ... write state ...
//	    _, err := sink.Emit(txCtx, observability.EmitCommand{...})
//	    return err
//	})
type EventSink struct {
	repo  EventRepository
	seq   SeqAllocator
	idgen idgen.Generator
	clock clock.Clock
}

// NewEventSink constructs an EventSink. The same SQLite EventRepo is
// typically wired as both repo and seq allocator.
func NewEventSink(repo EventRepository, seq SeqAllocator, gen idgen.Generator, clk clock.Clock) *EventSink {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &EventSink{repo: repo, seq: seq, idgen: gen, clock: clk}
}

// Emit constructs an Event, validates it, and appends it through the
// EventRepository.
//
// ctx may carry a tx (recommended; ADR-0014 § 2 same-tx double write). The
// repo decides whether to use the tx or fall back to a single-shot exec.
func (s *EventSink) Emit(ctx context.Context, cmd EmitCommand) (EventID, error) {
	if s == nil {
		return "", errors.New("event sink: nil receiver")
	}
	if s.repo == nil || s.seq == nil || s.idgen == nil {
		return "", errors.New("event sink: missing dependency (repo / seq / idgen)")
	}
	occurredAt := cmd.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = s.clock.Now()
	}
	if cmd.Payload == nil {
		cmd.Payload = map[string]any{}
	}
	e, err := NewEvent(NewEventInput{
		ID:            EventID(s.idgen.NewULID()),
		OccurredAt:    occurredAt,
		Seq:           s.seq.NextSeq(),
		EventType:     cmd.EventType,
		Refs:          cmd.Refs,
		Actor:         cmd.Actor,
		Payload:       cmd.Payload,
		CorrelationID: cmd.CorrelationID,
		DecisionID:    cmd.DecisionID,
		CreatedAt:     s.clock.Now(),
	})
	if err != nil {
		return "", err
	}
	if err := s.repo.Append(ctx, e); err != nil {
		return "", err
	}
	return e.ID(), nil
}
