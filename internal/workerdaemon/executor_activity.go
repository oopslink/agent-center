package workerdaemon

// executor_activity.go — the daemon-side bridge that makes concurrently-forked
// executors first-class producers of the AgentActivityEvent stream (ADR-0049 /
// T758). The executor subsystem (F1–F5) is deliberately center-free; the ONLY
// object holding both the center Reporter (c.cfg.Reporter.ReportAgentActivity) and
// the per-agent executor plumbing is AgentController, so the executor→activity
// bridge lives here.
//
// Three event kinds, all emitted as agent.EventTypeLifecycle so the Web Console
// Agent-detail Activity panel renders them in its "Control" category (never folded
// into the Checking group) with zero frontend change — the row preview reads
// payload.event + payload.scope:
//
//	executor.start     — emitted at fork time (launchExecutor): pid + routed cli/model.
//	executor.stop      — one per terminal completion, via the executor.ActivityObserver
//	                     port off Monitor.Finalize (this-process reap, orphan poll, and
//	                     crash recovery all funnel through it). Distinguishes the four
//	                     stop classes: succeeded / failed(exit) / stalled(watchdog) /
//	                     orphan(recovered) — reusing completion.go's classification.
//	executor.progress  — throttled heartbeat, via the same port off Monitor.SampleProgress.
//
// Every payload carries executor_id + task_ref as the identifying prefix (design
// point 3), and each event is tagged with interaction_ref "executor:<id>" so the UI
// groups one executor's activity together. Emission is best-effort: a report error
// is logged, never propagated (it must not perturb the executor lifecycle).

import (
	"context"
	"encoding/json"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
	"github.com/oopslink/agent-center/internal/workerdaemon/orchestrator"
)

// executorActivityObserver implements executor.ActivityObserver for one agent,
// mapping the executor-scoped stop/progress facts onto ReportAgentActivity calls
// tagged with that agent's id. Constructed per executorEngine (per agent).
type executorActivityObserver struct {
	c       *AgentController
	agentID string
}

var _ executor.ActivityObserver = executorActivityObserver{}

// ExecutorStopped emits one terminal executor.stop lifecycle event.
func (o executorActivityObserver) ExecutorStopped(ev executor.StopEvent) {
	o.c.emitExecutorLifecycle(o.agentID, ev.ExecutorID, ev.TaskRef, executorStopPayload(ev), stampOr(ev.At, o.c.now))
}

// ExecutorProgress emits one executor.progress lifecycle heartbeat.
func (o executorActivityObserver) ExecutorProgress(ev executor.ProgressEvent) {
	o.c.emitExecutorLifecycle(o.agentID, ev.ExecutorID, ev.TaskRef, executorProgressPayload(ev), stampOr(ev.At, o.c.now))
}

// emitExecutorStart emits the executor.start lifecycle event right after a
// successful fork (launchExecutor), where the routed cli/model + pid are richest.
func (c *AgentController) emitExecutorStart(agentID, taskRef, title string, launched *orchestrator.Launched) {
	if launched == nil {
		return
	}
	pid := 0
	if launched.Handle != nil {
		pid = launched.Handle.PID
	}
	payload := executorStartPayload(executorStartFields{
		ExecutorID:  launched.ExecutorID,
		TaskRef:     taskRef,
		PID:         pid,
		CLI:         launched.CLI,
		Model:       launched.Model,
		ModelSource: string(launched.ModelSource),
		ProblemID:   launched.ProblemID,
		Title:       title,
	})
	c.emitExecutorLifecycle(agentID, launched.ExecutorID, taskRef, payload, c.now())
}

