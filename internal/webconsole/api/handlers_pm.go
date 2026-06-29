package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/autoassign"
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
	case errors.Is(err, pmservice.ErrNotMember), errors.Is(err, pmservice.ErrNotOwner):
		writeError(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, pmservice.ErrCannotRemoveOwner):
		writeError(w, http.StatusConflict, "cannot_remove_owner", err.Error())
	case errors.Is(err, pm.ErrProjectArchived):
		// v2.9 #297: archived project is read-only (irreversible) — every child
		// mutation rejects with 409, cross-surface (mirrors ErrPlanArchived).
		writeError(w, http.StatusConflict, "project_archived", err.Error())
	case errors.Is(err, pm.ErrIllegalTransition), errors.Is(err, pm.ErrInvalidStatus),
		errors.Is(err, pm.ErrBlockReasonRequired),
		errors.Is(err, pm.ErrCrossProject), errors.Is(err, pm.ErrEmptyProjectScope),
		errors.Is(err, pm.ErrProjectExists), errors.Is(err, pm.ErrMemberExists),
		errors.Is(err, pm.ErrTaskExists), errors.Is(err, pm.ErrIssueExists),
		errors.Is(err, pm.ErrDerivedIssueProjectMismatch): // T192: cross-project derived_from_issue link
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
	tags := i.Tags()
	if tags == nil {
		tags = []string{}
	}
	m := map[string]any{
		"id": string(i.ID()), "project_id": string(i.ProjectID()), "title": i.Title(),
		"description": i.Description(), "status": string(i.Status()), "created_by": string(i.CreatedBy()),
		"version": i.Version(), "created_at": i.CreatedAt().Format(time.RFC3339Nano),
		"updated_at": i.UpdatedAt().Format(time.RFC3339Nano),
		"tags":       tags, "status_changed_at": rfc3339OrEmpty(i.StatusChangedAt()),
	}
	if ref := orgRefToken("I", i.OrgNumber()); ref != "" {
		m["org_ref"] = ref
	}
	return m
}

func pmTaskMap(t *pm.Task) map[string]any {
	tags := t.Tags()
	if tags == nil {
		tags = []string{}
	}
	m := map[string]any{
		"id": string(t.ID()), "project_id": string(t.ProjectID()), "title": t.Title(),
		"description": t.Description(), "status": string(t.Status()), "assignee": string(t.Assignee()),
		"derived_from_issue": string(t.DerivedFromIssue()), "completed_by": string(t.CompletedBy()),
		"blocked_reason": t.BlockedReason(), "version": t.Version(),
		"created_at": t.CreatedAt().Format(time.RFC3339Nano), "updated_at": t.UpdatedAt().Format(time.RFC3339Nano),
		"tags": tags, "status_changed_at": rfc3339OrEmpty(t.StatusChangedAt()),
		// v2.9 P3 Stage B: orthogonal archived state (independent of status) — the
		// archive FE renders an "已归档" badge + read-only affordance off these.
		"archived": t.IsArchived(), "archived_by": string(t.ArchivedBy()),
		"archived_at": rfc3339OrEmptyPtr(t.ArchivedAt()),
		// v2.18.3 BE-1: capability requirements the BE-2 auto-assign reconciler reads
		// (always present as an array; [] = unrestricted).
		"required_capabilities": capsOrEmpty(t.RequiredCapabilities()),
	}
	if ref := orgRefToken("T", t.OrgNumber()); ref != "" {
		m["org_ref"] = ref
	}
	// T106: the task's plan association (empty for a backlog task) so the Task
	// detail sidebar can show + link to the owning plan. Omitted when empty.
	if pid := string(t.PlanID()); pid != "" {
		m["plan_id"] = pid
	}
	// v2.13.0 I18/F2: cycle-node git metadata — present only for scaffolded nodes
	// (branch/base set); ordinary tasks stay clean. F3/F4 + the FE board read these.
	if t.Branch() != "" || t.Base() != "" || t.SkipMergeCheck() {
		m["branch"] = t.Branch()
		m["base"] = t.Base()
		m["skip_merge_check"] = t.SkipMergeCheck()
	}
	// F3 model routing (design §5 & §10): per-task executor model override, emitted
	// only when set so ordinary tasks stay clean.
	if t.Model() != "" {
		m["model"] = t.Model()
	}
	return m
}

