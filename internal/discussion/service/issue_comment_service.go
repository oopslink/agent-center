package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/discussion"
	"github.com/oopslink/agent-center/internal/observability"
)

// IssueCommentService is the議事-message facade per ADR-0021 § 3: an
// "Issue comment" is always a Message on issue.conversation_id (no
// IssueComment entity). The facade:
//
//   1. loads the Issue
//   2. rejects if issue.conversation_id is null (caller must
//      bind-conversation first)
//   3. delegates to Conversation BC AddMessage
//   4. if this is the first non-opener Message, fires
//      RecordDiscussionStart (open → under_discussion)
type IssueCommentService struct {
	issueRepo discussion.IssueRepository
	convRepo  conversation.ConversationRepository
	msgRepo   conversation.MessageRepository
	msgAdder  ConversationMessageAdder
	lifecycle *IssueLifecycleService
	clock     clock.Clock
}

// NewIssueCommentService wires the facade.
func NewIssueCommentService(
	issueRepo discussion.IssueRepository,
	convRepo conversation.ConversationRepository,
	msgRepo conversation.MessageRepository,
	msgAdder ConversationMessageAdder,
	lifecycle *IssueLifecycleService,
	clk clock.Clock,
) *IssueCommentService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &IssueCommentService{
		issueRepo: issueRepo,
		convRepo:  convRepo,
		msgRepo:   msgRepo,
		msgAdder:  msgAdder,
		lifecycle: lifecycle,
		clock:     clk,
	}
}

// CommentInput captures the facade arguments.
type CommentInput struct {
	IssueID          discussion.IssueID
	Content          string
	ContentKind      conversation.MessageContentKind
	SenderIdentityID conversation.IdentityRef
	Direction        conversation.MessageDirection
	Actor            observability.Actor
}

// CommentResult wraps the produced Message id.
type CommentResult struct {
	MessageID conversation.MessageID
}

// Comment writes a single Message on issue.conversation_id. Returns
// ErrIssueNoConversationBound if the issue is not yet bound; returns
// ErrIssueWithdrawn if the issue is in a terminal/withdrawn state.
func (s *IssueCommentService) Comment(ctx context.Context, in CommentInput) (*CommentResult, error) {
	if err := in.Actor.Validate(); err != nil {
		return nil, err
	}
	if err := in.SenderIdentityID.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Content) == "" {
		return nil, errors.New("issue comment: content required")
	}
	if !in.ContentKind.IsValid() {
		return nil, fmt.Errorf("issue comment: invalid content kind %q", in.ContentKind)
	}
	if !in.Direction.IsValid() {
		// default to internal when omitted
		in.Direction = conversation.DirectionInternal
	}
	issue, err := s.issueRepo.FindByID(ctx, in.IssueID)
	if err != nil {
		return nil, err
	}
	if issue.Status() == discussion.StatusWithdrawn {
		return nil, discussion.ErrIssueWithdrawn
	}
	if issue.IsTerminal() {
		// closed_with_tasks / closed_no_action also block writes per
		// discussion/00 § 2 invariants (terminal = no-IO).
		return nil, fmt.Errorf("%w: issue is terminal (%s)", discussion.ErrIssueInvalidTransition, issue.Status())
	}
	if !issue.HasConversation() {
		return nil, discussion.ErrIssueNoConversationBound
	}
	// Determine whether this is the first non-opener Message so we can
	// fire RecordDiscussionStart afterwards.
	openerSelfComment := string(in.SenderIdentityID) == issue.OpenedByIdentityID()
	shouldTriggerStart := !openerSelfComment && issue.Status() == discussion.StatusOpen

	// Delegate to Conversation BC (its AddMessage uses its own tx; no
	// cross-BC tx required for the comment path itself per ADR-0021).
	res, err := s.msgAdder.AddMessage(ctx, convservice.AddMessageCommand{
		ConversationID:   issue.ConversationID(),
		SenderIdentityID: in.SenderIdentityID,
		ContentKind:      in.ContentKind,
		Content:          in.Content,
		Direction:        in.Direction,
		Actor:            in.Actor,
	})
	if err != nil {
		return nil, err
	}
	if shouldTriggerStart && s.lifecycle != nil {
		if _, err := s.lifecycle.RecordDiscussionStart(ctx, RecordDiscussionStartCommand{
			IssueID:               issue.ID(),
			FirstMessageID:        res.MessageID,
			FirstSenderIdentityID: in.SenderIdentityID,
			Actor:                 in.Actor,
		}); err != nil {
			// Discussion start failed (CAS conflict on concurrent transition).
			// Surface explicitly per § 17 — do NOT silently swallow.
			return nil, fmt.Errorf("issue comment: record discussion start: %w", err)
		}
	}
	return &CommentResult{MessageID: res.MessageID}, nil
}
