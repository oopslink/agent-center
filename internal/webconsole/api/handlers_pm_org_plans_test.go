package api

import "testing"

// v2.10.0 [T6] — the global cross-project Plan list default-filter semantics.
// The active default shows pending/running/paused plans and hides terminal
// done/discarded plans; an explicit status filter shows exactly its members.
// (The aggregation/iteration + AssignmentPool exclusion are
// covered end-to-end by the run-real capture against a seeded instance.)
func TestPlanStatusPasses_T6_DefaultExcludesTerminal(t *testing.T) {
	for _, s := range []string{"pending", "running", "paused"} {
		if !statusPasses(s, map[string]bool{}, planTerminalStatus) {
			t.Errorf("default Plan view should pass %q", s)
		}
	}
	for _, s := range []string{"done", "discarded"} {
		if statusPasses(s, map[string]bool{}, planTerminalStatus) {
			t.Errorf("default Plan view should exclude %s", s)
		}
	}
	// explicit filter: only members pass, including terminal statuses.
	explicit := map[string]bool{"done": true}
	if !statusPasses("done", explicit, planTerminalStatus) {
		t.Errorf("explicit {done} should pass done")
	}
	if statusPasses("running", explicit, planTerminalStatus) {
		t.Errorf("explicit {done} should exclude running")
	}
	if !statusPasses("discarded", map[string]bool{"discarded": true}, planTerminalStatus) {
		t.Errorf("explicit {discarded} should pass discarded")
	}
}
