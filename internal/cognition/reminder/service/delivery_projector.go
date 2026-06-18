package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/oopslink/agent-center/internal/cognition/reminder"
	"github.com/oopslink/agent-center/internal/outbox"
)

// ReminderDeliverer is the narrow port the delivery projector uses to wake the
// remindee (§3.4): post the reminder content as a system directed message to the
// remindee agent's conversation. The CONCRETE implementation (wired in cli) does
// the conversation-domain work — resolve/ensure the remindee's DM and call the
// conversation MessageWriter.AddMessage with a system sender — and the EXISTING
// WakeProjector turns that message into agent.wake. Keeping it a port keeps this
// projector unit-testable and free of the conversation/wake machinery.
//
// ANTI-LOOP (§3.3 / Cognition invariant #5): delivery posts a normal directed
// message that wakes ONLY the remindee; cognition.reminder.fired is NEVER added
// to the supervisor self-wake allowlist, so a reminder cannot spiral.
type ReminderDeliverer interface {
	// Deliver posts the reminder content to the remindee agent's conversation,
	// org-scoped (orgID drives the conversation's org).
	Deliver(ctx context.Context, orgID, remindeeAgentID, content, reminderID string) error
}

// reminderFiredPayload is the subset of the fired event payload the projector needs.
type reminderFiredPayload struct {
	ReminderID      string `json:"reminder_id"`
	RemindeeAgentID string `json:"remindee_agent_id"`
	Content         string `json:"content"`
	OrganizationID  string `json:"organization_id"`
	FiringID        string `json:"firing_id"`
}

// FiringMarker resolves a fired firing's final outcome once delivery completes
// (pending → delivered). A narrow port keeps the projector unit-testable; the
// sqlite ReminderRepo satisfies it.
type FiringMarker interface {
	UpdateFiringOutcome(ctx context.Context, firingID string, outcome reminder.FiringOutcome) error
}

// ReminderDeliveryProjector consumes cognition.reminder.fired and delivers the
// reminder to the remindee (§3.4). It is an outbox.Projector; the Relay guards
// (projector, event) idempotency (at-least-once delivery). Non-fired events are
// ignored. A delivery error leaves the event unprocessed for retry next pass.
// After a successful delivery it resolves the firing pending→delivered so the
// recorded outcome reflects reality (and skip_if_overlap no longer sees overlap).
type ReminderDeliveryProjector struct {
	deliverer ReminderDeliverer
	firings   FiringMarker
}

// NewReminderDeliveryProjector wires the projector to a deliverer and the firing
// marker that records the delivered outcome.
func NewReminderDeliveryProjector(deliverer ReminderDeliverer, firings FiringMarker) *ReminderDeliveryProjector {
	return &ReminderDeliveryProjector{deliverer: deliverer, firings: firings}
}

// compile-time check: it is an outbox projector.
var _ outbox.Projector = (*ReminderDeliveryProjector)(nil)

// Name is the stable AppliedStore key for this projector.
func (p *ReminderDeliveryProjector) Name() string { return "cognition.reminder.delivery" }

// Project delivers a fired reminder; it ignores every other event type.
func (p *ReminderDeliveryProjector) Project(ctx context.Context, e outbox.Event) error {
	if e.EventType != string(EventReminderFired) {
		return nil
	}
	var pl reminderFiredPayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return fmt.Errorf("reminder delivery: decode payload: %w", err)
	}
	if pl.RemindeeAgentID == "" {
		return fmt.Errorf("reminder delivery: event %s missing remindee_agent_id", e.ID)
	}
	if err := p.deliverer.Deliver(ctx, pl.OrganizationID, pl.RemindeeAgentID, pl.Content, pl.ReminderID); err != nil {
		return err
	}
	// Delivered → resolve the firing pending→delivered. Older events without a
	// firing_id (or a nil marker in tests) simply skip the write-back. If this
	// fails the relay retries the whole event; Deliver is at-least-once and the
	// update is idempotent, so a redelivery converges.
	if pl.FiringID != "" && p.firings != nil {
		if err := p.firings.UpdateFiringOutcome(ctx, pl.FiringID, reminder.OutcomeDelivered); err != nil {
			return fmt.Errorf("reminder delivery: mark firing %s delivered: %w", pl.FiringID, err)
		}
	}
	return nil
}
