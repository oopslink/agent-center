package workerdaemon

// api_error.go — automatic recovery from a TRANSIENT API / connection error that
// ends a turn (T475: "[优化] 优化 agent 的错误处理 — API Error: Connection closed
// mid-response").
//
// THE GAP THIS CLOSES. The Anthropic API occasionally drops a streaming response
// mid-flight ("Connection closed mid-response"), returns a 5xx, or times out. When
// claude exhausts its own internal retries it ends the turn with a `result` whose
// is_error=true — but the SESSION stays alive (claude does NOT exit), so the
// crash-driven self-heal never fires. Before this file, onEvent's L2 surface then
// ran surfaceTurnFailure, which cleared the in-flight WorkItem pointer and left the
// work SILENTLY ABANDONED (the agent went idle mid-task) over a blip that a single
// retry would have ridden out.
//
// THE FIX. This is the connection-error sibling of rate_limit.go. When a turn ends
// in a transient API error we SCHEDULE a resume (keep currentTaskID, set
// managedAgent.rateLimitResumeAt — the shared, reason-agnostic resume slot) so the
// OnTick that finds it due injects the resume nudge into the still-live session and
// re-drives the interrupted turn. THE KEY DIFFERENCE from a rate-limit: a rate-limit
// carries a server window (retry_after / resets_at) and may re-schedule until the
// window clears; a connection error carries NO window, so the resume is BOUNDED — an
// exponential backoff (base → cap) and a hard retry cap, after which it falls through
// to surfaceTurnFailure. That bound matters: these turns are expensive (the issue's
// own example burned ~$30), so a persistently-failing API must surface, never loop.

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// Transient-API-error auto-retry defaults (overridable via AgentControllerConfig).
const (
	// defaultAPIErrorBackoffBase is the first wait before re-driving the turn; each
	// further consecutive transient error doubles it (capped). Short because a
	// connection blip usually clears in seconds — unlike an LLM usage window.
	defaultAPIErrorBackoffBase = 2 * time.Second
	// defaultAPIErrorBackoffCap caps the exponential growth so a long burst of errors
	// can't push the next retry absurdly far out.
	defaultAPIErrorBackoffCap = 60 * time.Second
	// defaultAPIErrorMaxRetries bounds the consecutive auto-resumes for one turn. After
	// this many reschedules the error is treated as non-transient (the API is down, not
	// blipping) and the turn surfaces as a failure rather than looping/re-paying forever.
	defaultAPIErrorMaxRetries = 5
)

// transientAPIErrorSignatures are the case-insensitive needles that mark a `result`
// is_error turn as a TRANSIENT API/connection error (so the controller schedules a
// bounded resume instead of abandoning the work). Matched against subtype + result
// text + the raw line. Kept deliberately TARGETED at network/server transients that
// claude surfaces in its terminal "API Error: …" summary — NOT broad failure words —
// so an ordinary task failure (bad model, auth, permission denied) still surfaces
// normally. Rate-limit needles ("overloaded", "429", …) live in rateLimitSignatures
// and are matched FIRST by onEvent, so they never reach here.
var transientAPIErrorSignatures = []string{
	"connection closed mid-response",
	"connection closed",
	"connection reset",
	"connection refused",
	"connection error",
	"broken pipe",
	"unexpected eof",
	"i/o timeout",
	"request timed out",
	"tls handshake",
	"internal server error",
	"bad gateway",
	"service unavailable",
	"gateway timeout",
}

// isTransientAPIError reports whether a `result` is_error event is a transient
// API/connection error (so the controller schedules a bounded resume instead of
// abandoning the work). Non-result / non-error events are never transient API errors
// here. A rate-limit (handled earlier by maybeScheduleRateLimitResume) may also match
// some needles ("overloaded"), but onEvent calls the rate-limit path FIRST and returns
// on a match, so this classifier only ever sees the non-rate-limit remainder.
func isTransientAPIError(ev StreamEvent) bool {
	if ev.Type != "result" || !ev.IsError {
		return false
	}
	// Normalise underscores → spaces so a snake_case subtype (e.g.
	// "internal_server_error") matches the spaced needles too.
	hay := strings.ReplaceAll(strings.ToLower(ev.Subtype+" "+ev.Result+" "+string(ev.Raw)), "_", " ")
	for _, sig := range transientAPIErrorSignatures {
		if strings.Contains(hay, sig) {
			return true
		}
	}
	return false
}

type apiErrorParams struct {
	backoffBase time.Duration
	backoffCap  time.Duration
	maxRetries  int
}

func (c *AgentController) apiErrorParams() apiErrorParams {
	p := apiErrorParams{
		backoffBase: c.cfg.APIErrorBackoffBase,
		backoffCap:  c.cfg.APIErrorBackoffCap,
		maxRetries:  c.cfg.APIErrorMaxRetries,
	}
	if p.backoffBase <= 0 {
		p.backoffBase = defaultAPIErrorBackoffBase
	}
	if p.backoffCap <= 0 {
		p.backoffCap = defaultAPIErrorBackoffCap
	}
	if p.maxRetries <= 0 {
		p.maxRetries = defaultAPIErrorMaxRetries
	}
	return p
}

