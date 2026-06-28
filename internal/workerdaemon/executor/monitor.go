package executor

// monitor.go — F5 orchestrator-side lifecycle engine (design §9 / §11.2 / §12).
//
// The Monitor is the orchestrator's executor-lifecycle brain. It DRIVES the F1
// Pool and consumes the F2 file protocol to:
//
//   - detect completion via the dual signal and finalize it (AwaitCompletion);
//   - run the watchdog over live executors, killing the stalled (Sweep);
//   - rebuild state after a crash and finalize/re-adopt orphans (Recover).
//
// Finalization is the UNIFIED WRITEBACK (design §3 "唯一写入者" / §11.2 step g):
// the orchestrator — and only it — relays the result to the source chat, writes
// memory, and updates the center task. This package never connects to the center,
// so that center-coupled work is a Writeback PORT; the Monitor owns deciding WHEN
// to report, then tearing down the worktree and releasing the pool slot.

import (
	"context"
	"errors"
	"fmt"

	"github.com/oopslink/agent-center/internal/clock"
)

// Writeback is the orchestrator's sole-writer result sink. It is implemented by
// the center-connected orchestrator (out of this package, by design): Report
// routes by Completion.Kind — Succeeded completes the task, Failed blocks/fails
// it, Crashed is reported as retryable so the orchestrator can re-queue.
type Writeback interface {
	Report(ctx context.Context, c Completion) error
}

// MonitorConfig wires the Monitor's collaborators. Exchange + Clock are required;
// the rest are optional so the Monitor degrades gracefully (e.g. no Writeback in
// a dry run, no Pool when only reconciling).
type MonitorConfig struct {
	Exchange   *FileExchange
	Worktrees  *WorktreeProvisioner
	Pool       *Pool
	Watchdog   *Watchdog
	Reconciler *Reconciler
	Writeback  Writeback
	// Liveness probes whether an adopted-orphan pid is still alive (CheckOrphan).
	// Nil → SignalLiveness (signal-0 existence check).
	Liveness LivenessProbe
	Clock    clock.Clock
	// signal delivers the watchdog kill to an adopted orphan's process group. An
	// unexported seam (tests set it to avoid signalling a real pid); nil →
	// realGroupSignal (production killpg).
	signal groupSignaler
}

// Monitor is the orchestrator-side lifecycle engine. Safe for the single
// orchestrator goroutine that drives it; method-level locking is unnecessary
// because the orchestrator is the sole coordinator (design §3).
type Monitor struct {
	fx        *FileExchange
	worktrees *WorktreeProvisioner
	pool      *Pool
	watchdog  *Watchdog
	recon     *Reconciler
	wb        Writeback
	live      LivenessProbe
	killSig   groupSignaler
	clk       clock.Clock
}

// NewMonitor validates cfg and builds a Monitor.
func NewMonitor(cfg MonitorConfig) (*Monitor, error) {
	if cfg.Exchange == nil {
		return nil, errors.New("executor: monitor exchange required")
	}
	clk := cfg.Clock
	if clk == nil {
		clk = clock.SystemClock{}
	}
	live := cfg.Liveness
	if live == nil {
		live = SignalLiveness{}
	}
	killSig := cfg.signal
	if killSig == nil {
		killSig = realGroupSignal
	}
	return &Monitor{
		fx:        cfg.Exchange,
		worktrees: cfg.Worktrees,
		pool:      cfg.Pool,
		watchdog:  cfg.Watchdog,
		recon:     cfg.Reconciler,
		wb:        cfg.Writeback,
		live:      live,
		killSig:   killSig,
		clk:       clk,
	}, nil
}

// AwaitCompletion blocks on a live executor process, harvests its files, applies
// the dual-signal classification (§9), and finalizes the result. This is the
// orchestrator's wake event (3) "executor 退出" handler for executors THIS
// process spawned (so it holds a reapable Handle).
func (m *Monitor) AwaitCompletion(ctx context.Context, h *Handle) (Completion, error) {
	if h == nil {
		return Completion{}, errors.New("executor: monitor await nil handle")
	}
	waitErr := h.Wait()
	out, hasOut, st := m.harvest(h.ExecutorID)
	c := Classify(CompletionFacts{
		ExecutorID: h.ExecutorID,
		Exited:     true,
		ExitErr:    waitErr,
		Output:     out,
		HasOutput:  hasOut,
		Status:     st,
	})
	if err := m.Finalize(ctx, c); err != nil {
		return c, err
	}
	return c, nil
}

