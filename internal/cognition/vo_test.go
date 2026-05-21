package cognition_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/observability"
)

func TestScopeKind_IsValid(t *testing.T) {
	cases := []struct {
		k    cognition.ScopeKind
		want bool
	}{
		{cognition.ScopeTask, true},
		{cognition.ScopeIssue, true},
		{cognition.ScopeConversation, true},
		{cognition.ScopeWorker, true},
		{cognition.ScopeGlobal, true},
		{cognition.ScopeKind("project"), false}, // memory-only
		{cognition.ScopeKind(""), false},
		{cognition.ScopeKind("bogus"), false},
	}
	for _, tc := range cases {
		if got := tc.k.IsValid(); got != tc.want {
			t.Errorf("ScopeKind(%q).IsValid() = %v, want %v", tc.k, got, tc.want)
		}
	}
}

func TestParseScopeKind(t *testing.T) {
	if _, err := cognition.ParseScopeKind("task"); err != nil {
		t.Fatalf("parse task: %v", err)
	}
	_, err := cognition.ParseScopeKind("bogus")
	if !errors.Is(err, cognition.ErrUnknownScopeKind) {
		t.Fatalf("unknown: got %v, want ErrUnknownScopeKind", err)
	}
}

func TestNewInvocationScope_HappyAndGlobal(t *testing.T) {
	s, err := cognition.NewInvocationScope(cognition.ScopeTask, "T-1")
	if err != nil {
		t.Fatalf("happy: %v", err)
	}
	if s.Kind() != cognition.ScopeTask || s.Key() != "T-1" {
		t.Errorf("got %+v", s)
	}
	if s.String() != "task:T-1" {
		t.Errorf("String() = %q", s.String())
	}
	// global rules
	g, err := cognition.NewInvocationScope(cognition.ScopeGlobal, "")
	if err != nil {
		t.Fatalf("global empty key: %v", err)
	}
	if g.Key() != cognition.GlobalScopeKey {
		t.Errorf("global key = %q, want %q", g.Key(), cognition.GlobalScopeKey)
	}
	if g.String() != "global" {
		t.Errorf("global String() = %q", g.String())
	}
}

func TestNewInvocationScope_GlobalKeyMismatch(t *testing.T) {
	if _, err := cognition.NewInvocationScope(cognition.ScopeGlobal, "wrong"); err == nil {
		t.Fatal("expected error for bad global key")
	}
}

func TestNewInvocationScope_EmptyKey(t *testing.T) {
	if _, err := cognition.NewInvocationScope(cognition.ScopeTask, ""); err == nil {
		t.Fatal("expected error for empty key")
	}
	if _, err := cognition.NewInvocationScope(cognition.ScopeTask, "   "); err == nil {
		t.Fatal("expected error for whitespace key")
	}
}

func TestNewInvocationScope_PathTraversalRejected(t *testing.T) {
	bad := []string{"../foo", "..", "foo/bar", "foo\\bar", "foo:bar", "foo\x00bar"}
	for _, b := range bad {
		if _, err := cognition.NewInvocationScope(cognition.ScopeTask, b); err == nil {
			t.Errorf("expected error for path-traversal key %q", b)
		}
	}
}

func TestNewInvocationScope_UnknownKind(t *testing.T) {
	if _, err := cognition.NewInvocationScope(cognition.ScopeKind("bogus"), "x"); !errors.Is(err, cognition.ErrUnknownScopeKind) {
		t.Fatalf("unknown kind: %v", err)
	}
}

func TestInvocationScope_Equal(t *testing.T) {
	a := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-1")
	b := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-1")
	c := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-2")
	if !a.Equal(b) {
		t.Error("a == b")
	}
	if a.Equal(c) {
		t.Error("a != c")
	}
}

func TestInvocationScope_JSON(t *testing.T) {
	s := cognition.MustNewInvocationScope(cognition.ScopeIssue, "I-9")
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got cognition.InvocationScope
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.Equal(s) {
		t.Errorf("roundtrip mismatch: %v vs %v", got, s)
	}
	// bad payload
	if err := json.Unmarshal([]byte(`{"kind":"bogus","key":"x"}`), &got); err == nil {
		t.Error("expected error on bogus kind")
	}
}

func TestInvocationScope_Zero(t *testing.T) {
	var s cognition.InvocationScope
	if !s.IsZero() {
		t.Error("zero scope should report IsZero")
	}
}

