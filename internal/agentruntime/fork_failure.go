package agentruntime

import (
	"errors"
	"fmt"
	"strings"

	"github.com/oopslink/agent-center/internal/agentruntime/modelrouter"
)

// =============================================================================
// fork-fail fail-loud (issue-0186f85e, P0·静默失效).
//
// When an executor fork fails AFTER the task was admitted (start_task ok →
// running) — no executor model resolvable, task.model not allowed, an LLM
// rate-limit, or any other spawn error — the task is running but NO executor
// will ever run it. The old behavior left it FAKE-RUNNING and relied on the
// lease to lapse → re-dispatch: silent, no structured report, no alert. In the
// v2.34.0 dogfood this stalled tasks for hours and got misdiagnosed as a
// hook/code bug (there was no machine-readable signal to say "should have run,
// didn't, because X").
//
// The fix: classify the fork error into a machine-readable cause and block the
// task (running→blocked, retryable) with a structured blocked_reason carrying
// that cause, so upper-layer alerting/diagnosis can distinguish the failure
// modes instead of seeing an opaque runner_failed or a wedged running task.
// =============================================================================

// ForkFailureCause is the machine-readable classification embedded in a
// fork-failed task's blocked_reason (as "[cause=<code>]").
type ForkFailureCause string

const (
	// CauseNoModelResolvable — the model router resolved to nothing (empty
	// allowed_executors + no default/supervisor fallback): modelrouter.ErrNoExecutorModel.
	CauseNoModelResolvable ForkFailureCause = "no_model_resolvable"
	// CauseModelNotAllowed — the task pinned a model not in allowed_executors:
	// modelrouter.ErrModelNotAllowed.
	CauseModelNotAllowed ForkFailureCause = "model_not_allowed"
	// CauseRateLimited — the fork failed against an LLM server-side rate limit /
	// overload / quota (a "should-have-run-but-starved" cause).
	CauseRateLimited ForkFailureCause = "rate_limited"
	// CauseForkError — any other (unclassified) fork/spawn failure.
	CauseForkError ForkFailureCause = "fork_error"
	// CauseRepoSourceUnavailable — the task's repo source could not be materialized
	// (clone/fetch failed every attempt: bad URL, missing credentials, remote down,
	// disk). issue-13e7bfe8: this used to be a SILENT hole — EnsureSource failed on the
	// control path, the task was left queued with only a log line, and the center's
	// wake-sweep re-drives a queued task ONLY while the agent has zero running tasks, so
	// a busy agent stranded it indefinitely. It is now surfaced as a blocked task.
	CauseRepoSourceUnavailable ForkFailureCause = "repo_source_unavailable"
	// CauseRepoRefUnavailable means the mirror is valid but neither a network
	// refresh nor its cached refs can satisfy the requested target ref.
	CauseRepoRefUnavailable ForkFailureCause = "repo_ref_unavailable"
)

// classifyForkFailure maps a fork error to a machine-readable cause so the
// blocked_reason lets alerting distinguish "should-have-run-but-didn't" causes
// (no model / model-not-allowed / rate-limited) from a generic fork error. The
// sentinel checks (errors.Is) come first; the rate-limit heuristic is a string
// match because a rate-limit surfaces as an opaque wrapped error, not a sentinel.
func classifyForkFailure(err error) ForkFailureCause {
	switch {
	case err == nil:
		return CauseForkError
	case errors.Is(err, modelrouter.ErrNoExecutorModel):
		return CauseNoModelResolvable
	case errors.Is(err, modelrouter.ErrModelNotAllowed):
		return CauseModelNotAllowed
	case looksRateLimited(err):
		return CauseRateLimited
	default:
		return CauseForkError
	}
}

// looksRateLimited reports whether err reads like an LLM server-side rate-limit /
// overload / quota exhaustion. Case-insensitive substring match over the common
// signatures (429, "rate limit", "too many requests", "overloaded", "quota").
func looksRateLimited(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, m := range []string{"rate limit", "rate_limit", "ratelimit", "too many requests", "429", "overloaded", "quota"} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// forkFailureReason renders the structured, machine-readable blocked_reason:
//
//	executor fork failed [cause=<code>]: <detail>
//
// The bracketed "[cause=<code>]" token is stable and greppable — upper-layer
// alerting keys off it to distinguish the fork-fail modes. Mirrors the existing
// "[runner_failed]" bracket convention used for post-fork executor exits.
func forkFailureReason(cause ForkFailureCause, err error) string {
	return fmt.Sprintf("executor fork failed [cause=%s]: %v", cause, err)
}
