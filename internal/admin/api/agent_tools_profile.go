package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/workforce"
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
	"create_task", "assign_task",
	"subscribe", "block_task", "complete_task",
	// v2.10.3 T170: full issue management for project members.
	// T200 WS4: posting to a task/issue is the single org-scoped post_message
	// (target type task|issue), so it is listed in orgAgentCapabilities, not here.
	"create_issue", "update_issue", "close_issue", "reopen_issue",
}

// orgAgentCapabilities are the org-scoped actions available to any agent
// regardless of project membership (self-discovery + own-work reads + replying
// in conversations it participates in).
var orgAgentCapabilities = []string{
	"list_my_tasks", "get_my_profile", "find_org_agent", "post_message", "list_messages",
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

	// my_projects — org projects this agent is a member of (best-effort: a nil
	// PMService yields []).
	myProjects, err := agentMemberProjects(r.Context(), d, orgID, a.IdentityMemberID())
	if err != nil {
		mapDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		// E2E finding F-3: the operating agent had NO way to learn its OWN identity
		// (display name) — with several agents in one conversation it would adopt
		// another agent's name from the @mention text and impersonate it. Surface the
		// agent's own display_name + agent_ref (the "agent:<member-id>" form used in
		// @mentions / assignee) so it can recognise which mentions are actually for it.
		"display_name":    a.Profile().Name,
		"agent_ref":       "agent:" + a.IdentityMemberID(),
		"org_id":          orgID,
		"org_name":        orgName,
		"my_projects":     myProjects,
		"my_capabilities": orgAgentCapabilities,
	})
}

// agentMemberProjects returns the org projects the agent (by identity-member id)
// is a member of, as [{id, name, role, my_capabilities}]. The agent's project-
// member ref is "agent:<identity-member-id>" (mirrors the #224 wake path). A nil
// PMService or empty member id yields an empty (never nil) slice. Shared by
// get_my_profile and the create_task "available projects" error hint (#239).
func agentMemberProjects(ctx context.Context, d HandlerDeps, orgID, identityMemberID string) ([]map[string]any, error) {
	out := []map[string]any{}
	if d.PMService == nil {
		return out, nil
	}
	agentRef := "agent:" + identityMemberID
	projects, err := d.PMService.ListProjects(ctx, orgID)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		members, merr := d.PMService.ListMembers(ctx, p.ID())
		if merr != nil {
			return nil, merr
		}
		for _, m := range members {
			if string(m.IdentityID()) == agentRef {
				out = append(out, map[string]any{
					"id":              string(p.ID()),
					"name":            p.Name(),
					"role":            string(m.Role()),
					"my_capabilities": projectMemberCapabilities,
				})
				break
			}
		}
	}
	return out, nil
}

