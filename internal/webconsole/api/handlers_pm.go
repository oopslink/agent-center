package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/identity"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// v2.7 B3 ProjectManager HTTP surface (ADR-0046, nested routes). Project-owned
// resources nest under /api/projects/{project_id}/... so ownership is explicit
// and membership gating is uniform: every project-scoped write goes through the
// pm Service's requireProjectMember. Reads are org-scoped (project must belong
// to the caller's org). channel/dm are NOT here (org-level / independent).

// pmCallerRef maps an authenticated webconsole identity to a pm IdentityRef.
// Webconsole callers are users (JWT session); kind-prefix per ADR-0033.
func pmCallerRef(id *identity.Identity) pm.IdentityRef {
	if id == nil {
		return ""
	}
	if id.Kind() == "agent" {
		return pm.IdentityRef("agent:" + id.ID())
	}
	return pm.IdentityRef("user:" + id.ID())
}

// mapPMError translates ProjectManager errors to HTTP responses.
func mapPMError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pm.ErrProjectNotFound), errors.Is(err, pm.ErrIssueNotFound),
		errors.Is(err, pm.ErrTaskNotFound), errors.Is(err, pm.ErrMemberNotFound),
		errors.Is(err, pm.ErrCodeRepoRefNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, pmservice.ErrNotMember):
		writeError(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, pm.ErrIllegalTransition), errors.Is(err, pm.ErrInvalidStatus),
		errors.Is(err, pm.ErrSelfVerify), errors.Is(err, pm.ErrBlockReasonRequired),
		errors.Is(err, pm.ErrCrossProject), errors.Is(err, pm.ErrEmptyProjectScope),
		errors.Is(err, pm.ErrProjectExists), errors.Is(err, pm.ErrMemberExists),
		errors.Is(err, pm.ErrTaskExists), errors.Is(err, pm.ErrIssueExists):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "pm_error", err.Error())
	}
}

// --- serializers ------------------------------------------------------------

func pmProjectMap(p *pm.Project) map[string]any {
	return map[string]any{
		"id": string(p.ID()), "organization_id": p.OrganizationID(), "name": p.Name(),
		"description": p.Description(), "status": string(p.Status()), "created_by": string(p.CreatedBy()),
		"version": p.Version(), "created_at": p.CreatedAt().Format(time.RFC3339Nano),
		"updated_at": p.UpdatedAt().Format(time.RFC3339Nano),
	}
}

func pmMemberMap(m *pm.ProjectMember) map[string]any {
	return map[string]any{
		"id": string(m.ID()), "project_id": string(m.ProjectID()), "identity_id": string(m.IdentityID()),
		"role": string(m.Role()), "added_by": string(m.AddedBy()), "created_at": m.CreatedAt().Format(time.RFC3339Nano),
	}
}

func pmIssueMap(i *pm.Issue) map[string]any {
	return map[string]any{
		"id": string(i.ID()), "project_id": string(i.ProjectID()), "title": i.Title(),
		"description": i.Description(), "status": string(i.Status()), "created_by": string(i.CreatedBy()),
		"version": i.Version(), "created_at": i.CreatedAt().Format(time.RFC3339Nano),
		"updated_at": i.UpdatedAt().Format(time.RFC3339Nano),
	}
}

func pmTaskMap(t *pm.Task) map[string]any {
	return map[string]any{
		"id": string(t.ID()), "project_id": string(t.ProjectID()), "title": t.Title(),
		"description": t.Description(), "status": string(t.Status()), "assignee": string(t.Assignee()),
		"derived_from_issue": string(t.DerivedFromIssue()), "completed_by": string(t.CompletedBy()),
		"blocked_reason": t.BlockedReason(), "version": t.Version(),
		"created_at": t.CreatedAt().Format(time.RFC3339Nano), "updated_at": t.UpdatedAt().Format(time.RFC3339Nano),
	}
}

func pmCodeRepoMap(c *pm.CodeRepoRef) map[string]any {
	return map[string]any{
		"id": c.ID(), "project_id": string(c.ProjectID()), "url": c.URL(), "label": c.Label(),
		"added_by": string(c.AddedBy()), "created_at": c.CreatedAt().Format(time.RFC3339Nano),
	}
}

