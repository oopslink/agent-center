package api

// v2.10.0 [T73]: task/issue-scoped file attachments. The generic file transport
// (handlers_files.go) handles blob upload (create→put→complete) + download with
// reverse reachability. This file adds the PLACEMENT surface for task/issue
// scopes so the Task/Issue detail pages can list + attach + download files:
//
//	GET  /api/orgs/{slug}/projects/{pid}/tasks/{tid}/files                          list task files
//	POST /api/orgs/{slug}/projects/{pid}/tasks/{tid}/files                          create upload session (scope=task)
//	POST /api/orgs/{slug}/projects/{pid}/tasks/{tid}/files/transfer/{tid2}/complete finalize + create the task reference
//	(+ the identical issue routes)
//
// The blob PUT reuses the generic PUT /files/transfer/{transfer_id}; download
// reuses GET /files/{ulid} (its fileReachableForHuman already resolves a
// task/issue reference → project membership). Authorization here is fail-closed:
// only a member of the scope's PROJECT may list / attach (resolved task→project /
// issue→project); a missing scope or one not in the {pid} of the route → 404; a
// non-member → 403. Assigned agents already attach via the agent file-tools
// (work-item domain), so this surface is the human/project-member path.

import (
	"net/http"

	"github.com/oopslink/agent-center/internal/files"
	filesservice "github.com/oopslink/agent-center/internal/files/service"
	"github.com/oopslink/agent-center/internal/identity"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// resolveScopeProjectID resolves a {task|issue, scopeID} placement to its owning
// project. Returns found=false (never an error) for a missing/unresolvable
// target — callers treat that as 404, matching refReachableForHuman's
// fail-closed handling.
func (s *Server) resolveScopeProjectID(r *http.Request, d HandlerDeps, scope files.FileScope, scopeID string) (pm.ProjectID, bool) {
	if d.PM == nil {
		return "", false
	}
	switch scope {
	case files.ScopeTask:
		tk, err := d.PM.GetTask(r.Context(), pm.TaskID(scopeID))
		if err != nil || tk == nil {
			return "", false
		}
		return tk.ProjectID(), true
	case files.ScopeIssue:
		is, err := d.PM.GetIssue(r.Context(), pm.IssueID(scopeID))
		if err != nil || is == nil {
			return "", false
		}
		return is.ProjectID(), true
	default:
		return "", false
	}
}

// requireScopeFilesAccess authorizes a task/issue file request fail-closed: the
// caller must be an org member, the scope target must exist AND belong to the
// {pid} in the route, and the caller must be a member of that project. On any
// failure it writes the response and returns ok=false.
func (s *Server) requireScopeFilesAccess(
	w http.ResponseWriter, r *http.Request, d HandlerDeps, scope files.FileScope, scopeID, pid string,
) (*identity.Identity, bool) {
	if d.FilesSvc == nil {
		writeError(w, http.StatusNotImplemented, "files_not_wired", "files service not wired")
		return nil, false
	}
	caller, _, _, ok := requireOrgMember(w, r, d)
	if !ok {
		return nil, false
	}
	projectID, found := s.resolveScopeProjectID(r, d, scope, scopeID)
	// Existence-non-disclosure: a missing target, or one not in the {pid} of the
	// route, is a 404 — never reveals whether the id exists in another project/org.
	if !found || string(projectID) != pid {
		writeError(w, http.StatusNotFound, "not_found", "no such "+string(scope))
		return nil, false
	}
	member, err := s.callerIsProjectMember(r.Context(), d, filesCallerRef(caller), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "membership_failed", err.Error())
		return nil, false
	}
	if !member {
		writeError(w, http.StatusForbidden, "forbidden", "not a member of this project")
		return nil, false
	}
	return caller, true
}

// scopeFileItem is one file row returned by the list endpoint.
type scopeFileItem struct {
	URI       string `json:"uri"`
	Filename  string `json:"filename"`
	MimeType  string `json:"mime_type"`
	Size      int64  `json:"size"`
	CreatedBy string `json:"created_by"`
	CreatedAt string `json:"created_at"`
}

