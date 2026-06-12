// Package service hosts the Conversation BC domain services
// (conversation/00 § 3).
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
)

// EvtConversationMessageAdded is the cross-BC OUTBOX event type emitted by
// AddMessage when a message is posted into a TASK-owned conversation (owner_ref
// has the `pm://tasks/` prefix). It is the wake trigger consumed by the
// Environment-BC WakeProjector (v2.7 D2-e-i / OQ5). NOTE: this reuses the same
// type string the observability EventSink already emits for the events table —
// the two live in SEPARATE tables (events vs outbox_events) with separate
// consumers, so there is no collision.
const EvtConversationMessageAdded = "conversation.message_added"

// ownerRefTasksPrefix is the task-owned conversation owner_ref scheme. A message
// posted into such a conversation is the OQ5 wake trigger.
const ownerRefTasksPrefix = "pm://tasks/"

// ownerRefIssuesPrefix is the issue-owned conversation owner_ref scheme (v2.7.1
// #227). Issue conversations belong to a project, so an @mention of a project
// member must reach the WakeProjector — but a project member is not (yet) a
// conversation participant, so the conversationHasAgentParticipant emit gate alone
// would never fire (chicken-and-egg: no emit → no projector → no auto-join). Emit
// for issue conversations unconditionally (symmetric to tasks) so the projector
// runs + can auto-join the @mentioned project member.
const ownerRefIssuesPrefix = "pm://issues/"

// ownerRefPlansPrefix is the plan-owned conversation owner_ref scheme (v2.9 #306
// ②). Like issue conversations, a plan conversation belongs to a project, so an
// @mention of a project-member agent must reach the WakeProjector — but that agent
// is not (yet) a conversation participant, so the conversationHasAgentParticipant
// emit gate alone would never fire (chicken-and-egg: no emit → no projector → no
// broaden/auto-join). Emit for plan conversations unconditionally (symmetric to
// tasks/issues) so the projector runs + can wake the @mentioned project member.
const ownerRefPlansPrefix = "pm://plans/"

// MessageWriter combines Conversation lifecycle (Open / Close / Archive)
// and AddMessage (v2 per ADR-0014 same-tx double-write).
type MessageWriter struct {
	db       *sql.DB
	convRepo conversation.ConversationRepository
	msgRepo  conversation.MessageRepository
	sink     *observability.EventSink
	idgen    idgen.Generator
	clock    clock.Clock

	// outbox is the OPTIONAL cross-BC outbox emitter (v2.7 D2-e-i wake trigger).
	// nil → AddMessage emits no outbox event (the legacy double-write to the
	// observability sink is unchanged). Set via WithOutbox at the production
	// wiring site; test fixtures that don't exercise wake leave it nil.
	outbox outbox.Repository
}

