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
	"strings"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

// WorktreeCleaner tears down a per-executor git worktree at finalize / recovery. It
// is a NARROW port defined IN THIS PACKAGE on purpose (red line D): the executor
// package must NEVER import reporepo, so the repo materializer is adapted to this
// string-only contract by a tiny adapter in a package that imports BOTH (agentruntime
// / the daemon wiring). A nil cleaner ⇒ no-op (today's behavior). Implementations
// remove ONLY the worktree at workspacePath — never the canonical source at
// sourcePath (design §10).
type WorktreeCleaner interface {
	RemoveWorktree(ctx context.Context, repoKey, sourcePath, workspacePath string) error
}

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
	// Tracker reads the per-executor recovery Record at finalize so the RepoKey/
	// SourcePath teardown handle (P5) is available for worktree cleanup. OPTIONAL —
	// nil (or a Record without a RepoKey) ⇒ no worktree cleanup, today's behavior.
	Tracker *Tracker
	// WorktreeCleaner tears down a repo-materializer worktree on finalize + recovery
	// (P4/P5). OPTIONAL — nil ⇒ no-op (today's behavior). Used together with Tracker.
	WorktreeCleaner WorktreeCleaner
	// Liveness probes whether an adopted-orphan pid is still alive (CheckOrphan).
	// Nil → SignalLiveness (signal-0 existence check).
	Liveness LivenessProbe
	Clock    clock.Clock
	// Observer, when set, receives executor stop + progress observations for the
	// agent activity stream (ADR-0049). OPTIONAL — nil disables emission; it is a
	// pure monitoring sink and never affects lifecycle decisions (activity.go).
	Observer ActivityObserver
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
	tracker   *Tracker
	cleaner   WorktreeCleaner
	obs       ActivityObserver
	live      LivenessProbe
	killSig   groupSignaler
	clk       clock.Clock

	// mu guards the two small activity-tracking maps below. The Monitor is otherwise
	// lock-free (design §3: the orchestrator is the sole coordinator), but these are
	// touched from BOTH the per-executor drain goroutines (Finalize) and the watchdog
	// tick goroutine (Sweep / SampleProgress), so they need synchronization.
	mu sync.Mutex
	// stallKilled records this-process executors the watchdog Sweep graceful-killed,
	// so the eventual Finalize can label their stop "stalled" (the reaped kill would
	// otherwise classify as a generic nonzero_exit — the "stalled" cause is lost).
	stallKilled map[string]struct{}
	// progressAt is the last status.last_progress_at emitted per executor, so
	// SampleProgress only emits on advance (change-only throttling).
	progressAt map[string]time.Time
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
		fx:          cfg.Exchange,
		worktrees:   cfg.Worktrees,
		pool:        cfg.Pool,
		watchdog:    cfg.Watchdog,
		recon:       cfg.Reconciler,
		wb:          cfg.Writeback,
		tracker:     cfg.Tracker,
		cleaner:     cfg.WorktreeCleaner,
		obs:         cfg.Observer,
		live:        live,
		killSig:     killSig,
		clk:         clk,
		stallKilled: make(map[string]struct{}),
		progressAt:  make(map[string]time.Time),
	}, nil
}

