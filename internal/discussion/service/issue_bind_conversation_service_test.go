package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/discussion"
	"github.com/oopslink/agent-center/internal/observability"
)

func TestBindAuto_HappyPath(t *testing.T) {
	h := newHarness(t)
	lifecycle := h.lifecycle(t)
	opener := NewIssueConversationOpener(h.convRepo, h.sink, h.gen, h.clk)
	bs := NewIssueBindConversationService(h.db, h.issueRepo, h.convRepo, opener, h.sink, h.clk)

	// Open via CLI (lazy)
	res, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	if got, _ := h.issueRepo.FindByID(context.Background(), res.IssueID); got.HasConversation() {
		t.Fatal("CLI path issue should start unbound")
	}
	convID, err := bs.BindAuto(context.Background(), BindAutoInput{
		IssueID: res.IssueID, Channel: "web", Actor: observability.Actor("user:h"),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := h.issueRepo.FindByID(context.Background(), res.IssueID)
	if got.ConversationID() != convID {
		t.Fatal("bind didn't write conversation_id")
	}
	if h.countEvents(t, "conversation.opened") != 1 {
		t.Fatal("conversation.opened expected")
	}
}

func TestBindAuto_RejectsAlreadyBound(t *testing.T) {
	h := newHarness(t)
	lifecycle := h.lifecycle(t)
	opener := NewIssueConversationOpener(h.convRepo, h.sink, h.gen, h.clk)
	bs := NewIssueBindConversationService(h.db, h.issueRepo, h.convRepo, opener, h.sink, h.clk)
	// Sync-build: pre-bound
	res, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginFeishuAt, Actor: observability.Actor("user:h"),
	})
	if _, err := bs.BindAuto(context.Background(), BindAutoInput{
		IssueID: res.IssueID, Actor: observability.Actor("user:h"),
	}); !errors.Is(err, discussion.ErrIssueInvalidTransition) {
		t.Fatalf("expected invalid transition, got %v", err)
	}
}

func TestBindAuto_RejectsTerminal(t *testing.T) {
	h := newHarness(t)
	lifecycle := h.lifecycle(t)
	opener := NewIssueConversationOpener(h.convRepo, h.sink, h.gen, h.clk)
	bs := NewIssueBindConversationService(h.db, h.issueRepo, h.convRepo, opener, h.sink, h.clk)
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
	if _, err := bs.BindAuto(context.Background(), BindAutoInput{
		IssueID: res.IssueID, Actor: observability.Actor("user:h"),
	}); !errors.Is(err, discussion.ErrIssueInvalidTransition) {
		t.Fatalf("expected invalid transition, got %v", err)
	}
}

func TestBindAuto_RejectsBadInputs(t *testing.T) {
	h := newHarness(t)
	opener := NewIssueConversationOpener(h.convRepo, h.sink, h.gen, h.clk)
	bs := NewIssueBindConversationService(h.db, h.issueRepo, h.convRepo, opener, h.sink, h.clk)
	if _, err := bs.BindAuto(context.Background(), BindAutoInput{IssueID: "X", Actor: "BAD"}); err == nil {
		t.Fatal("bad actor")
	}
	if _, err := bs.BindAuto(context.Background(), BindAutoInput{IssueID: "", Actor: observability.Actor("user:h")}); err == nil {
		t.Fatal("empty issue")
	}
	// nil opener
	bs2 := NewIssueBindConversationService(h.db, h.issueRepo, h.convRepo, nil, h.sink, h.clk)
	if _, err := bs2.BindAuto(context.Background(), BindAutoInput{IssueID: "X", Actor: observability.Actor("user:h")}); err == nil {
		t.Fatal("nil opener err")
	}
	// non-existent issue
	if _, err := bs.BindAuto(context.Background(), BindAutoInput{IssueID: "ghost", Actor: observability.Actor("user:h")}); !errors.Is(err, discussion.ErrIssueNotFound) {
		t.Fatalf("expected not_found, got %v", err)
	}
}

