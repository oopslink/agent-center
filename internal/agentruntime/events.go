package agentruntime

// events.go — the stdout→activity sink (onEvent) and the exactly-once exit
// coordinator (onExit), moved off AgentController. They run on the SESSION READER
// GOROUTINE and mutate SessionState under the SHARED mutex — the same fields, the
// same critical sections as before the move (reviewer redline).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/sessioninstance"
	"github.com/oopslink/agent-center/internal/claudestream"
)

// onEvent maps a parsed StreamEvent to a ReportAgentActivity call + the local
// W3/W4 task sink + the L2 no-silent-failure surface + the turn-end hooks.
func (r *LocalRuntime) onEvent(ev claudestream.StreamEvent) {
	agentID := r.cfg.AgentID
	r.mu.Lock()
	st := r.state
	var workItemRef, toolName string
	var rlRetryAfter int
	var rlResetAt int64
	var taskForEvent string
	var clearEventTask bool
	// sawIncomplete snapshots whether THIS turn was cut short by a connection drop (an
	// assistant_text truncation marker seen earlier this turn), read at turn-end so the
	// `result` branch below can schedule a bounded resume even when is_error=false (T799).
	var sawIncomplete bool
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
	case "assistant_text":
		// T799: claude prints "API Error: Connection closed mid-response. The response
		// above may be incomplete." as ordinary assistant text; flag the turn so the
		// terminal `result` schedules a resume even if is_error=false.
		if isIncompleteTurnMarker(ev.Text) {
			st.SawIncompleteTurn = true
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
			st.SawIncompleteTurn = false // fresh turn → drop a stale truncation marker
		}
	case "result":
		st.ToolNames = nil
		rlRetryAfter, rlResetAt = st.RLRetryAfterSecs, st.RLResetAtUnix
		st.RLRetryAfterSecs, st.RLResetAtUnix = 0, 0
		sawIncomplete = st.SawIncompleteTurn
		st.SawIncompleteTurn = false // turn ended → consume the truncation marker
	}
	taskForEvent = st.EventTaskID
	r.mu.Unlock()

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
	r.maybeReportCenterBypassAlert(agentID, workItemRef, ev)
	r.maybeFailCodexPoisonedTransport(agentID, workItemRef, ev)
	r.maybeFailCodexMissingAgentCenterRegistry(agentID, workItemRef, ev)
	if clearEventTask {
		r.mu.Lock()
		st.LastEventTaskID = st.EventTaskID
		st.EventTaskID = ""
		r.mu.Unlock()
	}

	if ev.Type == "result" && ev.IsError {
		if r.maybeScheduleRateLimitResume(agentID, ev, rlRetryAfter, rlResetAt) {
			return
		}
		if r.maybeScheduleAPIErrorResume(agentID, ev, sawIncomplete) {
			return
		}
		r.surfaceTurnFailure(agentID, ev)
	}

	// T799: a turn CUT SHORT by a connection drop can end with is_error=FALSE — claude
	// prints "…the response above may be incomplete." as ordinary assistant text and
	// then closes the turn "successfully", so IsTransientAPIError (which only inspects a
	// `result` is_error) never fires. Handle it HERE, before the clean-turn path:
	// schedule the SAME bounded resume of the still-live turn. Does NOT return when there
	// is nothing to resume or the budget is spent, so the normal clean-turn handling below
	// still runs (the truncation marker was consumed above).
	if ev.Type == "result" && !ev.IsError && sawIncomplete {
		if r.maybeScheduleAPIErrorResume(agentID, ev, sawIncomplete) {
			return
		}
	}

	if ev.Type == "result" && !ev.IsError {
		r.resetAPIErrorRetries(agentID)
		var isCodex bool
		var codexRecycleReason string
		r.mu.Lock()
		isCodex = st.CLI == CLICodex
		completedConvID := ""
		if st.CurrentTaskID == "" && st.CurrentConversationID != "" {
			completedConvID = st.CurrentConversationID
			st.CurrentConversationID = ""
		}
		if isCodex {
			st.CodexCleanTurns++
			codexRecycleReason = r.codexRecycleReasonLocked(ev, st.CodexCleanTurns)
			if codexRecycleReason != "" {
				// Prevent repeated signals if tests wire OnFatal as a no-op.
				st.CodexCleanTurns = 0
			}
		}
		r.mu.Unlock()
		if completedConvID != "" {
			r.clearInterruptedConverse(agentID)
			r.log("converse agent=%s conv=%s clean turn completed — cleared in-flight conversation", agentID, completedConvID)
		}
		if !isCodex {
			if home, _, _, pathErr := r.agentPaths(agentID); pathErr == nil {
				r.bg.Add(1)
				go func() {
					defer r.bg.Done()
					if merrr := sessioninstance.MarkCompletedTurn(home); merrr != nil {
						r.log("restart-recovery: MarkCompletedTurn(%s) failed: %v", agentID, merrr)
					}
				}()
			}
		}
		go r.maybeReplyNudge(agentID)
		var usageTaskID string
		r.mu.Lock()
		usageTaskID = st.UsageTaskAtResult()
		r.mu.Unlock()
		go r.maybeReportUsage(agentID, ev, usageTaskID)
		if isCodex {
			if codexRecycleReason != "" {
				r.commitDirtyMemory(agentID)
			} else {
				go r.commitDirtyMemory(agentID)
			}
		}
		if codexRecycleReason != "" {
			r.log("codex agent=%s checkpoint recycle: %s", agentID, codexRecycleReason)
			if r.cfg.OnFatal != nil {
				r.cfg.OnFatal(codexRecycleReason)
			}
		}
	}
}

