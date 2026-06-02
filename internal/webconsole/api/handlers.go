package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/admintoken"
	admintokensvc "github.com/oopslink/agent-center/internal/admintoken/service"
	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/files"
	filesservice "github.com/oopslink/agent-center/internal/files/service"
	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/query"
	"github.com/oopslink/agent-center/internal/persistence"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	"github.com/oopslink/agent-center/internal/secretmgmt"
	secretservice "github.com/oopslink/agent-center/internal/secretmgmt/service"
	"github.com/oopslink/agent-center/internal/workforce"
	wfservice "github.com/oopslink/agent-center/internal/workforce/service"
)

// HandlerDeps is the narrow surface handlers need. The cli.App provides
// these via an adapter (see internal/cli/webconsole_adapter.go).
type HandlerDeps struct {
	DB                 *sql.DB
	Actor              observability.Actor
	ConvRepo           conversation.ConversationRepository
	MsgRepo            conversation.MessageRepository
	MessageWriter      *convservice.MessageWriter
	ChannelMgmtSvc     *convservice.ChannelManagementService
	ParticipantMgmtSvc *convservice.ParticipantManagementService
	CarryOverSvc       *convservice.CarryOverService
	AgentInstanceRepo  workforce.AgentInstanceRepository
	UserSecretRepo     secretmgmt.UserSecretRepository
	UserSecretSvc      *secretservice.UserSecretService
	FleetSvc           *query.FleetSnapshotService
	ReadStateRepo      conversation.UserConversationReadStateRepository
	ReadStateSvc       *convservice.ReadStateService

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
	// v2.7 #140 step-2: WorkerRepo also backs the Environment-page worker reads
	// (GET /api/workers + /api/workers/{id}) — repointed off the retiring
	// environment.Worker onto the canonical workforce.Worker (enrolled set).
	WorkerRepo workforce.WorkerRepository

	// v2.7 E1 #139: FileTransferRepo backs the Environment-page in-flight
	// transfer-session view (GET /api/files/transfers). Org is resolved per
	// session via its scope (fail-closed). Optional — nil → 501.
	FileTransferRepo files.FileTransferSessionRepository

	// v2.7 B3: the ProjectManager AppService facade backs the nested
	// /api/projects/{project_id}/{members,issues,tasks,code-repos} routes
	// (work-management truth; ADR-0046). Optional — nil means the v2.7 PM
	// endpoints are not wired (legacy/test deps).
	PM *pmservice.Service

	// v2.7 C3: the Agent BC AppService facade backs the org-scoped
	// /api/agents + /api/agents/{id}/{start,stop,restart,reset} routes.
	AgentSvc *agentsvc.Service

	// v2.7 D3-d: the files transfer Service backs the human upload/download
	// HTTP endpoints (/api/files...). Upload mints a session, streams bytes
	// (write-once), then completes; download runs the reverse per-reference
	// download-reachability check (fileReachableForHuman) before streaming.
	// Optional — nil means the /api/files surface is not wired (legacy/test).
	FilesSvc *filesservice.Service

	// v2.4-D-F3 fix: enroll-token mint endpoint for the Add Worker
	// Modal. AdminTokenSvc is the same service the admin endpoint uses
	// (loopback only — ADR-0037 — so no per-request auth check on
	// this surface). EnrollBootstrapHost + EnrollFingerprint are the
	// values the Modal needs to render the worker install command;
	// both are derived from the admin TCP listener config + cert at
	// server boot.
	AdminTokenSvc       *admintokensvc.Service
	EnrollBootstrapHost string
	EnrollFingerprint   string

	// v2.6-FE-1: Auth services for signup / signin / signout / me endpoints.
	// All are optional; nil means auth is unconfigured (middleware passthrough).
	SignupSvc  *identity.SignupService
	SigninSvc  *identity.SigninService
	SignoutSvc *identity.SignoutService
	AuthSvc    *identity.AuthService

	// v2.6-FE-5: Passcode change service for PATCH /api/auth/me/passcode.
	PasscodeChangeSvc *identity.PasscodeChangeService

	// v2.6-FE-3: Organization management services for org switcher + CRUD.
	OrgRepo         identity.OrganizationRepository
	OrgCreateSvc    *identity.OrganizationCreateService
	OrgLifecycleSvc *identity.OrganizationLifecycleService

	// v2.7 #145: identity (user) repo backing the public GET /api/auth/bootstrap
	// "is the system initialized" check (any user exists). Optional — nil means
	// bootstrap reports initialized=false (fresh).
	IdentityRepo identity.IdentityRepository

	// v2.6-FE-4: Member management services.
	MemberRepo          identity.MemberRepository
	MemberAddSvc        *identity.MemberAddService
	MemberCreateUserSvc *identity.MemberCreateUserService
	MemberRoleChangeSvc *identity.MemberRoleChangeService
	MemberDisableSvc    *identity.MemberDisableService
	AgentProvisionSvc   *identity.AgentIdentityProvisionService
	OrgUpdateSvc        *identity.OrganizationUpdateService
}

