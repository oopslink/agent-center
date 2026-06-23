package workerdaemon

// rate_limit.go — automatic recovery from LLM server-side rate-limiting (issue:
// "[可用性] LLM 服务端限流，如何自动恢复").
//
// THE GAP THIS CLOSES. claude streams a `rate_limit_event` (carrying a retry-after
// / reset window) when the Anthropic API throttles it, and — if it exhausts its
// own internal retry budget — ends the turn with a `result` whose is_error=true.
// Before this file, that path was pure observability: onEvent reported the
// rate_limit activity and surfaceTurnFailure cleared the in-flight WorkItem
// pointer, leaving the work SILENTLY ABANDONED (the agent went idle mid-task). The
// session itself stays ALIVE across a rate-limit (claude does NOT exit), so the
// crash-driven self-heal machinery never fires — and even if it did, its 1→2→4→8→16s
// backoff (cap 30s, 5 attempts) is far shorter than an LLM usage-limit window
// (minutes to hours) and would just burn the circuit-breaker budget re-hitting the
// limit.
//
// THE FIX. When a turn ends in a rate-limit is_error, instead of abandoning it we
// SCHEDULE a resume: keep currentTaskID, set managedAgent.rateLimitResumeAt to when
// the window clears (parsed retry_after / resets_at, else a default backoff), and on
// the OnTick that finds it due, inject the resume nudge into the still-live session
// so claude re-drives the interrupted turn. This is the live-session analogue of the
// self-heal nextRelaunchAt drain — same OnTick cadence, but no relaunch (the session
// never died). A bare rate_limit_event (turn NOT ended) is only RECORDED (its window
// remembered for a possible later is_error); we never force-resume a live turn, which
// would corrupt claude's own internal retry.

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// Rate-limit auto-recovery defaults (overridable via AgentControllerConfig).
const (
	// defaultRateLimitBackoff is used when claude gave no window (no retry_after /
	// resets_at) — a conservative wait before re-driving the turn.
	defaultRateLimitBackoff = 60 * time.Second
	// defaultRateLimitMinBackoff floors every wait so a stale/past reset timestamp
	// (or a tiny retry_after) can never hot-loop the resume.
	defaultRateLimitMinBackoff = 5 * time.Second
	// defaultRateLimitMaxBackoff caps the wait so an absurd window can't park an
	// agent effectively forever; an LLM usage-limit window fits comfortably under it.
	defaultRateLimitMaxBackoff = 1 * time.Hour
)

// rateLimitSignatures are the case-insensitive needles that mark a `result`
// is_error turn as an LLM server-side rate-limit (vs an ordinary failure). Matched
// against subtype + result text + the raw line.
var rateLimitSignatures = []string{
	"rate limit", "rate_limit", "ratelimit",
	"too many requests", "429",
	"overloaded",
	"usage limit", "usage_limit",
}

// isRateLimitError reports whether a `result` is_error event is an LLM server-side
// rate-limit (so the controller schedules a resume instead of abandoning the work).
// Non-result / non-error events are never rate-limit errors here (a bare rate_limit
// event is handled separately — it does not end the turn).
func isRateLimitError(ev StreamEvent) bool {
	if ev.Type != "result" || !ev.IsError {
		return false
	}
	hay := strings.ToLower(ev.Subtype + " " + ev.Result + " " + string(ev.Raw))
	for _, sig := range rateLimitSignatures {
		if strings.Contains(hay, sig) {
			return true
		}
	}
	return false
}

type rateLimitParams struct {
	defaultBackoff time.Duration
	minBackoff     time.Duration
	maxBackoff     time.Duration
}

func (c *AgentController) rateLimitParams() rateLimitParams {
	p := rateLimitParams{
		defaultBackoff: c.cfg.RateLimitDefaultBackoff,
		minBackoff:     c.cfg.RateLimitMinBackoff,
		maxBackoff:     c.cfg.RateLimitMaxBackoff,
	}
	if p.defaultBackoff <= 0 {
		p.defaultBackoff = defaultRateLimitBackoff
	}
	if p.minBackoff <= 0 {
		p.minBackoff = defaultRateLimitMinBackoff
	}
	if p.maxBackoff <= 0 {
		p.maxBackoff = defaultRateLimitMaxBackoff
	}
	return p
}

// decideRateLimitResume is the PURE window→delay policy (unit-tested). It prefers an
// explicit retry_after, then an absolute resets_at (relative to now), then the
// default backoff — and clamps the result to [minBackoff, maxBackoff] so a past /
// tiny / absurd window can neither hot-loop nor park the agent forever.
func decideRateLimitResume(retryAfterSecs int, resetAtUnix int64, now time.Time, p rateLimitParams) time.Duration {
	var d time.Duration
	switch {
	case retryAfterSecs > 0:
		d = time.Duration(retryAfterSecs) * time.Second
	case resetAtUnix > 0:
		d = time.Unix(resetAtUnix, 0).Sub(now)
	default:
		d = p.defaultBackoff
	}
	if d < p.minBackoff {
		d = p.minBackoff
	}
	if d > p.maxBackoff {
		d = p.maxBackoff
	}
	return d
}

