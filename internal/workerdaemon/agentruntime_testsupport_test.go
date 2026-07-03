package workerdaemon

// agentruntime_testsupport_test.go — daemon-package test shims that keep the
// pre-0b white-box tests exercising the SAME moved logic, now driven through the
// per-agent runtime (agentruntime testapi wrappers) + the shared SessionState. No
// behavior is added; each shim is a thin delegation to the agent's LocalRuntime.

import (
	"context"
	"time"

	"github.com/oopslink/agent-center/internal/workerdaemon/agentruntime"
)

// installTestAgent installs a runtime-backed managedAgent (fresh SessionState, no
// live session) and returns its state so a test can seed state fields directly. This
// mirrors what bringUpSession does, minus the actual session start.
func (c *AgentController) installTestAgent(agentID string) *agentruntime.SessionState {
	rt, st := c.newRuntimeFor(agentID)
	c.mu.Lock()
	c.agents[agentID] = &managedAgent{agentID: agentID, runtime: rt, state: st}
	c.mu.Unlock()
	return st
}

// --- Reader-goroutine callbacks + turn-end hooks (driven through the runtime). ---

func (c *AgentController) onEvent(agentID string, ev StreamEvent) {
	if rt := c.runtimeFor(agentID); rt != nil {
		rt.OnEvent(ev)
	}
}

func (c *AgentController) maybeReportUsage(agentID string, ev StreamEvent, taskID string) {
	if rt := c.runtimeFor(agentID); rt != nil {
		rt.MaybeReportUsage(agentID, ev, taskID)
	}
}

func (c *AgentController) recordTaskEvent(agentID, taskID string, ev StreamEvent, eventType, payload string, delivered bool) {
	if rt := c.runtimeFor(agentID); rt != nil {
		rt.RecordTaskEvent(agentID, taskID, ev, eventType, payload, delivered)
	}
}

// recordCrashAndSchedule drives the moved crash-recording state machine through the
// shared self-heal store (same behavior as the old controller method).
func (c *AgentController) recordCrashAndSchedule(agentID string, version int, hadWork bool, taskID, model, displayName, promptDescription string, envVars map[string]string, concurrencyEnabled bool, msg string) string {
	return c.selfHeal.RecordCrashAndSchedule(agentruntime.RelaunchSpec{
		AgentID:            agentID,
		Version:            version,
		Nudge:              hadWork,
		TaskID:             taskID,
		Model:              model,
		DisplayName:        displayName,
		PromptDescription:  promptDescription,
		EnvVars:            envVars,
		ConcurrencyEnabled: concurrencyEnabled,
	}, c.now(), msg)
}

// selfHealEntryForTest snapshots the self-heal entry for the state-machine assertions.
func (c *AgentController) selfHealEntryForTest(agentID string) (crashCount int, failed, present bool) {
	return c.selfHeal.EntryForTest(agentID)
}

// --- Pure re-exports (moved to agentruntime; tests keep the old unqualified name). ---

var isRateLimitError = agentruntime.IsRateLimitError
var isTransientAPIError = agentruntime.IsTransientAPIError
var taskIDFromStartTool = agentruntime.TaskIDFromStartTool
var isTaskTerminalTool = agentruntime.IsTaskTerminalTool
var rateLimitResumePayload = agentruntime.RateLimitResumePayload
var apiErrorResumePayload = agentruntime.APIErrorResumePayload

// --- Pure-policy shims: the decide* functions + their param structs moved into
// agentruntime with exported names; these thin wrappers keep the pre-0b pure-policy
// unit tests (which assert the exact curve/cap/reset) compiling + asserting unchanged.

type selfHealParams struct {
	maxAttempts int
	backoffBase time.Duration
	backoffCap  time.Duration
	resetWindow time.Duration
}

type selfHealDecision struct {
	failed     bool
	crashCount int
	backoff    time.Duration
}

func decideSelfHeal(prevCount int, lastRelaunchAt, now time.Time, p selfHealParams) selfHealDecision {
	failed, count, backoff := agentruntime.DecideSelfHeal(prevCount, lastRelaunchAt, now, agentruntime.SelfHealParams{
		MaxAttempts: p.maxAttempts,
		BackoffBase: p.backoffBase,
		BackoffCap:  p.backoffCap,
		ResetWindow: p.resetWindow,
	})
	return selfHealDecision{failed: failed, crashCount: count, backoff: backoff}
}

type rateLimitParams struct {
	defaultBackoff time.Duration
	minBackoff     time.Duration
	maxBackoff     time.Duration
}

func decideRateLimitResume(retryAfterSecs int, resetAtUnix int64, now time.Time, p rateLimitParams) time.Duration {
	return agentruntime.DecideRateLimitResume(retryAfterSecs, resetAtUnix, now, p.defaultBackoff, p.minBackoff, p.maxBackoff)
}

type apiErrorParams struct {
	backoffBase time.Duration
	backoffCap  time.Duration
	maxRetries  int
}

func decideAPIErrorBackoff(attempt int, p apiErrorParams) time.Duration {
	return agentruntime.DecideAPIErrorBackoff(attempt, p.backoffBase, p.backoffCap)
}

var _ = context.Background
