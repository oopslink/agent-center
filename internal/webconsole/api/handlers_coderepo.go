package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/oopslink/agent-center/internal/coderepo"
	coderepprovider "github.com/oopslink/agent-center/internal/coderepo/provider"
	coderepservice "github.com/oopslink/agent-center/internal/coderepo/service"
	"github.com/oopslink/agent-center/internal/identity"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// --- workspace Repos (org-admin gated) --------------------------------------

// coderepoMap is the MASKED workspace-Repo DTO: the credential is NEVER serialized;
// callers see only has_credential (v2.18.4 BE-1, issue-f980c8de). referenceCount is
// the number of projects referencing this repo (v2.18.4 BE-2 FE contract: "used by
// N projects" + the delete-confirm prompt).
func coderepoMap(r *coderepo.Repo, referenceCount int) map[string]any {
	return map[string]any{
		"id": r.ID(), "organization_id": r.OrgID(), "label": r.Label(), "description": r.Description(),
		"url": r.URL(), "provider": string(r.Provider()), "default_branch": r.DefaultBranch(),
		"has_credential":  r.HasCredential(),
		"reference_count": referenceCount,
		"created_by":      string(r.CreatedBy()), "created_at": r.CreatedAt().Format(time.RFC3339Nano),
		"updated_at": r.UpdatedAt().Format(time.RFC3339Nano), "version": r.Version(),
	}
}

// repoDTO builds the masked repo DTO with its live reference_count (count of
// projects referencing it). A count error degrades to 0 rather than failing the
// read (the count is advisory UI metadata, not the repo itself).
func (s *Server) repoDTO(r *http.Request, d HandlerDeps, repo *coderepo.Repo) map[string]any {
	n, err := d.CodeRepoSvc.CountReferencingProjects(r.Context(), repo.ID())
	if err != nil {
		n = 0
	}
	return coderepoMap(repo, n)
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

// mapViewingError maps the remote-viewing reads (BE-2): a missing viewing wiring →
// 501, a remote fetch failure (bad credential / unreachable host / parse) → 502
// (an upstream/gateway problem, distinct from a center fault), else fall through to
// the shared mapper (repo-not-found, etc.).
func mapViewingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, coderepservice.ErrViewingNotConfigured):
		writeError(w, http.StatusNotImplemented, "not_configured", err.Error())
	case errors.Is(err, coderepo.ErrRepoNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	default:
		writeError(w, http.StatusBadGateway, "remote_error", err.Error())
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
		out = append(out, s.repoDTO(r, d, repo))
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
	writeJSON(w, http.StatusCreated, s.repoDTO(r, d, repo))
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
	writeJSON(w, http.StatusOK, s.repoDTO(r, d, repo))
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

// --- remote viewing (member-readable; commits / branches, BE-2) -------------

// listCodeRepoCommitsHandler serves GET .../code-repos/{repo_id}/commits?branch=&limit=
// — recent commits from the repo's remote via the provider abstraction (go-github /
// git fallback). Member-readable (the credential is used server-side, never returned).
// Response shape (FE contract): {commits:[{sha,message,author,date,...}], branch, source}.
func (s *Server) listCodeRepoCommitsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.CodeRepoSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "code repo service not wired")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	repo, ok := s.codeRepoForOrg(w, r, d, r.PathValue("repo_id"), orgID)
	if !ok {
		return
	}
	branch := r.URL.Query().Get("branch")
	commits, err := d.CodeRepoSvc.ListCommits(r.Context(), repo.ID(), branch, parseLimitQuery(r))
	if err != nil {
		mapViewingError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(commits))
	for _, c := range commits {
		out = append(out, commitWireDTO(c))
	}
	if branch == "" {
		branch = repo.DefaultBranch()
	}
	writeJSON(w, http.StatusOK, map[string]any{"commits": out, "branch": branch, "source": string(repo.Provider())})
}

// listCodeRepoBranchesHandler serves GET .../code-repos/{repo_id}/branches.
// Response shape (FE contract): {branches:[{name,is_default,...}], source}.
func (s *Server) listCodeRepoBranchesHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.CodeRepoSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "code repo service not wired")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	repo, ok := s.codeRepoForOrg(w, r, d, r.PathValue("repo_id"), orgID)
	if !ok {
		return
	}
	branches, err := d.CodeRepoSvc.ListBranches(r.Context(), repo.ID())
	if err != nil {
		mapViewingError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(branches))
	for _, b := range branches {
		out = append(out, branchWireDTO(b))
	}
	writeJSON(w, http.StatusOK, map[string]any{"branches": out, "source": string(repo.Provider())})
}

// codeRepoForOrg loads a repo and verifies it belongs to the caller's org (cross-org
// → 404, so existence isn't leaked across workspaces), returning the repo for source/
// default-branch use.
func (s *Server) codeRepoForOrg(w http.ResponseWriter, r *http.Request, d HandlerDeps, repoID, orgID string) (*coderepo.Repo, bool) {
	repo, err := d.CodeRepoSvc.GetRepo(r.Context(), repoID)
	if err != nil || repo.OrgID() != orgID {
		writeError(w, http.StatusNotFound, "not_found", "code repo not found")
		return nil, false
	}
	return repo, true
}

// commitWireDTO is the FE-locked commit shape ({sha,message,author,date}); url +
// author_email ride along as harmless extras.
func commitWireDTO(c coderepprovider.Commit) map[string]any {
	return map[string]any{
		"sha": c.SHA, "message": c.Message, "author": c.Author,
		"date": c.CommittedAt.Format(time.RFC3339), "author_email": c.AuthorEmail, "url": c.URL,
	}
}

// branchWireDTO is the FE-locked branch shape ({name,is_default}); commit_sha extra.
func branchWireDTO(b coderepprovider.Branch) map[string]any {
	return map[string]any{"name": b.Name, "is_default": b.IsDefault, "commit_sha": b.CommitSHA}
}

// parseLimitQuery reads the optional ?limit= (invalid/absent → 0 = provider default).
func parseLimitQuery(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n < 0 {
		return 0
	}
	return n
}
