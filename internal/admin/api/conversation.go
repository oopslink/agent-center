package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
)

// =============================================================================
// ConvRepo — Find / FindByID / FindByName
// =============================================================================

func (s *Server) convFindHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ConvRepo == nil {
		writeError(w, http.StatusNotImplemented, "conv_repo_not_wired", "")
		return
	}
	filter := conversation.ConversationFilter{}
	if v := r.URL.Query().Get("kind"); v != "" {
		k := conversation.ConversationKind(v)
		filter.Kind = &k
	}
	if v := r.URL.Query().Get("status"); v != "" {
		st := conversation.ConversationStatus(v)
		filter.Status = &st
	}
	list, err := d.ConvRepo.Find(r.Context(), filter)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, c := range list {
		out[i] = convMap(c)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) convFindByIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ConvRepo == nil {
		writeError(w, http.StatusNotImplemented, "conv_repo_not_wired", "")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	c, err := d.ConvRepo.FindByID(r.Context(), conversation.ConversationID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, convMap(c))
}

func (s *Server) convFindByNameHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ConvRepo == nil {
		writeError(w, http.StatusNotImplemented, "conv_repo_not_wired", "")
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing_name", "")
		return
	}
	c, err := d.ConvRepo.FindByName(r.Context(), name)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, convMap(c))
}

// =============================================================================
// MsgRepo — FindByID / FindByConversationID / Append
// =============================================================================

func (s *Server) msgFindByIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.MsgRepo == nil {
		writeError(w, http.StatusNotImplemented, "msg_repo_not_wired", "")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	m, err := d.MsgRepo.FindByID(r.Context(), conversation.MessageID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, messageMap(m))
}

func (s *Server) msgFindByConversationIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.MsgRepo == nil {
		writeError(w, http.StatusNotImplemented, "msg_repo_not_wired", "")
		return
	}
	convID := r.URL.Query().Get("conversation_id")
	if convID == "" {
		writeError(w, http.StatusBadRequest, "missing_conversation_id", "")
		return
	}
	list, err := d.MsgRepo.FindByConversationID(r.Context(),
		conversation.ConversationID(convID),
		conversation.MessageFilter{Limit: 200})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, m := range list {
		out[i] = messageMap(m)
	}
	writeJSON(w, http.StatusOK, out)
}

// v2.3-1 (task #24): proper tail/recent proxy. Replaces the prior
// CLI workaround that called find-by-conversation-id and trimmed the
// last N client-side (which capped at the server's hard-coded 200).
// `?n=` honours MessageRepo.FindRecent's zero-default (50) when omitted.
func (s *Server) msgFindRecentHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.MsgRepo == nil {
		writeError(w, http.StatusNotImplemented, "msg_repo_not_wired", "")
		return
	}
	convID := r.URL.Query().Get("conversation_id")
	if convID == "" {
		writeError(w, http.StatusBadRequest, "missing_conversation_id", "")
		return
	}
	n := 0
	if v := r.URL.Query().Get("n"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			n = parsed
		}
	}
	list, err := d.MsgRepo.FindRecent(r.Context(), conversation.ConversationID(convID), n)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, m := range list {
		out[i] = messageMap(m)
	}
	writeJSON(w, http.StatusOK, out)
}

// msgAppendHandler is the AppService AddMessage path (not the bare repo
// Append — repos don't emit events). Wraps MessageWriter.AddMessage.
type msgAppendReq struct {
	ConversationID   string `json:"conversation_id"`
	SenderIdentityID string `json:"sender_identity_id"`
	ContentKind      string `json:"content_kind"`
	Content          string `json:"content"`
	Direction        string `json:"direction"`
	InputRequestRef  string `json:"input_request_ref"`
}

