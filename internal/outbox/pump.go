package outbox

import (
	"context"
	"time"
)

// Pump runs a Relay as a background drainer (v2.7 B3 server-runtime wiring,
// plan §10 OQ1). It mirrors the SSE EventFanout pattern: a SINGLE goroutine
// drains the backlog once on boot, then drains on a ticker. Single-threaded by
// construction so a batch never runs concurrently with itself (projection order
// + idempotency stay simple). The ticker interval is the user-perceived upper
// bound on participant/WorkItem eventual-consistency latency — keep it short
// (seconds).
type Pump struct {
	relay     *Relay
	interval  time.Duration
	batchSize int
	onError   func(error)
	tickHooks []func(context.Context)
}

// NewPump builds a Pump. interval<=0 defaults to 1s; batchSize<=0 defaults to 100.
func NewPump(relay *Relay, interval time.Duration, batchSize int) *Pump {
	if interval <= 0 {
		interval = time.Second
	}
	if batchSize <= 0 {
		batchSize = 100
	}
	return &Pump{relay: relay, interval: interval, batchSize: batchSize}
}

// WithErrorHandler sets a callback for drain errors (the loop keeps running;
// the failed event stays unprocessed for the next pass).
func (p *Pump) WithErrorHandler(fn func(error)) *Pump {
	p.onError = fn
	return p
}

// WithTickHook registers a callback run on every tick BEFORE the drain pass — the
// reuse point for periodic scanners that emit outbox events (the Cognition
// ReminderTickProjector scans due reminders and emits cognition.reminder.fired,
// which the same tick's drain then relays). A panicking/slow hook would stall the
// loop, so hooks must be cheap + non-panicking. v2.x reminders (design §D4).
func (p *Pump) WithTickHook(fn func(context.Context)) *Pump {
	p.tickHooks = append(p.tickHooks, fn)
	return p
}

// Run drains the backlog immediately, then on each tick, until ctx is canceled.
// Blocks; call as `go pump.Run(ctx)`.
func (p *Pump) Run(ctx context.Context) {
	p.drain(ctx) // clear any backlog on boot
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, hook := range p.tickHooks {
				hook(ctx)
			}
			p.drain(ctx)
		}
	}
}

// drain repeatedly RunOnce until a pass processes nothing (backlog cleared) or
// errors. Bounded by a safety cap so a persistently-failing event can't spin.
func (p *Pump) drain(ctx context.Context) {
	for i := 0; i < 1000; i++ {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := p.relay.RunOnce(ctx, p.batchSize)
		if err != nil {
			if p.onError != nil {
				p.onError(err)
			}
			return
		}
		if n == 0 {
			return
		}
	}
}
