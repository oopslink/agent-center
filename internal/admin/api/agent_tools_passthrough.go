package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// =============================================================================
// Agent MCP passthrough tools — thin wrappers over the pm AppServices (v2.7 D2
// slice b2/d-ii-B, ADR-0049). These complete the OQ4 tool surface (except files,
// deferred to D3, and the explicit-comms write tools shipped in b2/d-ii).
//
// Every tool goes through requireAgentOnWorker (the b1 guardrail: worker proven
// by the TOKEN OWNER, target agent bound to it). The agent's actor ref is
// `agent:<bare-id>` (agentActor). The WRITE tools pass that actor straight into
// the pm AppService — the AppService's own requireProjectMember is the
// write-gate: an agent assigned a task is a ProjectMember (#5a), so it passes
// for its own projects and gets ErrNotMember (→403) for foreign projects. No
// extra membership check is layered on top.
//
// The READ tools scope per-agent STRICTLY to own work (OQ4) — #5a project
// membership is the OQ6 WRITE gate and does NOT widen reads:
//   - get_task  uses own-work scope (requireOwnTask: the agent must hold a
//     WorkItem for pm://tasks/{taskID}, else 403 not_agents_task).
//   - get_issue uses own-LINK scope: the agent must hold a WorkItem for a Task
//     derived from the issue (task.DerivedFromIssue == issue_id), else 403
//     not_in_issue_domain. (Membership alone does NOT grant reading arbitrary
//     project issues.)
// =============================================================================

// --- create_task -------------------------------------------------------------

type createTaskReq struct {
	AgentID          string `json:"agent_id"`
	ProjectID        string `json:"project_id"`
	Title            string `json:"title"`
	Description      string `json:"description"`
	DerivedFromIssue string `json:"derived_from_issue"`
}

// createTaskHandler creates a Task via pm.CreateTask with actor=agent. The pm
// AppService's requireProjectMember bounds the agent to its own projects (a
// foreign project → ErrNotMember → 403).
func (s *Server) createTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req createTaskReq
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
	if strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "missing_title", "")
		return
	}
	taskID, err := d.PMService.CreateTask(r.Context(), pmservice.CreateTaskCommand{
		ProjectID:        pm.ProjectID(req.ProjectID),
		Title:            req.Title,
		Description:      req.Description,
		DerivedFromIssue: pm.IssueID(req.DerivedFromIssue),
		CreatedBy:        pm.IdentityRef(agentActor(a)),
	})
	if err != nil {
		// v2.7.1 #239: precise, distinct messages — a missing project is "not
		// found" (with the agent's available projects as a hint), NOT the
		// misleading "not a member" (@oopslink screenshot pain). The domain now
		// returns ErrProjectNotFound vs ErrNotMember distinctly (requireProjectMember).
		switch {
		case errors.Is(err, pm.ErrProjectNotFound):
			writeError(w, http.StatusNotFound, "project_not_found",
				"project "+req.ProjectID+" not found"+availableProjectsHint(r.Context(), d, a.OrganizationID(), a.IdentityMemberID()))
			return
		case errors.Is(err, pmservice.ErrNotMember):
			writeError(w, http.StatusForbidden, "not_a_project_member",
				"not a member of project "+req.ProjectID+", please ask an owner to add you")
			return
		}
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task_id": string(taskID)})
}

// --- assign_task / reassign_task ---------------------------------------------

type assignTaskReq struct {
	AgentID  string `json:"agent_id"`
	TaskID   string `json:"task_id"`
	Assignee string `json:"assignee"`
}

