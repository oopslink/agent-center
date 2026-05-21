// Package service hosts the Discussion BC application + domain services.
//
// The lifecycle service orchestrates cross-BC tx (Issue ↔ Conversation
// sync-build path, Issue conclude → TaskRuntime spawn). Per ADR-0021 the
// Issue ↔ Conversation linkage is 1:1 strong; per ADR-0014 § 2 the cross-
// BC writes share a single tx.
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/discussion"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

// ConversationOpener is the minimal port the lifecycle service uses to
// build a sibling Conversation in the same tx. In production this is the
// Conversation BC's MessageWriter (Phase 1) but only the cross-BC subset
// is required, so we redeclare the surface here for clean substitution.
//
// The implementation MUST honor ctx.tx (tx-via-ctx) so the Conversation
// write joins the Discussion tx.
type ConversationOpener interface {
	OpenIssueConversation(ctx context.Context, in OpenIssueConversationInput) (conversation.ConversationID, error)
}

// OpenIssueConversationInput drives ConversationOpener.OpenIssueConversation.
type OpenIssueConversationInput struct {
	Title              string
	PrimaryChannelHint string
	IssueID            discussion.IssueID
	Actor              observability.Actor
}

// ConversationMessageAdder is the minimal port the lifecycle service uses
// for adding messages (e.g. conclusion system message). Honors tx-via-ctx.
type ConversationMessageAdder interface {
	AddMessage(ctx context.Context, in convservice.AddMessageCommand) (convservice.AddMessageResult, error)
}

// ProjectExistenceChecker enforces issue.project_id referential integrity
// at the application layer (schema has no FK; conventions § 9.w).
type ProjectExistenceChecker interface {
	ProjectExists(ctx context.Context, projectID string) (bool, error)
}

// ErrProjectNotFound is returned when issue.project_id refers to an
// unknown project (application-layer FK enforcement; § 9.w).
var ErrProjectNotFound = errors.New("discussion service: project not found")

// IssueLifecycleService orchestrates Issue.Open / Withdraw / Conclude /
// RecordDiscussionStart with cross-BC tx where needed.
type IssueLifecycleService struct {
	db           *sql.DB
	issueRepo    discussion.IssueRepository
	convOpener   ConversationOpener
	msgAdder     ConversationMessageAdder
	spawner      IssueConcludeSpawner
	projectCheck ProjectExistenceChecker
	sink         *observability.EventSink
	idgen        idgen.Generator
	clock        clock.Clock
}

// NewIssueLifecycleService wires the dependencies.
//
// convOpener / msgAdder / projectCheck may be nil for narrow unit tests
// that bypass cross-BC wiring; production paths must inject all three.
func NewIssueLifecycleService(
	db *sql.DB,
	repo discussion.IssueRepository,
	convOpener ConversationOpener,
	msgAdder ConversationMessageAdder,
	sink *observability.EventSink,
	gen idgen.Generator,
	clk clock.Clock,
) *IssueLifecycleService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &IssueLifecycleService{
		db:         db,
		issueRepo:  repo,
		convOpener: convOpener,
		msgAdder:   msgAdder,
		sink:       sink,
		idgen:      gen,
		clock:      clk,
	}
}

// WithProjectExistenceChecker wires the app-layer project FK check.
func (s *IssueLifecycleService) WithProjectExistenceChecker(c ProjectExistenceChecker) *IssueLifecycleService {
	s.projectCheck = c
	return s
}

// OpenIssueCommand drives IssueLifecycleService.Open.
type OpenIssueCommand struct {
	ProjectID          string
	Title              string
	Description        string
	DescriptionBlobRef string
	OpenedByIdentityID string
	Origin             discussion.Origin
	PrimaryChannelHint string // only used on sync-build branches (web / feishu / supervisor)
	Actor              observability.Actor
}

// OpenIssueResult wraps the created identifiers.
type OpenIssueResult struct {
	IssueID        discussion.IssueID
	ConversationID conversation.ConversationID // empty on lazy-create path
	EventID        observability.EventID
}

