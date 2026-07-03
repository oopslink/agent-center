package agentruntime

// events.go — the stdout→activity sink (onEvent) and the exactly-once exit
// coordinator (onExit), moved off AgentController. They run on the SESSION READER
// GOROUTINE and mutate SessionState under the SHARED mutex — the same fields, the
// same critical sections as before the move (reviewer redline).

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/claudestream"
	"github.com/oopslink/agent-center/internal/workerdaemon/sessioninstance"
)

// onEvent maps a parsed StreamEvent to a ReportAgentActivity call + the local
// W3/W4 task sink + the L2 no-silent-failure surface + the turn-end hooks.
func (r *LocalRuntime) onEvent(ev claudestream.StreamEvent) {
	agentID := r.cfg.AgentID
	r.cfg.Mu.Lock()
	st := r.state
	var workItemRef, toolName string
	var rlRetryAfter int
	var rlResetAt int64
	var taskForEvent string
	var clearEventTask bool
	workItemRef = st.CurrentTaskID
	switch ev.Type {
	case "tool_use":
		if ev.ToolUseID != "" {
			if st.ToolNames == nil {
				st.ToolNames = map[string]string{}
			}
			st.ToolNames[ev.ToolUseID] = ev.ToolName
		}
		if tid := taskIDFromStartTool(ev.ToolName, ev.ToolInput); tid != "" {
			st.EventTaskID = tid
		}
		if isTaskTerminalTool(ev.ToolName) {
			clearEventTask = true
		}
	case "tool_result":
		toolName = st.ToolNames[ev.ToolUseID]
	case "rate_limit":
		st.RLRetryAfterSecs = ev.RetryAfterSecs
		st.RLResetAtUnix = ev.ResetAtUnix
	case "system":
		st.ToolNames = nil
		if ev.Subtype == "init" {
			st.RLRetryAfterSecs, st.RLResetAtUnix = 0, 0
		}
	case "result":
		st.ToolNames = nil
		rlRetryAfter, rlResetAt = st.RLRetryAfterSecs, st.RLResetAtUnix
		st.RLRetryAfterSecs, st.RLResetAtUnix = 0, 0
	}
	taskForEvent = st.EventTaskID
	r.cfg.Mu.Unlock()

	payload, err := json.Marshal(StreamActivityPayload(ev, toolName))
	reportOK := false
	if err != nil {
		r.log("activity agent=%s marshal event: %v", agentID, err)
	} else if rerr := r.cfg.Reporter.ReportAgentActivity(
		context.Background(), agentID, ActivityEventType(ev), string(payload),
		workItemRef, "", time.Now(),
	); rerr != nil {
		r.log("activity agent=%s report: %v", agentID, rerr)
	} else {
		reportOK = true
	}

	routeTask := taskForEvent
	if routeTask == "" {
		routeTask = workItemRef
	}
	if err == nil {
		r.recordTaskEvent(agentID, routeTask, ev, ActivityEventType(ev), string(payload), reportOK)
	}
	if clearEventTask {
		r.cfg.Mu.Lock()
		st.LastEventTaskID = st.EventTaskID
		st.EventTaskID = ""
		r.cfg.Mu.Unlock()
	}

	if ev.Type == "result" && ev.IsError {
		if r.maybeScheduleRateLimitResume(agentID, ev, rlRetryAfter, rlResetAt) {
			return
		}
		if r.maybeScheduleAPIErrorResume(agentID, ev) {
			return
		}
		r.surfaceTurnFailure(agentID, ev)
	}

	if ev.Type == "result" && !ev.IsError {
		r.resetAPIErrorRetries(agentID)
		var isCodex bool
		r.cfg.Mu.Lock()
		isCodex = st.CLI == CLICodex
		r.cfg.Mu.Unlock()
		if !isCodex {
			if home, _, _, pathErr := r.agentPaths(agentID); pathErr == nil {
				r.cfg.BG.Add(1)
				go func() {
					defer r.cfg.BG.Done()
					if merrr := sessioninstance.MarkCompletedTurn(home); merrr != nil {
						r.log("restart-recovery: MarkCompletedTurn(%s) failed: %v", agentID, merrr)
					}
				}()
			}
		}
		go r.maybeReplyNudge(agentID)
		var usageTaskID string
		r.cfg.Mu.Lock()
		usageTaskID = st.UsageTaskAtResult()
		r.cfg.Mu.Unlock()
		go r.maybeReportUsage(agentID, ev, usageTaskID)
	}
}