// assignTaskHandler serves BOTH assign_task and reassign_task (same logic — pm
// AssignTask handles first-assign vs reassign). actor=agent; the assignee is a
// full identity ref (e.g. agent:X or user:Y). pm.AssignTask's requireProjectMember
// is the write-gate.
func (s *Server) assignTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req assignTaskReq
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
	if strings.TrimSpace(req.TaskID) == "" {
		writeError(w, http.StatusBadRequest, "missing_task_id", "")
		return
	}
	if strings.TrimSpace(req.Assignee) == "" {
		writeError(w, http.StatusBadRequest, "missing_assignee", "")
		return
	}
	if err := d.PMService.AssignTask(r.Context(), pm.TaskID(req.TaskID),
		pm.IdentityRef(req.Assignee), pm.IdentityRef(agentActor(a))); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- subscribe / unsubscribe -------------------------------------------------

type subscribeReq struct {
	AgentID  string `json:"agent_id"`
	TaskID   string `json:"task_id"`
	Identity string `json:"identity"`
}

// subscribeHandler subscribes identity (defaulting to the agent's own ref) to a
// Task via pm.SubscribeTask with actor=agent.
func (s *Server) subscribeHandler(w http.ResponseWriter, r *http.Request) {
	s.subscribeOp(w, r, true)
}

// unsubscribeHandler unsubscribes identity (defaulting to the agent's own ref)
// from a Task via pm.UnsubscribeTask with actor=agent.
func (s *Server) unsubscribeHandler(w http.ResponseWriter, r *http.Request) {
	s.subscribeOp(w, r, false)
}

func (s *Server) subscribeOp(w http.ResponseWriter, r *http.Request, subscribe bool) {
	d := hd(r)
	var req subscribeReq
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
	if strings.TrimSpace(req.TaskID) == "" {
		writeError(w, http.StatusBadRequest, "missing_task_id", "")
		return
	}
	// identity defaults to the agent's own ref when omitted.
	identity := strings.TrimSpace(req.Identity)
	if identity == "" {
		identity = agentActor(a)
	}
	actor := pm.IdentityRef(agentActor(a))
	var err error
	if subscribe {
		err = d.PMService.SubscribeTask(r.Context(), pm.TaskID(req.TaskID), pm.IdentityRef(identity), actor)
	} else {
		err = d.PMService.UnsubscribeTask(r.Context(), pm.TaskID(req.TaskID), pm.IdentityRef(identity), actor)
	}
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- verify_task -------------------------------------------------------------

type verifyTaskReq struct {
	AgentID string `json:"agent_id"`
	TaskID  string `json:"task_id"`
}

// verifyTaskHandler verifies a completed Task via pm.VerifyTask with by=agent.
// The pm AR enforces no-self-verify (ErrSelfVerify when the agent is the
// completer) — mapped to 422 via the existing pm-error mapper.
func (s *Server) verifyTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req verifyTaskReq
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
	if strings.TrimSpace(req.TaskID) == "" {
		writeError(w, http.StatusBadRequest, "missing_task_id", "")
		return
	}
	if err := d.PMService.VerifyTask(r.Context(), pm.TaskID(req.TaskID),
		pm.IdentityRef(agentActor(a))); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- get_task ----------------------------------------------------------------

// getTaskHandler returns the task projection for a task the agent OWNS (own-work
// scope: requireOwnTask — the agent must hold a WorkItem for pm://tasks/{taskID},
// else 403 not_agents_task). Accepts GET (?agent_id=&task_id=) or POST body.
func (s *Server) getTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	agentID, taskID, ok := readAgentTool2(w, r, "task_id")
	if !ok {
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, agentID)
	if !ok {
		return
	}
	if d.PMService == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return
	}
	if !s.requireOwnTask(w, r, d, a, taskID) {
		return
	}
	t, err := d.PMService.GetTask(r.Context(), pm.TaskID(taskID))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, agentTaskMap(t))
}

// --- get_issue ---------------------------------------------------------------

