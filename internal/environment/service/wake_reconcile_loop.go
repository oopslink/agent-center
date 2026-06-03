package service

import (
	"context"
	"time"
)

// WakeReconcileLoop runs WakeProjector.ReconcileOnce as a background job — the
// D2-e-iii POLL-FALLBACK for OQ5 wakeup ("push优先 + poll兜底"). It mirrors the
// outbox Pump / files GCLoop pattern: a SINGLE goroutine runs once on boot, then
// on a ticker, until ctx is canceled. Single-threaded by construction so a sweep
// never overlaps itself.
//
// Self-heal: the sweep recomputes each waiting_input agent's unread from its
// read-state cursor and enqueues any pending agent.wake batch INDEPENDENT of
// whether a push wake signal was ever emitted. It uses the SAME batch key as the
// push path, so push/poll converge and the ControlLog dedups duplicates.
//
// Slow cadence: the cursor-based recompute makes the tick interval non-critical
// (60s default); it is a safety net, not the primary latency path.
type WakeReconcileLoop struct {
	proj     *WakeProjector
	interval time.Duration
	logger   func(string)
}

// NewWakeReconcileLoop builds the loop. interval<=0 defaults to 60s.
func NewWakeReconcileLoop(proj *WakeProjector, interval time.Duration, logger func(string)) *WakeReconcileLoop {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &WakeReconcileLoop{proj: proj, interval: interval, logger: logger}
}

// Run executes one reconcile sweep immediately, then on each tick, until ctx is
// canceled. Blocks; call as `go loop.Run(ctx)`. Never panics — a sweep error is
// logged and the loop keeps running (the failed work retries on the next tick).
func (l *WakeReconcileLoop) Run(ctx context.Context) {
	l.runOnce(ctx)
	t := time.NewTicker(l.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			l.runOnce(ctx)
		}
	}
}

func (l *WakeReconcileLoop) runOnce(ctx context.Context) {
	if l.proj == nil {
		return
	}
	if err := l.proj.ReconcileOnce(ctx); err != nil {
		if l.logger != nil {
			l.logger("wake reconcile loop: " + err.Error())
		}
	}
}
