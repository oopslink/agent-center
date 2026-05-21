package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/discussion"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

// IssueBindConversationService handles the lazy-create path
// (`issue bind-conversation`):
//
//   - --auto: build a fresh kind=issue Conversation + write
//     issue.conversation_id atomically (cross-BC tx)
//   - --to=<id>: bind to an existing kind=issue Conversation that is open
//     and not already owned by another Issue
//
// Per ADR-0021 the relationship is 1:1 strong; rebinding is rejected at
// the AR + SQL level (UpdateConversationID WHERE conversation_id IS NULL).
type IssueBindConversationService struct {
	db         *sql.DB
	issueRepo  discussion.IssueRepository
	convRepo   conversation.ConversationRepository
	convOpener ConversationOpener
	sink       *observability.EventSink
	clock      clock.Clock
}

// NewIssueBindConversationService wires the service.
func NewIssueBindConversationService(
	db *sql.DB,
	issueRepo discussion.IssueRepository,
	convRepo conversation.ConversationRepository,
	convOpener ConversationOpener,
	sink *observability.EventSink,
	clk clock.Clock,
) *IssueBindConversationService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &IssueBindConversationService{
		db:         db,
		issueRepo:  issueRepo,
		convRepo:   convRepo,
		convOpener: convOpener,
		sink:       sink,
		clock:      clk,
	}
}

// BindAutoInput drives BindAuto.
type BindAutoInput struct {
	IssueID            discussion.IssueID
	Channel            string
	Actor              observability.Actor
}

// BindAuto creates a fresh kind=issue Conversation + writes
// issue.conversation_id inside one tx. Emits `conversation.opened`
// (refs.issue_id) via the underlying opener.
func (s *IssueBindConversationService) BindAuto(ctx context.Context, in BindAutoInput) (conversation.ConversationID, error) {
	if err := in.Actor.Validate(); err != nil {
		return "", err
	}
	if strings.TrimSpace(string(in.IssueID)) == "" {
		return "", errors.New("issue bind: issue_id required")
	}
	if s.convOpener == nil {
		return "", errors.New("issue bind: ConversationOpener required for --auto")
	}
	var convID conversation.ConversationID
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		issue, err := s.issueRepo.FindByID(txCtx, in.IssueID)
		if err != nil {
			return err
		}
		if issue.IsTerminal() {
			return fmt.Errorf("%w: terminal issue rejects bind", discussion.ErrIssueInvalidTransition)
		}
		if issue.HasConversation() {
			return fmt.Errorf("%w: issue already bound to conversation %s",
				discussion.ErrIssueInvalidTransition, issue.ConversationID())
		}
		id, err := s.convOpener.OpenIssueConversation(txCtx, OpenIssueConversationInput{
			Title:              issue.Title(),
			PrimaryChannelHint: in.Channel,
			IssueID:            issue.ID(),
			Actor:              in.Actor,
		})
		if err != nil {
			return fmt.Errorf("issue bind: open conversation: %w", err)
		}
		convID = id
		now := s.clock.Now()
		if err := s.issueRepo.UpdateConversationID(txCtx, issue.ID(), id, issue.Version(), now); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return convID, nil
}

// BindToInput drives BindTo.
type BindToInput struct {
	IssueID        discussion.IssueID
	ConversationID conversation.ConversationID
	Actor          observability.Actor
}

// BindTo binds an Issue to an existing kind=issue Conversation. The
// target must be:
//
//   - kind=issue
//   - status=open
//   - not yet owned by any other Issue
//
// The Issue must currently have no conversation_id.
func (s *IssueBindConversationService) BindTo(ctx context.Context, in BindToInput) error {
	if err := in.Actor.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(string(in.IssueID)) == "" {
		return errors.New("issue bind: issue_id required")
	}
	if strings.TrimSpace(string(in.ConversationID)) == "" {
		return errors.New("issue bind: target conversation_id required")
	}
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		issue, err := s.issueRepo.FindByID(txCtx, in.IssueID)
		if err != nil {
			return err
		}
		if issue.IsTerminal() {
			return fmt.Errorf("%w: terminal issue rejects bind", discussion.ErrIssueInvalidTransition)
		}
		if issue.HasConversation() {
			return fmt.Errorf("%w: issue already bound to conversation %s",
				discussion.ErrIssueInvalidTransition, issue.ConversationID())
		}
		conv, err := s.convRepo.FindByID(txCtx, in.ConversationID)
		if err != nil {
			return err
		}
		if conv.Kind() != conversation.ConversationKindIssue {
			return fmt.Errorf("%w: target conversation kind=%s (want issue)",
				conversation.ErrConversationInvalidKind, conv.Kind())
		}
		if !conv.IsOpen() {
			return conversation.ErrConversationClosed
		}
		// Check target conversation is not already owned by another Issue.
		// We scan for any issue with conversation_id == target.
		taken, err := s.issueRepo.FindByStatus(txCtx, discussion.StatusOpen, discussion.IssueFilter{Limit: 1000})
		if err != nil {
			return err
		}
		for _, other := range taken {
			if other.ID() != issue.ID() && other.ConversationID() == in.ConversationID {
				return fmt.Errorf("%w: conversation %s already owned by issue %s",
					conversation.ErrConversationAlreadyExists, in.ConversationID, other.ID())
			}
		}
		// Also need to check non-open issues (under_discussion / concluded
		// could still own the conversation in a partially-amended flow).
		others, err := s.issueRepo.FindByStatus(txCtx, discussion.StatusUnderDiscussion, discussion.IssueFilter{Limit: 1000})
		if err != nil {
			return err
		}
		for _, other := range others {
			if other.ID() != issue.ID() && other.ConversationID() == in.ConversationID {
				return fmt.Errorf("%w: conversation %s already owned by issue %s",
					conversation.ErrConversationAlreadyExists, in.ConversationID, other.ID())
			}
		}
		now := s.clock.Now()
		if err := s.issueRepo.UpdateConversationID(txCtx, issue.ID(), in.ConversationID, issue.Version(), now); err != nil {
			return err
		}
		return nil
	})
}