// capsOrEmpty renders a capability slice as a non-nil JSON array ([] for nil/empty),
// so required_capabilities always serializes as an array (v2.18.3 BE-1).
func capsOrEmpty(caps []string) []string {
	if caps == nil {
		return []string{}
	}
	return caps
}

// orEmptyTags returns the tag slice, or a non-nil empty slice so the DTO emits
// [] rather than null.
func orEmptyTags(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}

// rfc3339OrEmpty formats a timestamp as RFC3339Nano, or "" when zero.
func rfc3339OrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

// rfc3339OrEmptyPtr formats an optional timestamp as RFC3339Nano, or "" when
// nil/zero (the orthogonal archived_at is nil for a never-archived task).
func rfc3339OrEmptyPtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return rfc3339OrEmpty(*t)
}

// orgRefToken renders the v2.7.1 #245 display/reference token ("T<n>"/"I<n>"),
// or "" when no number is allocated (rows predating the migration backfill) so
// the DTO omits org_ref and the UI gracefully falls back to the hash handle.
func orgRefToken(prefix string, n int) string {
	if n <= 0 {
		return ""
	}
	return prefix + strconv.Itoa(n)
}

func pmCodeRepoMap(c *pm.CodeRepoRef) map[string]any {
	return map[string]any{
		"id": c.ID(), "project_id": string(c.ProjectID()), "url": c.URL(), "label": c.Label(),
		"added_by": string(c.AddedBy()), "created_at": c.CreatedAt().Format(time.RFC3339Nano),
		// v2.18.4 BE-1: workspace-Repo reference fields. repo_id "" = legacy url-only ref.
		"repo_id": c.RepoID(), "is_primary": c.IsPrimary(),
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
	// v2.9 #298 (@oopslink): the project LIST excludes archived by DEFAULT — sidebar
	// and default views don't show archived projects. ?status=archived returns only
	// archived (Projects-page "Archived" group); ?status=all returns both. (A single
	// project GET still reads an archived project — only the list filters.)
	status := r.URL.Query().Get("status")
	out := make([]map[string]any, 0, len(ps))
	for _, p := range ps {
		archived := p.Status() == pm.ProjectArchived
		switch status {
		case "archived":
			if !archived {
				continue
			}
		case "all":
			// keep both active + archived
		default: // "" or "active" → default excludes archived
			if archived {
				continue
			}
		}
		m := pmProjectMap(p)
		// v2.10.0 #T81 (§3.4.1, finding D1): the Projects list cards show
		// per-project counts (tasks/issues/plans/repos) — the mockup's
		// "12 tasks · 3 issues · 4 plans · 2 repos" meta line. The single-project
		// GET (pmGetProjectHandler) stays count-free; only the LIST carries them.
		// N is bounded by an org's project count and each read is an indexed
		// project_id scan, so the per-project fan-out is acceptable here.
		counts, err := pmProjectCounts(r.Context(), d, p.ID())
		if err != nil {
			mapPMError(w, err)
			return
		}
		m["task_count"] = counts.tasks
		m["issue_count"] = counts.issues
		m["plan_count"] = counts.plans
		m["repo_count"] = counts.repos
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": out})
}

// pmProjectCountsResult carries the per-project list-card counts (§3.4.1).
type pmProjectCountsResult struct{ tasks, issues, plans, repos int }

// pmProjectCounts returns the task/issue/plan/repo counts for one project,
// reusing the existing PM list reads (len of each). Any read error is returned
// so the list handler fails loudly rather than reporting a silently-wrong 0.
func pmProjectCounts(ctx context.Context, d HandlerDeps, id pm.ProjectID) (pmProjectCountsResult, error) {
	tasks, err := d.PM.ListTasks(ctx, id)
	if err != nil {
		return pmProjectCountsResult{}, err
	}
	issues, err := d.PM.ListIssues(ctx, id)
	if err != nil {
		return pmProjectCountsResult{}, err
	}
	// Plan orchestration is OPTIONAL (v2.9 #284): a deployment with no
	// PlanRepository wired returns ErrPlansUnavailable. That is a "feature not
	// wired" sentinel, not a data error — degrade the plan count to 0 rather
	// than failing the whole projects list. Production always wires it (app.go).
	var planCount int
	plans, err := d.PM.ListPlans(ctx, id)
	switch {
	case err == nil:
		planCount = len(plans)
	case errors.Is(err, pmservice.ErrPlansUnavailable):
		planCount = 0
	default:
		return pmProjectCountsResult{}, err
	}
	repos, err := d.PM.ListCodeRepos(ctx, id)
	if err != nil {
		return pmProjectCountsResult{}, err
	}
	return pmProjectCountsResult{
		tasks:  len(tasks),
		issues: len(issues),
		plans:  planCount,
		repos:  len(repos),
	}, nil
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
	writeJSON(w, http.StatusOK, projectMapWithAutoAssign(r.Context(), d, p))
}

// projectMapWithAutoAssign builds the project DTO and enriches it with the
// v2.18.3 BE-1 project-level auto-assign master switch read from the settings store
// (absent ⇒ ON, decision 1). A settings-store hiccup defaults to ON rather than
// failing the read.
func projectMapWithAutoAssign(ctx context.Context, d HandlerDeps, p *pm.Project) map[string]any {
	m := pmProjectMap(p)
	enabled, _ := autoassign.Enabled(ctx, d.SettingsStore, string(p.ID()))
	m["auto_assign_enabled"] = enabled
	return m
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
		// v2.18.3 BE-1: project-level auto-assign master switch. nil → unchanged.
		// Persisted to the settings store (not the project row).
		AutoAssignEnabled *bool `json:"auto_assign_enabled"`
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
	if req.AutoAssignEnabled != nil {
		if d.SettingsStore == nil {
			writeError(w, http.StatusNotImplemented, "not_configured", "settings store not configured")
			return
		}
		if err := autoassign.SetEnabled(r.Context(), d.SettingsStore, string(p.ID()), *req.AutoAssignEnabled); err != nil {
			writeError(w, http.StatusInternalServerError, "settings_write_failed", err.Error())
			return
		}
	}
	got, _ := d.PM.GetProject(r.Context(), p.ID())
	writeJSON(w, http.StatusOK, projectMapWithAutoAssign(r.Context(), d, got))
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

// pmRemoveMemberHandler handles DELETE /api/projects/{project_id}/members/{identity_id}
// (v2.7 #207/#208). identity_id is the prefixed ref (user:.../agent:...); the
// net/http ServeMux unescapes the {identity_id} path wildcard, so the frontend
// sends encodeURIComponent(identity_id) and the colon round-trips. Owner-only +
// owner-protected; see Service.RemoveProjectMember.
func (s *Server) pmRemoveMemberHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	p, caller, ok := s.pmRequireProjectInOrg(w, r, d)
	if !ok {
		return
	}
	identityID := r.PathValue("identity_id")
	if identityID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "identity_id required")
		return
	}
	if err := d.PM.RemoveProjectMember(r.Context(), pmservice.RemoveProjectMemberCommand{
		ProjectID: p.ID(), IdentityID: pm.IdentityRef(identityID), Actor: caller,
	}); err != nil {
		// Honor the #207/#208 contract code the frontend keys on: the target not
		// being a member is `not_member` (404), distinct from the generic
		// not_found mapPMError would emit for ErrMemberNotFound.
		if errors.Is(err, pm.ErrMemberNotFound) {
			writeError(w, http.StatusNotFound, "not_member", "identity is not a member of this project")
			return
		}
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
	// T131/T302: the project Issue list accepts the SAME filter params as the
	// org-wide Issue list (status + assignee + q + time-range) PLUS sort + page,
	// all pushed to SQL via ListIssuesOrgPage scoped to this one project. Pagination
	// is OPTIONAL — with no page_size the repo returns every row (back-compat for
	// callers that ignore `total`). Default (no status) EXCLUDES terminal issues.
	q := pm.OrgListQuery{ProjectIDs: []pm.ProjectID{p.ID()}}
	if err := applyListFilters(r, &q, issueTerminalStatus); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	// Issues are never assignable; an explicit assignee filter excludes them all.
	if strings.TrimSpace(r.URL.Query().Get("assignee")) != "" {
		writeJSON(w, http.StatusOK, map[string]any{"issues": []any{}, "total": 0})
		return
	}
	is, total, err := d.PM.ListIssuesOrgPage(r.Context(), q)
	if err != nil {
		mapPMError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(is))
	for _, i := range is {
		out = append(out, pmIssueMap(i))
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": out, "total": total})
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
	// ?unplanned=1|true restricts to the Backlog (v2.9 Work Board): tasks not yet
	// selected into any Plan (empty plan_id). It is the kanban backlog POOL — it
	// must return EVERY matching task (no pagination), so it keeps the in-memory
	// filter path. The normal project Task LIST (below) uses SQL pagination/sort.
	if u := r.URL.Query().Get("unplanned"); u == "1" || u == "true" {
		ts, err := d.PM.ListUnplannedTasks(r.Context(), p.ID())
		if err != nil {
			mapPMError(w, err)
			return
		}
		statusFilter := parseSetParam(r, "status")
		assigneeFilter := strings.TrimSpace(r.URL.Query().Get("assignee"))
		tf, terr := parseTimeFilter(r)
		if terr != nil {
			writeError(w, http.StatusBadRequest, "invalid_filter", terr.Error())
			return
		}
		out := make([]map[string]any, 0, len(ts))
		for _, t := range ts {
			if !statusPasses(string(t.Status()), statusFilter, taskTerminalStatus) {
				continue
			}
			if assigneeFilter != "" && !assigneeMatches(string(t.Assignee()), assigneeFilter) {
				continue
			}
			if !tf.passes(t.CreatedAt(), t.UpdatedAt()) {
				continue
			}
			out = append(out, pmTaskMap(t))
		}
		writeJSON(w, http.StatusOK, map[string]any{"tasks": out, "total": len(out)})
		return
	}
	// T131/T302: the project Task LIST accepts the SAME filter params as the
	// org-wide Task list (status + assignee + q + time-range) PLUS sort + page,
	// pushed to SQL via ListTasksOrgPage scoped to this one project. Default (no
	// status) EXCLUDES terminal tasks; pagination is optional (no page_size → all).
	q := pm.OrgListQuery{ProjectIDs: []pm.ProjectID{p.ID()}}
	if err := applyListFilters(r, &q, taskTerminalStatus); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	q.Assignee = strings.TrimSpace(r.URL.Query().Get("assignee"))
	ts, total, err := d.PM.ListTasksOrgPage(r.Context(), q)
	if err != nil {
		mapPMError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(ts))
	for _, t := range ts {
		out = append(out, pmTaskMap(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": out, "total": total})
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
		// F3 model routing (design §5 & §10): optional per-task executor model override.
		Model string `json:"model"`
		// v2.18.3 BE-1: optional capability requirements (canonicalized by the domain).
		RequiredCapabilities []string `json:"required_capabilities"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	id, err := d.PM.CreateTask(r.Context(), pmservice.CreateTaskCommand{ProjectID: p.ID(), Title: req.Title, Description: req.Description, DerivedFromIssue: pm.IssueID(req.DerivedFromIssue), CreatedBy: caller, Model: strings.TrimSpace(req.Model), RequiredCapabilities: req.RequiredCapabilities})
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

// issueIDPtr converts an optional JSON string to an optional pm.IssueID, preserving
// nil (= field absent → unchanged). A present "" maps to a non-nil "" (= clear the
// link). T192 — used by the task update/batch-patch handlers for derived_from_issue.
func issueIDPtr(s *string) *pm.IssueID {
	if s == nil {
		return nil
	}
	id := pm.IssueID(*s)
	return &id
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
		// DerivedFromIssue (T192): nil = unchanged; "" = clear the link; an id (re)links.
		DerivedFromIssue *string `json:"derived_from_issue"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := d.PM.UpdateTask(r.Context(), pmservice.UpdateTaskCommand{
		TaskID: t.ID(), Title: req.Title, Description: req.Description,
		DerivedFromIssue: issueIDPtr(req.DerivedFromIssue), Actor: caller,
	}); err != nil {
		mapPMError(w, err)
		return
	}
	got, _ := d.PM.GetTask(r.Context(), t.ID())
	writeJSON(w, http.StatusOK, pmTaskMap(got))
}

// pmBatchUpdateTaskHandler applies any subset of {status, assignee, tags} to a
// task in one atomic tx (v2.8.1 edit-task #278). A field absent from the JSON
// body (nil pointer) is left unchanged; assignee:"" unassigns. This is the bare
// PATCH on the task; the typed sub-routes (.../assign, .../status, ...) remain.
func (s *Server) pmBatchUpdateTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	t, caller, ok := s.pmRequireTaskInProject(w, r, d)
	if !ok {
		return
	}
	var req struct {
		Status      *string   `json:"status"`
		Assignee    *string   `json:"assignee"`
		Tags        *[]string `json:"tags"`
		Title       *string   `json:"title"`
		Description *string   `json:"description"`
		// DerivedFromIssue (T192): nil = unchanged; "" = clear the link; an id (re)links.
		DerivedFromIssue *string `json:"derived_from_issue"`
		// v2.18.3 BE-1: nil = unchanged; non-nil replaces (empty clears → unrestricted).
		RequiredCapabilities *[]string `json:"required_capabilities"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := d.PM.BatchUpdateTask(r.Context(), t.ID(), pmservice.BatchTaskPatch{
		Status: req.Status, Assignee: req.Assignee, Tags: req.Tags,
		Title: req.Title, Description: req.Description,
		DerivedFromIssue:     issueIDPtr(req.DerivedFromIssue),
		RequiredCapabilities: req.RequiredCapabilities,
	}, caller); err != nil {
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
	s.pmTaskAction(w, r, func(id pm.TaskID, c pm.IdentityRef) error {
		return d.PM.BlockTask(r.Context(), id, req.Reason, pm.BlockReasonObstacle, c)
	})
}

// pmUnblockTaskHandler — POST /tasks/{task_id}/unblock {input_request_message_id?,
// comment?}: clears a stuck (blocked_reason) annotation + re-dispatches the task
// (v2.14.0 I14/F6 §13.C). Both body fields are OPTIONAL: an obstacle block uses
// `comment` (the owner's resolution); an input_required block uses `comment` as
// the user's reply and may thread it under the original input_request via
// `input_request_message_id`. The reply→Conversation input_reply write is handled
// by the TaskInputConversationProjector (ADR-0052 outbox purity).
//
// TODO(F6): strict validation that input_request_message_id belongs to the task
// conversation + is unanswered is SKIPPED here — the projector's threading is
// idempotent (a stray id just yields a top-level/mismatched reply, never a write
// to another task), so the §7 easy-read validation is left as a follow-up.
func (s *Server) pmUnblockTaskHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InputRequestMessageID string `json:"input_request_message_id"`
		Comment               string `json:"comment"`
	}
	// Body is optional (empty body ⇒ obstacle unblock with no comment); only a
	// malformed non-empty body is rejected.
	if err := decodeJSON(r, &req); err != nil && r.ContentLength != 0 {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	d := hd(r)
	s.pmTaskAction(w, r, func(id pm.TaskID, c pm.IdentityRef) error {
		return d.PM.UnblockTask(r.Context(), pmservice.UnblockTaskCommand{
			TaskID:                id,
			Comment:               req.Comment,
			InputRequestMessageID: req.InputRequestMessageID,
			Actor:                 c,
		})
	})
}

func (s *Server) pmCompleteTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	s.pmTaskAction(w, r, func(id pm.TaskID, c pm.IdentityRef) error { return d.PM.CompleteTask(r.Context(), id, c) })
}

func (s *Server) pmDiscardTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	s.pmTaskAction(w, r, func(id pm.TaskID, c pm.IdentityRef) error { return d.PM.DiscardTask(r.Context(), id, c) })
}

// pmSetTaskStatusHandler — POST /tasks/{task_id}/status {status}: free status set
// (v2.8.1 @oopslink: any VALID target, NO adjacency — the Change-status menu
// offers the full enum). 400 on an invalid enum value; 404/403 via the resolver.
func (s *Server) pmSetTaskStatusHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	t, caller, ok := s.pmRequireTaskInProject(w, r, d)
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := d.PM.SetTaskStatus(r.Context(), t.ID(), pm.TaskStatus(req.Status), caller); err != nil {
		mapPMError(w, err)
		return
	}
	got, _ := d.PM.GetTask(r.Context(), t.ID())
	writeJSON(w, http.StatusOK, pmTaskMap(got))
}

// pmSetIssueStatusHandler — POST /issues/{issue_id}/status {status}: free status
// set (v2.8.1, any VALID target, NO adjacency). Symmetric with the task path.
func (s *Server) pmSetIssueStatusHandler(w http.ResponseWriter, r *http.Request) {
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
	if err := d.PM.SetIssueStatus(r.Context(), issueID, pm.IssueStatus(req.Status), caller); err != nil {
		mapPMError(w, err)
		return
	}
	got, _ := d.PM.GetIssue(r.Context(), issueID)
	writeJSON(w, http.StatusOK, pmIssueMap(got))
}

func (s *Server) pmUnassignTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	s.pmTaskAction(w, r, func(id pm.TaskID, c pm.IdentityRef) error { return d.PM.UnassignTask(r.Context(), id, c) })
}

func (s *Server) pmReopenTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	s.pmTaskAction(w, r, func(id pm.TaskID, c pm.IdentityRef) error { return d.PM.ReopenTask(r.Context(), id, c) })
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

// pmUnsubscribeTaskHandler removes a MANUAL subscriber row — the only way to
// evict someone retained on the task Conversation (OQ13: unassign/reassign/
// reopen keep the offboarded assignee as a sticky subscriber; explicit
// unsubscribe is the eviction path). Creator / current assignee are role-derived
// so this is a no-op for them until the role is dropped.
func (s *Server) pmUnsubscribeTaskHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IdentityID string `json:"identity_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	d := hd(r)
	s.pmTaskAction(w, r, func(id pm.TaskID, c pm.IdentityRef) error {
		return d.PM.UnsubscribeTask(r.Context(), id, pm.IdentityRef(req.IdentityID), c)
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

// pmBatchUpdateIssueHandler — the bare PATCH /issues/{id} (v2.8.1 edit-consolidation):
// an atomic dirty-only multi-field save (title/description/status/tags) so the
// Edit-Issue modal can replace the issue-detail sidebar's per-field inline editors.
// The Issue analogue of pmBatchUpdateTaskHandler; issues have NO assignee. Superset
// of the old title/description-only pmUpdateIssueHandler (left unused), mirroring the
// #232 task repoint (avoids the Go-mux duplicate-route panic).
func (s *Server) pmBatchUpdateIssueHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	i, caller, ok := s.pmRequireIssueInProject(w, r, d)
	if !ok {
		return
	}
	var req struct {
		Status      *string   `json:"status"`
		Tags        *[]string `json:"tags"`
		Title       *string   `json:"title"`
		Description *string   `json:"description"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := d.PM.BatchUpdateIssue(r.Context(), i.ID(), pmservice.BatchIssuePatch{
		Status: req.Status, Tags: req.Tags, Title: req.Title, Description: req.Description,
	}, caller); err != nil {
		mapPMError(w, err)
		return
	}
	got, _ := d.PM.GetIssue(r.Context(), i.ID())
	writeJSON(w, http.StatusOK, pmIssueMap(got))
}
