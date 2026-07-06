package api

// v2.7 post-D3 — agent MCP file tools (server contract, task #104). Admin HTTP
// endpoints that let an agent (via its Worker daemon) upload/download/attach
// files, gated by per-agent reachability over the agent's OWN enumerable domain.
//
// These are the agent-side analog of D3-d's HUMAN file transport
// (webconsole/api/handlers_files.go): the byte mechanics
// (CreateUploadSession/WriteBlob/CompleteUpload/OpenBlob) are identical; what
// differs is the authorization model. A human reaches a blob through org/project
// membership + conversation participation; an agent reaches a blob through the
// scopes it can ENUMERATE for itself:
//
//	upload_file              POST /admin/agent-tools/upload_file
//	put bytes                PUT  /admin/files/transfer/{transfer_id}?agent_id=
//	complete                 POST /admin/files/transfer/{transfer_id}/complete
//	download                 GET  /admin/files/{ulid}?agent_id=
//	attach_file              POST /admin/agent-tools/attach_file
//
// Every endpoint runs behind requireAgentOnWorker (the b1 guardrail: worker
// proven by the TOKEN OWNER, target agent bound to it). Upload/complete/attach
// that name a {scope, scope_id} are AUTHZ-FIRST: the scope must be in the agent's
// own-domain (agentOwnDomainScopes) or the call is rejected 403 before any blob
// or reference write. Download is FAIL-CLOSED: a blob is readable only if it has
// at least one LIVE reference in a scope the agent can enumerate.
//
// The daemon-side byte-mover + path containment is a SEPARATE follow-up slice —
// NOT built here.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/files"
	filesservice "github.com/oopslink/agent-center/internal/files/service"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// transferURIPrefix mirrors the (unexported) files.transPrefix. A transfer URI
// is ac://transfers/{sessionID}; the PUT/complete routes carry the bare session
// id in the path and rebuild the full URI here. (Parallels the webconsole
// transport constant; intentionally duplicated to keep the two transports
// independent.)
const transferURIPrefix = "ac://transfers/"

// contentTypeFromRefs returns the MimeType of the first live reference carrying
// one (best-effort; "" when none). Reachability has already passed, so a repo
// error here is non-fatal — the caller falls back to octet-stream.
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

// mapFilesError maps files-service sentinels to HTTP statuses (parallels the
// webconsole transport's mapper; duplicated to keep transports independent).
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

// =============================================================================
// The unified own-domain helper (used by BOTH attach/upload authz AND download
// reachability — symmetric, per the PM).
// =============================================================================

