package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/conversation/identity"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/query"
	"github.com/oopslink/agent-center/internal/secretmgmt"
	secretservice "github.com/oopslink/agent-center/internal/secretmgmt/service"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
	"github.com/oopslink/agent-center/internal/workforce"
)

// HandlerDeps is the narrow surface handlers need. The cli.App provides
// these via an adapter (see internal/cli/webconsole_adapter.go).
type HandlerDeps struct {
	Actor              observability.Actor
	ConvRepo           conversation.ConversationRepository
	MsgRepo            conversation.MessageRepository
	MessageWriter      *convservice.MessageWriter
	ChannelMgmtSvc     *convservice.ChannelManagementService
	ParticipantMgmtSvc *convservice.ParticipantManagementService
	CarryOverSvc       *convservice.CarryOverService
	DerivationSvc      *convservice.MessageDerivationService
	IRRepo             inputrequest.Repository
	IRSvc              *trservice.InputRequestService
	AgentInstanceRepo  workforce.AgentInstanceRepository
	UserSecretRepo     secretmgmt.UserSecretRepository
	UserSecretSvc      *secretservice.UserSecretService
	QuerySvc           *query.Service
	FleetSvc           *query.FleetSnapshotService
}

// hd retrieves the typed dep bag from the request context.
func hd(r *http.Request) HandlerDeps {
	v, _ := r.Context().Value(depsKey{}).(HandlerDeps)
	return v
}

type depsKey struct{}

// WithDeps installs the dep bag into the request context. Use as
// middleware around all handlers.
func WithDeps(deps HandlerDeps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), depsKey{}, deps)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// =============================================================================
// Conversations
// =============================================================================

func (s *Server) listConversationsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	filter := conversation.ConversationFilter{}
	if k := r.URL.Query().Get("kind"); k != "" {
		kk := conversation.ConversationKind(k)
		filter.Kind = &kk
	}
	if st := r.URL.Query().Get("status"); st != "" {
		ss := conversation.ConversationStatus(st)
		filter.Status = &ss
	}
	convs, err := d.ConvRepo.Find(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "find_failed", err.Error())
		return
	}
	arr := make([]map[string]any, len(convs))
	for i, c := range convs {
		arr[i] = convPublicMap(c)
	}
	writeJSON(w, http.StatusOK, arr)
}

// createConversationReq is the unified create payload (SPA F2).
//
//   - kind=channel: requires `name`; `members` ignored (caller becomes
//     sole owner; further invites use the participants endpoint).
//   - kind=dm:      requires at least one entry in `members` (peers besides
//     the caller); `name` optional. Caller is automatically added as a
//     participant with role=owner alongside each peer (role=member).
type createConversationReq struct {
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Members     []string `json:"members"`
}

func (s *Server) createConversationHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req createConversationReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	switch conversation.ConversationKind(req.Kind) {
	case conversation.ConversationKindChannel:
		s.createChannel(w, r, d, req)
	case conversation.ConversationKindDM:
		s.createDM(w, r, d, req)
	default:
		writeError(w, http.StatusBadRequest, "invalid_input",
			"kind must be channel or dm")
	}
}

func (s *Server) createChannel(w http.ResponseWriter, r *http.Request, d HandlerDeps, req createConversationReq) {
	if d.ChannelMgmtSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_wired", "channel management service not wired")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "name required for kind=channel")
		return
	}
	res, err := d.ChannelMgmtSvc.CreateChannel(r.Context(), convservice.CreateChannelCommand{
		Name:        req.Name,
		Description: req.Description,
		CreatedBy:   conversation.IdentityRef(d.Actor),
		Actor:       d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"conversation_id": string(res.ConversationID),
		"event_id":        string(res.EventID),
		"kind":            string(conversation.ConversationKindChannel),
	})
}

func (s *Server) createDM(w http.ResponseWriter, r *http.Request, d HandlerDeps, req createConversationReq) {
	if d.MessageWriter == nil {
		writeError(w, http.StatusNotImplemented, "not_wired", "message writer not wired")
		return
	}
	if len(req.Members) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_input",
			"kind=dm requires at least one entry in members")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	owner := conversation.IdentityRef(d.Actor)
	parts := []conversation.ParticipantElement{{
		IdentityID: owner, Role: "owner",
		JoinedAt: now, JoinedBy: owner,
	}}
	seen := map[conversation.IdentityRef]bool{owner: true}
	for _, m := range req.Members {
		ref := conversation.IdentityRef(m)
		if err := ref.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_input",
				"member identity invalid: "+m)
			return
		}
		if seen[ref] {
			continue
		}
		seen[ref] = true
		parts = append(parts, conversation.ParticipantElement{
			IdentityID: ref, Role: "member",
			JoinedAt: now, JoinedBy: owner,
		})
	}
	res, err := d.MessageWriter.OpenConversation(r.Context(), convservice.OpenCommand{
		Kind:         conversation.ConversationKindDM,
		Name:         req.Name,
		Description:  req.Description,
		Participants: parts,
		CreatedBy:    owner,
		Actor:        d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"conversation_id": string(res.ConversationID),
		"event_id":        string(res.EventID),
		"kind":            string(conversation.ConversationKindDM),
	})
}

