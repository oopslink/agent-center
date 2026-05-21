package scheduler

import (
	"testing"

	"github.com/oopslink/agent-center/internal/cognition"
)

func TestRefsForScope_AllPaths(t *testing.T) {
	cases := []struct {
		kind cognition.ScopeKind
		key  string
	}{
		{cognition.ScopeTask, "T-1"},
		{cognition.ScopeIssue, "I-1"},
		{cognition.ScopeConversation, "C-1"},
		{cognition.ScopeWorker, "W-1"},
		{cognition.ScopeGlobal, ""},
	}
	for _, tc := range cases {
		s, err := cognition.NewInvocationScope(tc.kind, tc.key)
		if err != nil {
			t.Fatalf("%v: %v", tc.kind, err)
		}
		_ = refsForScope(s) // exercise switch
	}
}

func TestTruncateBoundary(t *testing.T) {
	if got := truncate("abc", 3); got != "abc" {
		t.Errorf("eq len = %q", got)
	}
}

func TestNewInMemoryQueue_DefaultsToFive(t *testing.T) {
	q := NewInMemoryQueue(0)
	if q.cap != 5 {
		t.Errorf("cap = %d", q.cap)
	}
	q2 := NewInMemoryQueue(-1)
	if q2.cap != 5 {
		t.Errorf("negative cap defaults")
	}
}

func TestDecrementULID_Edges(t *testing.T) {
	// trim back, normal char
	if string(decrementULID("ABCD")) != "ABCC" {
		t.Errorf("normal = %s", decrementULID("ABCD"))
	}
	// last char '0' → truncate
	if string(decrementULID("ABC0")) != "ABC" {
		t.Errorf("zero last = %s", decrementULID("ABC0"))
	}
	if string(decrementULID("")) != "" {
		t.Error("empty")
	}
}
