package service

import (
	"context"
	"time"
)

// GCLoop runs RunGCOnce as a background job (v2.7 D3-c server-runtime wiring).
// It mirrors the outbox Pump / SSE EventFanout pattern: a SINGLE goroutine runs
// once on boot, then on a ticker, until ctx is canceled. Single-threaded by
// construction so a GC pass never overlaps itself. The 7-day default grace makes
// the tick interval non-critical (an hour is plenty).
type GCLoop struct {
	svc      *Service
	grace    time.Duration
	interval time.Duration
	onError  func(error)
}

// NewGCLoop builds a GCLoop. grace<=0 defaults to DefaultGCGrace (7d);
// interval<=0 defaults to 1h.
func NewGCLoop(svc *Service, grace, interval time.Duration) *GCLoop {
	if grace <= 0 {
		grace = DefaultGCGrace
	}
	if interval <= 0 {
		interval = time.Hour
	}
	return &GCLoop{svc: svc, grace: grace, interval: interval}
}

// WithErrorHandler sets a callback for GC pass errors (the loop keeps running;
// the failed work is retried on the next tick).
func (g *GCLoop) WithErrorHandler(fn func(error)) *GCLoop {
	g.onError = fn
	return g
}

// Run executes one GC pass immediately, then on each tick, until ctx is
// canceled. Blocks; call as `go loop.Run(ctx)`.
func (g *GCLoop) Run(ctx context.Context) {
	g.runOnce(ctx)
	t := time.NewTicker(g.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			g.runOnce(ctx)
		}
	}
}

func (g *GCLoop) runOnce(ctx context.Context) {
	if _, err := g.svc.RunGCOnce(ctx, g.grace); err != nil {
		if g.onError != nil {
			g.onError(err)
		}
	}
}
