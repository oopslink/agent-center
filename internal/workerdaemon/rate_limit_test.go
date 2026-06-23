package workerdaemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/claudestream"
)

// TestIsRateLimitError pins the classifier: only a `result` is_error turn whose
// subtype/result/raw carries a rate-limit signature counts (so an ordinary failure
// still surfaces normally, and a bare rate_limit event — which does not END a turn —
// is not misclassified here).
func TestIsRateLimitError(t *testing.T) {
	cases := []struct {
		name string
		ev   StreamEvent
		want bool
	}{
		{"result rate limit text", StreamEvent{Type: "result", IsError: true, Result: "API Error: rate limit exceeded"}, true},
		{"result rate_limit_error subtype", StreamEvent{Type: "result", IsError: true, Subtype: "rate_limit_error"}, true},
		{"result overloaded", StreamEvent{Type: "result", IsError: true, Result: "Overloaded"}, true},
		{"result 429 in raw", StreamEvent{Type: "result", IsError: true, Raw: []byte(`{"error":{"status":429}}`)}, true},
		{"result usage limit", StreamEvent{Type: "result", IsError: true, Result: "5-hour usage limit reached"}, true},
		{"ordinary error not rate limit", StreamEvent{Type: "result", IsError: true, Result: "model x not found"}, false},
		{"rate limit text but not error", StreamEvent{Type: "result", IsError: false, Result: "rate limit"}, false},
		{"non-result type", StreamEvent{Type: "rate_limit", Result: "rate limit"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRateLimitError(tc.ev); got != tc.want {
				t.Fatalf("isRateLimitError = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestDecideRateLimitResume pins the window→delay policy: prefer retry_after, then
// resets_at relative to now, then the default — clamped to [min, max] so a tiny,
// past, or absurd window can neither hot-loop nor park the agent forever.
func TestDecideRateLimitResume(t *testing.T) {
	p := rateLimitParams{defaultBackoff: 60 * time.Second, minBackoff: 5 * time.Second, maxBackoff: time.Hour}
	now := time.Unix(1_000_000, 0)
	cases := []struct {
		name       string
		retryAfter int
		resetAt    int64
		want       time.Duration
	}{
		{"retry_after used", 30, 0, 30 * time.Second},
		{"tiny retry_after floored", 2, 0, 5 * time.Second},
		{"resets_at relative", 0, now.Unix() + 45, 45 * time.Second},
		{"past resets_at floored", 0, now.Unix() - 100, 5 * time.Second},
		{"no window → default", 0, 0, 60 * time.Second},
		{"huge retry_after capped", 100_000, 0, time.Hour},
		{"retry_after wins over resets_at", 20, now.Unix() + 999, 20 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decideRateLimitResume(tc.retryAfter, tc.resetAt, now, p); got != tc.want {
				t.Fatalf("decideRateLimitResume = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestRateLimit_SchedulesResumeAndOnTickResumes is the end-to-end controller path:
// a rate-limit is_error turn must NOT abandon the in-flight work (currentTaskID
// preserved, no surface-failure clear) but schedule a resume at now+window; the
// OnTick that finds it due injects the resume nudge into the still-live session and
// consumes the schedule (idempotent — a second tick does not re-inject).
func TestRateLimit_SchedulesResumeAndOnTickResumes(t *testing.T) {
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

	// claude emits the rate-limit window, then the turn ends in a rate-limit error.
	fs.emit(claudestream.StreamEvent{Type: "rate_limit", RetryAfterSecs: 30})
	fs.emit(claudestream.StreamEvent{Type: "result", Subtype: "error_during_execution", Result: "API Error: rate_limit_error", IsError: true})

	// The in-flight work is NOT abandoned, and a resume is scheduled at now+30s.
	c.mu.Lock()
	ma := c.agents["agent-1"]
	gotTask := ma.currentTaskID
	gotResumeAt := ma.rateLimitResumeAt
	c.mu.Unlock()
	if gotTask != "wi-1" {
		t.Fatalf("rate-limited turn must PRESERVE currentTaskID for resume, got %q", gotTask)
	}
	if want := clock.now().Add(30 * time.Second); !gotResumeAt.Equal(want) {
		t.Fatalf("rateLimitResumeAt = %s, want %s", gotResumeAt, want)
	}
	// It did not inject anything yet (the window has not cleared).
	if got := len(fs.injectedMsgs()); got != injectedAfterWork {
		t.Fatalf("must not inject before the window clears, got %d injects", got-injectedAfterWork)
	}
	// And it surfaced a rate_limit activity flagged as a scheduled resume.
	if !hasRateLimitResumeActivity(rep.activityCalls()) {
		t.Fatalf("expected a rate_limit resume_scheduled activity, got %+v", rep.activityCalls())
	}

	// Before the window clears, OnTick must NOT resume.
	clock.advance(10 * time.Second)
	c.OnTick(context.Background())
	if got := len(fs.injectedMsgs()); got != injectedAfterWork {
		t.Fatalf("OnTick resumed before the window cleared")
	}

	// Window cleared → OnTick injects the resume nudge once.
	clock.advance(25 * time.Second) // now past now+30s
	c.OnTick(context.Background())
	msgs := fs.injectedMsgs()
	if len(msgs) != injectedAfterWork+1 || msgs[len(msgs)-1] != DefaultResumeNudge {
		t.Fatalf("OnTick must inject the resume nudge once on window clear, got %+v", msgs)
	}
	// Schedule consumed → a further tick does not re-inject.
	c.mu.Lock()
	cleared := c.agents["agent-1"].rateLimitResumeAt.IsZero()
	c.mu.Unlock()
	if !cleared {
		t.Fatalf("rateLimitResumeAt must be consumed after resume")
	}
	clock.advance(time.Minute)
	c.OnTick(context.Background())
	if got := len(fs.injectedMsgs()); got != injectedAfterWork+1 {
		t.Fatalf("a consumed resume must not re-fire, got %d injects", got-injectedAfterWork)
	}
}

// TestRateLimit_NonRateLimitErrorStillFails pins that an ORDINARY is_error turn is
// unchanged by the rate-limit path: no resume is scheduled and surfaceTurnFailure
// still clears the in-flight pointer (the agent did not get stuck pretending to be
// rate-limited).
func TestRateLimit_NonRateLimitErrorStillFails(t *testing.T) {
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
	gotTask := ma.currentTaskID
	gotResumeAt := ma.rateLimitResumeAt
	c.mu.Unlock()
	if gotTask != "" {
		t.Fatalf("an ordinary is_error turn must clear currentTaskID (surfaceTurnFailure), got %q", gotTask)
	}
	if !gotResumeAt.IsZero() {
		t.Fatalf("an ordinary is_error turn must NOT schedule a rate-limit resume")
	}
}

// TestRateLimit_NoResumeWhenNoInflightWork pins that a rate-limit is_error with NO
// in-flight WorkItem (e.g. an idle/converse turn) does not schedule a resume — there
// is nothing to re-drive, so it falls through to the normal failure surface.
func TestRateLimit_NoResumeWhenNoInflightWork(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	c, _, rs := newTestController(t, t.TempDir())
	c.cfg.Now = clock.now
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	fs := rs.last()
	// No work() injected → currentTaskID empty.
	fs.emit(claudestream.StreamEvent{Type: "result", Subtype: "rate_limit_error", Result: "overloaded", IsError: true})

	c.mu.Lock()
	resumeAt := c.agents["agent-1"].rateLimitResumeAt
	c.mu.Unlock()
	if !resumeAt.IsZero() {
		t.Fatalf("no in-flight work → must NOT schedule a resume, got %s", resumeAt)
	}
}

// TestRateLimit_DrainSkipsDeadSession pins that a due resume whose session has gone
// (crashed/stopped) is dropped — the crash-driven self-heal path owns that recovery,
// not the rate-limit drain.
func TestRateLimit_DrainSkipsDeadSession(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	c, _, _ := newTestController(t, t.TempDir())
	c.cfg.Now = clock.now
	defer c.Shutdown(context.Background())

	// A managed agent with a DUE resume but no live session.
	c.mu.Lock()
	c.agents["agent-1"] = &managedAgent{
		agentID:           "agent-1",
		currentTaskID:     "wi-1",
		rateLimitResumeAt: clock.now().Add(-time.Second),
	}
	c.mu.Unlock()

	c.OnTick(context.Background())

	c.mu.Lock()
	resumeAt := c.agents["agent-1"].rateLimitResumeAt
	c.mu.Unlock()
	if !resumeAt.IsZero() {
		t.Fatalf("a due resume with a dead session must be cleared, got %s", resumeAt)
	}
}

// TestRateLimit_DrainInjectErrorTolerated pins that an Inject failure during the
// resume (session closing mid-drain) is logged and dropped, not fatal — the schedule
// is still consumed.
func TestRateLimit_DrainInjectErrorTolerated(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	c, _, rs := newTestController(t, t.TempDir())
	c.cfg.Now = clock.now
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	fs := rs.last()
	fs.mu.Lock()
	fs.stopped = true // make Inject return ErrSessionClosed
	fs.mu.Unlock()

	c.mu.Lock()
	c.agents["agent-1"].currentTaskID = "wi-1"
	c.agents["agent-1"].rateLimitResumeAt = clock.now().Add(-time.Second)
	c.mu.Unlock()

	c.OnTick(context.Background()) // must not panic; schedule consumed despite the error

	c.mu.Lock()
	resumeAt := c.agents["agent-1"].rateLimitResumeAt
	c.mu.Unlock()
	if !resumeAt.IsZero() {
		t.Fatalf("schedule must be consumed even when Inject fails, got %s", resumeAt)
	}
}

// TestResumeNudgeText_ConfiguredOverride pins the configured ResumeNudge wins over
// the default (the shared helper used by rate-limit + self-heal + boot relaunch).
func TestResumeNudgeText_ConfiguredOverride(t *testing.T) {
	c, _, _ := newTestController(t, t.TempDir())
	if got := c.resumeNudgeText(); got != DefaultResumeNudge {
		t.Fatalf("unset → default, got %q", got)
	}
	c.cfg.ResumeNudge = "继续你的任务"
	if got := c.resumeNudgeText(); got != "继续你的任务" {
		t.Fatalf("configured nudge must win, got %q", got)
	}
}

// TestRateLimitResumePayload pins the activity payload carries the window fields it
// has (resets_at + subtype here; the retry_after branch is covered by the e2e test).
func TestRateLimitResumePayload(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	p := rateLimitResumePayload(StreamEvent{Subtype: "rate_limit_error"}, 0, 1_700_000_500, at)
	for _, want := range []string{`"action":"resume_scheduled"`, `"resets_at":1700000500`, `"subtype":"rate_limit_error"`, `"resume_at":`} {
		if !strings.Contains(p, want) {
			t.Fatalf("payload %s missing %s", p, want)
		}
	}
	if strings.Contains(p, "retry_after") {
		t.Fatalf("payload must omit absent retry_after: %s", p)
	}
}

func hasRateLimitResumeActivity(calls []activityCall) bool {
	for _, a := range calls {
		if a.eventType == "rate_limit" && strings.Contains(a.payload, "resume_scheduled") {
			return true
		}
	}
	return false
}
