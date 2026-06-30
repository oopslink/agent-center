package service

import (
	"context"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

// activity_event_gc.go (incident 2026-06-30) — the periodic GC for the
// agent_activity_events telemetry log. The table is append-only (one row per agent
// activity event) and had NO GC, unlike worker_control_events which got one in T340.
// It grew unbounded (~354k rows / ~598MB over 26 days), bloating the whole DB to 691MB
// and causing SQLITE_BUSY write-lock contention that stalled the control-flow engine.
// This sweep range-deletes rows older than a retention window (default 7 days).
//
// SAFETY: agent_activity_events is pure observability telemetry with NO replay/ack
// contract (the contrast with worker_control_events, whose GC must never drop an
// un-acked command). Nothing downstream re-reads a pruned raw event: the agent-detail
// activity feed is a live/recent view, and the usage rollup keeps its own aggregates in
// agent_activity_daily. So straight age-based retention is safe — no per-row gate needed.

// ActivityEventGCRepo is the narrow repo surface the GC needs — the batched, age-gated
// retention delete. The sqlite ActivityEventRepo implements it.
type ActivityEventGCRepo interface {
	// DeleteOlderThan deletes up to `limit` events with occurred_at before `cutoff`,
	// returning the count deleted; the caller loops until it returns < limit.
	DeleteOlderThan(ctx context.Context, cutoff time.Time, limit int) (int64, error)
}

const (
	// DefaultActivityEventRetention is the default age past which an activity event is
	// pruned. 7 days keeps the agent-detail feed useful while bounding growth; the raw
	// log is telemetry, the durable aggregates live in agent_activity_daily.
	DefaultActivityEventRetention = 7 * 24 * time.Hour
	// DefaultActivityEventGCInterval is the default sweep cadence. The multi-day
	// retention makes the tick non-critical — hourly is plenty.
	DefaultActivityEventGCInterval = time.Hour
	// activityEventGCBatch bounds one DELETE so a large backlog never locks the table in
	// a single big transaction; the tick loops batches until the backlog drains.
	activityEventGCBatch = 500
	// activityEventGCMaxBatchesPerTick caps the work one tick will do (defense against a
	// pathological backlog monopolizing the goroutine); the remainder is picked up next
	// tick. 500 * 2000 = 1M rows/tick headroom.
	activityEventGCMaxBatchesPerTick = 2000
)

// ActivityEventGC prunes the agent-activity telemetry log on a retention window. It
// mirrors ControlEventGC: a ticker-driven Run(ctx) that stops on ctx cancel, plus an
// explicit Tick for tests. Single goroutine by construction (a pass never overlaps
// itself).
type ActivityEventGC struct {
	repo      ActivityEventGCRepo
	clk       clock.Clock
	retention time.Duration
	interval  time.Duration
	batch     int
	log       func(string, ...any)
}

// NewActivityEventGC wires the GC. retention<=0 → DefaultActivityEventRetention;
// interval<=0 → DefaultActivityEventGCInterval; nil clk → system clock; nil log → no-op.
func NewActivityEventGC(repo ActivityEventGCRepo, clk clock.Clock, retention, interval time.Duration, log func(string, ...any)) *ActivityEventGC {
	if retention <= 0 {
		retention = DefaultActivityEventRetention
	}
	if interval <= 0 {
		interval = DefaultActivityEventGCInterval
	}
	if clk == nil {
		clk = clock.SystemClock{}
	}
	if log == nil {
		log = func(string, ...any) {}
	}
	return &ActivityEventGC{repo: repo, clk: clk, retention: retention, interval: interval, batch: activityEventGCBatch, log: log}
}

// Tick runs one full GC pass: it deletes events older than (now - retention) in bounded
// batches until the backlog drains (or the per-tick batch cap is hit). Returns the total
// rows deleted this pass. Exposed for tests + the boot pass.
func (g *ActivityEventGC) Tick(ctx context.Context) (int64, error) {
	cutoff := g.clk.Now().Add(-g.retention)
	var total int64
	for i := 0; i < activityEventGCMaxBatchesPerTick; i++ {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, err := g.repo.DeleteOlderThan(ctx, cutoff, g.batch)
		if err != nil {
			return total, err
		}
		total += n
		if n < int64(g.batch) {
			break // drained this pass
		}
	}
	return total, nil
}

// Run executes one GC pass immediately, then on each tick, until ctx is canceled.
// Blocks; call as `go gc.Run(ctx)`.
func (g *ActivityEventGC) Run(ctx context.Context) {
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

func (g *ActivityEventGC) runOnce(ctx context.Context) {
	n, err := g.Tick(ctx)
	if err != nil {
		g.log("activity-event gc: tick failed: %v", err)
		return
	}
	if n > 0 {
		g.log("activity-event gc: pruned %d activity event(s) older than %s", n, g.retention)
	}
}
