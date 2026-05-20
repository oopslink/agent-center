package conversation

import (
	"errors"
	"strings"
	"time"
)

// Message is the Conversation sub-entity (conversation/01 § 4).
//
// Append-only with one exception: vendor_msg_ref may be backfilled once
// (nil → set). Any second mutation of vendor_msg_ref → ErrMessageImmutable.
type Message struct {
	id                MessageID
	conversationID    ConversationID
	senderIdentityID  IdentityRef
	contentKind       MessageContentKind
	content           string
	direction         MessageDirection
	vendorMsgRef      string
	inputRequestRef   string
	postedAt          time.Time
	createdAt         time.Time
}

// NewMessageInput captures the constructor args.
type NewMessageInput struct {
	ID               MessageID
	ConversationID   ConversationID
	SenderIdentityID IdentityRef
	ContentKind      MessageContentKind
	Content          string
	Direction        MessageDirection
	VendorMsgRef     string
	InputRequestRef  string
	PostedAt         time.Time
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
	at := in.PostedAt.UTC()
	return &Message{
		id:               in.ID,
		conversationID:   in.ConversationID,
		senderIdentityID: in.SenderIdentityID,
		contentKind:      in.ContentKind,
		content:          in.Content,
		direction:        in.Direction,
		vendorMsgRef:     in.VendorMsgRef,
		inputRequestRef:  in.InputRequestRef,
		postedAt:         at,
		createdAt:        at,
	}, nil
}

// RehydrateMessageInput is for repository round-trip.
type RehydrateMessageInput struct {
	ID               MessageID
	ConversationID   ConversationID
	SenderIdentityID IdentityRef
	ContentKind      MessageContentKind
	Content          string
	Direction        MessageDirection
	VendorMsgRef     string
	InputRequestRef  string
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
		vendorMsgRef:     in.VendorMsgRef,
		inputRequestRef:  in.InputRequestRef,
		postedAt:         in.PostedAt.UTC(),
		createdAt:        in.CreatedAt.UTC(),
	}, nil
}

// Getters.

func (m *Message) ID() MessageID                  { return m.id }
func (m *Message) ConversationID() ConversationID { return m.conversationID }
func (m *Message) SenderIdentityID() IdentityRef  { return m.senderIdentityID }
func (m *Message) ContentKind() MessageContentKind { return m.contentKind }
func (m *Message) Content() string                { return m.content }
func (m *Message) Direction() MessageDirection    { return m.direction }
func (m *Message) VendorMsgRef() string           { return m.vendorMsgRef }
func (m *Message) HasVendorMsgRef() bool          { return m.vendorMsgRef != "" }
func (m *Message) InputRequestRef() string        { return m.inputRequestRef }
func (m *Message) PostedAt() time.Time            { return m.postedAt }
func (m *Message) CreatedAt() time.Time           { return m.createdAt }

// SetVendorMsgRef backfills the vendor message id. Once set (non-empty)
// it cannot be changed.
func (m *Message) SetVendorMsgRef(ref string) error {
	if strings.TrimSpace(ref) == "" {
		return errors.New("message: vendor_msg_ref required")
	}
	if m.vendorMsgRef != "" {
		return ErrMessageImmutable
	}
	m.vendorMsgRef = ref
	return nil
}
