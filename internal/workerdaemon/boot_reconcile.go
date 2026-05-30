package workerdaemon

// boot_reconcile.go is the v2.7 D2-f s4 boot-recovery DECISION core. When a worker
// daemon (re)starts with the control path active, it must reconcile every agent's
// REAL local state (did its persistent supervisor survive the daemon restart?)
// against the CENTER's desired state (should it be running? in-flight work?), and
// for each agent take exactly one action: re-attach the survivor, relaunch a dead
// one, stop+reap an unwanted one, or leave an idle one alone.
//
// decideBootAction is the PURE heart of that reconcile (s4a): it maps
// (local probe state × center record) → a single bootAction, with NO side effects,
// so the full decision matrix is exhaustively unit-testable. The ORCHESTRATION
// that enumerates agents, probes, locks, and executes the action lives in s4b.
//
// 🔴 EXHAUSTIVENESS (PM): the decision space is the FULL Cartesian product
//
//	probe  ∈ {Reattachable, Unavailable}
//	center ∈ {running+inflight, running+no-inflight, stopped, no-record}
//
// = 8 cells, each with an EXPLICIT action (no implicit fallthrough). The matrix:
//
//	                | running+inflight | running+idle | stopped/stopping | no-record (orphan)
//	  Reattachable  | reattach         | reattach     | stop+reap        | stop+reap (orphan)
//	  Unavailable   | reap+relaunch    | NOOP         | reap-only        | reap-only (dead orphan)
//
// Key calls (PM-confirmed):
//   - reattach NEVER injects a nudge (claude is alive and mid-task; a nudge would
//     corrupt the in-flight turn). Only a RELAUNCH of an agent with an ACTIVE
//     WorkItem nudges (claude resumed the session-id but may need a push).
//   - desired==stopped/stopping WINS over any in-flight WorkItem (an orphan WI under
//     a stopped agent is the rollback/reset path's job, not boot-resume's).
//   - source set = center resume-set ∪ LOCAL home enumeration: a locally-alive
//     supervisor the center has NO record of is an orphan that must be stopped —
//     only the local enumeration surfaces it (the center never lists it).
//   - Unavailable + running + NO in-flight WI → NOOP: an idle desired-running agent
//     is NOT relaunched (it comes up on the next agent.work); relaunch fires ONLY
//     when there is in-flight work to drive. (For v2.7 the only realistic boot-time
//     Unavailable reasons are dead/missing, for which there is nothing to reap; the
//     theoretical incompatible-idle survivor cannot occur until a protocol bump and
//     is then handled by the reap-before-relaunch on the next work command.)

import "github.com/oopslink/agent-center/internal/supervisormanager"

// bootActionKind enumerates the mutually-exclusive boot actions.
type bootActionKind int

const (
	// bootNoop: do nothing. An idle desired-running agent whose supervisor is gone
	// (comes up on the next agent.work) — we never relaunch an agent with no
	// in-flight work to drive.
	bootNoop bootActionKind = iota
	// bootReattach: a live, compatible supervisor for a desired-running agent —
	// re-attach to it (resume event-pump from its durable offset). NEVER nudges.
	bootReattach
	// bootReapRelaunch: the supervisor is gone/incompatible but the agent is
	// desired-running WITH in-flight work — reap any residual, then relaunch a
	// fresh supervisor (which reads the DURABLE epoch, not 0). Nudge iff an ACTIVE
	// WorkItem is in flight.
	bootReapRelaunch
	// bootStopReap: a LIVE supervisor that must NOT keep running — either the agent
	// is desired-stopped, or it is a local orphan the center has no record of.
	// Stop the supervisor + reap residual.
	bootStopReap
	// bootReapOnly: the supervisor is already gone but residual may linger and the
	// agent must NOT run (desired-stopped, or a dead orphan). Reap residual; do not
	// relaunch.
	bootReapOnly
)

