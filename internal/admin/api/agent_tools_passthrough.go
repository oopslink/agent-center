package api

import (
	"errors"
	"net/http"
	"strconv"
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
	// T199/WS3: optional one-step create→dispatch. Both omitted ⇒ backlog (the
	// pre-T199 default).
	Assignee string `json:"assignee"`
	Dispatch bool   `json:"dispatch"`
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
	// T199/WS3: validate the optional assignee ref shape HERE so a malformed ref is
	// a clear 400 (not an opaque domain 500). Empty = unassigned (no-op).
	assignee := strings.TrimSpace(req.Assignee)
	if assignee != "" {
		if err := pm.IdentityRef(assignee).Validate(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_assignee", err.Error())
			return
		}
	}
	taskID, err := d.PMService.CreateTask(r.Context(), pmservice.CreateTaskCommand{
		ProjectID:        pm.ProjectID(req.ProjectID),
		Title:            req.Title,
		Description:      req.Description,
		DerivedFromIssue: pm.IssueID(req.DerivedFromIssue),
		CreatedBy:        pm.IdentityRef(agentActor(a)),
		Assignee:         pm.IdentityRef(assignee),
		Dispatch:         req.Dispatch,
	})
	if err != nil {
		// T199/WS3 dispatch/assign error paths — clear codes (acceptance: "错误路径
		// 有明确报错(非同项目等)"). A cross-org agent assignee is a 422 (not the
		// default opaque 500); the built-in pool being absent is a server invariant
		// breach surfaced as 501 (pm_not_wired class), not a user error.
		switch {
		case errors.Is(err, pm.ErrCrossOrgAssignee):
			writeError(w, http.StatusUnprocessableEntity, "cross_org_assignee", err.Error())
			return
		case errors.Is(err, pmservice.ErrBuiltinPoolMissing):
			writeError(w, http.StatusNotImplemented, "builtin_pool_missing", err.Error())
			return
		}
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
	m := agentTaskMap(t)
	// ADR-0047 §-1: expose the derived `claimable` on the single-task read too.
	if claimable, cerr := d.PMService.TaskClaimableByID(r.Context(), t.ID()); cerr == nil {
		m["claimable"] = claimable
	}
	writeJSON(w, http.StatusOK, m)
}

// --- list_tasks (v2.9.1 #T38) ------------------------------------------------

type listTasksReq struct {
	AgentID   string   `json:"agent_id"`
	ProjectID string   `json:"project_id"`
	Status    []string `json:"status"`   // optional; one or more task statuses
	Assignee  string   `json:"assignee"` // optional; exact identity ref (agent:x / user:y)
}

// listTasksHandler lists ALL tasks in a project (board overview), optionally
// filtered by status and/or assignee — the MCP `list_tasks` tool. Project-member
// guarded (org-isolation §5.7: a non-member / cross-org project → 404, no
// disclosure). Reuses the agentTaskMap summary (incl. org_ref + plan_id).
func (s *Server) listTasksHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req listTasksReq
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
	tasks, err := d.PMService.ListProjectTasksForMember(r.Context(),
		pm.ProjectID(req.ProjectID), pm.IdentityRef(agentActor(a)))
	if err != nil {
		mapDomainError(w, err) // non-member / not-found → 404 (§5.7)
		return
	}
	// Optional status set (case-sensitive enum values) + exact assignee filter.
	statusSet := map[string]bool{}
	for _, st := range req.Status {
		if s := strings.TrimSpace(st); s != "" {
			statusSet[s] = true
		}
	}
	assignee := strings.TrimSpace(req.Assignee)
	out := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		if len(statusSet) > 0 && !statusSet[string(t.Status())] {
			continue
		}
		if assignee != "" && string(t.Assignee()) != assignee {
			continue
		}
		out = append(out, agentTaskMap(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": out, "total": len(out)})
}

// --- get_issue ---------------------------------------------------------------

// getIssueHandler returns the issue projection — PROJECT-MEMBER read scope
// (v2.10.3 T170 relaxation, owner-approved): the agent may read ANY issue in a
// project it is a member of (#5a project membership now gates issue reads too).
// This REPLACES the prior OQ4 own-link scope (the agent had to hold a WorkItem
// for a Task derived from the issue), which was too tight — a PD/agent could not
// read sibling issues in its own project, and the "open an issue to discuss"
// flow had no read path. A non-member gets ErrNotMember → 403; a missing issue
// gets ErrIssueNotFound → 404. Accepts GET or POST.
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
	i, err := d.PMService.GetIssueForMember(r.Context(), pm.IssueID(issueID), pm.IdentityRef(agentActor(a)))
	if err != nil {
		mapDomainError(w, err) // ErrIssueNotFound → 404, ErrNotMember → 403
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
	m := map[string]any{
		"id": string(t.ID()), "project_id": string(t.ProjectID()), "title": t.Title(),
		"description": t.Description(), "status": string(t.Status()), "assignee": string(t.Assignee()),
		"derived_from_issue": string(t.DerivedFromIssue()), "completed_by": string(t.CompletedBy()),
		"blocked_reason": t.BlockedReason(), "version": t.Version(),
		"plan_id":    string(t.PlanID()), // v2.9.1 #T38: empty = backlog (not selected into a plan)
		"created_at": t.CreatedAt().Format(time.RFC3339Nano), "updated_at": t.UpdatedAt().Format(time.RFC3339Nano),
	}
	if t.OrgNumber() > 0 { // v2.7.1 #245: T<n> display/ref token (omitted when unallocated)
		m["org_ref"] = "T" + strconv.Itoa(t.OrgNumber())
	}
	return m
}

// agentIssueMap projects a pm.Issue to the wire shape used elsewhere (mirrors
// webconsole's pmIssueMap).
func agentIssueMap(i *pm.Issue) map[string]any {
	tags := i.Tags()
	if tags == nil {
		tags = []string{} // always render a JSON array, never null
	}
	m := map[string]any{
		"id": string(i.ID()), "project_id": string(i.ProjectID()), "title": i.Title(),
		"description": i.Description(), "status": string(i.Status()), "created_by": string(i.CreatedBy()),
		"tags":    tags, // v2.10.3 T170: surface the label set to agents
		"version": i.Version(), "created_at": i.CreatedAt().Format(time.RFC3339Nano),
		"updated_at": i.UpdatedAt().Format(time.RFC3339Nano),
	}
	if i.OrgNumber() > 0 { // v2.7.1 #245: I<n> display/ref token
		m["org_ref"] = "I" + strconv.Itoa(i.OrgNumber())
	}
	return m
}
