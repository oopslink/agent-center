package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// org_list_page.go — real SQL LIMIT/OFFSET pagination for the org-scoped,
// cross-project Issues / Tasks / Plans lists. Filtering (project set, status,
// assignee, title/name search, created/updated ranges), sorting (vetted column +
// direction) and the page window are ALL pushed into one SQL query; a sibling
// COUNT with the same WHERE yields the total. Replaces the old handler-side
// "load every row, filter + sort + slice in Go" path.

// orgWhereOpts toggles entity-specific predicates in the shared WHERE builder.
type orgWhereOpts struct {
	hasAssignee    bool // tasks: apply the assignee filter
	hasCreatedBy   bool // issues: apply the created_by (author) filter
	excludeBuiltin bool // plans: WHERE is_builtin = 0 (the builtin pool is not a user plan)
}

// orgSortColumns maps a client sort key → a vetted physical column. The key is
// always validated against this map (never interpolated raw), so the dynamic
// ORDER BY can't be an injection vector. org_ref sorts by the numeric org_number
// (so T9 < T10, unlike a string sort). An unknown/empty key → updated_at.
var orgSortColumns = map[string]string{
	"created_at": "created_at",
	"updated_at": "updated_at",
	"status":     "status",
	"title":      "title",
	"name":       "name",
	"org_ref":    "org_number",
}

// buildOrgListWhere builds the shared " WHERE ..." clause + args for a
// cross-project filtered list. Returns ("WHERE 1=0", nil) for an empty project
// set (→ no rows, total 0) so callers need no special-case.
func buildOrgListWhere(q pm.OrgListQuery, opts orgWhereOpts) (string, []any) {
	if len(q.ProjectIDs) == 0 {
		return " WHERE 1=0", nil
	}
	var conds []string
	var args []any

	ph := make([]string, len(q.ProjectIDs))
	for i, p := range q.ProjectIDs {
		ph[i] = "?"
		args = append(args, string(p))
	}
	conds = append(conds, "project_id IN ("+strings.Join(ph, ",")+")")

	if opts.excludeBuiltin {
		conds = append(conds, "is_builtin = 0")
	}

	if len(q.Statuses) > 0 {
		sp := make([]string, len(q.Statuses))
		for i, s := range q.Statuses {
			sp[i] = "?"
			args = append(args, s)
		}
		conds = append(conds, "status IN ("+strings.Join(sp, ",")+")")
	} else if len(q.ExcludeStatuses) > 0 {
		sp := make([]string, len(q.ExcludeStatuses))
		for i, s := range q.ExcludeStatuses {
			sp[i] = "?"
			args = append(args, s)
		}
		conds = append(conds, "status NOT IN ("+strings.Join(sp, ",")+")")
	}

	if opts.hasAssignee && q.Assignee != "" {
		// Mirror the handler's assigneeMatches: the stored ref equals the filter,
		// OR its bare member-id matches (so "agent-x" matches "agent:agent-x" and
		// vice versa). Covers refs stored as "agent:<id>" / "user:<id>" or bare.
		bare := bareSchemeID(q.Assignee)
		conds = append(conds, "(assignee = ? OR assignee = ? OR assignee = ? OR assignee = ?)")
		args = append(args, q.Assignee, bare, "agent:"+bare, "user:"+bare)
	}

	if opts.hasCreatedBy && q.CreatedBy != "" {
		conds = append(conds, "created_by = ?")
		args = append(args, q.CreatedBy)
	}

	if q.Q != "" {
		// case-insensitive substring (mirrors strings.Contains(ToLower(...))).
		conds = append(conds, "instr(lower("+searchColumn(opts)+"), ?) > 0")
		args = append(args, strings.ToLower(q.Q))
	}

	addRange := func(col string, lo, hi any, hasLo, hasHi bool) {
		if hasLo {
			conds = append(conds, col+" >= ?")
			args = append(args, lo)
		}
		if hasHi {
			conds = append(conds, col+" <= ?")
			args = append(args, hi)
		}
	}
	if q.CreatedAfter != nil {
		addRange("created_at", ts(*q.CreatedAfter), nil, true, false)
	}
	if q.CreatedBefore != nil {
		addRange("created_at", nil, ts(*q.CreatedBefore), false, true)
	}
	if q.UpdatedAfter != nil {
		addRange("updated_at", ts(*q.UpdatedAfter), nil, true, false)
	}
	if q.UpdatedBefore != nil {
		addRange("updated_at", nil, ts(*q.UpdatedBefore), false, true)
	}

	return " WHERE " + strings.Join(conds, " AND "), args
}