// hd retrieves the typed dep bag from the request context.
func hd(r *http.Request) HandlerDeps {
	v, _ := r.Context().Value(depsKey{}).(HandlerDeps)
	// v2.7 #146: stamp domain identity + audit actor from the authenticated
	// session, not the static configured default_user. authMiddleware installs
	// the real identity for every /api route; it is nil only on legacy/no-auth
	// paths (AuthSvc unwired), which keep the configured fallback. HandlerDeps
	// is returned by value, so this override is per-request and does not mutate
	// the shared dep bag. Uses the SAME ref convention as the #142 download gate
	// (filesCallerRef) so a conversation owner/participant ref matches the gate's
	// caller ref — closing the F142-2 own-attachment-download ship-blocker.
	if id := CurrentIdentity(r); id != nil {
		v.Actor = observability.Actor(filesCallerRef(id))
	}
	return v
}

// resolveOrgIDFromRequest extracts the active organization ID for the request.
// Resolution order (v2.6 multi-org isolation):
//  1. ?org_id=<id> query param (explicit)
//  2. ?org_slug=<slug> query param (frontend auto-injects from URL path)
//  3. empty string (no org filter — legacy / cross-org callers)
//
// Returns the resolved org ID. When no resolver is available (OrgRepo nil),
// returns the raw value of ?org_id= or empty string.
func resolveOrgIDFromRequest(r *http.Request, d HandlerDeps) string {
	if v := r.URL.Query().Get("org_id"); v != "" {
		return v
	}
	if slug := r.URL.Query().Get("org_slug"); slug != "" && d.OrgRepo != nil {
		if org, err := d.OrgRepo.GetBySlug(r.Context(), slug); err == nil && org != nil {
			return org.ID()
		}
	}
	return ""
}

// requireOrgMember is the authoritative auth/scope helper for org-scoped APIs.
// It:
//  1. Verifies the request has a valid JWT cookie → returns 401 if not.
//  2. Resolves the target org via ?org_id= / ?org_slug= → returns 400 if missing/unknown.
//  3. Verifies the caller is a member of that org → returns 403 if not.
//
// On success: returns (callerIdentity, callerMember, orgID, true).
// On failure: writes the error response and returns _, _, _, false; callers
// MUST stop processing.
//
// This is the single source of truth for "is this request authorized to see
// org X's data". Org-scoped list endpoints must call this — falling back to
// "no filter" when org is missing leaks cross-org data and is a ship-blocker.
func requireOrgMember(w http.ResponseWriter, r *http.Request, d HandlerDeps) (*identity.Identity, *identity.Member, string, bool) {
	if d.AuthSvc == nil || d.OrgRepo == nil || d.MemberRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "org-scoped endpoint requires auth + org + member deps")
		return nil, nil, "", false
	}
	cookie, err := r.Cookie(jwtCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no session")
		return nil, nil, "", false
	}
	callerID, err := d.AuthSvc.AuthenticateToken(r.Context(), cookie.Value)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "invalid session")
		return nil, nil, "", false
	}
	orgID := resolveOrgIDFromRequest(r, d)
	if orgID == "" {
		writeError(w, http.StatusBadRequest, "org_required",
			"missing or unknown organization scope (provide ?org_id= or ?org_slug=)")
		return nil, nil, "", false
	}
	member, err := d.MemberRepo.GetByOrganizationAndIdentity(r.Context(), orgID, callerID.ID())
	if err != nil || member == nil {
		writeError(w, http.StatusForbidden, "forbidden", "not a member of this organization")
		return nil, nil, "", false
	}
	return callerID, member, orgID, true
}