// maybeScheduleRateLimitResume is the onEvent hook for a `result` is_error turn. If
// the failure is an LLM rate-limit AND there is an in-flight WorkItem on a live
// session, it SCHEDULES an automatic resume (keeps currentTaskID, sets
// rateLimitResumeAt to when the window clears) and returns true so onEvent does NOT
// fall through to surfaceTurnFailure (which would abandon the work). It returns false
// — caller proceeds with the normal failure surface — when the error is not a
// rate-limit, the session is gone (let self-heal own recovery), or there is no
// in-flight WorkItem to resume (e.g. a converse turn → surfaceConverseFailure still
// owns the visible-error UX). retryAfterSecs/resetAtUnix are the window remembered
// from this turn's rate_limit event (0 = none → default backoff).
func (c *AgentController) maybeScheduleRateLimitResume(agentID string, ev StreamEvent, retryAfterSecs int, resetAtUnix int64) bool {
	if !isRateLimitError(ev) {
		return false
	}
	now := c.now()
	delay := decideRateLimitResume(retryAfterSecs, resetAtUnix, now, c.rateLimitParams())
	resumeAt := now.Add(delay)

	c.mu.Lock()
	ma := c.agents[agentID]
	if ma == nil || ma.session == nil || ma.currentTaskID == "" {
		// No live session or no in-flight WorkItem → nothing to auto-resume here.
		c.mu.Unlock()
		return false
	}
	wiID := ma.currentTaskID
	ma.rateLimitResumeAt = resumeAt
	c.mu.Unlock()

	// Observability: surface the rate-limit + the scheduled resume (distinct from a
	// terminal failure) in the activity stream and the daemon log. currentTaskID is
	// intentionally PRESERVED so the resumed turn re-drives the same work.
	if c.cfg.Reporter != nil {
		payload := rateLimitResumePayload(ev, retryAfterSecs, resetAtUnix, resumeAt)
		if err := c.cfg.Reporter.ReportAgentActivity(
			context.Background(), agentID, "rate_limit", payload, wiID, "", now,
		); err != nil {
			c.log("agent=%s rate-limit activity report: %v", agentID, err)
		}
	}
	c.log("agent=%s work_item=%s rate-limited (subtype=%q) → resume scheduled in %s (at %s); work NOT abandoned",
		agentID, wiID, ev.Subtype, delay, resumeAt.Format(time.RFC3339))
	return true
}

// drainRateLimitResumes is invoked from OnTick (single-threaded ControlLoop
// goroutine). It injects the resume nudge into every agent whose rate-limit window
// has cleared and whose session is still live — re-driving the interrupted turn —
// then consumes the schedule. An agent whose session died meanwhile is skipped (the
// crash-driven self-heal path owns that recovery); its stale schedule is cleared.
func (c *AgentController) drainRateLimitResumes(ctx context.Context, now time.Time) {
	type due struct {
		agentID string
		sess    agentSession
	}
	var dues []due
	c.mu.Lock()
	for id, ma := range c.agents {
		if ma == nil || ma.rateLimitResumeAt.IsZero() || ma.rateLimitResumeAt.After(now) {
			continue
		}
		if ma.session == nil {
			ma.rateLimitResumeAt = time.Time{} // session gone → self-heal owns recovery
			continue
		}
		dues = append(dues, due{agentID: id, sess: ma.session})
		ma.rateLimitResumeAt = time.Time{} // consume the schedule (no re-fire)
	}
	c.mu.Unlock()

	for _, d := range dues {
		if err := d.sess.Inject(ctx, c.resumeNudgeText()); err != nil {
			// Session closed between the snapshot and the inject — drop; a crash
			// would re-route recovery through self-heal, and a clean stop is intended.
			c.log("agent=%s rate-limit resume inject: %v — skipped", d.agentID, err)
			continue
		}
		c.log("agent=%s rate-limit window cleared → resumed (resume nudge injected)", d.agentID)
		// A rate-limited agent may have been surfaced as errored at the center; the
		// resume is a recovery, so clear any lingering error → running (best-effort).
		c.reportRecovered(d.agentID)
	}
}

// resumeNudgeText is the message injected to re-drive an interrupted turn (shared by
// the rate-limit resume and the self-heal / boot relaunch paths): the configured
// ResumeNudge, or DefaultResumeNudge when unset.
func (c *AgentController) resumeNudgeText() string {
	if msg := strings.TrimSpace(c.cfg.ResumeNudge); msg != "" {
		return c.cfg.ResumeNudge
	}
	return DefaultResumeNudge
}

// rateLimitResumePayload builds the activity payload for a rate-limited turn whose
// resume was scheduled (so the console can show "限流中，预计 HH:MM 重试"). On the
// (unreachable) marshal error it degrades to a minimal valid object.
func rateLimitResumePayload(ev StreamEvent, retryAfterSecs int, resetAtUnix int64, resumeAt time.Time) string {
	p := map[string]any{
		"type":      "rate_limit",
		"action":    "resume_scheduled",
		"resume_at": resumeAt.UTC().Format(time.RFC3339),
	}
	if retryAfterSecs > 0 {
		p["retry_after"] = retryAfterSecs
	}
	if resetAtUnix > 0 {
		p["resets_at"] = resetAtUnix
	}
	if s := strings.TrimSpace(ev.Subtype); s != "" {
		p["subtype"] = s
	}
	b, err := json.Marshal(p)
	if err != nil {
		return `{"type":"rate_limit","action":"resume_scheduled"}`
	}
	return string(b)
}
