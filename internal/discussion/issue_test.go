package discussion

import (
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
)

func mustIssue(t *testing.T, in NewIssueInput) *Issue {
	t.Helper()
	if in.OpenedAt.IsZero() {
		in.OpenedAt = time.Date(2026, 5, 21, 1, 2, 3, 0, time.UTC)
	}
	i, err := NewIssue(in)
	if err != nil {
		t.Fatalf("new issue: %v", err)
	}
	return i
}

func TestNewIssue_HappyAndFails(t *testing.T) {
	base := NewIssueInput{
		ID:                 "ISS-1",
		ProjectID:          "P-1",
		Title:              "hello",
		OpenedByIdentityID: "user:hayang",
		Origin:             OriginCLI,
		OpenedAt:           time.Now().UTC(),
	}
	i, err := NewIssue(base)
	if err != nil {
		t.Fatalf("happy: %v", err)
	}
	if i.Status() != StatusOpen || i.Version() != 1 {
		t.Fatal("default status/version wrong")
	}
	if i.HasConversation() {
		t.Fatal("default no conversation")
	}

	// Now mutate base to break each invariant.
	for name, mut := range map[string]func(*NewIssueInput){
		"empty_id":     func(in *NewIssueInput) { in.ID = "" },
		"empty_proj":   func(in *NewIssueInput) { in.ProjectID = "" },
		"empty_title":  func(in *NewIssueInput) { in.Title = "" },
		"empty_opener": func(in *NewIssueInput) { in.OpenedByIdentityID = "" },
		"bad_origin":   func(in *NewIssueInput) { in.Origin = "bogus" },
		"zero_at":      func(in *NewIssueInput) { in.OpenedAt = time.Time{} },
	} {
		t.Run(name, func(t *testing.T) {
			in := base
			mut(&in)
			if _, err := NewIssue(in); err == nil {
				t.Fatalf("%s should error", name)
			}
		})
	}
}

func TestIssue_MarkUnderDiscussion_LegalAndIdempotent(t *testing.T) {
	i := mustIssue(t, NewIssueInput{
		ID: "I", ProjectID: "P", Title: "t",
		OpenedByIdentityID: "user:h", Origin: OriginCLI,
	})
	now := time.Now().UTC()
	if err := i.MarkUnderDiscussion(now); err != nil {
		t.Fatalf("first: %v", err)
	}
	if i.Status() != StatusUnderDiscussion || i.Version() != 2 {
		t.Fatal("status/version wrong after first")
	}
	// Idempotent
	if err := i.MarkUnderDiscussion(now); err != nil {
		t.Fatal("idempotent")
	}
	if i.Version() != 2 {
		t.Fatal("version should not bump on idempotent")
	}
}

func TestIssue_MarkUnderDiscussion_Illegal(t *testing.T) {
	i := mustIssue(t, NewIssueInput{
		ID: "I", ProjectID: "P", Title: "t",
		OpenedByIdentityID: "user:h", Origin: OriginCLI,
	})
	if err := i.Withdraw("dup", "x", "user:h", time.Now()); err != nil {
		t.Fatal(err)
	}
	err := i.MarkUnderDiscussion(time.Now())
	if !errors.Is(err, ErrIssueInvalidTransition) {
		t.Fatalf("expected invalid transition, got %v", err)
	}
}

func TestIssue_Withdraw_LegalAndIllegal(t *testing.T) {
	i := mustIssue(t, NewIssueInput{
		ID: "I", ProjectID: "P", Title: "t",
		OpenedByIdentityID: "user:h", Origin: OriginCLI,
	})
	if err := i.Withdraw("duplicate", "dup of #5", "user:h", time.Now()); err != nil {
		t.Fatal(err)
	}
	if i.Status() != StatusWithdrawn || i.WithdrawReason() == "" || i.WithdrawMessage() == "" {
		t.Fatal("withdraw fields not set")
	}
	if i.ConcludedAt() == nil || i.ConcludedByIdentityID() != "user:h" {
		t.Fatal("concluded fields should also be set on withdraw")
	}
	// Re-withdraw blocked
	err := i.Withdraw("x", "y", "user:h", time.Now())
	if !errors.Is(err, ErrIssueWithdrawn) {
		t.Fatalf("re-withdraw: %v", err)
	}

	// Withdraw missing reason / message / actor
	j := mustIssue(t, NewIssueInput{
		ID: "J", ProjectID: "P", Title: "t",
		OpenedByIdentityID: "user:h", Origin: OriginCLI,
	})
	if err := j.Withdraw("", "x", "user:h", time.Now()); err == nil {
		t.Fatal("empty reason should err")
	}
	if err := j.Withdraw("r", "", "user:h", time.Now()); err == nil {
		t.Fatal("empty message should err")
	}
	if err := j.Withdraw("r", "x", "", time.Now()); err == nil {
		t.Fatal("empty actor should err")
	}
}

