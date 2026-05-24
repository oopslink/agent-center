package api

import (
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/workforce"
	wfservice "github.com/oopslink/agent-center/internal/workforce/service"
)

// =============================================================================
// EnrollSvc — POST /admin/workforce/worker/enroll
// =============================================================================

type enrollReq struct {
	WorkerID     string   `json:"worker_id"`
	Capabilities []string `json:"capabilities"`
}

func (s *Server) workerEnrollHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.EnrollSvc == nil {
		writeError(w, http.StatusNotImplemented, "enroll_svc_not_wired", "")
		return
	}
	var req enrollReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	res, err := d.EnrollSvc.Enroll(r.Context(), wfservice.EnrollCommand{
		WorkerID:      workforce.WorkerID(req.WorkerID),
		Capabilities:  req.Capabilities,
		ActorIdentity: d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"worker_id": string(res.WorkerID),
		"event_id":  string(res.EventID),
		"version":   res.Version,
	})
}

// =============================================================================
// AcceptanceSvc — Propose / Accept / Ignore / Unignore
// =============================================================================

type proposeReq struct {
	WorkerID           string `json:"worker_id"`
	CandidatePath      string `json:"candidate_path"`
	SuggestedProjectID string `json:"suggested_project_id"`
	SuggestedKind      string `json:"suggested_kind"`
}

func (s *Server) proposalProposeHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.AcceptanceSvc == nil {
		writeError(w, http.StatusNotImplemented, "acceptance_svc_not_wired", "")
		return
	}
	var req proposeReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	actor := d.Actor
	if req.WorkerID != "" {
		actor = observability.Actor("worker:" + req.WorkerID)
	}
	res, err := d.AcceptanceSvc.Propose(r.Context(), wfservice.ProposeCommand{
		WorkerID:           workforce.WorkerID(req.WorkerID),
		CandidatePath:      req.CandidatePath,
		SuggestedProjectID: workforce.ProjectID(req.SuggestedProjectID),
		SuggestedKind:      workforce.ProjectKind(req.SuggestedKind),
		Actor:              actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"proposal_id":    string(res.ProposalID),
		"event_id":       string(res.EventID),
		"already_exists": res.AlreadyExists,
	})
}

type proposalAcceptReq struct {
	ProposalID         string `json:"proposal_id"`
	OverrideProjectID  string `json:"override_project_id"`
	OverrideKind       string `json:"override_kind"`
	OverrideProjectName string `json:"override_project_name"`
}

func (s *Server) proposalAcceptHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.AcceptanceSvc == nil {
		writeError(w, http.StatusNotImplemented, "acceptance_svc_not_wired", "")
		return
	}
	var req proposalAcceptReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	res, err := d.AcceptanceSvc.Accept(r.Context(), wfservice.AcceptCommand{
		ProposalID:          workforce.ProposalID(req.ProposalID),
		OverrideProjectID:   workforce.ProjectID(req.OverrideProjectID),
		OverrideKind:        workforce.ProjectKind(req.OverrideKind),
		OverrideProjectName: req.OverrideProjectName,
		Actor:               d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	evIDs := make([]string, len(res.EventIDs))
	for i, e := range res.EventIDs {
		evIDs[i] = string(e)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"proposal_id":     string(res.ProposalID),
		"mapping_id":      string(res.MappingID),
		"project_id":      string(res.ProjectID),
		"project_created": res.ProjectCreated,
		"event_ids":       evIDs,
	})
}

type proposalIgnoreReq struct {
	ProposalID string `json:"proposal_id"`
}

func (s *Server) proposalIgnoreHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.AcceptanceSvc == nil {
		writeError(w, http.StatusNotImplemented, "acceptance_svc_not_wired", "")
		return
	}
	var req proposalIgnoreReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	evID, err := d.AcceptanceSvc.Ignore(r.Context(), wfservice.IgnoreCommand{
		ProposalID: workforce.ProposalID(req.ProposalID),
		Actor:      d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"event_id": string(evID)})
}

