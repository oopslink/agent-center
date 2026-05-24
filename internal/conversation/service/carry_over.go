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

// CarryOverService implements CV3 cross-conversation message reference
// material­isation (ADR-0035). Each call selects messages from a source
// conversation and materialises one row per (child_conversation,
// source_message) link in the same tx as
// `conversation.message_references_added`.
type CarryOverService struct {
	db       *sql.DB
	convRepo conversation.ConversationRepository
	msgRepo  conversation.MessageRepository
	refRepo  conversation.ConversationMessageReferenceRepository
	sink     *observability.EventSink
	idgen    idgen.Generator
	clock    clock.Clock
}

// NewCarryOverService constructs the service.
func NewCarryOverService(
	db *sql.DB,
	convRepo conversation.ConversationRepository,
	msgRepo conversation.MessageRepository,
	refRepo conversation.ConversationMessageReferenceRepository,
	sink *observability.EventSink,
	gen idgen.Generator,
	clk clock.Clock,
) *CarryOverService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &CarryOverService{
		db:       db,
		convRepo: convRepo,
		msgRepo:  msgRepo,
		refRepo:  refRepo,
		sink:     sink,
		idgen:    gen,
		clock:    clk,
	}
}

// MaterialiseCommand is the input.
type MaterialiseCommand struct {
	ChildConversationID  conversation.ConversationID
	SourceConversationID conversation.ConversationID
	SourceMessageIDs     []conversation.MessageID
	CreatedBy            conversation.IdentityRef
	Actor                observability.Actor
}

// MaterialiseResult tracks the saved refs + emitted event id.
type MaterialiseResult struct {
	References []*conversation.ConversationMessageReference
	EventID    observability.EventID
}

// Sentinel errors.
var (
	ErrCarryOverSourceMsgNotInConv = errors.New("carry_over: source message does not belong to source conversation")
)

// Materialise inserts the references + emits the event in one tx. Validations:
//   - child and source conversations must exist
//   - every source message must belong to the source conversation
//   - empty SourceMessageIDs returns an empty result without DB writes
func (s *CarryOverService) Materialise(ctx context.Context, cmd MaterialiseCommand) (MaterialiseResult, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return MaterialiseResult{}, err
	}
	if err := cmd.CreatedBy.Validate(); err != nil {
		return MaterialiseResult{}, fmt.Errorf("carry_over: created_by: %w", err)
	}
	if strings.TrimSpace(string(cmd.ChildConversationID)) == "" {
		return MaterialiseResult{}, errors.New("carry_over: child_conversation_id required")
	}
	if strings.TrimSpace(string(cmd.SourceConversationID)) == "" {
		return MaterialiseResult{}, errors.New("carry_over: source_conversation_id required")
	}
	if len(cmd.SourceMessageIDs) == 0 {
		return MaterialiseResult{}, nil
	}
	var res MaterialiseResult
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if _, err := s.convRepo.FindByID(txCtx, cmd.ChildConversationID); err != nil {
			return fmt.Errorf("carry_over: child conversation: %w", err)
		}
		if _, err := s.convRepo.FindByID(txCtx, cmd.SourceConversationID); err != nil {
			return fmt.Errorf("carry_over: source conversation: %w", err)
		}
		if err := validateMessagesInSourceConv(txCtx, s.msgRepo, cmd.SourceConversationID, cmd.SourceMessageIDs); err != nil {
			return fmt.Errorf("carry_over: %w", err)
		}
		now := s.clock.Now()
		refs := make([]*conversation.ConversationMessageReference, 0, len(cmd.SourceMessageIDs))
		for _, msgID := range cmd.SourceMessageIDs {
			refs = append(refs, &conversation.ConversationMessageReference{
				ID:                   s.idgen.NewULID(),
				ChildConversationID:  cmd.ChildConversationID,
				SourceConversationID: cmd.SourceConversationID,
				SourceMessageID:      msgID,
				CreatedBy:            cmd.CreatedBy,
				CreatedAt:            now,
			})
		}
		if err := s.refRepo.Save(txCtx, refs); err != nil {
			return err
		}
		evID, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "conversation.message_references_added",
			Refs:      observability.EventRefs{ConversationID: string(cmd.ChildConversationID)},
			Actor:     cmd.Actor,
			Payload: map[string]any{
				"child_conversation_id":  string(cmd.ChildConversationID),
				"source_conversation_id": string(cmd.SourceConversationID),
				"reference_count":        len(refs),
				"created_by":             string(cmd.CreatedBy),
			},
		})
		if err != nil {
			return err
		}
		res = MaterialiseResult{References: refs, EventID: evID}
		return nil
	})
	return res, err
}

// FindByChildConv proxies to the repo (read-side convenience).
func (s *CarryOverService) FindByChildConv(ctx context.Context, childConvID conversation.ConversationID) ([]*conversation.ConversationMessageReference, error) {
	return s.refRepo.FindByChildConvID(ctx, childConvID)
}

// FindBySourceMsg proxies to the repo (reverse lookup).
func (s *CarryOverService) FindBySourceMsg(ctx context.Context, sourceMsgID conversation.MessageID) ([]*conversation.ConversationMessageReference, error) {
	return s.refRepo.FindBySourceMsgID(ctx, sourceMsgID)
}