func TestIssue_Withdraw_FromTerminalRejected(t *testing.T) {
	i := mustIssue(t, NewIssueInput{
		ID: "I", ProjectID: "P", Title: "t",
		OpenedByIdentityID: "user:h", Origin: OriginCLI,
	})
	if err := i.Conclude(Resolution{Kind: ResolutionClosedNoAction, Summary: "skip"}, "user:h", time.Now()); err != nil {
		t.Fatal(err)
	}
	// Now closed_no_action; Withdraw should err
	err := i.Withdraw("x", "y", "user:h", time.Now())
	if !errors.Is(err, ErrIssueInvalidTransition) {
		t.Fatalf("expected invalid transition, got %v", err)
	}
}

func TestIssue_Conclude_AllKinds(t *testing.T) {
	cases := []struct {
		name      string
		res       Resolution
		want      Status
		expectErr bool
	}{
		{"no_action", Resolution{Kind: ResolutionClosedNoAction, Summary: "skip"}, StatusClosedNoAction, false},
		{"with_tasks", Resolution{
			Kind: ResolutionClosedWithTasks, Summary: "go",
			Tasks: []dispatch.IssueConcludeTaskSpec{{LocalID: "a", Title: "T1"}},
		}, StatusClosedWithTasks, false},
		{"withdrawn", Resolution{Kind: ResolutionWithdrawn, Summary: "back"}, StatusWithdrawn, false},
		{"bad_resolution", Resolution{Kind: "bogus", Summary: "x"}, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			i := mustIssue(t, NewIssueInput{
				ID: "I", ProjectID: "P", Title: "t",
				OpenedByIdentityID: "user:h", Origin: OriginCLI,
			})
			err := i.Conclude(c.res, "user:h", time.Now())
			if c.expectErr && err == nil {
				t.Fatal("expected err")
			}
			if !c.expectErr {
				if err != nil {
					t.Fatalf("unexpected: %v", err)
				}
				if i.Status() != c.want {
					t.Fatalf("status=%s want %s", i.Status(), c.want)
				}
				if i.ConclusionSummary() == "" || i.ConcludedByIdentityID() == "" {
					t.Fatal("conclusion fields not set")
				}
			}
		})
	}
}

func TestIssue_Conclude_TerminalRejected(t *testing.T) {
	i := mustIssue(t, NewIssueInput{
		ID: "I", ProjectID: "P", Title: "t",
		OpenedByIdentityID: "user:h", Origin: OriginCLI,
	})
	if err := i.Conclude(Resolution{Kind: ResolutionClosedNoAction, Summary: "skip"}, "user:h", time.Now()); err != nil {
		t.Fatal(err)
	}
	err := i.Conclude(Resolution{Kind: ResolutionClosedNoAction, Summary: "again"}, "user:h", time.Now())
	if !errors.Is(err, ErrIssueAlreadyConcluded) {
		t.Fatalf("expected already concluded, got %v", err)
	}

	// withdrawn separately
	j := mustIssue(t, NewIssueInput{
		ID: "J", ProjectID: "P", Title: "t",
		OpenedByIdentityID: "user:h", Origin: OriginCLI,
	})
	if err := j.Withdraw("x", "y", "user:h", time.Now()); err != nil {
		t.Fatal(err)
	}
	err = j.Conclude(Resolution{Kind: ResolutionClosedNoAction, Summary: "skip"}, "user:h", time.Now())
	if !errors.Is(err, ErrIssueWithdrawn) {
		t.Fatalf("expected withdrawn, got %v", err)
	}
}

func TestIssue_Conclude_EmptyConcludedBy(t *testing.T) {
	i := mustIssue(t, NewIssueInput{
		ID: "I", ProjectID: "P", Title: "t",
		OpenedByIdentityID: "user:h", Origin: OriginCLI,
	})
	err := i.Conclude(Resolution{Kind: ResolutionClosedNoAction, Summary: "skip"}, "", time.Now())
	if err == nil {
		t.Fatal("expected empty concluded_by err")
	}
}

func TestIssue_BindConversation_FlowsAndRejects(t *testing.T) {
	i := mustIssue(t, NewIssueInput{
		ID: "I", ProjectID: "P", Title: "t",
		OpenedByIdentityID: "user:h", Origin: OriginCLI,
	})
	if err := i.BindConversation("conv-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if i.ConversationID() != "conv-1" {
		t.Fatal("conv id wrong")
	}
	// Rebind rejected
	err := i.BindConversation("conv-2", time.Now())
	if !errors.Is(err, ErrIssueInvalidTransition) {
		t.Fatal("rebind must fail")
	}
	// Empty rejected
	j := mustIssue(t, NewIssueInput{
		ID: "J", ProjectID: "P", Title: "t",
		OpenedByIdentityID: "user:h", Origin: OriginCLI,
	})
	if err := j.BindConversation("", time.Now()); err == nil {
		t.Fatal("empty conv id should err")
	}
	// Terminal rejected
	if err := j.Conclude(Resolution{Kind: ResolutionClosedNoAction, Summary: "skip"}, "user:h", time.Now()); err != nil {
		t.Fatal(err)
	}
	err = j.BindConversation("conv-x", time.Now())
	if !errors.Is(err, ErrIssueInvalidTransition) {
		t.Fatal("terminal bind must fail")
	}
}

