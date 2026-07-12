package projectmanager

// Stage status is DERIVED, never stored (2026-07-03 plan-stage-model design §4.1 —
// "status 不落库、是投影"). There is no second state machine advanced independently of
// the member nodes: a stage's status is a pure function of (a) its member nodes'
// statuses and (b) its gate's resolution state. This file is that pure projection —
// PURE domain, no I/O, no orchestration import (the service maps the live graph node
// statuses onto these small enums and calls ProjectStageStatus).

// StageStatus is the projected lifecycle of a Stage (§4.1). It is computed, not
// stored, so there is no state-machine/transition table — every read recomputes it
// from the current member + gate state.
type StageStatus string

const (
	// StageOpen — no member node has started yet (全未起, §4.1).
	StageOpen StageStatus = "open"
	// StageRunning — a member is running, or members are done but the gate is still
	// undecided (有成员 running / gate 未决, §4.1).
	StageRunning StageStatus = "running"
	// StageReopen — a gate reject re-opened this stage's sub-DAG for another bounded
	// round (gate reject 触发 reopen, §4.1).
	StageReopen StageStatus = "reopen"
	// StageDone — every member is terminal AND the gate has passed (or there is no
	// gate: a pure barrier just waits for全完成) (成员全 completed 且 gate 通过, §4.1).
	StageDone StageStatus = "done"
)

// StageMemberState is the coarse per-member-node state the projection consumes. The
// service derives it from the orchestration node status: an open/reopened node is
// StageMemberOpen unless it has begun (running); a completed/discarded node is
// StageMemberDone (discarded = a pruned/skipped branch counts as satisfied-terminal,
// mirroring the engine's ReadyNodes/IsAutoDone treatment).
type StageMemberState string

const (
	StageMemberOpen    StageMemberState = "open"    // not started (open/reopen, not yet running)
	StageMemberRunning StageMemberState = "running" // in flight
	StageMemberDone    StageMemberState = "done"    // terminal (completed or discarded)
)

// StageGateState is the coarse gate resolution the projection consumes. The service
// derives it from the gate CONDITION node: absent (no gate) → StageGateNone; resolved
// success → StageGatePassed; a reopened (bounded reject) gate → StageGateReopened;
// otherwise (created, awaiting acceptance) → StageGatePending.
type StageGateState string

const (
	StageGateNone     StageGateState = "none"     // stage has no acceptance gate (pure barrier)
	StageGatePending  StageGateState = "pending"  // gate exists, not yet resolved
	StageGatePassed   StageGateState = "passed"   // gate resolved success → downstream released
	StageGateReopened StageGateState = "reopened" // gate reject re-ran the stage sub-DAG
)

// ProjectStageStatus computes a Stage's DERIVED status (§4.1) from its member nodes'
// coarse states and its gate's resolution state. It is the single source of the stage
// status projection — the get_stage API and the stage-level progress render both go
// through it, so the "no second state machine" invariant holds by construction.
//
// An empty stage (no members) is StageOpen (nothing has started; a gate cannot pass
// with no members to accept).
func ProjectStageStatus(members []StageMemberState, gate StageGateState) StageStatus {
	// A reject-driven reopen dominates: the stage is being re-run for another round
	// regardless of individual member progress (§4.1 reopen).
	if gate == StageGateReopened {
		return StageReopen
	}
	if len(members) == 0 {
		return StageOpen
	}
	allDone := true
	anyRunning := false
	anyStarted := false
	for _, m := range members {
		switch m {
		case StageMemberDone:
			anyStarted = true
		case StageMemberRunning:
			anyRunning = true
			anyStarted = true
			allDone = false
		default: // StageMemberOpen
			allDone = false
		}
	}
	if allDone {
		// 全 completed: done only once the gate passes (or there is no gate — a pure
		// barrier is done at全完成). A pending gate keeps the stage running (awaiting
		// acceptance, §4.1 "gate 未决 → running").
		switch gate {
		case StageGateNone, StageGatePassed:
			return StageDone
		default: // StageGatePending
			return StageRunning
		}
	}
	if anyRunning || anyStarted {
		return StageRunning
	}
	return StageOpen
}