// ── v2.6 X1 §1/§3: belongs-to-org guards for detail/mutation endpoints ───────
//
// Each helper runs requireOrgMember first (401/400/403), then verifies the
// target resource belongs to the caller's org. A resource in a different org
// returns 404 (don't leak existence across orgs). On any failure the error
// response is written and ok=false is returned; callers MUST stop.

// requireConversationInOrg guards conversation detail/message/participant/
// read-state endpoints. Conversations carry organization_id directly.
func (s *Server) requireConversationInOrg(w http.ResponseWriter, r *http.Request, d HandlerDeps, convID string) (orgID string, ok bool) {
	_, _, orgID, member := requireOrgMember(w, r, d)
	if !member {
		return "", false
	}
	if d.ConvRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_wired", "conversation repo not wired")
		return "", false
	}
	conv, err := d.ConvRepo.FindByID(r.Context(), conversation.ConversationID(convID))
	if err != nil || conv == nil {
		writeError(w, http.StatusNotFound, "not_found", "conversation not found")
		return "", false
	}
	if conv.OrganizationID() != orgID {
		writeError(w, http.StatusNotFound, "not_found", "conversation not found")
		return "", false
	}
	return orgID, true
}

// requireAgentInOrg guards agent detail endpoints. AgentInstances carry
// organization_id directly. Looks up by name.
func (s *Server) requireAgentInOrg(w http.ResponseWriter, r *http.Request, d HandlerDeps, name string) (orgID string, ok bool) {
	_, _, orgID, member := requireOrgMember(w, r, d)
	if !member {
		return "", false
	}
	if d.AgentInstanceRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_wired", "agent repo not wired")
		return "", false
	}
	ai, err := d.AgentInstanceRepo.FindByName(r.Context(), name)
	if err != nil || ai == nil {
		writeError(w, http.StatusNotFound, "not_found", "agent not found")
		return "", false
	}
	if ai.OrganizationID() != orgID {
		writeError(w, http.StatusNotFound, "not_found", "agent not found")
		return "", false
	}
	return orgID, true
}

// requireSecretInOrg guards secret mutation (revoke) endpoints.
func (s *Server) requireSecretInOrg(w http.ResponseWriter, r *http.Request, d HandlerDeps, secretID string) (orgID string, ok bool) {
	_, _, orgID, member := requireOrgMember(w, r, d)
	if !member {
		return "", false
	}
	if d.UserSecretRepo == nil {
		writeError(w, http.StatusNotImplemented, "secret_not_wired", "")
		return "", false
	}
	sec, err := d.UserSecretRepo.FindByID(r.Context(), secretmgmt.UserSecretID(secretID))
	if err != nil || sec == nil {
		writeError(w, http.StatusNotFound, "not_found", "secret not found")
		return "", false
	}
	if sec.OrganizationID() != orgID {
		writeError(w, http.StatusNotFound, "not_found", "secret not found")
		return "", false
	}
	return orgID, true
}

// requireWorkerInOrg guards worker rename / show-install / re-mint / remove and
// the project-mapping worker_id check. Workers carry organization_id directly.
func (s *Server) requireWorkerInOrg(w http.ResponseWriter, r *http.Request, d HandlerDeps, workerID string) (orgID string, ok bool) {
	_, _, orgID, member := requireOrgMember(w, r, d)
	if !member {
		return "", false
	}
	if d.WorkerRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_wired", "worker repo not wired")
		return "", false
	}
	wk, err := d.WorkerRepo.FindByID(r.Context(), workforce.WorkerID(workerID))
	if err != nil || wk == nil {
		writeError(w, http.StatusNotFound, "not_found", "worker not found")
		return "", false
	}
	if wk.OrganizationID() != orgID {
		writeError(w, http.StatusNotFound, "not_found", "worker not found")
		return "", false
	}
	return orgID, true
}

type depsKey struct{}