// Sweep is one watchdog tick (design §9): over every live pool handle, read the
// status and graceful-kill any executor whose progress has stalled. The kill
// makes the process exit, so the AwaitCompletion path then classifies it as a
// failure ("按失败处理"). Returns the ids it killed. A missing/not-yet-written
// status is skipped (a just-launched executor is not judged stalled).
func (m *Monitor) Sweep(ctx context.Context) ([]string, error) {
	if m.pool == nil || m.watchdog == nil {
		return nil, nil
	}
	now := m.clk.Now()
	var killed []string
	for _, h := range m.pool.Handles() {
		st, err := m.fx.ReadStatus(h.ExecutorID)
		if err != nil {
			continue // no/invalid status yet → not stalled
		}
		if !m.watchdog.Check(st, now).Stalled {
			continue
		}
		if err := m.watchdog.GracefulKill(ctx, h); err != nil {
			return killed, fmt.Errorf("executor: sweep kill %s: %w", h.ExecutorID, err)
		}
		killed = append(killed, h.ExecutorID)
	}
	return killed, nil
}

// CheckOrphan runs ONE watchdog + completion tick for an ADOPTED orphan executor
// (design §12): a process re-adopted after an orchestrator restart, for which this
// orchestrator holds NO reapable handle (it cannot Wait a reparented non-child).
// The orphan's completion is therefore observed by POLLING liveness, not reaping —
// this is the gap Sweep cannot cover (Sweep only sees pool handles, and an adopted
// orphan is a handle-less reservation). The caller ticks it until done:
//
//   - process gone  → harvest + dual-signal Classify + Finalize → done=true
//   - stalled        → graceful-kill by pid + finalize as failed (§9 "按失败处理") → done=true
//   - still running  → no-op → done=false
//
// done=true means the orphan reached a terminal Completion and was finalized (slot
// released, writeback reported, dir torn down for terminal outcomes): stop polling.
func (m *Monitor) CheckOrphan(ctx context.Context, executorID string, pid int) (Completion, bool, error) {
	if pid <= 0 {
		return Completion{}, false, fmt.Errorf("executor: check orphan %s: invalid pid %d", executorID, pid)
	}
	if m.live.Alive(pid) {
		// Alive: the only thing that ends it early is a watchdog stall.
		if m.watchdog != nil {
			st, err := m.fx.ReadStatus(executorID)
			if err == nil && m.watchdog.Check(st, m.clk.Now()).Stalled {
				h := recoveredHandle(executorID, pid, m.killSig)
				if kErr := m.watchdog.GracefulKill(ctx, h); kErr != nil {
					return Completion{}, false, fmt.Errorf("executor: orphan watchdog kill %s: %w", executorID, kErr)
				}
				// We KNOW it stalled, so finalize as a definite failure now (design §9
				// "按失败处理") rather than let the next tick observe the kill and
				// (mis)classify it as a retryable crash — that would re-queue a job that
				// just proved it hangs. GracefulKill returns after SIGKILL, so it is gone.
				c := m.stalledCompletion(executorID, &st)
				if fErr := m.Finalize(ctx, c); fErr != nil {
					return c, false, fErr
				}
				return c, true, nil
			}
		}
		return Completion{ExecutorID: executorID, Kind: OutcomeRunning}, false, nil
	}
	// Process gone: classify from the durable files via the same dual signal Recover
	// uses (Exited=false, Alive=false → output/status decide succeeded/failed/crashed).
	out, hasOut, st := m.harvest(executorID)
	c := Classify(CompletionFacts{
		ExecutorID: executorID,
		Exited:     false,
		Alive:      false,
		Output:     out,
		HasOutput:  hasOut,
		Status:     st,
	})
	if err := m.Finalize(ctx, c); err != nil {
		return c, false, err
	}
	return c, true, nil
}

