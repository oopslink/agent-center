package workerdaemon

// self_heal.go — mid-run crash recovery (v2.7 GATE-7 Mode-B, slice B1). When a
// desired-running agent's supervisor/claude dies WHILE the daemon stays up, the
// boot-reconcile Mode-B path does NOT fire (it only runs on a daemon restart). This
// state machine recovers it:
//
//   - onExit (the supervisor-session pump goroutine) RECORDS the crash + schedules a
//     backed-off relaunch — it NEVER calls startSession (start is only safe on the
//     single-threaded ControlLoop goroutine).
//   - OnTick (invoked by the ControlLoop each tick, single-threaded) DRAINS due
//     relaunches: reap residual + startSession (resumes the durable session.epoch →
//     same session-id → context preserved) + nudge (re-drive an interrupted turn).
//
// Backoff caps the relaunch rate; after the attempt cap the agent CIRCUIT-BREAKS to a
// terminal state (manual recovery only) so a crash-loop can't thrash (claude crash →
// relaunch → crash → …). Params are @oopslink/PM authoritative (msgs c277e962 /
// 9fe6748e): backoff 1→2→4→8→16s (cap 30s), maxAttempts 5 (crashCount 1..5 relaunch,
// the 6th crash → terminal), reset the count after a healthy run ≥ 60s.
//
// B1 surfaces all of this in the daemon LOG (per-relaunch timestamp + attempt# +
// backoff + crashCount + the terminal/given-up transition + last crash cause) — the
// observability the verification needs. B2 adds the formal LifecycleFailed agent-BC
// state + Fleet column (terminal state user-visible).

import (
	"context"
	"time"

	"github.com/oopslink/agent-center/internal/supervisormanager"
)

// Self-heal defaults (overridable via AgentControllerConfig; manual is authoritative).
const (
	defaultSelfHealMaxAttempts = 5                // crashCount 1..5 → relaunch; the (max+1)th crash → terminal
	defaultSelfHealBackoffBase = 1 * time.Second  // 1→2→4→8→16s
	defaultSelfHealBackoffCap  = 30 * time.Second // ceiling (never hit at maxAttempts=5; matters if raised)
	defaultSelfHealResetWindow = 60 * time.Second // healthy run ≥ this since last relaunch → reset crashCount
)

// selfHealEntry is the per-agent crash-recovery state. It SURVIVES the managedAgent
// delete that onExit performs on a crash. Guarded by AgentController.mu.
type selfHealEntry struct {
	crashCount     int       // consecutive crashes (reset after a healthy run)
	lastRelaunchAt time.Time // when the last self-heal relaunch fired (zero = never)
	nextRelaunchAt time.Time // a relaunch is due at/after this (zero = none pending)
	failed         bool      // circuit-broken (cap exhausted) → NO auto relaunch until manual reset
	lastCrashMsg   string    // most recent crash cause (observability)
	version        int       // reconcile version captured at crash, for the relaunch
	nudge          bool      // had active work at crash → re-drive the interrupted turn
	taskID         string    // in-flight WorkItem id captured at crash (survives the
	// managedAgent delete) → the relaunch rebinds currentTaskID to it so a FAILED
	// re-drive turn surfaces via L2 (no-silent-failure across Mode-B). Empty = idle crash.
	model string // agent's claude --model captured at crash (survives the managedAgent
	// delete) → the self-heal relaunch spawns the re-driven claude with the SAME model.
	// self-heal gets NO fresh reconcile, so without this the re-drive would fall back to
	// claude's default model (a real product regression, not just a test gap). Empty = none.
	displayName string // agent's display_name captured at crash (survives the managedAgent
	// delete) → the self-heal relaunch spawns the supervisor with the SAME git author NAME.
	// self-heal gets NO fresh reconcile, so without this the re-drive would fall back to the
	// ULID AgentID default (T469). Empty = none (ULID fallback).
	promptDescription string // T728: already-gated description text captured at crash
	// (survives the managedAgent delete) → the self-heal relaunch spawns the supervisor with
	// the SAME persona段. self-heal gets NO fresh reconcile, so without this the re-drive would
	// silently drop the injected description. Empty = none (no injection).
	envVars            map[string]string // agent profile env captured at crash; self-heal gets no fresh reconcile.
	concurrencyEnabled bool              // concurrent mode captured at crash; self-heal gets no fresh reconcile.
}

type selfHealParams struct {
	maxAttempts int
	backoffBase time.Duration
	backoffCap  time.Duration
	resetWindow time.Duration
}