// --- gates ------------------------------------------------------------------

// pmRequireProjectInOrg resolves {project_id}, requires org membership, and
// verifies the project belongs to the caller's org (cross-org → 404). Returns
// the project + caller ref.
func (s *Server) pmRequireProjectInOrg(w http.ResponseWriter, r *http.Request, d HandlerDeps) (*pm.Project, pm.IdentityRef, bool) {
	if d.PM == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "ProjectManager service not wired")
		return nil, "", false
	}
	callerID, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return nil, "", false
	}
	p, err := d.PM.GetProject(r.Context(), pm.ProjectID(r.PathValue("project_id")))
	if err != nil || p.OrganizationID() != orgID {
		writeError(w, http.StatusNotFound, "not_found", "project not found in this organization")
		return nil, "", false
	}
	return p, pmCallerRef(callerID), true
}

// --- Project (org-scoped) ---------------------------------------------------

func (s *Server) pmListProjectsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.PM == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	ps, err := d.PM.ListProjects(r.Context(), orgID)
	if err != nil {
		mapPMError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(ps))
	for _, p := range ps {
		out = append(out, pmProjectMap(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": out})
}

func (s *Server) pmCreateProjectHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.PM == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return
	}
	callerID, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	var req struct{ Name, Description string }
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	id, err := d.PM.CreateProject(r.Context(), pmservice.CreateProjectCommand{OrganizationID: orgID, Name: req.Name, Description: req.Description, CreatedBy: pmCallerRef(callerID)})
	if err != nil {
		mapPMError(w, err)
		return
	}
	p, _ := d.PM.GetProject(r.Context(), id)
	writeJSON(w, http.StatusOK, pmProjectMap(p))
}

func (s *Server) pmGetProjectHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	p, _, ok := s.pmRequireProjectInOrg(w, r, d)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, pmProjectMap(p))
}

func (s *Server) pmUpdateProjectHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	p, caller, ok := s.pmRequireProjectInOrg(w, r, d)
	if !ok {
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := d.PM.UpdateProject(r.Context(), pmservice.UpdateProjectCommand{
		ProjectID: p.ID(), Name: req.Name, Description: req.Description, Actor: caller,
	}); err != nil {
		mapPMError(w, err)
		return
	}
	got, _ := d.PM.GetProject(r.Context(), p.ID())
	writeJSON(w, http.StatusOK, pmProjectMap(got))
}

// pmArchiveProjectHandler handles DELETE /api/projects/{project_id} as a
// lifecycle archive (active→archived), NOT a hard delete.
func (s *Server) pmArchiveProjectHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	p, caller, ok := s.pmRequireProjectInOrg(w, r, d)
	if !ok {
		return
	}
	if err := d.PM.ArchiveProject(r.Context(), p.ID(), caller); err != nil {
		mapPMError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "archived"})
}

// --- Members (nested) -------------------------------------------------------

func (s *Server) pmListMembersHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	p, _, ok := s.pmRequireProjectInOrg(w, r, d)
	if !ok {
		return
	}
	ms, err := d.PM.ListMembers(r.Context(), p.ID())
	if err != nil {
		mapPMError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(ms))
	for _, m := range ms {
		out = append(out, pmMemberMap(m))
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": out})
}