// AwaitCompletionNoFinalize blocks on a live executor process, harvests its files,
// and applies the dual-signal classification (§9) — but does NOT finalize. It is the
// recovery-aware split (T851 §4.6): the runtime decides recover-vs-finalize from the
// returned Completion, so the durable dir/worktree survive (teardown lives ONLY in
// Finalize) until the runtime judges the executor terminal.
//
// wasStall reports — WITHOUT consuming the mark (peek, not take) — whether Sweep
// watchdog-killed this executor, so the caller can pick the recovery budget (a stall
// is more suspect than a crash). The eventual Finalize still consumes + labels the
// stall cause via emitStop, so nothing is lost on the finalize path; a caller that
// RECOVERS instead must ClearStalled(id) so the mark does not linger onto the
// recovered incarnation's later terminal stop.
func (m *Monitor) AwaitCompletionNoFinalize(h *Handle) (Completion, bool, error) {
	if h == nil {
		return Completion{}, false, errors.New("executor: monitor await nil handle")
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
	return c, m.peekStalled(h.ExecutorID), nil
}

// AwaitCompletion blocks on a live executor process, harvests its files, applies
// the dual-signal classification (§9), and finalizes the result. This is the
// orchestrator's wake event (3) "executor 退出" handler for executors THIS
// process spawned (so it holds a reapable Handle). It is the reap-and-finalize
// wrapper over AwaitCompletionNoFinalize — byte-for-byte the pre-T851 behavior for
// callers that do not drive recovery.
func (m *Monitor) AwaitCompletion(ctx context.Context, h *Handle) (Completion, error) {
	c, _, err := m.AwaitCompletionNoFinalize(h)
	if err != nil {
		return c, err
	}
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
		// Mark the stall BEFORE the kill, not after. GracefulKill blocks for the
		// SIGTERM→SIGKILL grace window, and a well-behaved executor that flushes
		// output.json + status=failed and exits WITHIN that window is reaped
		// concurrently by its own drain goroutine (AwaitCompletion→Finalize→
		// emitStop→takeStalled). If the mark were set only after GracefulKill
		// returned, that reap would race ahead, find no mark, and mislabel the
		// stall-kill as a generic nonzero_exit — losing the "stalled" cause that is
		// the whole point of watchdog observability. Marking first makes the cause
		// visible to any concurrent reap; markStalled/takeStalled are mutex-guarded,
		// so the write here happens-before the reap's read.
		m.markStalled(h.ExecutorID)
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
	c, terminal, _, _, err := m.CheckOrphanNoFinalize(ctx, executorID, pid)
	if err != nil {
		return c, false, err
	}
	if terminal {
		if fErr := m.Finalize(ctx, c); fErr != nil {
			return c, false, fErr
		}
	}
	return c, terminal, nil
}

// CheckOrphanNoFinalize probes an adopted orphan and classifies it (§9) WITHOUT
// finalizing — the recovery-aware split (T851 §4.6). It still KILLS a stalled orphan
// (that is detection mechanism, and a hung process must be stopped either way), but
// leaves the recover-vs-finalize decision — and thus the dir/worktree teardown, which
// lives only in Finalize — to the runtime.
//
// Returns:
//   - terminal: the orphan reached a terminal outcome (dead, or stall-killed). false ⇒
//     still alive and healthy (caller keeps polling).
//   - killed: this call issued a GracefulKill (a stalled orphan).
//   - wasStall: the terminal cause is a stall (⇒ the runtime applies the stricter
//     stall recovery budget). For a plain crash/exit this is false.
//
// The old CheckOrphan is the finalize-on-terminal wrapper over this — byte-for-byte
// the pre-T851 behavior.
func (m *Monitor) CheckOrphanNoFinalize(ctx context.Context, executorID string, pid int) (c Completion, terminal, killed, wasStall bool, err error) {
	if pid <= 0 {
		return Completion{}, false, false, false, fmt.Errorf("executor: check orphan %s: invalid pid %d", executorID, pid)
	}
	if m.live.Alive(pid) {
		// Alive: the only thing that ends it early is a watchdog stall.
		if m.watchdog != nil {
			st, rerr := m.fx.ReadStatus(executorID)
			if rerr == nil && m.watchdog.Check(st, m.clk.Now()).Stalled {
				h := recoveredHandle(executorID, pid, m.killSig)
				if kErr := m.watchdog.GracefulKill(ctx, h); kErr != nil {
					return Completion{}, false, false, false, fmt.Errorf("executor: orphan watchdog kill %s: %w", executorID, kErr)
				}
				// KNOWN stall: synthesize the definite-failure Completion (design §9) but
				// do NOT finalize — the runtime decides bounded recover vs finalize.
				// GracefulKill returns after SIGKILL, so the process is gone.
				return m.stalledCompletion(executorID, &st), true, true, true, nil
			}
		}
		return Completion{ExecutorID: executorID, Kind: OutcomeRunning}, false, false, false, nil
	}
	// Process gone: classify from the durable files via the same dual signal Recover
	// uses (Exited=false, Alive=false → output/status decide succeeded/failed/crashed).
	out, hasOut, st := m.harvest(executorID)
	c = Classify(CompletionFacts{
		ExecutorID: executorID,
		Exited:     false,
		Alive:      false,
		Output:     out,
		HasOutput:  hasOut,
		Status:     st,
	})
	c.Recovered = true // observed via the orphan poll, not a reaped this-process exit
	return c, true, false, false, nil
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
		Recovered:  true, // only CheckOrphan (orphan path) synthesizes this
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
		it.Completion.Recovered = true // finalized from durable files at restart (orphan)
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

// ReleaseSlot frees the pool concurrency slot held by a this-process executor whose
// process has DIED, WITHOUT any writeback or teardown (T851 §4.6). The recovery
// driver calls it on the RECOVER branch — which deliberately skips Finalize — before
// relaunching the executor as an orphan, so the dead Launch handle's slot does not
// leak (the relaunched orphan is counted separately, not via a pool slot).
//
// It is a DISTINCT primitive rather than folding the release into
// AwaitCompletionNoFinalize on purpose: Finalize releases the slot ONLY after a
// successful writeback (a writeback failure retains slot + dir for retry, an invariant
// TestMonitor_Finalize_WritebackErrorRetainsDir pins). The recover branch has no
// writeback, so releasing here is unambiguous and leaves that finalize-path invariant
// untouched. Idempotent (pool.Release no-ops an unknown/already-freed id); the
// finalize branch does NOT call it (Finalize frees the slot itself, step 2).
func (m *Monitor) ReleaseSlot(executorID string) {
	if m.pool != nil {
		m.pool.Release(executorID)
	}
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
	// 2b) Emit the terminal stop to the activity stream BEFORE teardown, so task_ref
	// is still resolvable from the (not-yet-removed) input.json. Best-effort +
	// observational: it never affects the finalize outcome (activity.go contract).
	m.emitStop(c)
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
	// P4/P5: tear down a repo-materializer worktree (never the canonical source). Runs
	// on BOTH the live drain path (AwaitCompletion→Finalize) and crash recovery
	// (Recover→Finalize). No-op when no cleaner/tracker is wired or the Record carries
	// no RepoKey (a plain-dir executor) — today's behavior, byte-for-byte.
	m.cleanupPreparedWorktree(ctx, c.ExecutorID)
	if err := m.fx.Remove(c.ExecutorID); err != nil {
		return fmt.Errorf("executor: remove dir %s: %w", c.ExecutorID, err)
	}
	return nil
}

// cleanupPreparedWorktree removes a repo-materializer worktree for a finalized
// executor (P4/P5). It reads the durable Record (written at spawn) for the
// RepoKey/SourcePath teardown handle and delegates to the narrow WorktreeCleaner
// port. Best-effort (the writeback already succeeded; a teardown failure must not
// fail the finalize) and a strict no-op unless BOTH a cleaner and a tracker are
// wired AND the Record names a RepoKey — so a plain-dir executor is untouched. The
// cleaner removes ONLY the worktree, never the canonical source (design §10).
func (m *Monitor) cleanupPreparedWorktree(ctx context.Context, executorID string) {
	if m.cleaner == nil || m.tracker == nil {
		return
	}
	rec, err := m.tracker.Read(executorID)
	if err != nil || strings.TrimSpace(rec.RepoKey) == "" {
		return
	}
	ws, err := m.fx.Layout().WorkspaceDir(executorID)
	if err != nil {
		return
	}
	_ = m.cleaner.RemoveWorktree(ctx, rec.RepoKey, rec.SourcePath, ws)
}

// emitStop reports one terminal completion to the activity Observer (best-effort,
// observational — never affects finalize). It resolves task_ref from the still
// -present input.json, maps the Completion's classification onto the StopEvent
// (Error.Kind → Reason), and — for a this-process watchdog kill — overrides the
// reason to "stalled" (the reaped kill classifies as a generic nonzero_exit; the
// stall cause, tracked by Sweep, is the accurate one). It also clears the id's
// per-executor tracking state (the executor is gone; a retryable re-launch mints a
// fresh id).
func (m *Monitor) emitStop(c Completion) {
	stalled := m.takeStalled(c.ExecutorID)
	m.clearProgress(c.ExecutorID)
	if m.obs == nil {
		return
	}
	ev := StopEvent{
		ExecutorID: c.ExecutorID,
		TaskRef:    m.taskRef(c.ExecutorID),
		Outcome:    c.Kind,
		Retryable:  c.Retryable,
		Recovered:  c.Recovered,
		At:         m.clk.Now(),
	}
	if c.Error != nil {
		ev.Reason = c.Error.Kind
		ev.Detail = c.Error.Message
	}
	if stalled && ev.Reason != "stalled" {
		// This-process watchdog kill: reaped as nonzero_exit, but the cause was a
		// stall. Prefer the accurate cause so the 4th stop class is distinguishable.
		ev.Reason = "stalled"
		if ev.Detail == "" {
			ev.Detail = "executor stalled: no progress before watchdog timeout, killed by orchestrator"
		}
	}
	m.obs.ExecutorStopped(ev)
}

// SampleProgress emits a progress heartbeat for every live this-process executor
// whose status.last_progress_at has ADVANCED since its last emitted sample
// (change-only throttling — a long-lived executor yields a readable heartbeat, not
// a per-tick flood, design point 2). Driven by the daemon's watchdog tick; a
// missing/terminal/never-progressed status is skipped. No-op without an Observer
// or Pool.
func (m *Monitor) SampleProgress() {
	if m.obs == nil || m.pool == nil {
		return
	}
	for _, h := range m.pool.Handles() {
		st, err := m.fx.ReadStatus(h.ExecutorID)
		if err != nil || st.State != StateRunning || st.LastProgressAt.IsZero() {
			continue
		}
		if !m.advancedProgress(h.ExecutorID, st.LastProgressAt) {
			continue
		}
		m.obs.ExecutorProgress(ProgressEvent{
			ExecutorID:     h.ExecutorID,
			TaskRef:        m.taskRef(h.ExecutorID),
			State:          string(st.State),
			Summary:        st.Summary,
			LastProgressAt: st.LastProgressAt,
			At:             m.clk.Now(),
		})
	}
}

// taskRef resolves an executor's source task ref from its input.json (the fork
// -time Source.TaskRef). Best-effort: an absent/mid-write input yields "".
func (m *Monitor) taskRef(executorID string) string {
	if m.fx == nil {
		return ""
	}
	in, err := m.fx.ReadInput(executorID)
	if err != nil {
		return ""
	}
	return in.Source.TaskRef
}

// markStalled records that Sweep watchdog-killed this-process executor id.
func (m *Monitor) markStalled(executorID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stallKilled[executorID] = struct{}{}
}

// takeStalled reports (and clears) whether id was watchdog-killed by Sweep.
func (m *Monitor) takeStalled(executorID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.stallKilled[executorID]
	delete(m.stallKilled, executorID)
	return ok
}

// peekStalled reports whether Sweep watchdog-killed id WITHOUT clearing the mark
// (unlike takeStalled). AwaitCompletionNoFinalize uses it to surface wasStall to the
// recovery driver while leaving the eventual Finalize→emitStop→takeStalled to consume
// + label the "stalled" cause — so no cause is lost on the finalize path (T851 §4.6).
func (m *Monitor) peekStalled(executorID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.stallKilled[executorID]
	return ok
}

// ClearStalled drops id's Sweep stall mark. The recovery driver calls it when it
// RECOVERS (rather than finalizes) a this-process stalled executor, so the peeked-
// but-not-consumed mark does not linger and mislabel the recovered incarnation's
// later terminal stop as "stalled" (T851 §4.6).
func (m *Monitor) ClearStalled(executorID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.stallKilled, executorID)
}

// advancedProgress reports whether at is newer than the last progress sample
// emitted for id, recording it when so (change-only throttle).
func (m *Monitor) advancedProgress(executorID string, at time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !at.After(m.progressAt[executorID]) {
		return false
	}
	m.progressAt[executorID] = at
	return true
}

// clearProgress drops the per-executor progress watermark (called at finalize).
func (m *Monitor) clearProgress(executorID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.progressAt, executorID)
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
