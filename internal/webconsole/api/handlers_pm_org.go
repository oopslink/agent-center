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
// open" = exclude terminal states; the sentinel `status=all` surfaces EVERY
// status incl. terminal — used by the message task-ref / T-number linkify
// resolver, T62/T76), project (repeated/comma; default all),
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
	"strconv"
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

// planTerminalStatus is the terminal Plan set the default ("all open") global
// Plan list (v2.10.0 [T6]) excludes. Plan statuses are {draft, running, done,
// archived}; only `archived` is the dead/hidden state (done plans still show in
// the list, like the mockup). Mirrors the issue/task default-exclude semantics.
var planTerminalStatus = map[string]bool{"archived": true}

// orgListQueryBase builds the shared SQL-pagination query (project-id set,
// status include/exclude, q search, time range, sort + page window) for the org
// Issues/Tasks/Plans list handlers. The project-id set already applies the
// project filter + the archived-project default-exclude (T42). terminal is the
// entity's default-hidden status set. A bad time filter returns a non-nil error
// (the caller writes a 400).
func orgListQueryBase(r *http.Request, projects []*pm.Project, terminal map[string]bool) (pm.OrgListQuery, error) {
	projectFilter := parseSetParam(r, "project")
	var ids []pm.ProjectID
	for _, p := range projects {
		if len(projectFilter) > 0 && !projectFilter[string(p.ID())] {
			continue
		}
		if p.Status() == pm.ProjectArchived && !projectFilter[string(p.ID())] {
			continue // archived project hidden unless explicitly filtered (T42)
		}
		ids = append(ids, p.ID())
	}
	q := pm.OrgListQuery{ProjectIDs: ids}
	if err := applyListFilters(r, &q, terminal); err != nil {
		return pm.OrgListQuery{}, err
	}
	return q, nil
}

// applyListFilters fills the status / q / time-range / sort / page-window fields
// of q from the request (the caller sets ProjectIDs). Shared by the org list
// handlers (cross-project) and the project-scoped list handlers (single project),
// so the filter+sort+pagination contract is identical on both surfaces.
func applyListFilters(r *http.Request, q *pm.OrgListQuery, terminal map[string]bool) error {
	statusFilter := parseSetParam(r, "status")
	tf, terr := parseTimeFilter(r)
	if terr != nil {
		return terr
	}
	q.Q = strings.TrimSpace(r.URL.Query().Get("q"))
	// status: ?status=all → no constraint; explicit set → include; else default
	// "all open" → exclude the terminal set (mirrors statusPasses).
	if !statusFilter["all"] {
		if len(statusFilter) > 0 {
			for st := range statusFilter {
				q.Statuses = append(q.Statuses, st)
			}
		} else {
			for st := range terminal {
				q.ExcludeStatuses = append(q.ExcludeStatuses, st)
			}
		}
	}
	if tf.hasCA {
		t := tf.createdAfter
		q.CreatedAfter = &t
	}
	if tf.hasCB {
		t := tf.createdBefore
		q.CreatedBefore = &t
	}
	if tf.hasUA {
		t := tf.updatedAfter
		q.UpdatedAfter = &t
	}
	if tf.hasUB {
		t := tf.updatedBefore
		q.UpdatedBefore = &t
	}
	pp := parsePageParams(r)
	q.SortColumn = pp.sortKey
	if pp.sortKey == "" {
		q.SortDesc = true // default updated_at DESC (newest first)
	} else {
		q.SortDesc = pp.sortDir == "desc"
	}
	q.Limit = pp.limit
	q.Offset = pp.offset
	return nil
}

