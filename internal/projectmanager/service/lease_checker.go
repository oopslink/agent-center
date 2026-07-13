package service

import (
	"context"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// lease_checker.go (v2.14.0 I14/F3 §2.5/§13.D; reworked by T456 / issue-21ba5b78
// I30) — the execution-lease watcher. An agent holds a running Task by a lease
// (StartTask grants it; the MCP heartbeat AND the worker-daemon's process-alive
// auto-renew extend it). If the lease lapses, this background checker NO LONGER
// reclaims the task.
//
// T456 (issue-21ba5b78/I30): the old behavior (Task.ExpireLease → open + assignee
// cleared) silently dropped a live agent's work and stranded structured nodes in a
// claimable=false orphan state. The lease is now a NUDGE trigger, not a reclaim
// trigger: a lapsed lease renews itself and @-nudges the SAME assignee in the task
// conversation (NudgeExpiredLeases → EvtTaskLeaseExpiredNudge → WakeProjector) so the
// owner is woken to continue. The task never leaves running and the assignee never
// changes. A truly-dead process is recovered by the worker-daemon self-heal (relaunch
// the SAME owner), not by reclaim/redispatch here.
//
// §13.D bound: a BLOCKED task is a LEGAL pause and is NEVER nudged by the lease
// (Task.NudgeOnLeaseExpiry no-ops on a blocked task) — it is handled by the §13.D
// overdue-block reminder instead.

// LeaseCheckDefaultTick is the cadence the lease checker sweeps at. A lapsed lease is
// nudged within (tick) of lapsing. It is well under DefaultExecutionLeaseTTL so a
// stalled agent is nudged promptly, but not so tight that a briefly-paused renew
// thrashes (the renew interval must stay < TTL).
const LeaseCheckDefaultTick = 1 * time.Minute

// NudgeExpiredLeases scans every running Task and NUDGES those whose execution lease
// has lapsed (T456): Task.NudgeOnLeaseExpiry renews the lease + logs lease_nudge
// (status/assignee unchanged), and the checker emits EvtTaskLeaseExpiredNudge so the
// WakeProjector wakes the SAME owner. Each nudge runs in its own tx with a fresh
// re-read, so a lease renewed (heartbeat / worker auto-renew) or a block recorded
// between the scan and the tx makes NudgeOnLeaseExpiry a safe no-op (it re-checks the
// row's current state). Renewing the lease rate-limits re-nudges to once per TTL.
// Returns the number nudged. A read-mostly sweep: tasks with a live/absent lease are
// skipped without a write.
func (s *Service) NudgeExpiredLeases(ctx context.Context) (int, error) {
	now := s.clock.Now()
	running, err := s.tasks.ListByStatuses(ctx, []pm.TaskStatus{pm.TaskRunning})
	if err != nil {
		return 0, err
	}
	// Prune confirmed-dead trackers for tasks no longer running (issue-6ff12523): a task
	// that left running is no longer a stuck-node candidate, so its verdict/reopen tally
	// resets.
	runningSet := make(map[pm.TaskID]struct{}, len(running))
	for _, snap := range running {
		runningSet[snap.ID()] = struct{}{}
	}
	s.pruneStuckTrackers(runningSet)

	nudged := 0
	for _, snap := range running {
		taskID := snap.ID()
		// Lease snapshot read at the TOP of the sweep — BEFORE this sweep's own nudge — so
		// the confirmed-dead activity check compares against the value the previous sweep
		// left (post its nudge), never counting our own nudge as agent activity.
		expBefore := snap.ExecutionLeaseExpiresAt()

		// Confirmed-dead accounting + auto-reconcile (issue-6ff12523): folds this
		// observation into the per-node tracker and, once confident, reopens the wedged
		// graph node (or blocks for triage past R_max). A node reopened/blocked this sweep
		// is `acted` — it is NOT also nudged (a block cleared its lease; a reopen emitted
		// its own re-dispatch wake).
		acted, aerr := s.reconcileStuckNode(ctx, snap, expBefore, now)
		if aerr != nil {
			return nudged, aerr
		}

		// T456 lease NUDGE (unchanged): only tasks with a lapsed lease are candidates
		// (NudgeOnLeaseExpiry no-ops on the rest); the authoritative re-check is in the tx.
		if !acted && leaseLapsed(expBefore, now) {
			var did bool
			if err := s.runInTx(ctx, func(txCtx context.Context) error {
				t, ferr := s.tasks.FindByID(txCtx, taskID)
				if ferr != nil {
					return ferr
				}
				fired, nerr := t.NudgeOnLeaseExpiry(DefaultExecutionLeaseTTL, now)
				if nerr != nil {
					return nerr
				}
				// No-op when the lease was renewed, the task is blocked, or it is no longer
				// running — nothing to persist or nudge.
				if !fired {
					return nil
				}
				if uerr := s.tasks.Update(txCtx, t); uerr != nil {
					return uerr
				}
				// §7.3: persist the "lease_nudge" lifecycle log entry.
				if lerr := s.flushActionLogs(txCtx, t); lerr != nil {
					return lerr
				}
				did = true
				// Sanctioned system→agent wake: the WakeProjector posts the @assignee nudge
				// into the task conversation + wakes the owner. NOT emitTaskStateChanged —
				// the task did not change state (still running, same assignee).
				return s.emitTaskLeaseExpiredNudge(txCtx, t)
			}); err != nil {
				return nudged, err
			}
			if did {
				nudged++
			}
		}

		// Anchor the tracker's lastExp to the lease value in effect AFTER this sweep's
		// nudge (issue-6ff12523), so the NEXT sweep's activity check discounts our own
		// nudge. No-op (and no re-read) for an untracked / just-acted task.
		if !acted && s.isStuckTracked(taskID) {
			if cur, cerr := s.currentLeaseExp(ctx, taskID); cerr != nil {
				return nudged, cerr
			} else {
				s.recordStuckLeaseExp(taskID, cur)
			}
		}
	}
	return nudged, nil
}

// emitTaskLeaseExpiredNudge emits EvtTaskLeaseExpiredNudge (T456) inside the caller's
// tx, refs keyed by task+project so the outbox row carries the task locus. The
// WakeProjector consumes it to nudge + wake the SAME assignee.
func (s *Service) emitTaskLeaseExpiredNudge(ctx context.Context, t *pm.Task) error {
	return s.emit(ctx, EvtTaskLeaseExpiredNudge,
		refsJSON(map[string]string{"task_id": string(t.ID()), "project_id": string(t.ProjectID())}),
		taskLeaseExpiredNudgePayload{
			TaskID:      string(t.ID()),
			ProjectID:   string(t.ProjectID()),
			OwnerRef:    "pm://tasks/" + string(t.ID()),
			AssigneeRef: string(t.Assignee()),
		})
}

// LeaseChecker is the background loop that periodically calls NudgeExpiredLeases
// (T456). It mirrors the AutoRedispatchReconciler wiring shape: a
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
	return c.svc.NudgeExpiredLeases(ctx)
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
				c.log("lease-checker: nudged %d lapsed-lease task(s)", n)
			}
		}
	}
}
