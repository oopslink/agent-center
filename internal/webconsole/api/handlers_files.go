package api

// v2.7 D3-d: webconsole HTTP file transport endpoints (human upload/download)
// plus the reverse per-reference download-reachability authorization.
//
// Transport is a 3-call upload flow + a single-call download:
//
//	POST   /api/files                        create upload session  → {file_uri, transfer_uri, transfer_id}
//	PUT    /api/files/transfer/{id}          stream the blob bytes  (write-once)
//	POST   /api/files/transfer/{id}/complete finalize (sha256+size readable)
//	GET    /api/files/{ulid}                 download (reachability-gated)
//
// Upload needs NO reachability (the caller is creating a new blob). Download is
// fail-closed: a human may read a blob only if it has at least one LIVE
// reference in a scope the caller can reach (fileReachableForHuman). The
// admin/agent byte endpoints + agent-domain reachability are POST-D3 (they ship
// with the agent file MCP tools) and are intentionally NOT built here.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/files"
	filesservice "github.com/oopslink/agent-center/internal/files/service"
	"github.com/oopslink/agent-center/internal/identity"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// transferURIPrefix mirrors the (unexported) files.transPrefix. A transfer URI
// is ac://transfers/{sessionID}; the PUT/complete routes carry the bare session
// id in the path and rebuild the full URI here.
const transferURIPrefix = "ac://transfers/"

// filesCallerRef maps an authenticated webconsole identity to the identity-ref
// convention used across the project (user:<id> / agent:<id>). Mirrors
// pmCallerRef so PM membership comparisons line up.
func filesCallerRef(id *identity.Identity) string {
	if id.Kind() == identity.KindAgent {
		return "agent:" + id.ID()
	}
	return "user:" + id.ID()
}

// =============================================================================
// Reverse per-reference download reachability (human)
// =============================================================================

