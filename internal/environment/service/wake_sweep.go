package service

import "time"

// --- T335 follow-up: server-side periodic session-heal sweep (the second net) ---
//
// T335 fixed the WORKER side: agent.work_available, when it arrives for a down
// desired-running agent, relaunches the dead session (workAvailable →
// relaunchForWake → reconcileAgentOnBoot) instead of ack-and-dropping. This sweep
// is the SERVER-side backstop: even if a wake is lost entirely (the worker never
// receives it), a periodic sweep re-emits agent.work_available for agents that look
// stuck, so a queued task can never sit forever behind a dead session.
//
// LEVER CHOICE — re-emit agent.work_available, NOT agent.reconcile. A same-version
// agent.reconcile is swallowed by the daemon's appliedVersion replay guard
// (agent_controller.go reconcile: pl.Version <= appliedVersion → replay no-op, never
// relaunches a dead-but-tracked session). work_available instead routes through the
// T335 relaunch-before-dedup path, so re-emitting it actually brings a down session
// back up (and on a LIVE session degrades to the harmless per-(agent,task) nudge
// dedup). No daemon change is required.

// SweepCandidate is one agent the session-heal sweep may nudge: a desired-running
// agent that has queued runnable work but no running task (≈ a dropped or idle
// session that should be working but is not). The composition root computes the set
// (it owns the PM + agent queries and the id-scheme resolution); the projector only
// applies the debounce and emits.
//
//   - WorkerID selects the agent's control stream.
//   - AgentID is the agent's ENTITY id (a.ID()) — the key the daemon session map and
//     the center resume-state use. It is NOT the agentActor/identity-member ref a
//     task assignee carries; the two differ, so the wiring must pass a.ID() here.
//   - TaskID is one of the agent's runnable queued tasks — the wake payload's dedup
//     anchor on the daemon and a log breadcrumb.
type SweepCandidate struct {
	WorkerID string
	AgentID  string
	TaskID   string
}

// commandTypeWorkAvailable mirrors the daemon's cmdTypeWorkAvailable. See the lever
// note above for why the sweep emits this rather than agent.reconcile.
const commandTypeWorkAvailable = "agent.work_available"

// sweepWakePayload matches the daemon workAvailablePayload {agent_id, task_id}.
type sweepWakePayload struct {
	AgentID string `json:"agent_id"`
	TaskID  string `json:"task_id"`
}

// defaultSweepGrace is the per-agent grace window when none is wired: a candidate
// must stay stuck this long before its first nudge.
const defaultSweepGrace = 60 * time.Second

// sweepMaxBackoff caps the per-episode re-nudge interval so a desired-running agent
// that stays stuck despite nudges does not append a ControlLog row every tick
// forever. Combined with the grace gate (no emit before a candidate has been stuck
// one full grace window) and the sweepMaxEmits give-up cap below, this bounds the
// sweep's ControlLog footprint. NOTE: control_events has no global GC today
// (append-only for every command type) — that is pre-existing and tracked separately
// as issue-b71ee81f; the sweep keeps its OWN footprint negligible via these gates.
const sweepMaxBackoff = 5 * time.Minute

// sweepMaxEmits is the per-episode give-up cap. After this many nudges across one
// stuck episode (grace + exponential backoff put the K-th nudge at roughly 15–20min
// in) an agent that STILL has no running session is treated as genuinely
// unrecoverable: the sweep stops nudging it and raises a visible signal once (via the
// wired sweepGiveUp hook — at least a warn, ideally a human-facing obstacle). This is
// the real bound on ControlLog for the one unbounded case — a "desired=running but
// the session never comes up" agent, which is NOT lifecycle failed/error and so is
// never excluded by the candidate gate — and it surfaces such an agent instead of
// silently slow-retrying forever. On recovery (the agent acquires a running task) or
// transition to terminal, its state is pruned and a future stuck episode re-arms.
const sweepMaxEmits = 6

