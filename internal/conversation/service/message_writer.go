// Package service hosts the Conversation BC domain services
// (conversation/00 § 3).
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

// MessageWriter combines Conversation lifecycle (Open / Close / Archive)
// and AddMessage (v2 per ADR-0014 same-tx double-write).
type MessageWriter struct {
	db       *sql.DB
	convRepo conversation.ConversationRepository
	msgRepo  conversation.MessageRepository
	sink     *observability.EventSink
	idgen    idgen.Generator
	clock    clock.Clock
}

// NewMessageWriter constructs the service.
func NewMessageWriter(
	db *sql.DB,
	convRepo conversation.ConversationRepository,
	msgRepo conversation.MessageRepository,
	sink *observability.EventSink,
	gen idgen.Generator,
	clk clock.Clock,
) *MessageWriter {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &MessageWriter{
		db:       db,
		convRepo: convRepo,
		msgRepo:  msgRepo,
		sink:     sink,
		idgen:    gen,
		clock:    clk,
	}
}

// OpenCommand opens a conversation (v2 per ADR-0032).
type OpenCommand struct {
	Kind                 conversation.ConversationKind
	Name                 string
	Description          string
	OrganizationID       string // v2.6: scopes the conversation to an org
	ParentConversationID conversation.ConversationID
	Participants         []conversation.ParticipantElement
	CreatedBy            conversation.IdentityRef
	Actor                observability.Actor
}

// OpenResult tracks the created conversation id + event id.
type OpenResult struct {
	ConversationID conversation.ConversationID
	EventID        observability.EventID
}

// OpenConversation creates a fresh conversation and emits
// `conversation.opened`. task / issue kinds rejected — they go through
// TaskRuntime / Discussion sync-create paths.
func (w *MessageWriter) OpenConversation(ctx context.Context, cmd OpenCommand) (OpenResult, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return OpenResult{}, err
	}
	if !cmd.Kind.IsValid() {
		return OpenResult{}, conversation.ErrConversationInvalidKind
	}
	if !cmd.Kind.IsDirectOpenAllowed() {
		return OpenResult{}, fmt.Errorf("%w: kind=%s requires cross-BC sync-create path",
			conversation.ErrConversationInvalidKind, cmd.Kind)
	}
	conv, err := conversation.NewConversation(conversation.NewConversationInput{
		ID:                   conversation.ConversationID(w.idgen.NewULID()),
		Kind:                 cmd.Kind,
		Name:                 cmd.Name,
		Description:          cmd.Description,
		OrganizationID:       cmd.OrganizationID,
		ParentConversationID: cmd.ParentConversationID,
		Participants:         cmd.Participants,
		CreatedBy:            cmd.CreatedBy,
		OpenedAt:             w.clock.Now(),
	})
	if err != nil {
		return OpenResult{}, err
	}
	var evID observability.EventID
	err = persistence.RunInTx(ctx, w.db, func(txCtx context.Context) error {
		if err := w.convRepo.Save(txCtx, conv); err != nil {
			return err
		}
		id, err := w.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "conversation.opened",
			Refs:      observability.EventRefs{ConversationID: string(conv.ID())},
			Actor:     cmd.Actor,
			Payload: map[string]any{
				"conversation_id":         string(conv.ID()),
				"kind":                    string(conv.Kind()),
				"name":                    conv.Name(),
				"parent_conversation_id":  string(conv.ParentConversationID()),
				"with_carry_over":         false,
			},
		})
		if err != nil {
			return err
		}
		evID = id
		return nil
	})
	if err != nil {
		return OpenResult{}, err
	}
	return OpenResult{ConversationID: conv.ID(), EventID: evID}, nil
}

// AddMessageCommand wraps the add-message CLI input.
type AddMessageCommand struct {
	ConversationID   conversation.ConversationID
	SenderIdentityID conversation.IdentityRef
	ContentKind      conversation.MessageContentKind
	Content          string
	Direction        conversation.MessageDirection
	InputRequestRef  string
	Actor            observability.Actor
}

// AddMessageResult tracks the message id + event id.
type AddMessageResult struct {
	MessageID conversation.MessageID
	EventID   observability.EventID
}