func TestTriggerEventSet_HappyAndSort(t *testing.T) {
	in := []observability.EventID{"01H3", "01H1", "01H2", "01H1"} // duplicate
	set, err := cognition.NewTriggerEventSet(in)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if set.Len() != 3 {
		t.Errorf("len = %d, want 3", set.Len())
	}
	got := set.IDs()
	want := []observability.EventID{"01H1", "01H2", "01H3"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sorted IDs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if !set.Contains("01H2") {
		t.Error("Contains(01H2)")
	}
	if set.Contains("not_present") {
		t.Error("Contains(not_present) false")
	}
}

func TestTriggerEventSet_Empty(t *testing.T) {
	if _, err := cognition.NewTriggerEventSet(nil); err == nil {
		t.Error("expected error for nil ids")
	}
	if _, err := cognition.NewTriggerEventSet([]observability.EventID{}); err == nil {
		t.Error("expected error for empty ids")
	}
	if _, err := cognition.NewTriggerEventSet([]observability.EventID{""}); err == nil {
		t.Error("expected error for empty id element")
	}
}

func TestTriggerEventSet_JSON(t *testing.T) {
	set, _ := cognition.NewTriggerEventSet([]observability.EventID{"A", "B"})
	b, _ := json.Marshal(set)
	want := `["A","B"]`
	if string(b) != want {
		t.Errorf("marshal = %s, want %s", b, want)
	}
	var got cognition.TriggerEventSet
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Len() != 2 {
		t.Errorf("roundtrip len: %d", got.Len())
	}
	// bad
	if err := json.Unmarshal([]byte(`[]`), &got); err == nil {
		t.Error("expected error on empty")
	}
}

func TestTriggerEventSet_EmptyMarshal(t *testing.T) {
	var zero cognition.TriggerEventSet
	b, err := json.Marshal(zero)
	if err != nil {
		t.Fatalf("marshal zero: %v", err)
	}
	if string(b) != "[]" {
		t.Errorf("zero marshal = %s", b)
	}
}

func TestInvocationStatus_IsValidIsTerminal(t *testing.T) {
	for _, s := range []cognition.InvocationStatus{cognition.StatusRunning, cognition.StatusSucceeded, cognition.StatusFailed, cognition.StatusTimedOut} {
		if !s.IsValid() {
			t.Errorf("%s !IsValid", s)
		}
	}
	if cognition.InvocationStatus("xxx").IsValid() {
		t.Error("bogus IsValid")
	}
	if cognition.StatusRunning.IsTerminal() {
		t.Error("running terminal")
	}
	for _, s := range []cognition.InvocationStatus{cognition.StatusSucceeded, cognition.StatusFailed, cognition.StatusTimedOut} {
		if !s.IsTerminal() {
			t.Errorf("%s !Terminal", s)
		}
	}
	if got := cognition.StatusRunning.String(); got != "running" {
		t.Errorf("String() = %q", got)
	}
}

func TestParseInvocationStatus(t *testing.T) {
	if _, err := cognition.ParseInvocationStatus("running"); err != nil {
		t.Fatalf("running: %v", err)
	}
	_, err := cognition.ParseInvocationStatus("zz")
	if !errors.Is(err, cognition.ErrUnknownStatus) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestInvocationFailedReason_IsValid(t *testing.T) {
	for _, r := range []cognition.InvocationFailedReason{
		cognition.FailedReasonClaudeNonZero, cognition.FailedReasonCLICommandError,
		cognition.FailedReasonOOM, cognition.FailedReasonCenterRestartOrphan,
		cognition.FailedReasonKilledByAdmin, cognition.FailedReasonUnknown,
	} {
		if !r.IsValid() {
			t.Errorf("%s !valid", r)
		}
	}
	if cognition.InvocationFailedReason("nope").IsValid() {
		t.Error("nope valid")
	}
	if got := cognition.FailedReasonOOM.String(); got != "oom" {
		t.Errorf("string = %q", got)
	}
}

func TestTokenUsage_Add(t *testing.T) {
	a := cognition.TokenUsage{Input: 10, Output: 20}
	b := cognition.TokenUsage{Input: 5, CacheRead: 1, CacheCreate: 2}
	c := a.Add(b)
	if c.Input != 15 || c.Output != 20 || c.CacheRead != 1 || c.CacheCreate != 2 {
		t.Errorf("add: %+v", c)
	}
	var z cognition.TokenUsage
	if !z.IsZero() {
		t.Error("zero")
	}
	if c.IsZero() {
		t.Error("c IsZero")
	}
}

func TestHardTimeoutFor(t *testing.T) {
	cases := map[cognition.ScopeKind]time.Duration{
		cognition.ScopeTask:         180 * time.Second,
		cognition.ScopeIssue:        180 * time.Second,
		cognition.ScopeConversation: 180 * time.Second,
		cognition.ScopeWorker:       180 * time.Second,
		cognition.ScopeGlobal:       600 * time.Second,
	}
	for k, want := range cases {
		got := cognition.HardTimeoutFor(k).Duration()
		if got != want {
			t.Errorf("HardTimeoutFor(%s) = %v, want %v", k, got, want)
		}
		if cognition.HardTimeoutFor(k).Seconds() != int(want.Seconds()) {
			t.Errorf("HardTimeoutFor(%s).Seconds()", k)
		}
	}
}

func TestDecisionKind_AllAndParse(t *testing.T) {
	all := cognition.AllDecisionKinds()
	if len(all) != 12 {
		t.Fatalf("len = %d, want 12", len(all))
	}
	for _, k := range all {
		if !k.IsValid() {
			t.Errorf("%s !valid", k)
		}
		parsed, err := cognition.ParseDecisionKind(string(k))
		if err != nil || parsed != k {
			t.Errorf("parse(%s): %v", k, err)
		}
	}
	if cognition.DecisionKind("invalid").IsValid() {
		t.Error("invalid IsValid")
	}
	_, err := cognition.ParseDecisionKind("xx")
	if !errors.Is(err, cognition.ErrUnknownDecisionKind) {
		t.Fatal("expected ErrUnknownDecisionKind")
	}
	if cognition.DecisionDispatch.String() != "dispatch" {
		t.Errorf("String() = %q", cognition.DecisionDispatch.String())
	}
}

func TestDecisionOutcome_IsValid(t *testing.T) {
	if !cognition.OutcomeSucceeded.IsValid() || !cognition.OutcomeFailed.IsValid() {
		t.Fatal("happy")
	}
	if cognition.DecisionOutcome("bogus").IsValid() {
		t.Fatal("bogus")
	}
	if cognition.OutcomeFailed.String() != "failed" {
		t.Errorf("string = %q", cognition.OutcomeFailed.String())
	}
}

func TestEventIDsAsStrings(t *testing.T) {
	got := cognition.EventIDsAsStrings([]observability.EventID{"a", "b"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("got %v", got)
	}
}
