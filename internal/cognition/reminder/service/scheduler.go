// Package service holds the Cognition Reminder application services: the
// ReminderScheduler (§3.3) that scans due reminders and fires them. Delivery
// (§3.4) is a separate projector that consumes the emitted
// cognition.reminder.fired event and posts to the remindee's conversation, so
// the scheduler stays decoupled from the conversation/wake machinery.
package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/reminder"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

// Domain event types (§3.7). The scheduler emits fired + completed; the create/
// pause/resume/update/cancel events are emitted by the reminder AppService when
// those ops happen.
const (
	EventReminderFired     observability.EventType = "cognition.reminder.fired"
	EventReminderCompleted observability.EventType = "cognition.reminder.completed"
)

// EventEmitter is the slice of observability.EventSink the scheduler needs; a
// narrow port keeps the scheduler unit-testable with a fake. *observability.EventSink
// satisfies it.
type EventEmitter interface {
	Emit(ctx context.Context, cmd observability.EmitCommand) (observability.EventID, error)
}

// IDGen produces a new firing ULID.
type IDGen interface{ NewID() string }

// IDGenFunc adapts a func to IDGen.
type IDGenFunc func() string

func (f IDGenFunc) NewID() string { return f() }

// ReminderScheduler scans due reminders and fires them (§3.3). It is driven by
// the ReminderTickProjector on the outbox Pump tick (§D4).
type ReminderScheduler struct {
	db    *sql.DB
	repo  reminder.Repository
	sink  EventEmitter
	idGen IDGen
}

// NewReminderScheduler constructs the scheduler.
func NewReminderScheduler(db *sql.DB, repo reminder.Repository, sink EventEmitter, idGen IDGen) *ReminderScheduler {
	return &ReminderScheduler{db: db, repo: repo, sink: sink, idGen: idGen}
}

// Tick scans for due reminders (status=active, next_run_at<=now) and fires each
// once. Each fire is its own tx (RecordFire + Update-CAS + reminder_firings +
// fired event), so one bad reminder cannot abort the others. Returns the number
// fired. A per-reminder error is collected and returned (joined) but does not
// stop the sweep.
func (s *ReminderScheduler) Tick(ctx context.Context, now time.Time) (fired int, err error) {
	due, derr := s.repo.FindDue(ctx, now)
	if derr != nil {
		return 0, derr
	}
	var errs []error
	for _, r := range due {
		if ferr := s.fireOne(ctx, r, now); ferr != nil {
			errs = append(errs, ferr)
			continue
		}
		fired++
	}
	return fired, errors.Join(errs...)
}

// fireOne records one firing in a single transaction (§3.3 Fire flow): advance
// the aggregate (RecordFire → recompute next_run_at or complete), CAS-persist it,
// append the reminder_firings row, and emit the fired event (+ completed when the
// fire ended the reminder). The reminder_firings + outbox event share the
// aggregate's tx so they are atomic with the state change.
func (s *ReminderScheduler) fireOne(ctx context.Context, r *reminder.Reminder, now time.Time) error {
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := r.RecordFire(now); err != nil {
			return err
		}
		if err := s.repo.Update(txCtx, r); err != nil {
			return err
		}
		if err := s.repo.AppendFiring(txCtx, reminder.Firing{
			ID:         s.idGen.NewID(),
			ReminderID: r.ID().String(),
			FiredAt:    now,
			Outcome:    reminder.OutcomeDelivered,
		}); err != nil {
			return err
		}
		refs := observability.EventRefs{
			AgentID:        r.RemindeeAgentID(),
			ProjectID:      r.ProjectID(),
			OrganizationID: r.OrganizationID(),
		}
		payload := map[string]any{
			"reminder_id":       r.ID().String(),
			"remindee_agent_id": r.RemindeeAgentID(),
			"creator_ref":       r.CreatorRef(),
			"content":           r.Content(),
			"fired_at":          now.UTC().Format(time.RFC3339Nano),
			"fired_count":       r.FiredCount(),
		}
		// fired → delivery projector posts to the remindee's conversation.
		if _, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: EventReminderFired, Refs: refs, Actor: "system", Payload: payload,
		}); err != nil {
			return err
		}
		// completed → lifecycle/audit (once fired its last time).
		if r.Status() == reminder.StatusCompleted {
			if _, err := s.sink.Emit(txCtx, observability.EmitCommand{
				EventType: EventReminderCompleted, Refs: refs, Actor: "system",
				Payload: map[string]any{"reminder_id": r.ID().String()},
			}); err != nil {
				return err
			}
		}
		return nil
	})
}
