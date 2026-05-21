package service

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/discussion"
	"github.com/oopslink/agent-center/internal/observability"
)

// realMessageWriterAdapter wraps the real Phase 1 MessageWriter so the
// IssueCommentService test path goes through the AddMessage stack.
type realMessageWriterAdapter struct {
	w *convservice.MessageWriter
}

func (r realMessageWriterAdapter) AddMessage(ctx context.Context, in convservice.AddMessageCommand) (convservice.AddMessageResult, error) {
	return r.w.AddMessage(ctx, in)
}

func openIssueWithConvBound(t *testing.T, h *testHarness, lifecycle *IssueLifecycleService) (discussion.IssueID, conversation.ConversationID) {
	t.Helper()
	res, err := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:hayang",
		Origin: discussion.OriginFeishuAt, Actor: observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return res.IssueID, res.ConversationID
}

func TestComment_HappyTriggerStart(t *testing.T) {
	h := newHarness(t)
	lifecycle := h.lifecycle(t)
	writer := convservice.NewMessageWriter(h.db, h.convRepo, h.msgRepo, h.sink, h.gen, h.clk)
	cs := NewIssueCommentService(h.issueRepo, h.convRepo, h.msgRepo, realMessageWriterAdapter{writer}, lifecycle, h.clk)

	issueID, _ := openIssueWithConvBound(t, h, lifecycle)

	// First non-opener comment → triggers discussion_started
	res, err := cs.Comment(context.Background(), CommentInput{
		IssueID:          issueID,
		Content:          "hello",
		ContentKind:      conversation.MessageContentText,
		SenderIdentityID: conversation.IdentityRef("user:peer"),
		Actor:            observability.Actor("user:peer"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.MessageID == "" {
		t.Fatal("message id empty")
	}
	got, _ := h.issueRepo.FindByID(context.Background(), issueID)
	if got.Status() != discussion.StatusUnderDiscussion {
		t.Fatalf("status: %s", got.Status())
	}
	if h.countEvents(t, "issue.discussion_started") != 1 {
		t.Fatal("discussion_started not emitted")
	}
}

func TestComment_OpenerSelfNoStart(t *testing.T) {
	h := newHarness(t)
	lifecycle := h.lifecycle(t)
	writer := convservice.NewMessageWriter(h.db, h.convRepo, h.msgRepo, h.sink, h.gen, h.clk)
	cs := NewIssueCommentService(h.issueRepo, h.convRepo, h.msgRepo, realMessageWriterAdapter{writer}, lifecycle, h.clk)

	issueID, _ := openIssueWithConvBound(t, h, lifecycle)
	// opener self-comment
	if _, err := cs.Comment(context.Background(), CommentInput{
		IssueID:          issueID,
		Content:          "self",
		ContentKind:      conversation.MessageContentText,
		SenderIdentityID: conversation.IdentityRef("user:hayang"),
		Actor:            observability.Actor("user:hayang"),
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := h.issueRepo.FindByID(context.Background(), issueID)
	if got.Status() != discussion.StatusOpen {
		t.Fatalf("status should remain open: %s", got.Status())
	}
	if h.countEvents(t, "issue.discussion_started") != 0 {
		t.Fatal("discussion_started should NOT fire on opener self-comment")
	}
}

func TestComment_NoConversationBound(t *testing.T) {
	h := newHarness(t)
	lifecycle := h.lifecycle(t)
	writer := convservice.NewMessageWriter(h.db, h.convRepo, h.msgRepo, h.sink, h.gen, h.clk)
	cs := NewIssueCommentService(h.issueRepo, h.convRepo, h.msgRepo, realMessageWriterAdapter{writer}, lifecycle, h.clk)
	// CLI path issue (no conversation)
	res, err := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:hayang",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = cs.Comment(context.Background(), CommentInput{
		IssueID:          res.IssueID,
		Content:          "x",
		ContentKind:      conversation.MessageContentText,
		SenderIdentityID: conversation.IdentityRef("user:peer"),
		Actor:            observability.Actor("user:peer"),
	})
	if !errors.Is(err, discussion.ErrIssueNoConversationBound) {
		t.Fatalf("expected ErrIssueNoConversationBound, got %v", err)
	}
}

func TestComment_RejectsWithdrawnAndTerminal(t *testing.T) {
	h := newHarness(t)
	lifecycle := h.lifecycle(t)
	writer := convservice.NewMessageWriter(h.db, h.convRepo, h.msgRepo, h.sink, h.gen, h.clk)
	cs := NewIssueCommentService(h.issueRepo, h.convRepo, h.msgRepo, realMessageWriterAdapter{writer}, lifecycle, h.clk)
	issueID, _ := openIssueWithConvBound(t, h, lifecycle)
	if _, err := lifecycle.Withdraw(context.Background(), WithdrawIssueCommand{
		IssueID: issueID, Reason: "dup", Message: "x", WithdrawnBy: "user:h",
		Actor: observability.Actor("user:h"),
	}); err != nil {
		t.Fatal(err)
	}
	_, err := cs.Comment(context.Background(), CommentInput{
		IssueID: issueID, Content: "x", ContentKind: conversation.MessageContentText,
		SenderIdentityID: conversation.IdentityRef("user:peer"), Actor: observability.Actor("user:peer"),
	})
	if !errors.Is(err, discussion.ErrIssueWithdrawn) {
		t.Fatalf("expected ErrIssueWithdrawn, got %v", err)
	}
}

func TestComment_RejectsInvalidInputs(t *testing.T) {
	h := newHarness(t)
	lifecycle := h.lifecycle(t)
	writer := convservice.NewMessageWriter(h.db, h.convRepo, h.msgRepo, h.sink, h.gen, h.clk)
	cs := NewIssueCommentService(h.issueRepo, h.convRepo, h.msgRepo, realMessageWriterAdapter{writer}, lifecycle, h.clk)
	issueID, _ := openIssueWithConvBound(t, h, lifecycle)

	cases := []struct {
		name string
		in   CommentInput
	}{
		{"bad_actor", CommentInput{IssueID: issueID, Content: "x", ContentKind: conversation.MessageContentText, SenderIdentityID: "user:p", Actor: "BAD"}},
		{"bad_sender", CommentInput{IssueID: issueID, Content: "x", ContentKind: conversation.MessageContentText, SenderIdentityID: "", Actor: observability.Actor("user:p")}},
		{"empty_content", CommentInput{IssueID: issueID, Content: "  ", ContentKind: conversation.MessageContentText, SenderIdentityID: "user:p", Actor: observability.Actor("user:p")}},
		{"bad_kind", CommentInput{IssueID: issueID, Content: "x", ContentKind: "bogus", SenderIdentityID: "user:p", Actor: observability.Actor("user:p")}},
		{"missing_issue", CommentInput{IssueID: "ghost", Content: "x", ContentKind: conversation.MessageContentText, SenderIdentityID: "user:p", Actor: observability.Actor("user:p")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := cs.Comment(context.Background(), c.in); err == nil {
				t.Fatal("expected err")
			}
		})
	}
}

func TestComment_DirectionDefaultsToInternal(t *testing.T) {
	h := newHarness(t)
	lifecycle := h.lifecycle(t)
	writer := convservice.NewMessageWriter(h.db, h.convRepo, h.msgRepo, h.sink, h.gen, h.clk)
	cs := NewIssueCommentService(h.issueRepo, h.convRepo, h.msgRepo, realMessageWriterAdapter{writer}, lifecycle, h.clk)
	issueID, _ := openIssueWithConvBound(t, h, lifecycle)
	// Pass invalid direction → defaults to internal
	if _, err := cs.Comment(context.Background(), CommentInput{
		IssueID:          issueID,
		Content:          "x",
		ContentKind:      conversation.MessageContentText,
		SenderIdentityID: conversation.IdentityRef("user:peer"),
		Direction:        "weird",
		Actor:            observability.Actor("user:peer"),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestComment_AddMessageError_Surfaces(t *testing.T) {
	h := newHarness(t)
	lifecycle := h.lifecycle(t)
	cs := NewIssueCommentService(h.issueRepo, h.convRepo, h.msgRepo, errorAdder{err: errors.New("boom")}, lifecycle, h.clk)
	issueID, _ := openIssueWithConvBound(t, h, lifecycle)
	if _, err := cs.Comment(context.Background(), CommentInput{
		IssueID:          issueID,
		Content:          "x",
		ContentKind:      conversation.MessageContentText,
		SenderIdentityID: conversation.IdentityRef("user:peer"),
		Actor:            observability.Actor("user:peer"),
	}); err == nil {
		t.Fatal("expected adder err to surface")
	}
}

type errorAdder struct{ err error }

func (e errorAdder) AddMessage(_ context.Context, _ convservice.AddMessageCommand) (convservice.AddMessageResult, error) {
	return convservice.AddMessageResult{}, e.err
}
