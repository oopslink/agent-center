package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/coderepo"
	coderepservice "github.com/oopslink/agent-center/internal/coderepo/service"
	"github.com/oopslink/agent-center/internal/identity"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// --- workspace Repos (org-admin gated) --------------------------------------

// coderepoMap is the MASKED workspace-Repo DTO: the credential is NEVER serialized;
// callers see only has_credential (v2.18.4 BE-1, issue-f980c8de).
func coderepoMap(r *coderepo.Repo) map[string]any {
	return map[string]any{
		"id": r.ID(), "organization_id": r.OrgID(), "label": r.Label(), "description": r.Description(),
		"url": r.URL(), "provider": string(r.Provider()), "default_branch": r.DefaultBranch(),
		"has_credential": r.HasCredential(),
		"created_by":     string(r.CreatedBy()), "created_at": r.CreatedAt().Format(time.RFC3339Nano),
		"updated_at": r.UpdatedAt().Format(time.RFC3339Nano), "version": r.Version(),
	}
}

func mapCodeRepoError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, coderepo.ErrRepoNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, coderepo.ErrLabelRequired), errors.Is(err, coderepo.ErrURLRequired),
		errors.Is(err, coderepo.ErrInvalidProvider), errors.Is(err, coderepo.ErrOrgRequired):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "code_repo_error", err.Error())
	}
}

// requireOrgAdmin resolves org membership and requires owner/admin role (workspace
// Repo + credential management is admin-only; project members can only ref/unref).
func (s *Server) requireOrgAdmin(w http.ResponseWriter, r *http.Request, d HandlerDeps) (callerID *identity.Identity, orgID string, ok bool) {
	id, member, org, mok := requireOrgMember(w, r, d)
	if !mok {
		return nil, "", false
	}
	if !member.Role().AtLeast(identity.RoleAdmin) {
		writeError(w, http.StatusForbidden, "forbidden", "only owner or admin can manage workspace repos")
		return nil, "", false
	}
	return id, org, true
}

func (s *Server) listCodeReposHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.CodeRepoSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "code repo service not wired")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d) // viewing the list is member-readable
	if !ok {
		return
	}
	repos, err := d.CodeRepoSvc.ListRepos(r.Context(), orgID)
	if err != nil {
		mapCodeRepoError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(repos))
	for _, repo := range repos {
		out = append(out, coderepoMap(repo))
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": out})
}

func (s *Server) createCodeRepoHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.CodeRepoSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "code repo service not wired")
		return
	}
	callerID, orgID, ok := s.requireOrgAdmin(w, r, d)
	if !ok {
		return
	}
	var req struct {
		Label         string `json:"label"`
		Description   string `json:"description"`
		URL           string `json:"url"`
		Provider      string `json:"provider"`
		DefaultBranch string `json:"default_branch"`
		Credential    string `json:"credential"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	id, err := d.CodeRepoSvc.CreateRepo(r.Context(), coderepservice.CreateRepoCommand{
		OrgID: orgID, Label: req.Label, Description: req.Description, URL: req.URL,
		Provider: coderepo.Provider(req.Provider), DefaultBranch: req.DefaultBranch,
		Credential: req.Credential, CreatedBy: coderepo.IdentityRef("user:" + callerID.ID()),
	})
	if err != nil {
		mapCodeRepoError(w, err)
		return
	}
	repo, _ := d.CodeRepoSvc.GetRepo(r.Context(), id)
	writeJSON(w, http.StatusCreated, coderepoMap(repo))
}

func (s *Server) updateCodeRepoHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.CodeRepoSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "code repo service not wired")
		return
	}
	_, orgID, ok := s.requireOrgAdmin(w, r, d)
	if !ok {
		return
	}
	repoID := r.PathValue("repo_id")
	if !s.codeRepoInOrg(w, r, d, repoID, orgID) {
		return
	}
	var req struct {
		Label         string  `json:"label"`
		Description   string  `json:"description"`
		URL           string  `json:"url"`
		Provider      string  `json:"provider"`
		DefaultBranch string  `json:"default_branch"`
		Credential    *string `json:"credential"` // nil=unchanged, ""=clear, non-empty=replace
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := d.CodeRepoSvc.UpdateRepo(r.Context(), coderepservice.UpdateRepoCommand{
		ID: repoID, Label: req.Label, Description: req.Description, URL: req.URL,
		Provider: coderepo.Provider(req.Provider), DefaultBranch: req.DefaultBranch, Credential: req.Credential,
	}); err != nil {
		mapCodeRepoError(w, err)
		return
	}
	repo, _ := d.CodeRepoSvc.GetRepo(r.Context(), repoID)
	writeJSON(w, http.StatusOK, coderepoMap(repo))
}

func (s *Server) deleteCodeRepoHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.CodeRepoSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "code repo service not wired")
		return
	}
	_, orgID, ok := s.requireOrgAdmin(w, r, d)
	if !ok {
		return
	}
	repoID := r.PathValue("repo_id")
	if !s.codeRepoInOrg(w, r, d, repoID, orgID) {
		return
	}
	unlinked, err := d.CodeRepoSvc.DeleteRepo(r.Context(), repoID)
	if err != nil {
		mapCodeRepoError(w, err)
		return
	}
	// Strong delete + unref: the confirm prompt ("解除 N 个项目引用") is rendered by the
	// FE from this count (or a prior GET); the credential is gone with the row.
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "unlinked_projects": unlinked})
}

// codeRepoInOrg verifies the repo exists AND belongs to the caller's org (cross-org
// → 404, so a repo's existence is not leaked across workspaces).
func (s *Server) codeRepoInOrg(w http.ResponseWriter, r *http.Request, d HandlerDeps, repoID, orgID string) bool {
	repo, err := d.CodeRepoSvc.GetRepo(r.Context(), repoID)
	if err != nil || repo.OrgID() != orgID {
		writeError(w, http.StatusNotFound, "not_found", "code repo not found")
		return false
	}
	return true
}

// --- project references (project-member gated) ------------------------------

func (s *Server) addProjectRepoRefHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	p, caller, ok := s.pmRequireProjectInOrg(w, r, d)
	if !ok {
		return
	}
	var req struct {
		RepoID    string `json:"repo_id"`
		URL       string `json:"url"`
		Label     string `json:"label"`
		IsPrimary bool   `json:"is_primary"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	id, err := d.PM.AddCodeRepoReference(r.Context(), pmservice.AddCodeRepoReferenceCommand{
		ProjectID: p.ID(), RepoID: req.RepoID, URL: req.URL, Label: req.Label, IsPrimary: req.IsPrimary, Actor: caller,
	})
	if err != nil {
		mapPMError(w, err)
		return
	}
	ref, _ := d.PM.GetCodeRepoRef(r.Context(), id)
	if ref == nil {
		writeJSON(w, http.StatusCreated, map[string]any{"id": id})
		return
	}
	writeJSON(w, http.StatusCreated, pmCodeRepoMap(ref))
}

func (s *Server) removeProjectRepoRefHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	p, caller, ok := s.pmRequireProjectInOrg(w, r, d)
	if !ok {
		return
	}
	if err := d.PM.RemoveCodeRepoReference(r.Context(), p.ID(), r.PathValue("ref_id"), caller); err != nil {
		mapPMError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) setPrimaryProjectRepoHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	p, caller, ok := s.pmRequireProjectInOrg(w, r, d)
	if !ok {
		return
	}
	if err := d.PM.SetPrimaryCodeRepo(r.Context(), p.ID(), r.PathValue("ref_id"), caller); err != nil {
		mapPMError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