// WithOutbox attaches the cross-BC outbox emitter used by AddMessage to emit the
// `conversation.message_added` wake-trigger event (same tx) for task-owned
// conversations. Returns the receiver for chaining. Passing nil disables the
// emission (the default). Mirrors the optional-dep style used by the
// WorkItemProjector's work-delivery deps.
func (w *MessageWriter) WithOutbox(o outbox.Repository) *MessageWriter {
	w.outbox = o
	return w
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

// newConversationID mints a conversation id (v2.7 #187): user-facing DM/channel
// get the "<prefix>-<8hex>" entity id; task/issue (and anything else) keep the
// internal ULID — a task conversation is navigated via its owner_ref (the
// user-facing id is the pm task id), so its conversation id stays internal.
func newConversationID(gen idgen.Generator, kind conversation.ConversationKind) conversation.ConversationID {
	switch kind {
	case conversation.ConversationKindChannel:
		return conversation.ConversationID(gen.NewEntityID("channel"))
	case conversation.ConversationKindDM:
		return conversation.ConversationID(gen.NewEntityID("dm"))
	default:
		return conversation.ConversationID(gen.NewULID())
	}
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
		ID:                   newConversationID(w.idgen, cmd.Kind),
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
				"conversation_id":        string(conv.ID()),
				"kind":                   string(conv.Kind()),
				"name":                   conv.Name(),
				"parent_conversation_id": string(conv.ParentConversationID()),
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
	// Attachments are the message's file attachments (v2.7 #133): unified
	// MessageAttachment {ac://files URI, filename, mime, size}. Optional; nil for
	// a plain message. Stored on the Message (attachments JSON column, A0).
	Attachments []conversation.MessageAttachment
	// ParentMessageID (v2.9.1 Thread P1) is the message being replied to. Empty for
	// a top-level message. The reply's root is DERIVED from the parent (Slack-style
	// depth-1: a reply always hangs off a root; a reply to a reply is merged into
	// the same thread). The parent must live in the SAME conversation.
	ParentMessageID conversation.MessageID
	Actor           observability.Actor
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
		// v2.9.1 Thread P1: when this message is a reply, resolve its parent/root.
		// The parent must exist AND live in the SAME conversation — a parent in
		// another conversation is rejected (conversations are org-scoped, so this
		// blocks cross-org thread stitching; §5.7 non-disclosure at the edge).
		// Depth-1: ResolveReplyPlacement redirects a reply-to-a-reply to the root.
		var parentRef, rootRef conversation.MessageID
		if strings.TrimSpace(string(cmd.ParentMessageID)) != "" {
			parent, err := w.msgRepo.FindByID(txCtx, cmd.ParentMessageID)
			if err != nil {
				return err // ErrMessageNotFound surfaces for an unknown parent
			}
			if parent.ConversationID() != cmd.ConversationID {
				return conversation.ErrMessageParentMismatch
			}
			parentRef, rootRef = conversation.ResolveReplyPlacement(parent)
		}
		m, err := conversation.NewMessage(conversation.NewMessageInput{
			ID:               conversation.MessageID(w.idgen.NewULID()),
			ConversationID:   cmd.ConversationID,
			SenderIdentityID: cmd.SenderIdentityID,
			ContentKind:      cmd.ContentKind,
			Content:          cmd.Content,
			Direction:        cmd.Direction,
			InputRequestRef:  cmd.InputRequestRef,
			Attachments:      cmd.Attachments,
			ParentMessageID:  parentRef,
			RootMessageID:    rootRef,
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
		// v2.7 D2-e-i (OQ5): when the message lands in a TASK-owned conversation,
		// ALSO append a `conversation.message_added` event to the cross-BC outbox
		// IN THIS SAME TX (the outbox repo joins via ExecutorFromCtx). The
		// Environment-BC WakeProjector consumes it to wake any agent whose
		// AgentWorkItem for the task is waiting_input. Only task conversations
		// (owner_ref `pm://tasks/`) carry waiting_input WorkItems, so non-task
		// conversations (channel/dm/issue/project/org) emit NOTHING. A nil outbox
		// dep (test fixtures not exercising wake) also skips silently.
		//
		// This runs INSIDE the same tx as msgRepo.Append + sink.Emit, so the
		// request_input atomic flow (AddMessage + WaitInput in one outer RunInTx)
		// stays atomic: the wake event commits iff the message commits. The
		// agent's own question carries sender=agent:<id>, which the WakeProjector
		// self-excludes — so request_input never wakes the asking agent.
		// Emit conversation.message_added to the cross-BC outbox for the
		// conversations where an agent may need waking:
		//   - TASK conversations (owner_ref pm://tasks/) → WorkItem wake (D2-e-i).
		//   - v2.7 #185: DM/Channel conversations that HAVE an agent participant →
		//     conversational wake (the WakeProjector decides DM-direct vs
		//     channel-@mention; loop-break is the user:-only sender check there).
		// Other conversations — and agent-less DMs/channels — emit nothing: there
		// is no agent to wake, so the event would be a pure no-op. (FINDING-I: the
		// emit was previously task-only, leaving the projector's DM/channel branch
		// dead code → DM/channel→agent never fired.)
		if w.outbox != nil {
			ownerRef := string(conv.OwnerRef())
			if strings.HasPrefix(ownerRef, ownerRefTasksPrefix) ||
				strings.HasPrefix(ownerRef, ownerRefIssuesPrefix) || // v2.7.1 #227: issue @mention → project-member auto-join
				strings.HasPrefix(ownerRef, ownerRefPlansPrefix) || // v2.9 #306 ②: plan @mention → project-member broaden (non-participant)
				conversationHasAgentParticipant(conv) {
				if emitErr := w.emitMessageAddedOutbox(txCtx, conv, m); emitErr != nil {
					return emitErr
				}
			}
		}
		res = AddMessageResult{MessageID: m.ID(), EventID: id}
		return nil
	})
	return res, err
}

// conversationHasAgentParticipant reports whether conv has at least one ACTIVE
// agent participant (IdentityRef "agent:<id>"). v2.7 #185: gates the DM/Channel
// conversation.message_added emit so agent-less human conversations stay
// no-emit (no agent to wake → the outbox event would be a no-op).
func conversationHasAgentParticipant(conv *conversation.Conversation) bool {
	for _, p := range conv.Participants() {
		if p.IsActive() && strings.HasPrefix(string(p.IdentityID), "agent:") {
			return true
		}
	}
	return false
}

// messageAddedOutboxPayload is the JSON payload of the EvtConversationMessageAdded
// outbox event the WakeProjector consumes. text is the message content; sender is
// the IdentityRef string (agent:<id> / user:<id>) used for wake self-exclusion.
type messageAddedOutboxPayload struct {
	ConversationID string `json:"conversation_id"`
	OwnerRef       string `json:"owner_ref"`
	MessageID      string `json:"message_id"`
	Sender         string `json:"sender"`
	Text           string `json:"text"`
}

// emitMessageAddedOutbox appends the wake-trigger event to the outbox inside the
// caller's tx. Called only for task-owned conversations (owner_ref `pm://tasks/`).
func (w *MessageWriter) emitMessageAddedOutbox(ctx context.Context, conv *conversation.Conversation, m *conversation.Message) error {
	pb, err := json.Marshal(messageAddedOutboxPayload{
		ConversationID: string(m.ConversationID()),
		OwnerRef:       string(conv.OwnerRef()),
		MessageID:      string(m.ID()),
		Sender:         string(m.SenderIdentityID()),
		Text:           m.Content(),
	})
	if err != nil {
		return err
	}
	refs, _ := json.Marshal(map[string]string{
		"conversation_id": string(m.ConversationID()),
		"message_id":      string(m.ID()),
		"owner_ref":       string(conv.OwnerRef()),
	})
	return w.outbox.Append(ctx, outbox.Event{
		ID:        w.idgen.NewULID(),
		EventType: EvtConversationMessageAdded,
		Refs:      string(refs),
		Payload:   string(pb),
		CreatedAt: w.clock.Now(),
	})
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
