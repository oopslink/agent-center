package workerdaemon

import (
	"context"
	"time"

	"github.com/oopslink/agent-center/internal/workerdaemon/agentruntime"
)

// lease_renew.go (T456 / issue-21ba5b78 I30 P0 #1) — the worker-side, process-alive
// execution-lease auto-renew.
//
// The server grants each running task a heartbeat-renewed execution lease. Before
// T456 the only renewer was the agent itself (the MCP `heartbeat` tool), so a long
// LLM turn (a multi-minute build/test that never pauses to call a tool) could let the
// lease lapse and the task be reclaimed. T456 removes the reclaim (a lapsed lease now
// only NUDGES — see the PM lease-checker), and ALSO closes the false-lapse hole here:
// the worker daemon — which KNOWS the agent process is alive — renews the lease for
// every live session's current task on a periodic tick, decoupled from the agent's
// turn. While the process is up the lease never lapses; only a truly-dead/disconnected
// worker stops renewing, which is exactly when the nudge/self-heal backstops should
// engage.
//
// Supersede-safety (per the task spec — "被 supersede 的旧 session 不应再续租"): the
// sweep iterates the controller's CURRENT agents map, which holds exactly one
// managedAgent per agentID. A superseded/restarted session's managedAgent is replaced
// (its currentTaskID reset), so only the live session's task is renewed — an old
// instance can never keep a lease alive.

// DefaultLeaseRenewEvery is the default cadence of the process-alive lease auto-renew.
// 60s is far below the server's execution-lease TTL (hours), so a renew always lands
// long before the lease could lapse, while keeping the renew traffic light.
const DefaultLeaseRenewEvery = 60 * time.Second

// leaseRenewTarget is one (agentID, taskID) the sweep will renew — a live session
// that currently has a task injected.
type leaseRenewTarget struct {
	agentID string
	taskID  string
}

// drainLeaseRenewals renews the execution lease for every live session's current task
// (T456). Invoked from OnTick (single-threaded ControlLoop goroutine) but rate-limited
// to cfg.LeaseRenewEvery via nextLeaseRenewAt so it does NOT run on every sub-second
// tick. Best-effort: a per-agent report error is logged and never aborts the rest or
// the agent loop.
func (c *AgentController) drainLeaseRenewals(ctx context.Context, now time.Time) {
	c.mu.Lock()
	if !c.nextLeaseRenewAt.IsZero() && now.Before(c.nextLeaseRenewAt) {
		c.mu.Unlock()
		return
	}
	c.nextLeaseRenewAt = now.Add(c.cfg.LeaseRenewEvery)
	// Snapshot the (agentID, managedAgent, runtime) triples under c.mu, then inspect
	// each agent's ma.state.* under its OWN StateMu (去共享状态) — never holding c.mu and
	// a StateMu at once, and never two StateMus at once (one agent inspected at a time).
	type leaseCandidate struct {
		id string
		ma *managedAgent
		rt *agentruntime.LocalRuntime
	}
	var candidates []leaseCandidate
	for id, ma := range c.agents {
		if ma == nil || ma.runtime == nil {
			continue
		}
		candidates = append(candidates, leaseCandidate{id: id, ma: ma, rt: ma.runtime})
	}
	c.mu.Unlock()

	var targets []leaseRenewTarget
	for _, cand := range candidates {
		mu := cand.rt.StateMu()
		mu.Lock()
		ma := cand.ma
		// A live session that is NOT being torn down (an intentional stop / survival
		// detach is in progress → its lease should be allowed to lapse) and currently
		// has a task injected. currentTaskID empty → idle/converse-only session, no
		// task lease to keep alive.
		var taskID string
		if ma.state != nil && ma.state.Session != nil && !ma.state.ExpectedStop && !ma.state.Detaching {
			taskID = ma.state.CurrentTaskID
		}
		mu.Unlock()
		if taskID == "" {
			continue
		}
		targets = append(targets, leaseRenewTarget{agentID: cand.id, taskID: taskID})
	}

	for _, t := range targets {
		if err := c.cfg.Reporter.RenewTaskLease(ctx, t.agentID, t.taskID, now); err != nil {
			// Best-effort: the lease is also re-armed on the next tick; a single failed
			// renew never stalls the agent loop. A persistent failure means the lease
			// will eventually lapse and the server's nudge/self-heal backstops engage.
			c.log("agent=%s lease-renew task=%s: %v", t.agentID, t.taskID, err)
		}
	}
}