// agentOwnDomainScopes returns every FileReference scope the agent may read or
// write — the agent's enumerable domain. It is the agent-side analog of the
// human's "org/project member + conversation participant" scope set, but is
// computed strictly from the agent's OWN work:
//
//   - {ScopeAgent, agentID}                always (the agent's private scope)
//   - per ASSIGNED task (v2.14.0 F7 issue I14 — AgentWorkItem retired; the agent's
//     own work is now expressed via Task.Assignee): PMService.ListRunnableAgentTasks
//     lists the agent's open/running assigned tasks → for each, {ScopeTask, taskID};
//     if DerivedFromIssue non-empty → {ScopeIssue, issueID}; resolve the task's
//     Conversation (ConvRepo.FindByOwnerRef(pm://tasks/{taskID})) → if found →
//     {ScopeConversation, convID}.
//   - per participant channel/DM (T44): every channel/DM in the agent's OWN org
//     where it is an ACTIVE participant → {ScopeConversation, convID}. This is the
//     direct realization of the doc's "conversation participant" analog: it lets an
//     agent place a file into (and read a file from) a channel/DM it is in, the
//     dual of the human chat-box attachment. Org-scoped by construction (Find is
//     a.OrganizationID()-filtered) so a cross-org conversation never appears — a
//     non-participant agent simply has no such scope (§5.7 fail-closed).
//   - per PROJECT the agent is a MEMBER of (agentProjectMemberConvScopes): the
//     task/issue/plan work of that project → {ScopeTask}/{ScopeIssue} and their
//     bound {ScopeConversation}. This mirrors the post_message project-member gate
//     (requireTaskAccess / GetIssueForMember) in the FILE domain, so an agent
//     @mentioned in a task/issue/plan conversation of a project it belongs to — but
//     holding no assigned work-item and not a formal participant — can DOWNLOAD an
//     attachment there, not just reply. Org-scoped + membership-gated (fail-closed).
//
// The result is deduped. Per-task lookup errors are TOLERATED (skip that task's
// derived scopes) — fail-closed: a scope we cannot resolve simply does not
// appear, so it is unreachable rather than wrongly granted. When PMService is
// unwired the per-task scopes are skipped (only agent + participant-conv scopes).
func (s *Server) agentOwnDomainScopes(d HandlerDeps, r *http.Request, a *agent.Agent) ([]filesservice.ScopeRef, error) {
	ctx := r.Context()
	scopes := []filesservice.ScopeRef{{Scope: files.ScopeAgent, ScopeID: string(a.ID())}}

	if d.PMService == nil {
		return append(scopes, s.agentParticipantConvScopes(d, r, a)...), nil
	}
	tasks, err := d.PMService.ListRunnableAgentTasks(ctx, pm.IdentityRef(agentActor(a)))
	if err != nil {
		return nil, err
	}
	for _, tk := range tasks {
		taskID := string(tk.ID())
		scopes = append(scopes, filesservice.ScopeRef{Scope: files.ScopeTask, ScopeID: taskID})

		// Derived issue + bound conversation are best-effort: a missing target
		// just omits that derived scope (fail-closed).
		if iss := string(tk.DerivedFromIssue()); iss != "" {
			scopes = append(scopes, filesservice.ScopeRef{Scope: files.ScopeIssue, ScopeID: iss})
		}
		if d.ConvRepo != nil {
			if conv, cerr := d.ConvRepo.FindByOwnerRef(ctx, conversation.NewTaskOwnerRef(taskID)); cerr == nil && conv != nil {
				scopes = append(scopes, filesservice.ScopeRef{Scope: files.ScopeConversation, ScopeID: string(conv.ID())})
			}
		}
	}
	scopes = append(scopes, s.agentParticipantConvScopes(d, r, a)...)
	scopes = append(scopes, s.agentProjectMemberConvScopes(d, r, a)...)
	return dedupScopeRefs(scopes), nil
}