// emitExecutorLifecycle marshals payload and posts it as a lifecycle activity for
// agentID, tagged with task_ref + the executor's interaction ref. Best-effort:
// a marshal/report failure is logged, never returned (activity is observational).
func (c *AgentController) emitExecutorLifecycle(agentID, execID, taskRef string, payload map[string]any, at time.Time) {
	b, err := json.Marshal(payload)
	if err != nil {
		c.log("agent=%s executor=%s lifecycle activity marshal: %v", agentID, execID, err)
		return
	}
	if err := c.cfg.Reporter.ReportAgentActivity(
		context.Background(), agentID, agent.EventTypeLifecycle, string(b),
		taskRef, executorInteractionRef(execID), at,
	); err != nil {
		c.log("agent=%s executor=%s lifecycle activity report: %v", agentID, execID, err)
	}
}

// executorInteractionRef is the activity interaction_ref grouping one executor's
// events in the UI (design point 3: "一眼区分是哪个 executor").
func executorInteractionRef(execID string) string { return "executor:" + execID }

// executorStartFields is the executor.start payload input (kept explicit so the
// pure builder below is table-testable without a Launched/Handle).
type executorStartFields struct {
	ExecutorID  string
	TaskRef     string
	PID         int
	CLI         string
	Model       string
	ModelSource string
	ProblemID   string
	Title       string
}

// executorStartPayload builds the executor.start lifecycle payload. Always carries
// the executor_id + task_ref prefix; scope=model gives the row a readable preview
// ("executor.start (claude-...)"). Empty optional fields are omitted.
func executorStartPayload(f executorStartFields) map[string]any {
	p := map[string]any{
		"event":       "executor.start",
		"executor_id": f.ExecutorID,
		"task_ref":    f.TaskRef,
		"pid":         f.PID,
		"cli":         f.CLI,
		"model":       f.Model,
		"scope":       f.Model,
	}
	putIfSet(p, "model_source", f.ModelSource)
	putIfSet(p, "problem_id", f.ProblemID)
	putIfSet(p, "title", f.Title)
	return p
}

// executorStopPayload builds the executor.stop lifecycle payload from the classified
// completion. outcome=Kind (succeeded|failed|crashed); reason=Error.Kind (or
// "stalled" for a watchdog kill); recovered flags the orphan-cleanup path. scope
// summarizes outcome[:reason] for the row preview ("executor.stop (failed:stalled)").
func executorStopPayload(ev executor.StopEvent) map[string]any {
	outcome := string(ev.Outcome)
	scope := outcome
	if ev.Reason != "" {
		scope = outcome + ":" + ev.Reason
	}
	p := map[string]any{
		"event":       "executor.stop",
		"executor_id": ev.ExecutorID,
		"task_ref":    ev.TaskRef,
		"outcome":     outcome,
		"retryable":   ev.Retryable,
		"recovered":   ev.Recovered,
		"scope":       scope,
	}
	putIfSet(p, "reason", ev.Reason)
	putIfSet(p, "detail", ev.Detail)
	return p
}

// executorProgressPayload builds the executor.progress heartbeat payload. Sourced
// from the status snapshot (state + last_progress_at + optional summary); scope=state
// gives the preview ("executor.progress (running)").
func executorProgressPayload(ev executor.ProgressEvent) map[string]any {
	p := map[string]any{
		"event":       "executor.progress",
		"executor_id": ev.ExecutorID,
		"task_ref":    ev.TaskRef,
		"state":       ev.State,
		"scope":       ev.State,
	}
	if !ev.LastProgressAt.IsZero() {
		p["last_progress_at"] = ev.LastProgressAt.UTC().Format(time.RFC3339)
	}
	putIfSet(p, "summary", ev.Summary)
	return p
}

// putIfSet adds k=v only when v is non-empty (omitempty for map payloads).
func putIfSet(p map[string]any, k, v string) {
	if v != "" {
		p[k] = v
	}
}

// stampOr returns at when set, else now() — the Observer events already carry the
// Monitor's clock time, but this guards a zero timestamp.
func stampOr(at time.Time, now func() time.Time) time.Time {
	if !at.IsZero() {
		return at
	}
	if now != nil {
		return now()
	}
	return at
}
