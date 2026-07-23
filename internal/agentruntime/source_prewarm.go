package agentruntime

// =============================================================================
// Repo-source prewarm (issue-13e7bfe8, P0·控制流 HOL 饿死).
//
// THE BUG. SpawnExecutor used to call Materializer.EnsureSource — a full `git
// clone` — INLINE on the control-command path. That path is not a place where a
// multi-minute network operation can live:
//
//	center control stream → ControlLoop.handleBatch (ONE goroutine per WORKER,
//	  serving EVERY agent on it) → Deliver over the agent's unix socket
//	  (http.Client.Timeout = 5s, hard) → agent HTTP handler → NotifyWorkAvailable
//	  → SpawnExecutor → EnsureSource → git clone
//
// A clone slower than 5s (i.e. any real repo) blew the client deadline. Three
// things then compounded:
//
//	① The worker's Deliver errored, so handleBatch never advanced its offset and
//	   re-pulled the SAME command every poll — forever. That cursor is shared by
//	   every agent on the worker, so ONE agent's slow clone starved ALL of them.
//	   Observed in prod: 420 consecutive retries, whole worker control flow wedged.
//	② The cancelled request ctx propagated into exec.CommandContext, SIGKILLing
//	   git mid-clone and leaving a half-written .git AT THE CANONICAL PATH — which
//	   the next EnsureSource happily "reused" (see materializer.go layer 2).
//	③ Each retry re-entered SpawnExecutor under a fresh, equally-doomed 5s ctx and
//	   re-took forkMu, so the attempts piled up behind the lock inside the agent.
//
// THE FIX (this file, layer 1). The control handler NEVER blocks on a clone. It
// consults an in-memory gate:
//
//	fresh source cached  → proceed inline (no network on the control path)
//	otherwise            → register the task as a waiter, kick ONE background
//	                       prewarm goroutine, and return immediately (task stays
//	                       queued). The control command acks in microseconds.
//
// When the background clone finishes it RE-DRIVES its waiters by calling
// SpawnExecutor again, which now hits the fresh-cache path and forks for real.
//
// The re-drive is not optional. "Leave it queued for a later re-emit" is what the
// pre-fix code claimed, but the center's only backstop (wake_projector's 60s
// sweep) skips any agent with a running task (`load.Running != 0`) — so a queued
// task on a busy agent would be stranded until the agent went fully idle, if ever.
// This gate therefore owns its own re-drive rather than relying on that sweep.
//
// The background clone runs under context.Background() + its OWN generous timeout:
// it must NOT inherit the 5s control ctx (that inheritance IS the bug), and it must
// still be bounded so a hung git cannot leak a goroutine forever.
// =============================================================================

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/reporepo"
)

const (
	// defaultSourcePrewarmTimeout bounds ONE background EnsureSource (clone or fetch).
	// Generous by design — a cold clone of a large repo is minutes, and the whole point
	// of this file is that nothing on the control path waits for it. It exists only so a
	// wedged git can never leak the goroutine forever.
	defaultSourcePrewarmTimeout = 15 * time.Minute

	// defaultSourceFreshFor is how long a materialized source is trusted without a
	// re-fetch. Within the window SpawnExecutor proceeds INLINE (no network, no defer),
	// so a burst of tasks on one repo pays the fetch once. Past it, the next spawn defers
	// once more to refresh — the cost is one background fetch + an automatic re-drive,
	// not a stranded task.
	defaultSourceFreshFor = 60 * time.Second

	// defaultSourcePrewarmBackoff is the base pause between failed prewarm attempts.
	defaultSourcePrewarmBackoff = 5 * time.Second

	// defaultSourcePrewarmAttempts is how many times ONE prewarm episode retries
	// EnsureSource before giving up and failing the waiters LOUDLY. Bounded retry is the
	// layer-3 "escape" applied at this layer: a permanently-bad repo (dead remote, bad
	// credentials) must not retry forever, it must become a visible blocked task.
	defaultSourcePrewarmAttempts = 3
)

// sourceEntry is the per-repo_key prewarm state.
type sourceEntry struct {
	// ready is the last successfully materialized source; nil until the first clone
	// lands. It is retained even across later failures so a transient fetch error can
	// DEGRADE to the still-usable existing source rather than block the task.
	ready   *reporepo.SourceRepo
	readyAt time.Time

	// inflight marks that a prewarm goroutine owns this key — the dedup that keeps N
	// concurrent work_available commands for one repo to ONE clone.
	inflight bool

	// waiters are task ids that deferred on this key and must be re-driven when the
	// source lands. A set: repeated work_available for the same task coalesces.
	waiters map[string]struct{}
}

