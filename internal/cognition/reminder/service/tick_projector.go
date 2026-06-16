package service

import (
	"context"

	"github.com/oopslink/agent-center/internal/clock"
)

// ReminderTickProjector drives the ReminderScheduler off the outbox Pump tick
// (§3.3 / §D4): register OnTick with pump.WithTickHook so every 1s tick scans for
// due reminders and fires them. Reusing the existing ticker avoids a second cron
// framework. Errors are routed to onError (the loop must not panic/stall).
type ReminderTickProjector struct {
	scheduler *ReminderScheduler
	clk       clock.Clock
	onError   func(error)
}

// NewReminderTickProjector wires the projector. onError may be nil.
func NewReminderTickProjector(scheduler *ReminderScheduler, clk clock.Clock, onError func(error)) *ReminderTickProjector {
	return &ReminderTickProjector{scheduler: scheduler, clk: clk, onError: onError}
}

// OnTick scans + fires due reminders at clk.Now(). Safe to pass to
// pump.WithTickHook. It never panics; a scan error is reported via onError and
// the next tick retries (FindDue is idempotent — only active/due reminders fire).
func (p *ReminderTickProjector) OnTick(ctx context.Context) {
	if _, err := p.scheduler.Tick(ctx, p.clk.Now()); err != nil && p.onError != nil {
		p.onError(err)
	}
}