// fileReachableForHuman reports whether the human caller may download the blob
// at fileURI. It is the reverse of the D3-b Reachable primitive: instead of
// asking "is any of MY scopes referenced", it walks the blob's LIVE references
// and, per ref, asks "can this caller reach THIS reference's domain".
//
// Fail-closed: an empty live-ref set, or no live ref whose domain the caller can
// access, denies (returns false). It returns true on the FIRST accessible ref.
//
// Per-scope resolution:
//   - task / issue:  resolve ref → project (PM.GetTask/GetIssue → ProjectID);
//     accessible iff the caller is a member of that project.
//   - project:       accessible iff the caller is a member of ref.ScopeID.
//   - conversation:  must be in the caller's org, then (T244) channel → any org
//     member; plan/task/issue → a LIVE participant OR a member of the conversation's
//     owning project (download mirrors read; never broader); dm → LIVE participant.
//   - agent / tmp:   NOT human-accessible — skipped (no human download grant;
//     these are reachable to agents in POST-D3).
func (s *Server) fileReachableForHuman(ctx context.Context, d HandlerDeps, caller *identity.Identity, orgID string, fileURI files.FileURI) (bool, error) {
	refs, err := d.FilesSvc.ListReferences(ctx, fileURI) // LIVE only (FindByURI filters deleted_at IS NULL)
	if err != nil {
		return false, err
	}
	if len(refs) == 0 {
		return false, nil // fail-closed: no live reference grants a human download
	}
	callerRef := filesCallerRef(caller)
	for _, ref := range refs {
		ok, err := s.refReachableForHuman(ctx, d, callerRef, orgID, ref)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// callerUploaded is the ATTACH-ONLY authorization predicate (v2.7 #142): it
// reports whether callerRef uploaded the blob at fileURI — i.e. a LIVE
// uploader-scope reference exists with ScopeID == callerRef. This is what lets a
// user attach a blob they uploaded to a message (the attach flow verifies it
// BEFORE creating a conversation reference, so a client cannot forge an attachment
// to another org's blob). It deliberately does NOT consult download reachability:
// attach-authz ("you uploaded it") and download-authz ("you are a current member
// of a scope referencing it") are distinct — see refReachableForHuman's uploader
// case. ListReferences returns only live (non-deleted) references.
func (s *Server) callerUploaded(ctx context.Context, d HandlerDeps, callerRef string, fileURI files.FileURI) (bool, error) {
	if d.FilesSvc == nil {
		return false, nil
	}
	refs, err := d.FilesSvc.ListReferences(ctx, fileURI)
	if err != nil {
		return false, err
	}
	for _, ref := range refs {
		if ref.Scope == files.ScopeUploader && ref.ScopeID == callerRef {
			return true, nil
		}
	}
	return false, nil
}

// refReachableForHuman resolves a single live reference's domain accessibility.
func (s *Server) refReachableForHuman(ctx context.Context, d HandlerDeps, callerRef, orgID string, ref files.FileReference) (bool, error) {
	switch ref.Scope {
	case files.ScopeTask:
		if d.PM == nil {
			return false, nil
		}
		tk, err := d.PM.GetTask(ctx, pm.TaskID(ref.ScopeID))
		if err != nil || tk == nil {
			return false, nil // missing/cross-domain target does not grant access
		}
		return s.callerIsProjectMember(ctx, d, callerRef, tk.ProjectID())

	case files.ScopeIssue:
		if d.PM == nil {
			return false, nil
		}
		is, err := d.PM.GetIssue(ctx, pm.IssueID(ref.ScopeID))
		if err != nil || is == nil {
			return false, nil
		}
		return s.callerIsProjectMember(ctx, d, callerRef, is.ProjectID())

	case files.ScopeProject:
		if d.PM == nil {
			return false, nil
		}
		return s.callerIsProjectMember(ctx, d, callerRef, pm.ProjectID(ref.ScopeID))

	case files.ScopeConversation:
		if d.ConvRepo == nil {
			return false, nil
		}
		conv, err := d.ConvRepo.FindByID(ctx, conversation.ConversationID(ref.ScopeID))
		if err != nil || conv == nil {
			return false, nil
		}
		// Must be in the caller's org.
		if conv.OrganizationID() != orgID {
			return false, nil
		}
		// T244: download authz must MIRROR read authz, or a viewer sees an
		// attachment they can't fetch. A CHANNEL is readable by EVERY org member —
		// the channel list + requireConversationInOrg (the message-read gate) don't
		// check participation — so its attachments, INCLUDING ones an agent posted,
		// are downloadable by any org member (download == read; never broader). A DM
		// (and the other participant-private kinds) stays strictly participant-gated:
		// fail-closed, no cross-member leak.
		if conv.Kind() == conversation.ConversationKindChannel {
			return true, nil
		}
		// A live participant always reaches (DM/plan/task/issue alike).
		if conv.HasActiveParticipant(conversation.IdentityRef(callerRef)) {
			return true, nil
		}
		// T244 follow-up (plan-chat 403): a PROJECT-SCOPED conversation
		// (plan/task/issue — owner_ref pm://plans|tasks|issues/...) collaborates a
		// whole project, but its participant set is only the creator + the
		// @mention-dispatched selected-task assignees (PlanParticipantProjector and
		// the task/issue ParticipantProjector are ADDITIVE). A project member who can
		// VIEW the chat but was never @mentioned (e.g. the human owner/PD opening the
		// plan chat) is NOT a participant and used to 403 on an attachment they can
		// plainly see. Align download with the SAME project-membership gate the
		// ScopeTask/ScopeIssue file references above already use, so a
		// conversation-scoped attachment and a task/issue-scoped reference to the
		// same blob behave identically (download == read; never broader). A DM has an
		// empty owner_ref → no project → stays strictly participant-gated.
		return s.convOwnerProjectMember(ctx, d, callerRef, conv.OwnerRef())

	case files.ScopeAgent, files.ScopeTmp, files.ScopeUploader:
		// Not a human DOWNLOAD grant. agent/tmp are agent-domain/transient. Uploader
		// (v2.7 #142) is ATTACH-ONLY reachability: it lets the uploader REFERENCE a
		// blob they uploaded (via callerUploaded, used by the attach flow), but it
		// must NOT grant download — download authz is current-scope-membership
		// (e.g. being a live conversation participant), NOT "I once uploaded this".
		// Conflating them would be a permanent access-leak backdoor (an uploader
		// removed from a conversation would still download via the uploader ref).
		return false, nil

	default:
		return false, nil
	}
}

// Conversation owner_ref schemes for PROJECT-SCOPED conversations (mirrors the
// unexported prefixes in internal/conversation/context_refs.go). A plan/task/issue
// conversation pins to a ProjectManager object; resolving it yields the project
// whose membership gates download (T244 follow-up).
const (
	ownerRefPlansPrefix  = "pm://plans/"
	ownerRefTasksPrefix  = "pm://tasks/"
	ownerRefIssuesPrefix = "pm://issues/"
)

// convOwnerProjectMember reports whether callerRef is a member of the project that
// OWNS a project-scoped conversation (plan/task/issue owner_ref). It is the
// download-reachability bridge that keeps a conversation-scoped attachment as
// reachable as a ScopeTask/ScopeIssue reference to the same project. A DM (empty
// owner_ref), a channel (id://organizations owner_ref — handled by the caller's
// kind switch), or any unknown scheme resolves to no project → false, so the
// caller stays on the strict participant gate (fail-closed).
func (s *Server) convOwnerProjectMember(ctx context.Context, d HandlerDeps, callerRef string, owner conversation.OwnerRef) (bool, error) {
	if d.PM == nil {
		return false, nil
	}
	o := owner.String()
	switch {
	case strings.HasPrefix(o, ownerRefPlansPrefix):
		pl, err := d.PM.GetPlan(ctx, pm.PlanID(strings.TrimPrefix(o, ownerRefPlansPrefix)))
		if err != nil || pl == nil {
			return false, nil // missing/cross-domain target does not grant access
		}
		return s.callerIsProjectMember(ctx, d, callerRef, pl.ProjectID())
	case strings.HasPrefix(o, ownerRefTasksPrefix):
		tk, err := d.PM.GetTask(ctx, pm.TaskID(strings.TrimPrefix(o, ownerRefTasksPrefix)))
		if err != nil || tk == nil {
			return false, nil
		}
		return s.callerIsProjectMember(ctx, d, callerRef, tk.ProjectID())
	case strings.HasPrefix(o, ownerRefIssuesPrefix):
		is, err := d.PM.GetIssue(ctx, pm.IssueID(strings.TrimPrefix(o, ownerRefIssuesPrefix)))
		if err != nil || is == nil {
			return false, nil
		}
		return s.callerIsProjectMember(ctx, d, callerRef, is.ProjectID())
	default:
		return false, nil
	}
}

// callerIsProjectMember reports whether callerRef (user:<id>/agent:<id>) is a
// member of projectID per the PM membership list.
func (s *Server) callerIsProjectMember(ctx context.Context, d HandlerDeps, callerRef string, projectID pm.ProjectID) (bool, error) {
	members, err := d.PM.ListMembers(ctx, projectID)
	if err != nil {
		return false, err
	}
	for _, m := range members {
		if string(m.IdentityID()) == callerRef {
			return true, nil
		}
	}
	return false, nil
}

// =============================================================================
// Upload: create → put → complete
// =============================================================================

type createUploadReq struct {
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	Scope       string `json:"scope"`    // optional
	ScopeID     string `json:"scope_id"` // optional
}

// createUploadHandler mints an upload session (new FileURI + transfer URI) owned
// by the caller. No reachability check — the caller is creating a new blob.
func (s *Server) createUploadHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.FilesSvc == nil {
		writeError(w, http.StatusNotImplemented, "files_not_wired", "files service not wired")
		return
	}
	caller, _, _, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	var req createUploadReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	scope := files.FileScope(req.Scope)
	// v2.7 #142: a client may set any client-settable scope, but NOT the
	// server-internal ScopeUploader (uploader reachability is server-derived at
	// complete from the session initiator, never a client claim).
	if req.Scope != "" && !scope.IsClientSettable() {
		writeError(w, http.StatusBadRequest, "invalid_scope", "unknown file scope")
		return
	}
	sess, err := d.FilesSvc.CreateUploadSession(r.Context(), filesservice.CreateUploadCmd{
		ContentType: req.ContentType,
		Size:        req.Size,
		Scope:       scope,
		ScopeID:     req.ScopeID,
		CreatedBy:   filesCallerRef(caller),
	})
	if err != nil {
		mapFilesError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"file_uri":     string(sess.FileURI()),
		"transfer_uri": sess.TransferURI(),
		"transfer_id":  sess.ID(),
	})
}