type selfHealDecision struct {
	failed     bool
	crashCount int           // updated consecutive-crash count
	backoff    time.Duration // valid iff !failed
}

// decideSelfHeal is the PURE crash→action policy (unit-tested for curve/cap/reset).
// prevCount is the prior consecutive-crash count; lastRelaunchAt is when the last
// relaunch fired (zero = never). A healthy run ≥ resetWindow since the last relaunch
// RESETS the count (the agent recovered; a later isolated crash starts fresh, not
// dragged into early circuit-break by long-ago crashes).
func decideSelfHeal(prevCount int, lastRelaunchAt, now time.Time, p selfHealParams) selfHealDecision {
	count := prevCount + 1
	if !lastRelaunchAt.IsZero() && now.Sub(lastRelaunchAt) >= p.resetWindow {
		count = 1 // healthy run since the last relaunch → this is a fresh crash
	}
	if count > p.maxAttempts {
		return selfHealDecision{failed: true, crashCount: count}
	}
	backoff := p.backoffBase << (count - 1)
	if backoff <= 0 || backoff > p.backoffCap { // <=0 guards a shift overflow
		backoff = p.backoffCap
	}
	return selfHealDecision{crashCount: count, backoff: backoff}
}

// now returns the controller's clock (test seam; defaults to time.Now).
func (c *AgentController) now() time.Time {
	if c.cfg.Now != nil {
		return c.cfg.Now()
	}
	return time.Now()
}

func (c *AgentController) selfHealParams() selfHealParams {
	p := selfHealParams{
		maxAttempts: c.cfg.SelfHealMaxAttempts,
		backoffBase: c.cfg.SelfHealBackoffBase,
		backoffCap:  c.cfg.SelfHealBackoffCap,
		resetWindow: c.cfg.SelfHealResetWindow,
	}
	if p.maxAttempts <= 0 {
		p.maxAttempts = defaultSelfHealMaxAttempts
	}
	if p.backoffBase <= 0 {
		p.backoffBase = defaultSelfHealBackoffBase
	}
	if p.backoffCap <= 0 {
		p.backoffCap = defaultSelfHealBackoffCap
	}
	if p.resetWindow <= 0 {
		p.resetWindow = defaultSelfHealResetWindow
	}
	return p
}

// recordCrashAndSchedule is called from onExit (pump goroutine) on an UNEXPECTED
// crash of a desired-running agent. It updates the self-heal state and either
// schedules a backed-off relaunch (OnTick performs it) or circuit-breaks to terminal.
// It NEVER starts a session (single-thread invariant). hadWork → nudge on relaunch.
//
// Returns the lifecycle STATE the caller should report (outside the lock): "error"
// (transient — a relaunch is scheduled), "failed" (terminal — the cap is reached), or
// "" (no report — a defensive crash after the agent is already terminal-failed).
func (c *AgentController) recordCrashAndSchedule(agentID string, version int, hadWork bool, taskID, model, displayName, promptDescription string, envVars map[string]string, concurrencyEnabled bool, msg string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.selfHeal[agentID]
	if e == nil {
		e = &selfHealEntry{}
		c.selfHeal[agentID] = e
	}
	if e.failed {
		// Already circuit-broken — auto side never touches a failed agent again.
		c.log("agent=%s self-heal: crash after terminal-failed — ignored (manual reset required). cause: %s", agentID, msg)
		return ""
	}
	dec := decideSelfHeal(e.crashCount, e.lastRelaunchAt, c.now(), c.selfHealParams())
	e.crashCount = dec.crashCount
	e.lastCrashMsg = msg
	e.version = version
	e.nudge = hadWork
	e.taskID = taskID                       // rebound to currentTaskID on the relaunch (L2×Mode-B)
	e.model = model                         // re-driven claude spawns with the SAME model (self-heal gets no fresh reconcile)
	e.displayName = displayName             // re-driven supervisor keeps the SAME git author NAME (T469)
	e.promptDescription = promptDescription // re-driven supervisor keeps the SAME injected persona段 (T728)
	e.envVars = cloneEnvVars(envVars)
	e.concurrencyEnabled = concurrencyEnabled
	if dec.failed {
		e.failed = true
		e.nextRelaunchAt = time.Time{}
		c.log("agent=%s self-heal TERMINAL: %d consecutive crashes reached the cap — circuit-broken, NO further auto relaunch (manual reset required). last cause: %s",
			agentID, dec.crashCount, msg)
		return "failed" // terminal lifecycle (Fleet-visible), reported by the caller
	}
	e.nextRelaunchAt = c.now().Add(dec.backoff)
	c.log("agent=%s self-heal: crash #%d → relaunch scheduled in %s (cause: %s)",
		agentID, dec.crashCount, dec.backoff, msg)
	return "error" // transient — the worker is still auto-retrying
}

