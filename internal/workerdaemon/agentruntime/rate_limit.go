package agentruntime

// rate_limit.go — automatic recovery from LLM server-side rate-limiting, moved off
// AgentController. drainResume (Tick) is shared with api_error.go (the reason-agnostic
// resume slot RateLimitResumeAt).

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/claudestream"
)

const (
	DefaultRateLimitBackoff    = 60 * time.Second
	DefaultRateLimitMinBackoff = 5 * time.Second
	DefaultRateLimitMaxBackoff = 1 * time.Hour
)

var rateLimitSignatures = []string{
	"rate limit", "rate_limit", "ratelimit",
	"too many requests", "429",
	"overloaded",
	"usage limit", "usage_limit",
}

// IsRateLimitError reports whether a `result` is_error event is an LLM server-side
// rate-limit.
func IsRateLimitError(ev claudestream.StreamEvent) bool {
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

func (r *LocalRuntime) rateLimitParams() rateLimitParams {
	p := rateLimitParams{
		defaultBackoff: r.cfg.RateLimitDefaultBackoff,
		minBackoff:     r.cfg.RateLimitMinBackoff,
		maxBackoff:     r.cfg.RateLimitMaxBackoff,
	}
	if p.defaultBackoff <= 0 {
		p.defaultBackoff = DefaultRateLimitBackoff
	}
	if p.minBackoff <= 0 {
		p.minBackoff = DefaultRateLimitMinBackoff
	}
	if p.maxBackoff <= 0 {
		p.maxBackoff = DefaultRateLimitMaxBackoff
	}
	return p
}

// DecideRateLimitResume is the PURE window→delay policy (unit-tested).
func DecideRateLimitResume(retryAfterSecs int, resetAtUnix int64, now time.Time, defaultBackoff, minBackoff, maxBackoff time.Duration) time.Duration {
	p := rateLimitParams{defaultBackoff: defaultBackoff, minBackoff: minBackoff, maxBackoff: maxBackoff}
	if p.defaultBackoff <= 0 {
		p.defaultBackoff = DefaultRateLimitBackoff
	}
	if p.minBackoff <= 0 {
		p.minBackoff = DefaultRateLimitMinBackoff
	}
	if p.maxBackoff <= 0 {
		p.maxBackoff = DefaultRateLimitMaxBackoff
	}
	return decideRateLimitResume(retryAfterSecs, resetAtUnix, now, p)
}

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

// maybeScheduleRateLimitResume schedules an automatic resume for a rate-limit
// is_error turn. Returns true when it claimed the failure.
func (r *LocalRuntime) maybeScheduleRateLimitResume(agentID string, ev claudestream.StreamEvent, retryAfterSecs int, resetAtUnix int64) bool {
	if !IsRateLimitError(ev) {
		return false
	}
	now := r.now()
	delay := decideRateLimitResume(retryAfterSecs, resetAtUnix, now, r.rateLimitParams())
	resumeAt := now.Add(delay)

	r.cfg.Mu.Lock()
	if r.state.Session == nil || r.state.CurrentTaskID == "" {
		r.cfg.Mu.Unlock()
		return false
	}
	wiID := r.state.CurrentTaskID
	r.state.RateLimitResumeAt = resumeAt
	r.cfg.Mu.Unlock()

	if r.cfg.Reporter != nil {
		payload := rateLimitResumePayload(ev, retryAfterSecs, resetAtUnix, resumeAt)
		if err := r.cfg.Reporter.ReportAgentActivity(
			context.Background(), agentID, "rate_limit", payload, wiID, "", now,
		); err != nil {
			r.log("agent=%s rate-limit activity report: %v", agentID, err)
		}
	}
	r.log("agent=%s work_item=%s rate-limited (subtype=%q) → resume scheduled in %s (at %s); work NOT abandoned",
		agentID, wiID, ev.Subtype, delay, resumeAt.Format(time.RFC3339))
	return true
}

// drainResume injects the resume nudge into this agent's live session when its
// resume slot (rate-limit OR api-error) is due, then consumes it. Per-agent form of
// the old drainRateLimitResumes.
func (r *LocalRuntime) drainResume(ctx context.Context, now time.Time) {
	agentID := r.cfg.AgentID
	r.cfg.Mu.Lock()
	st := r.state
	if st.RateLimitResumeAt.IsZero() || st.RateLimitResumeAt.After(now) {
		r.cfg.Mu.Unlock()
		return
	}
	if st.Session == nil {
		st.RateLimitResumeAt = time.Time{}
		r.cfg.Mu.Unlock()
		return
	}
	sess := st.Session
	st.RateLimitResumeAt = time.Time{}
	r.cfg.Mu.Unlock()

	if err := sess.Inject(ctx, r.resumeNudgeText()); err != nil {
		r.log("agent=%s rate-limit resume inject: %v — skipped", agentID, err)
		return
	}
	r.log("agent=%s rate-limit window cleared → resumed (resume nudge injected)", agentID)
	r.reportRecovered()
}

func rateLimitResumePayload(ev claudestream.StreamEvent, retryAfterSecs int, resetAtUnix int64, resumeAt time.Time) string {
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