func (r *LocalRuntime) maybeFailCodexMissingAgentCenterRegistry(agentID, workItemRef string, ev claudestream.StreamEvent) {
	if !r.isCodexRuntime() {
		return
	}
	if !codexAgentCenterRegistryMissing(ev) {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type":          "codex_agent_center_mcp_registry_missing",
		"required":      "mcp__agent_center",
		"work_item_ref": workItemRef,
	})
	if r.cfg.Reporter != nil {
		if err := r.cfg.Reporter.ReportAgentActivity(
			context.Background(), agentID, "mcp_registry_missing", string(payload), workItemRef, "", time.Now(),
		); err != nil {
			r.log("codex agent=%s mcp registry missing activity report: %v", agentID, err)
		}
	}
	if home, _, _, err := r.agentPaths(agentID); err == nil {
		if cerr := sessioninstance.ClearSessionID(home); cerr != nil {
			r.log("codex agent=%s clear registry-missing thread_id failed: %v", agentID, cerr)
		}
	} else {
		r.log("codex agent=%s locate home for registry-missing thread_id clear failed: %v", agentID, err)
	}
	r.log("codex agent=%s real tool registry missing mcp__agent_center; failing session", agentID)
	if r.cfg.OnFatal != nil {
		r.cfg.OnFatal("codex real tool registry missing mcp__agent_center")
	}
}

func (r *LocalRuntime) maybeFailCodexPoisonedTransport(agentID, workItemRef string, ev claudestream.StreamEvent) {
	if !r.isCodexRuntime() || !codexPoisoningTransportError(ev) {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type":          "codex_poisoning_transport_error",
		"error":         "invalid peer certificate: UnknownIssuer",
		"work_item_ref": workItemRef,
	})
	if r.cfg.Reporter != nil {
		if err := r.cfg.Reporter.ReportAgentActivity(
			context.Background(), agentID, "codex_transport_poisoned", string(payload), workItemRef, "", time.Now(),
		); err != nil {
			r.log("codex agent=%s transport poison activity report: %v", agentID, err)
		}
	}
	r.log("codex agent=%s transport returned UnknownIssuer; failing session before registry can be trusted", agentID)
	if r.cfg.OnFatal != nil {
		r.cfg.OnFatal("codex transport invalid peer certificate: UnknownIssuer")
	}
}

func (r *LocalRuntime) isCodexRuntime() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state.CLI == CLICodex
}

func codexAgentCenterRegistryMissing(ev claudestream.StreamEvent) bool {
	switch ev.Type {
	case "unknown":
		if len(ev.Raw) == 0 {
			return false
		}
		raw := strings.ToLower(string(ev.Raw))
		if !strings.Contains(raw, "tool_search") || !strings.Contains(raw, "output") {
			return false
		}
		if !mentionsAgentCenterPostMessage(raw) {
			return false
		}
		return !strings.Contains(raw, "mcp__agent_center")
	case "assistant_text":
		text := strings.ToLower(ev.Text)
		if !mentionsAgentCenterPostMessage(text) {
			return false
		}
		if !strings.Contains(text, "mcp") && !strings.Contains(text, "tool registry") && !strings.Contains(text, "工具注册表") {
			return false
		}
		for _, sig := range []string{
			"搜索结果为 0", "search results were 0", "0 results",
			"not provided", "missing", "not found", "could not find", "cannot find",
			"未提供", "找不到", "未找到", "未发现",
		} {
			if strings.Contains(text, sig) {
				return true
			}
		}
	}
	return false
}

func mentionsAgentCenterPostMessage(s string) bool {
	return (strings.Contains(s, "agent-center") || strings.Contains(s, "agent_center")) &&
		strings.Contains(s, "post_message")
}

func codexPoisoningTransportError(ev claudestream.StreamEvent) bool {
	if ev.Type != "error" {
		return false
	}
	msg := strings.ToLower(ev.Result)
	return strings.Contains(msg, "invalid peer certificate") && strings.Contains(msg, "unknownissuer")
}

