package api

import (
	"net/http"

	"github.com/oopslink/agent-center/internal/conversation"
)

// followConversationHandler records an explicit follow (v2.8 #268). The
// follow/unfollow pair drives whether a conversation contributes to the
// user's unread/mention badges; auto-follow happens separately on
// participate / @mention.
func (s *Server) followConversationHandler(w http.ResponseWriter, r *http.Request) {
	s.setFollowState(w, r, true)
}

// unfollowConversationHandler records an explicit unfollow (DELETE).
func (s *Server) unfollowConversationHandler(w http.ResponseWriter, r *http.Request) {
	s.setFollowState(w, r, false)
}

func (s *Server) setFollowState(w http.ResponseWriter, r *http.Request, follow bool) {
	d := hd(r)
	if d.FollowStateSvc == nil {
		writeError(w, http.StatusNotImplemented, "follow_state_not_wired", "")
		return
	}
	convID := conversation.ConversationID(r.PathValue("id"))
	userID := readStateUserID(r, d)
	if err := userID.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_user_id", err.Error())
		return
	}
	// Org guard (also yields 404 for a missing / cross-org conv).
	if _, ok := s.requireConversationInOrg(w, r, d, string(convID)); !ok {
		return
	}
	var err error
	if follow {
		err = d.FollowStateSvc.Follow(r.Context(), userID, convID)
	} else {
		err = d.FollowStateSvc.Unfollow(r.Context(), userID, convID)
	}
	if err != nil {
		mapDomainError(w, err)
		return
	}
	// Reflect the effective resolved state (agents always resolve false —
	// Q-T1), not the raw request, so the client never shows a follow that
	// the human-only model silently dropped.
	followed, err := d.FollowStateSvc.IsFollowed(r.Context(), userID, convID)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"conversation_id": string(convID),
		"user_id":         string(userID),
		"followed":        followed,
	})
}

// resolveDisplayName returns the display name for an identity ref (used to
// compute mention_count, which matches @display_name). Empty when the repo
// is unwired or the identity is unresolvable — mention_count then resolves
// to 0, never an error.
func resolveDisplayName(r *http.Request, d HandlerDeps, ref conversation.IdentityRef) string {
	if d.IdentityRepo == nil {
		return ""
	}
	ident, err := d.IdentityRepo.GetByID(r.Context(), refBareID(ref))
	if err != nil || ident == nil {
		return ""
	}
	return ident.DisplayName()
}

// embedBadges writes unread_count / mention_count / followed onto a
// conversation DTO row for the v2.8 #268 badge model. followed is supplied
// pre-resolved by the caller (batch ResolveFollowed in the list path, or
// IsFollowed in the detail path), already agent-aware. unread/mention are
// zeroed for non-human identities (Q-T1) and computed fail-soft otherwise
// (a badge error never breaks the conversation list).
func (s *Server) embedBadges(r *http.Request, d HandlerDeps,
	userID conversation.IdentityRef, displayName string,
	conv *conversation.Conversation, row map[string]any, followed bool,
) {
	row["followed"] = followed
	// Badges count only followed conversations (contract: unfollowed → stops),
	// and never for agents (Q-T1).
	if d.ReadStateSvc == nil || !userID.IsHuman() || !followed {
		row["unread_count"] = 0
		row["mention_count"] = 0
		return
	}
	sum, err := d.ReadStateSvc.UnreadWithMentions(r.Context(), userID, conv.ID(), displayName)
	if err != nil {
		row["unread_count"] = 0
		row["mention_count"] = 0
		return
	}
	row["unread_count"] = sum.UnreadCount
	row["mention_count"] = sum.MentionCount
}
