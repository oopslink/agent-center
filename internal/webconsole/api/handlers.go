package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/admintoken"
	admintokensvc "github.com/oopslink/agent-center/internal/admintoken/service"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/conversation/identity"
	"github.com/oopslink/agent-center/internal/discussion"
	disservice "github.com/oopslink/agent-center/internal/discussion/service"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/query"
	"github.com/oopslink/agent-center/internal/secretmgmt"
	secretservice "github.com/oopslink/agent-center/internal/secretmgmt/service"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
	"github.com/oopslink/agent-center/internal/workforce"
	wfservice "github.com/oopslink/agent-center/internal/workforce/service"
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
	ProjectRepo        workforce.ProjectRepository
	QuerySvc           *query.Service
	FleetSvc           *query.FleetSnapshotService
	ReadStateRepo      conversation.UserConversationReadStateRepository
	ReadStateSvc       *convservice.ReadStateService
	// IssueRepo / TaskRepo back the BC-native list + detail
	// endpoints (`GET /api/issues`, `GET /api/issues/{id}`,
	// `GET /api/tasks`, `GET /api/tasks/{id}`). Restores BC ownership:
	// Issue projection lives in Discussion BC and Task projection
	// lives in TaskRuntime BC, so SPA reads no longer require the
	// Conversation BC's `kind=issue|task` filter.
	IssueRepo discussion.IssueRepository
	TaskRepo  task.Repository

	// v2.5.x #61: IssueLifecycleSvc backs the Web Console "Open Issue"
	// + "Conclude" mutation surfaces. Same service the CLI uses; the
	// webconsole calls it directly (no admin transport hop) since both
	// surfaces run in the same process per ADR-0037.
	IssueLifecycleSvc *disservice.IssueLifecycleService

	// v2.5.x #62: TaskSvc backs the Web Console New Task button +
	// TaskDetail lifecycle actions (Suspend / Resume / Abandon). Same
	// service the CLI uses.
	TaskSvc *trservice.TaskService

	// v2.4-D-X1 (@oopslink ask): workers.name editing surface in the
	// Fleet view. WorkerRenameSvc is the workforce service that wraps
	// WorkerRepo.UpdateName + emits workforce.worker.renamed.
	WorkerRenameSvc interface {
		Rename(ctx context.Context, cmd wfservice.RenameCommand) error
	}

	// v2.5-B1: AddWorker creates the Worker AR at mint-enroll time
	// (status=offline) so the Modal can hand the user an install
	// command and close immediately while Fleet shows the new row.
	// Same *WorkerEnrollService instance satisfies both Rename and
	// AddWorker; kept as a separate interface field so server boot
	// can leave AddWorker unwired without losing rename (useful in
	// tests that don't exercise the v2.5 flow).
	WorkerAddSvc interface {
		AddWorker(ctx context.Context, cmd wfservice.AddWorkerCommand) (wfservice.AddWorkerResult, error)
	}

	// v2.5-B4: RemoveWorker drops the Worker AR + emits
	// workforce.worker.removed. The webconsole handler also calls
	// AdminTokenSvc.RevokeAllForWorker BEFORE the drop so the
	// daemon's next admin call 401s — order matters: tokens dead
	// first, row gone second, so a partial failure leaves the user
	// with a still-deletable row instead of an undead worker.
	WorkerRemoveSvc interface {
		RemoveWorker(ctx context.Context, cmd wfservice.RemoveWorkerCommand) (wfservice.RemoveWorkerResult, error)
	}

	// v2.5-B2: WorkerRepo backs the show-install-command lookup —
	// we need the Worker's name to embed in `--worker-name=...`
	// when rebuilding the install line.
	WorkerRepo workforce.WorkerRepository

	// v2.5.3 (#58): Project CRUD UI backs `POST /api/projects` +
	// `PATCH /api/projects/{id}` + `DELETE /api/projects/{id}`
	// + `POST /api/projects/{id}/workers`. ProjectCRUDSvc owns the
	// Project AR write path; MappingRepo lets the handler list
	// existing mappings for the cascade-on-delete check + the
	// Map worker UI.
	ProjectCRUDSvc interface {
		Add(ctx context.Context, cmd wfservice.AddCommand) (wfservice.AddResult, error)
		Update(ctx context.Context, cmd wfservice.UpdateCommand) (wfservice.UpdateResult, error)
		Remove(ctx context.Context, cmd wfservice.RemoveCommand) (observability.EventID, error)
	}
	MappingRepo workforce.WorkerProjectMappingRepository

	// v2.4-D-F3 fix: enroll-token mint endpoint for the Add Worker
	// Modal. AdminTokenSvc is the same service the admin endpoint uses
	// (loopback only — ADR-0037 — so no per-request auth check on
	// this surface). EnrollBootstrapHost + EnrollFingerprint are the
	// values the Modal needs to render the worker install command;
	// both are derived from the admin TCP listener config + cert at
	// server boot.
	AdminTokenSvc      *admintokensvc.Service
	EnrollBootstrapHost string
	EnrollFingerprint   string
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

// listRefsHandler returns the carry-over references that landed into a
// child conversation (CV3). The frontend uses these to draw the
// "from #parent" divider in Issue / Task detail pages.
func (s *Server) listRefsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.CarryOverSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_wired", "carry-over service not wired")
		return
	}
	id := conversation.ConversationID(r.PathValue("id"))
	refs, err := d.CarryOverSvc.FindByChildConv(r.Context(), id)
	if err != nil {
		mapDomainError(w, err)
		return
	}
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
	writeJSON(w, http.StatusOK, arr)
}

// unreadHandler reports {last_seen_message_id, unread_count} for the
// (user, conversation) pair. Powers the v2.1-C unread badge.
func (s *Server) unreadHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ReadStateSvc == nil {
		writeError(w, http.StatusNotImplemented, "read_state_not_wired", "")
		return
	}
	convID := conversation.ConversationID(r.PathValue("id"))
	userID := readStateUserID(r, d)
	if err := userID.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_user_id", err.Error())
		return
	}
	// Resolve the conversation so a missing one returns 404 rather
	// than a silent zero-count answer.
	if _, err := d.ConvRepo.FindByID(r.Context(), convID); err != nil {
		mapDomainError(w, err)
		return
	}
	summary, err := d.ReadStateSvc.Unread(r.Context(), userID, convID)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"conversation_id":      string(convID),
		"user_id":              string(userID),
		"last_seen_message_id": string(summary.LastSeenMessageID),
		"unread_count":         summary.UnreadCount,
	})
}

type markSeenReq struct {
	UserID            string `json:"user_id"`
	LastSeenMessageID string `json:"last_seen_message_id"`
}

