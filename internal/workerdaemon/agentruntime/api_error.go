package agentruntime

// api_error.go — automatic recovery from a TRANSIENT API/connection error that ends
// a turn (T475), moved off AgentController. Uses the shared resume slot
// (RateLimitResumeAt); the drain lives in rate_limit.go's drainResume.

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/claudestream"
)

const (
	DefaultAPIErrorBackoffBase = 2 * time.Second
	DefaultAPIErrorBackoffCap  = 60 * time.Second
	DefaultAPIErrorMaxRetries  = 5
)

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

// IsTransientAPIError reports whether a `result` is_error event is a transient
// API/connection error.
func IsTransientAPIError(ev claudestream.StreamEvent) bool {
	if ev.Type != "result" || !ev.IsError {
		return false
	}
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

func (r *LocalRuntime) apiErrorParams() apiErrorParams {
	p := apiErrorParams{
		backoffBase: r.cfg.APIErrorBackoffBase,
		backoffCap:  r.cfg.APIErrorBackoffCap,
		maxRetries:  r.cfg.APIErrorMaxRetries,
	}
	if p.backoffBase <= 0 {
		p.backoffBase = DefaultAPIErrorBackoffBase
	}
	if p.backoffCap <= 0 {
		p.backoffCap = DefaultAPIErrorBackoffCap
	}
	if p.maxRetries <= 0 {
		p.maxRetries = DefaultAPIErrorMaxRetries
	}
	return p
}

// DecideAPIErrorBackoff is the PURE attempt→delay policy (unit-tested).
func DecideAPIErrorBackoff(attempt int, base, cap time.Duration) time.Duration {
	p := apiErrorParams{backoffBase: base, backoffCap: cap}
	if p.backoffBase <= 0 {
		p.backoffBase = DefaultAPIErrorBackoffBase
	}
	if p.backoffCap <= 0 {
		p.backoffCap = DefaultAPIErrorBackoffCap
	}
	return decideAPIErrorBackoff(attempt, p)
}

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

// maybeScheduleAPIErrorResume schedules a bounded resume for a transient API error.
func (r *LocalRuntime) maybeScheduleAPIErrorResume(agentID string, ev claudestream.StreamEvent) bool {
	if !IsTransientAPIError(ev) {
		return false
	}
	p := r.apiErrorParams()

	r.cfg.Mu.Lock()
	st := r.state
	if st.Session == nil || st.CurrentTaskID == "" {
		r.cfg.Mu.Unlock()
		return false
	}
	if st.APIErrorRetries >= p.maxRetries {
		st.APIErrorRetries = 0
		st.RateLimitResumeAt = time.Time{}
		wi := st.CurrentTaskID
		r.cfg.Mu.Unlock()
		r.log("agent=%s work_item=%s transient API error but retry budget (%d) spent → surfacing failure", agentID, wi, p.maxRetries)
		return false
	}
	st.APIErrorRetries++
	attempt := st.APIErrorRetries
	wiID := st.CurrentTaskID
	delay := decideAPIErrorBackoff(attempt, p)
	now := r.now()
	resumeAt := now.Add(delay)
	st.RateLimitResumeAt = resumeAt
	r.cfg.Mu.Unlock()

	if r.cfg.Reporter != nil {
		payload := apiErrorResumePayload(ev, attempt, p.maxRetries, resumeAt)
		if err := r.cfg.Reporter.ReportAgentActivity(
			context.Background(), agentID, "api_error", payload, wiID, "", now,
		); err != nil {
			r.log("agent=%s api-error activity report: %v", agentID, err)
		}
	}
	r.log("agent=%s work_item=%s transient API error (subtype=%q) → resume scheduled in %s (attempt %d/%d, at %s); work NOT abandoned",
		agentID, wiID, ev.Subtype, delay, attempt, p.maxRetries, resumeAt.Format(time.RFC3339))
	return true
}

// resetAPIErrorRetries zeroes the transient-API-error retry budget on a CLEAN turn-end.
func (r *LocalRuntime) resetAPIErrorRetries(agentID string) {
	r.cfg.Mu.Lock()
	r.state.APIErrorRetries = 0
	r.cfg.Mu.Unlock()
}

func apiErrorResumePayload(ev claudestream.StreamEvent, attempt, maxRetries int, resumeAt time.Time) string {
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