func TestBindAuto_OpenerFailsRollsBack(t *testing.T) {
	h := newHarness(t)
	lifecycle := h.lifecycle(t)
	bs := NewIssueBindConversationService(h.db, h.issueRepo, h.convRepo, failingOpener{}, h.sink, h.clk)
	res, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	if _, err := bs.BindAuto(context.Background(), BindAutoInput{
		IssueID: res.IssueID, Actor: observability.Actor("user:h"),
	}); err == nil {
		t.Fatal("expected err")
	}
	got, _ := h.issueRepo.FindByID(context.Background(), res.IssueID)
	if got.HasConversation() {
		t.Fatal("issue should still be unbound after rollback")
	}
}

func setupSecondConvKind(t *testing.T, h *testHarness, kind conversation.ConversationKind) conversation.ConversationID {
	t.Helper()
	conv, err := conversation.NewConversation(conversation.NewConversationInput{
		ID:        conversation.ConversationID("CONV-X"),
		Kind:      kind,
		Name:      "stand-alone",
		CreatedBy: conversation.IdentityRef("system"),
		OpenedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.convRepo.Save(context.Background(), conv); err != nil {
		t.Fatal(err)
	}
	return conv.ID()
}

func TestBindTo_HappyPath(t *testing.T) {
	h := newHarness(t)
	lifecycle := h.lifecycle(t)
	opener := NewIssueConversationOpener(h.convRepo, h.sink, h.gen, h.clk)
	bs := NewIssueBindConversationService(h.db, h.issueRepo, h.convRepo, opener, h.sink, h.clk)

	res, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	cid := setupSecondConvKind(t, h, conversation.ConversationKindIssue)
	if err := bs.BindTo(context.Background(), BindToInput{
		IssueID: res.IssueID, ConversationID: cid, Actor: observability.Actor("user:h"),
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := h.issueRepo.FindByID(context.Background(), res.IssueID)
	if got.ConversationID() != cid {
		t.Fatal("conv id not written")
	}
}

func TestBindTo_RejectsWrongKind(t *testing.T) {
	h := newHarness(t)
	lifecycle := h.lifecycle(t)
	opener := NewIssueConversationOpener(h.convRepo, h.sink, h.gen, h.clk)
	bs := NewIssueBindConversationService(h.db, h.issueRepo, h.convRepo, opener, h.sink, h.clk)
	res, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	cid := setupSecondConvKind(t, h, conversation.ConversationKindDM)
	err := bs.BindTo(context.Background(), BindToInput{
		IssueID: res.IssueID, ConversationID: cid, Actor: observability.Actor("user:h"),
	})
	if !errors.Is(err, conversation.ErrConversationInvalidKind) {
		t.Fatalf("expected wrong kind, got %v", err)
	}
}

func TestBindTo_RejectsClosed(t *testing.T) {
	h := newHarness(t)
	lifecycle := h.lifecycle(t)
	opener := NewIssueConversationOpener(h.convRepo, h.sink, h.gen, h.clk)
	bs := NewIssueBindConversationService(h.db, h.issueRepo, h.convRepo, opener, h.sink, h.clk)
	res, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	cid := setupSecondConvKind(t, h, conversation.ConversationKindIssue)
	// close it
	now := time.Now().UTC()
	if err := h.convRepo.UpdateStatus(context.Background(), cid,
		conversation.ConversationActive, conversation.ConversationClosed,
		1, "reason", "msg", now); err != nil {
		t.Fatal(err)
	}
	err := bs.BindTo(context.Background(), BindToInput{
		IssueID: res.IssueID, ConversationID: cid, Actor: observability.Actor("user:h"),
	})
	if !errors.Is(err, conversation.ErrConversationClosed) {
		t.Fatalf("expected closed, got %v", err)
	}
}

func TestBindTo_RejectsAlreadyOwned(t *testing.T) {
	h := newHarness(t)
	lifecycle := h.lifecycle(t)
	opener := NewIssueConversationOpener(h.convRepo, h.sink, h.gen, h.clk)
	bs := NewIssueBindConversationService(h.db, h.issueRepo, h.convRepo, opener, h.sink, h.clk)

	// Issue A pre-bound via sync-build
	resA, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "A", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginFeishuAt, Actor: observability.Actor("user:h"),
	})
	// Issue B is CLI (unbound)
	resB, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "B", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	err := bs.BindTo(context.Background(), BindToInput{
		IssueID: resB.IssueID, ConversationID: resA.ConversationID, Actor: observability.Actor("user:h"),
	})
	if !errors.Is(err, conversation.ErrConversationAlreadyExists) {
		t.Fatalf("expected already_exists, got %v", err)
	}
}

func TestBindTo_RejectsAlreadyOwnedByUnderDiscussion(t *testing.T) {
	h := newHarness(t)
	lifecycle := h.lifecycle(t)
	opener := NewIssueConversationOpener(h.convRepo, h.sink, h.gen, h.clk)
	bs := NewIssueBindConversationService(h.db, h.issueRepo, h.convRepo, opener, h.sink, h.clk)

	// Issue A pre-bound via sync-build, then transition to under_discussion.
	resA, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "A", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginFeishuAt, Actor: observability.Actor("user:h"),
	})
	if _, err := lifecycle.RecordDiscussionStart(context.Background(), RecordDiscussionStartCommand{
		IssueID:               resA.IssueID,
		FirstMessageID:        "M1",
		FirstSenderIdentityID: conversation.IdentityRef("user:p"),
		Actor:                 observability.Actor("user:p"),
	}); err != nil {
		t.Fatal(err)
	}
	// New unbound issue B
	resB, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "B", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	err := bs.BindTo(context.Background(), BindToInput{
		IssueID: resB.IssueID, ConversationID: resA.ConversationID, Actor: observability.Actor("user:h"),
	})
	if !errors.Is(err, conversation.ErrConversationAlreadyExists) {
		t.Fatalf("expected already_exists, got %v", err)
	}
}

