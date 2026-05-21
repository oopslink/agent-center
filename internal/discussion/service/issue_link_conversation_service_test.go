package service

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/discussion"
	"github.com/oopslink/agent-center/internal/observability"
)

func TestLink_HappyAndDedupe(t *testing.T) {
	h := newHarness(t)
	lifecycle := h.lifecycle(t)
	ls := NewIssueLinkConversationService(h.db, h.issueRepo, h.convRepo, h.clk)

	res, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	cid := setupSecondConvKind(t, h, conversation.ConversationKindDM)
	if err := ls.Link(context.Background(), LinkInput{
		IssueID: res.IssueID, ConversationID: cid, Actor: observability.Actor("user:h"),
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := h.issueRepo.FindByID(context.Background(), res.IssueID)
	if len(got.RelatedConversationIDs()) != 1 || got.RelatedConversationIDs()[0] != cid {
		t.Fatalf("related: %v", got.RelatedConversationIDs())
	}
	// dedupe — second link no-op
	v0 := got.Version()
	if err := ls.Link(context.Background(), LinkInput{
		IssueID: res.IssueID, ConversationID: cid, Actor: observability.Actor("user:h"),
	}); err != nil {
		t.Fatal(err)
	}
	got, _ = h.issueRepo.FindByID(context.Background(), res.IssueID)
	if got.Version() != v0 {
		t.Fatal("dedupe should not bump version")
	}
	if len(got.RelatedConversationIDs()) != 1 {
		t.Fatalf("dedupe failed, got %d", len(got.RelatedConversationIDs()))
	}
}

func TestLink_TargetConversationMissing(t *testing.T) {
	h := newHarness(t)
	lifecycle := h.lifecycle(t)
	ls := NewIssueLinkConversationService(h.db, h.issueRepo, h.convRepo, h.clk)
	res, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	err := ls.Link(context.Background(), LinkInput{
		IssueID: res.IssueID, ConversationID: "ghost", Actor: observability.Actor("user:h"),
	})
	if !errors.Is(err, conversation.ErrConversationNotFound) {
		t.Fatalf("expected not_found, got %v", err)
	}
}

func TestLink_BadInputsAndIssueMissing(t *testing.T) {
	h := newHarness(t)
	ls := NewIssueLinkConversationService(h.db, h.issueRepo, h.convRepo, h.clk)
	if err := ls.Link(context.Background(), LinkInput{IssueID: "X", ConversationID: "C", Actor: "BAD"}); err == nil {
		t.Fatal("bad actor")
	}
	if err := ls.Link(context.Background(), LinkInput{IssueID: "", ConversationID: "C", Actor: observability.Actor("user:h")}); err == nil {
		t.Fatal("empty issue")
	}
	if err := ls.Link(context.Background(), LinkInput{IssueID: "X", ConversationID: "", Actor: observability.Actor("user:h")}); err == nil {
		t.Fatal("empty conv")
	}
	// non-existent issue
	if err := ls.Link(context.Background(), LinkInput{IssueID: "ghost", ConversationID: "C", Actor: observability.Actor("user:h")}); !errors.Is(err, discussion.ErrIssueNotFound) {
		t.Fatalf("expected issue not_found, got %v", err)
	}
}

func TestLink_TerminalIssueRejected(t *testing.T) {
	h := newHarness(t)
	lifecycle := h.lifecycle(t)
	ls := NewIssueLinkConversationService(h.db, h.issueRepo, h.convRepo, h.clk)
	res, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	if _, err := lifecycle.Withdraw(context.Background(), WithdrawIssueCommand{
		IssueID: res.IssueID, Reason: "x", Message: "y", WithdrawnBy: "user:h",
		Actor: observability.Actor("user:h"),
	}); err != nil {
		t.Fatal(err)
	}
	cid := setupSecondConvKind(t, h, conversation.ConversationKindDM)
	err := ls.Link(context.Background(), LinkInput{
		IssueID: res.IssueID, ConversationID: cid, Actor: observability.Actor("user:h"),
	})
	if !errors.Is(err, discussion.ErrIssueInvalidTransition) {
		t.Fatalf("expected invalid transition, got %v", err)
	}
}