func (s *Server) msgAppendHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.MessageWriter == nil {
		writeError(w, http.StatusNotImplemented, "message_writer_not_wired", "")
		return
	}
	var req msgAppendReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	sender := conversation.IdentityRef(req.SenderIdentityID)
	if req.SenderIdentityID == "" {
		sender = conversation.IdentityRef(d.Actor)
	}
	ck := req.ContentKind
	if ck == "" {
		ck = string(conversation.MessageContentText)
	}
	dir := req.Direction
	if dir == "" {
		dir = string(conversation.DirectionInbound)
	}
	res, err := d.MessageWriter.AddMessage(r.Context(), convservice.AddMessageCommand{
		ConversationID:   conversation.ConversationID(req.ConversationID),
		SenderIdentityID: sender,
		ContentKind:      conversation.MessageContentKind(ck),
		Content:          req.Content,
		Direction:        conversation.MessageDirection(dir),
		InputRequestRef:  req.InputRequestRef,
		Actor:            d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message_id": string(res.MessageID),
		"event_id":   string(res.EventID),
	})
}

// =============================================================================
// MessageWriter — OpenConversation / AddMessage / Close / Archive
// =============================================================================

type openConvReq struct {
	Kind                 string                            `json:"kind"`
	Name                 string                            `json:"name"`
	Description          string                            `json:"description"`
	ParentConversationID string                            `json:"parent_conversation_id"`
	Participants         []conversation.ParticipantElement `json:"participants"`
	CreatedBy            string                            `json:"created_by"`
}

func (s *Server) openConversationHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.MessageWriter == nil {
		writeError(w, http.StatusNotImplemented, "message_writer_not_wired", "")
		return
	}
	var req openConvReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	createdBy := conversation.IdentityRef(req.CreatedBy)
	if req.CreatedBy == "" {
		createdBy = conversation.IdentityRef(d.Actor)
	}
	res, err := d.MessageWriter.OpenConversation(r.Context(), convservice.OpenCommand{
		Kind:                 conversation.ConversationKind(req.Kind),
		Name:                 req.Name,
		Description:          req.Description,
		ParentConversationID: conversation.ConversationID(req.ParentConversationID),
		Participants:         req.Participants,
		CreatedBy:            createdBy,
		Actor:                d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"conversation_id": string(res.ConversationID),
		"event_id":        string(res.EventID),
	})
}

type closeConvReq struct {
	ConversationID string `json:"conversation_id"`
	Version        int    `json:"version"`
	Reason         string `json:"reason"`
	Message        string `json:"message"`
}

func (s *Server) closeConversationHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.MessageWriter == nil {
		writeError(w, http.StatusNotImplemented, "message_writer_not_wired", "")
		return
	}
	var req closeConvReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	evID, err := d.MessageWriter.Close(r.Context(), convservice.CloseCommand{
		ConversationID: conversation.ConversationID(req.ConversationID),
		Version:        req.Version,
		Reason:         req.Reason,
		Message:        req.Message,
		Actor:          d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"event_id": string(evID)})
}

type archiveConvReq struct {
	ConversationID string `json:"conversation_id"`
	Version        int    `json:"version"`
	ArchivedBy     string `json:"archived_by"`
}

func (s *Server) archiveConversationHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.MessageWriter == nil {
		writeError(w, http.StatusNotImplemented, "message_writer_not_wired", "")
		return
	}
	var req archiveConvReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	archivedBy := conversation.IdentityRef(req.ArchivedBy)
	if req.ArchivedBy == "" {
		archivedBy = conversation.IdentityRef(d.Actor)
	}
	evID, err := d.MessageWriter.Archive(r.Context(), convservice.ArchiveCommand{
		ConversationID: conversation.ConversationID(req.ConversationID),
		Version:        req.Version,
		ArchivedBy:     archivedBy,
		Actor:          d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"event_id": string(evID)})
}

// =============================================================================
// ChannelMgmtSvc — CreateChannel / ArchiveChannel
// =============================================================================

type createChannelReq struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedBy   string `json:"created_by"`
}

