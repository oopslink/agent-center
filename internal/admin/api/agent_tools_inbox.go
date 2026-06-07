package api

import (
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
)

// getMyUnreadReq is the body for POST /admin/agent-tools/get_my_unread.
type getMyUnreadReq struct {
	AgentID string `json:"agent_id"`
}

// getMyUnreadHandler returns the unread messages directed at the OPERATING agent
// — every unread message in a DM it participates in + every unread @mention of it
// in a channel it participates in — across its conversations, org-scoped. The read
// side of the v2.8.1 #278 D dual-stream (PR4b). Own-scoped via requireAgentOnWorker;
// the agent reads only its own inbox.
func (s *Server) getMyUnreadHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req getMyUnreadReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.InboxSvc == nil {
		writeError(w, http.StatusNotImplemented, "inbox_svc_not_wired", "")
		return
	}
	// Dual-ref: an agent may appear as a participant / read-state cursor / sender
	// under EITHER its execution ref OR its identity-member ref (see
	// ListUnreadForIdentity / wake_projector). Pass both.
	refs := []conversation.IdentityRef{conversation.IdentityRef("agent:" + string(a.ID()))}
	if a.IdentityMemberID() != "" {
		refs = append(refs, conversation.IdentityRef("agent:"+a.IdentityMemberID()))
	}
	items, err := d.InboxSvc.ListUnreadForIdentity(r.Context(), refs, a.OrganizationID(), a.Profile().Name)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(items))
	for i, it := range items {
		out[i] = map[string]any{
			"conversation_id":   string(it.ConversationID),
			"conversation_kind": string(it.ConversationKind),
			"conversation_name": it.ConversationName,
			"message_id":        string(it.MessageID),
			"sender":            string(it.SenderRef),
			"content":           it.Content,
			"posted_at":         it.PostedAt.Format(time.RFC3339Nano),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"unread": out})
}
