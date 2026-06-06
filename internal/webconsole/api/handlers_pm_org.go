// handlers_pm_org.go — org-scoped cross-project work-item aggregation (v2.8 #258/#260).
//
// GET /api/issues  and  GET /api/tasks  return, for the caller's org (resolved
// via requireOrgMember's ?org_slug=/?org_id=), every issue/task across ALL the
// org's projects — the data behind the global Sidebar > Workspace > Issues/Tasks
// pages. Aggregation is org-scoped (only the org's projects, no cross-org leak)
// and equals the sum of each project's items, by iterating the org's projects
// and reusing the per-project read methods (no new repo/migration).
//
// Filters (query params): status (repeated or comma-separated; default = "all
// open" = exclude terminal states), project (repeated/comma; default all),
// assignee (member-id or ref; tasks only — issues are never assignable).
// Sorted updated_at DESC. Each row is complete-consumable (mock=契约): bare
// entity id + org_ref (#245 I12/T34) + project{id,name} + enriched task
// assignee {ref, display_name, member_id} | null.
package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// Terminal status sets — "all open" (the default when ?status= is omitted)
// excludes these. Values are the raw pm domain status strings.
var issueTerminalStatus = map[string]bool{"resolved": true, "closed": true, "withdrawn": true}
var taskTerminalStatus = map[string]bool{"completed": true, "verified": true, "canceled": true}

func (s *Server) pmListOrgIssuesHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.PM == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	projects, err := d.PM.ListProjects(r.Context(), orgID)
	if err != nil {
		mapPMError(w, err)
		return
	}
	statusFilter := parseSetParam(r, "status")
	projectFilter := parseSetParam(r, "project")
	// Issues are never assignable; an explicit assignee filter excludes them all.
	assigneeSet := strings.TrimSpace(r.URL.Query().Get("assignee")) != ""

	items := make([]map[string]any, 0)
	for _, p := range projects {
		if len(projectFilter) > 0 && !projectFilter[string(p.ID())] {
			continue
		}
		issues, lerr := d.PM.ListIssues(r.Context(), p.ID())
		if lerr != nil {
			mapPMError(w, lerr)
			return
		}
		for _, i := range issues {
			if !statusPasses(string(i.Status()), statusFilter, issueTerminalStatus) {
				continue
			}
			if assigneeSet {
				continue // issues have no assignee
			}
			items = append(items, orgIssueRow(i, p))
		}
	}
	sortItemsUpdatedDesc(items)
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) pmListOrgTasksHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.PM == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	projects, err := d.PM.ListProjects(r.Context(), orgID)
	if err != nil {
		mapPMError(w, err)
		return
	}
	statusFilter := parseSetParam(r, "status")
	projectFilter := parseSetParam(r, "project")
	assigneeFilter := strings.TrimSpace(r.URL.Query().Get("assignee"))

	items := make([]map[string]any, 0)
	for _, p := range projects {
		if len(projectFilter) > 0 && !projectFilter[string(p.ID())] {
			continue
		}
		tasks, lerr := d.PM.ListTasks(r.Context(), p.ID())
		if lerr != nil {
			mapPMError(w, lerr)
			return
		}
		for _, t := range tasks {
			if !statusPasses(string(t.Status()), statusFilter, taskTerminalStatus) {
				continue
			}
			ref := string(t.Assignee())
			if assigneeFilter != "" && !assigneeMatches(ref, assigneeFilter) {
				continue
			}
			items = append(items, s.orgTaskRow(r, d, t, p))
		}
	}
	sortItemsUpdatedDesc(items)
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

// orgIssueRow builds the DTO row for an issue. assignee is always null — issues
// are not assignable in the domain (only tasks have an assignee).
func orgIssueRow(i *pm.Issue, p *pm.Project) map[string]any {
	m := map[string]any{
		"id":         string(i.ID()),
		"project":    map[string]any{"id": string(p.ID()), "name": p.Name()},
		"title":      i.Title(),
		"status":     string(i.Status()),
		"assignee":   nil,
		"priority":   nil, // issues have no priority field
		"created_at": i.CreatedAt().Format(time.RFC3339Nano),
		"updated_at": i.UpdatedAt().Format(time.RFC3339Nano),
	}
	if ref := orgRefToken("I", i.OrgNumber()); ref != "" {
		m["org_ref"] = ref
	}
	return m
}