func (s *Server) createChannelHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ChannelMgmtSvc == nil {
		writeError(w, http.StatusNotImplemented, "channel_mgmt_svc_not_wired", "")
		return
	}
	var req createChannelReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	createdBy := conversation.IdentityRef(req.CreatedBy)
	if req.CreatedBy == "" {
		createdBy = conversation.IdentityRef(d.Actor)
	}
	res, err := d.ChannelMgmtSvc.CreateChannel(r.Context(), convservice.CreateChannelCommand{
		Name:        req.Name,
		Description: req.Description,
		CreatedBy:   createdBy,
		Actor:       d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"conversation_id": string(res.ConversationID),
		"event_id":        string(res.EventID),
	})
}

type archiveChannelReq struct {
	Name       string `json:"name"`
	ArchivedBy string `json:"archived_by"`
}

func (s *Server) archiveChannelHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ChannelMgmtSvc == nil {
		writeError(w, http.StatusNotImplemented, "channel_mgmt_svc_not_wired", "")
		return
	}
	var req archiveChannelReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	archivedBy := conversation.IdentityRef(req.ArchivedBy)
	if req.ArchivedBy == "" {
		archivedBy = conversation.IdentityRef(d.Actor)
	}
	evID, err := d.ChannelMgmtSvc.ArchiveChannel(r.Context(), convservice.ArchiveChannelCommand{
		Name:       req.Name,
		ArchivedBy: archivedBy,
		Actor:      d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"event_id": string(evID)})
}

// =============================================================================
// ParticipantMgmtSvc — Invite / Kick
// =============================================================================

type inviteParticipantReq struct {
	ConversationName string `json:"conversation_name"`
	IdentityID       string `json:"identity_id"`
	Role             string `json:"role"`
	InvitedBy        string `json:"invited_by"`
}

func (s *Server) inviteParticipantHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ParticipantMgmtSvc == nil {
		writeError(w, http.StatusNotImplemented, "participant_mgmt_svc_not_wired", "")
		return
	}
	var req inviteParticipantReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	invitedBy := conversation.IdentityRef(req.InvitedBy)
	if req.InvitedBy == "" {
		invitedBy = conversation.IdentityRef(d.Actor)
	}
	evID, err := d.ParticipantMgmtSvc.Invite(r.Context(), convservice.InviteCommand{
		ConversationName: req.ConversationName,
		IdentityID:       conversation.IdentityRef(req.IdentityID),
		Role:             req.Role,
		InvitedBy:        invitedBy,
		Actor:            d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"event_id": string(evID)})
}

type kickParticipantReq struct {
	ConversationName string `json:"conversation_name"`
	IdentityID       string `json:"identity_id"`
	KickedBy         string `json:"kicked_by"`
	Reason           string `json:"reason"`
}

func (s *Server) kickParticipantHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ParticipantMgmtSvc == nil {
		writeError(w, http.StatusNotImplemented, "participant_mgmt_svc_not_wired", "")
		return
	}
	var req kickParticipantReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	kickedBy := conversation.IdentityRef(req.KickedBy)
	if req.KickedBy == "" {
		kickedBy = conversation.IdentityRef(d.Actor)
	}
	evID, err := d.ParticipantMgmtSvc.Kick(r.Context(), convservice.KickCommand{
		ConversationName: req.ConversationName,
		IdentityID:       conversation.IdentityRef(req.IdentityID),
		KickedBy:         kickedBy,
		Reason:           req.Reason,
		Actor:            d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"event_id": string(evID)})
}

// v2.3-1 (task #24): self-leave proxy. Previously `channel leave` CLI
// fell back to the legacy direct-service path because there was no
// admin endpoint mirror; this closes that gap so the CLI uniformly
// goes through Client → admin endpoint → AppService (conventions § 0.4).
type leaveParticipantReq struct {
	ConversationName string `json:"conversation_name"`
	IdentityID       string `json:"identity_id"`
	Reason           string `json:"reason"`
}