func (s *Server) pmAddMemberHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	p, caller, ok := s.pmRequireProjectInOrg(w, r, d)
	if !ok {
		return
	}
	var req struct {
		IdentityID string `json:"identity_id"`
		Role       string `json:"role"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if _, err := d.PM.AddProjectMember(r.Context(), pmservice.AddProjectMemberCommand{ProjectID: p.ID(), IdentityID: pm.IdentityRef(req.IdentityID), Role: pm.ProjectMemberRole(req.Role), Actor: caller}); err != nil {
		mapPMError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- Issues (nested) --------------------------------------------------------

func (s *Server) pmListIssuesHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	p, _, ok := s.pmRequireProjectInOrg(w, r, d)
	if !ok {
		return
	}
	is, err := d.PM.ListIssues(r.Context(), p.ID())
	if err != nil {
		mapPMError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(is))
	for _, i := range is {
		out = append(out, pmIssueMap(i))
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": out})
}

func (s *Server) pmCreateIssueHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	p, caller, ok := s.pmRequireProjectInOrg(w, r, d)
	if !ok {
		return
	}
	var req struct{ Title, Description string }
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	id, err := d.PM.CreateIssue(r.Context(), pmservice.CreateIssueCommand{ProjectID: p.ID(), Title: req.Title, Description: req.Description, CreatedBy: caller})
	if err != nil {
		mapPMError(w, err)
		return
	}
	i, _ := d.PM.GetIssue(r.Context(), id)
	writeJSON(w, http.StatusOK, pmIssueMap(i))
}

func (s *Server) pmTransitionIssueHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	p, caller, ok := s.pmRequireProjectInOrg(w, r, d)
	if !ok {
		return
	}
	issueID := pm.IssueID(r.PathValue("issue_id"))
	i, err := d.PM.GetIssue(r.Context(), issueID)
	if err != nil || i.ProjectID() != p.ID() {
		writeError(w, http.StatusNotFound, "not_found", "issue not found in this project")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := d.PM.TransitionIssue(r.Context(), issueID, pm.IssueStatus(req.Status), caller); err != nil {
		mapPMError(w, err)
		return
	}
	got, _ := d.PM.GetIssue(r.Context(), issueID)
	writeJSON(w, http.StatusOK, pmIssueMap(got))
}

// pmRequireIssueInProject resolves {project_id}+{issue_id}, verifying both org
// membership and that the issue belongs to the path project.
func (s *Server) pmRequireIssueInProject(w http.ResponseWriter, r *http.Request, d HandlerDeps) (*pm.Issue, pm.IdentityRef, bool) {
	p, caller, ok := s.pmRequireProjectInOrg(w, r, d)
	if !ok {
		return nil, "", false
	}
	i, err := d.PM.GetIssue(r.Context(), pm.IssueID(r.PathValue("issue_id")))
	if err != nil || i.ProjectID() != p.ID() {
		writeError(w, http.StatusNotFound, "not_found", "issue not found in this project")
		return nil, "", false
	}
	return i, caller, true
}

func (s *Server) pmGetIssueHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	i, _, ok := s.pmRequireIssueInProject(w, r, d)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, pmIssueMap(i))
}

func (s *Server) pmUpdateIssueHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	i, caller, ok := s.pmRequireIssueInProject(w, r, d)
	if !ok {
		return
	}
	var req struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := d.PM.UpdateIssue(r.Context(), pmservice.UpdateIssueCommand{
		IssueID: i.ID(), Title: req.Title, Description: req.Description, Actor: caller,
	}); err != nil {
		mapPMError(w, err)
		return
	}
	got, _ := d.PM.GetIssue(r.Context(), i.ID())
	writeJSON(w, http.StatusOK, pmIssueMap(got))
}

// --- Tasks (nested) ---------------------------------------------------------

func (s *Server) pmListTasksHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	p, _, ok := s.pmRequireProjectInOrg(w, r, d)
	if !ok {
		return
	}
	ts, err := d.PM.ListTasks(r.Context(), p.ID())
	if err != nil {
		mapPMError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(ts))
	for _, t := range ts {
		out = append(out, pmTaskMap(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": out})
}

func (s *Server) pmCreateTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	p, caller, ok := s.pmRequireProjectInOrg(w, r, d)
	if !ok {
		return
	}
	var req struct {
		Title            string `json:"title"`
		Description      string `json:"description"`
		DerivedFromIssue string `json:"derived_from_issue"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	id, err := d.PM.CreateTask(r.Context(), pmservice.CreateTaskCommand{ProjectID: p.ID(), Title: req.Title, Description: req.Description, DerivedFromIssue: pm.IssueID(req.DerivedFromIssue), CreatedBy: caller})
	if err != nil {
		mapPMError(w, err)
		return
	}
	t, _ := d.PM.GetTask(r.Context(), id)
	writeJSON(w, http.StatusOK, pmTaskMap(t))
}

func (s *Server) pmGetTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	t, _, ok := s.pmRequireTaskInProject(w, r, d)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, pmTaskMap(t))
}