// agentProjectMemberConvScopes returns the file-domain scopes for the task/issue/
// plan work of every project the agent is a MEMBER of — the file-domain
// realization of the post_message PROJECT-MEMBER gate (requireTaskAccess /
// GetIssueForMember).
//
// THE GAP THIS CLOSES: an agent @mentioned in a TASK conversation whose owning
// project it is a member of — but that holds NO assigned work-item and is not a
// formal active participant — could post_message a reply (requireTaskAccess admits
// the project member, T183) yet got 403 file_not_reachable when it tried to
// download an attachment posted in that same conversation. agentOwnDomainScopes
// only enumerated the file-scopes of the agent's ASSIGNED (runnable) tasks and its
// active-participant conversations, so a member-but-not-assignee/participant had no
// {ScopeConversation, taskConvID} — the download was narrower than the reply.
// v2.7.1 #227 (issue @mention → project-member auto-join) and v2.9 #306 (plan
// @mention → project-member broaden) already broadened the WAKE path at the
// project-member boundary; TASK/ISSUE/PLAN conversations lacked the equivalent
// FILE-domain broadening. This adds it at the SAME project-member boundary
// post_message enforces — no authz widening.
//
// For each project in the agent's OWN org where the agent is a member it grants:
//   - per task:  {ScopeTask, taskID}; derived {ScopeIssue, issueID} when set; and
//     the task's bound {ScopeConversation, convID}.
//   - per issue: {ScopeIssue, issueID}; and the issue's bound {ScopeConversation}.
//   - per plan:  the plan's bound {ScopeConversation, convID}.
//
// Fail-closed by construction: ListProjects is org-filtered (a cross-org project
// never appears) and each project is admitted only after an explicit membership
// match, so a non-member — of another project or another org — gains nothing. A
// nil PMService/ConvRepo, an empty org, or any per-list error yields NO scopes
// (denied, never wrongly granted). The result is deduped with the rest of the
// own-domain set by the caller. Never grants {ScopeProject} (a project-scoped ref
// stays unreachable, preserving the existing project-scope denial).
func (s *Server) agentProjectMemberConvScopes(d HandlerDeps, r *http.Request, a *agent.Agent) []filesservice.ScopeRef {
	if d.PMService == nil || d.ConvRepo == nil {
		return nil
	}
	orgID := a.OrganizationID()
	if orgID == "" {
		return nil
	}
	ctx := r.Context()
	self := agentActor(a)
	projects, err := d.PMService.ListProjects(ctx, orgID)
	if err != nil {
		return nil // fail-closed: cannot enumerate projects → grant nothing
	}
	var out []filesservice.ScopeRef
	for _, p := range projects {
		members, merr := d.PMService.ListMembers(ctx, p.ID())
		if merr != nil {
			continue // fail-closed: skip a project we cannot resolve membership for
		}
		isMember := false
		for _, m := range members {
			if string(m.IdentityID()) == self {
				isMember = true
				break
			}
		}
		if !isMember {
			continue // not a member → no scopes from this project (fail-closed)
		}
		// Tasks: {ScopeTask} + derived {ScopeIssue} + bound conversation.
		if tasks, terr := d.PMService.ListTasks(ctx, p.ID()); terr == nil {
			for _, tk := range tasks {
				taskID := string(tk.ID())
				out = append(out, filesservice.ScopeRef{Scope: files.ScopeTask, ScopeID: taskID})
				if iss := string(tk.DerivedFromIssue()); iss != "" {
					out = append(out, filesservice.ScopeRef{Scope: files.ScopeIssue, ScopeID: iss})
				}
				if conv, cerr := d.ConvRepo.FindByOwnerRef(ctx, conversation.NewTaskOwnerRef(taskID)); cerr == nil && conv != nil {
					out = append(out, filesservice.ScopeRef{Scope: files.ScopeConversation, ScopeID: string(conv.ID())})
				}
			}
		}
		// Issues: {ScopeIssue} + bound conversation.
		if issues, ierr := d.PMService.ListIssues(ctx, p.ID()); ierr == nil {
			for _, is := range issues {
				issueID := string(is.ID())
				out = append(out, filesservice.ScopeRef{Scope: files.ScopeIssue, ScopeID: issueID})
				if conv, cerr := d.ConvRepo.FindByOwnerRef(ctx, conversation.NewIssueOwnerRef(issueID)); cerr == nil && conv != nil {
					out = append(out, filesservice.ScopeRef{Scope: files.ScopeConversation, ScopeID: string(conv.ID())})
				}
			}
		}
		// Plans: bound conversation (no plan file-scope kind — reached via its conv).
		if plans, perr := d.PMService.ListPlans(ctx, p.ID()); perr == nil {
			for _, pl := range plans {
				if conv, cerr := d.ConvRepo.FindByOwnerRef(ctx, conversation.NewPlanOwnerRef(string(pl.ID()))); cerr == nil && conv != nil {
					out = append(out, filesservice.ScopeRef{Scope: files.ScopeConversation, ScopeID: string(conv.ID())})
				}
			}
		}
	}
	return out
}

