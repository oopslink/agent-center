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
// assignee (member-id or ref; tasks only — issues are never assignable), and
// time-range (v2.8.1): created_after / created_before / updated_after /
// updated_before, all optional RFC3339 instants. The FE sends ABSOLUTE instants
// (local date-picker selection + tz offset) so the comparison against the
// UTC-stored timestamps is tz-safe with no off-by-one (see timeFilter).
// Sorted updated_at DESC. Each row is complete-consumable (mock=契约): bare
// entity id + org_ref (#245 I12/T34) + project{id,name} + enriched task
// assignee {ref, display_name, member_id} | null.
package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// Terminal status sets — "all open" (the default when ?status= is omitted)
// excludes these. Values are the raw pm domain status strings.
var issueTerminalStatus = map[string]bool{"resolved": true, "closed": true, "withdrawn": true}

// taskTerminalStatus is the terminal Task set the default ("all open") view
// excludes. v2.9.1 ADR-0046: the Task state machine is {open, running, completed,
// discarded, reopened} — terminal = {completed, discarded}. (Pre-ADR-0046 this read
// {completed, verified, canceled}; verified was removed and canceled renamed
// discarded, so the old map both named dead states AND missed `discarded` — a
// discarded task wrongly survived the default filter.)
var taskTerminalStatus = map[string]bool{"completed": true, "discarded": true}

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
	tf, terr := parseTimeFilter(r)
	if terr != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", terr.Error())
		return
	}

	items := make([]map[string]any, 0)
	for _, p := range projects {
		if len(projectFilter) > 0 && !projectFilter[string(p.ID())] {
			continue
		}
		// v2.9.1 (T42): hide items of an ARCHIVED project by default — UNLESS the
		// user explicitly filters to that project (else filtering by it would be an
		// empty, confusing list). Mirrors the archived-project / archived-channel
		// default-exclude semantics (#310 / task-169c598d).
		if p.Status() == pm.ProjectArchived && !projectFilter[string(p.ID())] {
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
			if !tf.passes(i.CreatedAt(), i.UpdatedAt()) {
				continue
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
	tf, terr := parseTimeFilter(r)
	if terr != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", terr.Error())
		return
	}

	items := make([]map[string]any, 0)
	for _, p := range projects {
		if len(projectFilter) > 0 && !projectFilter[string(p.ID())] {
			continue
		}
		// v2.9.1 (T42): hide items of an ARCHIVED project by default — UNLESS the
		// user explicitly filters to that project (else filtering by it would be an
		// empty, confusing list). Mirrors the archived-project / archived-channel
		// default-exclude semantics (#310 / task-169c598d).
		if p.Status() == pm.ProjectArchived && !projectFilter[string(p.ID())] {
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
			if !tf.passes(t.CreatedAt(), t.UpdatedAt()) {
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
		"tags":       orEmptyTags(i.Tags()), "status_changed_at": rfc3339OrEmpty(i.StatusChangedAt()),
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
		"tags":       orEmptyTags(t.Tags()), "status_changed_at": rfc3339OrEmpty(t.StatusChangedAt()),
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

// timeFilter holds optional created/updated lower+upper bounds (v2.8.1 work-items
// time-range filter). The FE sends ABSOLUTE RFC3339 instants — it converts the
// user's local date-picker selection to RFC3339 WITH the local tz offset (e.g.
// "today" in GMT+8 → created_after=2026-06-08T00:00:00+08:00 &
// created_before=2026-06-08T23:59:59+08:00). The backend parses to UTC and
// compares against the UTC-stored created_at/updated_at, so there is NO
// server-side timezone guesswork and NO off-by-one at the day boundary (the FE,
// which knows the user's tz, owns the local-date→instant conversion).
type timeFilter struct {
	createdAfter, createdBefore time.Time
	updatedAfter, updatedBefore time.Time
	hasCA, hasCB, hasUA, hasUB  bool
}

// parseTimeFilter reads created_after/created_before/updated_after/updated_before
// (all optional RFC3339). A malformed value is a 400 (invalid_filter) so a bad
// param is never silently ignored (which would over-return rows).
func parseTimeFilter(r *http.Request) (timeFilter, error) {
	var f timeFilter
	parse := func(name string, dst *time.Time, has *bool) error {
		raw := strings.TrimSpace(r.URL.Query().Get(name))
		if raw == "" {
			return nil
		}
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return fmt.Errorf("%s must be RFC3339 (e.g. 2026-06-08T00:00:00+08:00): %w", name, err)
		}
		*dst, *has = t.UTC(), true
		return nil
	}
	if err := parse("created_after", &f.createdAfter, &f.hasCA); err != nil {
		return f, err
	}
	if err := parse("created_before", &f.createdBefore, &f.hasCB); err != nil {
		return f, err
	}
	if err := parse("updated_after", &f.updatedAfter, &f.hasUA); err != nil {
		return f, err
	}
	if err := parse("updated_before", &f.updatedBefore, &f.hasUB); err != nil {
		return f, err
	}
	return f, nil
}

// passes reports whether a row's (created_at, updated_at) fall within every
// specified bound. Bounds are inclusive; an unset bound is unconstrained.
func (f timeFilter) passes(createdAt, updatedAt time.Time) bool {
	if f.hasCA && createdAt.Before(f.createdAfter) {
		return false
	}
	if f.hasCB && createdAt.After(f.createdBefore) {
		return false
	}
	if f.hasUA && updatedAt.Before(f.updatedAfter) {
		return false
	}
	if f.hasUB && updatedAt.After(f.updatedBefore) {
		return false
	}
	return true
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