// sourceGate holds the per-repo_key prewarm state for one runtime.
type sourceGate struct {
	mu      sync.Mutex
	entries map[string]*sourceEntry

	// wg tracks in-flight prewarm goroutines (including the re-drive they perform), so
	// tests can await an episode deterministically instead of sleeping.
	wg sync.WaitGroup
}

// sourceFreshFor / sourcePrewarmTimeout / sourcePrewarmBackoff / sourcePrewarmAttempts
// resolve the tunables, letting tests collapse the timings without touching defaults.
func (r *LocalRuntime) sourceFreshFor() time.Duration {
	if d := r.cfg.SourceFreshFor; d > 0 {
		return d
	}
	return defaultSourceFreshFor
}

func (r *LocalRuntime) sourcePrewarmTimeout() time.Duration {
	if d := r.cfg.SourcePrewarmTimeout; d > 0 {
		return d
	}
	return defaultSourcePrewarmTimeout
}

func (r *LocalRuntime) sourcePrewarmBackoff() time.Duration {
	// Negative is meaningful: tests set it to collapse the backoff to zero.
	if r.cfg.SourcePrewarmBackoff != 0 {
		if r.cfg.SourcePrewarmBackoff < 0 {
			return 0
		}
		return r.cfg.SourcePrewarmBackoff
	}
	return defaultSourcePrewarmBackoff
}

func (r *LocalRuntime) sourcePrewarmAttempts() int {
	if n := r.cfg.SourcePrewarmAttempts; n > 0 {
		return n
	}
	return defaultSourcePrewarmAttempts
}

// freshSource returns the cached source for key when it is present AND still inside the
// freshness window. This is the ONLY source lookup the control path performs: it is pure
// in-memory bookkeeping, so it cannot block, cannot touch the network, and cannot burn
// the caller's 5s deadline.
func (r *LocalRuntime) freshSource(key string) (reporepo.SourceRepo, bool) {
	r.sources.mu.Lock()
	defer r.sources.mu.Unlock()
	e := r.sources.entries[key]
	if e == nil || e.ready == nil {
		return reporepo.SourceRepo{}, false
	}
	if r.now().Sub(e.readyAt) > r.sourceFreshFor() {
		return reporepo.SourceRepo{}, false
	}
	return *e.ready, true
}

// dirExists reports whether path is present on disk. Used to refuse to serve a cached
// source whose directory has since disappeared.
func dirExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// anySource returns the cached source for key REGARDLESS of freshness. Only the prewarm
// re-drive may use it: serving a lapsed source there is strictly better than the
// alternative (an unbounded defer→prewarm→re-drive→defer livelock when a fetch outlives
// the freshness window). The source is at worst one episode old, and the next spawn past
// the window refreshes it normally.
func (r *LocalRuntime) anySource(key string) (reporepo.SourceRepo, bool) {
	r.sources.mu.Lock()
	defer r.sources.mu.Unlock()
	if e := r.sources.entries[key]; e != nil && e.ready != nil {
		return *e.ready, true
	}
	return reporepo.SourceRepo{}, false
}

// deferForSource registers taskID as a waiter on key and starts a background prewarm if
// none is already running for it. It returns immediately — that immediacy is the whole
// point: the control command it is called from acks now instead of dying on a 5s
// deadline and wedging the worker's shared control cursor.
func (r *LocalRuntime) deferForSource(agentID, taskID, key string, target reporepo.RepoTarget) {
	r.sources.mu.Lock()
	if r.sources.entries == nil {
		r.sources.entries = map[string]*sourceEntry{}
	}
	e := r.sources.entries[key]
	if e == nil {
		e = &sourceEntry{waiters: map[string]struct{}{}}
		r.sources.entries[key] = e
	}
	if e.waiters == nil {
		e.waiters = map[string]struct{}{}
	}
	e.waiters[taskID] = struct{}{}
	if e.inflight {
		// A clone for this repo is already running; this task just joined its waiters
		// and will be re-driven with the rest. Do NOT start a second clone.
		r.sources.mu.Unlock()
		r.log("work_available agent=%s task=%s repo_key=%s: repo source materializing (already in flight) — task left queued, will be re-driven when it lands",
			agentID, taskID, key)
		return
	}
	e.inflight = true
	r.sources.mu.Unlock()

	r.log("work_available agent=%s task=%s repo_key=%s: repo source not ready — starting BACKGROUND materialize (control command returns now, task left queued, re-driven on completion)",
		agentID, taskID, key)

	r.sources.wg.Add(1)
	go func() {
		defer r.sources.wg.Done()
		r.runSourcePrewarm(agentID, key, target)
	}()
}

