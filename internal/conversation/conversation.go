package conversation

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Conversation is the Conversation BC AR (conversation/01).
//
// Invariants per § 6:
//  1. id / kind immutable
//  2. closed is terminal
//  3. closed conversations refuse add-message
//  4. task / issue kind must come from cross-BC sync-create path
//     (this BC only allows phase-1 direct open for dm/group_thread/adhoc/
//     notification)
type Conversation struct {
	id                       ConversationID
	kind                     ConversationKind
	title                    string
	primaryChannelHint       string
	primaryChannelThreadKey  string
	status                   ConversationStatus
	openedAt                 time.Time
	closedAt                 *time.Time
	closedReason             string
	closedMessage            string
	createdAt                time.Time
	updatedAt                time.Time
	version                  int
}

// NewConversationInput captures the constructor args.
type NewConversationInput struct {
	ID                      ConversationID
	Kind                    ConversationKind
	Title                   string
	PrimaryChannelHint      string
	PrimaryChannelThreadKey string
	OpenedAt                time.Time
}

// NewConversation constructs a fresh open Conversation.
//
// Per conversation/01 § 6.5: task / issue kind conversations are NOT
// created via the direct path — caller must use cross-BC factories. We
// accept them here at the AR level (for repository round-tripping); the
// **MessageWriter** service rejects open-with-task/issue.
func NewConversation(in NewConversationInput) (*Conversation, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("conversation: id required")
	}
	if !in.Kind.IsValid() {
		return nil, ErrConversationInvalidKind
	}
	if in.OpenedAt.IsZero() {
		return nil, errors.New("conversation: opened_at required")
	}
	at := in.OpenedAt.UTC()
	return &Conversation{
		id:                      in.ID,
		kind:                    in.Kind,
		title:                   in.Title,
		primaryChannelHint:      in.PrimaryChannelHint,
		primaryChannelThreadKey: in.PrimaryChannelThreadKey,
		status:                  ConversationOpen,
		openedAt:                at,
		createdAt:               at,
		updatedAt:               at,
		version:                 1,
	}, nil
}

// RehydrateConversationInput is for repository round-trip.
type RehydrateConversationInput struct {
	ID                      ConversationID
	Kind                    ConversationKind
	Title                   string
	PrimaryChannelHint      string
	PrimaryChannelThreadKey string
	Status                  ConversationStatus
	OpenedAt                time.Time
	ClosedAt                *time.Time
	ClosedReason            string
	ClosedMessage           string
	CreatedAt               time.Time
	UpdatedAt               time.Time
	Version                 int
}

// RehydrateConversation reconstructs without invariant checks.
func RehydrateConversation(in RehydrateConversationInput) (*Conversation, error) {
	if !in.Kind.IsValid() {
		return nil, ErrConversationInvalidKind
	}
	if !in.Status.IsValid() {
		return nil, ErrConversationInvalidStatus
	}
	if in.Version < 1 {
		return nil, errors.New("conversation: version must be >= 1")
	}
	return &Conversation{
		id:                      in.ID,
		kind:                    in.Kind,
		title:                   in.Title,
		primaryChannelHint:      in.PrimaryChannelHint,
		primaryChannelThreadKey: in.PrimaryChannelThreadKey,
		status:                  in.Status,
		openedAt:                in.OpenedAt.UTC(),
		closedAt:                copyTimePtr(in.ClosedAt),
		closedReason:            in.ClosedReason,
		closedMessage:           in.ClosedMessage,
		createdAt:               in.CreatedAt.UTC(),
		updatedAt:               in.UpdatedAt.UTC(),
		version:                 in.Version,
	}, nil
}

// Getters.

func (c *Conversation) ID() ConversationID                   { return c.id }
func (c *Conversation) Kind() ConversationKind               { return c.kind }
func (c *Conversation) Title() string                        { return c.title }
func (c *Conversation) PrimaryChannelHint() string           { return c.primaryChannelHint }
func (c *Conversation) PrimaryChannelThreadKey() string      { return c.primaryChannelThreadKey }
func (c *Conversation) Status() ConversationStatus           { return c.status }
func (c *Conversation) OpenedAt() time.Time                  { return c.openedAt }
func (c *Conversation) ClosedAt() *time.Time                 { return copyTimePtr(c.closedAt) }
func (c *Conversation) ClosedReason() string                 { return c.closedReason }
func (c *Conversation) ClosedMessage() string                { return c.closedMessage }
func (c *Conversation) CreatedAt() time.Time                 { return c.createdAt }
func (c *Conversation) UpdatedAt() time.Time                 { return c.updatedAt }
func (c *Conversation) Version() int                         { return c.version }

// IsOpen reports whether the conversation accepts new messages.
func (c *Conversation) IsOpen() bool { return c.status == ConversationOpen }

// Close transitions open→closed with reason+message (§ 16).
func (c *Conversation) Close(at time.Time, reason, message string) error {
	if c.status == ConversationClosed {
		return ErrConversationClosed
	}
	if strings.TrimSpace(reason) == "" {
		return errors.New("conversation: close reason required (conventions § 16)")
	}
	if strings.TrimSpace(message) == "" {
		return errors.New("conversation: close message required (conventions § 16)")
	}
	at = at.UTC()
	c.status = ConversationClosed
	c.closedAt = &at
	c.closedReason = reason
	c.closedMessage = message
	c.updatedAt = at
	c.version++
	return nil
}

// SetPrimaryChannel updates the channel route hint; allowed in either
// status because Bridge writes it asynchronously (conversation/01 § 6.6).
func (c *Conversation) SetPrimaryChannel(hint, threadKey string, at time.Time) error {
	if strings.TrimSpace(hint) == "" {
		return errors.New("conversation: channel hint required")
	}
	if strings.TrimSpace(threadKey) == "" {
		return errors.New("conversation: thread key required")
	}
	c.primaryChannelHint = hint
	c.primaryChannelThreadKey = threadKey
	c.updatedAt = at.UTC()
	c.version++
	return nil
}

func copyTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// _ keeps fmt referenced (used by some debug helpers).
var _ = fmt.Sprintf
