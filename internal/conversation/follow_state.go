package conversation

import (
	"context"
	"errors"
	"time"
)

// UserConversationFollowState records an EXPLICIT override of a
// conversation's default follow state for a given user. Row absence
// ("no override") means the service falls back to the kind default:
// top-level conversations (channel / DM) are followed by default, while
// threads (parent_conversation_id non-null) are NOT followed until the
// user participates or is @mentioned (auto-follow). See migration 0050
// and FollowStateService for the default + auto-follow rules.
//
// HUMAN-ONLY (Q-T1 / D3 directed-wake): rows are never written for agent
// identities; the service skips agents entirely.
type UserConversationFollowState struct {
	UserID         IdentityRef
	ConversationID ConversationID
	Followed       bool
	UpdatedAt      time.Time
	Version        int
}

// UserConversationFollowStateRepository is the port over the
// user_conversation_follow_state table (migration 0050). It mirrors the
// optimistic-locking contract of UserConversationReadStateRepository.
type UserConversationFollowStateRepository interface {
	// FindByUserAndConv returns the row or ErrFollowStateNotFound when
	// absent (= "no explicit override; use the kind default").
	FindByUserAndConv(ctx context.Context, userID IdentityRef,
		convID ConversationID) (*UserConversationFollowState, error)

	// FindByUserBatch returns every override row for a user. Powers the
	// sidebar's per-conversation followed flag without an N+1 of
	// FindByUserAndConv. Caller bounds N by the natural cardinality of
	// conversations the user has explicitly (un)followed.
	FindByUserBatch(ctx context.Context, userID IdentityRef) (
		[]*UserConversationFollowState, error)

	// Upsert inserts when Version == 0; otherwise CAS on
	// (user_id, conversation_id, version). Bumps Version on success.
	// Returns ErrFollowStateVersionConflict when a concurrent writer
	// already moved the row.
	Upsert(ctx context.Context, s *UserConversationFollowState) error

	// InsertIfAbsent records followed=true ONLY when no row exists yet —
	// the auto-follow primitive. It must NEVER override an explicit
	// unfollow (followed=false): a present row is left untouched.
	// Returns true when a row was inserted, false when one already
	// existed.
	InsertIfAbsent(ctx context.Context, s *UserConversationFollowState) (bool, error)

	// DeleteByConversationID hard-removes all follow-state rows for a
	// conversation (DM / conversation delete, symmetric with read-state).
	// Idempotent: no rows = no error.
	DeleteByConversationID(ctx context.Context, convID ConversationID) error
}

// Follow-state sentinel errors.
var (
	ErrFollowStateNotFound        = errors.New("conversation: follow state not found")
	ErrFollowStateVersionConflict = errors.New("conversation: follow state version conflict (optimistic lock)")
)