// putBlobHandler streams the request body into the blob backing the open upload
// session. Write-once: a second PUT after the bytes exist returns 409.
func (s *Server) putBlobHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.FilesSvc == nil {
		writeError(w, http.StatusNotImplemented, "files_not_wired", "files service not wired")
		return
	}
	if _, _, _, ok := requireOrgMember(w, r, d); !ok {
		return
	}
	transferURI := transferURIPrefix + r.PathValue("transfer_id")
	// ContentLength may be -1 (chunked); WriteBlob/Put treats <0 as "stream".
	if err := d.FilesSvc.WriteBlob(r.Context(), transferURI, r.Body, r.ContentLength); err != nil {
		mapFilesError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"written": true})
}

type completeUploadReq struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// completeUploadHandler finalizes the upload session so the blob is readable.
func (s *Server) completeUploadHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.FilesSvc == nil {
		writeError(w, http.StatusNotImplemented, "files_not_wired", "files service not wired")
		return
	}
	caller, _, _, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	var req completeUploadReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	transferURI := transferURIPrefix + r.PathValue("transfer_id")
	callerRef := filesCallerRef(caller)
	// v2.7 #142: authorize the SESSION INITIATOR before completing. Without this,
	// any org member could complete someone else's upload session and (below) be
	// granted uploader-reachability to a blob they did not upload — a
	// privilege-escalation seam. The session's CreatedBy is the trustworthy
	// uploader identity; reject a non-initiator. (The agent file-tools complete
	// path already checks this; the webconsole path was missing it.)
	sess, err := d.FilesSvc.FindSessionByTransferURI(r.Context(), transferURI)
	if err != nil {
		mapFilesError(w, err)
		return
	}
	if sess.CreatedBy() != callerRef {
		writeError(w, http.StatusForbidden, "forbidden", "not the upload session initiator")
		return
	}
	if err := d.FilesSvc.CompleteUpload(r.Context(), transferURI, req.SHA256, req.Size); err != nil {
		mapFilesError(w, err)
		return
	}
	// v2.7 #142: record an uploader-scope reference so the initiator can REACH the
	// blob they just uploaded (refReachableForHuman ScopeUploader → ScopeID==caller).
	// This is what lets the attach flow verify caller-owns-the-blob before creating
	// a conversation reference. Server-derived (scope_id=callerRef), never client-set.
	if _, rerr := d.FilesSvc.AddReference(r.Context(), filesservice.AddReferenceCmd{
		FileURI:   sess.FileURI(),
		Scope:     files.ScopeUploader,
		ScopeID:   callerRef,
		MimeType:  sess.ContentType(),
		SizeBytes: sess.Size(),
		CreatedBy: callerRef,
	}); rerr != nil {
		writeError(w, http.StatusInternalServerError, "reference_failed", rerr.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"completed": true})
}

