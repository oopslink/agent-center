package service

import (
	"context"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

// control_event_gc.go (T340, issue-b71ee81f) — the periodic GC for the
// worker_control_events command stream. The stream is append-only for every command
// type (agent.reconcile / agent.converse / agent.work_available, incl. the T335
// session-heal sweep's epoch-key rows) and had NO GC, so it grew monotonically
// forever. This sweep range-deletes rows older than a retention window (default 3
// days, configurable) that the owning worker has already ACKED.
//
// SAFETY (the second net the sweep's own footnote flagged): the GC NEVER deletes an
// un-acked row (offset > worker.last_acked_offset). On reconnect a worker replays
// CommandsAfter(last_acked) — so an un-acked command (e.g. an agent.converse queued
// to a worker that then went offline) must survive even past retention. Already-acked
// rows are dead weight: the desired lifecycle/work state is re-derived on reconnect
// (ResumeState boot-reconcile + the server work_available sweep), never replayed from
// the acked tail. Orphan rows whose worker no longer exists are pruned by time alone.

// ControlEventGCRepo is the narrow repo surface the GC needs — the batched,
// ack-gated retention delete. The sqlite ControlEventRepo implements it.
type ControlEventGCRepo interface {
	// DeleteAckedBefore deletes up to `limit` acked rows created before `cutoff`,
	// returning the count deleted; the caller loops until it returns < limit.
	DeleteAckedBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error)
}

const (
	// DefaultControlEventRetention is the default age past which an acked control
	// event is pruned. oopslink ruling (T340): keep 3 days. Overridable via wiring.
	DefaultControlEventRetention = 3 * 24 * time.Hour
	// DefaultControlEventGCInterval is the default sweep cadence. The multi-day
	// retention makes the tick non-critical — hourly is plenty.
	DefaultControlEventGCInterval = time.Hour
	// controlEventGCBatch bounds one DELETE so a large backlog never locks the table
	// in a single big transaction; the tick loops batches until the backlog drains.
	controlEventGCBatch = 500
	// controlEventGCMaxBatchesPerTick caps the work one tick will do (defense against
	// a pathological backlog monopolizing the goroutine); the remainder is picked up
	// next tick. 500 * 2000 = 1M rows/tick headroom.
	controlEventGCMaxBatchesPerTick = 2000
)

// ControlEventGC prunes the control-event stream on a retention window. It mirrors
// the pm LeaseChecker shape: a ticker-driven Run(ctx) that stops on ctx cancel, plus
// an explicit Tick for tests. Single goroutine by construction (a pass never overlaps
// itself).
type ControlEventGC struct {
	repo      ControlEventGCRepo
	clk       clock.Clock
	retention time.Duration
	interval  time.Duration
	batch     int
	log       func(string, ...any)
}

// NewControlEventGC wires the GC. retention<=0 → DefaultControlEventRetention;
// interval<=0 → DefaultControlEventGCInterval; nil clk → system clock; nil log → no-op.
func NewControlEventGC(repo ControlEventGCRepo, clk clock.Clock, retention, interval time.Duration, log func(string, ...any)) *ControlEventGC {
	if retention <= 0 {
		retention = DefaultControlEventRetention
	}
	if interval <= 0 {
		interval = DefaultControlEventGCInterval
	}
	if clk == nil {
		clk = clock.SystemClock{}
	}
	if log == nil {
		log = func(string, ...any) {}
	}
	return &ControlEventGC{repo: repo, clk: clk, retention: retention, interval: interval, batch: controlEventGCBatch, log: log}
}

// Tick runs one full GC pass: it deletes acked rows older than (now - retention) in
// bounded batches until the backlog drains (or the per-tick batch cap is hit).
// Returns the total rows deleted this pass. Exposed for tests + the boot pass.
func (g *ControlEventGC) Tick(ctx context.Context) (int64, error) {
	cutoff := g.clk.Now().Add(-g.retention)
	var total int64
	for i := 0; i < controlEventGCMaxBatchesPerTick; i++ {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, err := g.repo.DeleteAckedBefore(ctx, cutoff, g.batch)
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
func (g *ControlEventGC) Run(ctx context.Context) {
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

func (g *ControlEventGC) runOnce(ctx context.Context) {
	n, err := g.Tick(ctx)
	if err != nil {
		g.log("control-event gc: tick failed: %v", err)
		return
	}
	if n > 0 {
		g.log("control-event gc: pruned %d acked control event(s) older than %s", n, g.retention)
	}
}
