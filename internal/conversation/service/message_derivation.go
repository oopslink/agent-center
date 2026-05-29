package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/observability"
)

// IssueOpener is the port the derivation service uses to create Issues.
// The Discussion BC service satisfies this shape.
type IssueOpener interface {
	OpenFromConversation(ctx context.Context, in OpenFromConversationInput) (OpenFromConversationResult, error)
}

// TaskCreator is the port the derivation service uses to create Tasks.
// The TaskRuntime BC service satisfies this shape.
type TaskCreator interface {
	CreateFromConversation(ctx context.Context, in CreateFromConversationInput) (CreateFromConversationResult, error)
}

// OpenFromConversationInput is the IssueOpener port input. (Implemented
// by an adapter shim in main wiring — discussion.IssueLifecycleService
// would need a small wrapper.)
type OpenFromConversationInput struct {
	ProjectID   string
	Title       string
	Description string
	OpenedBy    conversation.IdentityRef
	Actor       observability.Actor
}

// OpenFromConversationResult is the IssueOpener port output.
type OpenFromConversationResult struct {
	IssueID        string
	ConversationID conversation.ConversationID
	EventID        observability.EventID
}

// CreateFromConversationInput is the TaskCreator port input.
type CreateFromConversationInput struct {
	ProjectID       string
	Title           string
	Description     string
	AgentInstanceID string
	CreatedBy       conversation.IdentityRef
	Actor           observability.Actor
}

// CreateFromConversationResult is the TaskCreator port output.
type CreateFromConversationResult struct {
	TaskID         string
	ConversationID conversation.ConversationID
	EventID        observability.EventID
}

// MessageDerivationService implements CV4 派生入口 (ADR-0036) — `issue
// open --from-conversation` / `task new --from-conversation`.
//
// Validation chain (per plan § 3.6):
//  1. source conversation exists + active (not closed/archived)
//  2. caller (CreatedBy) is an active participant of the source conv
//     (channel rule; other kinds permissive)
//  3. all SourceMessageIDs belong to source conv
//  4. delegates Issue/Task creation to the BC's own service (own tx)
//  5. carry-over refs materialised via CarryOverService (own tx)
//
// Sequential-tx note: Issue/Task creation and carry-over write live in
// separate transactions. If the carry-over write fails after the Issue/
// Task is committed, retries can re-materialise the refs (refs are
// append-only with unique (child_conv, source_msg) guard). Plan §
// "同事务" intent is best-effort here; a future refactor could pull the
// inner tx logic out for true atomicity (F1 follow-up).
type MessageDerivationService struct {
	db          *sql.DB
	convRepo    conversation.ConversationRepository
	msgRepo     conversation.MessageRepository
	carryOver   *CarryOverService
	issueOpener IssueOpener
	taskCreator TaskCreator
	sink        *observability.EventSink
	clock       clock.Clock
}

// NewMessageDerivationService constructs the service. issueOpener and
// taskCreator may be nil iff only the other side is exercised.
func NewMessageDerivationService(
	db *sql.DB,
	convRepo conversation.ConversationRepository,
	msgRepo conversation.MessageRepository,
	carryOver *CarryOverService,
	issueOpener IssueOpener,
	taskCreator TaskCreator,
	sink *observability.EventSink,
	clk clock.Clock,
) *MessageDerivationService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &MessageDerivationService{
		db:          db,
		convRepo:    convRepo,
		msgRepo:     msgRepo,
		carryOver:   carryOver,
		issueOpener: issueOpener,
		taskCreator: taskCreator,
		sink:        sink,
		clock:       clk,
	}
}

// Sentinel errors specific to derivation.
var (
	ErrDerivationSourceNotActive      = errors.New("derivation: source conversation is not active")
	ErrDerivationCallerNotParticipant = errors.New("derivation: caller is not an active participant of source conversation")
	ErrDerivationOpenerNotWired       = errors.New("derivation: issue opener not wired")
	ErrDerivationCreatorNotWired      = errors.New("derivation: task creator not wired")
)

