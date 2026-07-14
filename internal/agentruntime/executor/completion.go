package executor

// completion.go — F5 (completion signal) dual-signal classification (design §9).
//
// The orchestrator NEVER trusts a single signal. It correlates THREE facts —
// the process exit code, the presence+content of output.json, and the status
// file — into exactly one Outcome:
//
//	exit == 0 AND output.json present(+success)        → Succeeded
//	exit != 0                                          → Failed   (detail from status.error)
//	process gone but status still "running" / no output → Crashed  (retryable)
//
// Classification is a PURE function over harvested facts (CompletionFacts) so the
// three determinations are exhaustively table-testable without spawning anything.
// The orchestrator's side effects (writeback, worktree teardown, pool release)
// live in monitor.go and consume the Completion this produces.

import "fmt"

// OutcomeKind is how an executor finished, as judged by the dual signal (§9).
type OutcomeKind string

const (
	// OutcomeRunning — not a completion: the process is still alive (used by crash
	// recovery to mark an orphan that must be re-adopted, design §12), never the
	// result of an observed exit.
	OutcomeRunning OutcomeKind = "running"
	// OutcomeSucceeded — exit 0 AND a valid success output.json (§9 case 1).
	OutcomeSucceeded OutcomeKind = "succeeded"
	// OutcomeFailed — a definite, non-retryable failure (§9 case 2): the executor
	// exited non-zero, or wrote an explicit failure output/status.
	OutcomeFailed OutcomeKind = "failed"
	// OutcomeCrashed — an anomaly the orchestrator may retry (§9 case 3): the
	// process vanished while status was still "running", or exited cleanly without
	// the output it promised.
	OutcomeCrashed OutcomeKind = "crashed"
)

// Completion is the orchestrator's resolved view of a finished executor (design
// §11.2 step f). It carries everything the unified writeback (step g) needs:
// the classification, the harvested artifacts, and the resolved error detail.
type Completion struct {
	ExecutorID string
	Kind       OutcomeKind
	// Output is the harvested output.json (nil when absent/invalid).
	Output *Output
	// Status is the last status file read (nil when absent/invalid); source of the
	// chat-relay summary and, for failures, the error detail.
	Status *Status
	// Error is the resolved failure detail for Failed/Crashed (nil for Succeeded/
	// Running). Always carries a human-readable message (conventions §16).
	Error *ErrorDetail
	// Retryable is true iff Kind == OutcomeCrashed: the orchestrator MAY re-queue
	// the work rather than reporting a terminal failure.
	Retryable bool
	// Recovered marks a completion observed via the crash-recovery / orphan path
	// (Monitor.Recover / Monitor.CheckOrphan) rather than a reaped this-process exit
	// (Monitor.AwaitCompletion). It is NOT set by Classify — the observing Monitor
	// method stamps it — so the pure classification stays orthogonal to how the
	// completion was witnessed. The activity stream surfaces it as the "orphan 清理"
	// stop class (design: executor lifecycle observability).
	Recovered bool
	// Git is the structured git state of the executor's worktree, probed ONCE at
	// finalize (before the writeback) and carried to the center so the delivery-audit
	// signal — did this executor push its work? — reaches the center's task record
	// (issue-f30b7e7b). nil for a Running outcome or when the probe could not run
	// (non-git / unresolvable workspace). The Monitor stamps it; Classify leaves it
	// nil (pure classification stays orthogonal to the git side-effect probe). It is
	// the SAME value persisted in the local `finalized` marker — one probe, reused.
	Git *FinalizedGitStatus
}

// CompletionFacts are the raw observations the classifier reasons over. The
// orchestrator harvests them two ways: on a live executor it observed exit
// (Exited=true, ExitErr set), or on crash recovery it found an orphan dir and
// probed liveness (Exited=false, Alive set).
type CompletionFacts struct {
	ExecutorID string
	// Exited reports the live path: we reaped the process (Wait returned) and
	// ExitErr is its exit status. False is the recovery/orphan path.
	Exited bool
	// ExitErr is the reaped exit status when Exited; nil means exit 0.
	ExitErr error
	// Alive reports, on the recovery path (!Exited), whether the executor process
	// is still running (probed by pid). Ignored when Exited.
	Alive bool
	// Output is output.json if present+valid, else nil. HasOutput disambiguates a
	// genuinely-absent output from a nil pointer.
	Output    *Output
	HasOutput bool
	// Status is the status file if present+valid, else nil.
	Status *Status
}

