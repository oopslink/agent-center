package api

import (
	"errors"
	"net/http"
	"time"

	agentbc "github.com/oopslink/agent-center/internal/agent"
	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	"github.com/oopslink/agent-center/internal/identity"
)

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
	case errors.Is(err, agentsvc.ErrWorkerNotInOrg):
		writeError(w, http.StatusBadRequest, "worker_not_in_org", err.Error())
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
	m := map[string]any{
		"id": string(a.ID()), "organization_id": a.OrganizationID(),
		"name": p.Name, "description": p.Description, "model": p.Model, "cli": p.CLI,
		"env_vars": p.EnvVars, "skills": a.Skills(), "worker_id": a.WorkerID(),
		"lifecycle": string(a.Lifecycle()), "availability": string(availability),
		"created_by": string(a.CreatedBy()), "version": a.Version(),
		"created_at": a.CreatedAt().Format(time.RFC3339Nano),
		"updated_at": a.UpdatedAt().Format(time.RFC3339Nano),
	}
	if le := a.LifecycleError(); le != "" {
		m["lifecycle_error"] = le
	}
	return m
}

func agentWorkItemMap(wi *agentbc.AgentWorkItem) map[string]any {
	return map[string]any{
		"id": wi.ID(), "agent_id": string(wi.AgentID()), "task_ref": wi.TaskRef(),
		"status": string(wi.Status()), "interactions": wi.Interactions(), "version": wi.Version(),
		"created_at": wi.CreatedAt().Format(time.RFC3339Nano),
		"updated_at": wi.UpdatedAt().Format(time.RFC3339Nano),
	}
}

func agentActivityMap(e *agentbc.AgentActivityEvent) map[string]any {
	m := map[string]any{
		"id": e.ID(), "agent_id": string(e.AgentID()), "event_type": e.EventType(),
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
	a, err := d.AgentSvc.GetAgent(r.Context(), agentbc.AgentID(r.PathValue("id")))
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

func (s *Server) agentCreateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.AgentSvc == nil {
		writeError(w, http.StatusNotImplemented, "agent_not_wired", "")
		return
	}
	caller, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	var req struct {
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Model       string            `json:"model"`
		CLI         string            `json:"cli"`
		EnvVars     map[string]string `json:"env_vars"`
		Skills      []string          `json:"skills"`
		WorkerID    string            `json:"worker_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	id, err := d.AgentSvc.CreateAgent(r.Context(), agentsvc.CreateAgentCommand{
		OrganizationID: orgID, Name: req.Name, Description: req.Description, Model: req.Model,
		CLI: req.CLI, EnvVars: req.EnvVars, Skills: req.Skills, WorkerID: req.WorkerID,
		CreatedBy: agentCallerRef(caller),
	})
	if err != nil {
		mapAgentError(w, err)
		return
	}
	a, _ := d.AgentSvc.GetAgent(r.Context(), id)
	s.agentWriteJSON(w, r, d, a)
}

func (s *Server) agentGetHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, _, ok := s.agentRequireInOrg(w, r, d)
	if !ok {
		return
	}
	s.agentWriteJSON(w, r, d, a)
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
	out := make([]map[string]any, 0, len(items))
	for _, wi := range items {
		out = append(out, agentWorkItemMap(wi))
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
	out := make([]map[string]any, 0, len(events))
	for _, e := range events {
		out = append(out, agentActivityMap(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"activity": out})
}
