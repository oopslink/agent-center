package agentruntime

// testapi.go — exported thin wrappers over moved-logic internals so the daemon's
// same-behavior unit tests (which live in package workerdaemon and drive these via
// the runtime) keep exercising the identical code paths after the 0b relocation.
// These are pure delegations; they add no behavior.

import (
	"encoding/json"
	"time"

	"github.com/oopslink/agent-center/internal/claudestream"
)

// MaybeReportUsage exposes the turn-end usage hook for daemon-level tests.
func (r *LocalRuntime) MaybeReportUsage(agentID string, ev claudestream.StreamEvent, taskID string) {
	r.maybeReportUsage(agentID, ev, taskID)
}

// SurfaceTurnFailure exposes the L2 failure surface for daemon-level tests.
func (r *LocalRuntime) SurfaceTurnFailure(agentID string, ev claudestream.StreamEvent) {
	r.surfaceTurnFailure(agentID, ev)
}

// MaybeScheduleRateLimitResume exposes the rate-limit resume scheduler for tests.
func (r *LocalRuntime) MaybeScheduleRateLimitResume(agentID string, ev claudestream.StreamEvent, retryAfterSecs int, resetAtUnix int64) bool {
	return r.maybeScheduleRateLimitResume(agentID, ev, retryAfterSecs, resetAtUnix)
}

// MaybeScheduleAPIErrorResume exposes the transient-API-error resume scheduler for tests.
func (r *LocalRuntime) MaybeScheduleAPIErrorResume(agentID string, ev claudestream.StreamEvent) bool {
	return r.maybeScheduleAPIErrorResume(agentID, ev)
}

// ResetAPIErrorRetries exposes the clean-turn retry-budget reset for tests.
func (r *LocalRuntime) ResetAPIErrorRetries(agentID string) { r.resetAPIErrorRetries(agentID) }

// RecordTaskEvent exposes the W3/W4 local task sink for tests.
func (r *LocalRuntime) RecordTaskEvent(agentID, taskID string, ev claudestream.StreamEvent, eventType, payload string, delivered bool) {
	r.recordTaskEvent(agentID, taskID, ev, eventType, payload, delivered)
}

// OnEvent / OnExit expose the reader-goroutine callbacks for tests that drive them
// directly (rather than via a fake session's emit).
func (r *LocalRuntime) OnEvent(ev claudestream.StreamEvent) { r.onEvent(ev) }
func (r *LocalRuntime) OnExit(err error)                    { r.onExit(err) }

// RateLimitResumePayload / APIErrorResumePayload expose the activity payload builders.
func RateLimitResumePayload(ev claudestream.StreamEvent, retryAfterSecs int, resetAtUnix int64, resumeAt time.Time) string {
	return rateLimitResumePayload(ev, retryAfterSecs, resetAtUnix, resumeAt)
}
func APIErrorResumePayload(ev claudestream.StreamEvent, attempt, maxRetries int, resumeAt time.Time) string {
	return apiErrorResumePayload(ev, attempt, maxRetries, resumeAt)
}

// TaskIDFromStartTool / IsTaskTerminalTool expose the tool classifiers.
func TaskIDFromStartTool(toolName string, toolInput json.RawMessage) string {
	return taskIDFromStartTool(toolName, toolInput)
}
func IsTaskTerminalTool(toolName string) bool { return isTaskTerminalTool(toolName) }
