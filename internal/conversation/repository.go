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
	// OrganizationID scopes results to a specific organization (v2.6).
	// Empty string means "no org filter" — used by legacy callers and
	// tests; production webconsole handlers should always set this.
	OrganizationID string
}

// DefaultConversationLimit caps Find when Limit <= 0.
const DefaultConversationLimit = 100

// ConversationRepository per ADR-0032 / 0034 (v2 surface).
type ConversationRepository interface {
	FindByID(ctx context.Context, id ConversationID) (*Conversation, error)
	Find(ctx context.Context, filter ConversationFilter) ([]*Conversation, error)
	FindByName(ctx context.Context, name string) (*Conversation, error)
	// FindByOwnerRef looks up a task/issue Conversation by its owner_ref URI
	// (pm://tasks|issues/{id}); ErrConversationNotFound if absent. Used by the
	// v2.7 ProjectManager→Conversation participant projector to create/sync the
	// bound Conversation idempotently.
	FindByOwnerRef(ctx context.Context, ownerRef OwnerRef) (*Conversation, error)
	FindByParent(ctx context.Context, parentID ConversationID) ([]*Conversation, error)
	Save(ctx context.Context, c *Conversation) error
	UpdateStatus(ctx context.Context, id ConversationID, from, to ConversationStatus, version int, closedReason, closedMessage string, at time.Time) error
	UpdateArchive(ctx context.Context, id ConversationID, version int, archivedBy IdentityRef, at time.Time) error
	UpdateParticipants(ctx context.Context, id ConversationID, participants []ParticipantElement, version int, at time.Time) error
}

// MessageFilter narrows MessageRepository queries.
type MessageFilter struct {
	Since *time.Time
	Tail  int // last N messages when non-zero
	Limit int
}

// MessageRepository per ADR-0031 (v2 — vendor_msg_ref dropped).
type MessageRepository interface {
	FindByID(ctx context.Context, id MessageID) (*Message, error)
	// FindByIDs batches lookups of multiple message ids in a single
	// query. Returns the messages that exist; missing ids are silently
	// skipped (caller compares len(input) vs len(output) to detect).
	// Order of returned messages is not guaranteed.
	FindByIDs(ctx context.Context, ids []MessageID) ([]*Message, error)
	FindByConversationID(ctx context.Context, conversationID ConversationID, filter MessageFilter) ([]*Message, error)
	FindRecent(ctx context.Context, conversationID ConversationID, n int) ([]*Message, error)
	Append(ctx context.Context, m *Message) error
}

// ConversationMessageReference is the carry-over VO (ADR-0035 /
// migration 0022).
type ConversationMessageReference struct {
	ID                   string
	ChildConversationID  ConversationID
	SourceConversationID ConversationID
	SourceMessageID      MessageID
	CreatedBy            IdentityRef
	CreatedAt            time.Time
}

// ConversationMessageReferenceRepository persists carry-over links.
//
// Both lookup methods cap results at DefaultReferenceLimit to prevent
// unbounded scans when a child conv accumulates many carry-overs or a
// popular source message gets cited across many derivations.
type ConversationMessageReferenceRepository interface {
	Save(ctx context.Context, refs []*ConversationMessageReference) error
	FindByChildConvID(ctx context.Context, childConvID ConversationID) ([]*ConversationMessageReference, error)
	FindBySourceMsgID(ctx context.Context, sourceMsgID MessageID) ([]*ConversationMessageReference, error)
	DeleteByChildConvID(ctx context.Context, childConvID ConversationID) error
}

// DefaultReferenceLimit caps both reference lookups + FindByParent so a
// pathological history (e.g. issue accumulating 10k+ source-message
// citations) doesn't return an unbounded slice. UI shows N most-recent.
const DefaultReferenceLimit = 1000