// OnTick is invoked by the ControlLoop each tick (single-threaded — the only safe
// caller of startSession). It drains every due self-heal relaunch. Implements the
// optional tickHandler the ControlLoop type-asserts (additive: a handler without it
// is simply never ticked).
func (c *AgentController) OnTick(ctx context.Context) {
	now := c.now()
	type due struct {
		agentID            string
		version            int
		nudge              bool
		taskID             string
		model              string
		displayName        string
		promptDescription  string
		envVars            map[string]string
		concurrencyEnabled bool
		attempt            int
	}
	var dues []due
	c.mu.Lock()
	for id, e := range c.selfHeal {
		if e.failed || e.nextRelaunchAt.IsZero() || e.nextRelaunchAt.After(now) {
			continue
		}
		// A live session already exists (e.g. a reconcile beat us to it) → drop the
		// pending relaunch; nothing to heal.
		if ma := c.agents[id]; ma != nil && ma.session != nil {
			e.nextRelaunchAt = time.Time{}
			continue
		}
		dues = append(dues, due{agentID: id, version: e.version, nudge: e.nudge, taskID: e.taskID, model: e.model, displayName: e.displayName, promptDescription: e.promptDescription, envVars: cloneEnvVars(e.envVars), concurrencyEnabled: e.concurrencyEnabled, attempt: e.crashCount})
		e.nextRelaunchAt = time.Time{} // consume the schedule (no re-fire)
		e.lastRelaunchAt = now         // healthy-run reset window is measured from here
	}
	c.mu.Unlock()

	for _, d := range dues {
		c.selfHealRelaunch(ctx, d.agentID, d.version, d.nudge, d.taskID, d.model, d.displayName, d.promptDescription, d.envVars, d.concurrencyEnabled, d.attempt)
	}

	// LLM 服务端限流自动恢复: re-drive any agent whose rate-limit window has cleared.
	// Unlike self-heal this needs NO relaunch — the session stayed alive across the
	// rate-limit; we just inject the resume nudge into the live session. See
	// rate_limit.go.
	c.drainRateLimitResumes(ctx, now)

	// T456 (issue-21ba5b78/I30): process-alive lease auto-renew. Renew the execution
	// lease for every live session's current task so a long LLM turn never lets the
	// lease lapse. Internally rate-limited to cfg.LeaseRenewEvery. See lease_renew.go.
	c.drainLeaseRenewals(ctx, now)

	// Task directory GC (design §11.3): clean up expired aborted/done task dirs.
	// Throttled to at most once per cfg.GCInterval. See gc_timer.go.
	c.maybeRunGC(now)

	// Executor watchdog (W3 / design §9 + §12): stall-kill live executors and poll
	// adopted orphans to completion. Throttled to defaultExecutorWatchdogInterval.
	// No-op for agents not running the concurrent path. See concurrent_exec.go.
	c.maybeRunExecutorWatchdog(ctx, now)
}