// DeriveIssueCommand wraps `issue open --from-conversation`.
type DeriveIssueCommand struct {
	SourceConversationID conversation.ConversationID
	SourceMessageIDs     []conversation.MessageID
	ProjectID            string
	Title                string
	Description          string
	CreatedBy            conversation.IdentityRef
	Actor                observability.Actor
}

// DeriveIssueResult bundles the Issue + carry-over outcomes.
type DeriveIssueResult struct {
	IssueID             string
	ChildConversationID conversation.ConversationID
	IssueEventID        observability.EventID
	CarryOverEventID    observability.EventID
	ReferenceCount      int
}

// DeriveIssue runs the full validation + opens the Issue + materialises
// carry-over refs.
func (s *MessageDerivationService) DeriveIssue(ctx context.Context, cmd DeriveIssueCommand) (DeriveIssueResult, error) {
	if s.issueOpener == nil {
		return DeriveIssueResult{}, ErrDerivationOpenerNotWired
	}
	if err := s.validateCommon(ctx, cmd.SourceConversationID, cmd.SourceMessageIDs, cmd.CreatedBy); err != nil {
		return DeriveIssueResult{}, err
	}
	if strings.TrimSpace(cmd.ProjectID) == "" {
		return DeriveIssueResult{}, errors.New("derivation: project_id required")
	}
	if strings.TrimSpace(cmd.Title) == "" {
		return DeriveIssueResult{}, errors.New("derivation: title required")
	}
	// 1. Create Issue (its own tx).
	openRes, err := s.issueOpener.OpenFromConversation(ctx, OpenFromConversationInput{
		ProjectID: cmd.ProjectID, Title: cmd.Title, Description: cmd.Description,
		OpenedBy: cmd.CreatedBy, Actor: cmd.Actor,
	})
	if err != nil {
		return DeriveIssueResult{}, fmt.Errorf("derivation: open issue: %w", err)
	}
	// 2. Materialise carry-over refs (own tx).
	out := DeriveIssueResult{
		IssueID: openRes.IssueID, ChildConversationID: openRes.ConversationID,
		IssueEventID: openRes.EventID,
	}
	if len(cmd.SourceMessageIDs) > 0 {
		coRes, err := s.carryOver.Materialise(ctx, MaterialiseCommand{
			ChildConversationID:  openRes.ConversationID,
			SourceConversationID: cmd.SourceConversationID,
			SourceMessageIDs:     cmd.SourceMessageIDs,
			CreatedBy:            cmd.CreatedBy,
			Actor:                cmd.Actor,
		})
		if err != nil {
			return out, fmt.Errorf("derivation: carry-over refs: %w", err)
		}
		out.CarryOverEventID = coRes.EventID
		out.ReferenceCount = len(coRes.References)
	}
	return out, nil
}

// DeriveTaskCommand wraps `task new --from-conversation`.
type DeriveTaskCommand struct {
	SourceConversationID conversation.ConversationID
	SourceMessageIDs     []conversation.MessageID
	ProjectID            string
	Title                string
	Description          string
	AgentInstanceID      string
	CreatedBy            conversation.IdentityRef
	Actor                observability.Actor
}

// DeriveTaskResult bundles the Task + carry-over outcomes.
type DeriveTaskResult struct {
	TaskID              string
	ChildConversationID conversation.ConversationID
	TaskEventID         observability.EventID
	CarryOverEventID    observability.EventID
	ReferenceCount      int
}