// onExit coordinates the exactly-once lifecycle report on session exit (three exit
// kinds). Crash path deletes the managedAgent (via RemoveAgent under the lock) and
// records + schedules a self-heal relaunch — byte-identical to before.
func (r *LocalRuntime) onExit(exitErr error) {
	agentID := r.cfg.AgentID
	r.cfg.Mu.Lock()
	st := r.state
	detaching := st.Detaching
	expected := st.ExpectedStop
	version := st.Version
	hadWork := st.HadWork
	taskID := st.CurrentTaskID
	model := st.Model
	displayName := st.DisplayName
	promptDescription := st.PromptDescription
	envVars := cloneEnv(st.EnvVars)
	cli := st.CLI
	wasConcurrent := r.cfg.ExecActive != nil && r.cfg.ExecActive(agentID)
	taskLog := st.TaskLog
	st.TaskLog, st.TaskLogID = nil, ""
	if r.cfg.RemoveAgent != nil {
		r.cfg.RemoveAgent(agentID)
	}
	r.cfg.Mu.Unlock()

	if taskLog != nil {
		_ = taskLog.Close()
	}

	if detaching {
		r.log("agent=%s detached (supervisor + claude survive for re-attach)", agentID)
		return
	}
	if expected {
		r.log("agent=%s exited (expected stop)", agentID)
		return
	}

	msg := ""
	if exitErr != nil {
		msg = exitErr.Error()
	} else {
		msg = "process exited unexpectedly"
	}
	r.log("agent=%s crashed: %s", agentID, msg)
	if cli == CLICodex {
		st.LifecycleOnce.Do(func() {
			if err := r.cfg.Reporter.ReportAgentLifecycle(context.Background(), agentID, "error", msg, time.Now()); err != nil {
				r.log("agent=%s (codex) report error: %v", agentID, err)
			}
		})
		return
	}
	state := r.cfg.SelfHeal.RecordCrashAndSchedule(RelaunchSpec{
		AgentID:            agentID,
		Version:            version,
		Nudge:              hadWork,
		TaskID:             taskID,
		Model:              model,
		DisplayName:        displayName,
		PromptDescription:  promptDescription,
		EnvVars:            envVars,
		ConcurrencyEnabled: wasConcurrent,
	}, r.now(), msg)
	if state != "" {
		st.LifecycleOnce.Do(func() {
			if err := r.cfg.Reporter.ReportAgentLifecycle(context.Background(), agentID, state, msg, time.Now()); err != nil {
				r.log("agent=%s report %s: %v", agentID, state, err)
			}
		})
	}
}

// maybeReplyNudge is the worker half of the reply-guardrail turn-end hook (T341).
func (r *LocalRuntime) maybeReplyNudge(agentID string) {
	r.cfg.Mu.Lock()
	sess := r.state.Session
	inConverse := r.state.CurrentConversationID != ""
	r.cfg.Mu.Unlock()
	if sess == nil || inConverse {
		return
	}
	if r.cfg.Reporter == nil {
		return
	}
	ctx := context.Background()
	prompts, err := r.cfg.Reporter.FetchReplyNudges(ctx, agentID)
	if err != nil {
		r.log("reply-guardrail agent=%s fetch nudges: %v", agentID, err)
		return
	}
	for _, p := range prompts {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if err := sess.Inject(ctx, p); err != nil {
			r.log("reply-guardrail agent=%s inject nudge: %v", agentID, err)
			return
		}
		r.log("reply-guardrail agent=%s injected directed-reply nudge", agentID)
	}
}

// MaybeReplyNudge is the daemon-facing entry (boot relaunch re-triggers the guardrail).
func (r *LocalRuntime) MaybeReplyNudge(agentID string) { r.maybeReplyNudge(agentID) }

// maybeReportUsage ships the result line's token totals to the center (F2).
func (r *LocalRuntime) maybeReportUsage(agentID string, ev claudestream.StreamEvent, taskID string) {
	if (r.cfg.DisableUsageReport != nil && r.cfg.DisableUsageReport()) || r.cfg.Reporter == nil {
		return
	}
	if ev.TokensIn == 0 && ev.TokensOut == 0 && ev.CacheReadTokens == 0 && ev.CacheWriteTokens == 0 {
		return
	}
	r.cfg.Mu.Lock()
	model := r.state.Model
	r.cfg.Mu.Unlock()
	if model == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.cfg.Reporter.ReportUsage(ctx, UsageReport{
		AgentID:          agentID,
		Model:            model,
		TaskID:           taskID,
		InputTokens:      ev.TokensIn,
		OutputTokens:     ev.TokensOut,
		CacheReadTokens:  ev.CacheReadTokens,
		CacheWriteTokens: ev.CacheWriteTokens,
		At:               time.Now(),
	}); err != nil {
		r.log("report_usage agent=%s: %v", agentID, err)
	}
}

// surfaceTurnFailure fails the in-flight WorkItem after an is_error turn (L2).
func (r *LocalRuntime) surfaceTurnFailure(agentID string, ev claudestream.StreamEvent) {
	r.cfg.Mu.Lock()
	wiID := r.state.CurrentTaskID
	convID := r.state.CurrentConversationID
	r.cfg.Mu.Unlock()

	if wiID == "" {
		if convID != "" {
			r.surfaceConverseFailure(agentID, convID, ev)
			return
		}
		r.log("L2 agent=%s is_error turn with NO in-flight WorkItem (subtype=%q) — surfaced as warning, not silently dropped", agentID, ev.Subtype)
		return
	}

	r.log("L2 agent=%s work_item=%s failed (is_error turn, subtype=%q)", agentID, wiID, ev.Subtype)

	r.cfg.Mu.Lock()
	if r.state.CurrentTaskID == wiID {
		r.state.CurrentTaskID = ""
	}
	r.cfg.Mu.Unlock()
}

// surfaceConverseFailure posts a VISIBLE system message into a failed converse
// turn's conversation (UX Rule 9).
func (r *LocalRuntime) surfaceConverseFailure(agentID, convID string, ev claudestream.StreamEvent) {
	if err := r.cfg.Reporter.ReportConverseError(
		context.Background(), agentID, convID, converseErrorSummary(ev), time.Now(),
	); err != nil {
		r.log("L2 agent=%s conv=%s converse-error report: %v", agentID, convID, err)
		return
	}
	r.log("L2 agent=%s conv=%s converse turn failed (is_error, subtype=%q) — system notice posted", agentID, convID, ev.Subtype)
	r.cfg.Mu.Lock()
	if r.state.CurrentConversationID == convID {
		r.state.CurrentConversationID = ""
	}
	r.cfg.Mu.Unlock()
}