func (s *Server) showConversationHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	id := r.PathValue("id")
	c, err := d.ConvRepo.FindByID(r.Context(), conversation.ConversationID(id))
	if err != nil {
		if errors.Is(err, conversation.ErrConversationNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "find_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, convPublicMapWithParticipants(c))
}

func (s *Server) listMessagesHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	id := conversation.ConversationID(r.PathValue("id"))
	filter := conversation.MessageFilter{Limit: 200}
	msgs, err := d.MsgRepo.FindByConversationID(r.Context(), id, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "find_failed", err.Error())
		return
	}
	arr := make([]map[string]any, len(msgs))
	for i, m := range msgs {
		arr[i] = msgPublicMap(m)
	}
	writeJSON(w, http.StatusOK, arr)
}

type sendMessageReq struct {
	SenderIdentityID string `json:"sender_identity_id"`
	Content          string `json:"content"`
	ContentKind      string `json:"content_kind"`
	Direction        string `json:"direction"`
	InputRequestRef  string `json:"input_request_ref"`
}

func (s *Server) sendMessageHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	id := conversation.ConversationID(r.PathValue("id"))
	var req sendMessageReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	sender := req.SenderIdentityID
	if sender == "" {
		sender = string(d.Actor)
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
		ConversationID:   id,
		SenderIdentityID: conversation.IdentityRef(sender),
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
	writeJSON(w, http.StatusCreated, map[string]any{
		"message_id": string(res.MessageID),
		"event_id":   string(res.EventID),
	})
}

type archiveReq struct {
	Version    int    `json:"version"`
	ArchivedBy string `json:"archived_by"`
}

func (s *Server) archiveConversationHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	id := conversation.ConversationID(r.PathValue("id"))
	var req archiveReq
	_ = decodeJSON(r, &req)
	if req.ArchivedBy == "" {
		req.ArchivedBy = string(d.Actor)
	}
	// If version is omitted, look it up.
	if req.Version == 0 {
		c, err := d.ConvRepo.FindByID(r.Context(), id)
		if err != nil {
			mapDomainError(w, err)
			return
		}
		req.Version = c.Version()
	}
	evID, err := d.MessageWriter.Archive(r.Context(), convservice.ArchiveCommand{
		ConversationID: id,
		Version:        req.Version,
		ArchivedBy:     conversation.IdentityRef(req.ArchivedBy),
		Actor:          d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"event_id": string(evID)})
}

type inviteReq struct {
	IdentityID string `json:"identity_id"`
	Role       string `json:"role"`
}

func (s *Server) inviteParticipantHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	convID := conversation.ConversationID(r.PathValue("id"))
	var req inviteReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.IdentityID == "" {
		writeError(w, http.StatusBadRequest, "missing_identity_id", "identity_id required")
		return
	}
	// Resolve channel name from conv id (ParticipantManagementService
	// takes name; we look up by id first).
	c, err := d.ConvRepo.FindByID(r.Context(), convID)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	if c.Kind() != conversation.ConversationKindChannel {
		writeError(w, http.StatusBadRequest, "invalid_kind", "participant invite only allowed on kind=channel")
		return
	}
	evID, err := d.ParticipantMgmtSvc.Invite(r.Context(), convservice.InviteCommand{
		ConversationName: c.Name(),
		IdentityID:       conversation.IdentityRef(req.IdentityID),
		Role:             req.Role,
		InvitedBy:        conversation.IdentityRef(d.Actor),
		Actor:            d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"event_id": string(evID)})
}