// Open creates an Issue. Sync-build origins (web_console / feishu_at /
// supervisor) build the kind=issue Conversation in the same tx and set
// issue.conversation_id; lazy-create origins (cli / agent_open_issue)
// leave conversation_id null (caller must bind-conversation later).
func (s *IssueLifecycleService) Open(ctx context.Context, cmd OpenIssueCommand) (*OpenIssueResult, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return nil, err
	}
	if !cmd.Origin.IsValid() {
		return nil, fmt.Errorf("%w: %s", discussion.ErrInvalidOrigin, cmd.Origin)
	}
	if strings.TrimSpace(cmd.ProjectID) == "" {
		return nil, errors.New("issue service: project_id required")
	}
	if strings.TrimSpace(cmd.Title) == "" {
		return nil, errors.New("issue service: title required")
	}
	if strings.TrimSpace(cmd.OpenedByIdentityID) == "" {
		return nil, errors.New("issue service: opener required")
	}
	// App-layer project FK (conventions § 9.w).
	if s.projectCheck != nil {
		ok, err := s.projectCheck.ProjectExists(ctx, cmd.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("issue service: project existence check: %w", err)
		}
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrProjectNotFound, cmd.ProjectID)
		}
	}
	now := s.clock.Now()
	res := &OpenIssueResult{
		IssueID: discussion.IssueID(s.idgen.NewULID()),
	}

	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		var convID conversation.ConversationID
		if cmd.Origin.NeedsSyncConversationBuild() {
			if s.convOpener == nil {
				return errors.New("issue service: sync-build origin requires ConversationOpener (nil)")
			}
			id, err := s.convOpener.OpenIssueConversation(txCtx, OpenIssueConversationInput{
				Title:              cmd.Title,
				PrimaryChannelHint: cmd.PrimaryChannelHint,
				IssueID:            res.IssueID,
				Actor:              cmd.Actor,
			})
			if err != nil {
				return fmt.Errorf("issue service: open sibling conversation: %w", err)
			}
			convID = id
			res.ConversationID = convID
		}
		issue, err := discussion.NewIssue(discussion.NewIssueInput{
			ID:                 res.IssueID,
			ProjectID:          cmd.ProjectID,
			Title:              cmd.Title,
			Description:        cmd.Description,
			DescriptionBlobRef: cmd.DescriptionBlobRef,
			OpenedByIdentityID: cmd.OpenedByIdentityID,
			Origin:             cmd.Origin,
			ConversationID:     convID,
			OpenedAt:           now,
		})
		if err != nil {
			return err
		}
		if err := s.issueRepo.Save(txCtx, issue); err != nil {
			return err
		}
		evID, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "issue.opened",
			Refs: observability.EventRefs{
				IssueID:        string(issue.ID()),
				ProjectID:      issue.ProjectID(),
				ConversationID: string(convID),
			},
			Actor: cmd.Actor,
			Payload: map[string]any{
				"issue_id":        string(issue.ID()),
				"project_id":      issue.ProjectID(),
				"title":           issue.Title(),
				"opener":          issue.OpenedByIdentityID(),
				"origin":          string(issue.Origin()),
				"conversation_id": string(convID),
			},
		})
		if err != nil {
			return err
		}
		res.EventID = evID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// WithdrawIssueCommand drives IssueLifecycleService.Withdraw.
type WithdrawIssueCommand struct {
	IssueID     discussion.IssueID
	Reason      string
	Message     string
	WithdrawnBy string
	Actor       observability.Actor
}

