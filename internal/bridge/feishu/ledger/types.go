// Package ledger hosts the Bridge BC FeishuDeliveryLedger audit Entity +
// Repository (bridge/00 § 5.1, plan-5 § 3.3).
//
// This is *not* a business aggregate (Bridge BC has none) — it's an ACL
// audit table tracking outbound delivery state per Message. The id is a
// ULID; message_id is a weak app-layer reference to a Conversation Message
// (conventions § 9.w: no FK).
package ledger

import (
	"errors"
	"strings"
	"time"
)

// DeliveryStatus is the ledger row status enum.
type DeliveryStatus string

// DeliveryStatus values (TEXT in DB; app-layer enum).
const (
	StatusPending   DeliveryStatus = "pending"
	StatusDelivered DeliveryStatus = "delivered"
	StatusFailed    DeliveryStatus = "failed"
)

// IsValid reports whether s is a known status.
func (s DeliveryStatus) IsValid() bool {
	switch s {
	case StatusPending, StatusDelivered, StatusFailed:
		return true
	}
	return false
}

// String returns the enum value.
func (s DeliveryStatus) String() string { return string(s) }

// FeishuDeliveryLedger is one audit row per outbound message.
type FeishuDeliveryLedger struct {
	id             string // ULID
	messageID      string // Conversation.Message.id (weak ref)
	conversationID string
	channel        string
	threadKey      string
	vendorMsgRef   string
	cardMessageID  string
	status         DeliveryStatus
	retryCount     int
	lastError      string
	deliveredAt    *time.Time
	updatedAt      time.Time
	createdAt      time.Time
	version        int
}

// NewLedgerInput captures the constructor args.
type NewLedgerInput struct {
	ID             string
	MessageID      string
	ConversationID string
	Channel        string
	ThreadKey      string
	CreatedAt      time.Time
}

// NewLedger constructs a pending ledger row. Caller persists it.
func NewLedger(in NewLedgerInput) (*FeishuDeliveryLedger, error) {
	if strings.TrimSpace(in.ID) == "" {
		return nil, errors.New("ledger: id required")
	}
	if strings.TrimSpace(in.MessageID) == "" {
		return nil, errors.New("ledger: message_id required")
	}
	if strings.TrimSpace(in.ConversationID) == "" {
		return nil, errors.New("ledger: conversation_id required")
	}
	if strings.TrimSpace(in.Channel) == "" {
		return nil, errors.New("ledger: channel required")
	}
	if in.CreatedAt.IsZero() {
		return nil, errors.New("ledger: created_at required")
	}
	at := in.CreatedAt.UTC()
	return &FeishuDeliveryLedger{
		id:             in.ID,
		messageID:      in.MessageID,
		conversationID: in.ConversationID,
		channel:        in.Channel,
		threadKey:      in.ThreadKey,
		status:         StatusPending,
		updatedAt:      at,
		createdAt:      at,
		version:        1,
	}, nil
}

// RehydrateInput is for Repository round-trip.
type RehydrateInput struct {
	ID             string
	MessageID      string
	ConversationID string
	Channel        string
	ThreadKey      string
	VendorMsgRef   string
	CardMessageID  string
	Status         DeliveryStatus
	RetryCount     int
	LastError      string
	DeliveredAt    *time.Time
	UpdatedAt      time.Time
	CreatedAt      time.Time
	Version        int
}

// Rehydrate reconstructs without invariant checks (repo path).
func Rehydrate(in RehydrateInput) *FeishuDeliveryLedger {
	var delivered *time.Time
	if in.DeliveredAt != nil {
		u := in.DeliveredAt.UTC()
		delivered = &u
	}
	return &FeishuDeliveryLedger{
		id:             in.ID,
		messageID:      in.MessageID,
		conversationID: in.ConversationID,
		channel:        in.Channel,
		threadKey:      in.ThreadKey,
		vendorMsgRef:   in.VendorMsgRef,
		cardMessageID:  in.CardMessageID,
		status:         in.Status,
		retryCount:     in.RetryCount,
		lastError:      in.LastError,
		deliveredAt:    delivered,
		updatedAt:      in.UpdatedAt.UTC(),
		createdAt:      in.CreatedAt.UTC(),
		version:        in.Version,
	}
}

// Getters.

// ID returns the ledger row id.
func (l *FeishuDeliveryLedger) ID() string { return l.id }

// MessageID returns the weakly-referenced Conversation Message id.
func (l *FeishuDeliveryLedger) MessageID() string { return l.messageID }

// ConversationID returns the conversation id.
func (l *FeishuDeliveryLedger) ConversationID() string { return l.conversationID }

// Channel returns the channel string.
func (l *FeishuDeliveryLedger) Channel() string { return l.channel }

// ThreadKey returns the vendor thread key (empty until known).
func (l *FeishuDeliveryLedger) ThreadKey() string { return l.threadKey }

// VendorMsgRef returns the feishu message id post-delivery (empty until set).
func (l *FeishuDeliveryLedger) VendorMsgRef() string { return l.vendorMsgRef }

// CardMessageID returns the feishu interactive card msg id (empty when N/A).
func (l *FeishuDeliveryLedger) CardMessageID() string { return l.cardMessageID }

// Status returns the current status enum.
func (l *FeishuDeliveryLedger) Status() DeliveryStatus { return l.status }

// RetryCount returns the number of retries attempted.
func (l *FeishuDeliveryLedger) RetryCount() int { return l.retryCount }

// LastError returns the most-recent error message (empty when none).
func (l *FeishuDeliveryLedger) LastError() string { return l.lastError }

// DeliveredAt returns the delivery success time (nil when not delivered).
func (l *FeishuDeliveryLedger) DeliveredAt() *time.Time {
	if l.deliveredAt == nil {
		return nil
	}
	t := *l.deliveredAt
	return &t
}

// UpdatedAt returns the last-modified time.
func (l *FeishuDeliveryLedger) UpdatedAt() time.Time { return l.updatedAt }

// CreatedAt returns the row creation time.
func (l *FeishuDeliveryLedger) CreatedAt() time.Time { return l.createdAt }

// Version returns the CAS version.
func (l *FeishuDeliveryLedger) Version() int { return l.version }

// Sentinel errors (sentinel pattern; errors.Is to test).
var (
	ErrLedgerNotFound        = errors.New("bridge ledger: not found")
	ErrLedgerDuplicate       = errors.New("bridge ledger: message_id already has a ledger row")
	ErrLedgerInvalidStatus   = errors.New("bridge ledger: invalid status")
	ErrLedgerVersionConflict = errors.New("bridge ledger: version conflict (optimistic lock)")
	ErrLedgerInvalidTransition = errors.New("bridge ledger: invalid status transition")
)