// scopeFilesListHandler lists the LIVE files attached to a task/issue scope.
func (s *Server) scopeFilesListHandler(scope files.FileScope, idParam string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := hd(r)
		scopeID := r.PathValue(idParam)
		if _, ok := s.requireScopeFilesAccess(w, r, d, scope, scopeID, r.PathValue("pid")); !ok {
			return
		}
		refs, err := d.FilesSvc.ListReferencesByScope(r.Context(), scope, scopeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
			return
		}
		out := make([]scopeFileItem, 0, len(refs))
		for _, ref := range refs {
			name := ref.DisplayName
			if name == "" {
				name = ref.Filename
			}
			out = append(out, scopeFileItem{
				URI:       string(ref.FileURI),
				Filename:  name,
				MimeType:  ref.MimeType,
				Size:      ref.SizeBytes,
				CreatedBy: ref.CreatedBy,
				CreatedAt: ref.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"files": out})
	}
}

type createScopeUploadReq struct {
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

// scopeFilesCreateHandler mints an upload session bound to the task/issue scope
// (project-member-gated). The blob is streamed via the generic PUT transfer
// route, then finalized via scopeFilesCompleteHandler.
func (s *Server) scopeFilesCreateHandler(scope files.FileScope, idParam string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := hd(r)
		scopeID := r.PathValue(idParam)
		caller, ok := s.requireScopeFilesAccess(w, r, d, scope, scopeID, r.PathValue("pid"))
		if !ok {
			return
		}
		var req createScopeUploadReq
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		sess, err := d.FilesSvc.CreateUploadSession(r.Context(), filesservice.CreateUploadCmd{
			ContentType: req.ContentType,
			Size:        req.Size,
			Scope:       scope,
			ScopeID:     scopeID,
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
}

type completeScopeUploadReq struct {
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Filename string `json:"filename"`
}

// scopeFilesCompleteHandler finalizes a task/issue upload session and creates the
// task/issue placement reference (so the file lists + downloads). Authorizes the
// session initiator (v2.7 #142) and re-checks project membership.
func (s *Server) scopeFilesCompleteHandler(scope files.FileScope, idParam string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := hd(r)
		scopeID := r.PathValue(idParam)
		caller, ok := s.requireScopeFilesAccess(w, r, d, scope, scopeID, r.PathValue("pid"))
		if !ok {
			return
		}
		var req completeScopeUploadReq
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		transferURI := transferURIPrefix + r.PathValue("transfer_id")
		callerRef := filesCallerRef(caller)
		sess, err := d.FilesSvc.FindSessionByTransferURI(r.Context(), transferURI)
		if err != nil {
			mapFilesError(w, err)
			return
		}
		if sess.CreatedBy() != callerRef {
			writeError(w, http.StatusForbidden, "forbidden", "not the upload session initiator")
			return
		}
		// Defensive: the session must target THIS scope (a session minted for a
		// different scope/id cannot be completed through this route).
		if sess.Scope() != scope || sess.ScopeID() != scopeID {
			writeError(w, http.StatusBadRequest, "scope_mismatch", "session scope does not match the route")
			return
		}
		if err := d.FilesSvc.CompleteUpload(r.Context(), transferURI, req.SHA256, req.Size); err != nil {
			mapFilesError(w, err)
			return
		}
		name := req.Filename
		if name == "" {
			name = "file"
		}
		if _, rerr := d.FilesSvc.AddReference(r.Context(), filesservice.AddReferenceCmd{
			FileURI:     sess.FileURI(),
			Scope:       scope,
			ScopeID:     scopeID,
			Filename:    name,
			MimeType:    sess.ContentType(),
			SizeBytes:   sess.Size(),
			DisplayName: name,
			CreatedBy:   callerRef,
		}); rerr != nil {
			writeError(w, http.StatusInternalServerError, "reference_failed", rerr.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"completed": true,
			"file_uri":  string(sess.FileURI()),
		})
	}
}