// runSourcePrewarm materializes the source for key OFF the control path, then re-drives
// every task that deferred on it. It retries a bounded number of times and, on final
// failure, fails its waiters LOUDLY rather than leaving them silently queued.
//
// Runs on its own goroutine. Never holds forkMu (the re-drive re-acquires it via
// SpawnExecutor) and never holds sources.mu across the clone.
func (r *LocalRuntime) runSourcePrewarm(agentID, key string, target reporepo.RepoTarget) {
	attempts := r.sourcePrewarmAttempts()
	var lastErr error

	for attempt := 1; attempt <= attempts; attempt++ {
		// context.Background(), NOT the control ctx: inheriting the caller's 5s deadline
		// is exactly the defect this file exists to remove. The timeout is ours and is
		// sized for a real clone.
		ctx, cancel := context.WithTimeout(context.Background(), r.sourcePrewarmTimeout())
		src, err := r.cfg.Materializer.EnsureSource(ctx, target)
		cancel()

		if err == nil {
			r.log("agent=%s repo_key=%s: repo source ready (attempt %d/%d) — re-driving deferred task(s)",
				agentID, key, attempt, attempts)
			r.finishPrewarm(agentID, key, &src, nil)
			return
		}
		lastErr = err
		r.log("agent=%s repo_key=%s: materialize repo source attempt %d/%d failed: %v",
			agentID, key, attempt, attempts, err)
		if attempt < attempts {
			if d := r.sourcePrewarmBackoff(); d > 0 {
				time.Sleep(d)
			}
		}
	}
	r.finishPrewarm(agentID, key, nil, lastErr)
}

// finishPrewarm closes one prewarm episode: it publishes the result, clears the in-flight
// flag, drains the waiter set, and then (outside the lock) either re-drives the waiters or
// fails them loudly.
//
// The DEGRADE case is deliberate: when a refresh fails but a previously materialized
// source is still on disk, the waiters are re-driven against that stale source instead of
// being blocked. A transient network blip must not fail tasks that a perfectly usable
// local checkout could have run. readyAt is stamped forward so the next spawn does not
// immediately re-defer into the same failing refresh.
func (r *LocalRuntime) finishPrewarm(agentID, key string, src *reporepo.SourceRepo, failure error) {
	r.sources.mu.Lock()
	e := r.sources.entries[key]
	if e == nil { // defensive: never expected (deferForSource created it)
		r.sources.mu.Unlock()
		return
	}
	e.inflight = false
	degraded := false
	switch {
	case src != nil:
		e.ready = src
		e.readyAt = r.now()
	case e.ready != nil && dirExists(e.ready.Path):
		// Refresh failed but the previously materialized source is STILL ON DISK →
		// degrade to it, don't block.
		e.readyAt = r.now()
		degraded = true
	default:
		// Either nothing was ever materialized, or the cached source is gone from disk
		// (e.g. a heal quarantined it and the follow-up clone then failed). Re-driving
		// against a path that no longer exists would strand every waiter silently —
		// PrepareWorktree would fail, the task would be "left queued", and the cache
		// would keep answering fresh for the rest of the window so later tasks skipped
		// the gate and failed inline too. Drop the cache and take the fail-loud path.
		e.ready = nil
	}
	waiters := make([]string, 0, len(e.waiters))
	for id := range e.waiters {
		waiters = append(waiters, id)
	}
	e.waiters = map[string]struct{}{}
	usable := e.ready != nil
	if !usable {
		// Nothing materialized and nothing cached: drop the entry so a later
		// work_available starts a clean episode (fresh attempt budget) rather than
		// inheriting this one's exhausted state.
		delete(r.sources.entries, key)
	}
	r.sources.mu.Unlock()

	if degraded {
		r.log("agent=%s repo_key=%s: repo source refresh FAILED (%v) but an existing source is present — DEGRADING to it and re-driving deferred task(s) rather than failing them",
			agentID, key, failure)
	}

	for _, taskID := range waiters {
		if usable {
			r.redriveDeferredSpawn(agentID, taskID)
			continue
		}
		r.failTaskRepoUnavailable(agentID, taskID, failure)
	}
}