func (s *Server) proposalUnignoreHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.AcceptanceSvc == nil {
		writeError(w, http.StatusNotImplemented, "acceptance_svc_not_wired", "")
		return
	}
	var req proposalIgnoreReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	evID, err := d.AcceptanceSvc.Unignore(r.Context(), wfservice.IgnoreCommand{
		ProposalID: workforce.ProposalID(req.ProposalID),
		Actor:      d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"event_id": string(evID)})
}

// =============================================================================
// AgentMgmtSvc — Create / Archive
// =============================================================================

type agentCreateReq struct {
	Name          string `json:"name"`
	AgentCLI      string `json:"agent_cli"`
	WorkerID      string `json:"worker_id"`
	Config        string `json:"config"`
	MaxConcurrent *int   `json:"max_concurrent"`
}

func (s *Server) agentCreateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.AgentMgmtSvc == nil {
		writeError(w, http.StatusNotImplemented, "agent_mgmt_svc_not_wired", "")
		return
	}
	var req agentCreateReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	res, err := d.AgentMgmtSvc.Create(r.Context(), wfservice.CreateAgentInstanceCommand{
		Name:          req.Name,
		AgentCLI:      req.AgentCLI,
		WorkerID:      workforce.WorkerID(req.WorkerID),
		Config:        req.Config,
		MaxConcurrent: req.MaxConcurrent,
		ActorIdentity: d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":          string(res.ID),
		"identity_id": "agent:" + string(res.ID),
		"event_id":    string(res.EventID),
	})
}

