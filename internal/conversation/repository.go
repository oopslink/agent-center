package conversation

import (
	"context"
	"time"
)

// ConversationFilter narrows ConversationRepository.Find.
type ConversationFilter struct {
	Kind   *ConversationKind
	Status *ConversationStatus
	Cursor *ConversationID
	Limit  int
}

// DefaultConversationLimit caps Find when Limit <= 0.
const DefaultConversationLimit = 100

// ConversationRepository per conversation/00 § 5.1.
type ConversationRepository interface {
	FindByID(ctx context.Context, id ConversationID) (*Conversation, error)
	Find(ctx context.Context, filter ConversationFilter) ([]*Conversation, error)
	FindByChannelAndThreadKey(ctx context.Context, channel, threadKey string) (*Conversation, error)
	Save(ctx context.Context, c *Conversation) error
	UpdateStatus(ctx context.Context, id ConversationID, from, to ConversationStatus, version int, closedReason, closedMessage string, closedAt time.Time) error
	UpdatePrimaryChannel(ctx context.Context, id ConversationID, channel, threadKey string, version int, at time.Time) error
}

// MessageFilter narrows MessageRepository queries.
type MessageFilter struct {
	Since *time.Time
	Tail  int // last N messages when non-zero
	Limit int
}

// MessageRepository per conversation/00 § 5.2.
type MessageRepository interface {
	FindByID(ctx context.Context, id MessageID) (*Message, error)
	FindByConversationID(ctx context.Context, conversationID ConversationID, filter MessageFilter) ([]*Message, error)
	FindByVendorMsgRef(ctx context.Context, vendorMsgRef string) (*Message, error)
	FindRecent(ctx context.Context, conversationID ConversationID, n int) ([]*Message, error)
	Append(ctx context.Context, m *Message) error
	UpdateVendorMsgRef(ctx context.Context, id MessageID, vendorMsgRef string) error
}