func TestIssue_AddRelatedConversation_DedupeAndReject(t *testing.T) {
	i := mustIssue(t, NewIssueInput{
		ID: "I", ProjectID: "P", Title: "t",
		OpenedByIdentityID: "user:h", Origin: OriginCLI,
	})
	v0 := i.Version()
	if err := i.AddRelatedConversation("c1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if i.Version() != v0+1 {
		t.Fatal("version should bump")
	}
	if err := i.AddRelatedConversation("c1", time.Now()); err != nil {
		t.Fatal("dedupe should not err")
	}
	if i.Version() != v0+1 {
		t.Fatal("version should not bump on dedupe")
	}
	if err := i.AddRelatedConversation("", time.Now()); err == nil {
		t.Fatal("empty should err")
	}
	rel := i.RelatedConversationIDs()
	if len(rel) != 1 || rel[0] != "c1" {
		t.Fatalf("rel: %v", rel)
	}
	// Terminal rejected
	if err := i.Withdraw("x", "y", "user:h", time.Now()); err != nil {
		t.Fatal(err)
	}
	err := i.AddRelatedConversation("c2", time.Now())
	if !errors.Is(err, ErrIssueInvalidTransition) {
		t.Fatal("terminal link must fail")
	}
}

func TestIssue_MarshalUnmarshalRelatedConversationIDsJSON(t *testing.T) {
	i := mustIssue(t, NewIssueInput{
		ID: "I", ProjectID: "P", Title: "t",
		OpenedByIdentityID: "user:h", Origin: OriginCLI,
	})
	// Empty
	s, err := i.MarshalRelatedConversationIDsJSON()
	if err != nil || s != "[]" {
		t.Fatalf("empty marshal: %q %v", s, err)
	}
	_ = i.AddRelatedConversation("c1", time.Now())
	_ = i.AddRelatedConversation("c2", time.Now())
	s, err = i.MarshalRelatedConversationIDsJSON()
	if err != nil {
		t.Fatal(err)
	}
	ids, err := UnmarshalRelatedConversationIDsJSON(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "c1" || ids[1] != "c2" {
		t.Fatalf("roundtrip: %v", ids)
	}
	// Empty input
	ids, err = UnmarshalRelatedConversationIDsJSON("")
	if err != nil || ids != nil {
		t.Fatalf("empty unmarshal: %v / %v", ids, err)
	}
	// Bad input
	if _, err := UnmarshalRelatedConversationIDsJSON("not-json"); err == nil {
		t.Fatal("bad json should err")
	}
}

func TestRehydrateIssue_RoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	concAt := now.Add(time.Hour)
	in := RehydrateIssueInput{
		ID:                     "I",
		ProjectID:              "P",
		Title:                  "t",
		Description:            "desc",
		DescriptionBlobRef:     "blob://x",
		OpenedByIdentityID:     "user:h",
		Origin:                 OriginWebConsole,
		OpenedAt:               now,
		Status:                 StatusUnderDiscussion,
		ConcludedAt:            &concAt,
		ConclusionSummary:      "summary",
		ConcludedByIdentityID:  "user:y",
		WithdrawReason:         "",
		WithdrawMessage:        "",
		ConversationID:         conversation.ConversationID("conv-1"),
		RelatedConversationIDs: []conversation.ConversationID{"c2", "c3"},
		CreatedAt:              now,
		UpdatedAt:              now,
		Version:                3,
	}
	i, err := RehydrateIssue(in)
	if err != nil {
		t.Fatal(err)
	}
	if i.Status() != StatusUnderDiscussion ||
		i.ConclusionSummary() != "summary" ||
		i.ConversationID() != "conv-1" ||
		len(i.RelatedConversationIDs()) != 2 ||
		i.ConcludedAt() == nil {
		t.Fatalf("roundtrip mismatch: %+v", i)
	}

	// bad status
	bad := in
	bad.Status = "bogus"
	if _, err := RehydrateIssue(bad); err == nil {
		t.Fatal("bad status should err")
	}
	// bad origin
	bad = in
	bad.Origin = "bogus"
	if _, err := RehydrateIssue(bad); err == nil {
		t.Fatal("bad origin should err")
	}
	// bad version
	bad = in
	bad.Version = 0
	if _, err := RehydrateIssue(bad); err == nil {
		t.Fatal("bad version should err")
	}
}
