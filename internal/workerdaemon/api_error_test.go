package workerdaemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/claudestream"
)

// TestIsTransientAPIError pins the classifier: only a `result` is_error turn whose
// subtype/result/raw carries a transient API/connection signature counts — so an
// ordinary failure (bad model, auth) still surfaces normally, and a non-result /
// non-error event is never misclassified.
func TestIsTransientAPIError(t *testing.T) {
	cases := []struct {
		name string
		ev   StreamEvent
		want bool
	}{
		{"connection closed mid-response (the issue)", StreamEvent{Type: "result", IsError: true, Result: "API Error: Connection closed mid-response. The response above may be incomplete."}, true},
		{"connection reset", StreamEvent{Type: "result", IsError: true, Result: "read tcp: connection reset by peer"}, true},
		{"internal server error subtype", StreamEvent{Type: "result", IsError: true, Subtype: "internal_server_error"}, true},
		{"503 service unavailable text", StreamEvent{Type: "result", IsError: true, Result: "API Error: 503 Service Unavailable"}, true},
		{"bad gateway in raw", StreamEvent{Type: "result", IsError: true, Raw: []byte(`{"error":"Bad Gateway"}`)}, true},
		{"i/o timeout", StreamEvent{Type: "result", IsError: true, Result: "post https://api...: i/o timeout"}, true},
		{"ordinary error not transient", StreamEvent{Type: "result", IsError: true, Result: "model x not found"}, false},
		{"permission denied not transient", StreamEvent{Type: "result", IsError: true, Result: "permission denied"}, false},
		{"transient text but not error", StreamEvent{Type: "result", IsError: false, Result: "connection closed"}, false},
		{"non-result type", StreamEvent{Type: "assistant_text", Result: "connection closed"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransientAPIError(tc.ev); got != tc.want {
				t.Fatalf("isTransientAPIError = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestDecideAPIErrorBackoff pins the attempt→delay policy: exponential base*2^(n-1)
// clamped to the cap, with a non-positive attempt floored to the first retry (never
// zero → never a hot-loop).
func TestDecideAPIErrorBackoff(t *testing.T) {
	p := apiErrorParams{backoffBase: 2 * time.Second, backoffCap: 60 * time.Second, maxRetries: 5}
	cases := []struct {
		name    string
		attempt int
		want    time.Duration
	}{
		{"attempt 1 = base", 1, 2 * time.Second},
		{"attempt 2 doubles", 2, 4 * time.Second},
		{"attempt 3 doubles", 3, 8 * time.Second},
		{"attempt 4 doubles", 4, 16 * time.Second},
		{"attempt 5 doubles", 5, 32 * time.Second},
		{"attempt 6 capped", 6, 60 * time.Second},
		{"attempt 99 capped", 99, 60 * time.Second},
		{"zero floored to first", 0, 2 * time.Second},
		{"negative floored to first", -3, 2 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decideAPIErrorBackoff(tc.attempt, p); got != tc.want {
				t.Fatalf("decideAPIErrorBackoff(%d) = %s, want %s", tc.attempt, got, tc.want)
			}
		})
	}
}

// TestAPIError_SchedulesResumeAndOnTickResumes is the end-to-end controller path: a
// transient-API-error is_error turn must NOT abandon the in-flight work (currentTaskID
// preserved) but schedule a backed-off resume; the OnTick that finds it due injects
// the resume nudge into the still-live session and consumes the schedule.
func TestAPIError_SchedulesResumeAndOnTickResumes(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	c, rep, rs := newTestController(t, t.TempDir())
	c.cfg.Now = clock.now
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := c.Handle(context.Background(), workCmd(t, "agent-1", "wi-1", "do the thing", 2)); err != nil {
		t.Fatalf("work: %v", err)
	}
	fs := rs.last()
	injectedAfterWork := len(fs.injectedMsgs())

	// The turn ends with the connection-closed error from the issue.
	fs.emit(claudestream.StreamEvent{Type: "result", Subtype: "error_during_execution", Result: "API Error: Connection closed mid-response. The response above may be incomplete.", IsError: true})

	// Work NOT abandoned; resume scheduled at now+base (2s) on the FIRST retry.
	c.mu.Lock()
	ma := c.agents["agent-1"]
	gotTask := ma.currentTaskID
	gotResumeAt := ma.rateLimitResumeAt
	gotRetries := ma.apiErrorRetries
	c.mu.Unlock()
	if gotTask != "wi-1" {
		t.Fatalf("transient-error turn must PRESERVE currentTaskID for resume, got %q", gotTask)
	}
	if gotRetries != 1 {
		t.Fatalf("first transient error must set apiErrorRetries=1, got %d", gotRetries)
	}
	if want := clock.now().Add(2 * time.Second); !gotResumeAt.Equal(want) {
		t.Fatalf("resumeAt = %s, want %s", gotResumeAt, want)
	}
	if got := len(fs.injectedMsgs()); got != injectedAfterWork {
		t.Fatalf("must not inject before the backoff elapses, got %d injects", got-injectedAfterWork)
	}
	if !hasAPIErrorResumeActivity(rep.activityCalls()) {
		t.Fatalf("expected an api_error resume_scheduled activity, got %+v", rep.activityCalls())
	}

	// Backoff elapsed → OnTick injects the resume nudge once.
	clock.advance(3 * time.Second)
	c.OnTick(context.Background())
	msgs := fs.injectedMsgs()
	if len(msgs) != injectedAfterWork+1 || msgs[len(msgs)-1] != DefaultResumeNudge {
		t.Fatalf("OnTick must inject the resume nudge once after the backoff, got %+v", msgs)
	}
	c.mu.Lock()
	cleared := c.agents["agent-1"].rateLimitResumeAt.IsZero()
	c.mu.Unlock()
	if !cleared {
		t.Fatalf("resume slot must be consumed after resume")
	}
}

// TestAPIError_BoundedRetriesThenFails pins the cap: after APIErrorMaxRetries
// reschedules, a further transient error is treated as a hard failure — no resume
// scheduled, the in-flight pointer cleared (surfaceTurnFailure), and the counter reset
// — so a persistently-failing API surfaces instead of looping (and re-paying) forever.
func TestAPIError_BoundedRetriesThenFails(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	c, _, rs := newTestController(t, t.TempDir())
	c.cfg.Now = clock.now
	c.cfg.APIErrorMaxRetries = 3
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := c.Handle(context.Background(), workCmd(t, "agent-1", "wi-1", "brief", 2)); err != nil {
		t.Fatalf("work: %v", err)
	}
	fs := rs.last()
	errEv := claudestream.StreamEvent{Type: "result", Subtype: "error_during_execution", Result: "API Error: Connection closed mid-response.", IsError: true}

	// 3 transient errors each schedule a resume (work preserved).
	for i := 1; i <= 3; i++ {
		fs.emit(errEv)
		c.mu.Lock()
		task, retries := c.agents["agent-1"].currentTaskID, c.agents["agent-1"].apiErrorRetries
		c.mu.Unlock()
		if task != "wi-1" {
			t.Fatalf("retry %d: work must stay in-flight, got %q", i, task)
		}
		if retries != i {
			t.Fatalf("retry %d: apiErrorRetries = %d, want %d", i, retries, i)
		}
	}

	// 4th transient error exceeds the budget → fail: pointer cleared, counter reset.
	fs.emit(errEv)
	c.mu.Lock()
	ma := c.agents["agent-1"]
	gotTask, gotResumeAt, gotRetries := ma.currentTaskID, ma.rateLimitResumeAt, ma.apiErrorRetries
	c.mu.Unlock()
	if gotTask != "" {
		t.Fatalf("budget spent → surfaceTurnFailure must clear currentTaskID, got %q", gotTask)
	}
	if !gotResumeAt.IsZero() {
		t.Fatalf("budget spent → must NOT schedule another resume, got %s", gotResumeAt)
	}
	if gotRetries != 0 {
		t.Fatalf("budget spent → retry counter must reset, got %d", gotRetries)
	}
}

// TestAPIError_CleanTurnResetsRetryBudget pins that a CLEAN turn-end clears the retry
// budget, so a recovered burst does not carry its count into a later, unrelated error.
func TestAPIError_CleanTurnResetsRetryBudget(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	c, _, rs := newTestController(t, t.TempDir())
	c.cfg.Now = clock.now
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := c.Handle(context.Background(), workCmd(t, "agent-1", "wi-1", "brief", 2)); err != nil {
		t.Fatalf("work: %v", err)
	}
	fs := rs.last()

	fs.emit(claudestream.StreamEvent{Type: "result", IsError: true, Result: "API Error: Connection closed mid-response."})
	c.mu.Lock()
	mid := c.agents["agent-1"].apiErrorRetries
	c.mu.Unlock()
	if mid != 1 {
		t.Fatalf("transient error must increment retries, got %d", mid)
	}

	// A clean turn-end (the resume recovered) must zero the budget.
	fs.emit(claudestream.StreamEvent{Type: "result", IsError: false, Result: "done", Subtype: "success"})
	c.mu.Lock()
	after := c.agents["agent-1"].apiErrorRetries
	c.mu.Unlock()
	if after != 0 {
		t.Fatalf("clean turn-end must reset apiErrorRetries, got %d", after)
	}
}

// TestAPIError_OrdinaryErrorStillFails pins that an ORDINARY is_error turn (not a
// transient API error and not a rate-limit) is unchanged: no resume is scheduled and
// surfaceTurnFailure still clears the in-flight pointer.
func TestAPIError_OrdinaryErrorStillFails(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	c, _, rs := newTestController(t, t.TempDir())
	c.cfg.Now = clock.now
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := c.Handle(context.Background(), workCmd(t, "agent-1", "wi-1", "brief", 2)); err != nil {
		t.Fatalf("work: %v", err)
	}
	fs := rs.last()

	fs.emit(claudestream.StreamEvent{Type: "result", Subtype: "error_during_execution", Result: "model x not found", IsError: true})

	c.mu.Lock()
	ma := c.agents["agent-1"]
	gotTask, gotResumeAt, gotRetries := ma.currentTaskID, ma.rateLimitResumeAt, ma.apiErrorRetries
	c.mu.Unlock()
	if gotTask != "" {
		t.Fatalf("an ordinary is_error turn must clear currentTaskID, got %q", gotTask)
	}
	if !gotResumeAt.IsZero() {
		t.Fatalf("an ordinary is_error turn must NOT schedule a resume")
	}
	if gotRetries != 0 {
		t.Fatalf("an ordinary is_error turn must NOT touch the retry counter, got %d", gotRetries)
	}
}

// TestAPIError_NoResumeWhenNoInflightWork pins that a transient error with NO in-flight
// WorkItem (idle/converse turn) does not schedule a resume — nothing to re-drive.
func TestAPIError_NoResumeWhenNoInflightWork(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	c, _, rs := newTestController(t, t.TempDir())
	c.cfg.Now = clock.now
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	fs := rs.last()
	// No work() injected → currentTaskID empty.
	fs.emit(claudestream.StreamEvent{Type: "result", IsError: true, Result: "API Error: Connection closed mid-response."})

	c.mu.Lock()
	ma := c.agents["agent-1"]
	resumeAt, retries := ma.rateLimitResumeAt, ma.apiErrorRetries
	c.mu.Unlock()
	if !resumeAt.IsZero() {
		t.Fatalf("no in-flight work → must NOT schedule a resume, got %s", resumeAt)
	}
	if retries != 0 {
		t.Fatalf("no in-flight work → must NOT increment retries, got %d", retries)
	}
}

// TestAPIError_RateLimitTakesPrecedence pins onEvent ordering: an "overloaded" turn is
// claimed by the rate-limit path FIRST (it carries a server window), so the bounded
// api-error counter is never touched — the two recovery reasons do not double-count.
func TestAPIError_RateLimitTakesPrecedence(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	c, _, rs := newTestController(t, t.TempDir())
	c.cfg.Now = clock.now
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := c.Handle(context.Background(), workCmd(t, "agent-1", "wi-1", "brief", 2)); err != nil {
		t.Fatalf("work: %v", err)
	}
	fs := rs.last()

	fs.emit(claudestream.StreamEvent{Type: "rate_limit", RetryAfterSecs: 30})
	fs.emit(claudestream.StreamEvent{Type: "result", Subtype: "rate_limit_error", Result: "Overloaded", IsError: true})

	c.mu.Lock()
	ma := c.agents["agent-1"]
	gotTask, gotResumeAt, gotRetries := ma.currentTaskID, ma.rateLimitResumeAt, ma.apiErrorRetries
	c.mu.Unlock()
	if gotTask != "wi-1" {
		t.Fatalf("rate-limit resume must preserve currentTaskID, got %q", gotTask)
	}
	if want := clock.now().Add(30 * time.Second); !gotResumeAt.Equal(want) {
		t.Fatalf("rate-limit window (30s) must own the resume time, got %s want %s", gotResumeAt, want)
	}
	if gotRetries != 0 {
		t.Fatalf("rate-limit path must NOT bump the api-error retry counter, got %d", gotRetries)
	}
}

// TestAPIErrorResumePayload pins the activity payload carries the attempt/cap + a
// bounded message and the resume time.
func TestAPIErrorResumePayload(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	p := apiErrorResumePayload(StreamEvent{Subtype: "error_during_execution", Result: "API Error: Connection closed mid-response."}, 2, 5, at)
	for _, want := range []string{`"type":"api_error"`, `"action":"resume_scheduled"`, `"attempt":2`, `"max":5`, `"resume_at":`, `"subtype":"error_during_execution"`, `"message":"API Error: Connection closed mid-response."`} {
		if !strings.Contains(p, want) {
			t.Fatalf("payload %s missing %s", p, want)
		}
	}
}

func hasAPIErrorResumeActivity(calls []activityCall) bool {
	for _, a := range calls {
		if a.eventType == "api_error" && strings.Contains(a.payload, "resume_scheduled") {
			return true
		}
	}
	return false
}
