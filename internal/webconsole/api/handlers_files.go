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
//   - conversation:  accessible iff the caller is a LIVE participant of the
//     conversation AND the conversation is in the caller's org.
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
		// Must be in the caller's org AND the caller must be a live participant.
		if conv.OrganizationID() != orgID {
			return false, nil
		}
		return conv.HasActiveParticipant(conversation.IdentityRef(callerRef)), nil

	case files.ScopeUploader:
		// v2.7 #142: an uploader-scope reference grants reachability to EXACTLY the
		// identity that uploaded the blob (ScopeID = the uploader's identity ref),
		// and only that identity — per-USER, not per-org (a same-org different user
		// does NOT reach it). This is how a user reaches a blob they just uploaded
		// (before any conversation reference exists), so the attach flow can verify
		// caller-owns-the-blob before creating a conversation reference.
		return ref.ScopeID == callerRef, nil

	case files.ScopeAgent, files.ScopeTmp:
		// Not human-accessible: an agent-domain or tmp placement never grants a
		// human download.
		return false, nil

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
	if req.Scope != "" && !scope.IsValid() {
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
	if _, _, _, ok := requireOrgMember(w, r, d); !ok {
		return
	}
	var req completeUploadReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	transferURI := transferURIPrefix + r.PathValue("transfer_id")
	if err := d.FilesSvc.CompleteUpload(r.Context(), transferURI, req.SHA256, req.Size); err != nil {
		mapFilesError(w, err)
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