// searchColumn is the column the q substring search runs against: plans search
// by name, issues/tasks by title.
func searchColumn(opts orgWhereOpts) string {
	if opts.excludeBuiltin { // plans
		return "name"
	}
	return "title"
}

// orgOrderBy builds the ORDER BY clause from the vetted sort column (default
// updated_at) + direction, with a stable id tie-break.
func orgOrderBy(q pm.OrgListQuery) string {
	col, ok := orgSortColumns[q.SortColumn]
	if !ok {
		col = "updated_at"
	}
	dir := "ASC"
	if q.SortDesc {
		dir = "DESC"
	}
	return " ORDER BY " + col + " " + dir + ", id " + dir
}

// orgLimitOffset appends LIMIT/OFFSET when a positive limit is set, returning the
// clause + the extra args.
func orgLimitOffset(q pm.OrgListQuery) (string, []any) {
	if q.Limit <= 0 {
		return "", nil
	}
	off := q.Offset
	if off < 0 {
		off = 0
	}
	return " LIMIT ? OFFSET ?", []any{q.Limit, off}
}

// bareSchemeID strips a "scheme:" prefix (agent:/user:) to the bare id, matching
// the webconsole bareRefID used by assigneeMatches.
func bareSchemeID(ref string) string {
	if i := strings.IndexByte(ref, ':'); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

// countOrg runs the COUNT(*) with the shared WHERE for the given table.
func countOrg(ctx context.Context, db *sql.DB, table, where string, args []any) (int, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, db)
	var n int
	if err := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+where, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// --- Issues -----------------------------------------------------------------

func (r *IssueRepo) ListOrgPage(ctx context.Context, q pm.OrgListQuery) ([]*pm.Issue, int, error) {
	where, args := buildOrgListWhere(q, orgWhereOpts{hasCreatedBy: true})
	total, err := countOrg(ctx, r.db, "pm_issues", where, args)
	if err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}
	limit, largs := orgLimitOffset(q)
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, issueSelect+where+orgOrderBy(q)+limit, append(append([]any{}, args...), largs...)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*pm.Issue
	for rows.Next() {
		i, err := scanIssue(rows.Scan)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, i)
	}
	return out, total, rows.Err()
}

// --- Tasks ------------------------------------------------------------------

func (r *TaskRepo) ListOrgPage(ctx context.Context, q pm.OrgListQuery) ([]*pm.Task, int, error) {
	where, args := buildOrgListWhere(q, orgWhereOpts{hasAssignee: true})
	total, err := countOrg(ctx, r.db, "pm_tasks", where, args)
	if err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}
	limit, largs := orgLimitOffset(q)
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, taskSelect+where+orgOrderBy(q)+limit, append(append([]any{}, args...), largs...)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*pm.Task
	for rows.Next() {
		t, err := scanTask(rows.Scan)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	return out, total, rows.Err()
}

// --- Plans ------------------------------------------------------------------

func (r *PlanRepo) ListOrgPage(ctx context.Context, q pm.OrgListQuery) ([]*pm.Plan, int, error) {
	where, args := buildOrgListWhere(q, orgWhereOpts{excludeBuiltin: true})
	total, err := countOrg(ctx, r.db, "pm_plans", where, args)
	if err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}
	limit, largs := orgLimitOffset(q)
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, planSelect+where+orgOrderBy(q)+limit, append(append([]any{}, args...), largs...)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*pm.Plan
	for rows.Next() {
		p, err := scanPlan(rows.Scan)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, p)
	}
	return out, total, rows.Err()
}
