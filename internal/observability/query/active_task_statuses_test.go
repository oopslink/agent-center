package query

import (
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TestActiveTaskStatuses_MatchesIsTerminal pins `activeTaskStatuses` (the default
// `query tasks` view) to be EXACTLY the non-terminal set — i.e. every pm.TaskStatus where
// !IsTerminal(), no more and no less.
//
// This test was added after the active-status list drifted because it did not exist. The comment on
// activeTaskStatuses claimed "Pinned to IsTerminal() by a partition test
// (TestActiveTaskStatuses_MatchesIsTerminal)" — the name was real, the test was not. So
// the list was in fact pinned by nothing: adding non-terminal statuses broke no test here,
// and had the list not been updated by hand, they would have been silently missing from the
// default active-work view forever. A comment asserting a guard is not a guard; this is the
// guard.
//
// It derives from pm.AllTaskStatuses() rather than a literal list on purpose — a copied
// list is what rots. If this fails, do not "fix" it by editing the expectation: decide
// deliberately whether the new status is active work, and set IsTerminal accordingly.
func TestActiveTaskStatuses_MatchesIsTerminal(t *testing.T) {
	inActive := make(map[pm.TaskStatus]bool, len(activeTaskStatuses))
	for _, s := range activeTaskStatuses {
		if inActive[s] {
			t.Fatalf("activeTaskStatuses contains %q twice", s)
		}
		inActive[s] = true
	}

	for _, s := range pm.AllTaskStatuses() {
		switch {
		case s.IsTerminal() && inActive[s]:
			t.Errorf("%q is terminal but is listed in activeTaskStatuses — concluded work must not show as active", s)
		case !s.IsTerminal() && !inActive[s]:
			t.Errorf("%q is non-terminal but is MISSING from activeTaskStatuses — active work would be invisible in the default task view", s)
		}
	}

	// No stray value that is not part of the enum at all.
	for _, s := range activeTaskStatuses {
		if !s.IsValid() {
			t.Errorf("activeTaskStatuses contains %q, which is not a valid TaskStatus", s)
		}
	}
}

// TestActiveTaskStatuses_IncludesParked is the read-layer guard: a PARKED task
// (blocked) is active work awaiting a human, so it must appear in the default active
// view. Dropping it there is how a task quietly stops being anybody's problem.
func TestActiveTaskStatuses_IncludesParked(t *testing.T) {
	for _, s := range []pm.TaskStatus{pm.TaskBlocked} {
		var found bool
		for _, a := range activeTaskStatuses {
			if a == s {
				found = true
			}
		}
		if !found {
			t.Errorf("parked status %q must be in the default active task view", s)
		}
	}
}