// orgTaskRow builds the DTO row for a task, enriching the assignee
// (ref → {ref, display_name, member_id}) via the identity directory.
func (s *Server) orgTaskRow(r *http.Request, d HandlerDeps, t *pm.Task, p *pm.Project) map[string]any {
	m := map[string]any{
		"id":         string(t.ID()),
		"project":    map[string]any{"id": string(p.ID()), "name": p.Name()},
		"title":      t.Title(),
		"status":     string(t.Status()),
		"assignee":   s.enrichAssignee(r, d, string(t.Assignee())),
		"priority":   nil, // pm domain has no task priority field (kept in DTO for forward-compat)
		"created_at": t.CreatedAt().Format(time.RFC3339Nano),
		"updated_at": t.UpdatedAt().Format(time.RFC3339Nano),
	}
	if ref := orgRefToken("T", t.OrgNumber()); ref != "" {
		m["org_ref"] = ref
	}
	return m
}

// enrichAssignee resolves a prefixed identity ref ("agent:<id>"/"user:<id>")
// into the complete-consumable {ref, display_name, member_id}. Returns nil for
// an empty/unassigned ref. display_name is best-effort (a directory miss leaves
// it empty; the UI falls back to the member-id handle).
func (s *Server) enrichAssignee(r *http.Request, d HandlerDeps, ref string) map[string]any {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	memberID := bareRefID(ref)
	out := map[string]any{"ref": ref, "member_id": memberID, "display_name": "", "assignee_lifecycle": ""}
	if d.IdentityRepo != nil && memberID != "" {
		if ident, err := d.IdentityRepo.GetByID(r.Context(), memberID); err == nil && ident != nil {
			out["display_name"] = ident.DisplayName()
		}
	}
	// v2.8 #272 (archived)-chip data: expose the agent lifecycle for an agent
	// assignee so the UI can render "(archived)" (#215 deleted-peer pattern) — and
	// later (error)/(stopped) etc. (generic string, not an archived bool, PD pick).
	// Only agent refs have a lifecycle; user refs leave it "". Best-effort: a
	// resolve miss leaves it "" (UI falls back to no chip).
	if d.AgentSvc != nil && strings.HasPrefix(ref, "agent:") && memberID != "" {
		if a, err := d.AgentSvc.ResolveAgent(r.Context(), memberID); err == nil && a != nil {
			out["assignee_lifecycle"] = string(a.Lifecycle())
		}
	}
	return out
}

// --- filter / sort helpers --------------------------------------------------

// parseSetParam collects a repeated and/or comma-separated query param into a
// set (e.g. ?status=open&status=running or ?status=open,running).
func parseSetParam(r *http.Request, name string) map[string]bool {
	set := map[string]bool{}
	for _, raw := range r.URL.Query()[name] {
		for _, v := range strings.Split(raw, ",") {
			if v = strings.TrimSpace(v); v != "" {
				set[v] = true
			}
		}
	}
	return set
}

// statusPasses reports whether a status string passes the filter: when the
// explicit set is non-empty, membership in it; otherwise the "all open"
// default = not a terminal status.
func statusPasses(status string, explicit, terminal map[string]bool) bool {
	if len(explicit) > 0 {
		return explicit[status]
	}
	return !terminal[status]
}

// assigneeMatches reports whether a task's assignee ref matches the filter,
// which may be a full ref ("agent:agent-x") or a bare member-id ("agent-x").
func assigneeMatches(ref, filter string) bool {
	if ref == "" {
		return false
	}
	return ref == filter || bareRefID(ref) == bareRefID(filter)
}

// sortItemsUpdatedDesc orders rows by updated_at descending (newest first),
// the default for both pages.
func sortItemsUpdatedDesc(items []map[string]any) {
	sort.SliceStable(items, func(a, b int) bool {
		ua, _ := items[a]["updated_at"].(string)
		ub, _ := items[b]["updated_at"].(string)
		return ua > ub // RFC3339Nano sorts lexicographically == chronologically
	})
}