// Withdraw transitions an Issue to withdrawn (terminal). reason + message
// are required (conventions § 16).
func (s *IssueLifecycleService) Withdraw(ctx context.Context, cmd WithdrawIssueCommand) (observability.EventID, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return "", err
	}
	if strings.TrimSpace(string(cmd.IssueID)) == "" {
		return "", errors.New("issue service: issue_id required")
	}
	if strings.TrimSpace(cmd.Reason) == "" || strings.TrimSpace(cmd.Message) == "" {
		return "", errors.New("issue service: reason + message required (conventions § 16)")
	}
	if strings.TrimSpace(cmd.WithdrawnBy) == "" {
		return "", errors.New("issue service: withdrawn_by required")
	}
	var evID observability.EventID
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		issue, err := s.issueRepo.FindByID(txCtx, cmd.IssueID)
		if err != nil {
			return err
		}
		now := s.clock.Now()
		// AR Withdraw enforces invariants (terminal / version bump).
		if err := issue.Withdraw(cmd.Reason, cmd.Message, cmd.WithdrawnBy, now); err != nil {
			return err
		}
		// Repository CAS write (using version pre-bump = issue.Version() - 1
		// post-Withdraw bump). UpdateWithdraw writes status + reason+message
		// + concluded_by + concluded_at in one statement.
		if err := s.issueRepo.UpdateWithdraw(txCtx, issue.ID(),
			cmd.Reason, cmd.Message, cmd.WithdrawnBy, now, issue.Version()-1); err != nil {
			return err
		}
		id, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "issue.withdrawn",
			Refs: observability.EventRefs{
				IssueID:        string(issue.ID()),
				ProjectID:      issue.ProjectID(),
				ConversationID: string(issue.ConversationID()),
			},
			Actor: cmd.Actor,
			Payload: map[string]any{
				"issue_id":     string(issue.ID()),
				"reason":       cmd.Reason,
				"message":      cmd.Message,
				"withdrawn_by": cmd.WithdrawnBy,
			},
		})
		if err != nil {
			return err
		}
		evID = id
		return nil
	})
	return evID, err
}

// RecordDiscussionStartCommand drives RecordDiscussionStart.
type RecordDiscussionStartCommand struct {
	IssueID               discussion.IssueID
	FirstMessageID        conversation.MessageID
	FirstSenderIdentityID conversation.IdentityRef
	Actor                 observability.Actor
}

// RecordDiscussionStart transitions an Issue from open to under_discussion
// the first time a non-opener Message lands. Idempotent (no-op if already
// under_discussion or beyond).
//
// IssueCommentService.Comment calls this internally; exposing it lets the
// hook be wired explicitly in tests.
func (s *IssueLifecycleService) RecordDiscussionStart(ctx context.Context, cmd RecordDiscussionStartCommand) (observability.EventID, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return "", err
	}
	if strings.TrimSpace(string(cmd.IssueID)) == "" {
		return "", errors.New("issue service: issue_id required")
	}
	if strings.TrimSpace(string(cmd.FirstMessageID)) == "" {
		return "", errors.New("issue service: first_message_id required")
	}
	if err := cmd.FirstSenderIdentityID.Validate(); err != nil {
		return "", err
	}
	var evID observability.EventID
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		issue, err := s.issueRepo.FindByID(txCtx, cmd.IssueID)
		if err != nil {
			return err
		}
		// Idempotent / no-op past open
		if issue.Status() != discussion.StatusOpen {
			return nil
		}
		now := s.clock.Now()
		prevVersion := issue.Version()
		if err := issue.MarkUnderDiscussion(now); err != nil {
			return err
		}
		if err := s.issueRepo.UpdateStatus(txCtx, issue.ID(),
			discussion.StatusOpen, discussion.StatusUnderDiscussion, prevVersion, now); err != nil {
			return err
		}
		id, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "issue.discussion_started",
			Refs: observability.EventRefs{
				IssueID:        string(issue.ID()),
				ProjectID:      issue.ProjectID(),
				ConversationID: string(issue.ConversationID()),
				MessageID:      string(cmd.FirstMessageID),
			},
			Actor: cmd.Actor,
			Payload: map[string]any{
				"issue_id":         string(issue.ID()),
				"first_message_id": string(cmd.FirstMessageID),
				"first_sender":     string(cmd.FirstSenderIdentityID),
			},
		})
		if err != nil {
			return err
		}
		evID = id
		return nil
	})
	return evID, err
}
