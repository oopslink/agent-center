package workerdaemon

import (
	"testing"

	"github.com/oopslink/agent-center/internal/workerdaemon/agentruntime"
)

// TestMaybeReportUsage covers the v2.15.0 I28/F2 worker turn-end usage hook:
// a clean result with token totals is reported (with the cache splits mapped
// through), an empty turn is skipped, and the DisableUsageReport kill-switch
// suppresses reporting. taskID is now passed in by the caller (captured at the
// `result` boundary — issue-af03da2f), not re-read from the managedAgent.
func TestMaybeReportUsage(t *testing.T) {
	c, rep, _ := newTestController(t, t.TempDir())
	st := c.installTestAgent("agent-1")
	withAgentState(c, "agent-1", func() { st.Model = "claude-opus-4-8" })

	ev := StreamEvent{
		Type: "result", TokensIn: 100, TokensOut: 50,
		CacheReadTokens: 200, CacheWriteTokens: 10,
	}
	c.maybeReportUsage("agent-1", ev, "task-7")

	if len(rep.usages) != 1 {
		t.Fatalf("usages = %d, want 1", len(rep.usages))
	}
	u := rep.usages[0]
	if u.agentID != "agent-1" || u.model != "claude-opus-4-8" || u.taskID != "task-7" ||
		u.inTok != 100 || u.outTok != 50 || u.cacheReadTok != 200 || u.cacheWriteTok != 10 {
		t.Fatalf("unexpected usage report: %+v", u)
	}

	// Empty turn (no tokens observed) → nothing to account.
	c.maybeReportUsage("agent-1", StreamEvent{Type: "result"}, "task-7")
	if len(rep.usages) != 1 {
		t.Fatalf("empty turn must not report; usages=%d", len(rep.usages))
	}

	// Kill-switch ON → suppressed.
	c.cfg.DisableUsageReport = true
	c.maybeReportUsage("agent-1", ev, "task-7")
	if len(rep.usages) != 1 {
		t.Fatalf("kill-switch must suppress reporting; usages=%d", len(rep.usages))
	}

	// Unknown agent (no managedAgent → no model) → skipped, no panic.
	c.cfg.DisableUsageReport = false
	c.maybeReportUsage("ghost", ev, "task-7")
	if len(rep.usages) != 1 {
		t.Fatalf("unknown agent must be skipped; usages=%d", len(rep.usages))
	}
}

// TestUsageTaskAtResult covers the issue-af03da2f attribution boundary: per-turn
// usage must bill the task the agent was working on, derived from its own
// start_task/complete_task MCP calls (the pull model never sets currentTaskID).
// The tricky case is a single-turn task: complete_task clears eventTaskID BEFORE
// the turn-end `result`, so without lastEventTaskID the completing turn would bill
// no task and the Top Cost Tasks panel stays empty.
func TestUsageTaskAtResult(t *testing.T) {
	st := &agentruntime.SessionState{}

	// Converse/idle turn — no task in flight → unattributed (non-task overhead).
	if got := st.UsageTaskAtResult(); got != "" {
		t.Fatalf("idle turn: got %q, want \"\"", got)
	}

	// Mid-task turn — start_task opened eventTaskID, not yet completed.
	st.EventTaskID = "task-A"
	if got := st.UsageTaskAtResult(); got != "task-A" {
		t.Fatalf("mid-task turn: got %q, want task-A", got)
	}
	// A second mid-task result still attributes (eventTaskID persists across turns).
	if got := st.UsageTaskAtResult(); got != "task-A" {
		t.Fatalf("mid-task turn 2: got %q, want task-A", got)
	}

	// Completing turn — complete_task cleared eventTaskID and stashed lastEventTaskID.
	st.EventTaskID = ""
	st.LastEventTaskID = "task-A"
	if got := st.UsageTaskAtResult(); got != "task-A" {
		t.Fatalf("completing turn: got %q, want task-A (from lastEventTaskID)", got)
	}

	// The NEXT turn (converse after completion) must NOT bill the finished task —
	// lastEventTaskID was consumed above.
	if got := st.UsageTaskAtResult(); got != "" {
		t.Fatalf("post-completion converse turn: got %q, want \"\" (no leak)", got)
	}
}
