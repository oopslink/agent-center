package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/coderepo"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// Agent MCP repo tools (v2.18.4 BE-2, issue-f980c8de): a standard, read-only
// repository-info surface for agents. PROJECT-MEMBER gated (the agent must be a
// member of the project whose references it reads). The encrypted credential is
// NEVER returned — only public metadata (+ optional live remote commits/branches
// fetched server-side via the provider abstraction).

// --- list_project_repos ------------------------------------------------------

type listProjectReposReq struct {
	AgentID   string `json:"agent_id"`
	ProjectID string `json:"project_id"`
}

// listProjectReposHandler returns a project's referenced repos, resolved through to
// the workspace Repo's public info. Project-member scoped; credential never emitted.
func (s *Server) listProjectReposHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req listProjectReposReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.PMService == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		writeError(w, http.StatusBadRequest, "missing_project_id", "")
		return
	}
	refs, err := d.PMService.ListCodeReposForMember(r.Context(), pm.ProjectID(req.ProjectID), pm.IdentityRef(agentActor(a)))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		out = append(out, s.agentRepoRefMap(r.Context(), d, a, ref))
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": out})
}

// --- get_repo_info -----------------------------------------------------------

type getRepoInfoReq struct {
	AgentID   string `json:"agent_id"`
	ProjectID string `json:"project_id"`
	RepoID    string `json:"repo_id"` // optional — empty resolves the project's primary
	Live      bool   `json:"live"`    // optional — attach remote commits/branches
}

// getRepoInfoHandler returns one repo's standard info for a project the agent is a
// member of. repo_id selects a specific referenced repo; empty resolves the
// project's PRIMARY. live=true attaches recent remote commits + branches (fetched
// server-side via the provider; a remote failure is reported alongside the static
// info rather than failing the whole call). Credential never emitted.
func (s *Server) getRepoInfoHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req getRepoInfoReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.PMService == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		writeError(w, http.StatusBadRequest, "missing_project_id", "")
		return
	}
	ref, err := d.PMService.ResolveProjectRepoForMember(r.Context(), pm.ProjectID(req.ProjectID), req.RepoID, pm.IdentityRef(agentActor(a)))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	info := s.agentRepoRefMap(r.Context(), d, a, ref)
	if req.Live {
		info["live"] = s.liveRepoView(r.Context(), d, a, ref)
	}
	writeJSON(w, http.StatusOK, info)
}

// agentRepoRefMap builds the credential-free public view of a project repo
// reference: a workspace-Repo ref resolves through to the Repo's label / description
// / url / provider / default_branch (org-scoped to the agent — a cross-org repo_id
// is treated as unresolved, never leaked); a legacy url-only ref reports its own
// url/label. is_primary + repo_id come from the reference.
func (s *Server) agentRepoRefMap(ctx context.Context, d HandlerDeps, a *agent.Agent, ref *pm.CodeRepoRef) map[string]any {
	m := map[string]any{
		"ref_id":     ref.ID(),
		"repo_id":    ref.RepoID(),
		"label":      ref.Label(),
		"url":        ref.URL(),
		"is_primary": ref.IsPrimary(),
	}
	if ref.RepoID() != "" && d.CodeRepoSvc != nil {
		if repo, err := d.CodeRepoSvc.GetRepo(ctx, ref.RepoID()); err == nil && repo.OrgID() == a.OrganizationID() {
			m["label"] = repo.Label()
			m["description"] = repo.Description()
			m["url"] = repo.URL()
			m["provider"] = string(repo.Provider())
			m["default_branch"] = repo.DefaultBranch()
		}
	}
	return m
}

// liveRepoView fetches recent remote commits + branches for a workspace-Repo ref
// (server-side, via the provider). It NEVER returns the credential; a remote/wiring
// failure is reported as an error string so the agent still gets the static info.
func (s *Server) liveRepoView(ctx context.Context, d HandlerDeps, a *agent.Agent, ref *pm.CodeRepoRef) map[string]any {
	live := map[string]any{}
	if ref.RepoID() == "" {
		live["error"] = "live view unavailable for a url-only reference (no workspace repo)"
		return live
	}
	if d.CodeRepoSvc == nil {
		live["error"] = "remote viewing not configured"
		return live
	}
	// Org guard: only view a repo in the agent's own workspace.
	if repo, err := d.CodeRepoSvc.GetRepo(ctx, ref.RepoID()); err != nil || repo.OrgID() != a.OrganizationID() {
		live["error"] = coderepo.ErrRepoNotFound.Error()
		return live
	}
	if commits, err := d.CodeRepoSvc.ListCommits(ctx, ref.RepoID(), "", 0); err != nil {
		live["commits_error"] = err.Error()
	} else {
		out := make([]map[string]any, 0, len(commits))
		for _, c := range commits {
			out = append(out, map[string]any{
				"sha": c.SHA, "message": c.Message, "author": c.Author,
				"date": c.CommittedAt.Format(time.RFC3339),
			})
		}
		live["commits"] = out
	}
	if branches, err := d.CodeRepoSvc.ListBranches(ctx, ref.RepoID()); err != nil {
		live["branches_error"] = err.Error()
	} else {
		out := make([]map[string]any, 0, len(branches))
		for _, b := range branches {
			out = append(out, map[string]any{"name": b.Name, "is_default": b.IsDefault})
		}
		live["branches"] = out
	}
	return live
}