// markSeenHandler advances the read cursor. Only-forward; backward
// requests return 200 with `bumped: false` (caller can treat as a
// no-op success).
func (s *Server) markSeenHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ReadStateSvc == nil {
		writeError(w, http.StatusNotImplemented, "read_state_not_wired", "")
		return
	}
	convID := conversation.ConversationID(r.PathValue("id"))
	var req markSeenReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.LastSeenMessageID == "" {
		writeError(w, http.StatusBadRequest, "missing_message_id", "last_seen_message_id required")
		return
	}
	userID := conversation.IdentityRef(req.UserID)
	if userID == "" {
		userID = conversation.IdentityRef(d.Actor)
	}
	if err := userID.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_user_id", err.Error())
		return
	}
	res, err := d.ReadStateSvc.MarkSeen(r.Context(), convservice.MarkSeenCommand{
		UserID:            userID,
		ConversationID:    convID,
		LastSeenMessageID: conversation.MessageID(req.LastSeenMessageID),
		Actor:             d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"last_seen_message_id": string(res.LastSeenMessageID),
		"version":              res.Version,
		"bumped":               res.Bumped,
		"event_id":             string(res.EventID),
	})
}

// readStateUserID picks the user id from the query string and falls
// back to the request actor (single-user v2 case).
func readStateUserID(r *http.Request, d HandlerDeps) conversation.IdentityRef {
	if u := r.URL.Query().Get("user_id"); u != "" {
		return conversation.IdentityRef(u)
	}
	return conversation.IdentityRef(d.Actor)
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
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
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

// postIssueHandler routes POST /api/issues to either derive (with source
// fields) or open-from-scratch (v2.5.x #61 Create Issue Modal). Branching
// keeps the URL stable while letting the SPA pick the flow by payload
// shape; the existing DeriveModal omits nothing, the new CreateIssueModal
// sends only project_id / title / description.
func (s *Server) postIssueHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req deriveIssueReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	// Open-from-scratch path: no source conversation/messages.
	if strings.TrimSpace(req.SourceConversationID) == "" && len(req.SourceMessageIDs) == 0 {
		s.openIssueFromScratch(w, r, d, req)
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

// openIssueFromScratch implements the v2.5.x #61 Create Issue Modal path.
// Wraps IssueLifecycleSvc.Open with OriginWebConsole (sync-build: a sibling
// kind=issue Conversation is created in the same tx, so issue.conversation_id
// is bound immediately).
func (s *Server) openIssueFromScratch(w http.ResponseWriter, r *http.Request, d HandlerDeps, req deriveIssueReq) {
	if d.IssueLifecycleSvc == nil {
		writeError(w, http.StatusNotImplemented, "issue_lifecycle_not_wired", "")
		return
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "project_id required")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "title required")
		return
	}
	res, err := d.IssueLifecycleSvc.Open(r.Context(), disservice.OpenIssueCommand{
		ProjectID:          req.ProjectID,
		Title:              req.Title,
		Description:        req.Description,
		OpenedByIdentityID: string(d.Actor),
		Origin:             discussion.OriginWebConsole,
		Actor:              d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"issue_id":        string(res.IssueID),
		"conversation_id": string(res.ConversationID),
		"event_id":        string(res.EventID),
	})
}

// concludeIssueReq is the POST /api/issues/{id}/conclude body. Tasks is
// only required when kind=closed_with_tasks; otherwise omitted.
type concludeIssueReq struct {
	Kind    string                  `json:"kind"`
	Summary string                  `json:"summary"`
	Tasks   []concludeIssueTaskReq  `json:"tasks,omitempty"`
}