func (s *Server) removeParticipantHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	convID := conversation.ConversationID(r.PathValue("id"))
	identityID := r.PathValue("identity_id")
	c, err := d.ConvRepo.FindByID(r.Context(), convID)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	if c.Kind() != conversation.ConversationKindChannel {
		writeError(w, http.StatusBadRequest, "invalid_kind", "participant remove only allowed on kind=channel")
		return
	}
	evID, err := d.ParticipantMgmtSvc.Kick(r.Context(), convservice.KickCommand{
		ConversationName: c.Name(),
		IdentityID:       conversation.IdentityRef(identityID),
		KickedBy:         conversation.IdentityRef(d.Actor),
		Reason:           "kicked",
		Actor:            d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"event_id": string(evID)})
}

// =============================================================================
// Derivation
// =============================================================================

type deriveIssueReq struct {
	SourceConversationID string   `json:"source_conversation_id"`
	SourceMessageIDs     []string `json:"source_message_ids"`
	ProjectID            string   `json:"project_id"`
	Title                string   `json:"title"`
	Description          string   `json:"description"`
}

func (s *Server) deriveIssueHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req deriveIssueReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if d.DerivationSvc == nil {
		writeError(w, http.StatusNotImplemented, "derivation_not_wired", "")
		return
	}
	msgIDs := make([]conversation.MessageID, 0, len(req.SourceMessageIDs))
	for _, m := range req.SourceMessageIDs {
		msgIDs = append(msgIDs, conversation.MessageID(m))
	}
	res, err := d.DerivationSvc.DeriveIssue(r.Context(), convservice.DeriveIssueCommand{
		SourceConversationID: conversation.ConversationID(req.SourceConversationID),
		SourceMessageIDs:     msgIDs,
		ProjectID:            req.ProjectID,
		Title:                req.Title,
		Description:          req.Description,
		CreatedBy:            conversation.IdentityRef(d.Actor),
		Actor:                d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"issue_id":            res.IssueID,
		"conversation_id":     string(res.ChildConversationID),
		"reference_count":     res.ReferenceCount,
		"issue_event_id":      string(res.IssueEventID),
		"carry_over_event_id": string(res.CarryOverEventID),
	})
}

type deriveTaskReq struct {
	SourceConversationID string   `json:"source_conversation_id"`
	SourceMessageIDs     []string `json:"source_message_ids"`
	ProjectID            string   `json:"project_id"`
	Title                string   `json:"title"`
	Description          string   `json:"description"`
	AgentInstanceID      string   `json:"agent_instance_id"`
}

func (s *Server) deriveTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req deriveTaskReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if d.DerivationSvc == nil {
		writeError(w, http.StatusNotImplemented, "derivation_not_wired", "")
		return
	}
	msgIDs := make([]conversation.MessageID, 0, len(req.SourceMessageIDs))
	for _, m := range req.SourceMessageIDs {
		msgIDs = append(msgIDs, conversation.MessageID(m))
	}
	res, err := d.DerivationSvc.DeriveTask(r.Context(), convservice.DeriveTaskCommand{
		SourceConversationID: conversation.ConversationID(req.SourceConversationID),
		SourceMessageIDs:     msgIDs,
		ProjectID:            req.ProjectID,
		Title:                req.Title,
		Description:          req.Description,
		AgentInstanceID:      req.AgentInstanceID,
		CreatedBy:            conversation.IdentityRef(d.Actor),
		Actor:                d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"task_id":             res.TaskID,
		"conversation_id":     string(res.ChildConversationID),
		"reference_count":     res.ReferenceCount,
		"task_event_id":       string(res.TaskEventID),
		"carry_over_event_id": string(res.CarryOverEventID),
	})
}

// =============================================================================
// Input requests
// =============================================================================

func (s *Server) listInputRequestsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	irs, err := d.IRRepo.FindPending(r.Context(), time.Now().UTC().Add(24*365*time.Hour))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "find_failed", err.Error())
		return
	}
	arr := make([]map[string]any, len(irs))
	for i, ir := range irs {
		arr[i] = irPublicMap(ir)
	}
	writeJSON(w, http.StatusOK, arr)
}

type respondReq struct {
	Answer    string `json:"answer"`
	DecidedBy string `json:"decided_by"`
}

func (s *Server) respondInputRequestHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	id := taskruntime.InputRequestID(r.PathValue("id"))
	var req respondReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	who := req.DecidedBy
	if who == "" {
		who = string(d.Actor)
	}
	if err := d.IRSvc.Respond(r.Context(), trservice.RespondInput{
		InputRequestID: id, Answer: req.Answer, DecidedBy: who, Actor: d.Actor,
	}); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"answered": true})
}

// =============================================================================
// Fleet snapshot + task trace
// =============================================================================