// stalledCompletion synthesizes the terminal Failed Completion for a watchdog-killed
// stalled executor (design §9 "stalled → 按失败处理"). It is non-retryable on
// purpose: a job that stalled past the timeout should not be auto-re-queued to stall
// again. Any harvested output is attached for the writeback's chat relay.
func (m *Monitor) stalledCompletion(executorID string, st *Status) Completion {
	c := Completion{
		ExecutorID: executorID,
		Kind:       OutcomeFailed,
		Status:     st,
		Error:      &ErrorDetail{Kind: "stalled", Message: "executor stalled: no progress before watchdog timeout, killed by orchestrator"},
	}
	if out, hasOut, _ := m.harvest(executorID); hasOut {
		c.Output = out
	}
	return c
}

// Recover rebuilds in-flight state at orchestrator startup (design §12) and acts
// on each orphan exactly once: terminal/crashed orphans are finalized (their
// result is not lost across the restart); still-alive orphans are re-adopted into
// the pool for correct concurrency accounting (and returned so the orchestrator
// can resume polling them). It NEVER re-spawns, so recovery cannot duplicate.
func (m *Monitor) Recover(ctx context.Context) ([]Reconciled, error) {
	if m.recon == nil {
		return nil, errors.New("executor: monitor reconciler required for recover")
	}
	items, err := m.recon.Reconcile()
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if it.Completion.Kind == OutcomeRunning {
			m.adopt(it.ExecutorID)
			continue
		}
		if err := m.Finalize(ctx, it.Completion); err != nil {
			return items, fmt.Errorf("executor: recover finalize %s: %w", it.ExecutorID, err)
		}
	}
	return items, nil
}

// adopt re-registers a recovered still-alive executor into the pool so it counts
// toward max_concurrent_tasks again. Best-effort: a full pool or an unknown id is
// not fatal to recovery (the executor keeps running regardless; the worst case is
// transient over-admission, never a lost executor).
func (m *Monitor) adopt(executorID string) {
	if m.pool == nil {
		return
	}
	_ = m.pool.Adopt(executorID)
}

// Finalize is the UNIFIED WRITEBACK + teardown (design §11.2 step g/h). It is the
// single place that reports a Completion to the center (sole writer), then tears
// down the worktree and frees the pool slot. Terminal outcomes (Succeeded/Failed)
// have their dir + worktree removed; a retryable Crash RETAINS them so the
// orchestrator can re-launch from the preserved input.json. Running is a no-op
// (not a completion).
func (m *Monitor) Finalize(ctx context.Context, c Completion) error {
	if c.Kind == OutcomeRunning {
		return nil
	}
	// 1) Report — the only center write. Must succeed before we drop durable state,
	// so a writeback failure leaves the dir intact for a retry (no silent loss).
	if m.wb != nil {
		if err := m.wb.Report(ctx, c); err != nil {
			return fmt.Errorf("executor: writeback %s: %w", c.ExecutorID, err)
		}
	}
	// 2) Free the concurrency slot so a queued executor can launch (design §3). The
	// process is already gone for every finalized outcome.
	if m.pool != nil {
		m.pool.Release(c.ExecutorID)
	}
	// 3) Teardown durable state — only for terminal outcomes; retain a retryable
	// crash's dir/worktree for re-launch + inspection (design §7 "清理或保留").
	if c.Retryable {
		return nil
	}
	if m.worktrees != nil {
		if ws, err := m.fx.Layout().WorkspaceDir(c.ExecutorID); err == nil {
			// Best-effort: the worktree may already be gone (crash mid-teardown); that
			// is the desired end state, not an error worth aborting cleanup over.
			_ = m.worktrees.Remove(ctx, ws)
		}
	}
	if err := m.fx.Remove(c.ExecutorID); err != nil {
		return fmt.Errorf("executor: remove dir %s: %w", c.ExecutorID, err)
	}
	return nil
}

// harvest reads output.json + status for executorID, tolerating either being
// absent/invalid (returns nil for whichever is unavailable). This is the raw
// fact-gathering the classifier consumes.
func (m *Monitor) harvest(executorID string) (out *Output, hasOut bool, st *Status) {
	if o, err := m.fx.ReadOutput(executorID); err == nil {
		out = &o
		hasOut = true
	}
	if s, err := m.fx.ReadStatus(executorID); err == nil {
		st = &s
	}
	return out, hasOut, st
}