// =============================================================================
// Download (reachability-gated)
// =============================================================================

// downloadHandler streams a blob's bytes after the reverse per-reference
// reachability check passes. Fail-closed: 403 when no live reference grants the
// caller a download.
func (s *Server) downloadHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.FilesSvc == nil {
		writeError(w, http.StatusNotImplemented, "files_not_wired", "files service not wired")
		return
	}
	caller, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	fileURI, err := files.NewFileURI(r.PathValue("ulid"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_file_uri", err.Error())
		return
	}
	reachable, err := s.fileReachableForHuman(r.Context(), d, caller, orgID, fileURI)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reachability_failed", err.Error())
		return
	}
	if !reachable {
		// Fail-closed. 403 (not 404) — the URI is well-formed; the caller simply
		// lacks a reachable reference.
		writeError(w, http.StatusForbidden, "forbidden", "no reachable reference grants download")
		return
	}
	// Pick a Content-Type from a live reference's MimeType, if any.
	contentType := contentTypeFromRefs(r.Context(), d, fileURI)
	rc, err := d.FilesSvc.OpenBlob(r.Context(), fileURI)
	if err != nil {
		mapFilesError(w, err)
		return
	}
	defer rc.Close()
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

// contentTypeFromRefs returns the MimeType of the first live reference carrying
// one (best-effort; "" when none). Reachability has already passed, so a repo
// error here is non-fatal — fall back to octet-stream.
func contentTypeFromRefs(ctx context.Context, d HandlerDeps, fileURI files.FileURI) string {
	refs, err := d.FilesSvc.ListReferences(ctx, fileURI)
	if err != nil {
		return ""
	}
	for _, ref := range refs {
		if ref.MimeType != "" {
			return ref.MimeType
		}
	}
	return ""
}

// mapFilesError maps files-service sentinels to HTTP statuses.
func mapFilesError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, filesservice.ErrBlobAlreadyExists):
		writeError(w, http.StatusConflict, "blob_exists", err.Error())
	case errors.Is(err, filesservice.ErrBlobNotFound),
		errors.Is(err, files.ErrTransferSessionNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, filesservice.ErrNotUploadSession),
		errors.Is(err, filesservice.ErrSessionNotOpen):
		writeError(w, http.StatusConflict, "session_state", err.Error())
	case errors.Is(err, files.ErrEmptyURI), errors.Is(err, files.ErrBadScheme), errors.Is(err, files.ErrBadULID):
		writeError(w, http.StatusBadRequest, "invalid_file_uri", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}