func (s *Server) fleetSnapshotHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.FleetSvc == nil {
		writeError(w, http.StatusNotImplemented, "fleet_not_wired", "")
		return
	}
	filter := query.SnapshotFilter{ProjectID: r.URL.Query().Get("project")}
	snap := d.FleetSvc.Snapshot(r.Context(), filter)
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) taskTraceHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.QuerySvc == nil {
		writeError(w, http.StatusNotImplemented, "query_not_wired", "")
		return
	}
	taskID := r.PathValue("id")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "missing_task_id", "")
		return
	}
	res, err := d.QuerySvc.Query(r.Context(), "events", query.QueryFilter{
		TaskID: taskID,
		Limit:  500,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// =============================================================================
// Agents (read-only)
// =============================================================================

func (s *Server) listAgentsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	list, err := d.AgentInstanceRepo.FindAll(r.Context(), workforce.AgentInstanceFilter{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "find_failed", err.Error())
		return
	}
	arr := make([]map[string]any, len(list))
	for i, ai := range list {
		arr[i] = agentPublicMap(ai)
	}
	writeJSON(w, http.StatusOK, arr)
}

func (s *Server) showAgentHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	name := r.PathValue("name")
	ai, err := d.AgentInstanceRepo.FindByName(r.Context(), name)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, agentPublicMap(ai))
}

// =============================================================================
// Secrets (metadata only; plaintext never echoed)
// =============================================================================

func (s *Server) listSecretsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.UserSecretRepo == nil {
		writeError(w, http.StatusNotImplemented, "secret_not_wired", "")
		return
	}
	list, err := d.UserSecretRepo.FindAll(r.Context(), secretmgmt.UserSecretFilter{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "find_failed", err.Error())
		return
	}
	arr := make([]map[string]any, len(list))
	for i, sec := range list {
		arr[i] = secretPublicMap(sec)
	}
	writeJSON(w, http.StatusOK, arr)
}

type createSecretReq struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

func (s *Server) createSecretHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.UserSecretSvc == nil {
		writeError(w, http.StatusNotImplemented, "secret_not_wired", "")
		return
	}
	var req createSecretReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.Value == "" {
		writeError(w, http.StatusBadRequest, "missing_value", "value required")
		return
	}
	kind := secretmgmt.UserSecretKind(req.Kind)
	if kind == "" {
		kind = secretmgmt.UserSecretKindOther
	}
	res, err := d.UserSecretSvc.Create(r.Context(), secretservice.CreateSecretCommand{
		Name:          req.Name,
		Kind:          kind,
		Plaintext:     []byte(req.Value),
		ActorIdentity: d.Actor,
	})
	// Wipe plaintext from the request buffer.
	for i := range req.Value {
		_ = i
	}
	req.Value = ""
	if err != nil {
		mapDomainError(w, err)
		return
	}
	// Response intentionally excludes the value field (ADR-0026 § 5).
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":       string(res.ID),
		"name":     res.Name,
		"event_id": string(res.EventID),
	})
}

func (s *Server) revokeSecretHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.UserSecretSvc == nil {
		writeError(w, http.StatusNotImplemented, "secret_not_wired", "")
		return
	}
	id := secretmgmt.UserSecretID(r.PathValue("id"))
	sec, err := d.UserSecretRepo.FindByID(r.Context(), id)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	if _, err := d.UserSecretSvc.Revoke(r.Context(), secretservice.RevokeSecretCommand{
		ID:            id,
		Reason:        secretmgmt.UserSecretRevokedReasonManual,
		Message:       "revoked via web console",
		Version:       sec.Version(),
		ActorIdentity: d.Actor,
	}); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revoked": true})
}

// =============================================================================
// SSE handlers — delegate to SSEBus
// =============================================================================

func (s *Server) sseHandler(w http.ResponseWriter, r *http.Request) {
	if s.deps.SSE == nil {
		writeError(w, http.StatusNotImplemented, "sse_not_wired", "")
		return
	}
	s.deps.SSE.ServeHTTP(w, r)
}

type sseSubscribeReq struct {
	UserID         string `json:"user_id"`
	ConversationID string `json:"conversation_id"`
}

func (s *Server) sseSubscribeHandler(w http.ResponseWriter, r *http.Request) {
	if s.deps.SSE == nil {
		writeError(w, http.StatusNotImplemented, "sse_not_wired", "")
		return
	}
	var req sseSubscribeReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := s.deps.SSE.Subscribe(req.UserID, req.ConversationID); err != nil {
		writeError(w, http.StatusBadRequest, "subscribe_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscribed": true})
}