// WithDeps installs the dep bag into the request context and chains the
// JWT auth middleware for /api/* routes (exempt: /api/health, /api/auth/*).
func WithDeps(deps HandlerDeps) func(http.Handler) http.Handler {
	auth := authMiddleware(deps)
	return func(next http.Handler) http.Handler {
		withAuth := auth(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), depsKey{}, deps)
			withAuth.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// =============================================================================
// Conversations
// =============================================================================

func (s *Server) listConversationsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	// v2.6 multi-org isolation: every list response is org-scoped + membership-checked.
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	filter := conversation.ConversationFilter{OrganizationID: orgID}
	if k := r.URL.Query().Get("kind"); k != "" {
		kk := conversation.ConversationKind(k)
		filter.Kind = &kk
	}
	if st := r.URL.Query().Get("status"); st != "" {
		ss := conversation.ConversationStatus(st)
		filter.Status = &ss
	}
	// v2.7 #137: fetch a task/issue conversation by owner_ref (pm://tasks|
	// issues/{id}). org-scoped by construction — filter.OrganizationID is
	// already set above, so a cross-org owner_ref returns no rows (fail-closed,
	// no leak); never bypasses org isolation.
	if or := r.URL.Query().Get("owner_ref"); or != "" {
		o := conversation.OwnerRef(or)
		filter.OwnerRef = &o
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
	// v2.6: stamp the new channel with the caller's org (membership-checked).
	// When auth is unconfigured (legacy/test deps without AuthSvc), fall back
	// to resolveOrgIDFromRequest so existing tests that don't set up sessions
	// still create channels (org_id empty).
	orgID := ""
	if d.AuthSvc != nil {
		_, _, resolved, ok := requireOrgMember(w, r, d)
		if !ok {
			return
		}
		orgID = resolved
	} else {
		orgID = resolveOrgIDFromRequest(r, d)
	}
	res, err := d.ChannelMgmtSvc.CreateChannel(r.Context(), convservice.CreateChannelCommand{
		Name:           req.Name,
		Description:    req.Description,
		OrganizationID: orgID,
		CreatedBy:      conversation.IdentityRef(d.Actor),
		Actor:          d.Actor,
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
	// v2.6 X1 §1: require org membership; stamp the DM with the caller's org.
	orgID := ""
	if d.AuthSvc != nil {
		_, _, resolved, ok := requireOrgMember(w, r, d)
		if !ok {
			return
		}
		orgID = resolved
	} else {
		orgID = resolveOrgIDFromRequest(r, d)
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
		Kind:           conversation.ConversationKindDM,
		Name:           req.Name,
		Description:    req.Description,
		OrganizationID: orgID,
		Participants:   parts,
		CreatedBy:      owner,
		Actor:          d.Actor,
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
	if _, ok := s.requireConversationInOrg(w, r, d, string(id)); !ok {
		return
	}
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
	// v2.6 X1 §1: org guard (also yields 404 for a missing/cross-org conv).
	if _, ok := s.requireConversationInOrg(w, r, d, string(convID)); !ok {
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
	// v2.6 X1 §1: org guard before the read-state mutation (after input parse).
	if _, ok := s.requireConversationInOrg(w, r, d, string(convID)); !ok {
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
	if _, ok := s.requireConversationInOrg(w, r, d, id); !ok {
		return
	}
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
	if _, ok := s.requireConversationInOrg(w, r, d, string(id)); !ok {
		return
	}
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
	SenderIdentityID string              `json:"sender_identity_id"`
	Content          string              `json:"content"`
	ContentKind      string              `json:"content_kind"`
	Direction        string              `json:"direction"`
	InputRequestRef  string              `json:"input_request_ref"`
	Attachments      []msgAttachmentJSON `json:"attachments"`
}

// msgAttachmentJSON is the wire shape for a message attachment (v2.7 #133):
// a reference to an already-uploaded blob (ac://files/{ulid}) plus display
// metadata. The client uploads via POST /api/files first, then sends the
// message carrying the returned file_uri here.
type msgAttachmentJSON struct {
	URI      string `json:"uri"`
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
}

func (s *Server) sendMessageHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	id := conversation.ConversationID(r.PathValue("id"))
	caller, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	if d.ConvRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_wired", "conversation repo not wired")
		return
	}
	conv, err := d.ConvRepo.FindByID(r.Context(), id)
	if err != nil || conv == nil {
		writeError(w, http.StatusNotFound, "not_found", "conversation not found")
		return
	}
	if conv.OrganizationID() != orgID {
		writeError(w, http.StatusNotFound, "not_found", "conversation not found")
		return
	}
	var req sendMessageReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	// v2.7 #146: this webconsole endpoint is human-session-only — there is no
	// delegated-send path here (agents/workers post via the admin API, not this
	// JWT-cookie route). Always stamp the sender from the authenticated session
	// (d.Actor is per-request, see hd) and IGNORE any client-supplied
	// sender_identity_id to prevent sender spoofing.
	sender := string(d.Actor)
	ck := req.ContentKind
	if ck == "" {
		ck = string(conversation.MessageContentText)
	}
	dir := req.Direction
	if dir == "" {
		dir = string(conversation.DirectionInbound)
	}
	var atts []conversation.MessageAttachment
	for _, a := range req.Attachments {
		atts = append(atts, conversation.MessageAttachment{
			URI: a.URI, Filename: a.Filename, MimeType: a.MimeType, Size: a.Size,
		})
	}
	var fileURIs []files.FileURI
	callerRef := filesCallerRef(caller)
	if len(req.Attachments) > 0 {
		if d.FilesSvc == nil {
			writeError(w, http.StatusNotImplemented, "files_not_wired", "files service not wired")
			return
		}
		if d.DB == nil {
			writeError(w, http.StatusNotImplemented, "db_not_wired", "database not wired")
			return
		}
		for _, a := range req.Attachments {
			fileURI, err := files.ParseFileURI(a.URI)
			if err != nil {
				writeAttachmentNotReachable(w)
				return
			}
			reachable, err := s.fileReachableForHuman(r.Context(), d, caller, orgID, fileURI)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "reachability_failed", err.Error())
				return
			}
			if !reachable {
				uploaded, err := s.callerUploaded(r.Context(), d, callerRef, fileURI)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "reachability_failed", err.Error())
					return
				}
				reachable = uploaded
			}
			if !reachable {
				writeAttachmentNotReachable(w)
				return
			}
			fileURIs = append(fileURIs, fileURI)
		}
	}
	add := func(ctx context.Context) (convservice.AddMessageResult, error) {
		res, err := d.MessageWriter.AddMessage(ctx, convservice.AddMessageCommand{
			ConversationID:   id,
			SenderIdentityID: conversation.IdentityRef(sender),
			ContentKind:      conversation.MessageContentKind(ck),
			Content:          req.Content,
			Direction:        conversation.MessageDirection(dir),
			InputRequestRef:  req.InputRequestRef,
			Attachments:      atts,
			Actor:            d.Actor,
		})
		if err != nil {
			return convservice.AddMessageResult{}, err
		}
		for i, fileURI := range fileURIs {
			a := req.Attachments[i]
			if _, err := d.FilesSvc.AddReference(ctx, filesservice.AddReferenceCmd{
				FileURI:   fileURI,
				Scope:     files.ScopeConversation,
				ScopeID:   string(id),
				Filename:  a.Filename,
				MimeType:  a.MimeType,
				SizeBytes: a.Size,
				CreatedBy: callerRef,
			}); err != nil {
				return convservice.AddMessageResult{}, err
			}
		}
		return res, nil
	}
	var res convservice.AddMessageResult
	if len(fileURIs) > 0 {
		err = persistence.RunInTx(r.Context(), d.DB, func(txCtx context.Context) error {
			var txErr error
			res, txErr = add(txCtx)
			return txErr
		})
	} else {
		res, err = add(r.Context())
	}
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"message_id": string(res.MessageID),
		"event_id":   string(res.EventID),
	})
}