func (r *LocalRuntime) maybeReportCenterBypassAlert(agentID, workItemRef string, ev claudestream.StreamEvent) {
	alert, ok := CenterBypassAlertFromEvent(ev)
	if !ok {
		return
	}
	payload, err := json.Marshal(alert)
	if err != nil {
		r.log("security agent=%s marshal center-bypass alert: %v", agentID, err)
		return
	}
	if rerr := r.cfg.Reporter.ReportAgentActivity(
		context.Background(), agentID, "security_alert", string(payload), workItemRef, "", time.Now(),
	); rerr != nil {
		r.log("security agent=%s center-bypass alert report: %v", agentID, rerr)
		return
	}
	r.log("SECURITY agent=%s center-bypass shell access observed reasons=%s", agentID, strings.Join(alert.Reasons, ","))
}

func (r *LocalRuntime) codexRecycleReasonLocked(ev claudestream.StreamEvent, cleanTurns int) string {
	inputLimit := r.cfg.CodexRecycleInputTokens
	if inputLimit == 0 {
		inputLimit = defaultCodexRecycleInputTokens
	}
	if inputLimit > 0 && ev.TokensIn >= inputLimit {
		return fmt.Sprintf("codex checkpoint recycle after clean turn: input_tokens=%d >= %d", ev.TokensIn, inputLimit)
	}
	turnLimit := r.cfg.CodexRecycleCleanTurns
	if turnLimit == 0 {
		turnLimit = defaultCodexRecycleCleanTurns
	}
	if turnLimit > 0 && cleanTurns >= turnLimit {
		return fmt.Sprintf("codex checkpoint recycle after %d clean turns", cleanTurns)
	}
	return ""
}

// onExit coordinates the exactly-once lifecycle report on session exit (three exit
// kinds). Crash path deletes the managedAgent (via RemoveAgent, now called AFTER the
// StateMu critical section so the seam can take c.mu — 去共享状态) and records +
// schedules a self-heal relaunch — behavior preserved.
func (r *LocalRuntime) onExit(exitErr error) {
	agentID := r.cfg.AgentID
	r.mu.Lock()
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
	// The executor engine now lives on the runtime (Phase 0c); read it under the held
	// Mu (this whole block is inside r.mu.Lock()).
	wasConcurrent := r.exec != nil
	taskLog := st.TaskLog
	st.TaskLog, st.TaskLogID = nil, ""
	r.mu.Unlock()

	// T860 piece ③: the daemon-side managedAgent map + its RemoveAgent seam are gone
	// (the agent is its OWN process now, not an in-process managedAgent). Nothing to
	// delete here — a crash exits the process and the worker launcher rebuilds it.

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
	// T860 piece ③ (gap4): the controller/process-per-agent model has NO in-process
	// self-heal (SelfHealStore is gone). An unexpected session crash → report the
	// crash lifecycle once, then signal THIS agent-runtime PROCESS to exit (OnFatal).
	// The worker's launcher rebuilds it with BOUNDED backoff + max-attempts
	// (crash-loop safety lives in the durable launcher, not the crashing process), and
	// the rebuilt process re-Boots + re-Starts the session. _ = version/hadWork/... :
	// the relaunch spec fields the old in-process self-heal carried are re-derived by
	// the rebuilt process from the center's ResumeState, so they are not threaded
	// through here.
	_ = version
	_ = hadWork
	_ = taskID
	_ = model
	_ = displayName
	_ = promptDescription
	_ = envVars
	_ = wasConcurrent
	st.LifecycleOnce.Do(func() {
		if err := r.cfg.Reporter.ReportAgentLifecycle(context.Background(), agentID, "error", msg, time.Now()); err != nil {
			r.log("agent=%s report crashed: %v", agentID, err)
		}
	})
	if r.cfg.OnFatal != nil {
		r.cfg.OnFatal(msg)
	}
}

// maybeReplyNudge is the worker half of the reply-guardrail turn-end hook (T341).
func (r *LocalRuntime) maybeReplyNudge(agentID string) {
	r.mu.Lock()
	sess := r.state.Session
	inConverse := r.state.CurrentConversationID != ""
	r.mu.Unlock()
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
	r.mu.Lock()
	model := r.state.Model
	r.mu.Unlock()
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
	r.mu.Lock()
	wiID := r.state.CurrentTaskID
	convID := r.state.CurrentConversationID
	r.mu.Unlock()

	if wiID == "" {
		if convID != "" {
			r.surfaceConverseFailure(agentID, convID, ev)
			return
		}
		r.log("L2 agent=%s is_error turn with NO in-flight WorkItem (subtype=%q) — surfaced as warning, not silently dropped", agentID, ev.Subtype)
		return
	}

	r.log("L2 agent=%s work_item=%s failed (is_error turn, subtype=%q)", agentID, wiID, ev.Subtype)

	r.mu.Lock()
	if r.state.CurrentTaskID == wiID {
		r.state.CurrentTaskID = ""
	}
	r.mu.Unlock()
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
	r.clearInterruptedConverse(agentID)
	r.mu.Lock()
	if r.state.CurrentConversationID == convID {
		r.state.CurrentConversationID = ""
	}
	r.mu.Unlock()
}
