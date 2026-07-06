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

// RecordTaskEvent exposes the W3/W4 local task sink for tests.
func (r *LocalRuntime) RecordTaskEvent(agentID, taskID string, ev claudestream.StreamEvent, eventType, payload string, delivered bool) {
	r.recordTaskEvent(agentID, taskID, ev, eventType, payload, delivered)
}

// OnEvent exposes the reader-goroutine event callback for tests that drive it
// directly (rather than via a fake session's emit).
func (r *LocalRuntime) OnEvent(ev claudestream.StreamEvent) { r.onEvent(ev) }

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

// SeedOrphanForTest registers an adopted orphan on the attached executor engine, a
// deterministic way for the daemon's SnapshotConcurrency aggregate test to make one
// executor appear (the engine internals are unexported). No-op with no engine.
func (r *LocalRuntime) SeedOrphanForTest(id string, pid int) {
	if ee := r.execEngine(); ee != nil {
		ee.addOrphan(id, pid)
	}
}