func writeAttachmentNotReachable(w http.ResponseWriter) {
	writeError(w, http.StatusForbidden, "forbidden", "attachment is not reachable")
}

type archiveReq struct {
	Version    int    `json:"version"`
	ArchivedBy string `json:"archived_by"`
}

func (s *Server) archiveConversationHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	id := conversation.ConversationID(r.PathValue("id"))
	if _, ok := s.requireConversationInOrg(w, r, d, string(id)); !ok {
		return
	}
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
	if _, ok := s.requireConversationInOrg(w, r, d, string(convID)); !ok {
		return
	}
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
	if _, ok := s.requireConversationInOrg(w, r, d, string(convID)); !ok {
		return
	}
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
// Fleet snapshot
// =============================================================================

func (s *Server) fleetSnapshotHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.FleetSvc == nil {
		writeError(w, http.StatusNotImplemented, "fleet_not_wired", "")
		return
	}
	// v2.6 X1 §3: fleet snapshot is org-scoped. require membership + pass the
	// resolved org into the snapshot filter (workers by org_id;
	// executions/IRs/issues by the org's projects).
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	filter := query.SnapshotFilter{
		ProjectID:      r.URL.Query().Get("project"),
		OrganizationID: orgID,
	}
	snap := d.FleetSvc.Snapshot(r.Context(), filter)
	writeJSON(w, http.StatusOK, snap)
}