// agentParticipantConvScopes returns a {ScopeConversation, convID} scope for every
// conversation in the agent's OWN org where the agent is an ACTIVE (non-left)
// participant — the file-domain realization of the post_message participant gate
// (agentIsActiveParticipant). It is best-effort and fail-closed: a missing ConvRepo,
// an empty org, or a Find error yields NO conversation scopes (the agent is denied,
// never wrongly granted).
//
// T204: ALL FIVE conversation kinds (channel, DM, task, issue, plan) are enumerated
// here by participation, so an agent can attach to — and read attachments from —
// ANY conversation it actively participates in, uniformly. Previously only
// channel/DM/plan were scanned: task/issue were assumed "reached via the work-item
// derivation in agentOwnDomainScopes", but that derivation only adds the *task*
// conversation scope (and the issue *scope*, not the issue *conversation* scope) and
// only for an agent that HOLDS a work-item — so an issue-conversation participant
// without a work-item (e.g. the PD discussing on an issue) had no
// {ScopeConversation, issueConvID} and hit scope_not_in_agent_domain on attach.
// Enumerating by participation closes that gap for issues AND covers task/plan
// participants who hold no work-item, at the SAME participant boundary post_message
// already enforces (no authz widening). The work-item derivation in
// agentOwnDomainScopes is retained (additive, deduped). The scan stays bounded —
// each kind is capped by Find's DefaultConversationLimit; participation is
// org-scoped by construction so no cross-org conversation appears.
func (s *Server) agentParticipantConvScopes(d HandlerDeps, r *http.Request, a *agent.Agent) []filesservice.ScopeRef {
	if d.ConvRepo == nil {
		return nil
	}
	orgID := a.OrganizationID()
	if orgID == "" {
		return nil
	}
	ctx := r.Context()
	refs := agentConvRefs(a) // execution ref + identity-member ref (dual-ref)
	var out []filesservice.ScopeRef
	for _, kind := range []conversation.ConversationKind{
		conversation.ConversationKindChannel,
		conversation.ConversationKindDM,
		conversation.ConversationKindTask,
		conversation.ConversationKindIssue,
		conversation.ConversationKindPlan,
	} {
		k := kind
		convs, err := d.ConvRepo.Find(ctx, conversation.ConversationFilter{
			OrganizationID: orgID,
			Kind:           &k,
		})
		if err != nil {
			continue // fail-closed: skip this kind rather than over-grant
		}
		for _, c := range convs {
			for _, ref := range refs {
				if c.HasActiveParticipant(ref) {
					out = append(out, filesservice.ScopeRef{
						Scope:   files.ScopeConversation,
						ScopeID: string(c.ID()),
					})
					break
				}
			}
		}
	}
	return out
}

// dedupScopeRefs removes duplicate {Scope, ScopeID} pairs, preserving order.
func dedupScopeRefs(in []filesservice.ScopeRef) []filesservice.ScopeRef {
	seen := make(map[filesservice.ScopeRef]struct{}, len(in))
	out := in[:0:0]
	for _, sr := range in {
		if _, ok := seen[sr]; ok {
			continue
		}
		seen[sr] = struct{}{}
		out = append(out, sr)
	}
	return out
}

// agentReachable reports whether the agent may download the blob at fileURI: it
// is reachable iff at least one LIVE reference to the blob is in a scope the
// agent can enumerate (agentOwnDomainScopes). Fail-closed — an empty own-domain
// (Reachable with empty callerScopes) yields false.
func (s *Server) agentReachable(d HandlerDeps, r *http.Request, a *agent.Agent, fileURI files.FileURI) (bool, error) {
	scopes, err := s.agentOwnDomainScopes(d, r, a)
	if err != nil {
		return false, err
	}
	return d.FilesSvc.Reachable(r.Context(), fileURI, scopes)
}

// agentScopeInDomain is the attach/upload authz membership test: it reports
// whether the requested {scope, scope_id} is present in the agent's own-domain.
// Rejecting a foreign project / org / other-conversation / other-task scope is
// thus just absence from this set.
func (s *Server) agentScopeInDomain(d HandlerDeps, r *http.Request, a *agent.Agent, scope files.FileScope, scopeID string) (bool, error) {
	scopes, err := s.agentOwnDomainScopes(d, r, a)
	if err != nil {
		return false, err
	}
	want := filesservice.ScopeRef{Scope: scope, ScopeID: scopeID}
	for _, sr := range scopes {
		if sr == want {
			return true, nil
		}
	}
	return false, nil
}

// =============================================================================
// upload_file → put → complete
// =============================================================================