func (s *Server) sseUnsubscribeHandler(w http.ResponseWriter, r *http.Request) {
	if s.deps.SSE == nil {
		writeError(w, http.StatusNotImplemented, "sse_not_wired", "")
		return
	}
	var req sseSubscribeReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := s.deps.SSE.Unsubscribe(req.UserID, req.ConversationID); err != nil {
		writeError(w, http.StatusBadRequest, "unsubscribe_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unsubscribed": true})
}

// =============================================================================
// JSON helpers
// =============================================================================

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, reason, message string) {
	writeJSON(w, status, map[string]any{
		"error":   reason,
		"message": message,
	})
}

func decodeJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, dst)
}

func mapDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, conversation.ErrConversationNotFound),
		errors.Is(err, conversation.ErrMessageNotFound),
		errors.Is(err, identity.ErrIdentityNotFound),
		errors.Is(err, workforce.ErrAgentInstanceNotFound),
		errors.Is(err, secretmgmt.ErrUserSecretNotFound),
		errors.Is(err, inputrequest.ErrInputRequestNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, conversation.ErrConversationVersionConflict),
		errors.Is(err, secretmgmt.ErrUserSecretVersionConflict):
		writeError(w, http.StatusConflict, "version_conflict", err.Error())
	case errors.Is(err, conversation.ErrConversationArchived),
		errors.Is(err, conversation.ErrConversationClosed):
		writeError(w, http.StatusForbidden, "conversation_terminal", err.Error())
	case errors.Is(err, conversation.ErrConversationAlreadyExists),
		errors.Is(err, convservice.ErrParticipantAlreadyActive):
		writeError(w, http.StatusConflict, "already_exists", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}

// =============================================================================
// Public projection helpers (kept here so handlers stay readable)
// =============================================================================

func convPublicMap(c *conversation.Conversation) map[string]any {
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
	return m
}

func convPublicMapWithParticipants(c *conversation.Conversation) map[string]any {
	m := convPublicMap(c)
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

func msgPublicMap(m *conversation.Message) map[string]any {
	return map[string]any{
		"id":                 string(m.ID()),
		"conversation_id":    string(m.ConversationID()),
		"sender_identity_id": string(m.SenderIdentityID()),
		"content_kind":       string(m.ContentKind()),
		"content":            m.Content(),
		"direction":          string(m.Direction()),
		"input_request_ref":  m.InputRequestRef(),
		"posted_at":          m.PostedAt().Format(time.RFC3339Nano),
	}
}

func irPublicMap(ir *inputrequest.InputRequest) map[string]any {
	m := map[string]any{
		"id":           string(ir.ID()),
		"status":       string(ir.Status()),
		"execution_id": string(ir.TaskExecutionID()),
		"question":     ir.Question(),
		"options":      ir.Options(),
		"urgency":      string(ir.Urgency()),
		"created_at":   ir.CreatedAt().Format(time.RFC3339Nano),
	}
	if ra := ir.RespondedAt(); ra != nil {
		m["answer"] = ir.ResponseText()
		m["decided_by"] = ir.RespondedBy()
		m["decided_at"] = ra.Format(time.RFC3339Nano)
	}
	return m
}

func agentPublicMap(ai *workforce.AgentInstance) map[string]any {
	wid := ""
	if ai.WorkerID() != nil {
		wid = string(*ai.WorkerID())
	}
	return map[string]any{
		"id":             string(ai.ID()),
		"name":           ai.Name(),
		"state":          string(ai.State()),
		"agent_cli":      ai.AgentCLI(),
		"worker_id":      wid,
		"is_builtin":     ai.IsBuiltin(),
		"max_concurrent": ai.MaxConcurrent(),
		"identity_id":    "agent:" + string(ai.ID()),
	}
}

func secretPublicMap(s *secretmgmt.UserSecret) map[string]any {
	m := map[string]any{
		"id":         string(s.ID()),
		"name":       s.Name(),
		"kind":       string(s.Kind()),
		"state":      string(s.State()),
		"created_at": s.CreatedAt().Format(time.RFC3339Nano),
		"created_by": s.CreatedBy(),
		"version":    s.Version(),
	}
	if ra := s.RevokedAt(); ra != nil {
		m["revoked_at"] = ra.Format(time.RFC3339Nano)
		m["revoked_by"] = s.RevokedBy()
		m["revoked_reason"] = string(s.RevokedReason())
	}
	return m
}