type agentArchiveReq struct {
	ID      string `json:"id"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Version int    `json:"version"`
}

func (s *Server) agentArchiveHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.AgentMgmtSvc == nil {
		writeError(w, http.StatusNotImplemented, "agent_mgmt_svc_not_wired", "")
		return
	}
	var req agentArchiveReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	evID, err := d.AgentMgmtSvc.Archive(r.Context(), wfservice.ArchiveAgentInstanceCommand{
		ID:            workforce.AgentInstanceID(req.ID),
		Reason:        workforce.AgentInstanceArchivedReason(req.Reason),
		Message:       req.Message,
		Version:       req.Version,
		ActorIdentity: d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"event_id": string(evID)})
}

// =============================================================================
// AgentInstanceRepo — FindAll / FindByID / FindByName
// =============================================================================

func (s *Server) agentFindAllHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.AgentInstanceRepo == nil {
		writeError(w, http.StatusNotImplemented, "agent_repo_not_wired", "")
		return
	}
	filter := workforce.AgentInstanceFilter{}
	if v := r.URL.Query().Get("state"); v != "" {
		st := workforce.AgentInstanceState(v)
		filter.State = &st
	}
	if v := r.URL.Query().Get("worker_id"); v != "" {
		wid := workforce.WorkerID(v)
		filter.WorkerID = &wid
	}
	list, err := d.AgentInstanceRepo.FindAll(r.Context(), filter)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, a := range list {
		out[i] = agentInstanceMap(a)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) agentFindByIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.AgentInstanceRepo == nil {
		writeError(w, http.StatusNotImplemented, "agent_repo_not_wired", "")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	a, err := d.AgentInstanceRepo.FindByID(r.Context(), workforce.AgentInstanceID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, agentInstanceMap(a))
}

func (s *Server) agentFindByNameHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.AgentInstanceRepo == nil {
		writeError(w, http.StatusNotImplemented, "agent_repo_not_wired", "")
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing_name", "")
		return
	}
	a, err := d.AgentInstanceRepo.FindByName(r.Context(), name)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, agentInstanceMap(a))
}

// =============================================================================
// ProposalRepo — FindByID / FindByWorkerID / FindPending
// =============================================================================

func (s *Server) proposalFindByIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ProposalRepo == nil {
		writeError(w, http.StatusNotImplemented, "proposal_repo_not_wired", "")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	p, err := d.ProposalRepo.FindByID(r.Context(), workforce.ProposalID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, proposalMap(p))
}

func (s *Server) proposalFindByWorkerIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ProposalRepo == nil {
		writeError(w, http.StatusNotImplemented, "proposal_repo_not_wired", "")
		return
	}
	wid := r.URL.Query().Get("worker_id")
	if wid == "" {
		writeError(w, http.StatusBadRequest, "missing_worker_id", "")
		return
	}
	var statuses []workforce.ProposalStatus
	if st := r.URL.Query().Get("status"); st != "" {
		statuses = append(statuses, workforce.ProposalStatus(st))
	}
	list, err := d.ProposalRepo.FindByWorkerID(r.Context(), workforce.WorkerID(wid), statuses...)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, p := range list {
		out[i] = proposalMap(p)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) proposalFindPendingHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ProposalRepo == nil {
		writeError(w, http.StatusNotImplemented, "proposal_repo_not_wired", "")
		return
	}
	list, err := d.ProposalRepo.FindPending(r.Context())
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, p := range list {
		out[i] = proposalMap(p)
	}
	writeJSON(w, http.StatusOK, out)
}

// =============================================================================
// ProjectRepo — FindAll / FindByID
// =============================================================================

func (s *Server) projectFindAllHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ProjectRepo == nil {
		writeError(w, http.StatusNotImplemented, "project_repo_not_wired", "")
		return
	}
	filter := workforce.ProjectFilter{}
	if v := r.URL.Query().Get("kind"); v != "" {
		k := workforce.ProjectKind(v)
		filter.Kind = &k
	}
	list, err := d.ProjectRepo.FindAll(r.Context(), filter)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, p := range list {
		out[i] = projectMap(p)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) projectFindByIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ProjectRepo == nil {
		writeError(w, http.StatusNotImplemented, "project_repo_not_wired", "")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	p, err := d.ProjectRepo.FindByID(r.Context(), workforce.ProjectID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projectMap(p))
}

// =============================================================================
// ProjectSvc — Add / Remove / Update
// =============================================================================

type projectAddReq struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	DefaultAgentCLI string `json:"default_agent_cli"`
	Description     string `json:"description"`
}

func (s *Server) projectAddHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ProjectSvc == nil {
		writeError(w, http.StatusNotImplemented, "project_svc_not_wired", "")
		return
	}
	var req projectAddReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	res, err := d.ProjectSvc.Add(r.Context(), wfservice.AddCommand{
		ID:              workforce.ProjectID(req.ID),
		Name:            req.Name,
		Kind:            workforce.ProjectKind(req.Kind),
		DefaultAgentCLI: req.DefaultAgentCLI,
		Description:     req.Description,
		Actor:           d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"project":  projectMap(res.Project),
		"event_id": string(res.EventID),
	})
}

type projectRemoveReq struct {
	ID string `json:"id"`
}

func (s *Server) projectRemoveHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ProjectSvc == nil {
		writeError(w, http.StatusNotImplemented, "project_svc_not_wired", "")
		return
	}
	var req projectRemoveReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	evID, err := d.ProjectSvc.Remove(r.Context(), wfservice.RemoveCommand{
		ID:    workforce.ProjectID(req.ID),
		Actor: d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"event_id": string(evID)})
}

type projectUpdateReq struct {
	ID              string  `json:"id"`
	Version         int     `json:"version"`
	Name            *string `json:"name"`
	Kind            *string `json:"kind"`
	DefaultAgentCLI *string `json:"default_agent_cli"`
	Description     *string `json:"description"`
}

func (s *Server) projectUpdateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ProjectSvc == nil {
		writeError(w, http.StatusNotImplemented, "project_svc_not_wired", "")
		return
	}
	var req projectUpdateReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	fields := workforce.ProjectUpdateFields{}
	if req.Name != nil {
		fields.Name = req.Name
	}
	if req.Kind != nil {
		k := workforce.ProjectKind(*req.Kind)
		fields.Kind = &k
	}
	if req.DefaultAgentCLI != nil {
		fields.DefaultAgentCLI = req.DefaultAgentCLI
	}
	if req.Description != nil {
		fields.Description = req.Description
	}
	res, err := d.ProjectSvc.Update(r.Context(), wfservice.UpdateCommand{
		ID:      workforce.ProjectID(req.ID),
		Version: req.Version,
		Fields:  fields,
		Actor:   d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"project":  projectMap(res.Project),
		"event_id": string(res.EventID),
	})
}

// =============================================================================
// WorkerRepo — FindAll / FindByID / FindByStatus
// =============================================================================

func (s *Server) workerFindAllHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.WorkerRepo == nil {
		writeError(w, http.StatusNotImplemented, "worker_repo_not_wired", "")
		return
	}
	list, err := d.WorkerRepo.FindAll(r.Context())
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, ww := range list {
		out[i] = workerMap(ww)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) workerFindByIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.WorkerRepo == nil {
		writeError(w, http.StatusNotImplemented, "worker_repo_not_wired", "")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	ww, err := d.WorkerRepo.FindByID(r.Context(), workforce.WorkerID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workerMap(ww))
}

func (s *Server) workerFindByStatusHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.WorkerRepo == nil {
		writeError(w, http.StatusNotImplemented, "worker_repo_not_wired", "")
		return
	}
	st := r.URL.Query().Get("status")
	if st == "" {
		writeError(w, http.StatusBadRequest, "missing_status", "")
		return
	}
	list, err := d.WorkerRepo.FindByStatus(r.Context(), workforce.WorkerStatus(st))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, ww := range list {
		out[i] = workerMap(ww)
	}
	writeJSON(w, http.StatusOK, out)
}

// =============================================================================
// Projection helpers
// =============================================================================

func workerMap(w *workforce.Worker) map[string]any {
	m := map[string]any{
		"worker_id":    string(w.ID()),
		"status":       string(w.Status()),
		"capabilities": w.Capabilities(),
		"version":      w.Version(),
		"enrolled_at":  w.EnrolledAt().Format(time.RFC3339Nano),
	}
	if hb := w.LastHeartbeatAt(); hb != nil {
		m["last_heartbeat_at"] = hb.Format(time.RFC3339Nano)
	}
	return m
}

func proposalMap(p *workforce.WorkerProjectProposal) map[string]any {
	return map[string]any{
		"proposal_id":          string(p.ID()),
		"worker_id":            string(p.WorkerID()),
		"status":               string(p.Status()),
		"candidate_path":       p.CandidatePath(),
		"suggested_project_id": string(p.SuggestedProjectID()),
		"suggested_kind":       string(p.SuggestedKind()),
		"version":              p.Version(),
	}
}

func projectMap(p *workforce.Project) map[string]any {
	return map[string]any{
		"id":                string(p.ID()),
		"name":              p.Name(),
		"kind":              string(p.Kind()),
		"default_agent_cli": p.DefaultAgentCLI(),
		"description":       p.Description(),
		"version":           p.Version(),
		"created_at":        p.CreatedAt().Format(time.RFC3339Nano),
	}
}

func agentInstanceMap(a *workforce.AgentInstance) map[string]any {
	wid := ""
	if a.WorkerID() != nil {
		wid = string(*a.WorkerID())
	}
	return map[string]any{
		"id":             string(a.ID()),
		"name":           a.Name(),
		"state":          string(a.State()),
		"agent_cli":      a.AgentCLI(),
		"worker_id":      wid,
		"is_builtin":     a.IsBuiltin(),
		"max_concurrent": a.MaxConcurrent(),
		"config":         a.Config(),
		"version":        a.Version(),
		"identity_id":    "agent:" + string(a.ID()),
	}
}