func (s *Server) leaveParticipantHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ParticipantMgmtSvc == nil {
		writeError(w, http.StatusNotImplemented, "participant_mgmt_svc_not_wired", "")
		return
	}
	var req leaveParticipantReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	identity := req.IdentityID
	if identity == "" {
		identity = string(d.Actor)
	}
	evID, err := d.ParticipantMgmtSvc.Leave(r.Context(), convservice.LeaveCommand{
		ConversationName: req.ConversationName,
		IdentityID:       conversation.IdentityRef(identity),
		Reason:           req.Reason,
		Actor:            d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"event_id": string(evID)})
}

// =============================================================================
// CarryOverSvc — FindByChildConv / FindBySourceMsg
// =============================================================================

func (s *Server) carryOverFindByChildConvHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.CarryOverSvc == nil {
		writeError(w, http.StatusNotImplemented, "carry_over_svc_not_wired", "")
		return
	}
	id := r.URL.Query().Get("child_conversation_id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_child_conversation_id", "")
		return
	}
	refs, err := d.CarryOverSvc.FindByChildConv(r.Context(), conversation.ConversationID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, refsToMap(refs))
}

func (s *Server) carryOverFindBySourceMsgHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.CarryOverSvc == nil {
		writeError(w, http.StatusNotImplemented, "carry_over_svc_not_wired", "")
		return
	}
	id := r.URL.Query().Get("source_message_id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_source_message_id", "")
		return
	}
	refs, err := d.CarryOverSvc.FindBySourceMsg(r.Context(), conversation.MessageID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, refsToMap(refs))
}

// =============================================================================
// ConvRefRepo — FindByChildConvID / FindBySourceMsgID
// (separate from CarryOverSvc proxies above; raw repo path)
// =============================================================================

func (s *Server) convRefFindByChildConvIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ConvRefRepo == nil {
		writeError(w, http.StatusNotImplemented, "conv_ref_repo_not_wired", "")
		return
	}
	id := r.URL.Query().Get("child_conversation_id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_child_conversation_id", "")
		return
	}
	refs, err := d.ConvRefRepo.FindByChildConvID(r.Context(), conversation.ConversationID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, refsToMap(refs))
}

func (s *Server) convRefFindBySourceMsgIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ConvRefRepo == nil {
		writeError(w, http.StatusNotImplemented, "conv_ref_repo_not_wired", "")
		return
	}
	id := r.URL.Query().Get("source_message_id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_source_message_id", "")
		return
	}
	refs, err := d.ConvRefRepo.FindBySourceMsgID(r.Context(), conversation.MessageID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, refsToMap(refs))
}

// =============================================================================
// Projection helpers
// =============================================================================

func convMap(c *conversation.Conversation) map[string]any {
	m := map[string]any{
		"id":                     string(c.ID()),
		"kind":                   string(c.Kind()),
		"name":                   c.Name(),
		"description":            c.Description(),
		"status":                 string(c.Status()),
		"parent_conversation_id": string(c.ParentConversationID()),
		"created_by":             string(c.CreatedBy()),
		"created_at":             c.CreatedAt().Format(time.RFC3339Nano),
		"updated_at":             c.UpdatedAt().Format(time.RFC3339Nano),
		"version":                c.Version(),
	}
	if a := c.ArchivedAt(); a != nil {
		m["archived_at"] = a.Format(time.RFC3339Nano)
		m["archived_by"] = string(c.ArchivedBy())
	}
	parts := c.Participants()
	arr := make([]map[string]any, len(parts))
	for i, p := range parts {
		arr[i] = map[string]any{
			"identity_id": string(p.IdentityID),
			"role":        p.Role,
			"joined_at":   p.JoinedAt,
			"joined_by":   string(p.JoinedBy),
			"left_at":     p.LeftAt,
			"left_reason": p.LeftReason,
		}
	}
	m["participants"] = arr
	return m
}