type uploadFileReq struct {
	AgentID     string `json:"agent_id"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	Scope       string `json:"scope"`    // optional
	ScopeID     string `json:"scope_id"` // optional
}

// uploadFileHandler mints an upload session owned by the operating agent
// (CreatedBy=agent:<id>). When a scope is supplied it is AUTHZ-FIRST: the
// {scope, scope_id} must be in the agent's own-domain, else 403
// scope_not_in_agent_domain and NO session is created. Returns the transfer +
// file URIs (echoing scope/scope_id).
func (s *Server) uploadFileHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req uploadFileReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.FilesSvc == nil {
		writeError(w, http.StatusNotImplemented, "files_not_wired", "files service not wired")
		return
	}
	scope := files.FileScope(req.Scope)
	if req.Scope != "" {
		if !scope.IsValid() {
			writeError(w, http.StatusBadRequest, "invalid_scope", "unknown file scope")
			return
		}
		// AUTHZ-FIRST: validate the placement is in the agent's domain BEFORE
		// creating any session.
		inDomain, err := s.agentScopeInDomain(d, r, a, scope, req.ScopeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		if !inDomain {
			writeError(w, http.StatusForbidden, "scope_not_in_agent_domain",
				"requested scope is not in the agent's own domain")
			return
		}
	}
	sess, err := d.FilesSvc.CreateUploadSession(r.Context(), filesservice.CreateUploadCmd{
		ContentType: req.ContentType,
		Size:        req.Size,
		Scope:       scope,
		ScopeID:     req.ScopeID,
		CreatedBy:   agentActor(a),
	})
	if err != nil {
		mapFilesError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"transfer_id":  sess.ID(),
		"transfer_uri": sess.TransferURI(),
		"file_uri":     string(sess.FileURI()),
		"scope":        req.Scope,
		"scope_id":     req.ScopeID,
	})
}

// putAgentBlobHandler streams the request body into the blob backing the open
// upload session. agent_id comes from the query string. Ownership is enforced:
// the session's CreatedBy must be agent:<agentID>, else 403. Write-once: a
// second PUT returns 409 (ErrBlobAlreadyExists, via mapFilesError).
func (s *Server) putAgentBlobHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	agentID := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	a, ok := s.requireAgentOnWorker(w, r, d, agentID)
	if !ok {
		return
	}
	if d.FilesSvc == nil {
		writeError(w, http.StatusNotImplemented, "files_not_wired", "files service not wired")
		return
	}
	transferURI := transferURIPrefix + r.PathValue("transfer_id")
	if !s.agentOwnsSession(w, r, d, a, transferURI) {
		return
	}
	// ContentLength may be -1 (chunked); WriteBlob treats <0 as "stream".
	if err := d.FilesSvc.WriteBlob(r.Context(), transferURI, r.Body, r.ContentLength); err != nil {
		mapFilesError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"written": true})
}

type completeAgentUploadReq struct {
	AgentID string `json:"agent_id"`
	SHA256  string `json:"sha256"` // optional
	Size    int64  `json:"size"`
	Scope   string `json:"scope"`    // optional
	ScopeID string `json:"scope_id"` // optional
}

// completeFileHandler finalizes the upload session (CompleteUpload) and, when a
// scope is supplied, RE-VALIDATES own-domain authz (defensive — the scope could
// have left the domain since create) before adding the placement reference. If
// AddReference fails the error is returned; the orphan blob is acceptable (the
// GC reclaims a blob with zero live references). Returns the file URI (+ the
// reference id when one was created).
func (s *Server) completeFileHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req completeAgentUploadReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.FilesSvc == nil {
		writeError(w, http.StatusNotImplemented, "files_not_wired", "files service not wired")
		return
	}
	transferURI := transferURIPrefix + r.PathValue("transfer_id")
	sess, ok := s.loadOwnedSession(w, r, d, a, transferURI)
	if !ok {
		return
	}
	scope := files.FileScope(req.Scope)
	if req.Scope != "" {
		if !scope.IsValid() {
			writeError(w, http.StatusBadRequest, "invalid_scope", "unknown file scope")
			return
		}
		// Defensive re-validation: the placement must STILL be in the agent's
		// domain at complete time.
		inDomain, err := s.agentScopeInDomain(d, r, a, scope, req.ScopeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		if !inDomain {
			writeError(w, http.StatusForbidden, "scope_not_in_agent_domain",
				"requested scope is not in the agent's own domain")
			return
		}
	}
	if err := d.FilesSvc.CompleteUpload(r.Context(), transferURI, req.SHA256, req.Size); err != nil {
		mapFilesError(w, err)
		return
	}
	out := map[string]any{"file_uri": string(sess.FileURI())}
	if req.Scope != "" {
		ref, err := d.FilesSvc.AddReference(r.Context(), filesservice.AddReferenceCmd{
			FileURI:   sess.FileURI(),
			Scope:     scope,
			ScopeID:   req.ScopeID,
			MimeType:  sess.ContentType(),
			SizeBytes: req.Size,
			CreatedBy: agentActor(a),
		})
		if err != nil {
			// The blob is complete but unreferenced — acceptable (GC reclaims).
			mapFilesError(w, err)
			return
		}
		out["reference_id"] = ref.ID
	}
	writeJSON(w, http.StatusOK, out)
}

// =============================================================================
// download (agent-domain reachability, fail-closed)
// =============================================================================

// downloadFileHandler streams a blob's bytes after the agent-domain reachability
// check passes. agent_id comes from the query string. Fail-closed: 403
// file_not_reachable when no LIVE reference grants the agent a download.
// Content-Type comes from the first live reference's MimeType (fallback
// application/octet-stream), mirroring D3-d.
func (s *Server) downloadFileHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	agentID := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	a, ok := s.requireAgentOnWorker(w, r, d, agentID)
	if !ok {
		return
	}
	if d.FilesSvc == nil {
		writeError(w, http.StatusNotImplemented, "files_not_wired", "files service not wired")
		return
	}
	fileURI, err := files.NewFileURI(r.PathValue("ulid"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_file_uri", err.Error())
		return
	}
	reachable, err := s.agentReachable(d, r, a, fileURI)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reachability_failed", err.Error())
		return
	}
	if !reachable {
		// Fail-closed. 403 (not 404) — the URI is well-formed; the agent simply
		// has no reachable reference in its own domain.
		writeError(w, http.StatusForbidden, "file_not_reachable",
			"no reachable reference grants the agent a download")
		return
	}
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

// =============================================================================
// attach_file
// =============================================================================

type attachFileReq struct {
	AgentID string `json:"agent_id"`
	FileURI string `json:"file_uri"`
	Scope   string `json:"scope"`
	ScopeID string `json:"scope_id"`
}

// attachFileHandler adds a placement reference for an existing blob into a scope
// in the agent's OWN domain. own-domain authz on {scope, scope_id} → 403
// scope_not_in_agent_domain when not in domain. Returns the new reference id.
func (s *Server) attachFileHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req attachFileReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.FilesSvc == nil {
		writeError(w, http.StatusNotImplemented, "files_not_wired", "files service not wired")
		return
	}
	fileURI, err := files.ParseFileURI(req.FileURI)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_file_uri", err.Error())
		return
	}
	scope := files.FileScope(req.Scope)
	if !scope.IsValid() {
		writeError(w, http.StatusBadRequest, "invalid_scope", "unknown file scope")
		return
	}
	if strings.TrimSpace(req.ScopeID) == "" {
		writeError(w, http.StatusBadRequest, "missing_scope_id", "")
		return
	}
	inDomain, err := s.agentScopeInDomain(d, r, a, scope, req.ScopeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if !inDomain {
		writeError(w, http.StatusForbidden, "scope_not_in_agent_domain",
			"requested scope is not in the agent's own domain")
		return
	}
	ref, err := d.FilesSvc.AddReference(r.Context(), filesservice.AddReferenceCmd{
		FileURI:   fileURI,
		Scope:     scope,
		ScopeID:   req.ScopeID,
		CreatedBy: agentActor(a),
	})
	if err != nil {
		mapFilesError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reference_id": ref.ID})
}

// =============================================================================
// session-ownership helpers
// =============================================================================

// loadOwnedSession loads the transfer session and verifies the operating agent
// owns it (session.CreatedBy == agent:<id>). On any failure it writes the error
// envelope and returns (nil, false).
func (s *Server) loadOwnedSession(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent, transferURI string) (*files.FileTransferSession, bool) {
	sess, err := d.FilesSvc.FindSessionByTransferURI(r.Context(), transferURI)
	if err != nil {
		mapFilesError(w, err)
		return nil, false
	}
	if sess.CreatedBy() != agentActor(a) {
		writeError(w, http.StatusForbidden, "session_not_owned",
			"transfer session is not owned by this agent")
		return nil, false
	}
	return sess, true
}

// agentOwnsSession is loadOwnedSession discarding the session value.
func (s *Server) agentOwnsSession(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent, transferURI string) bool {
	_, ok := s.loadOwnedSession(w, r, d, a, transferURI)
	return ok
}