type concludeIssueTaskReq struct {
	LocalID     string `json:"local_id,omitempty"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Priority    string `json:"priority,omitempty"`
}

// concludeIssueHandler serves POST /api/issues/{id}/conclude. Wraps
// IssueLifecycleSvc.Conclude. v2.5.x #61.
func (s *Server) concludeIssueHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IssueLifecycleSvc == nil {
		writeError(w, http.StatusNotImplemented, "issue_lifecycle_not_wired", "")
		return
	}
	id := discussion.IssueID(r.PathValue("id"))
	if strings.TrimSpace(string(id)) == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "id required")
		return
	}
	var req concludeIssueReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	kind := discussion.ResolutionKind(req.Kind)
	if !kind.IsValid() {
		writeError(w, http.StatusBadRequest, "invalid_input",
			"kind must be one of closed_no_action / closed_with_tasks / withdrawn")
		return
	}
	resolution := discussion.Resolution{
		Kind:    kind,
		Summary: req.Summary,
	}
	if kind == discussion.ResolutionClosedWithTasks {
		resolution.Tasks = make([]dispatch.IssueConcludeTaskSpec, 0, len(req.Tasks))
		for i, t := range req.Tasks {
			localID := t.LocalID
			if localID == "" {
				localID = fmt.Sprintf("t%d", i+1)
			}
			resolution.Tasks = append(resolution.Tasks, dispatch.IssueConcludeTaskSpec{
				LocalID:     localID,
				Title:       t.Title,
				Description: t.Description,
				Priority:    task.Priority(t.Priority),
			})
		}
	}
	res, err := d.IssueLifecycleSvc.Conclude(r.Context(), disservice.ConcludeIssueCommand{
		IssueID:     id,
		Resolution:  resolution,
		ConcludedBy: string(d.Actor),
		Actor:       d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	taskIDs := make([]string, len(res.TaskIDs))
	for i, tid := range res.TaskIDs {
		taskIDs[i] = string(tid)
	}
	eventIDs := make([]string, len(res.EventIDs))
	for i, eid := range res.EventIDs {
		eventIDs[i] = string(eid)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issue_id":  string(res.IssueID),
		"task_ids":  taskIDs,
		"event_ids": eventIDs,
	})
}

type deriveTaskReq struct {
	SourceConversationID string   `json:"source_conversation_id"`
	SourceMessageIDs     []string `json:"source_message_ids"`
	ProjectID            string   `json:"project_id"`
	Title                string   `json:"title"`
	Description          string   `json:"description"`
	AgentInstanceID      string   `json:"agent_instance_id"`
	// v2.5.x #62 — fields used only by the open-from-scratch branch.
	ParentTaskID     string `json:"parent_task_id,omitempty"`
	Priority         string `json:"priority,omitempty"`
	RequiresWorktree bool   `json:"requires_worktree,omitempty"`
	WithConversation bool   `json:"with_conversation,omitempty"`
}

// postTaskHandler routes POST /api/tasks to either derive (with source
// fields) or create-from-scratch (v2.5.x #62 Create Task Modal). Same
// branching pattern as postIssueHandler for symmetry.
func (s *Server) postTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req deriveTaskReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.SourceConversationID) == "" && len(req.SourceMessageIDs) == 0 {
		s.createTaskFromScratch(w, r, d, req)
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

// createTaskFromScratch implements the v2.5.x #62 Create Task Modal
// path. Wraps TaskSvc.Create.
func (s *Server) createTaskFromScratch(w http.ResponseWriter, r *http.Request, d HandlerDeps, req deriveTaskReq) {
	if d.TaskSvc == nil {
		writeError(w, http.StatusNotImplemented, "task_service_not_wired", "")
		return
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "project_id required")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "title required")
		return
	}
	in := trservice.TaskCreateInput{
		ProjectID:        req.ProjectID,
		Title:            req.Title,
		Description:      req.Description,
		ParentTaskID:     taskruntime.TaskID(req.ParentTaskID),
		Priority:         task.Priority(req.Priority),
		RequiresWorktree: req.RequiresWorktree,
		WithConversation: req.WithConversation,
		ConversationTitle: req.Title,
		Actor:            d.Actor,
	}
	res, err := d.TaskSvc.Create(r.Context(), in)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"task_id":         string(res.TaskID),
		"conversation_id": string(res.ConversationID),
	})
}

// suspendTaskHandler / resumeTaskHandler / abandonTaskHandler wrap the
// TaskService lifecycle methods. v2.5.x #62.

func (s *Server) suspendTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.TaskSvc == nil {
		writeError(w, http.StatusNotImplemented, "task_service_not_wired", "")
		return
	}
	id := taskruntime.TaskID(r.PathValue("id"))
	if strings.TrimSpace(string(id)) == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "id required")
		return
	}
	evID, err := d.TaskSvc.Suspend(r.Context(), trservice.LifecycleCommand{
		TaskID: id,
		Actor:  d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task_id":  string(id),
		"event_id": string(evID),
	})
}

func (s *Server) resumeTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.TaskSvc == nil {
		writeError(w, http.StatusNotImplemented, "task_service_not_wired", "")
		return
	}
	id := taskruntime.TaskID(r.PathValue("id"))
	if strings.TrimSpace(string(id)) == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "id required")
		return
	}
	evID, err := d.TaskSvc.Resume(r.Context(), trservice.LifecycleCommand{
		TaskID: id,
		Actor:  d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task_id":  string(id),
		"event_id": string(evID),
	})
}

type abandonTaskReq struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

func (s *Server) abandonTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.TaskSvc == nil {
		writeError(w, http.StatusNotImplemented, "task_service_not_wired", "")
		return
	}
	id := taskruntime.TaskID(r.PathValue("id"))
	if strings.TrimSpace(string(id)) == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "id required")
		return
	}
	var req abandonTaskReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	evID, err := d.TaskSvc.Abandon(r.Context(), trservice.AbandonCommand{
		TaskID:  id,
		Reason:  req.Reason,
		Message: req.Message,
		Actor:   d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task_id":  string(id),
		"event_id": string(evID),
	})
}

// updateIssueReq is the PATCH /api/issues/{id} body (v2.5.x #64 Edit).
type updateIssueReq struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// updateIssueHandler serves PATCH /api/issues/{id} — wraps
// IssueLifecycleSvc.UpdateMetadata.
func (s *Server) updateIssueHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IssueLifecycleSvc == nil {
		writeError(w, http.StatusNotImplemented, "issue_lifecycle_not_wired", "")
		return
	}
	id := discussion.IssueID(r.PathValue("id"))
	if strings.TrimSpace(string(id)) == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "id required")
		return
	}
	var req updateIssueReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	evID, err := d.IssueLifecycleSvc.UpdateMetadata(r.Context(), disservice.UpdateMetadataCommand{
		IssueID:     id,
		Title:       req.Title,
		Description: req.Description,
		Actor:       d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issue_id": string(id),
		"event_id": string(evID),
	})
}

// reopenIssueHandler serves POST /api/issues/{id}/reopen — wraps
// IssueLifecycleSvc.Reopen (v2.5.x #64, (c) semantics — spawned tasks
// are NOT cascaded).
func (s *Server) reopenIssueHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IssueLifecycleSvc == nil {
		writeError(w, http.StatusNotImplemented, "issue_lifecycle_not_wired", "")
		return
	}
	id := discussion.IssueID(r.PathValue("id"))
	if strings.TrimSpace(string(id)) == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "id required")
		return
	}
	evID, err := d.IssueLifecycleSvc.Reopen(r.Context(), disservice.ReopenCommand{
		IssueID: id,
		Actor:   d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issue_id": string(id),
		"event_id": string(evID),
	})
}

// updateTaskReq is the PATCH /api/tasks/{id} body (v2.5.x #65 Edit).
type updateTaskReq struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
}

// updateTaskHandler serves PATCH /api/tasks/{id} — wraps
// TaskService.UpdateMetadata for the Web Console Edit modal.
func (s *Server) updateTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.TaskSvc == nil {
		writeError(w, http.StatusNotImplemented, "task_service_not_wired", "")
		return
	}
	id := taskruntime.TaskID(r.PathValue("id"))
	if strings.TrimSpace(string(id)) == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "id required")
		return
	}
	var req updateTaskReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	evID, err := d.TaskSvc.UpdateMetadata(r.Context(), trservice.UpdateMetadataCommand{
		TaskID:      id,
		Title:       req.Title,
		Description: req.Description,
		Priority:    task.Priority(req.Priority),
		Actor:       d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task_id":  string(id),
		"event_id": string(evID),
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

type cancelInputRequestReq struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

func (s *Server) cancelInputRequestHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	id := taskruntime.InputRequestID(r.PathValue("id"))
	var req cancelInputRequestReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.Reason == "" {
		req.Reason = "user_cancel"
	}
	if d.IRSvc == nil {
		writeError(w, http.StatusNotImplemented, "ir_svc_not_wired", "")
		return
	}
	if err := d.IRSvc.Cancel(r.Context(), trservice.CancelInput{
		InputRequestID: id,
		Reason:         req.Reason,
		Message:        req.Message,
		Actor:          d.Actor,
	}); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cancelled": true})
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

// listProjectsHandler returns every project as the full projection
// (id, name, kind, default_agent_cli, description, created_at,
// updated_at). Powers both the v2.1-A DeriveModal project picker AND
// the v2.3-4 /projects list page. Read-only; CRUD verbs go through
// the `agent-center project` CLI subtree (ADR-0029).
func (s *Server) listProjectsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ProjectRepo == nil {
		writeError(w, http.StatusNotImplemented, "project_repo_not_wired", "")
		return
	}
	list, err := d.ProjectRepo.FindAll(r.Context(), workforce.ProjectFilter{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "find_failed", err.Error())
		return
	}
	arr := make([]map[string]any, len(list))
	for i, p := range list {
		arr[i] = projectPublicMap(p)
	}
	writeJSON(w, http.StatusOK, arr)
}

// showProjectHandler returns a single Project projection (404 if not
// found). Powers the v2.3-4 /projects/{id} detail page.
func (s *Server) showProjectHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ProjectRepo == nil {
		writeError(w, http.StatusNotImplemented, "project_repo_not_wired", "")
		return
	}
	id := workforce.ProjectID(r.PathValue("id"))
	p, err := d.ProjectRepo.FindByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, workforce.ErrProjectNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "find_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, projectPublicMap(p))
}

// =============================================================================
// Issues (BC-native read; Discussion BC owns the Issue projection)
// =============================================================================
//
// v2.3-5a per Option C in #agent-center:97f6710d — backend-only ST.
// The SPA's #5b cutover replaces its `GET /api/conversations?kind=issue`
// call with these endpoints; this restores BC ownership (Issue
// projection from Discussion BC, not Conversation BC).
//
// Read-only. Mutations (open / withdraw / conclude / link / bind /
// comment) flow through the CLI / admin server per ADR-0029.

// listIssuesHandler serves `GET /api/issues[?project_id=<id>][&status=<s>]`.
// v2.5.15 (#68): project_id is now OPTIONAL — when omitted the handler
// returns issues across all projects (Discussion BC FindAll). Optional
// `status` filters by Discussion BC's 6-state status enum.
func (s *Server) listIssuesHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IssueRepo == nil {
		writeError(w, http.StatusNotImplemented, "issue_repo_not_wired", "")
		return
	}
	projectID := r.URL.Query().Get("project_id")
	filter := discussion.IssueFilter{}
	if st := r.URL.Query().Get("status"); st != "" {
		ss := discussion.Status(st)
		filter.Status = &ss
	}
	var (
		list []*discussion.Issue
		err  error
	)
	if projectID != "" {
		list, err = d.IssueRepo.FindByProject(r.Context(), projectID, filter)
	} else {
		list, err = d.IssueRepo.FindAll(r.Context(), filter)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "find_failed", err.Error())
		return
	}
	arr := make([]map[string]any, len(list))
	for i, is := range list {
		arr[i] = issuePublicMap(is)
	}
	writeJSON(w, http.StatusOK, arr)
}

// showIssueHandler serves `GET /api/issues/{id}`. 404 on not found.
func (s *Server) showIssueHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IssueRepo == nil {
		writeError(w, http.StatusNotImplemented, "issue_repo_not_wired", "")
		return
	}
	id := discussion.IssueID(r.PathValue("id"))
	is, err := d.IssueRepo.FindByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, discussion.ErrIssueNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "find_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, issuePublicMap(is))
}

// =============================================================================
// Tasks (BC-native read; TaskRuntime BC owns the Task projection)
// =============================================================================
//
// Sibling to the Issues read endpoints above. Coexists with the
// existing `GET /api/tasks/{id}/trace` — net/http's pattern-matcher
// resolves `/{id}/trace` ahead of `/{id}` because the longer pattern
// wins on tie-break (ServeMux specificity rule). Verified by
// TestAPI_TaskTrace + TestAPI_ShowTask_Happy in this package.

// listTasksHandler serves `GET /api/tasks[?project_id=<id>][&status=<s>]`.
// v2.5.15 (#70): project_id is now OPTIONAL — when omitted the handler
// returns tasks across all projects (TaskRuntime BC FindAll). Optional
// `status` filters by Task.Status.
func (s *Server) listTasksHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.TaskRepo == nil {
		writeError(w, http.StatusNotImplemented, "task_repo_not_wired", "")
		return
	}
	projectID := r.URL.Query().Get("project_id")
	filter := task.Filter{}
	if st := r.URL.Query().Get("status"); st != "" {
		ss := task.Status(st)
		filter.Status = &ss
	}
	var (
		list []*task.Task
		err  error
	)
	if projectID != "" {
		list, err = d.TaskRepo.FindByProject(r.Context(), projectID, filter)
	} else {
		list, err = d.TaskRepo.FindAll(r.Context(), filter)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "find_failed", err.Error())
		return
	}
	arr := make([]map[string]any, len(list))
	for i, tk := range list {
		arr[i] = taskPublicMap(tk)
	}
	writeJSON(w, http.StatusOK, arr)
}

// showTaskHandler serves `GET /api/tasks/{id}`. 404 on not found.
func (s *Server) showTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.TaskRepo == nil {
		writeError(w, http.StatusNotImplemented, "task_repo_not_wired", "")
		return
	}
	id := taskruntime.TaskID(r.PathValue("id"))
	tk, err := d.TaskRepo.FindByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, task.ErrTaskNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "find_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, taskPublicMap(tk))
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
		errors.Is(err, conversation.ErrReadStateVersionConflict),
		errors.Is(err, secretmgmt.ErrUserSecretVersionConflict):
		writeError(w, http.StatusConflict, "version_conflict", err.Error())
	case errors.Is(err, conversation.ErrReadStateMessageNotInConversation):
		writeError(w, http.StatusUnprocessableEntity, "message_not_in_conversation", err.Error())
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

// projectPublicMap is the wire-format projection for a workforce
// Project. v2.5.5 simplified the shape to id / name / description /
// tags / version / created_at / updated_at — kind and
// default_agent_cli are gone alongside the type. Tags is always
// emitted (possibly empty) so the SPA can render a stable empty-state
// chip row.
func projectPublicMap(p *workforce.Project) map[string]any {
	tags := p.Tags()
	if tags == nil {
		tags = []string{}
	}
	row := map[string]any{
		"id":          string(p.ID()),
		"name":        p.Name(),
		"description": p.Description(),
		"tags":        tags,
		"version":     p.Version(),
		"created_at":  p.CreatedAt().Format(time.RFC3339Nano),
		"updated_at":  p.UpdatedAt().Format(time.RFC3339Nano),
	}
	return row
}

// issuePublicMap is the wire-format projection for a Discussion BC
// Issue AR — v2.3-5a `GET /api/issues[/{id}]`. Only fields the Issue
// AR exposes via getters are projected (the AR has neither `kind` nor
// `priority` getters in v2; do NOT invent them from the
// `discussion.Origin` enum — that classifies the entry-point, not the
// issue's category). `closed_at` mirrors AR's `concluded_at` (nil
// until conclude/withdraw lands). `closed_reason` is included only
// for the withdrawn terminal — Discussion BC's conclusion paths use
// `conclusion_summary`, which we do not project here to avoid
// confusing the SPA's terminal-banner shape.
func issuePublicMap(i *discussion.Issue) map[string]any {
	m := map[string]any{
		"id":              string(i.ID()),
		"project_id":      i.ProjectID(),
		"conversation_id": string(i.ConversationID()),
		"title":           i.Title(),
		"description":     i.Description(),
		"status":          string(i.Status()),
		"opened_at":       i.OpenedAt().Format(time.RFC3339Nano),
		"opener":          i.OpenedByIdentityID(),
	}
	if ca := i.ConcludedAt(); ca != nil {
		m["closed_at"] = ca.Format(time.RFC3339Nano)
	}
	if reason := i.WithdrawReason(); reason != "" {
		m["closed_reason"] = reason
	}
	return m
}

// taskPublicMap is the wire-format projection for a TaskRuntime BC
// Task AR — v2.3-5a `GET /api/tasks[/{id}]`. Mirrors the Issue
// projection shape (id / project_id / conversation_id / title /
// status / priority / created_at) plus task-only addenda
// (current_execution_id when active, depends_on_task_ids when
// non-empty).
func taskPublicMap(t *task.Task) map[string]any {
	m := map[string]any{
		"id":              string(t.ID()),
		"project_id":      t.ProjectID(),
		"conversation_id": t.ConversationID(),
		"title":           t.Title(),
		"description":     t.Description(),
		"status":          string(t.Status()),
		"priority":        string(t.Priority()),
		"created_at":      t.CreatedAt().Format(time.RFC3339Nano),
	}
	if execID := string(t.CurrentExecutionID()); execID != "" {
		m["current_execution_id"] = execID
	}
	if deps := t.DependsOnTaskIDs(); len(deps) > 0 {
		as := make([]string, len(deps))
		for k, d := range deps {
			as[k] = string(d)
		}
		m["depends_on_task_ids"] = as
	}
	return m
}

// =============================================================================
// AdminToken — enroll-token mint endpoint for the Add Worker Modal.
//
// Loopback-only Web Console per ADR-0037, so no bearer-auth check.
// CLI mode-1 (admin endpoint) keeps the same shape via
// /admin/admintoken/mint-enroll for cross-host / multi-user setups.
// =============================================================================

type mintEnrollReq struct {
	// Name is the operator-facing friendly label for the future worker
	// (v2.4-D-X1 @oopslink). The Modal user types this; daemon embeds
	// it in --worker-name. id is generated server-side and immutable.
	Name string `json:"name"`
}

type mintEnrollResp struct {
	ID            string `json:"id"`
	Token         string `json:"token"`
	ExpiresAt     string `json:"expires_at"`
	Fingerprint   string `json:"fingerprint"`
	BootstrapHost string `json:"bootstrap_host"`
	WorkerID      string `json:"worker_id"`
	WorkerName    string `json:"worker_name"`
}

// generateWorkerID returns a short, human-typeable worker id like
// `worker-7f3a91c2`. 32 bits of entropy is plenty for a single
// operator's lifetime of installs (collision probability < 1e-9 after
// 1000 mints); shorter than a full ULID so the install command fits
// on one screen.
func generateWorkerID() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("worker-%x", b[:]), nil
}

func (s *Server) mintEnrollHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.AdminTokenSvc == nil {
		writeError(w, http.StatusNotImplemented, "admintoken_svc_not_wired",
			"server started without AdminTokenSvc; check ServerCommand wiring")
		return
	}
	if d.EnrollBootstrapHost == "" || d.EnrollFingerprint == "" {
		writeError(w, http.StatusServiceUnavailable, "enroll_not_configured",
			"admin TCP listener not enabled — set server.admin_tcp_listen in config")
		return
	}
	var req mintEnrollReq
	// Body is optional — older clients don't send {name}. decodeJSON
	// tolerates empty body, leaving req.Name = "".
	_ = decodeJSON(r, &req)
	workerName := strings.TrimSpace(req.Name)
	createdBy := ""
	if string(d.Actor) != "" {
		createdBy = string(d.Actor)
	}
	// v2.5-B1: generate worker_id BEFORE the token so we can bind the
	// token to it. The binding lets the show-install-command endpoint
	// (v2.5-B2) look the token up by worker_id and decrypt the
	// stored ciphertext.
	workerID, gerr := generateWorkerID()
	if gerr != nil {
		writeError(w, http.StatusInternalServerError, "gen_worker_id_failed", gerr.Error())
		return
	}
	if workerName == "" {
		workerName = workerID // safe default — Fleet shows id when name absent
	}
	res, err := d.AdminTokenSvc.CreateEnrollToken(r.Context(), admintokensvc.CreateEnrollCommand{
		Owner:     admintoken.Owner("enroll:worker:" + workerID),
		Scopes:    []admintoken.Scope{"workforce:enroll"},
		CreatedBy: createdBy,
		TTL:       30 * time.Minute,
		WorkerID:  workerID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "mint_failed", err.Error())
		return
	}
	tok, ferr := d.AdminTokenSvc.FindByID(r.Context(), res.ID)
	if ferr != nil {
		writeError(w, http.StatusInternalServerError, "mint_lookup_failed", ferr.Error())
		return
	}
	expiresAt := ""
	if exp := tok.ExpiresAt(); exp != nil {
		expiresAt = exp.UTC().Format(time.RFC3339Nano)
	}
	// v2.5-B1: pre-create the Worker AR (status=offline) so Fleet
	// shows the new row immediately; Modal can close right away.
	// If the workforce write fails we revoke the just-minted token
	// so the user doesn't end up with an unbound install command.
	if d.WorkerAddSvc != nil {
		if _, addErr := d.WorkerAddSvc.AddWorker(r.Context(), wfservice.AddWorkerCommand{
			WorkerID:      workforce.WorkerID(workerID),
			Name:          workerName,
			ActorIdentity: d.Actor,
		}); addErr != nil {
			_ = d.AdminTokenSvc.Revoke(r.Context(), admintokensvc.RevokeCommand{
				ID:     res.ID,
				By:     createdBy,
				Reason: "mint-enroll: add-worker failed: " + addErr.Error(),
			})
			writeError(w, http.StatusInternalServerError, "add_worker_failed", addErr.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, mintEnrollResp{
		ID:            string(res.ID),
		Token:         res.Plaintext,
		ExpiresAt:     expiresAt,
		Fingerprint:   d.EnrollFingerprint,
		BootstrapHost: d.EnrollBootstrapHost,
		WorkerID:      workerID,
		WorkerName:    workerName,
	})
}

// revokeEnrollHandler revokes the token whose plaintext sha256 hash
// matches `token_hint` (first 12 chars of the plaintext are accepted
// as a hint — we look up by ID prefix or full plaintext lookup).
//
// In v0 this is best-effort: the frontend already calls it from the
// Modal-close auto-revoke (UI § 9 D2), and silently swallows any
// failure. We accept either ?token_hint=<first-12-of-plaintext> or
// ?id=<token-id>. If neither resolves to a row we return 204 so the
// frontend stays quiet on no-op revokes.
func (s *Server) revokeEnrollHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.AdminTokenSvc == nil {
		writeError(w, http.StatusNotImplemented, "admintoken_svc_not_wired", "")
		return
	}
	q := r.URL.Query()
	id := admintoken.TokenID(q.Get("id"))
	if string(id) == "" {
		// token_hint is currently advisory — we don't index by
		// plaintext prefix. Treat as no-op success so the Modal's
		// fire-and-forget close path doesn't surface noise.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := d.AdminTokenSvc.Revoke(r.Context(), admintokensvc.RevokeCommand{
		ID:     id,
		By:     string(d.Actor),
		Reason: "web-console enroll-modal closed",
	}); err != nil {
		// Already-revoked / not-found → 204 is still acceptable for
		// the close path.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// =============================================================================
// Worker rename — PATCH /api/workers/{id}/name
//
// Loopback-only; no auth gate (matches existing webconsole pattern per
// ADR-0037). Returns 200 with the new {id, name} on success.
// =============================================================================

type workerRenameReq struct {
	Name string `json:"name"`
}

func (s *Server) workerRenameHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.WorkerRenameSvc == nil {
		writeError(w, http.StatusNotImplemented, "worker_rename_svc_not_wired", "")
		return
	}
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	var req workerRenameReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "missing_name", "name must be non-empty")
		return
	}
	if err := d.WorkerRenameSvc.Rename(r.Context(), wfservice.RenameCommand{
		WorkerID: workforce.WorkerID(id),
		Name:     req.Name,
		Actor:    d.Actor,
	}); err != nil {
		if errors.Is(err, workforce.ErrWorkerNotFound) {
			writeError(w, http.StatusNotFound, "worker_not_found", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "rename_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "name": strings.TrimSpace(req.Name)})
}

// =============================================================================
// Show install command — GET /api/workers/{id}/install-command
//
// v2.5-B2 (#50). Re-displays the install command for a worker whose
// enroll token is still alive (not consumed / not expired / not
// revoked). Plaintext is AES-GCM-decrypted from the row on the fly
// (the master_key path lives in admintoken/service). 401 when the
// token has any of those terminal states, OR when the server was
// started without a master_key (no plaintext was ever stored).
//
// Response mirrors mintEnrollResp so the SPA can re-use the same
// command-rendering helper (no logic duplication on the frontend).
// =============================================================================

type showInstallCommandResp struct {
	ID            string `json:"id"`             // admin_token row id
	Token         string `json:"token"`          // decrypted plaintext bearer
	ExpiresAt     string `json:"expires_at"`     // RFC3339Nano UTC
	Fingerprint   string `json:"fingerprint"`    // pinned server cert sha256
	BootstrapHost string `json:"bootstrap_host"` // host:port for tcp://
	WorkerID      string `json:"worker_id"`
	WorkerName    string `json:"worker_name"`
}

func (s *Server) showInstallCommandHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.AdminTokenSvc == nil {
		writeError(w, http.StatusNotImplemented, "admintoken_svc_not_wired",
			"server started without AdminTokenSvc; check ServerCommand wiring")
		return
	}
	if d.EnrollBootstrapHost == "" || d.EnrollFingerprint == "" {
		writeError(w, http.StatusServiceUnavailable, "enroll_not_configured",
			"admin TCP listener not enabled — set server.admin_tcp_listen in config")
		return
	}
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	res, err := d.AdminTokenSvc.ShowInstallToken(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, admintokensvc.ErrShowInstallNoMasterKey):
			writeError(w, http.StatusServiceUnavailable, "show_install_no_master_key",
				"server started without secret_management.master_key_file; install-command "+
					"re-display unavailable. Re-mint a fresh enroll token instead.")
			return
		case errors.Is(err, admintoken.ErrTokenNotFound):
			writeError(w, http.StatusUnauthorized, "no_active_enroll_token",
				"no active enroll token for this worker — token was used, expired, or revoked. "+
					"Re-mint via the Fleet row's 'Re-mint install command' action.")
			return
		case errors.Is(err, admintoken.ErrTokenExpired):
			writeError(w, http.StatusUnauthorized, "enroll_token_expired",
				"enroll token expired (30 min cap) — re-mint via Fleet row action.")
			return
		default:
			writeError(w, http.StatusInternalServerError, "show_install_failed", err.Error())
			return
		}
	}
	workerName := id
	if d.WorkerRepo != nil {
		w2, ferr := d.WorkerRepo.FindByID(r.Context(), workforce.WorkerID(id))
		if ferr == nil {
			workerName = w2.Name()
		}
		// ErrWorkerNotFound is non-fatal: fall back to id as name.
	}
	writeJSON(w, http.StatusOK, showInstallCommandResp{
		ID:            string(res.ID),
		Token:         res.Plaintext,
		ExpiresAt:     res.ExpiresAt.UTC().Format(time.RFC3339Nano),
		Fingerprint:   d.EnrollFingerprint,
		BootstrapHost: d.EnrollBootstrapHost,
		WorkerID:      id,
		WorkerName:    workerName,
	})
}

// =============================================================================
// Re-mint install command — POST /api/workers/{id}/install-command/re-mint
//
// v2.5-B3 (#51). Mints a fresh enroll token bound to an existing
// Worker so the operator can retry an install after the original
// token expired or was burned by an unrelated process. Same response
// shape as show-install-command so the SPA can reuse the renderer.
//
// Preconditions (in priority order):
//   - 503 enroll_not_configured   — admin TCP listener disabled
//   - 404 worker_not_found        — no such worker id
//   - 409 worker_already_online   — daemon already enrolled (long-
//                                    term token exists); re-minting
//                                    would just churn an orphan
//                                    enroll token. Operator should
//                                    remove + re-add (v2.5-B4) if
//                                    they really want to reset.
//   - 200 with fresh install command on success.
//
// Side effects: any prior active enroll token for this worker_id is
// revoked first so the show-install-command lookup returns the new
// one. Best-effort: a revoke failure isn't fatal (the new token is
// what the user needs).
// =============================================================================

func (s *Server) reMintInstallCommandHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.AdminTokenSvc == nil {
		writeError(w, http.StatusNotImplemented, "admintoken_svc_not_wired", "")
		return
	}
	if d.EnrollBootstrapHost == "" || d.EnrollFingerprint == "" {
		writeError(w, http.StatusServiceUnavailable, "enroll_not_configured",
			"admin TCP listener not enabled — set server.admin_tcp_listen in config")
		return
	}
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	// Worker must exist + must not already be enrolled.
	if d.WorkerRepo == nil {
		writeError(w, http.StatusNotImplemented, "worker_repo_not_wired", "")
		return
	}
	worker, werr := d.WorkerRepo.FindByID(r.Context(), workforce.WorkerID(id))
	if werr != nil {
		if errors.Is(werr, workforce.ErrWorkerNotFound) {
			writeError(w, http.StatusNotFound, "worker_not_found",
				"no worker with id "+id+" — add it via the Fleet 'Add Worker' button first.")
			return
		}
		writeError(w, http.StatusInternalServerError, "worker_lookup_failed", werr.Error())
		return
	}
	enrolled, herr := d.AdminTokenSvc.HasLongTermTokenForWorker(r.Context(), id)
	if herr != nil {
		writeError(w, http.StatusInternalServerError, "enroll_check_failed", herr.Error())
		return
	}
	if enrolled {
		writeError(w, http.StatusConflict, "worker_already_online",
			"worker has already enrolled — remove it from the Fleet (Remove action) "+
				"first if you want to re-install.")
		return
	}
	// Tear down any stale enroll token bound to this worker so the
	// show-install-command lookup returns the new one cleanly.
	_ = d.AdminTokenSvc.RevokeActiveEnrollForWorker(r.Context(), id, "re-mint via Web Console")
	createdBy := ""
	if string(d.Actor) != "" {
		createdBy = string(d.Actor)
	}
	res, err := d.AdminTokenSvc.CreateEnrollToken(r.Context(), admintokensvc.CreateEnrollCommand{
		Owner:     admintoken.Owner("enroll:worker:" + id),
		Scopes:    []admintoken.Scope{"workforce:enroll"},
		CreatedBy: createdBy,
		TTL:       30 * time.Minute,
		WorkerID:  id,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "mint_failed", err.Error())
		return
	}
	tok, ferr := d.AdminTokenSvc.FindByID(r.Context(), res.ID)
	if ferr != nil {
		writeError(w, http.StatusInternalServerError, "mint_lookup_failed", ferr.Error())
		return
	}
	expiresAt := ""
	if exp := tok.ExpiresAt(); exp != nil {
		expiresAt = exp.UTC().Format(time.RFC3339Nano)
	}
	writeJSON(w, http.StatusOK, showInstallCommandResp{
		ID:            string(res.ID),
		Token:         res.Plaintext,
		ExpiresAt:     expiresAt,
		Fingerprint:   d.EnrollFingerprint,
		BootstrapHost: d.EnrollBootstrapHost,
		WorkerID:      id,
		WorkerName:    worker.Name(),
	})
}

// =============================================================================
// Remove worker — DELETE /api/workers/{id}
//
// v2.5-B4 (#52). Cross-BC orchestration:
//   1. Revoke all admin tokens bound to the worker FIRST so its
//      daemon (if still running) hits 401 on the next call.
//   2. Drop the Worker AR (emits workforce.worker.removed).
//
// Order matters: revoke-first means a partial failure leaves the
// user with tokens that no longer work but a row they can re-try
// the delete on — better than an orphan worker with live tokens.
//
// Response:
//   - 204 on success.
//   - 404 worker_not_found if the row is gone (re-deletes are 404).
//   - 500 on persistence / event failures (rare; both legs are
//     individually transactional).
// =============================================================================

func (s *Server) removeWorkerHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.WorkerRemoveSvc == nil {
		writeError(w, http.StatusNotImplemented, "worker_remove_svc_not_wired", "")
		return
	}
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	// Revoke first. Non-fatal: if the admin token side errors we
	// keep going — the operator's explicit intent is "this worker is
	// gone", and a dangling Worker row with revoke-failed tokens is
	// still worse than no Worker row.
	if d.AdminTokenSvc != nil {
		if _, rerr := d.AdminTokenSvc.RevokeAllForWorker(r.Context(), id,
			"worker removed via Web Console"); rerr != nil {
			// Log via the writeError path? No — keep server-internal.
			// Move on and let RemoveWorker still try to drop the row.
			_ = rerr
		}
	}
	if _, err := d.WorkerRemoveSvc.RemoveWorker(r.Context(), wfservice.RemoveWorkerCommand{
		WorkerID:      workforce.WorkerID(id),
		ActorIdentity: d.Actor,
		Reason:        "web-console remove worker",
	}); err != nil {
		if errors.Is(err, workforce.ErrWorkerNotFound) {
			writeError(w, http.StatusNotFound, "worker_not_found",
				"no worker with id "+id)
			return
		}
		writeError(w, http.StatusInternalServerError, "remove_worker_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// =============================================================================
// Project CRUD — v2.5.3 (#58)
//
// Web Console parity for the existing `agent-center project` CLI. The
// read endpoints landed in v2.3-4 #30; this section adds create /
// update / delete + worker-mapping CRUD. Default delete refuses when
// the project still has open tasks/issues/mappings; ?force=true
// invalidates the mappings + drops the project anyway.
// =============================================================================

// createProjectReq is the POST /api/projects body. v2.5.5 simplified
// the shape: the id is server-generated, kind / default_agent_cli are
// gone, and tags is a JSON array of free-text strings (UI suggests a
// builtin pool but the server doesn't validate the set).
type createProjectReq struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

func (s *Server) createProjectHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ProjectCRUDSvc == nil {
		writeError(w, http.StatusNotImplemented, "project_crud_not_wired", "")
		return
	}
	var req createProjectReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "missing_field",
			"name is required")
		return
	}
	res, err := d.ProjectCRUDSvc.Add(r.Context(), wfservice.AddCommand{
		Name:        req.Name,
		Description: req.Description,
		Tags:        req.Tags,
		Actor:       d.Actor,
	})
	if err != nil {
		mapWorkforceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projectPublicMap(res.Project))
}

// updateProjectReq is the PATCH /api/projects/{id} body. v2.5.5
// dropped kind / default_agent_cli; mutable surface is now name +
// description + tags.
type updateProjectReq struct {
	Version     int       `json:"version"`
	Name        *string   `json:"name,omitempty"`
	Description *string   `json:"description,omitempty"`
	Tags        *[]string `json:"tags,omitempty"`
}

func (s *Server) updateProjectHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ProjectCRUDSvc == nil {
		writeError(w, http.StatusNotImplemented, "project_crud_not_wired", "")
		return
	}
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	var req updateProjectReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	fields := workforce.ProjectUpdateFields{
		Name:        req.Name,
		Description: req.Description,
		Tags:        req.Tags,
	}
	res, err := d.ProjectCRUDSvc.Update(r.Context(), wfservice.UpdateCommand{
		ID:      workforce.ProjectID(id),
		Version: req.Version,
		Fields:  fields,
		Actor:   d.Actor,
	})
	if err != nil {
		mapWorkforceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projectPublicMap(res.Project))
}

func (s *Server) deleteProjectHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ProjectCRUDSvc == nil {
		writeError(w, http.StatusNotImplemented, "project_crud_not_wired", "")
		return
	}
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	force := r.URL.Query().Get("force") == "true"
	pid := workforce.ProjectID(id)
	// Cascade-decision: count active mappings + open tasks + open
	// issues. With force=false we refuse if any > 0; with force=true
	// we tear down dependents first (mappings only — task/issue
	// cleanup is a future ADR; for now we just delete the Project
	// row and let the orphan tasks/issues surface in their own UI).
	mappingCount := 0
	taskCount := 0
	issueCount := 0
	if d.MappingRepo != nil {
		n, _ := d.MappingRepo.CountActiveByProjectID(r.Context(), pid)
		mappingCount = n
	}
	if d.TaskRepo != nil {
		ts, _ := d.TaskRepo.FindByProject(r.Context(), string(pid), task.Filter{})
		for _, t := range ts {
			if string(t.Status()) == "open" || string(t.Status()) == "suspended" {
				taskCount++
			}
		}
	}
	if d.IssueRepo != nil {
		is, _ := d.IssueRepo.FindByProject(r.Context(), string(pid), discussion.IssueFilter{})
		for _, i := range is {
			if string(i.Status()) != "closed" {
				issueCount++
			}
		}
	}
	if !force && (mappingCount > 0 || taskCount > 0 || issueCount > 0) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":          "project_has_active_work",
			"message":        fmt.Sprintf("Project has %d active mappings, %d open tasks, %d open issues. Use ?force=true to cascade-delete.", mappingCount, taskCount, issueCount),
			"mapping_count":  mappingCount,
			"task_count":     taskCount,
			"issue_count":    issueCount,
		})
		return
	}
	if force && mappingCount > 0 && d.MappingRepo != nil {
		mappings, _ := d.MappingRepo.FindByProjectID(r.Context(), pid)
		for _, m := range mappings {
			if m.Status() != workforce.MappingActive {
				continue
			}
			_ = d.MappingRepo.Invalidate(r.Context(), m.ID(),
				workforce.InvalidateReasonManualRemove,
				"project force-deleted via Web Console",
				time.Now().UTC())
		}
	}
	if _, err := d.ProjectCRUDSvc.Remove(r.Context(), wfservice.RemoveCommand{
		ID:    pid,
		Actor: d.Actor,
	}); err != nil {
		mapWorkforceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type createMappingReq struct {
	WorkerID string `json:"worker_id"`
	Path     string `json:"path"`
}

func (s *Server) listProjectMappingsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.MappingRepo == nil {
		writeError(w, http.StatusNotImplemented, "mapping_repo_not_wired", "")
		return
	}
	id := r.PathValue("id")
	mappings, err := d.MappingRepo.FindByProjectID(r.Context(), workforce.ProjectID(id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(mappings))
	for _, m := range mappings {
		out = append(out, mappingPublicMap(m))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createProjectMappingHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.MappingRepo == nil {
		writeError(w, http.StatusNotImplemented, "mapping_repo_not_wired", "")
		return
	}
	id := r.PathValue("id")
	var req createMappingReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.WorkerID) == "" || strings.TrimSpace(req.Path) == "" {
		writeError(w, http.StatusBadRequest, "missing_field",
			"worker_id and path are required")
		return
	}
	mid := workforce.MappingID("M-" + generateShortID())
	m, err := workforce.NewWorkerProjectMapping(workforce.NewMappingInput{
		ID:        mid,
		WorkerID:  workforce.WorkerID(req.WorkerID),
		ProjectID: workforce.ProjectID(id),
		BasePath:  req.Path,
		AddedAt:   time.Now().UTC(),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_mapping", err.Error())
		return
	}
	if err := d.MappingRepo.Save(r.Context(), m); err != nil {
		writeError(w, http.StatusInternalServerError, "save_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, mappingPublicMap(m))
}

func (s *Server) deleteProjectMappingHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.MappingRepo == nil {
		writeError(w, http.StatusNotImplemented, "mapping_repo_not_wired", "")
		return
	}
	mid := workforce.MappingID(r.PathValue("mapping_id"))
	if err := d.MappingRepo.Invalidate(r.Context(), mid,
		workforce.InvalidateReasonManualRemove,
		"unmapped via Web Console",
		time.Now().UTC()); err != nil {
		mapWorkforceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func mappingPublicMap(m *workforce.WorkerProjectMapping) map[string]any {
	return map[string]any{
		"id":         string(m.ID()),
		"worker_id":  string(m.WorkerID()),
		"project_id": string(m.ProjectID()),
		"path":       m.BasePath(),
		"status":     string(m.Status()),
		"added_at":   m.AddedAt().Format(time.RFC3339Nano),
	}
}

// mapWorkforceError translates known workforce sentinels into HTTP
// status codes. Unknown errors → 500.
func mapWorkforceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workforce.ErrProjectNotFound):
		writeError(w, http.StatusNotFound, "project_not_found", err.Error())
	case errors.Is(err, workforce.ErrProjectAlreadyExists):
		writeError(w, http.StatusConflict, "project_already_exists", err.Error())
	case errors.Is(err, workforce.ErrProjectHasActiveDeps):
		writeError(w, http.StatusConflict, "project_has_active_deps", err.Error())
	case errors.Is(err, workforce.ErrProjectVersionConflict):
		writeError(w, http.StatusConflict, "project_version_conflict", err.Error())
	case errors.Is(err, workforce.ErrMappingNotFound):
		writeError(w, http.StatusNotFound, "mapping_not_found", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "workforce_error", err.Error())
	}
}

// generateShortID returns a short hex id suitable for mapping ids.
// 8 hex chars = 32 bits — plenty for a single operator's lifetime
// of mappings (collision prob < 1e-9 after 1k mappings).
func generateShortID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x", b[:])
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
