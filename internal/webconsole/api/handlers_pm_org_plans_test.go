package api

import "testing"

// v2.10.0 [T6] — the global cross-project Plan list default-filter semantics.
// The "all open" default (empty explicit status) shows draft/running/done plans
// and hides only the terminal `archived` state; an explicit status filter shows
// exactly its members. (The aggregation/iteration + builtin-pool exclusion are
// covered end-to-end by the run-real capture against a seeded instance.)
func TestPlanStatusPasses_T6_DefaultExcludesArchivedOnly(t *testing.T) {
	// default view: every non-archived status passes.
	for _, s := range []string{"draft", "running", "done"} {
		if !statusPasses(s, map[string]bool{}, planTerminalStatus) {
			t.Errorf("default Plan view should pass %q", s)
		}
	}
	// archived is the only terminal/hidden status by default.
	if statusPasses("archived", map[string]bool{}, planTerminalStatus) {
		t.Errorf("default Plan view should exclude archived")
	}
	// explicit filter: only members pass (even archived if explicitly asked).
	explicit := map[string]bool{"done": true}
	if !statusPasses("done", explicit, planTerminalStatus) {
		t.Errorf("explicit {done} should pass done")
	}
	if statusPasses("running", explicit, planTerminalStatus) {
		t.Errorf("explicit {done} should exclude running")
	}
	if !statusPasses("archived", map[string]bool{"archived": true}, planTerminalStatus) {
		t.Errorf("explicit {archived} should pass archived")
	}
}