func TestBindTo_RejectsBadInputsAndAlreadyBound(t *testing.T) {
	h := newHarness(t)
	lifecycle := h.lifecycle(t)
	opener := NewIssueConversationOpener(h.convRepo, h.sink, h.gen, h.clk)
	bs := NewIssueBindConversationService(h.db, h.issueRepo, h.convRepo, opener, h.sink, h.clk)
	// pre-bound
	resA, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "A", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginFeishuAt, Actor: observability.Actor("user:h"),
	})
	cid := setupSecondConvKind(t, h, conversation.ConversationKindIssue)
	// rebind blocked
	if err := bs.BindTo(context.Background(), BindToInput{IssueID: resA.IssueID, ConversationID: cid, Actor: observability.Actor("user:h")}); !errors.Is(err, discussion.ErrIssueInvalidTransition) {
		t.Fatalf("expected invalid transition, got %v", err)
	}
	// bad inputs
	if err := bs.BindTo(context.Background(), BindToInput{IssueID: "X", ConversationID: "C", Actor: "BAD"}); err == nil {
		t.Fatal("bad actor")
	}
	if err := bs.BindTo(context.Background(), BindToInput{IssueID: "", ConversationID: "C", Actor: observability.Actor("user:h")}); err == nil {
		t.Fatal("empty issue")
	}
	if err := bs.BindTo(context.Background(), BindToInput{IssueID: "X", ConversationID: "", Actor: observability.Actor("user:h")}); err == nil {
		t.Fatal("empty conv")
	}
	// non-existent target conv
	resB, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "B", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	if err := bs.BindTo(context.Background(), BindToInput{IssueID: resB.IssueID, ConversationID: "ghost", Actor: observability.Actor("user:h")}); !errors.Is(err, conversation.ErrConversationNotFound) {
		t.Fatalf("expected not_found, got %v", err)
	}
	// terminal issue
	if _, err := lifecycle.Withdraw(context.Background(), WithdrawIssueCommand{
		IssueID: resB.IssueID, Reason: "x", Message: "y", WithdrawnBy: "user:h",
		Actor: observability.Actor("user:h"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := bs.BindTo(context.Background(), BindToInput{IssueID: resB.IssueID, ConversationID: cid, Actor: observability.Actor("user:h")}); !errors.Is(err, discussion.ErrIssueInvalidTransition) {
		t.Fatalf("expected terminal block, got %v", err)
	}
}