// projectByID indexes the org's projects for O(1) row enrichment of a page.
func projectByID(projects []*pm.Project) map[string]*pm.Project {
	m := make(map[string]*pm.Project, len(projects))
	for _, p := range projects {
		m[string(p.ID())] = p
	}
	return m
}

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
	q, terr := orgListQueryBase(r, projects, issueTerminalStatus)
	if terr != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", terr.Error())
		return
	}
	// Issues are never assignable; an explicit assignee filter excludes them all.
	if strings.TrimSpace(r.URL.Query().Get("assignee")) != "" {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}, "total": 0})
		return
	}
	issues, total, lerr := d.PM.ListIssuesOrgPage(r.Context(), q)
	if lerr != nil {
		mapPMError(w, lerr)
		return
	}
	byID := projectByID(projects)
	items := make([]map[string]any, 0, len(issues))
	for _, i := range issues {
		if p := byID[string(i.ProjectID())]; p != nil {
			items = append(items, orgIssueRow(i, p))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
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
	q, terr := orgListQueryBase(r, projects, taskTerminalStatus)
	if terr != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", terr.Error())
		return
	}
	q.Assignee = strings.TrimSpace(r.URL.Query().Get("assignee"))
	tasks, total, lerr := d.PM.ListTasksOrgPage(r.Context(), q)
	if lerr != nil {
		mapPMError(w, lerr)
		return
	}
	byID := projectByID(projects)
	items := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		if p := byID[string(t.ProjectID())]; p != nil {
			items = append(items, s.orgTaskRow(r, d, t, p))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

// pmListOrgPlansHandler (v2.10.0 [T6]) — GET /api/orgs/{slug}/plans returns,
// for the caller's org, every structured Plan across ALL the org's projects:
// the data behind the global Workspace > Plan list. Mirrors the Issues/Tasks
// aggregation (iterate the org's non-archived projects, reuse the per-project
// ListPlanSummaries, no new repo/migration). Each row is the per-project plan
// summary (id, name, status, progress{done,total}, has_failed, node_count,
// timestamps) PLUS project{id,name} for the cross-project list + detail link.
//
// Excludes the per-project builtin assignment pool (ADR-0047 is_builtin) — it
// is not a user-authored plan and has its own Work Board column, not the list.
// Filters: status (default = all non-archived), project, time-range. Sorted
// updated_at DESC.
func (s *Server) pmListOrgPlansHandler(w http.ResponseWriter, r *http.Request) {
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
	// T124/T98: the SQL query INCLUDES archived plans so ?status=archived/all can
	// surface them (the default ExcludeStatuses hides them otherwise); the builtin
	// assignment pool is excluded in SQL (is_builtin = 0). Each row's progress/
	// has_failed is derived by the service for the returned PAGE only.
	q, terr := orgListQueryBase(r, projects, planTerminalStatus)
	if terr != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", terr.Error())
		return
	}
	details, total, lerr := d.PM.ListOrgPlansPage(r.Context(), q)
	if lerr != nil {
		mapPlanError(w, lerr)
		return
	}
	byID := projectByID(projects)
	items := make([]map[string]any, 0, len(details))
	for _, detail := range details {
		row := pmPlanSummaryMap(detail)
		if p := byID[string(detail.Plan.ProjectID())]; p != nil {
			row["project"] = map[string]any{"id": string(p.ID()), "name": p.Name()}
		}
		items = append(items, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
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

// statusPasses reports whether a status string passes the filter:
//   - ?status=all (T62/task-336335c5): the escape hatch — EVERY status passes,
//     terminal included. The message task-ref / T-number linkify resolver uses
//     this so a reference to a completed/discarded task (the common agent case)
//     resolves instead of silently staying plain text. `all` is not a real pm
//     status, so it can never collide with a concrete value and dominates when
//     combined.
//   - explicit non-empty: membership in the requested set (a terminal status
//     surfaces only when explicitly asked for).
//   - otherwise the "all open" default = not a terminal status.
func statusPasses(status string, explicit, terminal map[string]bool) bool {
	if explicit["all"] {
		return true
	}
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

// pageParams holds the optional server-side sort + pagination controls shared by
// the org list handlers (issues/tasks/plans). All are optional: with no params
// the behavior is identical to before (sort updated_at DESC, return everything),
// so existing callers/tests are unaffected.
//
//   - sort: a row key to sort by (created_at | updated_at | status | title |
//     org_ref). Unknown/empty → default updated_at.
//   - dir:  asc | desc. Empty → desc for the default updated_at sort, asc otherwise.
//   - limit: page size (>0 enables paging; <=0 = no limit = all).
//   - offset: rows to skip (also accepts page= 1-based with page_size=).
type pageParams struct {
	sortKey string
	sortDir string
	limit   int
	offset  int
}

// sortableOrgKeys is the allowlist of row keys a client may sort by — guards
// against sorting on an absent/unstable field. All map to string row values
// (timestamps are RFC3339Nano, which sort lexicographically == chronologically).
var sortableOrgKeys = map[string]bool{
	"created_at": true, "updated_at": true, "status": true,
	"title": true, "name": true, "org_ref": true,
	// reminder list also reuses this helper (handlers_reminders.go).
	"next_run_at": true, "last_fired_at": true,
}

func parsePageParams(r *http.Request) pageParams {
	q := r.URL.Query()
	pp := pageParams{
		sortKey: strings.TrimSpace(q.Get("sort")),
		sortDir: strings.ToLower(strings.TrimSpace(q.Get("dir"))),
	}
	if !sortableOrgKeys[pp.sortKey] {
		pp.sortKey = ""
	}
	if pp.sortDir != "asc" && pp.sortDir != "desc" {
		pp.sortDir = ""
	}
	pp.limit = atoiOr(q.Get("limit"), 0)
	pp.offset = atoiOr(q.Get("offset"), 0)
	// Convenience: page= (1-based) + page_size= → offset/limit, when limit not set.
	if pp.limit <= 0 {
		if ps := atoiOr(q.Get("page_size"), 0); ps > 0 {
			pp.limit = ps
			if pg := atoiOr(q.Get("page"), 1); pg > 1 {
				pp.offset = (pg - 1) * ps
			}
		}
	}
	if pp.limit < 0 {
		pp.limit = 0
	}
	if pp.offset < 0 {
		pp.offset = 0
	}
	return pp
}

func atoiOr(s string, def int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// applyPageItems sorts the filtered rows by the requested column (default
// updated_at DESC, matching the prior sortItemsUpdatedDesc behavior) and slices
// the requested page. It returns the page slice plus the TOTAL (pre-slice) count
// so the client can render pagination. Stable + tie-broken by id.
func applyPageItems(items []map[string]any, pp pageParams) ([]map[string]any, int) {
	key := pp.sortKey
	dir := pp.sortDir
	if key == "" {
		key = "updated_at"
		if dir == "" {
			dir = "desc"
		}
	}
	if dir == "" {
		dir = "asc"
	}
	sort.SliceStable(items, func(a, b int) bool {
		va, vb := rowString(items[a], key), rowString(items[b], key)
		if va == vb {
			return rowString(items[a], "id") < rowString(items[b], "id") // stable tie-break
		}
		if dir == "desc" {
			return va > vb
		}
		return va < vb
	})
	total := len(items)
	off := pp.offset
	if off > total {
		off = total
	}
	end := total
	if pp.limit > 0 && off+pp.limit < end {
		end = off + pp.limit
	}
	return items[off:end], total
}

// rowString reads a row field as a string (org rows store sortable fields as
// strings; a non-string/absent field sorts as "").
func rowString(row map[string]any, key string) string {
	if v, ok := row[key].(string); ok {
		return v
	}
	return ""
}
