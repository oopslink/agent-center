package workerdaemon

import "testing"

// TestMaybeReportUsage covers the v2.15.0 I28/F2 worker turn-end usage hook:
// a clean result with token totals is reported (with the cache splits mapped
// through), an empty turn is skipped, and the DisableUsageReport kill-switch
// suppresses reporting.
func TestMaybeReportUsage(t *testing.T) {
	c, rep, _ := newTestController(t, t.TempDir())
	c.agents["agent-1"] = &managedAgent{
		agentID: "agent-1", model: "claude-opus-4-8", currentTaskID: "task-7",
	}

	ev := StreamEvent{
		Type: "result", TokensIn: 100, TokensOut: 50,
		CacheReadTokens: 200, CacheWriteTokens: 10,
	}
	c.maybeReportUsage("agent-1", ev)

	if len(rep.usages) != 1 {
		t.Fatalf("usages = %d, want 1", len(rep.usages))
	}
	u := rep.usages[0]
	if u.agentID != "agent-1" || u.model != "claude-opus-4-8" || u.taskID != "task-7" ||
		u.inTok != 100 || u.outTok != 50 || u.cacheReadTok != 200 || u.cacheWriteTok != 10 {
		t.Fatalf("unexpected usage report: %+v", u)
	}

	// Empty turn (no tokens observed) → nothing to account.
	c.maybeReportUsage("agent-1", StreamEvent{Type: "result"})
	if len(rep.usages) != 1 {
		t.Fatalf("empty turn must not report; usages=%d", len(rep.usages))
	}

	// Kill-switch ON → suppressed.
	c.cfg.DisableUsageReport = true
	c.maybeReportUsage("agent-1", ev)
	if len(rep.usages) != 1 {
		t.Fatalf("kill-switch must suppress reporting; usages=%d", len(rep.usages))
	}

	// Unknown agent (no managedAgent → no model) → skipped, no panic.
	c.cfg.DisableUsageReport = false
	c.maybeReportUsage("ghost", ev)
	if len(rep.usages) != 1 {
		t.Fatalf("unknown agent must be skipped; usages=%d", len(rep.usages))
	}
}
