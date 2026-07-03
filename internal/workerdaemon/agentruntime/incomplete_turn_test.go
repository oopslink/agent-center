package agentruntime

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/claudestream"
)

// TestIsIncompleteTurnMarker pins the T799 truncation detector: the connection-drop
// phrases claude prints as assistant TEXT match (case-insensitively); ordinary text
// and empty text do not. (Ported from T799's api_error_test.go into agentruntime, where
// 0b/0c relocated the turn-recovery logic — the regression guard lives with the fix.)
func TestIsIncompleteTurnMarker(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"connection closed mid-response", "API Error: Connection closed mid-response.", true},
		{"response above may be incomplete", "The response above may be incomplete.", true},
		{"full issue line", "API Error: Connection closed mid-response. The response above may be incomplete.", true},
		{"case-insensitive", "connection CLOSED MID-response", true},
		{"ordinary text", "Here is the answer you asked for.", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isIncompleteTurnMarker(tc.text); got != tc.want {
				t.Fatalf("isIncompleteTurnMarker(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

// advClock is a mutable, mutex-guarded clock for the resume-backoff tests.
type advClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *advClock) now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *advClock) advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func (r *recReporter) hasActivity(eventType string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.activity {
		if a == eventType {
			return true
		}
	}
	return false
}

// incompleteTurnRuntime builds a LocalRuntime wired for the onEvent→resume→Tick path
// with an advanceable clock and a recording reporter (mirrors the daemon harness that
// T799's controller-level tests used, driven directly against LocalRuntime).
func incompleteTurnRuntime(t *testing.T) (*LocalRuntime, *SessionState, *recReporter, *advClock, *fakeSession) {
	t.Helper()
	rep := &recReporter{}
	clk := &advClock{t: time.Unix(1_000_000, 0)}
	st := &SessionState{}
	fs := &fakeSession{}
	st.Session = fs
	cfg := LocalRuntimeConfig{
		AgentID:  "agent-1",
		Reporter: rep,
		Log:      func(string, ...any) {},
		Now:      clk.now,
	}
	return NewLocalRuntime(cfg, st), st, rep, clk, fs
}

// TestAPIError_IncompleteMarkerSchedulesResumeOnCleanResult is the core T799 fix: a
// turn whose assistant_text carried the truncation marker but ended with a `result`
// is_error=FALSE (claude "succeeded" after printing the connection-drop as text) must
// STILL schedule a bounded resume of the in-flight work — instead of being treated as
// a clean turn and left silently incomplete.
func TestAPIError_IncompleteMarkerSchedulesResumeOnCleanResult(t *testing.T) {
	rt, st, rep, clk, fs := incompleteTurnRuntime(t)
	st.CurrentTaskID = "wi-1" // an in-flight WorkItem

	// The turn prints the connection-drop as ordinary assistant TEXT, then ends the
	// turn "successfully" (is_error=false) — the shape that escaped IsTransientAPIError.
	rt.onEvent(claudestream.StreamEvent{Type: "assistant_text", Text: "API Error: Connection closed mid-response. The response above may be incomplete."})
	rt.onEvent(claudestream.StreamEvent{Type: "result", Subtype: "success", IsError: false})

	rt.StateMu().Lock()
	gotTask, gotResumeAt, gotRetries, gotFlag := st.CurrentTaskID, st.RateLimitResumeAt, st.APIErrorRetries, st.SawIncompleteTurn
	rt.StateMu().Unlock()
	if gotTask != "wi-1" {
		t.Fatalf("incomplete turn must PRESERVE CurrentTaskID for resume, got %q", gotTask)
	}
	if gotRetries != 1 {
		t.Fatalf("incomplete turn must set APIErrorRetries=1, got %d", gotRetries)
	}
	if gotFlag {
		t.Fatalf("SawIncompleteTurn must be consumed at turn-end, still set")
	}
	if want := clk.now().Add(2 * time.Second); !gotResumeAt.Equal(want) {
		t.Fatalf("resumeAt = %s, want %s", gotResumeAt, want)
	}
	if !rep.hasActivity("api_error") {
		t.Fatalf("expected an api_error resume_scheduled activity, got %+v", rep.activity)
	}

	// Backoff elapsed → Tick injects the resume nudge into the still-live session.
	clk.advance(3 * time.Second)
	if err := rt.Tick(context.Background(), clk.now()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	msgs := fs.msgs()
	if len(msgs) == 0 || msgs[len(msgs)-1] != DefaultResumeNudge {
		t.Fatalf("Tick must inject the resume nudge after the backoff, got %+v", msgs)
	}
}

// TestAPIError_IncompleteMarkerResumesConverseTurn covers the T799 guard relaxation: a
// DM/@mention (agent.converse) turn has NO WorkItem but DOES have an in-flight
// conversation — a truncated converse turn must resume (re-drive the same turn) instead
// of only posting a system notice.
func TestAPIError_IncompleteMarkerResumesConverseTurn(t *testing.T) {
	rt, st, _, clk, fs := incompleteTurnRuntime(t)
	// A converse turn: sets CurrentConversationID, leaves CurrentTaskID empty.
	st.CurrentConversationID = "conv-1"

	rt.onEvent(claudestream.StreamEvent{Type: "assistant_text", Text: "…the response above may be incomplete."})
	rt.onEvent(claudestream.StreamEvent{Type: "result", Subtype: "success", IsError: false})

	rt.StateMu().Lock()
	gotConv, gotTask, gotResumeAt, gotRetries := st.CurrentConversationID, st.CurrentTaskID, st.RateLimitResumeAt, st.APIErrorRetries
	rt.StateMu().Unlock()
	if gotTask != "" {
		t.Fatalf("converse turn must have no WorkItem, got CurrentTaskID=%q", gotTask)
	}
	if gotConv != "conv-1" {
		t.Fatalf("converse resume must PRESERVE CurrentConversationID, got %q", gotConv)
	}
	if gotRetries != 1 {
		t.Fatalf("truncated converse turn must set APIErrorRetries=1, got %d", gotRetries)
	}
	if gotResumeAt.IsZero() {
		t.Fatalf("truncated converse turn must schedule a resume (guard relaxed to convID)")
	}

	// The resume drains into the live session like any other.
	clk.advance(3 * time.Second)
	if err := rt.Tick(context.Background(), clk.now()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	msgs := fs.msgs()
	if len(msgs) == 0 || msgs[len(msgs)-1] != DefaultResumeNudge {
		t.Fatalf("Tick must inject the resume nudge for the converse turn, got %+v", msgs)
	}
}