// decideAPIErrorBackoff is the PURE attempt→delay policy (unit-tested): exponential
// backoff base*2^(attempt-1), clamped to the cap. attempt is 1-based (the 1st retry
// waits base). A non-positive attempt is treated as the first retry so the wait can
// never collapse to zero and hot-loop.
func decideAPIErrorBackoff(attempt int, p apiErrorParams) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := p.backoffBase
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= p.backoffCap {
			return p.backoffCap
		}
	}
	if d > p.backoffCap {
		d = p.backoffCap
	}
	return d
}

// maybeScheduleAPIErrorResume is the onEvent hook for a `result` is_error turn that
// the rate-limit path did NOT claim. If the failure is a transient API/connection
// error AND there is an in-flight WorkItem on a live session AND the retry budget is
// not yet spent, it SCHEDULES a bounded, backed-off resume (keeps currentTaskID, sets
// rateLimitResumeAt) and returns true so onEvent does NOT fall through to
// surfaceTurnFailure. It returns false — caller proceeds with the normal failure
// surface — when the error is not transient, the session is gone (let self-heal own
// recovery), there is no in-flight WorkItem to resume, or the retry cap is reached (a
// persistently-failing API must surface, not loop). On hitting the cap it RESETS the
// counter so the eventual surfaceTurnFailure + a future turn start clean.
func (c *AgentController) maybeScheduleAPIErrorResume(agentID string, ev StreamEvent) bool {
	if !isTransientAPIError(ev) {
		return false
	}
	p := c.apiErrorParams()

	c.mu.Lock()
	ma := c.agents[agentID]
	if ma == nil || ma.session == nil || ma.currentTaskID == "" {
		// No live session or no in-flight WorkItem → nothing to auto-resume here.
		c.mu.Unlock()
		return false
	}
	if ma.apiErrorRetries >= p.maxRetries {
		// Budget spent: the API is down, not blipping. Reset so the surfaceTurnFailure
		// below + the next turn start fresh, and let the normal failure surface run.
		// Also clear any resume slot still pending from the LAST retry so a stale drain
		// can't re-drive the work we're about to abandon (defensive: in practice OnTick
		// has already consumed it by the time this next error arrives).
		ma.apiErrorRetries = 0
		ma.rateLimitResumeAt = time.Time{}
		c.mu.Unlock()
		c.log("agent=%s work_item=%s transient API error but retry budget (%d) spent → surfacing failure", agentID, ma.currentTaskID, p.maxRetries)
		return false
	}
	ma.apiErrorRetries++
	attempt := ma.apiErrorRetries
	wiID := ma.currentTaskID
	delay := decideAPIErrorBackoff(attempt, p)
	now := c.now()
	resumeAt := now.Add(delay)
	ma.rateLimitResumeAt = resumeAt
	c.mu.Unlock()

	// Observability: surface the transient error + the scheduled retry (distinct from a
	// terminal failure) in the activity stream and the daemon log. currentTaskID is
	// intentionally PRESERVED so the resumed turn re-drives the same work.
	if c.cfg.Reporter != nil {
		payload := apiErrorResumePayload(ev, attempt, p.maxRetries, resumeAt)
		if err := c.cfg.Reporter.ReportAgentActivity(
			context.Background(), agentID, "api_error", payload, wiID, "", now,
		); err != nil {
			c.log("agent=%s api-error activity report: %v", agentID, err)
		}
	}
	c.log("agent=%s work_item=%s transient API error (subtype=%q) → resume scheduled in %s (attempt %d/%d, at %s); work NOT abandoned",
		agentID, wiID, ev.Subtype, delay, attempt, p.maxRetries, resumeAt.Format(time.RFC3339))
	return true
}

// resetAPIErrorRetries zeroes the transient-API-error retry budget for an agent on a
// CLEAN turn-end (a recovered burst must not carry its count into a later, unrelated
// error). No-op when the agent is unknown. Guarded by mu.
func (c *AgentController) resetAPIErrorRetries(agentID string) {
	c.mu.Lock()
	if ma := c.agents[agentID]; ma != nil {
		ma.apiErrorRetries = 0
	}
	c.mu.Unlock()
}

// apiErrorResumePayload builds the activity payload for a transient-API-error turn
// whose resume was scheduled (so the console can show "连接错误，重试中 N/M"). On the
// (unreachable) marshal error it degrades to a minimal valid object.
func apiErrorResumePayload(ev StreamEvent, attempt, maxRetries int, resumeAt time.Time) string {
	p := map[string]any{
		"type":      "api_error",
		"action":    "resume_scheduled",
		"attempt":   attempt,
		"max":       maxRetries,
		"resume_at": resumeAt.UTC().Format(time.RFC3339),
	}
	if s := strings.TrimSpace(ev.Subtype); s != "" {
		p["subtype"] = s
	}
	if r := strings.TrimSpace(ev.Result); r != "" {
		p["message"] = r
	}
	b, err := json.Marshal(p)
	if err != nil {
		return `{"type":"api_error","action":"resume_scheduled"}`
	}
	return string(b)
}
