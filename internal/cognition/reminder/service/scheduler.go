// Package service holds the Cognition Reminder application services: the
// ReminderScheduler (§3.3) that scans due reminders and fires them. Delivery
// (§3.4) is a separate projector that consumes the emitted
// cognition.reminder.fired event and posts to the remindee's conversation, so
// the scheduler stays decoupled from the conversation/wake machinery.
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/reminder"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/outbox"
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

// IDGen produces a new firing ULID (idgen.Generator satisfies it).
type IDGen interface{ NewULID() string }

// IDGenFunc adapts a func to IDGen.
type IDGenFunc func() string

func (f IDGenFunc) NewULID() string { return f() }

// ReminderScheduler scans due reminders and fires them (§3.3). It is driven by
// the ReminderTickProjector on the outbox Pump tick (§D4).
type ReminderScheduler struct {
	db     *sql.DB
	repo   reminder.Repository
	sink   EventEmitter
	outbox outbox.Repository
	idGen  IDGen
}

// NewReminderScheduler constructs the scheduler. outboxRepo is REQUIRED for
// delivery: the fired event is appended to the outbox (in the same tx as the
// state change) so the ReminderDeliveryProjector — which drains the outbox, not
// the observability events table — actually delivers/wakes the remindee (F1).
func NewReminderScheduler(db *sql.DB, repo reminder.Repository, sink EventEmitter, outboxRepo outbox.Repository, idGen IDGen) *ReminderScheduler {
	return &ReminderScheduler{db: db, repo: repo, sink: sink, outbox: outboxRepo, idGen: idGen}
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
		didFire, ferr := s.fireOne(ctx, r, now)
		if ferr != nil {
			errs = append(errs, ferr)
			continue
		}
		if didFire {
			fired++
		}
	}
	return fired, errors.Join(errs...)
}

// fireOne processes one due reminder in a single transaction (§3.3 Fire flow).
// Normally it FIRES: advance the aggregate (RecordFire → recompute next_run_at or
// complete), CAS-persist it, append the reminder_firings row, and emit the fired
// event (+ completed when the fire ended the reminder). But when skip_if_overlap
// is set and the previous occurrence is still IN FLIGHT (a pending firing exists),
// it SKIPS this occurrence instead: advance past it (RecordSkip — no fired_count
// bump, no delivery) and record a skipped_overlap firing, so a slow remindee is
// not piled on. The firing + event share the aggregate's tx so they are atomic
// with the state change. Returns whether it actually fired (a skip returns false)
// so Tick counts only real fires.
func (s *ReminderScheduler) fireOne(ctx context.Context, r *reminder.Reminder, now time.Time) (bool, error) {
	didFire := false
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if r.SkipIfOverlap() {
			inFlight, err := s.repo.HasPendingFiring(txCtx, r.ID().String())
			if err != nil {
				return err
			}
			if inFlight {
				// Previous occurrence dispatched but not yet delivered → drop this
				// one: advance the schedule without firing, record the skip.
				if err := r.RecordSkip(now); err != nil {
					return err
				}
				if err := s.repo.Update(txCtx, r); err != nil {
					return err
				}
				return s.repo.AppendFiring(txCtx, reminder.Firing{
					ID:         s.idGen.NewULID(),
					ReminderID: r.ID().String(),
					FiredAt:    now,
					Outcome:    reminder.OutcomeSkippedOverlap,
					Detail:     "previous firing still in flight",
				})
			}
		}
		if err := r.RecordFire(now); err != nil {
			return err
		}
		if err := s.repo.Update(txCtx, r); err != nil {
			return err
		}
		if err := s.repo.AppendFiring(txCtx, reminder.Firing{
			ID:         s.idGen.NewULID(),
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
			"organization_id":   r.OrganizationID(),
			"creator_ref":       r.CreatorRef(),
			"content":           r.Content(),
			"fired_at":          now.UTC().Format(time.RFC3339Nano),
			"fired_count":       r.FiredCount(),
		}
		// fired → observability audit (events table). This is the audit trail,
		// NOT the delivery channel.
		if _, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: EventReminderFired, Refs: refs, Actor: "system", Payload: payload,
		}); err != nil {
			return err
		}
		// fired → outbox (drives the ReminderDeliveryProjector). The delivery
		// projector drains the OUTBOX, not the events table, so without this
		// Append the remindee is never delivered/woken (F1 ship-blocker). Same
		// "Sink.Emit audit + outbox.Append projection" double-write the agent/pm/
		// conversation services already use; both sit in the aggregate's tx so the
		// fired event is atomic with the state change.
		if err := s.appendFiredOutbox(txCtx, r, now); err != nil {
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
		didFire = true
		return nil
	})
	return didFire, err
}

// firedOutboxPayload is the JSON the ReminderDeliveryProjector decodes off the
// outbox fired event (delivery_projector.go reminderFiredPayload). Keep these
// fields in sync with that decoder.
type firedOutboxPayload struct {
	ReminderID      string `json:"reminder_id"`
	RemindeeAgentID string `json:"remindee_agent_id"`
	Content         string `json:"content"`
	OrganizationID  string `json:"organization_id"`
}

// appendFiredOutbox writes the cognition.reminder.fired event to the outbox
// inside the caller's tx, so the delivery projector (which drains the outbox)
// delivers the reminder to the remindee.
func (s *ReminderScheduler) appendFiredOutbox(ctx context.Context, r *reminder.Reminder, now time.Time) error {
	pb, err := json.Marshal(firedOutboxPayload{
		ReminderID:      r.ID().String(),
		RemindeeAgentID: r.RemindeeAgentID(),
		Content:         r.Content(),
		OrganizationID:  r.OrganizationID(),
	})
	if err != nil {
		return err
	}
	rb, err := json.Marshal(map[string]string{
		"reminder_id": r.ID().String(),
		"agent_id":    r.RemindeeAgentID(),
		"project_id":  r.ProjectID(),
	})
	if err != nil {
		return err
	}
	return s.outbox.Append(ctx, outbox.Event{
		ID:        s.idGen.NewULID(),
		EventType: string(EventReminderFired),
		Refs:      string(rb),
		Payload:   string(pb),
		CreatedAt: now,
	})
}
