package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	agentbc "github.com/oopslink/agent-center/internal/agent"
	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/persistence"
)

// agentFacingID returns the business-layer id for an agent: its identity-member
// id ("agent-<ulid>"), the ONLY agent id that crosses the BC boundary (v2.7 #185
// — the execution-entity ULID is internal). Falls back to the entity id only for
// a standalone agent with no member (should not occur after no-middle-state;
// defensive so a response never carries an empty id).
func agentFacingID(a *agentbc.Agent) string {
	if m := strings.TrimSpace(a.IdentityMemberID()); m != "" {
		return m
	}
	return string(a.ID())
}

// v2.7 C3 Agent HTTP surface (ADR-0049). The new Agent BC is ORG-scoped (an
// Agent belongs to an Org and can take tasks across projects — it is NOT
// project-nested), so routes live at /api/agents + /api/agents/{id}/... gated by
// requireOrgMember + an agent-in-org check. Lifecycle verbs are INTENT only —
// they transition the Agent AR + emit an outbox event; the Environment BC (D2
// AgentController) reconciles the real worker process. (This surface replaces
// the legacy workforce.AgentInstance /api/agents routes; that backend retires
// with the rest of the legacy execution stack in #107.)

// agentCallerRef maps an authenticated webconsole identity to an Agent-BC ref.
func agentCallerRef(id *identity.Identity) agentbc.IdentityRef {
	if id == nil {
		return ""
	}
	if id.Kind() == "agent" {
		return agentbc.IdentityRef("agent:" + id.ID())
	}
	return agentbc.IdentityRef("user:" + id.ID())
}

// mapAgentError translates Agent-BC errors to HTTP responses.
func mapAgentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agentbc.ErrAgentNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, agentbc.ErrIllegalLifecycle):
		writeError(w, http.StatusConflict, "illegal_transition", err.Error())
	case errors.Is(err, agentbc.ErrAgentNotStopped):
		// v2.7 #197: must stop the agent before deleting it.
		writeError(w, http.StatusConflict, "agent_running", err.Error())
	case errors.Is(err, agentbc.ErrAgentHasActiveWork):
		writeError(w, http.StatusConflict, "agent_has_active_work", err.Error())
	case errors.Is(err, agentsvc.ErrWorkerNotInOrg):
		writeError(w, http.StatusBadRequest, "worker_not_in_org", err.Error())
	case errors.Is(err, agentbc.ErrUnsupportedCLI):
		// v2.7 #181 / FINDING-F: cli not in the execution allowlist.
		writeError(w, http.StatusBadRequest, "invalid_cli", err.Error())
	case errors.Is(err, agentsvc.ErrResetNotConfirmed),
		errors.Is(err, agentbc.ErrInvalidResetScope),
		errors.Is(err, agentbc.ErrWorkerRequired),
		errors.Is(err, agentbc.ErrInvalidLifecycle):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "agent_error", err.Error())
	}
}

// --- serializers ------------------------------------------------------------

func agentMap(a *agentbc.Agent, availability agentbc.Availability) map[string]any {
	p := a.Profile()
	// v2.7 #183 (FINDING): emit skills/env_vars as [] / {} never null. Go nil
	// slices/maps marshal to JSON null, but the SPA types them non-null
	// (skills: string[]) and reads a.skills.length — a freshly-created agent
	// with no skills sent "skills": null → AgentDetail crashed
	// (Cannot read properties of null (reading 'length')). Honor the contract
	// at the serializer so every consumer (get/list/create) is safe.
	skills := a.Skills()
	if skills == nil {
		skills = []string{}
	}
	envVars := p.EnvVars
	if envVars == nil {
		envVars = map[string]string{}
	}
	m := map[string]any{
		// v2.7 #185: the business-layer id is the identity-member id; the
		// execution-entity ULID is internal and must NOT appear in API responses.
		"id": agentFacingID(a), "organization_id": a.OrganizationID(),
		"name": p.Name, "description": p.Description, "model": p.Model, "cli": p.CLI,
		"env_vars": envVars, "skills": skills, "worker_id": a.WorkerID(),
		"lifecycle": string(a.Lifecycle()), "availability": string(availability),
		"created_by": string(a.CreatedBy()), "version": a.Version(),
		// v2.7 #157: kept for back-compat (equals id now). Lets the Members page
		// navigate an agent member → AgentDetail.
		"identity_member_id": a.IdentityMemberID(),
		"created_at":         a.CreatedAt().Format(time.RFC3339Nano),
		"updated_at":         a.UpdatedAt().Format(time.RFC3339Nano),
	}
	if le := a.LifecycleError(); le != "" {
		m["lifecycle_error"] = le
	}
	return m
}