// AddMessage appends a Message inside the same tx as the
// `conversation.message_added` event.
func (w *MessageWriter) AddMessage(ctx context.Context, cmd AddMessageCommand) (AddMessageResult, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return AddMessageResult{}, err
	}
	if err := cmd.SenderIdentityID.Validate(); err != nil {
		return AddMessageResult{}, conversation.ErrMessageInvalidSender
	}
	var res AddMessageResult
	err := persistence.RunInTx(ctx, w.db, func(txCtx context.Context) error {
		conv, err := w.convRepo.FindByID(txCtx, cmd.ConversationID)
		if err != nil {
			return err
		}
		if !conv.IsActive() {
			if conv.IsTerminal() {
				return conversation.ErrConversationArchived
			}
			return conversation.ErrConversationClosed
		}
		m, err := conversation.NewMessage(conversation.NewMessageInput{
			ID:               conversation.MessageID(w.idgen.NewULID()),
			ConversationID:   cmd.ConversationID,
			SenderIdentityID: cmd.SenderIdentityID,
			ContentKind:      cmd.ContentKind,
			Content:          cmd.Content,
			Direction:        cmd.Direction,
			InputRequestRef:  cmd.InputRequestRef,
			PostedAt:         w.clock.Now(),
		})
		if err != nil {
			return err
		}
		if err := w.msgRepo.Append(txCtx, m); err != nil {
			return err
		}
		id, err := w.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "conversation.message_added",
			Refs: observability.EventRefs{
				ConversationID: string(m.ConversationID()),
				MessageID:      string(m.ID()),
			},
			Actor: cmd.Actor,
			Payload: map[string]any{
				"conversation_id": string(m.ConversationID()),
				"message_id":      string(m.ID()),
				"sender":          string(m.SenderIdentityID()),
				"content_kind":    string(m.ContentKind()),
				"direction":       string(m.Direction()),
			},
		})
		if err != nil {
			return err
		}
		res = AddMessageResult{MessageID: m.ID(), EventID: id}
		return nil
	})
	return res, err
}

// CloseCommand wraps the close call.
type CloseCommand struct {
	ConversationID conversation.ConversationID
	Version        int
	Reason         string
	Message        string
	Actor          observability.Actor
}

// Close transitions the conversation to closed.
func (w *MessageWriter) Close(ctx context.Context, cmd CloseCommand) (observability.EventID, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return "", err
	}
	if cmd.Reason == "" || cmd.Message == "" {
		return "", errors.New("close: reason and message both required")
	}
	var evID observability.EventID
	err := persistence.RunInTx(ctx, w.db, func(txCtx context.Context) error {
		now := w.clock.Now()
		if err := w.convRepo.UpdateStatus(txCtx, cmd.ConversationID,
			conversation.ConversationActive, conversation.ConversationClosed,
			cmd.Version, cmd.Reason, cmd.Message, now); err != nil {
			return err
		}
		id, err := w.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "conversation.closed",
			Refs:      observability.EventRefs{ConversationID: string(cmd.ConversationID)},
			Actor:     cmd.Actor,
			Payload: map[string]any{
				"conversation_id": string(cmd.ConversationID),
				"reason":          cmd.Reason,
				"message":         cmd.Message,
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

// ArchiveCommand wraps the archive call.
type ArchiveCommand struct {
	ConversationID conversation.ConversationID
	Version        int
	ArchivedBy     conversation.IdentityRef
	Actor          observability.Actor
}

// Archive transitions the conversation to archived (terminal).
func (w *MessageWriter) Archive(ctx context.Context, cmd ArchiveCommand) (observability.EventID, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return "", err
	}
	if err := cmd.ArchivedBy.Validate(); err != nil {
		return "", fmt.Errorf("archive: archived_by: %w", err)
	}
	var evID observability.EventID
	err := persistence.RunInTx(ctx, w.db, func(txCtx context.Context) error {
		now := w.clock.Now()
		if err := w.convRepo.UpdateArchive(txCtx, cmd.ConversationID, cmd.Version, cmd.ArchivedBy, now); err != nil {
			return err
		}
		id, err := w.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "conversation.archived",
			Refs:      observability.EventRefs{ConversationID: string(cmd.ConversationID)},
			Actor:     cmd.Actor,
			Payload: map[string]any{
				"conversation_id": string(cmd.ConversationID),
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
