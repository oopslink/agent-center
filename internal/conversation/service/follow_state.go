package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
)

// FollowStateService owns follow/unfollow writes and the followed-state
// read used by the v2.8 #268 badge model.
//
// DEFAULT (no override row):
//   - top-level conversation (channel / DM, ParentConversationID empty)
//     → followed;
//   - thread (ParentConversationID non-null) → NOT followed until the
//     user participates or is @mentioned (auto-follow).
//
// A row exists only to record an explicit override of that default.
//
// HUMAN-ONLY (Q-T1 / D3): agent identities never get rows and always
// resolve to followed=false — directed-wake, no badge accumulation.
type FollowStateService struct {
	fsRepo   conversation.UserConversationFollowStateRepository
	convRepo conversation.ConversationRepository
	clock    clock.Clock
}

// NewFollowStateService constructs the service.
func NewFollowStateService(
	fsRepo conversation.UserConversationFollowStateRepository,
	convRepo conversation.ConversationRepository,
	clk clock.Clock,
) *FollowStateService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &FollowStateService{fsRepo: fsRepo, convRepo: convRepo, clock: clk}
}

// IsFollowed resolves the effective followed state for (user, conv):
// explicit override row if present, else the kind default. Agents always
// resolve to false.
func (s *FollowStateService) IsFollowed(ctx context.Context,
	userID conversation.IdentityRef, convID conversation.ConversationID,
) (bool, error) {
	if !userID.IsHuman() {
		return false, nil
	}
	row, err := s.fsRepo.FindByUserAndConv(ctx, userID, convID)
	if err == nil {
		return row.Followed, nil
	}
	if !errors.Is(err, conversation.ErrFollowStateNotFound) {
		return false, err
	}
	return s.defaultFollow(ctx, convID)
}

// ResolveFollowed batch-resolves followed state for many conversations
// the caller already holds (sidebar / list embedding). One repo round-trip
// for the user's override rows; defaults computed from the supplied
// Conversation objects (no extra reads). Agents → all false.
func (s *FollowStateService) ResolveFollowed(ctx context.Context,
	userID conversation.IdentityRef, convs []*conversation.Conversation,
) (map[conversation.ConversationID]bool, error) {
	out := make(map[conversation.ConversationID]bool, len(convs))
	if !userID.IsHuman() {
		for _, c := range convs {
			out[c.ID()] = false
		}
		return out, nil
	}
	overrides, err := s.fsRepo.FindByUserBatch(ctx, userID)
	if err != nil {
		return nil, err
	}
	ov := make(map[conversation.ConversationID]bool, len(overrides))
	for _, o := range overrides {
		ov[o.ConversationID] = o.Followed
	}
	for _, c := range convs {
		if v, ok := ov[c.ID()]; ok {
			out[c.ID()] = v
			continue
		}
		out[c.ID()] = c.ParentConversationID() == ""
	}
	return out, nil
}

// Follow records an explicit follow (followed=true). No-op for agents.
func (s *FollowStateService) Follow(ctx context.Context,
	userID conversation.IdentityRef, convID conversation.ConversationID,
) error {
	return s.setFollow(ctx, userID, convID, true)
}

// Unfollow records an explicit unfollow (followed=false). No-op for agents.
func (s *FollowStateService) Unfollow(ctx context.Context,
	userID conversation.IdentityRef, convID conversation.ConversationID,
) error {
	return s.setFollow(ctx, userID, convID, false)
}

// AutoFollow follows a conversation only if the user has no override yet
// (participate / @mention). It never resurrects an explicit unfollow and
// is a no-op for agents. Primarily meaningful for threads (top-level is
// already followed by default).
func (s *FollowStateService) AutoFollow(ctx context.Context,
	userID conversation.IdentityRef, convID conversation.ConversationID,
) error {
	if !userID.IsHuman() {
		return nil
	}
	if err := userID.Validate(); err != nil {
		return fmt.Errorf("auto follow: user_id: %w", err)
	}
	_, err := s.fsRepo.InsertIfAbsent(ctx, &conversation.UserConversationFollowState{
		UserID:         userID,
		ConversationID: convID,
		Followed:       true,
		UpdatedAt:      s.clock.Now(),
	})
	return err
}

// setFollow upserts an explicit override, retrying once on an optimistic
// version conflict (a concurrent writer moved the row between our read and
// write).
func (s *FollowStateService) setFollow(ctx context.Context,
	userID conversation.IdentityRef, convID conversation.ConversationID, followed bool,
) error {
	if !userID.IsHuman() {
		return nil
	}
	if err := userID.Validate(); err != nil {
		return fmt.Errorf("set follow: user_id: %w", err)
	}
	if convID == "" {
		return errors.New("set follow: conversation_id required")
	}
	for attempt := 0; attempt < 2; attempt++ {
		existing, err := s.fsRepo.FindByUserAndConv(ctx, userID, convID)
		state := &conversation.UserConversationFollowState{
			UserID:         userID,
			ConversationID: convID,
			Followed:       followed,
			UpdatedAt:      s.clock.Now(),
		}
		switch {
		case err == nil:
			if existing.Followed == followed {
				return nil // idempotent: already in the requested state.
			}
			state.Version = existing.Version
		case errors.Is(err, conversation.ErrFollowStateNotFound):
			// Version 0 → insert path.
		default:
			return err
		}
		uerr := s.fsRepo.Upsert(ctx, state)
		if uerr == nil {
			return nil
		}
		if errors.Is(uerr, conversation.ErrFollowStateVersionConflict) {
			continue // retry once with a fresh read.
		}
		return uerr
	}
	return conversation.ErrFollowStateVersionConflict
}

// DeleteByConversationID removes all follow-state rows for a conversation,
// for the DM / conversation hard-delete cascade (symmetric with read-state).
// Idempotent.
func (s *FollowStateService) DeleteByConversationID(ctx context.Context,
	convID conversation.ConversationID,
) error {
	return s.fsRepo.DeleteByConversationID(ctx, convID)
}

// defaultFollow returns the kind default for a conversation with no
// override row: top-level → followed, thread → not followed.
func (s *FollowStateService) defaultFollow(ctx context.Context,
	convID conversation.ConversationID,
) (bool, error) {
	c, err := s.convRepo.FindByID(ctx, convID)
	if err != nil {
		return false, err
	}
	return c.ParentConversationID() == "", nil
}
