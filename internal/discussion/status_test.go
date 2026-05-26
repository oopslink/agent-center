package discussion

import "testing"

func TestStatus_IsValidAndTerminal(t *testing.T) {
	valid := []Status{
		StatusOpen, StatusUnderDiscussion, StatusConcluded,
		StatusClosedNoAction, StatusClosedWithTasks, StatusWithdrawn,
	}
	for _, s := range valid {
		if !s.IsValid() {
			t.Errorf("expected %s valid", s)
		}
	}
	if Status("bogus").IsValid() {
		t.Error("bogus should not be valid")
	}
	if !StatusClosedNoAction.IsTerminal() ||
		!StatusClosedWithTasks.IsTerminal() ||
		!StatusWithdrawn.IsTerminal() {
		t.Error("closed_* / withdrawn must be terminal")
	}
	if StatusOpen.IsTerminal() {
		t.Error("open must not be terminal")
	}
}

func TestStatus_StringRoundTrip(t *testing.T) {
	if StatusOpen.String() != "open" {
		t.Fatal("string mismatch")
	}
}

func TestCanTransitionTo_LegalSet(t *testing.T) {
	legal := []struct{ from, to Status }{
		{StatusOpen, StatusUnderDiscussion},
		{StatusOpen, StatusConcluded},
		{StatusOpen, StatusClosedNoAction},
		{StatusOpen, StatusClosedWithTasks},
		{StatusOpen, StatusWithdrawn},
		{StatusUnderDiscussion, StatusConcluded},
		{StatusUnderDiscussion, StatusClosedWithTasks},
		{StatusUnderDiscussion, StatusClosedNoAction},
		{StatusUnderDiscussion, StatusWithdrawn},
		{StatusConcluded, StatusClosedNoAction},
		{StatusConcluded, StatusClosedWithTasks},
		{StatusConcluded, StatusWithdrawn},
		// v2.5.x #64 (c semantics @oopslink #agent-center:93118955):
		// reopen is legal from any concluded/withdrawn terminal back to
		// open. Spawned tasks (closed_with_tasks) are not cascaded.
		{StatusClosedNoAction, StatusOpen},
		{StatusClosedWithTasks, StatusOpen},
		{StatusWithdrawn, StatusOpen},
	}
	for _, c := range legal {
		if !CanTransitionTo(c.from, c.to) {
			t.Errorf("expected legal: %s → %s", c.from, c.to)
		}
	}
}

func TestCanTransitionTo_IllegalSet(t *testing.T) {
	illegal := []struct{ from, to Status }{
		// Terminal states still reject non-open targets (reopen is
		// explicitly the only outgoing edge added).
		{StatusClosedNoAction, StatusUnderDiscussion},
		{StatusWithdrawn, StatusConcluded},
		{StatusClosedWithTasks, StatusClosedNoAction},
		{StatusClosedNoAction, StatusClosedWithTasks},
		{StatusOpen, Status("bogus")},
		{Status("bogus"), StatusOpen},
	}
	for _, c := range illegal {
		if CanTransitionTo(c.from, c.to) {
			t.Errorf("expected illegal: %s → %s", c.from, c.to)
		}
	}
}
