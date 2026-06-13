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
	// OwnerRef, when set, returns only the conversation pinned to that pm
	// owner_ref (pm://tasks|issues/{id}). v2.7 #137: the UI fetches a task/
	// issue conversation by owner_ref. Combined with OrganizationID it is
	// org-scoped by construction (a cross-org owner_ref yields no rows —
	// fail-closed, no leak).
	OwnerRef *OwnerRef
}

// DefaultConversationLimit caps Find when Limit <= 0.
const DefaultConversationLimit = 100

// ConversationRepository per ADR-0032 / 0034 (v2 surface).
type ConversationRepository interface {
	FindByID(ctx context.Context, id ConversationID) (*Conversation, error)
	Find(ctx context.Context, filter ConversationFilter) ([]*Conversation, error)
	FindByName(ctx context.Context, name string) (*Conversation, error)
	// FindByNameInOrg looks up a channel by name WITHIN an organization (v2.7 #195:
	// channel name is org-scoped unique, not global). Returns ErrConversationNotFound
	// if absent in that org. Used by the org-scoped create dedup + the webconsole
	// participant lookups so a name shared across orgs resolves the right channel.
	FindByNameInOrg(ctx context.Context, orgID, name string) (*Conversation, error)
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
	// Delete hard-removes the conversation row (v2.7 #198, DM delete). Channels
	// use UpdateArchive (terminal-but-retained); only DMs are hard-deleted. The
	// caller deletes the conversation's messages + read-state in the same tx (no
	// DB-level cascade). Idempotent: deleting an absent id is a no-op.
	Delete(ctx context.Context, id ConversationID) error
}

// MessageFilter narrows MessageRepository queries.
type MessageFilter struct {
	Since *time.Time
	Tail  int // last N messages when non-zero
	Limit int
	// TopLevelOnly excludes thread replies (parent_message_id IS NOT NULL) — the
	// main conversation flow shows only top-level messages; replies live in the
	// thread side panel (v2.9.1 Thread P1). Opt-in; default false keeps every other
	// caller's behavior unchanged.
	TopLevelOnly bool
}

// ThreadDigest is the per-root thread summary used to badge a top-level message
// (v2.9.1 Thread P1): how many replies and when the latest one landed.
type ThreadDigest struct {
	ReplyCount int
	// LastActivityAt is the latest reply's posted_at as an RFC3339Nano string
	// (same wire format msgPublicMap emits for posted_at). Empty when no replies.
	LastActivityAt string
	// LastReplyID is the latest reply's message id (ULID). v2.9.1 P3 has-activity:
	// a thread has "new activity since last viewed" for a user when
	// LastReplyID > that user's conversation last_seen_message_id (lexicographic
	// ULID compare, the same monotonic ordering unread uses). Empty when no replies.
	LastReplyID string
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
	// RecentByConversations batch-fetches the last-n messages per conversation
	// across the whole input set in a SINGLE window-function query (NO N+1) — the
	// v2.8.1 channels-list enrich uses it to render a recent-messages preview for
	// the whole page in one round-trip. Each returned slice is newest-first; a
	// conversation with no messages simply has no map entry. n <= 0 → empty map.
	RecentByConversations(ctx context.Context, convIDs []ConversationID, n int) (map[ConversationID][]*Message, error)
	// FindThreadReplies returns ONLY the replies of a thread (root_message_id ==
	// rootMessageID) within a conversation, in posted_at order (v2.9.1 Thread P1).
	// The root itself is NOT included — the caller already has it from the main
	// list. Scoped to the conversation, so replies from another conversation never
	// leak in. Empty slice when the root has no replies.
	FindThreadReplies(ctx context.Context, conversationID ConversationID, rootMessageID MessageID) ([]*Message, error)
	// ThreadReplyDigests returns the reply count + last-activity timestamp per
	// thread root for a whole conversation in a SINGLE grouped query (NO N+1) — the
	// foundation for the message-list thread-button badge. Roots with no replies
	// are absent from the map.
	ThreadReplyDigests(ctx context.Context, conversationID ConversationID) (map[MessageID]ThreadDigest, error)
	Append(ctx context.Context, m *Message) error
	// DeleteByConversationID hard-removes all messages of a conversation (v2.7
	// #198, DM delete). Idempotent: no rows = no error.
	DeleteByConversationID(ctx context.Context, conversationID ConversationID) error
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
