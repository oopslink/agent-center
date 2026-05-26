package service

import (
	"context"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/workforce"
)

// HeartbeatReconciler periodically flips workers Online → Offline
// when their last_heartbeat_at has gone stale (worker daemon crashed,
// network partitioned, machine asleep, etc.). v2.4-D-X1 B8 fix —
// without this, a worker that stopped heartbeating stayed wedged at
// `online` forever, since heartbeats are the only thing that touched
// the status field in the steady state.
//
// Edge case (per PD in #agent-center:0c9f6bb7): freshly enrolled
// workers might not have completed their first heartbeat yet by the
// time the reconciler ticks. We compare against `max(enrolled_at,
// last_heartbeat_at)` so a worker still inside its first-heartbeat
// window is never wrongly marked offline.
type HeartbeatReconciler struct {
	repo     workforce.WorkerRepository
	sink     *observability.EventSink
	clk      clock.Clock
	staleAge time.Duration
	tick     time.Duration
}

// HeartbeatReconcileDefaults captures the tunables. The default
// stale-age of 60s balances "fast offline detection" against "don't
// flag a healthy worker as offline just because the poll loop got
// caught behind a slow tick". Tick interval = 30s so we catch a
// transition within (staleAge + tick) ≤ 90s.
const (
	HeartbeatReconcileDefaultStaleAge     = 60 * time.Second
	HeartbeatReconcileDefaultTickInterval = 30 * time.Second
)

// NewHeartbeatReconciler wires the reconciler. Zero values fall back
// to the package defaults.
func NewHeartbeatReconciler(
	repo workforce.WorkerRepository,
	sink *observability.EventSink,
	clk clock.Clock,
	staleAge, tick time.Duration,
) *HeartbeatReconciler {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	if staleAge <= 0 {
		staleAge = HeartbeatReconcileDefaultStaleAge
	}
	if tick <= 0 {
		tick = HeartbeatReconcileDefaultTickInterval
	}
	return &HeartbeatReconciler{repo: repo, sink: sink, clk: clk, staleAge: staleAge, tick: tick}
}

// Run blocks until ctx is done, calling Tick on the configured cadence.
// Errors per tick are logged via the sink (as
// `workforce.worker.reconcile_failed`) so a failed scan doesn't kill
// the loop.
func (r *HeartbeatReconciler) Run(ctx context.Context, actor observability.Actor) error {
	t := time.NewTicker(r.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := r.Tick(ctx, actor); err != nil && r.sink != nil {
				_, _ = r.sink.Emit(ctx, observability.EmitCommand{
					EventType: "workforce.worker.reconcile_failed",
					Actor:     actor,
					Payload: map[string]any{
						"reason":  "reconcile_failed",
						"message": err.Error(),
					},
				})
			}
		}
	}
}

// Tick scans every online worker once. Exposed so tests can step the
// reconciler deterministically without spinning a goroutine.
func (r *HeartbeatReconciler) Tick(ctx context.Context, actor observability.Actor) error {
	workers, err := r.repo.FindByStatus(ctx, workforce.WorkerOnline)
	if err != nil {
		return err
	}
	now := r.clk.Now()
	cutoff := now.Add(-r.staleAge)
	for _, w := range workers {
		// PM-requested edge case: workers freshly enrolled but not yet
		// heartbeated should NOT be marked offline during their first-
		// heartbeat window. Compare against max(enrolled_at, last_hb).
		anchor := w.EnrolledAt()
		if hb := w.LastHeartbeatAt(); hb != nil && hb.After(anchor) {
			anchor = *hb
		}
		if !anchor.Before(cutoff) {
			continue // still inside the live window
		}
		// Persist via UpdateStatus (CAS on version) — Save() is
		// INSERT-only and would fail with already-exists. We don't
		// need MarkOffline's full payload (reason/message) in the
		// row right now; the SSE event below carries the why.
		if err := r.repo.UpdateStatus(ctx, w.ID(), workforce.WorkerOnline, workforce.WorkerOffline, w.Version()); err != nil {
			return err
		}
		if r.sink != nil {
			_, _ = r.sink.Emit(ctx, observability.EmitCommand{
				EventType: "workforce.worker.offline",
				Refs:      observability.EventRefs{WorkerID: string(w.ID())},
				Actor:     actor,
				Payload: map[string]any{
					"worker_id":     string(w.ID()),
					"reason":        string(workforce.OfflineReasonHeartbeatTimeout),
					"last_seen_at":  anchor.UTC().Format(time.RFC3339Nano),
					"stale_for":     now.Sub(anchor).String(),
					"transition_at": now.UTC().Format(time.RFC3339Nano),
				},
			})
		}
	}
	return nil
}
