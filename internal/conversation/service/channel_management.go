package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

// ChannelManagementService implements CV1 — kind=channel CRUD per
// ADR-0032. Channel is the only Conversation kind where `name` is
// REQUIRED + globally unique.
//
// Lifecycle:
//   - Create: writes a new channel Conversation + emits
//     `conversation.opened`. creator is auto-added as the first
//     participant (role=owner).
//   - Archive: state machine transitions to archived (terminal,
//     read-only per ADR-0032 § 5).
type ChannelManagementService struct {
	db       *sql.DB
	convRepo conversation.ConversationRepository
	sink     *observability.EventSink
	idgen    idgen.Generator
	clock    clock.Clock
}

// NewChannelManagementService constructs the service. clk defaults to
// system clock when nil.
func NewChannelManagementService(
	db *sql.DB,
	convRepo conversation.ConversationRepository,
	sink *observability.EventSink,
	gen idgen.Generator,
	clk clock.Clock,
) *ChannelManagementService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &ChannelManagementService{
		db:       db,
		convRepo: convRepo,
		sink:     sink,
		idgen:    gen,
		clock:    clk,
	}
}

// CreateChannelCommand wraps `agent-center channel create`.
type CreateChannelCommand struct {
	Name           string
	Description    string
	OrganizationID string // v2.6: scopes the channel to an org (multi-tenant isolation)
	CreatedBy      conversation.IdentityRef
	Actor          observability.Actor
}

// CreateChannelResult is what callers get.
type CreateChannelResult struct {
	ConversationID conversation.ConversationID
	EventID        observability.EventID
}

// CreateChannel inserts a kind=channel Conversation + emits
// `conversation.opened` in the same tx. Name is validated for global
// uniqueness (the partial unique index also catches the race).
// Returns ErrConversationAlreadyExists when the name is taken.
func (s *ChannelManagementService) CreateChannel(ctx context.Context, cmd CreateChannelCommand) (CreateChannelResult, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return CreateChannelResult{}, err
	}
	if err := cmd.CreatedBy.Validate(); err != nil {
		return CreateChannelResult{}, fmt.Errorf("channel: created_by: %w", err)
	}
	name := strings.TrimSpace(cmd.Name)
	if name == "" {
		return CreateChannelResult{}, errors.New("channel: name required")
	}
	now := s.clock.Now()
	conv, err := conversation.NewConversation(conversation.NewConversationInput{
		ID:             conversation.ConversationID(s.idgen.NewULID()),
		Kind:           conversation.ConversationKindChannel,
		Name:           name,
		Description:    cmd.Description,
		OrganizationID: cmd.OrganizationID,
		CreatedBy:      cmd.CreatedBy,
		OpenedAt:       now,
		Participants: []conversation.ParticipantElement{
			{
				IdentityID: cmd.CreatedBy,
				Role:       "owner",
				JoinedAt:   now.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
				JoinedBy:   cmd.CreatedBy,
			},
		},
	})
	if err != nil {
		return CreateChannelResult{}, err
	}
	var evID observability.EventID
	err = persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		// App-layer pre-check (the DB partial unique index is the
		// ultimate authority but a friendlier ErrAlreadyExists from a
		// pre-check is good UX).
		if existing, lookErr := s.convRepo.FindByName(txCtx, name); lookErr == nil && existing != nil {
			return conversation.ErrConversationAlreadyExists
		} else if lookErr != nil && !errors.Is(lookErr, conversation.ErrConversationNotFound) {
			return lookErr
		}
		if err := s.convRepo.Save(txCtx, conv); err != nil {
			return err
		}
		id, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "conversation.opened",
			Refs:      observability.EventRefs{ConversationID: string(conv.ID())},
			Actor:     cmd.Actor,
			Payload: map[string]any{
				"conversation_id":        string(conv.ID()),
				"kind":                   string(conv.Kind()),
				"name":                   conv.Name(),
				"created_by":             string(conv.CreatedBy()),
				"parent_conversation_id": "",
				"with_carry_over":        false,
			},
		})
		if err != nil {
			return err
		}
		evID = id
		return nil
	})
	if err != nil {
		return CreateChannelResult{}, err
	}
	return CreateChannelResult{ConversationID: conv.ID(), EventID: evID}, nil
}

// ArchiveChannelCommand wraps `agent-center channel archive`.
type ArchiveChannelCommand struct {
	Name       string
	ArchivedBy conversation.IdentityRef
	Actor      observability.Actor
}

// ArchiveChannel transitions a channel to archived (terminal). Lookup is
// by name (the user-facing identifier for channels).
func (s *ChannelManagementService) ArchiveChannel(ctx context.Context, cmd ArchiveChannelCommand) (observability.EventID, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return "", err
	}
	if err := cmd.ArchivedBy.Validate(); err != nil {
		return "", fmt.Errorf("channel: archived_by: %w", err)
	}
	if strings.TrimSpace(cmd.Name) == "" {
		return "", errors.New("channel: name required")
	}
	var evID observability.EventID
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		conv, err := s.convRepo.FindByName(txCtx, cmd.Name)
		if err != nil {
			return err
		}
		if conv.Kind() != conversation.ConversationKindChannel {
			return fmt.Errorf("%w: target conversation kind=%s (want channel)",
				conversation.ErrConversationInvalidKind, conv.Kind())
		}
		now := s.clock.Now()
		if err := s.convRepo.UpdateArchive(txCtx, conv.ID(), conv.Version(), cmd.ArchivedBy, now); err != nil {
			return err
		}
		id, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "conversation.archived",
			Refs:      observability.EventRefs{ConversationID: string(conv.ID())},
			Actor:     cmd.Actor,
			Payload: map[string]any{
				"conversation_id": string(conv.ID()),
				"name":            conv.Name(),
				"archived_by":     string(cmd.ArchivedBy),
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
