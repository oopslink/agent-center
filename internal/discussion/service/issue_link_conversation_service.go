package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/discussion"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

// IssueLinkConversationService manages the weak `related_conversation_ids`
// lineage list per ADR-0021 § 9. Unlike BindConversation this is N:N,
// emits no Discussion-side event (lineage-only metadata; query-side
// consumers traverse it).
type IssueLinkConversationService struct {
	db        *sql.DB
	issueRepo discussion.IssueRepository
	convRepo  conversation.ConversationRepository
	clock     clock.Clock
}

// NewIssueLinkConversationService wires the service.
func NewIssueLinkConversationService(
	db *sql.DB,
	issueRepo discussion.IssueRepository,
	convRepo conversation.ConversationRepository,
	clk clock.Clock,
) *IssueLinkConversationService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &IssueLinkConversationService{db: db, issueRepo: issueRepo, convRepo: convRepo, clock: clk}
}

// LinkInput drives Link.
type LinkInput struct {
	IssueID        discussion.IssueID
	ConversationID conversation.ConversationID
	Actor          observability.Actor
}

// Link appends conv to issue.related_conversation_ids (dedupe). Target
// conversation must exist (any kind allowed — the lineage column is
// strictly weak per ADR-0021 § 9).
func (s *IssueLinkConversationService) Link(ctx context.Context, in LinkInput) error {
	if err := in.Actor.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(string(in.IssueID)) == "" {
		return errors.New("issue link: issue_id required")
	}
	if strings.TrimSpace(string(in.ConversationID)) == "" {
		return errors.New("issue link: conversation_id required")
	}
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		issue, err := s.issueRepo.FindByID(txCtx, in.IssueID)
		if err != nil {
			return err
		}
		// Verify target conversation exists (returns ErrConversationNotFound otherwise).
		if _, err := s.convRepo.FindByID(txCtx, in.ConversationID); err != nil {
			return err
		}
		now := s.clock.Now()
		prevVersion := issue.Version()
		if err := issue.AddRelatedConversation(in.ConversationID, now); err != nil {
			return err
		}
		// AR bumps version only when the link is new (dedupe skips bump).
		if issue.Version() == prevVersion {
			return nil
		}
		return s.issueRepo.UpdateRelatedConversationIDs(txCtx, issue.ID(),
			issue.RelatedConversationIDs(), prevVersion, now)
	})
}
