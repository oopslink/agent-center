package service

import (
	"context"
	"errors"
	"strings"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
)

// IssueConversationOpener creates `kind=issue` Conversation rows in the
// caller's tx (via ctx). It is intentionally lightweight — Phase 1's
// MessageWriter rejects `kind=issue` direct opens (per
// conversation/01 § 6.5: those must come via cross-BC factories), and
// this is that cross-BC factory.
//
// Per ADR-0021 the resulting Conversation is 1:1 with the Issue and is
// always opened on Issue.open (sync-build path) or Issue.bind-conversation
// --auto (lazy path).
type IssueConversationOpener struct {
	convRepo conversation.ConversationRepository
	sink     *observability.EventSink
	idgen    idgen.Generator
	clock    clock.Clock
}

// NewIssueConversationOpener constructs the opener.
func NewIssueConversationOpener(repo conversation.ConversationRepository, sink *observability.EventSink, gen idgen.Generator, clk clock.Clock) *IssueConversationOpener {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &IssueConversationOpener{convRepo: repo, sink: sink, idgen: gen, clock: clk}
}

// OpenIssueConversation creates the Conversation row + emits
// `conversation.opened`. Caller MUST pass a tx-bearing ctx so the write
// joins the Discussion BC tx (sync-build path).
func (o *IssueConversationOpener) OpenIssueConversation(ctx context.Context, in OpenIssueConversationInput) (conversation.ConversationID, error) {
	if o.convRepo == nil || o.sink == nil || o.idgen == nil {
		return "", errors.New("issue conversation opener: dependency missing")
	}
	if err := in.Actor.Validate(); err != nil {
		return "", err
	}
	if strings.TrimSpace(string(in.IssueID)) == "" {
		return "", errors.New("issue conversation opener: issue_id required")
	}
	conv, err := conversation.NewConversation(conversation.NewConversationInput{
		ID:                 conversation.ConversationID(o.idgen.NewULID()),
		Kind:               conversation.ConversationKindIssue,
		Title:              in.Title,
		PrimaryChannelHint: in.PrimaryChannelHint,
		OpenedAt:           o.clock.Now(),
	})
	if err != nil {
		return "", err
	}
	if err := o.convRepo.Save(ctx, conv); err != nil {
		return "", err
	}
	if _, err := o.sink.Emit(ctx, observability.EmitCommand{
		EventType: "conversation.opened",
		Refs: observability.EventRefs{
			ConversationID: string(conv.ID()),
			IssueID:        string(in.IssueID),
		},
		Actor: in.Actor,
		Payload: map[string]any{
			"conversation_id": string(conv.ID()),
			"kind":            string(conv.Kind()),
			"issue_id":        string(in.IssueID),
		},
	}); err != nil {
		return "", err
	}
	return conv.ID(), nil
}