// agentWorkItemMap renders a work item. agentFacingID is the business-layer id
// of the owning agent (v2.7 #185) — the records are always for one agent, so the
// caller passes it rather than leaking the entity wi.AgentID().
func agentWorkItemMap(wi *agentbc.AgentWorkItem, agentFacingID string) map[string]any {
	return map[string]any{
		"id": wi.ID(), "agent_id": agentFacingID, "task_ref": wi.TaskRef(),
		"status": string(wi.Status()), "interactions": wi.Interactions(), "version": wi.Version(),
		"created_at": wi.CreatedAt().Format(time.RFC3339Nano),
		"updated_at": wi.UpdatedAt().Format(time.RFC3339Nano),
	}
}

// agentActivityMap renders an activity event. agentFacingID is the owning
// agent's business-layer id (v2.7 #185), passed by the caller (single-agent
// listing) so the entity e.AgentID() never leaks.
func agentActivityMap(e *agentbc.AgentActivityEvent, agentFacingID string) map[string]any {
	m := map[string]any{
		"id": e.ID(), "agent_id": agentFacingID, "event_type": e.EventType(),
		"payload": e.Payload(), "occurred_at": e.OccurredAt().Format(time.RFC3339Nano),
	}
	if r := e.WorkItemRef(); r != "" {
		m["work_item_ref"] = r
	}
	if r := e.InteractionRef(); r != "" {
		m["interaction_ref"] = r
	}
	return m
}

// --- gate -------------------------------------------------------------------

// agentRequireInOrg resolves {id}, requires org membership, and verifies the
// Agent belongs to the caller's org (cross-org → 404).
func (s *Server) agentRequireInOrg(w http.ResponseWriter, r *http.Request, d HandlerDeps) (*agentbc.Agent, string, bool) {
	if d.AgentSvc == nil {
		writeError(w, http.StatusNotImplemented, "agent_not_wired", "Agent service not wired")
		return nil, "", false
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return nil, "", false
	}
	// v2.7 #185: {id} is the business-layer member id; ResolveAgent bridges it to
	// the execution entity (also accepts a raw entity id for back-compat).
	a, err := d.AgentSvc.ResolveAgent(r.Context(), r.PathValue("id"))
	if err != nil || a.OrganizationID() != orgID {
		writeError(w, http.StatusNotFound, "not_found", "agent not found in this organization")
		return nil, "", false
	}
	return a, orgID, true
}

// agentWriteJSON writes an agent with its derived availability.
func (s *Server) agentWriteJSON(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agentbc.Agent) {
	avail, err := d.AgentSvc.Availability(r.Context(), a)
	if err != nil {
		mapAgentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, agentMap(a, avail))
}

// --- handlers ---------------------------------------------------------------

func (s *Server) agentListHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.AgentSvc == nil {
		writeError(w, http.StatusNotImplemented, "agent_not_wired", "")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	as, err := d.AgentSvc.ListAgents(r.Context(), orgID)
	if err != nil {
		mapAgentError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(as))
	for _, a := range as {
		avail, aerr := d.AgentSvc.Availability(r.Context(), a)
		if aerr != nil {
			mapAgentError(w, aerr)
			return
		}
		out = append(out, agentMap(a, avail))
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": out})
}

// NOTE: agentCreateHandler (POST /api/agents) was removed in v2.7 (#185 /
// no-middle-state). Agents are created ONLY via POST /api/members/agent
// (addAgentMemberHandler), which atomically provisions the identity member AND
// the execution agent (#157) so every agent carries a member id.

func (s *Server) agentGetHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, _, ok := s.agentRequireInOrg(w, r, d)
	if !ok {
		return
	}
	s.agentWriteJSON(w, r, d, a)
}