// redriveDeferredSpawn re-enters SpawnExecutor for a task that deferred on a repo source
// now materialized. It runs on the prewarm goroutine (never the control path), so it uses
// a background ctx: the control command that originally carried this task id was acked and
// its request ctx is long gone.
//
// It RETRIES, because this re-drive is the task's only remaining driver. The task has
// already been drained from the waiter set, the control command that carried it was acked
// long ago, and the center's wake-sweep re-drives a queued task only while the agent has
// no running task. SpawnExecutor reports every non-fork the same way — (nil, nil) — so a
// transient cause (a get_task blip, a momentarily saturated pool) is indistinguishable
// here from a terminal one (task already running/closed). A single attempt would let any
// blip strand the task permanently, so we retry a bounded number of times and give up
// loudly. Re-attempting a task that was legitimately un-forkable is harmless: SpawnExecutor
// re-checks status and simply declines again.
func (r *LocalRuntime) redriveDeferredSpawn(agentID, taskID string) {
	attempts := r.sourcePrewarmAttempts()
	for attempt := 1; attempt <= attempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), r.sourcePrewarmTimeout())
		res, err := r.SpawnExecutor(ctx, SpawnRequest{TaskID: taskID, redrive: true})
		cancel()

		if err == nil && res != nil {
			r.log("agent=%s task=%s re-drive after repo source ready: forked executor=%s", agentID, taskID, res.ExecutorID)
			return
		}
		if err != nil {
			r.log("agent=%s task=%s re-drive after repo source ready (attempt %d/%d): %v", agentID, taskID, attempt, attempts, err)
		} else {
			r.log("agent=%s task=%s re-drive after repo source ready (attempt %d/%d): not forked — see the preceding line for why",
				agentID, taskID, attempt, attempts)
		}
		if attempt < attempts {
			if d := r.sourcePrewarmBackoff(); d > 0 {
				time.Sleep(d)
			}
		}
	}
	// Deliberately NOT blocked: a non-fork is frequently the correct outcome (the task
	// was cancelled, completed, or picked up elsewhere), and blocking a healthy task on
	// that basis would be worse than the log. This line is the last trace of a task that
	// deferred on a repo source and never forked.
	r.log("work_available agent=%s task=%s: repo source is READY but the task did not fork after %d re-drive attempt(s) — NOT retrying further; if the task is still open it needs a new work_available",
		agentID, taskID, attempts)
}

// failTaskRepoUnavailable surfaces a permanently un-materializable repo LOUDLY instead of
// leaving the task silently queued (the fail-loud requirement).
//
// The task was never admitted (start_task was never called — the whole repo-workspace
// step runs BEFORE admission, red line A), and the center REFUSES to block a non-running
// task: Task.Block returns ErrIllegalTransition unless status == running, and the handler
// rolls the reason message back with it, so a bare block_task here would 422 and leave NO
// trace. So admit first, then block — the same start_task→block shape
// blockTaskOnForkFailure relies on. A declined start_task is itself logged loudly and the
// task stays queued (the center considers it un-runnable right now anyway).
func (r *LocalRuntime) failTaskRepoUnavailable(agentID, taskID string, cause error) {
	ctx, cancel := context.WithTimeout(context.Background(), r.sourcePrewarmTimeout())
	defer cancel()

	failureCause := CauseRepoSourceUnavailable
	if errors.Is(cause, reporepo.ErrCacheRefUnavailable) {
		failureCause = CauseRepoRefUnavailable
	}
	r.log("work_available agent=%s task=%s REPO SOURCE UNAVAILABLE [cause=%s] after %d attempt(s): %v — admitting + blocking the task (fail-loud; NOT left silently queued)",
		agentID, taskID, failureCause, r.sourcePrewarmAttempts(), cause)

	if r.toolCaller() == nil {
		r.log("work_available agent=%s task=%s repo source unavailable: no center transport — cannot surface, task left queued", agentID, taskID)
		return
	}
	if err := r.startCenterTask(ctx, agentID, taskID); err != nil {
		r.log("work_available agent=%s task=%s repo source unavailable: start_task (to make the task blockable) declined: %v — task left queued, failure visible in this log only",
			agentID, taskID, err)
		return
	}
	reason := forkFailureReason(failureCause, cause)
	body := map[string]any{
		"agent_id":    agentID,
		"task_id":     taskID,
		"reason":      reason,
		"reason_type": "obstacle",
	}
	if bErr := r.toolCaller().CallAgentTool(ctx, "block_task", body, nil); bErr != nil {
		r.log("work_available agent=%s task=%s repo source unavailable: block_task failed: %v", agentID, taskID, bErr)
	}
}

// waitSourcePrewarm blocks until every in-flight prewarm episode (and the re-drive it
// performs) has finished. Test-only determinism seam — production never calls it.
func (r *LocalRuntime) waitSourcePrewarm() { r.sources.wg.Wait() }
