package workerdaemon

// self_heal.go — the DAEMON-SIDE driver of mid-run crash recovery. The decide/record
// state machine moved into agentruntime.SelfHealStore (docs §4.0.3); this file keeps
// the OnTick drain + the relaunch ORCHESTRATION (reap + bring-up + executor re-attach),
// which stays daemon-side because it is entangled with the executor面 (Phase 0c) and
// must create a fresh runtime per relaunch (through bringUpSession, exactly like a
// fresh managedAgent in the old startSession).
//
// onExit (in the runtime, on the pump goroutine) RECORDS the crash + schedules a
// relaunch via the shared store; OnTick (ControlLoop goroutine, single-threaded)
// DRAINS due relaunches and brings the session back up.

import (
	"context"
	"time"

	"github.com/oopslink/agent-center/internal/supervisormanager"
	"github.com/oopslink/agent-center/internal/workerdaemon/agentruntime"
)

// OnTick is invoked by the ControlLoop each tick (single-threaded — the only safe
// caller of session bring-up). It drains due self-heal relaunches, ticks every live
// runtime (rate-limit / api-error resume), then runs lease-renew + GC + executor
// watchdog. Preserves the pre-extraction ordering.
func (c *AgentController) OnTick(ctx context.Context) {
	now := c.now()

	// Self-heal relaunch drain: dead agents have no runtime, so the daemon drives
	// this from the survival store.
	c.drainSelfHeal(ctx, now)

	// Per-live-runtime maintenance (rate-limit / api-error resume drains). Snapshot
	// the runtimes under the lock, then Tick outside it.
	c.mu.Lock()
	rts := make([]*agentruntime.LocalRuntime, 0, len(c.agents))
	for _, ma := range c.agents {
		if ma.runtime != nil {
			rts = append(rts, ma.runtime)
		}
	}
	c.mu.Unlock()
	for _, rt := range rts {
		_ = rt.Tick(ctx, now)
	}

	// T456 process-alive lease auto-renew (rate-limited internally to LeaseRenewEvery).
	c.drainLeaseRenewals(ctx, now)

	// Task directory GC (throttled to cfg.GCInterval).
	c.maybeRunGC(now)

	// Executor watchdog (W3; throttled). No-op for non-concurrent agents.
	c.maybeRunExecutorWatchdog(ctx, now)
}

// drainSelfHeal relaunches every due (non-terminal, scheduled) agent whose session is
// not already live. The store selects + consumes the schedule under the shared lock
// (isLive reads the agents map with the lock held); the relaunch runs outside it.
func (c *AgentController) drainSelfHeal(ctx context.Context, now time.Time) {
	isLive := func(agentID string) bool {
		// Called with c.mu HELD by DrainDue — must not re-lock.
		return c.agents[agentID].live()
	}
	for _, d := range c.selfHeal.DrainDue(now, isLive) {
		c.selfHealRelaunch(ctx, d)
	}
}

// selfHealRelaunch performs ONE due relaunch on the ControlLoop goroutine: acquire the
// agent home lock, then reap residual + bring up a fresh session (resumes the durable
// epoch) + nudge iff the crash interrupted active work. Reuses bootReapRelaunch (the
// shared reap+resume+nudge sequence). On a relaunch that fails to come up it advances
// the self-heal state (bounded-not-limbo).
func (c *AgentController) selfHealRelaunch(ctx context.Context, d agentruntime.RelaunchSpec) {
	home, _, _, err := c.agentPaths(d.AgentID)
	if err != nil {
		c.log("agent=%s self-heal relaunch resolve home: %v — skip", d.AgentID, err)
		return
	}
	release, lerr := supervisormanager.AcquireHomeLock(home)
	if lerr != nil {
		// Another holder — re-arm for the next tick (idempotent).
		c.selfHeal.Rearm(d.AgentID, c.now())
		c.log("agent=%s self-heal relaunch home lock busy: %v — retry next tick", d.AgentID, lerr)
		return
	}
	defer release()
	c.log("agent=%s self-heal RELAUNCH attempt=%d at=%s (nudge=%v)", d.AgentID, d.Attempt, c.now().Format(time.RFC3339), d.Nudge)
	if rerr := c.bootReapRelaunch(ctx, d.AgentID, home, d.Version, d.Nudge, d.TaskID, d.Model, d.DisplayName, d.PromptDescription, d.EnvVars, d.ConcurrencyEnabled); rerr != nil {
		c.log("agent=%s self-heal RELAUNCH attempt=%d FAILED to come up: %v", d.AgentID, d.Attempt, rerr)
		spec := d
		spec.Attempt = 0
		state := c.selfHeal.RecordRelaunchFailAndSchedule(spec, c.now(), rerr.Error())
		if state == "failed" {
			if err := c.cfg.Reporter.ReportAgentLifecycle(context.Background(), d.AgentID, state, rerr.Error(), c.now()); err != nil {
				c.log("agent=%s report %s: %v", d.AgentID, state, err)
			}
		}
	}
}

// clearSelfHeal drops an agent's self-heal state (incl the terminal failed flag) — the
// command-driven manual/intentional path (reconcile running/stop/reset).
func (c *AgentController) clearSelfHeal(agentID string) {
	c.selfHeal.Clear(agentID)
}