// agentDeleteHandler hard-deletes an agent (v2.7 #197). Guards (in the service):
// the agent must be Stopped (else 409 agent_running) and idle (no active work
// item, else 409 agent_has_active_work). In one tx it deletes the agent row
// (releasing the worker binding) AND cascade-deletes the agent's identity-member
// + identity (symmetric to #157's atomic create — no orphan member). Lingering
// pm/conversation refs to the deleted agent render as "(deleted)" + a v2.8 sweep.
func (s *Server) agentDeleteHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, orgID, ok := s.agentRequireInOrg(w, r, d)
	if !ok {
		return
	}
	if d.AgentSvc == nil || d.IdentityRepo == nil || d.MemberRepo == nil || d.DB == nil {
		writeError(w, http.StatusNotImplemented, "not_wired", "agent delete deps not wired")
		return
	}
	memberID := strings.TrimSpace(a.IdentityMemberID())
	if err := persistence.RunInTx(r.Context(), d.DB, func(txCtx context.Context) error {
		if err := d.AgentSvc.DeleteAgent(txCtx, a.ID()); err != nil {
			return err
		}
		if memberID != "" {
			if m, merr := d.MemberRepo.GetByOrganizationAndIdentity(txCtx, orgID, memberID); merr == nil && m != nil {
				if derr := d.MemberRepo.Delete(txCtx, m.ID()); derr != nil {
					return derr
				}
			}
			if derr := d.IdentityRepo.Delete(txCtx, memberID); derr != nil {
				return derr
			}
		}
		return nil
	}); err != nil {
		mapAgentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": agentFacingID(a)})
}

// agentLifecycleAction runs a lifecycle verb then returns the refreshed agent.
func (s *Server) agentLifecycleAction(w http.ResponseWriter, r *http.Request, run func(id agentbc.AgentID) error) {
	d := hd(r)
	a, _, ok := s.agentRequireInOrg(w, r, d)
	if !ok {
		return
	}
	if err := run(a.ID()); err != nil {
		mapAgentError(w, err)
		return
	}
	got, _ := d.AgentSvc.GetAgent(r.Context(), a.ID())
	s.agentWriteJSON(w, r, d, got)
}

func (s *Server) agentStartHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	s.agentLifecycleAction(w, r, func(id agentbc.AgentID) error { return d.AgentSvc.StartAgent(r.Context(), id) })
}

func (s *Server) agentStopHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	s.agentLifecycleAction(w, r, func(id agentbc.AgentID) error { return d.AgentSvc.StopAgent(r.Context(), id) })
}

func (s *Server) agentRestartHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	s.agentLifecycleAction(w, r, func(id agentbc.AgentID) error { return d.AgentSvc.RestartAgent(r.Context(), id) })
}

func (s *Server) agentResetHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scope   string `json:"scope"`
		Confirm bool   `json:"confirm"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	d := hd(r)
	s.agentLifecycleAction(w, r, func(id agentbc.AgentID) error {
		return d.AgentSvc.ResetAgent(r.Context(), id, agentbc.ResetScope(req.Scope), req.Confirm)
	})
}

func (s *Server) agentWorkItemsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, _, ok := s.agentRequireInOrg(w, r, d)
	if !ok {
		return
	}
	items, err := d.AgentSvc.ListWorkItems(r.Context(), a.ID())
	if err != nil {
		mapAgentError(w, err)
		return
	}
	facing := agentFacingID(a)
	out := make([]map[string]any, 0, len(items))
	for _, wi := range items {
		out = append(out, agentWorkItemMap(wi, facing))
	}
	writeJSON(w, http.StatusOK, map[string]any{"work_items": out})
}

func (s *Server) agentActivityHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, _, ok := s.agentRequireInOrg(w, r, d)
	if !ok {
		return
	}
	events, err := d.AgentSvc.ListActivity(r.Context(), a.ID(), 0)
	if err != nil {
		mapAgentError(w, err)
		return
	}
	facing := agentFacingID(a)
	out := make([]map[string]any, 0, len(events))
	for _, e := range events {
		out = append(out, agentActivityMap(e, facing))
	}
	writeJSON(w, http.StatusOK, map[string]any{"activity": out})
}