func (k bootActionKind) String() string {
	switch k {
	case bootNoop:
		return "noop"
	case bootReattach:
		return "reattach"
	case bootReapRelaunch:
		return "reap_relaunch"
	case bootStopReap:
		return "stop_reap"
	case bootReapOnly:
		return "reap_only"
	default:
		return "unknown"
	}
}

// bootAction is the decision for one agent: the kind + (relaunch-only) whether to
// inject the resume nudge.
type bootAction struct {
	Kind bootActionKind
	// Nudge is meaningful ONLY for bootReapRelaunch: inject the ResumeNudge because
	// an ACTIVE WorkItem is in flight (claude resumed the session-id but the
	// interrupted turn may need a push — GATE-7 validates). Always false for every
	// other kind (notably reattach, where claude is alive and a nudge would corrupt
	// the in-flight turn).
	Nudge bool
}

// centerRecord is the center's desired view of one agent for boot reconcile (the
// s4 projection of a ResumeAgent). A nil *centerRecord means the CENTER HAS NO
// RECORD of this agent — i.e. it surfaced only from the local home enumeration
// (an orphan).
type centerRecord struct {
	// DesiredLifecycle is the center's desired lifecycle ("running" | "stopped" |
	// "stopping" | ...).
	DesiredLifecycle string
	// HasInflight is true iff the agent has ≥1 in-flight WorkItem (active ∪
	// waiting_input) — the trigger that makes a dead desired-running agent worth
	// relaunching (vs left idle).
	HasInflight bool
	// HasActive is true iff the agent has ≥1 ACTIVE WorkItem — drives the relaunch
	// nudge.
	HasActive bool
}

// wantsRunning reports whether the center desires this agent running. Anything
// other than "running" (stopped/stopping/error/empty) is treated as NOT running;
// stopped/stopping in particular WIN over in-flight work.
func (r *centerRecord) wantsRunning() bool {
	return r != nil && r.DesiredLifecycle == "running"
}

// decideBootAction maps (local supervisor probe state × center record) → the one
// boot action for an agent. PURE: no side effects, no I/O — the whole 8-cell
// matrix is exhaustively unit-testable. rec == nil ⇒ the center has no record
// (orphan); otherwise rec carries the desired lifecycle + in-flight flags.
//
// The two probe states partition the matrix:
//   - Reattachable: a live, compatible supervisor exists locally.
//   - Unavailable:  no live+compatible supervisor (dead / missing / incompatible).
func decideBootAction(probe supervisormanager.ProbeState, rec *centerRecord) bootAction {
	switch probe {
	case supervisormanager.Reattachable:
		// A LIVE local supervisor exists.
		switch {
		case rec == nil:
			// Orphan: locally alive but the center has no record → stop+reap.
			return bootAction{Kind: bootStopReap}
		case rec.wantsRunning():
			// Desired-running + alive → re-attach (idle or busy; NEVER nudge —
			// claude is alive). Covers BOTH running+inflight and running+idle.
			return bootAction{Kind: bootReattach}
		default:
			// Desired-stopped/stopping (or any non-running) WINS → stop the live
			// supervisor + reap, regardless of any orphan in-flight WI.
			return bootAction{Kind: bootStopReap}
		}

	case supervisormanager.Unavailable:
		// NO live+compatible supervisor locally.
		switch {
		case rec == nil:
			// Dead orphan: no center record + nothing live → reap any residual.
			return bootAction{Kind: bootReapOnly}
		case rec.wantsRunning():
			if rec.HasInflight {
				// Desired-running with in-flight work → reap residual + relaunch;
				// nudge iff an ACTIVE WorkItem is in flight.
				return bootAction{Kind: bootReapRelaunch, Nudge: rec.HasActive}
			}
			// Desired-running but IDLE (no in-flight work) → NOOP: do not relaunch;
			// it comes up on the next agent.work.
			return bootAction{Kind: bootNoop}
		default:
			// Desired-stopped/stopping + already gone → reap any residual.
			return bootAction{Kind: bootReapOnly}
		}

	default:
		// Unknown probe state: be conservative — do nothing (never relaunch on an
		// uncategorised state).
		return bootAction{Kind: bootNoop}
	}
}