// selfHealRelaunch performs ONE due relaunch on the ControlLoop goroutine: acquire
// the agent home lock (single-instance, cross-daemon), then reap residual + start a
// fresh supervisor (resumes the durable epoch) + nudge iff the crash interrupted
// active work. Reuses bootReapRelaunch (same reap+resume+nudge sequence).
func (c *AgentController) selfHealRelaunch(ctx context.Context, agentID string, version int, nudge bool, taskID, model, displayName, promptDescription string, envVars map[string]string, concurrencyEnabled bool, attempt int) {
	home, _, _, err := c.agentPaths(agentID)
	if err != nil {
		c.log("agent=%s self-heal relaunch resolve home: %v — skip", agentID, err)
		return
	}
	release, lerr := supervisormanager.AcquireHomeLock(home)
	if lerr != nil {
		// Another holder (concurrent op) — re-arm for the next tick (idempotent).
		c.mu.Lock()
		if e := c.selfHeal[agentID]; e != nil && !e.failed {
			e.nextRelaunchAt = c.now()
		}
		c.mu.Unlock()
		c.log("agent=%s self-heal relaunch home lock busy: %v — retry next tick", agentID, lerr)
		return
	}
	defer release()
	c.log("agent=%s self-heal RELAUNCH attempt=%d at=%s (nudge=%v)", agentID, attempt, c.now().Format(time.RFC3339), nudge)
	if rerr := c.bootReapRelaunch(ctx, agentID, home, version, nudge, taskID, model, displayName, promptDescription, envVars, concurrencyEnabled); rerr != nil {
		// The relaunch FAILED to come up (e.g. "supervisor did not come up within 15s"
		// — gate3b/c). nextRelaunchAt was already consumed in OnTick, so without this the
		// agent would SILENT-LIMBO: no retry, no circuit-break, no surface (FINDING-3
		// #117 part A). LOUD-surface it (distinct from the success log) and treat the
		// fail like a crash toward the circuit-break: increment the crash/attempt
		// accounting and reschedule with backoff, OR go terminal at the cap — so a
		// relaunch that keeps failing to come up cannot loop forever, it eventually
		// circuit-breaks to terminal (Fleet-visible).
		c.log("agent=%s self-heal RELAUNCH attempt=%d FAILED to come up: %v", agentID, attempt, rerr)
		state := c.recordRelaunchFailAndSchedule(agentID, version, nudge, taskID, model, displayName, promptDescription, envVars, concurrencyEnabled, rerr.Error())
		if state == "failed" {
			// Terminal circuit-break — surface the Fleet-visible lifecycle once.
			if err := c.cfg.Reporter.ReportAgentLifecycle(context.Background(), agentID, state, rerr.Error(), c.now()); err != nil {
				c.log("agent=%s report %s: %v", agentID, state, err)
			}
		}
	}
}

// recordRelaunchFailAndSchedule advances the self-heal state when a RELAUNCH itself
// fails to come up (FINDING-3 #117 part A) — the bounded-not-limbo mechanism. It is
// the relaunch-fail analogue of recordCrashAndSchedule: it reuses the SAME state
// machine (decideSelfHeal curve/cap/reset over crashCount) so a relaunch that keeps
// failing counts toward the cap exactly like a crash and eventually circuit-breaks to
// terminal, instead of silent-limbo. Called on the ControlLoop goroutine (OnTick →
// selfHealRelaunch), so it takes c.mu like recordCrashAndSchedule.
//
// Returns "failed" when the cap is reached (terminal — the caller surfaces the
// Fleet-visible lifecycle), or "error" when another backed-off retry was scheduled.
func (c *AgentController) recordRelaunchFailAndSchedule(agentID string, version int, nudge bool, taskID, model, displayName, promptDescription string, envVars map[string]string, concurrencyEnabled bool, msg string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.selfHeal[agentID]
	if e == nil {
		// Defensive: a relaunch implies an entry exists; recreate if somehow cleared.
		e = &selfHealEntry{}
		c.selfHeal[agentID] = e
	}
	if e.failed {
		c.log("agent=%s self-heal: relaunch-fail after terminal-failed — ignored (manual reset required). cause: %s", agentID, msg)
		return ""
	}
	dec := decideSelfHeal(e.crashCount, e.lastRelaunchAt, c.now(), c.selfHealParams())
	e.crashCount = dec.crashCount
	e.lastCrashMsg = msg
	e.version = version
	e.nudge = nudge
	e.taskID = taskID
	e.model = model
	e.displayName = displayName
	e.promptDescription = promptDescription
	e.envVars = cloneEnvVars(envVars)
	e.concurrencyEnabled = concurrencyEnabled
	if dec.failed {
		e.failed = true
		e.nextRelaunchAt = time.Time{}
		c.log("agent=%s self-heal TERMINAL: relaunch failed to come up %d time(s), reached the cap — circuit-broken, NO further auto relaunch (manual reset required). last cause: %s",
			agentID, dec.crashCount, msg)
		return "failed"
	}
	e.nextRelaunchAt = c.now().Add(dec.backoff)
	c.log("agent=%s self-heal: relaunch-fail #%d → retry scheduled in %s (cause: %s)",
		agentID, dec.crashCount, dec.backoff, msg)
	return "error"
}

// clearSelfHeal drops an agent's self-heal state (incl the terminal failed flag). A
// COMMAND-driven lifecycle change (reconcile running/stop/reset) is the manual /
// intentional path — it clears any crash accounting and un-latches a circuit-broken
// agent (the operator's reset/restart = the only way out of terminal-failed). The
// self-heal relaunch path (OnTick) does NOT call this, so crashCount accrues toward
// the cap across auto relaunches.
func (c *AgentController) clearSelfHeal(agentID string) {
	c.mu.Lock()
	delete(c.selfHeal, agentID)
	c.mu.Unlock()
}