func (s *Server) pmUpdateTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	t, caller, ok := s.pmRequireTaskInProject(w, r, d)
	if !ok {
		return
	}
	var req struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := d.PM.UpdateTask(r.Context(), pmservice.UpdateTaskCommand{
		TaskID: t.ID(), Title: req.Title, Description: req.Description, Actor: caller,
	}); err != nil {
		mapPMError(w, err)
		return
	}
	got, _ := d.PM.GetTask(r.Context(), t.ID())
	writeJSON(w, http.StatusOK, pmTaskMap(got))
}

// pmRequireTaskInProject resolves {project_id}+{task_id}, verifying both org
// membership and that the task belongs to the path project.
func (s *Server) pmRequireTaskInProject(w http.ResponseWriter, r *http.Request, d HandlerDeps) (*pm.Task, pm.IdentityRef, bool) {
	p, caller, ok := s.pmRequireProjectInOrg(w, r, d)
	if !ok {
		return nil, "", false
	}
	t, err := d.PM.GetTask(r.Context(), pm.TaskID(r.PathValue("task_id")))
	if err != nil || t.ProjectID() != p.ID() {
		writeError(w, http.StatusNotFound, "not_found", "task not found in this project")
		return nil, "", false
	}
	return t, caller, true
}

// pmTaskAction runs a task sub-action then returns the refreshed task.
func (s *Server) pmTaskAction(w http.ResponseWriter, r *http.Request, run func(taskID pm.TaskID, caller pm.IdentityRef) error) {
	d := hd(r)
	t, caller, ok := s.pmRequireTaskInProject(w, r, d)
	if !ok {
		return
	}
	if err := run(t.ID(), caller); err != nil {
		mapPMError(w, err)
		return
	}
	got, _ := d.PM.GetTask(r.Context(), t.ID())
	writeJSON(w, http.StatusOK, pmTaskMap(got))
}

func (s *Server) pmAssignTaskHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Assignee string `json:"assignee"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	d := hd(r)
	s.pmTaskAction(w, r, func(id pm.TaskID, caller pm.IdentityRef) error {
		return d.PM.AssignTask(r.Context(), id, pm.IdentityRef(req.Assignee), caller)
	})
}

func (s *Server) pmStartTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	s.pmTaskAction(w, r, func(id pm.TaskID, c pm.IdentityRef) error { return d.PM.StartTask(r.Context(), id, c) })
}

func (s *Server) pmBlockTaskHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	d := hd(r)
	s.pmTaskAction(w, r, func(id pm.TaskID, c pm.IdentityRef) error { return d.PM.BlockTask(r.Context(), id, req.Reason, c) })
}

func (s *Server) pmUnblockTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	s.pmTaskAction(w, r, func(id pm.TaskID, c pm.IdentityRef) error { return d.PM.UnblockTask(r.Context(), id, c) })
}

func (s *Server) pmCompleteTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	s.pmTaskAction(w, r, func(id pm.TaskID, c pm.IdentityRef) error { return d.PM.CompleteTask(r.Context(), id, c) })
}

func (s *Server) pmVerifyTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	s.pmTaskAction(w, r, func(id pm.TaskID, c pm.IdentityRef) error { return d.PM.VerifyTask(r.Context(), id, c) })
}

func (s *Server) pmCancelTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	s.pmTaskAction(w, r, func(id pm.TaskID, c pm.IdentityRef) error { return d.PM.CancelTask(r.Context(), id, c) })
}

func (s *Server) pmSubscribeTaskHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IdentityID string `json:"identity_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	d := hd(r)
	s.pmTaskAction(w, r, func(id pm.TaskID, c pm.IdentityRef) error {
		return d.PM.SubscribeTask(r.Context(), id, pm.IdentityRef(req.IdentityID), c)
	})
}

// --- Code repo refs (nested) ------------------------------------------------

func (s *Server) pmListCodeReposHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	p, _, ok := s.pmRequireProjectInOrg(w, r, d)
	if !ok {
		return
	}
	cs, err := d.PM.ListCodeRepos(r.Context(), p.ID())
	if err != nil {
		mapPMError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(cs))
	for _, c := range cs {
		out = append(out, pmCodeRepoMap(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"code_repos": out})
}
