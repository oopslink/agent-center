package conversation

import (
	"errors"
	"strings"
	"time"
)

// Message is the Conversation sub-entity (v2 per ADR-0031: vendor_msg_ref
// dropped — Bridge BC撤回).
//
// Append-only: once inserted, immutable.
type Message struct {
	id               MessageID
	conversationID   ConversationID
	senderIdentityID IdentityRef
	contentKind      MessageContentKind
	content          string
	direction        MessageDirection
	inputRequestRef  string
	contextRefs      ContextRefs
	attachments      []MessageAttachment
	// Thread refs (v2.9.1 P1). A ROOT message (top-level) leaves both empty. A
	// REPLY sets both to the SAME root id (Slack-style depth-1: a reply always
	// hangs off a root, never off another reply). parentMessageID is kept distinct
	// from rootMessageID for forward-compat and read clarity, but under depth-1 the
	// two are always equal for a reply.
	parentMessageID MessageID
	rootMessageID   MessageID
	postedAt        time.Time
	createdAt       time.Time
}

// NewMessageInput captures the constructor args.
type NewMessageInput struct {
	ID               MessageID
	ConversationID   ConversationID
	SenderIdentityID IdentityRef
	ContentKind      MessageContentKind
	Content          string
	Direction        MessageDirection
	InputRequestRef  string
	ContextRefs      ContextRefs
	Attachments      []MessageAttachment
	// ParentMessageID / RootMessageID are the resolved thread refs (empty for a
	// top-level message). The caller (MessageWriter.AddMessage) resolves them from
	// the reply target via ResolveReplyPlacement before constructing the Message.
	ParentMessageID MessageID
	RootMessageID   MessageID
	PostedAt        time.Time
}

// NewMessage constructs a Message after validating invariants.
func NewMessage(in NewMessageInput) (*Message, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("message: id required")
	}
	if strings.TrimSpace(string(in.ConversationID)) == "" {
		return nil, errors.New("message: conversation_id required")
	}
	if err := in.SenderIdentityID.Validate(); err != nil {
		return nil, ErrMessageInvalidSender
	}
	if !in.ContentKind.IsValid() {
		return nil, errors.New("message: invalid content_kind")
	}
	if !in.Direction.IsValid() {
		return nil, errors.New("message: invalid direction")
	}
	if in.PostedAt.IsZero() {
		return nil, errors.New("message: posted_at required")
	}
	if err := validateThreadRefs(in.ID, in.ParentMessageID, in.RootMessageID); err != nil {
		return nil, err
	}
	at := in.PostedAt.UTC()
	return &Message{
		id:               in.ID,
		conversationID:   in.ConversationID,
		senderIdentityID: in.SenderIdentityID,
		contentKind:      in.ContentKind,
		content:          in.Content,
		direction:        in.Direction,
		inputRequestRef:  in.InputRequestRef,
		contextRefs:      in.ContextRefs,
		attachments:      append([]MessageAttachment(nil), in.Attachments...),
		parentMessageID:  in.ParentMessageID,
		rootMessageID:    in.RootMessageID,
		postedAt:         at,
		createdAt:        at,
	}, nil
}

// validateThreadRefs enforces the depth-1 thread invariant on a message's
// parent/root refs: either both empty (a root/top-level message) or both set and
// EQUAL (a reply hanging off that root). A message may not be its own parent/root.
func validateThreadRefs(id, parent, root MessageID) error {
	if parent == "" && root == "" {
		return nil // root message
	}
	if parent == "" || root == "" {
		return ErrMessageInvalidThread // exactly one set → inconsistent
	}
	if parent != root {
		return ErrMessageInvalidThread // depth-1: a reply hangs off the root only
	}
	if parent == id {
		return ErrMessageSelfReply
	}
	return nil
}

// RehydrateMessageInput is for repository round-trip.
type RehydrateMessageInput struct {
	ID               MessageID
	ConversationID   ConversationID
	SenderIdentityID IdentityRef
	ContentKind      MessageContentKind
	Content          string
	Direction        MessageDirection
	InputRequestRef  string
	ContextRefs      ContextRefs
	Attachments      []MessageAttachment
	ParentMessageID  MessageID
	RootMessageID    MessageID
	PostedAt         time.Time
	CreatedAt        time.Time
}

// RehydrateMessage reconstructs without invariant checks.
func RehydrateMessage(in RehydrateMessageInput) (*Message, error) {
	if !in.ContentKind.IsValid() {
		return nil, errors.New("message: invalid content_kind")
	}
	if !in.Direction.IsValid() {
		return nil, errors.New("message: invalid direction")
	}
	return &Message{
		id:               in.ID,
		conversationID:   in.ConversationID,
		senderIdentityID: in.SenderIdentityID,
		contentKind:      in.ContentKind,
		content:          in.Content,
		direction:        in.Direction,
		inputRequestRef:  in.InputRequestRef,
		contextRefs:      in.ContextRefs,
		attachments:      append([]MessageAttachment(nil), in.Attachments...),
		parentMessageID:  in.ParentMessageID,
		rootMessageID:    in.RootMessageID,
		postedAt:         in.PostedAt.UTC(),
		createdAt:        in.CreatedAt.UTC(),
	}, nil
}

// Getters.

func (m *Message) ID() MessageID                   { return m.id }
func (m *Message) ConversationID() ConversationID  { return m.conversationID }
func (m *Message) SenderIdentityID() IdentityRef   { return m.senderIdentityID }
func (m *Message) ContentKind() MessageContentKind { return m.contentKind }
func (m *Message) Content() string                 { return m.content }
func (m *Message) Direction() MessageDirection     { return m.direction }
func (m *Message) InputRequestRef() string         { return m.inputRequestRef }
func (m *Message) ContextRefs() ContextRefs        { return m.contextRefs }
func (m *Message) ParentMessageID() MessageID      { return m.parentMessageID }
func (m *Message) RootMessageID() MessageID        { return m.rootMessageID }
func (m *Message) PostedAt() time.Time             { return m.postedAt }
func (m *Message) CreatedAt() time.Time            { return m.createdAt }

// IsThreadRoot reports whether this message is a top-level (root) message — i.e.
// it has no parent. Replies return false.
func (m *Message) IsThreadRoot() bool { return m.parentMessageID == "" }

// ThreadID returns the id of the thread this message belongs to: the root
// message's id. For a root message that is its own id; for a reply it is the
// stored rootMessageID. This is the stable key the read side groups a thread by.
func (m *Message) ThreadID() MessageID {
	if m.rootMessageID != "" {
		return m.rootMessageID
	}
	return m.id
}

// ResolveReplyPlacement computes the (parent, root) refs for a NEW reply that
// targets `target`. Depth-1 (Slack-style): a reply always attaches to a ROOT, so
// both returned ids equal target's thread root — if `target` is itself a reply,
// the new reply is merged into the same thread (redirected to target's root)
// rather than creating a second level. Returns (parentID, rootID), always equal.
func ResolveReplyPlacement(target *Message) (MessageID, MessageID) {
	root := target.ThreadID()
	return root, root
}

// Attachments returns a defensive copy of the message attachments.
func (m *Message) Attachments() []MessageAttachment {
	if len(m.attachments) == 0 {
		return nil
	}
	out := make([]MessageAttachment, len(m.attachments))
	copy(out, m.attachments)
	return out
}
