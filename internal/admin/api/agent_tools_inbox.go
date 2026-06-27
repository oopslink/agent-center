package api

import (
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/observability"
)

// agentConvRefs returns the identity refs an agent may appear under in a
// conversation (execution ref + identity-member ref). See ListUnreadForIdentity.
func agentConvRefs(a *agent.Agent) []conversation.IdentityRef {
	refs := []conversation.IdentityRef{conversation.IdentityRef("agent:" + string(a.ID()))}
	if a.IdentityMemberID() != "" {
		refs = append(refs, conversation.IdentityRef("agent:"+a.IdentityMemberID()))
	}
	return refs
}

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
	refs := agentConvRefs(a)
	items, err := d.InboxSvc.ListUnreadForIdentity(r.Context(), refs, a.OrganizationID(), a.Profile().Name)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(items))
	for i, it := range items {
		m := map[string]any{
			"conversation_id":   string(it.ConversationID),
			"conversation_kind": string(it.ConversationKind),
			"conversation_name": it.ConversationName,
			"message_id":        string(it.MessageID),
			"sender":            string(it.SenderRef),
			"content":           it.Content,
			"posted_at":         it.PostedAt.Format(time.RFC3339Nano),
			// I7-D2 (cognition/04-wake-guardrail.md §3.6): tag each item with the
			// sender kind + reply obligation. A HUMAN directed message must be
			// answered (reply_required=true); an AGENT-authored mention is "可回可不回"
			// — reply only if content warrants, otherwise SilentAck via mark_seen
			// (reply_required=false). This does NOT change wake/inbox filtering, only
			// the response semantic surfaced to the agent.
			"actor_kind":     string(it.ActorKind),
			"reply_required": it.ActorKind == conversation.ActorKindHuman,
		}
		// v2.10.0 [T74]: surface inbound attachments (file_uri + metadata) so the
		// agent can perceive + download_file a screenshot a human sent. Present
		// only when the message carries attachments. Each uri is from a
		// conversation the agent participates in (download_file authz fail-closed).
		if len(it.Attachments) > 0 {
			atts := make([]map[string]any, len(it.Attachments))
			for j, a := range it.Attachments {
				atts[j] = map[string]any{
					"uri":       a.URI,
					"filename":  a.Filename,
					"mime_type": a.MimeType,
					"size":      a.Size,
				}
			}
			m["attachments"] = atts
		}
		out[i] = m
	}
	writeJSON(w, http.StatusOK, map[string]any{"unread": out})
}

// markSeenReq is the body for POST /admin/agent-tools/mark_seen.
type markSeenReq struct {
	AgentID        string `json:"agent_id"`
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
}

// markSeenHandler advances the operating agent's read cursor in a conversation
// (v2.8.1 #278 D PR4b): after the agent replies to (or otherwise handles) an
// unread message from get_my_unread, it calls this so the message is not
// re-surfaced / re-handled. The cursor is keyed by the ref the agent actually
// participates as in that conversation (so get_my_unread, which reads that ref,
// sees the advance). only-forward (a stale cursor never regresses).
func (s *Server) markSeenHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req markSeenReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.ReadStateSvc == nil {
		writeError(w, http.StatusNotImplemented, "read_state_svc_not_wired", "")
		return
	}
	if req.ConversationID == "" || req.MessageID == "" {
		writeError(w, http.StatusBadRequest, "missing_fields", "conversation_id and message_id are required")
		return
	}
	convID := conversation.ConversationID(req.ConversationID)
	refs := agentConvRefs(a)
	// Mark under the ref the agent actually participates as in this conversation
	// (the cursor get_my_unread reads). Fall back to the execution ref.
	selfRef := refs[0]
	if d.ConvRepo != nil {
		if c, err := d.ConvRepo.FindByID(r.Context(), convID); err == nil && c != nil {
			for _, p := range c.Participants() {
				if p.LeftAt != "" {
					continue
				}
				for _, ref := range refs {
					if p.IdentityID == ref {
						selfRef = ref
					}
				}
			}
		}
	}
	if _, err := d.ReadStateSvc.MarkSeen(r.Context(), convservice.MarkSeenCommand{
		UserID:            selfRef,
		ConversationID:    convID,
		LastSeenMessageID: conversation.MessageID(req.MessageID),
		Actor:             observability.Actor(selfRef),
		Trigger:           convservice.MarkSeenTriggerAgentTool,
	}); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