// DeriveTask runs the full validation + creates the Task + materialises
// carry-over refs.
func (s *MessageDerivationService) DeriveTask(ctx context.Context, cmd DeriveTaskCommand) (DeriveTaskResult, error) {
	if s.taskCreator == nil {
		return DeriveTaskResult{}, ErrDerivationCreatorNotWired
	}
	if err := s.validateCommon(ctx, cmd.SourceConversationID, cmd.SourceMessageIDs, cmd.CreatedBy); err != nil {
		return DeriveTaskResult{}, err
	}
	if strings.TrimSpace(cmd.ProjectID) == "" {
		return DeriveTaskResult{}, errors.New("derivation: project_id required")
	}
	if strings.TrimSpace(cmd.Title) == "" {
		return DeriveTaskResult{}, errors.New("derivation: title required")
	}
	if strings.TrimSpace(cmd.AgentInstanceID) == "" {
		return DeriveTaskResult{}, errors.New("derivation: agent_instance_id required for task derivation")
	}
	createRes, err := s.taskCreator.CreateFromConversation(ctx, CreateFromConversationInput{
		ProjectID: cmd.ProjectID, Title: cmd.Title, Description: cmd.Description,
		AgentInstanceID: cmd.AgentInstanceID,
		CreatedBy:       cmd.CreatedBy, Actor: cmd.Actor,
	})
	if err != nil {
		return DeriveTaskResult{}, fmt.Errorf("derivation: create task: %w", err)
	}
	out := DeriveTaskResult{
		TaskID: createRes.TaskID, ChildConversationID: createRes.ConversationID,
		TaskEventID: createRes.EventID,
	}
	if len(cmd.SourceMessageIDs) > 0 {
		coRes, err := s.carryOver.Materialise(ctx, MaterialiseCommand{
			ChildConversationID:  createRes.ConversationID,
			SourceConversationID: cmd.SourceConversationID,
			SourceMessageIDs:     cmd.SourceMessageIDs,
			CreatedBy:            cmd.CreatedBy,
			Actor:                cmd.Actor,
		})
		if err != nil {
			return out, fmt.Errorf("derivation: carry-over refs: %w", err)
		}
		out.CarryOverEventID = coRes.EventID
		out.ReferenceCount = len(coRes.References)
	}
	return out, nil
}

// validateCommon enforces the shared validation chain (source conv
// active, caller is active participant of channel-kind source, messages
// belong to source).
func (s *MessageDerivationService) validateCommon(ctx context.Context, sourceID conversation.ConversationID, msgIDs []conversation.MessageID, createdBy conversation.IdentityRef) error {
	if err := createdBy.Validate(); err != nil {
		return fmt.Errorf("derivation: created_by: %w", err)
	}
	if strings.TrimSpace(string(sourceID)) == "" {
		return errors.New("derivation: source_conversation_id required")
	}
	source, err := s.convRepo.FindByID(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("derivation: source conversation: %w", err)
	}
	if !source.IsActive() {
		return ErrDerivationSourceNotActive
	}
	// Channel kind: enforce participant membership. Other kinds are
	// permissive (eg. issue/task convs may be derived from without a
	// strict participant check yet — refined per ADR-0036 follow-up).
	if source.Kind() == conversation.ConversationKindProjectChannel && !source.HasActiveParticipant(createdBy) {
		return ErrDerivationCallerNotParticipant
	}
	return validateMessagesInSourceConv(ctx, s.msgRepo, sourceID, msgIDs)
}

// validateMessagesInSourceConv batches the message lookup (1 query for
// all ids instead of N) and asserts each lives in sourceID. Shared with
// CarryOverService.Materialise.
func validateMessagesInSourceConv(ctx context.Context, msgRepo conversation.MessageRepository, sourceID conversation.ConversationID, msgIDs []conversation.MessageID) error {
	if len(msgIDs) == 0 {
		return nil
	}
	msgs, err := msgRepo.FindByIDs(ctx, msgIDs)
	if err != nil {
		return fmt.Errorf("derivation: messages: %w", err)
	}
	found := make(map[conversation.MessageID]*conversation.Message, len(msgs))
	for _, m := range msgs {
		found[m.ID()] = m
	}
	for _, mid := range msgIDs {
		m, ok := found[mid]
		if !ok {
			return fmt.Errorf("derivation: message %s: %w", mid, conversation.ErrMessageNotFound)
		}
		if m.ConversationID() != sourceID {
			return fmt.Errorf("%w: message %s belongs to %s, not %s",
				ErrCarryOverSourceMsgNotInConv, mid, m.ConversationID(), sourceID)
		}
	}
	return nil
}