func messageMap(m *conversation.Message) map[string]any {
	out := map[string]any{
		"id":                 string(m.ID()),
		"conversation_id":    string(m.ConversationID()),
		"sender_identity_id": string(m.SenderIdentityID()),
		"content_kind":       string(m.ContentKind()),
		"content":            m.Content(),
		"direction":          string(m.Direction()),
		"input_request_ref":  m.InputRequestRef(),
		"posted_at":          m.PostedAt().Format(time.RFC3339Nano),
	}
	// 引用 (quote): emit the raw pointer for a quoting message; the resolved preview
	// (sender + snippet, or a deleted stub) is injected separately by the agent read
	// handlers via attachQuotePreviews — an O(1) batch lookup, not a per-msg round-trip.
	// Mirrors the UI serializer (webconsole handlers.go) so agent + UI see quotes alike.
	if qid := m.QuotedMessageID(); qid != "" {
		out["quoted_message_id"] = string(qid)
	}
	return out
}

// quoteSnippetMaxRunes bounds the inlined quoted-message preview text so a huge
// quoted message can't bloat every agent read response (matches the UI read model).
const quoteSnippetMaxRunes = 120

// quoteSnippet returns a single-line, rune-safe truncation of a quoted message's
// content for the agent-facing preview (newlines collapse to spaces; over the cap
// appends an ellipsis). Byte-identical to the UI's quoteSnippet.
func quoteSnippet(content string) string {
	s := strings.Join(strings.Fields(content), " ")
	r := []rune(s)
	if len(r) <= quoteSnippetMaxRunes {
		return s
	}
	return string(r[:quoteSnippetMaxRunes]) + "…"
}

// attachQuotePreviews resolves the 引用 (quote) preview for every agent-facing message
// DTO that carries a quoted_message_id: it injects a `quoted_message` object
// {id, sender_identity_id, content_snippet, is_deleted} so the agent sees WHAT was
// quoted, not just the bare pointer. Missing / deleted target → {id, is_deleted:true}.
// One batch FindByIDs for the whole page (mirrors the UI's attachQuotePreviews). A
// resolution error is non-fatal — the raw quoted_message_id still rides the DTO.
func attachQuotePreviews(ctx context.Context, repo conversation.MessageRepository, dtos []map[string]any) {
	if repo == nil {
		return
	}
	var ids []conversation.MessageID
	seen := make(map[string]struct{})
	for _, mm := range dtos {
		qid, ok := mm["quoted_message_id"].(string)
		if !ok || qid == "" {
			continue
		}
		if _, dup := seen[qid]; dup {
			continue
		}
		seen[qid] = struct{}{}
		ids = append(ids, conversation.MessageID(qid))
	}
	if len(ids) == 0 {
		return
	}
	quoted, err := repo.FindByIDs(ctx, ids)
	if err != nil {
		return // non-fatal: the raw quoted_message_id still rides each DTO.
	}
	byID := make(map[string]*conversation.Message, len(quoted))
	for _, q := range quoted {
		byID[string(q.ID())] = q
	}
	for _, mm := range dtos {
		qid, ok := mm["quoted_message_id"].(string)
		if !ok || qid == "" {
			continue
		}
		if q := byID[qid]; q != nil {
			mm["quoted_message"] = map[string]any{
				"id":                 string(q.ID()),
				"sender_identity_id": string(q.SenderIdentityID()),
				"content_snippet":    quoteSnippet(q.Content()),
				"is_deleted":         false,
			}
		} else {
			mm["quoted_message"] = map[string]any{
				"id":         qid,
				"is_deleted": true,
			}
		}
	}
}

func refsToMap(refs []*conversation.ConversationMessageReference) []map[string]any {
	arr := make([]map[string]any, len(refs))
	for i, ref := range refs {
		arr[i] = map[string]any{
			"id":                     ref.ID,
			"child_conversation_id":  string(ref.ChildConversationID),
			"source_conversation_id": string(ref.SourceConversationID),
			"source_message_id":      string(ref.SourceMessageID),
			"created_by":             string(ref.CreatedBy),
			"created_at":             ref.CreatedAt.Format(time.RFC3339Nano),
		}
	}
	return arr
}