// sweepAgentState is the per-agent debounce/backoff memory the sweep keeps across
// ticks. It is IN-MEMORY ONLY and NOT persisted: on process restart the map is empty,
// so a still-stuck agent simply re-earns its grace window from the first post-restart
// tick (at most one extra grace of delay before the next nudge). That is acceptable
// for a slow backstop — do not assume this state survives a restart.
type sweepAgentState struct {
	firstSeen time.Time // when the agent first entered the stuck candidate set
	lastEmit  time.Time // when we last emitted a wake for it (zero = never)
	emitCount int       // emits so far this episode (drives the backoff)
	gaveUp    bool      // true once we hit sweepMaxEmits and raised the give-up signal
}

// selectDueSweeps applies the per-agent grace + exponential backoff + give-up cap to
// the raw candidate set, mutating the debounce state, and returns two disjoint sets:
//   - due:    agents to nudge (emit agent.work_available) THIS tick.
//   - giveUp: agents that just crossed sweepMaxEmits and must be escalated ONCE (the
//     caller raises the visible signal); they are not nudged again until they recover.
//
// It also prunes state for agents that recovered (dropped out of the candidate set).
// This is the single home of the sweep's timing memory — the caller only performs I/O
// for whatever this returns.
//
// Rules:
//   - First sighting of an agent → record firstSeen, emit nothing (the grace clock
//     starts). A normally-booting session typically acquires a running task and drops
//     out before grace elapses, so it is never nudged.
//   - Stuck < grace → skip (still booting / just dispatched).
//   - Stuck >= grace, never emitted → emit (first nudge).
//   - Already emitted, < sweepMaxEmits → re-emit only after an exponentially growing,
//     capped interval (grace, 2·grace, 4·grace, … up to sweepMaxBackoff).
//   - Reached sweepMaxEmits → give up once (return in giveUp, latch gaveUp), then stay
//     silent for the rest of the episode.
func (p *WakeProjector) selectDueSweeps(cands []SweepCandidate) (due, giveUp []SweepCandidate) {
	now := p.clock.Now()
	grace := p.sweepGrace
	if grace <= 0 {
		grace = defaultSweepGrace
	}

	p.sweepMu.Lock()
	defer p.sweepMu.Unlock()
	if p.sweepState == nil {
		p.sweepState = make(map[string]*sweepAgentState)
	}

	// Prune agents that recovered (tracked but no longer a candidate) so a future
	// re-entry restarts the grace clock — and the give-up latch — from scratch.
	present := make(map[string]struct{}, len(cands))
	for _, c := range cands {
		present[c.AgentID] = struct{}{}
	}
	for id := range p.sweepState {
		if _, ok := present[id]; !ok {
			delete(p.sweepState, id)
		}
	}

	for _, c := range cands {
		st := p.sweepState[c.AgentID]
		if st == nil {
			p.sweepState[c.AgentID] = &sweepAgentState{firstSeen: now}
			continue // grace starts now — do not nudge a freshly-seen (maybe booting) agent
		}
		if st.gaveUp {
			continue // already escalated this episode; wait for recovery to re-arm
		}
		if now.Sub(st.firstSeen) < grace {
			continue // still inside the grace window
		}
		if st.emitCount > 0 {
			backoff := grace
			for i := 1; i < st.emitCount && backoff < sweepMaxBackoff; i++ {
				backoff *= 2
			}
			if backoff > sweepMaxBackoff {
				backoff = sweepMaxBackoff
			}
			if now.Sub(st.lastEmit) < backoff {
				continue // within the backoff interval since the last nudge
			}
		}
		if st.emitCount >= sweepMaxEmits {
			// Nudged the cap's worth of times across this episode and the agent STILL
			// has no running session → genuinely unrecoverable by the sweep. Escalate
			// once and go silent (the real bound on ControlLog for this case).
			st.gaveUp = true
			giveUp = append(giveUp, c)
			continue
		}
		st.lastEmit = now
		st.emitCount++
		due = append(due, c)
	}
	return due, giveUp
}
