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
	"sort"
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
	// GitRunner probes the terminal worktree's git status at finalize (issue-0186f85e ②:
	// branch / HEAD sha / dirty / pushed → the `finalized` marker). OPTIONAL — nil defaults
	// to the WorktreeProvisioner's runner when one is wired, else the real git binary; the
	// probe is best-effort so a non-git workspace just yields Probed=false.
	GitRunner GitRunner
	// signal delivers the watchdog kill to an adopted orphan's process group. An
	// unexported seam (tests set it to avoid signalling a real pid); nil →
	// realGroupSignal (production killpg).
	signal groupSignaler
	// Log is the fail-loud sink for the T969 codex thread_id capture (a codex run with
	// no captured thread_id logs loud that resume is unavailable). OPTIONAL — nil → no-op.
	Log func(format string, args ...any)
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
	git       GitRunner
	clk       clock.Clock
	// log is the fail-loud sink (T969 codex thread_id capture); never nil after
	// NewMonitor (defaults to a no-op).
	log func(format string, args ...any)

	// mu guards the two small activity-tracking maps below. The Monitor is otherwise
	// lock-free (design §3: the orchestrator is the sole coordinator), but these are
	// touched from BOTH the per-executor drain goroutines (Finalize) and the watchdog
	// tick goroutine (Sweep / SampleProgress), so they need synchronization.
	mu sync.Mutex
	// stallKilled records this-process executors the watchdog Sweep graceful-killed,
	// so the eventual Finalize can label their stop "stalled" (the reaped kill would
	// otherwise classify as a generic nonzero_exit — the "stalled" cause is lost).
	stallKilled map[string]struct{}
	// fused records this-process executors FuseKillTask circuit-broke because the center
	// revoked their task's execution lease (blocked mid-flight / reassigned / terminal —
	// issue-88e32d98 P0). Unlike a stall this is a DELIBERATE stop the center ordered, so
	// the recovery driver must NOT relaunch it (a fused → recover → relaunch produced an
	// unfused executor that kept running a blocked task — the P67 Ship 事故). The drain's
	// point-recovery consumes this mark (TakeFused) and quiet-finalizes instead.
	fused map[string]struct{}
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
	// Git runner for the finalize-time worktree status probe (issue-0186f85e ②): prefer an
	// explicit runner, then the provisioner's (so a test's fake git is reused), else real git.
	git := cfg.GitRunner
	if git == nil {
		if cfg.Worktrees != nil {
			git = cfg.Worktrees.runner
		} else {
			git = NewExecGitRunner()
		}
	}
	logf := cfg.Log
	if logf == nil {
		logf = func(string, ...any) {}
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
		git:         git,
		clk:         clk,
		log:         logf,
		stallKilled: make(map[string]struct{}),
		fused:       make(map[string]struct{}),
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

// FuseKillTask circuit-breaks the live this-process executor running taskRef
// (issue-88e32d98, P0 block-fuse): the center revoked the task's execution lease — it
// was blocked mid-flight (ADR-0046: a block leaves status=running, so the executor
// would otherwise keep going — the P67 Ship 事故 that merged dead code), reassigned, or
// terminal — so stop the process NOW. Same mechanism as the watchdog Sweep's per-handle
// kill (mark the cause BEFORE the kill so the concurrent drain labels the stop
// correctly), just triggered by a center signal rather than a local stall. Unlike Sweep
// it marks the death FUSED, not stalled: a stall is a local hang the recovery driver
// RELAUNCHES; a fuse is a center-ordered stop it must NOT relaunch (marking it stalled
// made point-recovery treat the fuse as a recoverable stall → relaunch of a blocked task,
// the P67 Ship 事故 — issue-88e32d98). The per-executor drain goroutine then observes the
// death, consumes the fuse mark (TakeFused), and QUIET-finalizes it (slot released, dir
// torn down, NO writeback — the center already owns the blocked task's state). Returns
// killed=true when a matching live handle was found and graceful-killed; (false,nil) when
// no live pool executor runs that task (e.g. it is an adopted orphan, or already gone) —
// best-effort, never fatal.
func (m *Monitor) FuseKillTask(ctx context.Context, taskRef string) (bool, error) {
	if m.pool == nil || m.watchdog == nil || strings.TrimSpace(taskRef) == "" {
		return false, nil
	}
	for _, h := range m.pool.Handles() {
		if m.taskRef(h.ExecutorID) != taskRef {
			continue
		}
		m.MarkFused(h.ExecutorID) // label the stop FUSED (do-not-recover) before the kill
		if err := m.watchdog.GracefulKill(ctx, h); err != nil {
			return false, fmt.Errorf("executor: fuse-kill %s (task %s): %w", h.ExecutorID, taskRef, err)
		}
		return true, nil
	}
	return false, nil
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

// Scan rebuilds in-flight executor state from durable files WITHOUT any side effect —
// no adopt, no finalize, no writeback (design §12; the pure Reconciler). The recovery
// DRIVER (boot self-reconcile) uses it so IT decides, per executor, whether to adopt /
// tier-recover / finalize — instead of Recover's unconditional "adopt alive, finalize
// the rest", which finalizes+tears-down a dead-but-should-continue executor BEFORE the
// §4.3 tier ladder can resume it (the T854 D6 P0-2 boot-recovery bug). Recover remains
// for callers wanting the old adopt-or-finalize batch.
func (m *Monitor) Scan() ([]Reconciled, error) {
	if m.recon == nil {
		return nil, errors.New("executor: monitor reconciler required for scan")
	}
	return m.recon.Reconcile()
}

// FinalizeRecovered finalizes a terminal orphan reconstructed from durable files at
// restart (Recovered=true), used by the boot driver for the executors it decides NOT
// to recover (succeeded / a legitimately-failed one). Best-effort at the call site.
func (m *Monitor) FinalizeRecovered(ctx context.Context, c Completion) error {
	c.Recovered = true
	return m.Finalize(ctx, c)
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
// captureCodexThreadID persists a completed codex executor's captured thread_id (from
// output.json, parsed by ParseCodexRunnerStream) into its recovery Record.SessionID, so
// a later resume can `codex exec resume <thread_id>` (T969 option A: capture at
// completion). No-op for a claude executor (its Record.RunnerCmd is not `codex exec`) or
// when no tracker is wired — the claude path is byte-identical. A codex run that captured
// NO thread_id (thread.started missing / stream truncated) is logged FAIL-LOUD: recovery
// then degrades to a tier-2 rerun (the safe fallback), never a resume-with-empty-id.
func (m *Monitor) captureCodexThreadID(c Completion) {
	if m.tracker == nil {
		return
	}
	rec, err := m.tracker.Read(c.ExecutorID)
	if err != nil {
		return // never tracked / already reaped — nothing to update
	}
	if !isCodexRunnerCmd(rec.RunnerCmd) {
		return // claude / non-codex: capture is a no-op (path unchanged)
	}
	var tid string
	if c.Output != nil {
		tid = strings.TrimSpace(c.Output.ThreadID)
	}
	if tid == "" {
		m.log("codex executor=%s: no thread_id captured (thread.started missing / stream truncated) — "+
			"tier-1 resume unavailable, recovery will tier-2 rerun", c.ExecutorID)
		return
	}
	rec.SessionID = tid
	if err := m.tracker.Write(rec); err != nil {
		m.log("codex executor=%s: persist thread_id to Record.SessionID failed: %v (recovery will tier-2 rerun)", c.ExecutorID, err)
	}
}

func (m *Monitor) Finalize(ctx context.Context, c Completion) error {
	if c.Kind == OutcomeRunning {
		return nil
	}
	// issue-37015227 ② — NON-DELIVERY HARD GATE. Before ANY writeback trusts a self-
	// reported success, verify the executor produced a real git side effect. A run that
	// exited 0 with output.json.success=true but left HEAD un-advanced past base, a clean
	// tree, and nothing pushed delivered NOTHING — trusting its "succeeded" is exactly the
	// canned-PASS zero-delivery hole. The gate downgrades such a success to a retryable
	// non_delivery (fail-loud), so it can never reach the center/supervisor as "succeeded".
	// Probe the worktree's structured git status ONCE, here, BEFORE the writeback —
	// so the delivery-audit signal (branch / HEAD / dirty / pushed / ahead-of-base)
	// rides the center Report onto the task record (issue-f30b7e7b: the center-side
	// stuck-node reconcile reads `pushed` to tell a recoverable dead process from a
	// terminal-but-never-pushed executor, which must be triage-blocked, not
	// auto-reopened). The same value feeds the non-delivery gate below AND the retained
	// `finalized` marker further down — one probe, reused everywhere (was probed twice
	// before: once in the gate, once at the marker, and never carried to the center).
	c.Git = m.finalizeGitStatus(ctx, c.ExecutorID)
	c = m.gateNonDelivery(ctx, c)
	// T969 (option A): persist a codex executor's captured thread_id into
	// Record.SessionID BEFORE teardown, so a later resume can `codex exec resume
	// <thread_id>`. codex mints its thread_id at runtime (thread.started), unlike
	// claude's pre-allocated session id, so it lands at completion here rather than at
	// Launch. Fail-loud: a codex run with NO captured thread_id logs loud (recovery then
	// tier-2 reruns — the safe fallback), never a silent resume-with-empty-id (Build also
	// guards that: an empty sessionID yields a plain `exec`, not `resume`).
	m.captureCodexThreadID(c)
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
	// 3) Teardown durable state. A retryable crash RETAINS its dir/worktree for
	// re-launch + inspection (design §7 "清理或保留").
	if c.Retryable {
		return nil
	}
	// DELAYED teardown (k8s TTL-after-finished analog): a TERMINAL (Succeeded/Failed)
	// executor's dir + worktree are RETAINED and stamped `finalized`; the periodic
	// ReapFinalized sweep removes them after the TTL / when the retained count exceeds
	// the cap. This leaves a window for the supervisor to push the executor's branch
	// and audit its work before the worktree is destroyed — closing the gap that lost
	// review-only commits (issue-f30b7e7b). All GC is agent-runtime-local (the center
	// only holds the writeback above), matching k8s's node-agent-owns-local-GC model.
	// Capture the worktree's structured git status BEFORE retaining (issue-0186f85e ②):
	// branch / HEAD sha / dirty / pushed, so a later delivery-detection / audit pass can
	// mechanically judge "did this executor really deliver, and was it pushed" WITHOUT the
	// worktree still present — root-causing T947 (teardown-before-audit lost unpushed work).
	// Reuse the single probe taken before the writeback (c.Git) rather than re-probing.
	if err := m.fx.MarkFinalized(c.ExecutorID, m.clk.Now(), c.Git); err != nil {
		// Can't stamp the retain marker → fall back to immediate teardown. Never leak:
		// a lost retain window is far better than an un-reaped dir/worktree.
		return m.tearDownExecutor(ctx, c.ExecutorID)
	}
	return nil
}

// gateNonDelivery is the issue-37015227 ② delivery hard-gate: it downgrades a
// self-reported OutcomeSucceeded that produced NO real git side effect to a retryable
// OutcomeCrashed with a machine-readable non_delivery cause, and logs FAIL-LOUD. It runs
// at the single Finalize funnel, BEFORE the writeback, so a zero-delivery success can
// never be relayed to the center/supervisor as "succeeded" (the canned-PASS hole).
//
// It only ever acts on a POSITIVE, unambiguous zero-delivery (FinalizedGitStatus.
// ZeroDelivery): the workspace is a real git worktree, its spawn-time base is known, and
// HEAD did not advance past base AND the tree is clean AND nothing was pushed. Every other
// case — not a git worktree (plain-dir executor), base unknown/unresolvable, any commit /
// dirty / pushed evidence, or a non-success outcome — is left UNTOUCHED (fail-safe: the
// gate blocks only proven non-delivery, never a run it cannot mechanically judge). The
// downgrade routes through the existing retryable path (dir/worktree RETAINED, reported as
// a retryable block), so no new writeback branch is needed.
func (m *Monitor) gateNonDelivery(ctx context.Context, c Completion) Completion {
	if c.Kind != OutcomeSucceeded {
		return c
	}
	// Reuse the single probe taken in Finalize (c.Git) rather than re-probing.
	gs := c.Git
	if gs == nil || !gs.Probed {
		return c // plain-dir / non-git / unresolvable workspace → cannot judge → trust
	}
	// DURABLE-DELIVERY criterion (issue-f30b7e7b N3): a genuine success must have its work
	// PUSHED — durable off this (about-to-be-reaped) worktree. The prior gate trusted ANY
	// commit past base (HasDelivery = ahead||dirty||pushed), which let the review-only bug
	// slip through: that executor COMMITS (ahead>0) but never PUSHES, so the commit dies with
	// the reaped worktree — "committed ≠ delivered". Requiring Pushed closes that blind spot.
	if gs.Pushed {
		return c // pushed HEAD → work is durably delivered off-machine → genuine success
	}
	// Probed && !Pushed: committed-but-not-pushed, dirty, or nothing produced — no DURABLE
	// delivery. Any work lives only in this worktree and is lost on reap. Refuse to let the
	// executor's self-reported success reach the center/judge as a real delivery (that was
	// the "commit 完即自认 done, 零可评审交付" false-success). Retryable-vs-auto-block is
	// decided DOWNSTREAM in the writeback (N4), not here — this only marks non-delivery.
	// An un-fetched remote also reads as not-pushed; that is the delivery-audit SAFE side (the
	// supervisor judgment still verifies), preferred over trusting a maybe-unpushed run.
	m.log("NON-DELIVERY executor=%s task=%s: reported SUCCESS but produced NO DURABLE delivery "+
		"(branch=%s head=%s ahead=%d dirty=%t pushed=false) — HEAD not pushed, work would die with "+
		"the reaped worktree; refusing to complete, downgrading to non_delivery (issue-f30b7e7b N3, "+
		"extends issue-37015227 ②)",
		c.ExecutorID, m.taskRef(c.ExecutorID), gs.Branch, gs.HeadSHA, gs.AheadOfBase, gs.Dirty)
	c.Kind = OutcomeCrashed
	c.Retryable = true
	c.Error = &ErrorDetail{
		Kind: "non_delivery",
		Message: "executor reported success but produced no durable delivery: HEAD was never " +
			"pushed (committed-but-unpushed work dies with the reaped worktree) — treated as " +
			"non-delivery",
	}
	return c
}

// finalizeGitStatus probes the terminal executor's worktree git state for the retain
// marker (issue-0186f85e ②) AND the non-delivery gate (issue-37015227 ②). Best-effort:
// an unresolvable workspace dir or a non-git workspace yields nil (a timestamp-only
// marker), never an error — the probe must never fail the finalize. It compares HEAD
// against the executor's spawn-time base ref (from the recovery Record) so the marker —
// and the gate — can tell whether HEAD actually advanced past base.
func (m *Monitor) finalizeGitStatus(ctx context.Context, executorID string) *FinalizedGitStatus {
	ws, err := m.fx.Layout().WorkspaceDir(executorID)
	if err != nil {
		return nil
	}
	gs := probeGitStatus(ctx, m.git, ws, m.recordBaseRef(executorID))
	if !gs.Probed {
		return nil // not a git worktree / probe failed → record timestamp only
	}
	return &gs
}

// recordBaseRef reads the executor's spawn-time base ref from its recovery Record, so the
// finalize probe can measure how far HEAD advanced past base. "" when no tracker is wired
// or the Record is absent/baseless (a plain-dir / untracked executor) — the gate then
// treats delivery as unjudgeable and trusts the reported outcome (fail-safe).
func (m *Monitor) recordBaseRef(executorID string) string {
	if m.tracker == nil {
		return ""
	}
	rec, err := m.tracker.Read(executorID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(rec.BaseRef)
}

// FinalizeFused terminally settles a FUSED executor (FuseKillTask circuit-break) WITHOUT
// any center writeback (issue-88e32d98 P0). It is Finalize minus step (1) Report: the
// center ALREADY blocked/revoked the task — that revocation is what tripped the fuse — so
// it owns the task's state. Reporting the killed process's outcome would (a) inject a
// spurious supervisor judgment turn that could complete/merge a task the center
// deliberately stopped (the P67 Ship 事故), and (b) risk double-writing an already-blocked
// task. So this only frees the pool slot, emits the terminal stop for observability, and
// tears down durable state — never re-launchable residue (it always MarkFinalized/teardown
// so a later boot Scan can't re-recover it). Running is a no-op.
func (m *Monitor) FinalizeFused(ctx context.Context, c Completion) error {
	if c.Kind == OutcomeRunning {
		return nil
	}
	m.captureCodexThreadID(c)
	// Free the concurrency slot (the process is already gone). No writeback precedes it,
	// so — unlike Finalize's writeback-gated release — this is unconditional.
	if m.pool != nil {
		m.pool.Release(c.ExecutorID)
	}
	// Terminal stop for the activity stream (before teardown, so task_ref still resolves).
	m.emitStop(c)
	// Force delayed-teardown (never RETAIN as retryable): a fused executor must not leave
	// state a later recovery/boot Scan could relaunch into a new unfused incarnation. A
	// fused executor's work is deliberately rejected, so no git-status audit marker is
	// captured (nil = timestamp-only) — unlike Finalize's retain-and-audit path.
	if err := m.fx.MarkFinalized(c.ExecutorID, m.clk.Now(), nil); err != nil {
		return m.tearDownExecutor(ctx, c.ExecutorID)
	}
	return nil
}

// tearDownExecutor removes a terminal executor's worktree + dir — the actual cleanup,
// deferred from Finalize to ReapFinalized (delayed teardown). Best-effort worktree
// removal (it may already be gone); the dir RemoveAll is the authoritative cleanup.
func (m *Monitor) tearDownExecutor(ctx context.Context, executorID string) error {
	if m.worktrees != nil {
		if ws, err := m.fx.Layout().WorkspaceDir(executorID); err == nil {
			_ = m.worktrees.Remove(ctx, ws)
		}
	}
	// P4/P5: tear down a repo-materializer worktree (never the canonical source).
	// No-op when no cleaner/tracker is wired or the Record carries no RepoKey.
	m.cleanupPreparedWorktree(ctx, executorID)
	if err := m.fx.Remove(executorID); err != nil {
		return fmt.Errorf("executor: remove dir %s: %w", executorID, err)
	}
	return nil
}

// ReapFinalized removes retained-terminal executors (delayed teardown / k8s TTL-
// after-finished + terminated-pod GC): it reaps any `finalized` dir older than ttl,
// and — bounding disk — reaps the OLDEST beyond `maxKeep` even if within ttl (keeps
// at most `maxKeep` newest). ttl<=0 reaps every finalized dir; maxKeep<=0 disables
// the count bound. Returns the number reaped. Best-effort per dir (a reap error is
// skipped, not fatal). Idempotent + safe to call every reconcile tick.
func (m *Monitor) ReapFinalized(ctx context.Context, ttl time.Duration, maxKeep int) (int, error) {
	refs, err := m.fx.ListFinalized()
	if err != nil {
		return 0, err
	}
	if len(refs) == 0 {
		return 0, nil
	}
	// Oldest first (finalizedAt ascending) so the count bound keeps the NEWEST maxKeep.
	sort.Slice(refs, func(i, j int) bool { return refs[i].At.Before(refs[j].At) })
	now := m.clk.Now()
	keepFrom := 0 // indices [0,keepFrom) are over-cap → reaped; 0 = no count-based reaping
	if maxKeep > 0 && len(refs) > maxKeep {
		keepFrom = len(refs) - maxKeep // reap the oldest (len-maxKeep), keep the newest maxKeep
	}
	reaped := 0
	for i, ref := range refs {
		overTTL := ttl <= 0 || now.Sub(ref.At) >= ttl
		overCap := i < keepFrom
		if !overTTL && !overCap {
			continue
		}
		if err := m.tearDownExecutor(ctx, ref.ExecutorID); err == nil {
			reaped++
		}
	}
	return reaped, nil
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
			Detail:         st.Detail,
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

// MarkFused records that a task's live executor was circuit-broken on a center lease
// revocation (issue-88e32d98 P0). FuseKillTask calls it before the graceful kill; the
// write happens-before the concurrent drain's TakeFused read (both mutex-guarded), so the
// fuse cause is never lost to a racing reap.
func (m *Monitor) MarkFused(executorID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fused[executorID] = struct{}{}
}

// TakeFused reports (and clears) whether FuseKillTask circuit-broke id. The point-
// recovery driver consumes it FIRST: a fused death is a center-ordered stop that must
// NOT be recovered/relaunched (it is quiet-finalized instead). Consuming (not peeking)
// keeps the mark from lingering onto a later same-id incarnation.
func (m *Monitor) TakeFused(executorID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.fused[executorID]
	delete(m.fused, executorID)
	return ok
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
