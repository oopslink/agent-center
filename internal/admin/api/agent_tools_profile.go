package api

import (
	"net/http"
	"strings"
)

// =============================================================================
// Agent self/org-discovery MCP tools (v2.7.1 #239) — let an agent discover its
// own scope + find peer agents WITHOUT a human round-trip (@oopslink screenshot
// scenario). Both ride the same per-agent guardrail (requireAgentOnWorker): the
// operating agent is fixed by the token-bound worker, so these are inherently
// self/own-org scoped — no cross-agent or cross-org reach.
//
// NEITHER tool touches a write path: get_my_profile is a read projection of the
// agent's own org/projects/capabilities (self only — no other entity); the
// project-membership default-write boundary (β) is deliberately NOT expanded
// (@oopslink: permissions don't widen). find_org_agent is a read of visible org
// agents by name. Capability lists are descriptive, derived from the existing
// authz (project membership = the write-gate), not a new permission grant.
// =============================================================================

// projectMemberCapabilities are the actions an agent that IS a member of a
// project may perform there. v2.7.1: project membership is the write-gate and v1
// project roles don't yet differentiate owner vs member (projectmanager/types.go),
// so this list is membership-derived, not role-derived. Descriptive only — it
// mirrors the pm write-gate + agent-tools surface, it does not grant anything.
var projectMemberCapabilities = []string{
	"create_task", "assign_task", "post_task_message",
	"subscribe", "block_task", "complete_task",
}

// orgAgentCapabilities are the org-scoped actions available to any agent
// regardless of project membership (self-discovery + own-work reads + replying
// in conversations it participates in).
var orgAgentCapabilities = []string{
	"get_my_work", "get_my_profile", "find_org_agent", "post_message",
}

// getMyProfileReq is the body for POST /admin/agent-tools/get_my_profile.
// agent_id is process-fixed (injected by the MCP host from cfg, never the model).
type getMyProfileReq struct {
	AgentID string `json:"agent_id"`
}

// getMyProfileHandler returns the OPERATING agent's own profile: org, the
// projects it is a member of (with role + per-project capabilities), and its
// org-scoped capabilities. Self-only — it reads nothing about other agents or
// other orgs. v2.7.1 #239.
func (s *Server) getMyProfileHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req getMyProfileReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	orgID := a.OrganizationID()

	// org name — best-effort (empty when the repo is unwired or the org is gone).
	orgName := ""
	if d.IdentityOrgRepo != nil {
		if org, err := d.IdentityOrgRepo.GetByID(r.Context(), orgID); err == nil && org != nil {
			orgName = org.Name()
		}
	}

	// my_projects — org projects this agent is a member of. The agent's
	// project-member ref is "agent:<identity-member-id>" (mirrors the #224
	// project-member wake path). Best-effort: a nil PMService yields [].
	myProjects := []map[string]any{}
	if d.PMService != nil {
		agentRef := "agent:" + a.IdentityMemberID()
		projects, err := d.PMService.ListProjects(r.Context(), orgID)
		if err != nil {
			mapDomainError(w, err)
			return
		}
		for _, p := range projects {
			members, merr := d.PMService.ListMembers(r.Context(), p.ID())
			if merr != nil {
				mapDomainError(w, merr)
				return
			}
			for _, m := range members {
				if string(m.IdentityID()) == agentRef {
					myProjects = append(myProjects, map[string]any{
						"id":              string(p.ID()),
						"name":            p.Name(),
						"role":            string(m.Role()),
						"my_capabilities": projectMemberCapabilities,
					})
					break
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"org_id":          orgID,
		"org_name":        orgName,
		"my_projects":     myProjects,
		"my_capabilities": orgAgentCapabilities,
	})
}

// findOrgAgentReq is the body for POST /admin/agent-tools/find_org_agent.
type findOrgAgentReq struct {
	AgentID string `json:"agent_id"`
	Name    string `json:"name"`
}

// findOrgAgentHandler returns the visible agents in the operating agent's org
// whose name matches the given substring (case-insensitive). An empty name
// returns all org agents. Read-only, org-confined (the org is the operating
// agent's own, from the guardrail-resolved agent). v2.7.1 #239.
func (s *Server) findOrgAgentHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req findOrgAgentReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	agents, err := d.AgentSvc.ListAgents(r.Context(), a.OrganizationID())
	if err != nil {
		mapDomainError(w, err)
		return
	}
	needle := strings.ToLower(strings.TrimSpace(req.Name))
	out := []map[string]any{}
	for _, ag := range agents {
		name := ag.Profile().Name
		if needle != "" && !strings.Contains(strings.ToLower(name), needle) {
			continue
		}
		// Business-layer id is the identity-member id (v2.7 #185); the
		// execution-entity ULID never crosses the boundary.
		id := ag.IdentityMemberID()
		if id == "" {
			id = string(ag.ID())
		}
		out = append(out, map[string]any{"id": id, "name": name})
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": out})
}
