package conversation

import (
	"context"
	"errors"
	"time"
)

// UserConversationReadState tracks how far a given user has read in a
// given conversation. Row absence ("never seen") is the initial state;
// the row is created on the first MarkSeen.
//
// Schema lives in migration 0026 (v2.1-C-1). See repository contract +
// invariants in v2.1-C-2 audit § 1.
type UserConversationReadState struct {
	UserID            IdentityRef
	ConversationID    ConversationID
	LastSeenMessageID MessageID
	UpdatedAt         time.Time
	Version           int
}

// UserConversationReadStateRepository is the port over the
// user_conversation_read_state table.
type UserConversationReadStateRepository interface {
	// FindByUserAndConv returns the row or ErrReadStateNotFound when
	// absent (= "never seen").
	FindByUserAndConv(ctx context.Context, userID IdentityRef,
		convID ConversationID) (*UserConversationReadState, error)

	// FindByUserBatch returns every row for a user (powers the
	// dashboard's "unread per conversation" panel). Caller bounds N by
	// the natural cardinality of conversations the user participates in.
	FindByUserBatch(ctx context.Context, userID IdentityRef) (
		[]*UserConversationReadState, error)

	// Upsert inserts when Version == 0; otherwise CAS on
	// (user_id, conversation_id, version). Bumps Version on success.
	// Returns ErrReadStateVersionConflict when a concurrent writer
	// already moved the row.
	Upsert(ctx context.Context, s *UserConversationReadState) error
}

// Read-state sentinel errors.
var (
	ErrReadStateNotFound        = errors.New("conversation: read state not found")
	ErrReadStateVersionConflict = errors.New("conversation: read state version conflict (optimistic lock)")
	// ErrReadStateMessageNotInConversation is raised by the service
	// layer when MarkSeen's message id refers to a different
	// conversation than the URL — guards against client-side bugs that
	// would otherwise poison a row.
	ErrReadStateMessageNotInConversation = errors.New("conversation: read state message id not in conversation")
)