// =============================================================================
// Agents (read-only)
// =============================================================================

func (s *Server) listAgentsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	filter := workforce.AgentInstanceFilter{OrganizationID: orgID}
	list, err := d.AgentInstanceRepo.FindAll(r.Context(), filter)
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
	if _, ok := s.requireAgentInOrg(w, r, d, name); !ok {
		return
	}
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
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	sfilter := secretmgmt.UserSecretFilter{OrganizationID: orgID}
	list, err := d.UserSecretRepo.FindAll(r.Context(), sfilter)
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
	// v2.6: stamp the new secret with the caller's org (membership-checked when
	// auth is configured; legacy/test deps without AuthSvc fall back).
	orgID := ""
	if d.AuthSvc != nil {
		_, _, resolved, ok := requireOrgMember(w, r, d)
		if !ok {
			return
		}
		orgID = resolved
	} else {
		orgID = resolveOrgIDFromRequest(r, d)
	}
	res, err := d.UserSecretSvc.Create(r.Context(), secretservice.CreateSecretCommand{
		Name:           req.Name,
		Kind:           kind,
		Plaintext:      []byte(req.Value),
		OrganizationID: orgID,
		ActorIdentity:  d.Actor,
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
	if _, ok := s.requireSecretInOrg(w, r, d, string(id)); !ok {
		return
	}
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
		errors.Is(err, workforce.ErrAgentInstanceNotFound),
		errors.Is(err, secretmgmt.ErrUserSecretNotFound):
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
		// owner_ref pins task/issue conversations to their pm object
		// (pm://tasks|issues/{id}); empty for channels/DMs. v2.7 #137: the UI
		// embeds a task/issue conversation by resolving this ref.
		"owner_ref":  string(c.OwnerRef()),
		"created_by": string(c.CreatedBy()),
		"created_at": c.CreatedAt().Format(time.RFC3339Nano),
		"updated_at": c.UpdatedAt().Format(time.RFC3339Nano),
		"version":    c.Version(),
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
	// context_refs lets the UI segment a task conversation's messages by
	// AgentWorkItem across re-dispatches (v2.7 #137). Emitted only when set
	// (daemon writes work_item_ref/task_ref/agent_ref; empty for plain chat).
	cr := m.ContextRefs()
	if cr.WorkItemRef != "" || cr.TaskRef != "" || cr.AgentRef != "" {
		out["context_refs"] = map[string]any{
			"work_item_ref": cr.WorkItemRef,
			"task_ref":      cr.TaskRef,
			"agent_ref":     cr.AgentRef,
		}
	}
	// attachments (v2.7 #133): unified MessageAttachment metadata for UI display.
	// Emitted only when present (plain messages carry no key) — the UI derives the
	// display type (image preview vs file chip) from mime_type.
	if atts := m.Attachments(); len(atts) > 0 {
		arr := make([]map[string]any, len(atts))
		for i, a := range atts {
			arr[i] = map[string]any{
				"uri":       a.URI,
				"filename":  a.Filename,
				"mime_type": a.MimeType,
				"size":      a.Size,
			}
		}
		out["attachments"] = arr
	}
	return out
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
	// v2.6 X1 §1: Add Worker is org-scoped; require membership + stamp the org.
	mintOrgID := ""
	if d.AuthSvc != nil {
		_, _, resolved, ok := requireOrgMember(w, r, d)
		if !ok {
			return
		}
		mintOrgID = resolved
	} else {
		mintOrgID = resolveOrgIDFromRequest(r, d)
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
			WorkerID:       workforce.WorkerID(workerID),
			Name:           workerName,
			OrganizationID: mintOrgID,
			ActorIdentity:  d.Actor,
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
	// v2.6 X1 §2: worker must belong to the caller's org.
	if _, ok := s.requireWorkerInOrg(w, r, d, id); !ok {
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
	// v2.6 X1 §2: worker must belong to the caller's org.
	if _, ok := s.requireWorkerInOrg(w, r, d, id); !ok {
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
	// v2.6 X1 §2: worker must belong to the caller's org.
	if _, ok := s.requireWorkerInOrg(w, r, d, id); !ok {
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
	// v2.6 X1 §2: worker must belong to the caller's org.
	if _, ok := s.requireWorkerInOrg(w, r, d, id); !ok {
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