// Classify maps the harvested facts to exactly one Completion (design §9). It is
// total: every combination yields a defined Outcome, so an unforeseen state can
// never be silently dropped (conventions §17).
func Classify(f CompletionFacts) Completion {
	c := Completion{ExecutorID: f.ExecutorID, Output: f.Output, Status: f.Status}
	if f.Exited {
		return classifyExited(f, c)
	}
	return classifyOrphan(f, c)
}

// classifyExited handles the live path: we observed the process exit.
func classifyExited(f CompletionFacts, c Completion) Completion {
	if f.ExitErr == nil { // exit 0 — half the success signal; the other half is output.json
		switch {
		case f.HasOutput && f.Output.Success:
			c.Kind = OutcomeSucceeded
			return c
		case f.HasOutput && !f.Output.Success:
			// Exit 0 but the executor explicitly reported failure in output.json. Trust
			// the explicit failure over the exit code — a definite (non-retryable) fail.
			c.Kind = OutcomeFailed
			c.Error = resolveError(f, "output_failure", "executor reported failure in output.json despite exit 0")
			return c
		default:
			// Exit 0 but NO output.json: the executor exited cleanly without producing
			// the result it owed. Anomalous → crashed/retryable, not a silent success.
			c.Kind = OutcomeCrashed
			c.Retryable = true
			c.Error = resolveError(f, "clean_exit_no_output", "executor exited 0 without writing output.json")
			return c
		}
	}
	// Non-zero exit → definite failure (§9 case 2). Detail precedence: status.error,
	// then output.error, then the exit error itself.
	c.Kind = OutcomeFailed
	c.Error = resolveError(f, "nonzero_exit", fmt.Sprintf("executor exited with error: %v", f.ExitErr))
	return c
}

// classifyOrphan handles crash recovery: no exit observed, liveness was probed.
func classifyOrphan(f CompletionFacts, c Completion) Completion {
	// A still-alive orphan is not a completion — the orchestrator re-adopts it
	// (design §12: rebuild "在管哪些 executor" without re-spawning).
	if f.Alive {
		c.Kind = OutcomeRunning
		return c
	}
	// The process is gone. Decide from the durable files.
	switch {
	case f.HasOutput && f.Output.Success:
		// It finished successfully before we noticed (e.g. exited during our downtime).
		c.Kind = OutcomeSucceeded
		return c
	case f.HasOutput && !f.Output.Success:
		c.Kind = OutcomeFailed
		c.Error = resolveError(f, "output_failure", "executor reported failure in output.json")
		return c
	case f.Status != nil && f.Status.State == StateFailed:
		c.Kind = OutcomeFailed
		c.Error = resolveError(f, "status_failed", "executor status=failed")
		return c
	case f.Status != nil && f.Status.State == StateDone:
		// Status claims done but no output.json — torn/incomplete terminal write.
		c.Kind = OutcomeCrashed
		c.Retryable = true
		c.Error = resolveError(f, "done_no_output", "executor status=done but output.json missing")
		return c
	default:
		// The core §9 case 3: process gone while status was still "running" (or no
		// status at all). Treat as a crash the orchestrator may retry.
		c.Kind = OutcomeCrashed
		c.Retryable = true
		c.Error = resolveError(f, "process_gone", "executor process gone while status still running")
		return c
	}
}

// resolveError picks the most specific error detail available: an explicit
// status.error, then output.error, else the synthesized (kind, message) fallback.
// It always returns a fully-populated ErrorDetail (both halves present).
func resolveError(f CompletionFacts, fallbackKind, fallbackMsg string) *ErrorDetail {
	if f.Status != nil && f.Status.Error != nil && f.Status.Error.Validate() == nil {
		return f.Status.Error
	}
	if f.Output != nil && f.Output.Error != nil && f.Output.Error.Validate() == nil {
		return f.Output.Error
	}
	return &ErrorDetail{Kind: fallbackKind, Message: fallbackMsg}
}
