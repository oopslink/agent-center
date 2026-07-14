package service

import (
	"context"
	"fmt"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

// retainedWorktreeNote surfaces a RETAINED committed-but-unpushed worktree in a triage-block
// reason (issue-f30b7e7b D2 / P1-C). When the last executor's reported delivery shows work
// that was committed but never pushed (Probed && !Pushed && (ahead>0 || dirty)), its worktree
// was kept — so a human can push that branch or investigate before discarding, and the commit
// is not silently lost. "" when there is nothing retained worth pushing (pushed / non-git /
// nothing committed).
func retainedWorktreeNote(d *pm.Delivery) string {
	if d == nil || !d.Probed || d.Pushed {
		return ""
	}
	if d.AheadOfBase <= 0 && !d.Dirty {
		return ""
	}
	branch := d.Branch
	if branch == "" {
		branch = "(detached)"
	}
	return fmt.Sprintf(" — NOTE: the last executor left committed-but-unpushed work on branch %s (HEAD %s, ahead %d) whose worktree was RETAINED; a human can push that branch or inspect it before discarding, so the commit is not lost.", branch, d.HeadSHA, d.AheadOfBase)
}

// copyTimePtr returns a defensive copy of a time pointer (nil-safe), so a tracker never
// aliases an aggregate's internal pointer.
func copyTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

// stuck_node_reconcile.go (issue-6ff12523) — the THIRD auto trigger for the shared
// reopenStuckPlanNode self-heal (issue-77d9beff ①). reset ("task_reset") and unblock
// ("task_unblock") already reopen a structured (graphed, non-builtin) plan node wedged
// Running while its executor is gone. This adds a FULLY-AUTOMATIC recovery: the periodic
// lease sweep (NudgeExpiredLeases, ~1min) accumulates a per-node "confirmed-dead"
// verdict and, once confident, reopens the node itself — no human reset/unblock needed.
//
// WHY NOT reclaim-on-lapse (the T456 / issue-21ba5b78 anti-orphan invariant): a merely
// LAPSED lease is NOT proof of death — the runtime auto-renews every 60s
// (agentruntime.DefaultLeaseRenewEvery) for as long as the PROCESS is alive, wholly
// independent of what the LLM is doing, so a live-but-heads-down agent keeps advancing
// its lease. T456 therefore turned lapse into a NUDGE (续租 + @-nudge the SAME owner),
// never a reclaim, to stop stranding live work in a claimable=false orphan state. This
// reconcile PRESERVES that: it never reclaims on lapse. It reopens ONLY after a
// multi-sweep CONFIRMED-DEAD verdict (no lease advance across N sweeps AND ≥ T_dead
// since the last observed activity), and it CLEARS the verdict the instant ANY activity
// (a genuine lease advance from a heartbeat / worker auto-renew) is observed — so a
// slow-but-alive owner is never reopened.
//
// ACTIVITY SIGNAL (why the lease-expiry value, discounting our own nudge): the ONLY
// clean liveness proxy the center-side sweep can observe is the execution lease. A LIVE
// runtime advances executionLeaseExpiresAt every ~60s; a DEAD one never does. The T456
// nudge ALSO advances the lease (by TTL) when it fires, which would masquerade as
// activity — so the tracker records the lease value AFTER this sweep's own nudge
// (lastExp) and only counts a FURTHER advance on a LATER sweep (which only an agent /
// worker renewal, running between sweeps, can produce) as real activity. A frozen lease
// across sweeps ⇒ strike; any inter-sweep advance ⇒ clear.
//
// EXECUTOR-EXIT FAST PATH (Part B) — NOT wired, by design. There is no DEFINITIVE
// executor-exit signal the center-side sweep can consume: the pm layer only sees the
// lease (a 60s-cadence liveness proxy, not a terminal-exit signal) and worker
// online/offline (worker-level presence that flaps on a transient disconnect / restart —
// exactly the false-positive T456 guards against). The runtime's OWN definitive-exit
// path already exists and already reopens the node: on boot the runtime detects a
// should-run-but-dead executor and calls ResetTask(confirmedDead=true)
// (agentruntime/boot_reconcile.go), which flows through the SAME reopenStuckPlanNode via
// the "task_reset" trigger. That path lives in internal/agentruntime (out of this task's
// scope). Rather than synthesise a signal from flappy presence, this reconcile
// implements only the confirmed-dead SLOW PATH; the fast path stays the runtime's job.

// Confirmed-dead thresholds (PD-set defaults, issue-6ff12523). Named constants so a
// future tighten/loosen is a one-line change, not a rework.
const (
	// StuckNodeDeadStrikeThreshold (N) — consecutive no-activity sweeps required before
	// a lapsed-lease node is even a death candidate. At the ~1min sweep cadence this is
	// ~N minutes of a frozen lease.
	StuckNodeDeadStrikeThreshold = 3

	// StuckNodeDeadTimeout (T_dead) — minimum wall time since the last observed activity
	// before a struck-out node is declared confirmed-dead. Gates the strike count so a
	// fast sweep cadence cannot rush the verdict.
	StuckNodeDeadTimeout = 10 * time.Minute

	// StuckNodeProgressStaleTimeout (T_stale, issue-0186f85e ③) — how long a running
	// structured node's task.updated_at may stay FROZEN before the node becomes a
	// confirmed-dead CANDIDATE, INDEPENDENT of whether its execution lease has lapsed. The
	// worker-daemon's process-alive auto-renew bumps updated_at every ~60s for as long as
	// the executor PROCESS lives (decoupled from the LLM turn — see WorkerRenewLease), so a
	// ≥ T_stale freeze is ≥ ~10 missed renews: strong evidence the process is gone. This is
	// the "更快识别假 running 无进展" entry: it no longer waits out the 5h execution-lease TTL
	// (DefaultExecutionLeaseTTL) before even beginning to accrue a death verdict, cutting the
	// zombie-reclaim window from ~5h to ~T_stale + the confirm gate. A live-but-heads-down
	// agent still renews (updated_at advances), so the T456 slow-but-alive guard is preserved.
	StuckNodeProgressStaleTimeout = 10 * time.Minute

	// StuckNodeMaxAutoReopens (R_max) — per-node cap on AUTOMATIC reopens. Past it the
	// reconcile stops re-reopening (a reopen loop is a bad-task / broken-environment
	// symptom auto-recovery cannot fix) and BLOCKS the task for human triage, reusing the
	// T862 §2B reset-exhaustion circuit breaker (BlockForResetExhaustion).
	StuckNodeMaxAutoReopens = 2

	// StuckNodeReopenCooldown (T_cooldown) — minimum wait between two automatic reopens of
	// the SAME node, so a freshly re-dispatched executor gets time to pick the node up
	// before the next verdict.
	StuckNodeReopenCooldown = 10 * time.Minute
)

// auditPlanNodeAutoReconciled is the change-ledger type for an automatic stuck-node
// reopen / triage-block (issue-6ff12523). Defined locally (a plain AuditChangeType
// value) so the audit enum file stays untouched — the ledger column is a free string.
const auditPlanNodeAutoReconciled = pm.AuditChangeType("node_auto_reconciled")

// stuckNodeTracker is the per-node confirmed-dead accounting the lease sweep carries
// across ticks (in-memory; a lost tracker on a restart just restarts the count — safe,
// the node is re-observed next sweep). Keyed by task id in Service.stuckTrackers.
type stuckNodeTracker struct {
	// lastExp is the node's execution-lease expiry recorded at the END of the previous
	// sweep (AFTER that sweep's own nudge). A later value observed at the TOP of a
	// subsequent sweep is a genuine agent/worker renewal (activity); an unchanged value
	// is a frozen lease (a strike). nil = no lease last seen.
	lastExp *time.Time
	// lastUpdatedAt is the task's updated_at recorded at the END of the previous sweep
	// (AFTER that sweep's own nudge), the issue-0186f85e ③ progress signal. A LATER value
	// at the top of a subsequent sweep is real progress — a worker process-alive auto-renew
	// (or any task write) that ran BETWEEN sweeps (our own nudge's bump was folded into this
	// anchor) — and clears the verdict; an unchanged value is a strike. This is the primary
	// "有真进展" liveness signal: it advances every ~60s while the executor process lives, so
	// a frozen updated_at (unlike a still-valid-but-unrenewed 5h lease) means death promptly.
	lastUpdatedAt time.Time
	// strikes counts consecutive no-activity sweeps since the last observed activity.
	strikes int
	// lastActivityAt is the wall time of the last observed activity (or first-tracked
	// time). The T_dead gate measures from here.
	lastActivityAt time.Time
	// lastReopenAt is the wall time of the last automatic reopen (T_cooldown gate). Zero =
	// no automatic reopen performed this session. The AUTHORITATIVE reopen COUNT is the
	// DURABLE Task.FruitlessReopens (issue-f30b7e7b D2) — the in-memory tracker keeps only
	// transient state (strikes / cooldown), so the R_max circuit breaker survives a center
	// restart (a lost in-memory tally used to reset the count and could never trip).
	lastReopenAt time.Time
}

// stuckTrackerFor returns (creating if absent) the tracker for a task, bootstrapping a
// fresh one's activity clock to `now`. Caller holds stuckMu.
func (s *Service) stuckTrackerFor(taskID pm.TaskID, now time.Time) *stuckNodeTracker {
	if s.stuckTrackers == nil {
		s.stuckTrackers = make(map[pm.TaskID]*stuckNodeTracker)
	}
	tr, ok := s.stuckTrackers[taskID]
	if !ok {
		tr = &stuckNodeTracker{lastActivityAt: now}
		s.stuckTrackers[taskID] = tr
	}
	return tr
}

// pruneStuckTrackers drops trackers for tasks no longer in the running set — a task that
// left running (reset to open, completed, discarded) is no longer a stuck-node candidate,
// so its verdict + reopen tally reset (a later re-run starts clean).
func (s *Service) pruneStuckTrackers(running map[pm.TaskID]struct{}) {
	s.stuckMu.Lock()
	defer s.stuckMu.Unlock()
	for id := range s.stuckTrackers {
		if _, ok := running[id]; !ok {
			delete(s.stuckTrackers, id)
		}
	}
}

// leaseLapsed reports whether a lease-expiry snapshot is set and already at/after now.
func leaseLapsed(exp *time.Time, now time.Time) bool {
	return exp != nil && !now.Before(*exp)
}

// progressStale reports whether a running node's task.updated_at has been FROZEN for at
// least T_stale (issue-0186f85e ③): no worker auto-renew / task write in that window, so
// the executor process is very likely gone. A zero updatedAt (never set) is not treated
// as stale — there is no baseline to measure a freeze from.
func progressStale(updatedAt, now time.Time) bool {
	return !updatedAt.IsZero() && now.Sub(updatedAt) >= StuckNodeProgressStaleTimeout
}

// accountStuckNode folds one sweep's observation of a running task into its confirmed-dead
// tracker and returns the ACTION to take this sweep:
//   - stuckActionNone: keep observing (or the node is alive / not reconcilable).
//   - stuckActionReopen: confirmed dead, under the R_max cap and past cooldown → reopen.
//   - stuckActionBlock: confirmed dead but the R_max cap is spent → block for triage.
//
// expBefore is the task's lease expiry read at the TOP of this sweep (before the nudge).
// It is a PURE accounting step (no DB writes); the caller enacts the returned action in a
// tx and then calls recordStuckLeaseExp with the post-nudge lease so the next sweep's
// activity check is anchored correctly.
type stuckAction int

const (
	stuckActionNone stuckAction = iota
	stuckActionReopen
	stuckActionBlock
)

// persistedReopens is the DURABLE Task.FruitlessReopens tally (issue-f30b7e7b D2) — the
// authoritative reopen count the R_max circuit breaker reads, so it survives a center
// restart. The in-memory tracker only carries transient strikes/cooldown state.
func (s *Service) accountStuckNode(reconcilable bool, taskID pm.TaskID, persistedReopens int, expBefore *time.Time, updatedAt time.Time, now time.Time) stuckAction {
	s.stuckMu.Lock()
	defer s.stuckMu.Unlock()

	// Not currently a wedged structured-plan running node. This is EITHER a task that
	// was never a candidate (built-in pool / backlog / ungraphed) OR a tracked node in
	// the TRANSIENT window between an auto-reopen (node → NodeReopen) and its
	// re-dispatch back to NodeRunning. FREEZE any existing tracker (do NOT strike, do
	// NOT delete) so the reopen tally (R_max circuit breaker) survives that window — a
	// deleted tracker would reset the count every cycle and the breaker could never
	// trip. A task that genuinely LEFT running is GC'd by pruneStuckTrackers instead.
	if !reconcilable {
		return stuckActionNone
	}

	tr, tracked := s.stuckTrackers[taskID]
	if !tracked {
		// ENTRY GATE — begin tracking once EITHER death signal fires: the execution lease
		// has lapsed (the original "图节点=Running 且租约已过期" trigger) OR task.updated_at has
		// been frozen ≥ T_stale (issue-0186f85e ③ — the fast path that no longer waits out
		// the 5h lease TTL). A live node (lease in the future AND still progressing) trips
		// neither, so a healthy plan carries no trackers.
		if !leaseLapsed(expBefore, now) && !progressStale(updatedAt, now) {
			return stuckActionNone
		}
		tr = s.stuckTrackerFor(taskID, now)
		tr.lastExp = copyTimePtr(expBefore)
		tr.lastUpdatedAt = updatedAt
		// First observation — no prior sweep to compare against, so no strike yet.
		return stuckActionNone
	}

	// Activity check (the "slow-but-alive" guard, T456): EITHER a lease expiry OR an
	// updated_at LATER than the value we anchored at the end of the previous sweep can only
	// come from a genuine agent/worker renewal running BETWEEN sweeps — our own nudge's bump
	// of both was already folded into the anchors. Any such advance clears the verdict.
	// updated_at is the primary signal (it advances every ~60s while the process lives, even
	// heads-down); the lease-value check is retained so a heartbeat that renews the lease
	// still registers as activity.
	leaseAdvanced := expBefore != nil && tr.lastExp != nil && expBefore.After(*tr.lastExp)
	progressAdvanced := updatedAt.After(tr.lastUpdatedAt)
	if leaseAdvanced || progressAdvanced {
		tr.strikes = 0
		tr.lastActivityAt = now
		tr.lastExp = copyTimePtr(expBefore)
		tr.lastUpdatedAt = updatedAt
		return stuckActionNone
	}

	// No activity this sweep → another strike.
	tr.strikes++
	tr.lastExp = copyTimePtr(expBefore)
	tr.lastUpdatedAt = updatedAt

	// Not yet confident: need BOTH N strikes AND ≥ T_dead since the last activity.
	if tr.strikes < StuckNodeDeadStrikeThreshold || now.Sub(tr.lastActivityAt) < StuckNodeDeadTimeout {
		return stuckActionNone
	}

	// Confirmed dead. Enforce the reopen cooldown between successive automatic reopens
	// (transient: a reopen this session anchors lastReopenAt so a freshly re-dispatched
	// executor gets time to pick the node up before the next verdict).
	if !tr.lastReopenAt.IsZero() && now.Sub(tr.lastReopenAt) < StuckNodeReopenCooldown {
		return stuckActionNone
	}

	// Circuit breaker: past the automatic-reopen cap → hand to human triage. Reads the
	// DURABLE persisted tally (issue-f30b7e7b D2), so R_max holds across a center restart.
	if persistedReopens >= StuckNodeMaxAutoReopens {
		return stuckActionBlock
	}
	return stuckActionReopen
}

// noteStuckReopen records a completed automatic reopen: bump the reopen tally, reset the
// cooldown + activity clock, and clear the strike count so the freshly re-dispatched
// executor is judged afresh.
func (s *Service) noteStuckReopen(taskID pm.TaskID, now time.Time) {
	s.stuckMu.Lock()
	defer s.stuckMu.Unlock()
	tr := s.stuckTrackerFor(taskID, now)
	// The reopen COUNT is durable (Task.FruitlessReopens, bumped in the reopen tx); the
	// tracker only records WHEN (cooldown) and resets transient strikes so the freshly
	// re-dispatched executor is judged afresh.
	tr.lastReopenAt = now
	tr.lastActivityAt = now
	tr.strikes = 0
}

// forgetStuckNode drops a task's tracker (e.g. after a triage-block hands the node to a
// human — auto-recovery is done with it).
func (s *Service) forgetStuckNode(taskID pm.TaskID) {
	s.stuckMu.Lock()
	defer s.stuckMu.Unlock()
	delete(s.stuckTrackers, taskID)
}

// recordStuckAnchors anchors a tracked node's lastExp AND lastUpdatedAt to the values in
// effect AFTER this sweep's nudge, so the next sweep's activity check discounts our own
// nudge (which bumps both the lease and updated_at). No-op for an untracked task.
func (s *Service) recordStuckAnchors(taskID pm.TaskID, exp *time.Time, updatedAt time.Time) {
	s.stuckMu.Lock()
	defer s.stuckMu.Unlock()
	if tr, ok := s.stuckTrackers[taskID]; ok {
		tr.lastExp = copyTimePtr(exp)
		tr.lastUpdatedAt = updatedAt
	}
}

// stuckStrikeSnapshot is the read-only transient strike count a reopen audit records (the
// authoritative reopen ROUND comes from the durable Task.FruitlessReopens, read in the tx).
func (s *Service) stuckStrikeSnapshot(taskID pm.TaskID) (strikes int) {
	s.stuckMu.Lock()
	defer s.stuckMu.Unlock()
	if tr, ok := s.stuckTrackers[taskID]; ok {
		return tr.strikes
	}
	return 0
}

// isStuckReconcilable reports whether a running task is a structured (graphed, non-builtin)
// plan node currently wedged Running at the graph level — the exact population
// reopenStuckPlanNode acts on. It mirrors that helper's guards so accounting only tracks
// nodes the reconcile could actually reopen (a built-in pool / backlog / ungraphed /
// unmapped task, or a node not currently NodeRunning, is never a candidate). A legally
// BLOCKED task is excluded (a block is a legal pause — never lease-reconciled, §13.D).
func (s *Service) isStuckReconcilable(txCtx context.Context, t *pm.Task) (bool, error) {
	if t.Status() != pm.TaskRunning {
		return false, nil
	}
	if t.BlockedReason() != "" {
		return false, nil // legal pause — never reconciled by the lease
	}
	pid := t.PlanID()
	if pid == "" || t.NodeID() == "" {
		return false, nil
	}
	if s.plans == nil || s.orch == nil {
		return false, nil
	}
	p, err := s.plans.FindByID(txCtx, pid)
	if err != nil {
		return false, err
	}
	if p.IsBuiltin() || p.GraphID() == "" {
		return false, nil // built-in pool recovery is auto-assign; ungraphed has no node
	}
	n, err := s.orch.GetNode(txCtx, orch.NodeID(t.NodeID()))
	if err != nil {
		return false, err
	}
	if n == nil {
		return false, nil
	}
	return n.Status() == orch.NodeRunning, nil
}

// reconcileStuckNode folds one sweep's observation of a running task into its
// confirmed-dead tracker and enacts the verdict (issue-6ff12523). Returns acted=true when
// this sweep reopened the node or blocked it for triage (so the caller skips the T456
// nudge for it). expBefore is the task's lease expiry read at the TOP of the sweep.
func (s *Service) reconcileStuckNode(ctx context.Context, t *pm.Task, expBefore *time.Time, now time.Time) (bool, error) {
	// Cheap gate: only a plan-mapped task can be a structured-node candidate, and only a
	// task whose lease has lapsed OR that is already tracked can accrue a verdict. This
	// skips the plan+node reads for the common case (a live-lease running node).
	if t.PlanID() == "" || t.NodeID() == "" {
		s.forgetStuckNode(t.ID())
		return false, nil
	}
	// Cheap gate: skip the plan+node reads unless a death signal is possible — the lease has
	// lapsed, OR updated_at has been frozen ≥ T_stale (issue-0186f85e ③), OR the node is
	// already tracked. A healthy running node (lease live AND progressing) never gets here.
	if !leaseLapsed(expBefore, now) && !progressStale(t.UpdatedAt(), now) && !s.isStuckTracked(t.ID()) {
		return false, nil
	}
	reconcilable, err := s.isStuckReconcilable(ctx, t)
	if err != nil {
		return false, err
	}
	switch s.accountStuckNode(reconcilable, t.ID(), t.FruitlessReopens(), expBefore, t.UpdatedAt(), now) {
	case stuckActionReopen:
		if rerr := s.autoReopenStuckNode(ctx, t.ID(), now); rerr != nil {
			return false, rerr
		}
		return true, nil
	case stuckActionBlock:
		if berr := s.blockStuckNodeForTriage(ctx, t.ID(), now); berr != nil {
			return false, berr
		}
		return true, nil
	default:
		return false, nil
	}
}

// isStuckTracked reports whether a task currently has a confirmed-dead tracker.
func (s *Service) isStuckTracked(taskID pm.TaskID) bool {
	s.stuckMu.Lock()
	defer s.stuckMu.Unlock()
	_, ok := s.stuckTrackers[taskID]
	return ok
}

// currentStuckAnchors re-reads a task's live execution-lease expiry AND updated_at
// (post-sweep anchor values), so the next sweep's activity check discounts this sweep's
// own nudge on both signals (issue-0186f85e ③).
func (s *Service) currentStuckAnchors(ctx context.Context, taskID pm.TaskID) (*time.Time, time.Time, error) {
	t, err := s.tasks.FindByID(ctx, taskID)
	if err != nil {
		return nil, time.Time{}, err
	}
	return t.ExecutionLeaseExpiresAt(), t.UpdatedAt(), nil
}

// autoReopenStuckNode enacts a confirmed-dead REOPEN in its own tx: re-read the task,
// run the shared reopenStuckPlanNode (Running→NodeReopen + ClearDispatch), then emit
// EvtTaskStateChanged so the PlanOrchestratorProjector advances the plan and
// re-dispatches the reopened node (the same wake reset/unblock rely on). The task never
// leaves Running (reopen is a graph-node action) — so prevStatus == Running. An audit row
// records the automatic reopen (node/plan/strikes/round/reason). Idempotent + re-read: a
// task that stopped being reconcilable between accounting and this tx is a safe no-op.
func (s *Service) autoReopenStuckNode(ctx context.Context, taskID pm.TaskID, now time.Time) error {
	strikes := s.stuckStrikeSnapshot(taskID)
	acted := false
	if err := s.runInTx(ctx, func(txCtx context.Context) error {
		t, ferr := s.tasks.FindByID(txCtx, taskID)
		if ferr != nil {
			return ferr
		}
		ok, cerr := s.isStuckReconcilable(txCtx, t)
		if cerr != nil {
			return cerr
		}
		if !ok {
			return nil // no longer wedged (recovered / reset between sweeps) — no-op.
		}
		// Delivery-aware DURABLE reopen tally (issue-f30b7e7b D2): a run that made forward
		// progress (a valid PUSHED delivery) clears the tally; a no-delivery run increments it
		// toward the R_max circuit breaker. Persisted on the task, so the breaker survives a
		// center restart (the in-memory tracker used to reset it every restart — gap a).
		if t.Delivery().HasValidDelivery() {
			t.ClearFruitlessReopens()
		} else {
			t.NoteFruitlessReopen()
		}
		if herr := s.reopenStuckPlanNode(txCtx, t, "stuck_node_auto_reconcile"); herr != nil {
			return herr
		}
		// reopenStuckPlanNode acts on the GRAPH node (ReopenNode + ClearDispatch), not the
		// task row — so the fruitless-tally bump above must be persisted explicitly.
		if uerr := s.tasks.Update(txCtx, t); uerr != nil {
			return uerr
		}
		// Change ledger: the automatic reopen (design §5 — "事后查得到 who/why 重开的").
		s.auditPlanByID(txCtx, t.ProjectID(), t.PlanID(), auditPlanNodeAutoReconciled, pm.SystemActor("lease-checker"), map[string]any{
			"task_id":      string(t.ID()),
			"node_id":      t.NodeID(),
			"action":       "reopen",
			"dead_strikes": strikes,
			"round":        t.FruitlessReopens(),
			"reason":       "confirmed-dead: lease frozen ≥ T_dead over ≥ N sweeps (no heartbeat / worker renewal)",
		})
		acted = true
		// prevStatus == Running: reopen is a graph-node action; the task stays running.
		return s.emitTaskStateChanged(txCtx, t, pm.TaskRunning, "stuck-node auto-reconcile (confirmed dead)")
	}); err != nil {
		return err
	}
	if acted {
		s.noteStuckReopen(taskID, now)
	}
	return nil
}

// blockStuckNodeForTriage trips the circuit breaker in its own tx: a node whose automatic
// reopens have hit R_max is BLOCKED for human triage instead of being reopened again,
// REUSING the T862 §2B reset-exhaustion mechanism (BlockForResetExhaustion → obstacle
// block, lease cleared, DISTINCT reset_exhausted log). A reopen loop is a bad-task /
// broken-environment symptom auto-recovery cannot fix, so it hands to a PD (who then
// unblocks — which reopens the node via the "task_unblock" trigger — or discards). Emits
// EvtTaskStateChanged (running→running annotation) + an audit row, then forgets the
// tracker (auto-recovery is done with the node). Re-read + idempotent.
func (s *Service) blockStuckNodeForTriage(ctx context.Context, taskID pm.TaskID, now time.Time) error {
	strikes := s.stuckStrikeSnapshot(taskID)
	blocked := false
	if err := s.runInTx(ctx, func(txCtx context.Context) error {
		t, ferr := s.tasks.FindByID(txCtx, taskID)
		if ferr != nil {
			return ferr
		}
		ok, cerr := s.isStuckReconcilable(txCtx, t)
		if cerr != nil {
			return cerr
		}
		if !ok {
			return nil // recovered / already blocked / reset between sweeps — no-op.
		}
		reason := "stuck-node auto-reconcile exhausted: auto-reopened R_max times, executor still confirmed dead — needs PD triage (task bad → discard, or environment broken → fix + redispatch)"
		// P1-C (issue-f30b7e7b D2): if the last executor left committed-but-unpushed work, its
		// worktree was RETAINED — point the human at the branch so they can push it or
		// investigate before discarding, rather than silently losing the commit.
		reason += retainedWorktreeNote(t.Delivery())
		if berr := t.BlockForResetExhaustion(reason, now); berr != nil {
			return berr
		}
		if uerr := s.tasks.Update(txCtx, t); uerr != nil {
			return uerr
		}
		if lerr := s.flushActionLogs(txCtx, t); lerr != nil {
			return lerr
		}
		s.auditPlanByID(txCtx, t.ProjectID(), t.PlanID(), auditPlanNodeAutoReconciled, pm.SystemActor("lease-checker"), map[string]any{
			"task_id":      string(t.ID()),
			"node_id":      t.NodeID(),
			"action":       "block_for_triage",
			"dead_strikes": strikes,
			"reopens":      t.FruitlessReopens(),
			"reason":       reason,
		})
		blocked = true
		return s.emitTaskStateChanged(txCtx, t, pm.TaskRunning, reason)
	}); err != nil {
		return err
	}
	if blocked {
		s.forgetStuckNode(taskID)
	}
	return nil
}
