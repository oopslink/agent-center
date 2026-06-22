package service

import (
	"context"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// lease_checker.go (v2.14.0 I14/F3 §2.5/§13.D) — the execution-lease reclaimer, the
// replacement for the deleted AgentWorkItem.FailFromAgentDeath stale sweep. An agent
// holds a running Task by a heartbeat-renewed lease (StartTask grants it,
// HeartbeatTask extends it). If the agent process dies, the lease stops being
// renewed and lapses; this background checker reclaims the task — returning it to
// `open` with the assignee cleared (Task.ExpireLease) so the PM can re-dispatch it.
//
// §13.D bound: a BLOCKED task is a LEGAL pause and is NEVER reclaimed by the lease
// (Task.ExpireLease no-ops on a blocked task). A blocked agent that then crashes is
// handled by the §13.D overdue-reminder + owner reassign/discard escape hatch, not
// by lease expiry.

// LeaseCheckDefaultTick is the cadence the lease checker sweeps at. A lapsed lease is
// reclaimed within (tick) of lapsing. It is well under DefaultExecutionLeaseTTL so a
// dead agent's task is freed promptly, but not so tight that a briefly-paused
// heartbeat thrashes (the heartbeat interval must stay < TTL).
const LeaseCheckDefaultTick = 1 * time.Minute

// ReclaimExpiredLeases scans every running Task and reclaims those whose execution
// lease has lapsed (Task.ExpireLease → open, assignee cleared, lease_expired logged).
// Each reclaim runs in its own tx with a fresh re-read, so a lease renewed (heartbeat)
// or a block recorded between the scan and the tx makes ExpireLease a safe no-op
// (it re-checks under the row's current state). Returns the number actually reclaimed.
// A read-mostly sweep: tasks with a live or absent lease are skipped without a write.
func (s *Service) ReclaimExpiredLeases(ctx context.Context) (int, error) {
	now := s.clock.Now()
	running, err := s.tasks.ListByStatuses(ctx, []pm.TaskStatus{pm.TaskRunning})
	if err != nil {
		return 0, err
	}
	reclaimed := 0
	for _, snap := range running {
		// Cheap pre-filter on the snapshot: only tasks with a lapsed lease are
		// candidates (ExpireLease would no-op on the rest). The authoritative re-check
		// happens inside the tx below.
		exp := snap.ExecutionLeaseExpiresAt()
		if exp == nil || now.Before(*exp) {
			continue
		}
		taskID := snap.ID()
		var did bool
		if err := s.runInTx(ctx, func(txCtx context.Context) error {
			t, ferr := s.tasks.FindByID(txCtx, taskID)
			if ferr != nil {
				return ferr
			}
			prevStatus := t.Status()
			if eerr := t.ExpireLease(now); eerr != nil {
				return eerr
			}
			// ExpireLease no-ops (status unchanged) when the lease was renewed, the
			// task was blocked, or it is no longer running — nothing to persist.
			if t.Status() == prevStatus {
				return nil
			}
			if uerr := s.tasks.Update(txCtx, t); uerr != nil {
				return uerr
			}
			// §7.3: persist the "lease_expired" lifecycle log entry.
			if lerr := s.flushActionLogs(txCtx, t); lerr != nil {
				return lerr
			}
			did = true
			return s.emitTaskStateChanged(txCtx, t, prevStatus, "execution lease expired")
		}); err != nil {
			return reclaimed, err
		}
		if did {
			reclaimed++
		}
	}
	return reclaimed, nil
}

// LeaseChecker is the background loop that periodically calls ReclaimExpiredLeases
// (v2.14.0 I14/F3 §2.5). It mirrors the AutoRedispatchReconciler wiring shape: a
// ticker-driven Run(ctx) that stops on ctx cancel, plus an explicit Tick for tests.
type LeaseChecker struct {
	svc  *Service
	clk  clock.Clock
	tick time.Duration
	log  func(string, ...any)
}

// NewLeaseChecker wires the checker. Zero tick → LeaseCheckDefaultTick; nil clk →
// system clock; nil log → no-op.
func NewLeaseChecker(svc *Service, clk clock.Clock, tick time.Duration, log func(string, ...any)) *LeaseChecker {
	if tick <= 0 {
		tick = LeaseCheckDefaultTick
	}
	if clk == nil {
		clk = clock.SystemClock{}
	}
	if log == nil {
		log = func(string, ...any) {}
	}
	return &LeaseChecker{svc: svc, clk: clk, tick: tick, log: log}
}

// Tick runs one sweep. Exposed for tests + the boot reconcile.
func (c *LeaseChecker) Tick(ctx context.Context) (int, error) {
	return c.svc.ReclaimExpiredLeases(ctx)
}

// Run sweeps every tick until ctx is canceled (the long-lived server goroutine).
func (c *LeaseChecker) Run(ctx context.Context) error {
	t := time.NewTicker(c.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if n, err := c.Tick(ctx); err != nil {
				c.log("lease-checker: tick failed: %v", err)
			} else if n > 0 {
				c.log("lease-checker: reclaimed %d expired-lease task(s)", n)
			}
		}
	}
}