// availableProjectsHint renders ", available: [name (id), ...]" listing the
// agent's own projects — appended to a "project not found" message so the agent
// can self-correct (v2.7.1 #239). Empty string when there are none / on error
// (the base message stands alone).
func availableProjectsHint(ctx context.Context, d HandlerDeps, orgID, identityMemberID string) string {
	projects, err := agentMemberProjects(ctx, d, orgID, identityMemberID)
	if err != nil || len(projects) == 0 {
		return ""
	}
	names := make([]string, 0, len(projects))
	for _, p := range projects {
		names = append(names, fmt.Sprintf("%v (%v)", p["name"], p["id"]))
	}
	return ", available: [" + strings.Join(names, ", ") + "]"
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
//
// T461: each row is enriched so the PD can dispatch work to a capable AND
// least-busy agent in ONE call (root-fix for uneven assignment):
//   - profile: display_name, agent_ref, capability_tags (dispatch labels)
//   - status:  lifecycle (running/stopped/error/...) + the bound worker's
//     online/offline + last_heartbeat_at (liveness)
//   - load:    the agent's non-terminal task counts by assignee — see the load
//     口径 note below.
//
// Load口径 (what counts): reuses the same metric the web console / FleetView
// show, so the numbers agree. `running` = tasks in status=running (a blocked
// task is running + a blocked_reason, so it counts). `open` = status=open —
// assigned but not yet started ("queued" in the request vocabulary; there is no
// separate queued status). `total` = running + open. EXCLUDED: terminal tasks
// (completed/discarded) and tasks under archived/done plans; `reopened` (a rare
// transient) is not separately bucketed. The status/load enrichments are
// best-effort: an unwired WorkerRepo/PMService simply omits worker fields /
// reports zero load — the roster still returns.
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
	// Task loads are aggregated ONCE across the org, keyed by "agent:<id>"
	// assignee ref (best-effort: a nil PMService yields a nil map → zero loads).
	var loads map[pm.IdentityRef]pm.AgentTaskLoad
	if d.PMService != nil {
		loads, _ = d.PMService.AgentTaskLoads(r.Context())
	}
	// Worker lookups are cached per distinct worker_id (agents commonly share a
	// worker); a nil cache value means "looked up, not found".
	workerCache := map[string]*workforce.Worker{}
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
		// v2.7.1 #241: also emit a ready-to-use assignee_ref ("agent:<id>", the
		// ADR-0033 actor-ref form). assign_task validates the assignee as a
		// prefixed ref, so the agent feeds assignee_ref straight in — no manual
		// "agent:"+id concatenation (which is a bare-id-vs-prefixed-ref footgun,
		// the same class as the #240 createDm bug). id stays bare for display/#192.
		ref := "agent:" + id

		tags := ag.CapabilityTags()
		if tags == nil {
			tags = []string{}
		}

		// status: lifecycle intent + bound-worker liveness (best-effort).
		status := map[string]any{"lifecycle": string(ag.Lifecycle())}
		if le := ag.LifecycleError(); le != "" {
			status["lifecycle_error"] = le
		}
		if d.WorkerRepo != nil {
			if wid := strings.TrimSpace(ag.WorkerID()); wid != "" {
				wk, seen := workerCache[wid]
				if !seen {
					if found, ferr := d.WorkerRepo.FindByID(r.Context(), workforce.WorkerID(wid)); ferr == nil {
						wk = found
					}
					workerCache[wid] = wk
				}
				if wk != nil {
					status["worker_status"] = string(wk.Status())
					if hb := wk.LastHeartbeatAt(); hb != nil {
						status["last_heartbeat_at"] = hb.UTC().Format(time.RFC3339Nano)
					}
				}
			}
		}

		l := loads[pm.IdentityRef(ref)]
		load := map[string]any{
			"running": l.Running,
			"open":    l.Pending,
			"total":   l.Running + l.Pending,
		}

		out = append(out, map[string]any{
			// Top-level id/name/assignee_ref kept for back-compat (#239/#241).
			"id":           id,
			"name":         name,
			"assignee_ref": ref,
			"profile": map[string]any{
				"display_name":    name,
				"agent_ref":       ref,
				"capability_tags": tags,
			},
			"status": status,
			"load":   load,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": out})
}

// findOrgChannelReq is the body for POST /admin/agent-tools/find_org_channel.
type findOrgChannelReq struct {
	AgentID string `json:"agent_id"`
	Name    string `json:"name"`
}

// findOrgChannelHandler returns the channels in the operating agent's org whose
// name matches the substring (case-insensitive; empty name lists all = the
// "available channels" list). This is the channel name→id resolution the agent
// needs before post_message (@oopslink "cha1" screenshot pain): an empty result
// IS the "no such channel" signal — there is no name-based send path, so no
// separate name-not-found error endpoint (v2.7.1 #246, the #239 pattern for
// channels). Read-only, org-confined (org from the guardrail-resolved agent).
//
// channel_id is an ENTITY id (bare, directly usable as post_message's
// conversation_id) — NOT an ADR-0033 actor ref, so no "agent:"/"user:" prefix
// (per the #241 ref-vs-id boundary).
func (s *Server) findOrgChannelHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req findOrgChannelReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.ConvRepo == nil {
		writeError(w, http.StatusNotImplemented, "conversation_not_wired", "")
		return
	}
	kind := conversation.ConversationKindChannel
	convs, err := d.ConvRepo.Find(r.Context(), conversation.ConversationFilter{
		Kind: &kind, OrganizationID: a.OrganizationID(), Limit: 500,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	needle := strings.ToLower(strings.TrimSpace(req.Name))
	out := []map[string]any{}
	for _, c := range convs {
		name := c.Name()
		if needle != "" && !strings.Contains(strings.ToLower(name), needle) {
			continue
		}
		out = append(out, map[string]any{"id": string(c.ID()), "name": name})
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": out})
}
