package executor

// activity.go — the executor subsystem's PORT into the agent activity stream
// (ADR-0049 §2 / docs/design/features/agent-concurrent-execution.md).
//
// A concurrently-forked executor is otherwise invisible to the Agent detail
// Activity panel: only the resident claude session streams into the append-only
// AgentActivityEvent flow, so an operator cannot see an executor start, make
// progress, or stop — and "is it still alive?" reduces to "is this flow still
// flowing?". This port closes that gap for the executor lifecycle.
//
// Center coupling lives OUTSIDE this package by design (§3 "监工是唯一连中心的人"):
// exactly like the Writeback port, activity emission is a PORT the center-connected
// worker daemon implements (it holds the Reporter + the resident agent id). This
// package only reports executor-scoped FACTS — the executor_id prefix, the resolved
// task_ref, and the completion classification — and never formats an activity
// payload or talks to the center itself.
//
// Contract: every method is BEST-EFFORT and observational. An implementation MUST
// NOT block the lifecycle or propagate an error back (there is no error return):
// a dropped activity event is a monitoring gap, never a reason to stall or fail a
// real executor. A nil ActivityObserver disables emission entirely.

import "time"

// ActivityObserver receives executor lifecycle observations for the activity
// stream. Implemented by the daemon (which maps each event onto a
// ReportAgentActivity call, tagged with the resident agent id + task_ref).
//
// Start is NOT on this port: the daemon emits it directly at fork time, where the
// routed {cli, model, model_source} + pid are richest and no teardown race exists.
// This port carries the two events the orchestrator-side Monitor owns — the
// terminal stop (its single Finalize point) and the throttled progress heartbeat.
type ActivityObserver interface {
	// ExecutorStopped reports one terminal completion (succeeded / failed /
	// crashed), for BOTH this-process reaps and recovered orphans. Emitted from
	// Monitor.Finalize, the single writeback/teardown point, so every finalized
	// outcome produces exactly one stop.
	ExecutorStopped(StopEvent)
	// ExecutorProgress reports that a running executor advanced (its
	// status.last_progress_at moved). Throttled to change-only by the Monitor so a
	// long-lived executor yields a readable heartbeat, not a per-tick flood.
	ExecutorProgress(ProgressEvent)
}

// StopEvent is the executor-scoped fact set for one terminal completion. The
// daemon renders it into a lifecycle activity payload; this package never formats
// JSON. Reason/Detail come straight from completion.go's classification so there
// is one source of truth for "why did it stop".
type StopEvent struct {
	ExecutorID string
	TaskRef    string
	// Outcome is the dual-signal classification (succeeded | failed | crashed).
	Outcome OutcomeKind
	// Reason is the machine-readable cause: the Completion's ErrorDetail.Kind
	// (nonzero_exit | output_failure | process_gone | clean_exit_no_output | ...),
	// or "stalled" when the watchdog killed it (design §9 "按失败处理"). Empty on
	// a clean success.
	Reason string
	// Detail is the human-readable message (ErrorDetail.Message); empty on success.
	Detail string
	// Retryable mirrors Completion.Retryable (a crash the orchestrator may re-queue).
	Retryable bool
	// Recovered marks the crash-recovery / orphan-cleanup path (Completion.Recovered):
	// the "orphan 清理" stop class, distinct from a reaped this-process exit.
	Recovered bool
	// At is when the stop was observed.
	At time.Time
}

// ProgressEvent is the executor-scoped fact set for one progress heartbeat,
// sampled from the status file (design §9: status.last_progress_at is the liveness
// signal the watchdog already watches). Summary is the executor's optional
// human-readable progress note.
type ProgressEvent struct {
	ExecutorID string
	TaskRef    string
	State      string
	Summary    string
	// Detail is the current-activity note (T880): a short sanitized "what it's doing"
	// hint ("读 task.go", "跑 go test") sampled from status.detail. Empty when the run
	// has not surfaced an activity yet.
	Detail         string
	LastProgressAt time.Time
	At             time.Time
}
