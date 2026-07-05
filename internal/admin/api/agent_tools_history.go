package api

import (
	"errors"
	"net/http"

	"github.com/oopslink/agent-center/internal/conversation"
)

// =============================================================================
// Agent MCP read tool — conversation history (browse chat records).
//
//	list_messages — read the message history of a conversation the agent
//	                participates in (DM / channel / task / issue / plan),
//	                newest-first window with older-history pagination.
//
// Motivation: the only prior read surface was get_my_unread (the agent's own
// unread inbox — DMs + @mentions). An agent could not BROWSE a conversation's
// history: it could not catch up on a channel it just joined, re-read context it
// already marked seen, or read messages it was never @mentioned in. This tool
// closes that gap for every conversation kind, behind the SAME participant gate
// post_message uses (agentIsActiveParticipant) — visibility never exceeds the
// write boundary, and a non-member is told to ask to be added (channels).
// =============================================================================

// listMessagesReq is the body for POST /admin/agent-tools/list_messages.
type listMessagesReq struct {
	AgentID        string `json:"agent_id"`
	ConversationID string `json:"conversation_id"`
	// Limit caps how many of the NEWEST messages to return (default 50, max 200).
	Limit int `json:"limit"`
	// BeforeMessageID is the older-history cursor: when set, only messages strictly
	// OLDER than this message are returned (the newest page before it). Pass the
	// previous response's next_before_message_id to walk further back. Empty = the
	// most-recent page.
	BeforeMessageID string `json:"before_message_id"`
}

const (
	listMessagesDefaultLimit = 50
	listMessagesMaxLimit     = 200
)

// listMessagesHandler returns a conversation's message history, newest window
// first, for the OPERATING agent — gated to conversations it is an active
// participant of (the same gate post_message's conversation branch enforces).
// Messages come back in chronological order (oldest→newest within the page).
// Pagination is keyset over (posted_at, id): pass before_message_id to load the
// page of older messages preceding a known message; has_more + next_before_message_id
// signal whether to keep walking back.
func (s *Server) listMessagesHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req listMessagesReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.ConvRepo == nil || d.MsgRepo == nil {
		writeError(w, http.StatusNotImplemented, "conversation_not_wired", "")
		return
	}
	if req.ConversationID == "" {
		writeError(w, http.StatusBadRequest, "missing_conversation_id",
			"conversation_id is required — use find_org_channel (or a conversation_id from get_my_unread) to obtain one")
		return
	}

	convID := conversation.ConversationID(req.ConversationID)
	conv, err := d.ConvRepo.FindByID(r.Context(), convID)
	if err != nil {
		if errors.Is(err, conversation.ErrConversationNotFound) {
			writeError(w, http.StatusNotFound, "conversation_not_found",
				"conversation "+req.ConversationID+" not found — use find_org_channel to resolve a channel name to its id")
			return
		}
		mapDomainError(w, err)
		return
	}
	// Read gate == write gate: an active participant may read; a non-member may not.
	// Channels get the actionable "ask to be added" wording (mirrors post_message).
	if !agentIsActiveParticipant(conv, a) {
		if conv.Kind() == conversation.ConversationKindChannel {
			writeError(w, http.StatusForbidden, "not_a_channel_member",
				"not a member of channel "+conv.Name()+" — ask an owner to add you before reading its history")
			return
		}
		writeError(w, http.StatusForbidden, "not_a_participant",
			"agent is not an active participant of this conversation")
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = listMessagesDefaultLimit
	}
	if limit > listMessagesMaxLimit {
		limit = listMessagesMaxLimit
	}

	filter := conversation.MessageFilter{Tail: limit + 1} // +1 probes has_more
	if req.BeforeMessageID != "" {
		cursor, cerr := d.MsgRepo.FindByID(r.Context(), conversation.MessageID(req.BeforeMessageID))
		if cerr != nil {
			if errors.Is(cerr, conversation.ErrMessageNotFound) {
				writeError(w, http.StatusNotFound, "before_message_id_not_found",
					"before_message_id "+req.BeforeMessageID+" not found in this conversation")
				return
			}
			mapDomainError(w, cerr)
			return
		}
		// Guard cross-conversation cursors: the cursor must belong to THIS conversation
		// or the keyset window is meaningless.
		if string(cursor.ConversationID()) != req.ConversationID {
			writeError(w, http.StatusBadRequest, "before_message_id_wrong_conversation",
				"before_message_id belongs to a different conversation")
			return
		}
		pa := cursor.PostedAt()
		filter.BeforePostedAt = &pa
		filter.BeforeID = string(cursor.ID())
	}

	msgs, err := d.MsgRepo.FindByConversationID(r.Context(), convID, filter)
	if err != nil {
		mapDomainError(w, err)
		return
	}

	// The +1 probe: more than `limit` rows means there is an older page. Drop the
	// extra OLDEST row (msgs[0]) so the caller gets exactly `limit`, and surface the
	// new oldest id as the next cursor.
	hasMore := false
	if len(msgs) > limit {
		hasMore = true
		msgs = msgs[1:]
	}

	out := make([]map[string]any, len(msgs))
	for i, m := range msgs {
		out[i] = messageMap(m)
	}
	// 引用 (quote): resolve the inline preview card for every quoting message in the
	// page so a browsing agent sees WHAT was quoted, not just a bare pointer.
	attachQuotePreviews(r.Context(), d.MsgRepo, out)
	resp := map[string]any{
		"conversation_id":   string(conv.ID()),
		"conversation_kind": string(conv.Kind()),
		"conversation_name": conv.Name(),
		"messages":          out,
		"has_more":          hasMore,
	}
	// next_before_message_id is the oldest returned message's id — pass it back as
	// before_message_id to fetch the preceding page. Present only when has_more.
	if hasMore && len(msgs) > 0 {
		resp["next_before_message_id"] = string(msgs[0].ID())
	}
	writeJSON(w, http.StatusOK, resp)
}