// getIssueHandler returns the issue projection — OWN-ASSOCIATED read scope (OQ4
// strictly-own): the agent may read an issue ONLY if it holds a WorkItem for a
// Task derived from this issue (task.DerivedFromIssue == issue_id), mirroring
// get_task's own-work strictness. NOTE: #5a project membership is the OQ6 WRITE
// gate, NOT a read widening — being a member does NOT grant reading arbitrary
// project issues (that would be a deliberate OQ4 relaxation needing sign-off).
// Else 403 not_in_issue_domain. Accepts GET or POST.
func (s *Server) getIssueHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	agentID, issueID, ok := readAgentTool2(w, r, "issue_id")
	if !ok {
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, agentID)
	if !ok {
		return
	}
	if d.PMService == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return
	}
	i, err := d.PMService.GetIssue(r.Context(), pm.IssueID(issueID))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	// Own-associated scope (OQ4): the agent may read the issue ONLY if it holds a
	// WorkItem for a Task derived from it. Membership (#5a, the WRITE gate) does
	// NOT widen reads to arbitrary project issues.
	items, err := d.AgentWorkItemRepo.ListByAgent(r.Context(), a.ID())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	associated := false
	for _, wi := range items {
		taskID := strings.TrimPrefix(wi.TaskRef(), "pm://tasks/")
		tk, terr := d.PMService.GetTask(r.Context(), pm.TaskID(taskID))
		if terr != nil {
			continue
		}
		if string(tk.DerivedFromIssue()) == issueID {
			associated = true
			break
		}
	}
	if !associated {
		writeError(w, http.StatusForbidden, "not_in_issue_domain",
			"agent may only read an issue its own task derives from (OQ4 own-scope)")
		return
	}
	writeJSON(w, http.StatusOK, agentIssueMap(i))
}

// --- helpers -----------------------------------------------------------------

// readAgentTool2 reads {agent_id, <idKey>} from either the query string (GET) or
// the JSON body (POST) — mirroring "GET with query or POST body". It writes the
// 400 envelope and returns ("","",false) on a missing field or invalid body.
func readAgentTool2(w http.ResponseWriter, r *http.Request, idKey string) (agentID, idVal string, ok bool) {
	if r.Method == http.MethodGet {
		agentID = strings.TrimSpace(r.URL.Query().Get("agent_id"))
		idVal = strings.TrimSpace(r.URL.Query().Get(idKey))
	} else {
		var body map[string]string
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return "", "", false
		}
		agentID = strings.TrimSpace(body["agent_id"])
		idVal = strings.TrimSpace(body[idKey])
	}
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "missing_agent_id", "")
		return "", "", false
	}
	if idVal == "" {
		writeError(w, http.StatusBadRequest, "missing_"+idKey, "")
		return "", "", false
	}
	return agentID, idVal, true
}

// agentTaskMap projects a pm.Task to the wire shape used elsewhere (mirrors
// webconsole's pmTaskMap).
func agentTaskMap(t *pm.Task) map[string]any {
	return map[string]any{
		"id": string(t.ID()), "project_id": string(t.ProjectID()), "title": t.Title(),
		"description": t.Description(), "status": string(t.Status()), "assignee": string(t.Assignee()),
		"derived_from_issue": string(t.DerivedFromIssue()), "completed_by": string(t.CompletedBy()),
		"blocked_reason": t.BlockedReason(), "version": t.Version(),
		"created_at": t.CreatedAt().Format(time.RFC3339Nano), "updated_at": t.UpdatedAt().Format(time.RFC3339Nano),
	}
}

// agentIssueMap projects a pm.Issue to the wire shape used elsewhere (mirrors
// webconsole's pmIssueMap).
func agentIssueMap(i *pm.Issue) map[string]any {
	return map[string]any{
		"id": string(i.ID()), "project_id": string(i.ProjectID()), "title": i.Title(),
		"description": i.Description(), "status": string(i.Status()), "created_by": string(i.CreatedBy()),
		"version": i.Version(), "created_at": i.CreatedAt().Format(time.RFC3339Nano),
		"updated_at": i.UpdatedAt().Format(time.RFC3339Nano),
	}
}
