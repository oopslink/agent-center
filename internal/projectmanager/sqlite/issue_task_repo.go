package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// marshalTags serializes a tag slice for storage. nil/empty → "" (so old/empty
// rows store the empty string, not "null"/"[]"). v2.8.1 edit #278.
func marshalTags(tags []string) any {
	if len(tags) == 0 {
		return ""
	}
	b, err := json.Marshal(tags)
	if err != nil {
		return ""
	}
	return string(b)
}

// unmarshalTags parses a stored tags string. "" (empty/old rows) → nil slice.
func unmarshalTags(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

// --- IssueRepo --------------------------------------------------------------

// IssueRepo implements pm.IssueRepository.
type IssueRepo struct{ db *sql.DB }

// NewIssueRepo constructs the repo.
func NewIssueRepo(db *sql.DB) *IssueRepo { return &IssueRepo{db: db} }

func (r *IssueRepo) Save(ctx context.Context, i *pm.Issue) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO pm_issues (id, project_id, title, description, status, created_by, created_at, updated_at, version, org_number, tags, status_changed_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		string(i.ID()), string(i.ProjectID()), i.Title(), nullString(i.Description()),
		string(i.Status()), string(i.CreatedBy()), ts(i.CreatedAt()), ts(i.UpdatedAt()), i.Version(), nullInt(i.OrgNumber()),
		marshalTags(i.Tags()), ts(i.StatusChangedAt()))
	if isUnique(err) {
		return pm.ErrIssueExists
	}
	return err
}

func (r *IssueRepo) Update(ctx context.Context, i *pm.Issue) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE pm_issues SET title=?, description=?, status=?, updated_at=?, version=?, tags=?, status_changed_at=? WHERE id=?`,
		i.Title(), nullString(i.Description()), string(i.Status()), ts(i.UpdatedAt()), i.Version(),
		marshalTags(i.Tags()), ts(i.StatusChangedAt()), string(i.ID()))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return pm.ErrIssueNotFound
	}
	return nil
}

func (r *IssueRepo) FindByID(ctx context.Context, id pm.IssueID) (*pm.Issue, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, issueSelect+` WHERE id = ?`, string(id))
	i, err := scanIssue(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pm.ErrIssueNotFound
	}
	return i, err
}

func (r *IssueRepo) ListByProject(ctx context.Context, projectID pm.ProjectID) ([]*pm.Issue, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, issueSelect+` WHERE project_id = ? ORDER BY created_at, id`, string(projectID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pm.Issue
	for rows.Next() {
		i, err := scanIssue(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// FindByStatuses returns issues in any of the given statuses across ALL
// projects (global), oldest-first, capped at limit (<=0 = uncapped). v2.7 #107
// #119 fleet issues-repoint: the pm successor to the retired discussion
// FindByStatus full scan, for the fleet pending-issues global-admin path.
func (r *IssueRepo) FindByStatuses(ctx context.Context, statuses []pm.IssueStatus, limit int) ([]*pm.Issue, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	ph := make([]string, len(statuses))
	args := make([]any, 0, len(statuses)+1)
	for i, s := range statuses {
		ph[i] = "?"
		args = append(args, string(s))
	}
	q := issueSelect + ` WHERE status IN (` + strings.Join(ph, ",") + `) ORDER BY created_at, id`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := exec.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pm.Issue
	for rows.Next() {
		i, err := scanIssue(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

const issueSelect = `SELECT id, project_id, title, description, status, created_by, created_at, updated_at, version, org_number, tags, status_changed_at FROM pm_issues`

func scanIssue(scan func(...any) error) (*pm.Issue, error) {
	var (
		id, projectID, title, status, createdBy, createdAt, updatedAt string
		desc                                                          sql.NullString
		version                                                       int
		orgNumber                                                     sql.NullInt64
		tags                                                          sql.NullString
		statusChangedAt                                               sql.NullString
	)
	if err := scan(&id, &projectID, &title, &desc, &status, &createdBy, &createdAt, &updatedAt, &version, &orgNumber, &tags, &statusChangedAt); err != nil {
		return nil, err
	}
	return pm.RehydrateIssue(pm.RehydrateIssueInput{
		ID: pm.IssueID(id), ProjectID: pm.ProjectID(projectID), Title: title, Description: desc.String,
		Status: pm.IssueStatus(status), CreatedBy: pm.IdentityRef(createdBy),
		CreatedAt: parseTime(createdAt), UpdatedAt: parseTime(updatedAt), Version: version,
		OrgNumber:       int(orgNumber.Int64),
		Tags:            unmarshalTags(tags.String),
		StatusChangedAt: parseTime(statusChangedAt.String),
	})
}

// --- TaskRepo ---------------------------------------------------------------

// TaskRepo implements pm.TaskRepository.
type TaskRepo struct{ db *sql.DB }

// NewTaskRepo constructs the repo.
func NewTaskRepo(db *sql.DB) *TaskRepo { return &TaskRepo{db: db} }

func (r *TaskRepo) Save(ctx context.Context, t *pm.Task) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO pm_tasks (id, project_id, title, description, status, assignee, derived_from_issue,
			completed_by, blocked_reason, created_by, created_at, updated_at, version, org_number, tags, status_changed_at, plan_id, archived_at, archived_by, branch, base, skip_merge_check, role, blocked_reason_type, blocked_comment, execution_lease_expires_at, model, required_capabilities)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		string(t.ID()), string(t.ProjectID()), t.Title(), nullString(t.Description()), string(t.Status()),
		nullString(string(t.Assignee())), nullString(string(t.DerivedFromIssue())),
		nullString(string(t.CompletedBy())), nullString(t.BlockedReason()),
		string(t.CreatedBy()), ts(t.CreatedAt()), ts(t.UpdatedAt()), t.Version(), nullInt(t.OrgNumber()),
		marshalTags(t.Tags()), ts(t.StatusChangedAt()), string(t.PlanID()),
		tsPtr(t.ArchivedAt()), string(t.ArchivedBy()), t.Branch(), t.Base(), t.SkipMergeCheck(), string(t.Role()),
		string(t.BlockedReasonType()), t.BlockedComment(), tsPtr(t.ExecutionLeaseExpiresAt()), nullString(t.Model()),
		marshalCaps(t.RequiredCapabilities()))
	if isUnique(err) {
		return pm.ErrTaskExists
	}
	return err
}

func (r *TaskRepo) Update(ctx context.Context, t *pm.Task) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE pm_tasks SET title=?, description=?, status=?, assignee=?, derived_from_issue=?,
			completed_by=?, blocked_reason=?, updated_at=?, version=?, tags=?, status_changed_at=?, plan_id=?, archived_at=?, archived_by=?, branch=?, base=?, skip_merge_check=?, role=?, blocked_reason_type=?, blocked_comment=?, execution_lease_expires_at=?, model=?, required_capabilities=? WHERE id=?`,
		t.Title(), nullString(t.Description()), string(t.Status()),
		nullString(string(t.Assignee())), nullString(string(t.DerivedFromIssue())),
		nullString(string(t.CompletedBy())), nullString(t.BlockedReason()),
		ts(t.UpdatedAt()), t.Version(), marshalTags(t.Tags()), ts(t.StatusChangedAt()), string(t.PlanID()),
		tsPtr(t.ArchivedAt()), string(t.ArchivedBy()), t.Branch(), t.Base(), t.SkipMergeCheck(), string(t.Role()),
		string(t.BlockedReasonType()), t.BlockedComment(), tsPtr(t.ExecutionLeaseExpiresAt()), nullString(t.Model()),
		marshalCaps(t.RequiredCapabilities()), string(t.ID()))
	if err != nil {
		// v2.18.0 W4c: the single-active partial UNIQUE index (migration 0072) was
		// DROPPED by 0084 — the per-agent run-slot cap is no longer a DB guarantee but
		// an application-layer ≤max_concurrent check (Service.enforceConcurrencyCap),
		// because a UNIQUE index can only express ≤1, never per-agent ≤N. An UPDATE no
		// longer changes the primary key, so there is no remaining UNIQUE on pm_tasks an
		// UPDATE can violate; surface the raw error.
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return pm.ErrTaskNotFound
	}
	return nil
}

// CountRunningUnblockedByAssignee counts the assignee's tasks holding a RUN SLOT —
// the exact predicate of the dropped single-active index (migration 0072): status =
// 'running' AND (blocked_reason IS NULL OR blocked_reason = ”). A blocked task is
// still status='running' (ADR-0046, blocked_reason set) but is a legal pause that
// frees its slot, so it is excluded — matching the old partial-index WHERE clause
// verbatim. excludeTaskID, when non-empty, omits that one row (the task being
// transitioned). NB: deliberately NO plan-terminal filter (unlike the T342 backlog
// metric) — a running+unblocked task occupies a slot regardless of its plan's state.
func (r *TaskRepo) CountRunningUnblockedByAssignee(ctx context.Context, assignee pm.IdentityRef, excludeTaskID pm.TaskID) (int, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	var n int
	err := exec.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pm_tasks
		   WHERE assignee = ? AND status = ?
		     AND (blocked_reason IS NULL OR blocked_reason = '')
		     AND id != ?`,
		string(assignee), string(pm.TaskRunning), string(excludeTaskID)).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// ClaimIfUnassigned is the atomic open-claim CAS (T83 §3.3): it writes the
// claimed state ONLY while the stored row is still `open` AND unassigned, so two
// concurrent claims can never both win — the second finds the row already
// assigned (WHERE matches nothing → 0 rows) and gets false. assignee/status are
// the gate (not version), so it is robust to the multiple in-memory version bumps
// of Assign()+Start().
func (r *TaskRepo) ClaimIfUnassigned(ctx context.Context, t *pm.Task) (bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE pm_tasks SET status=?, assignee=?, updated_at=?, version=?, status_changed_at=?
		 WHERE id=? AND status='open' AND (assignee IS NULL OR assignee='')`,
		string(t.Status()), nullString(string(t.Assignee())),
		ts(t.UpdatedAt()), t.Version(), ts(t.StatusChangedAt()), string(t.ID()))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (r *TaskRepo) FindByID(ctx context.Context, id pm.TaskID) (*pm.Task, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, taskSelect+` WHERE id = ?`, string(id))
	t, err := scanTask(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pm.ErrTaskNotFound
	}
	return t, err
}

func (r *TaskRepo) ListByProject(ctx context.Context, projectID pm.ProjectID) ([]*pm.Task, error) {
	return r.list(ctx, taskSelect+` WHERE project_id = ? ORDER BY created_at, id`, string(projectID))
}

func (r *TaskRepo) ListByAssignee(ctx context.Context, assignee pm.IdentityRef) ([]*pm.Task, error) {
	return r.list(ctx, taskSelect+` WHERE assignee = ? ORDER BY created_at, id`, string(assignee))
}

// ListByPlan returns the tasks selected into a Plan (v2.9 #283), stable-ordered
// (created_at, id).
func (r *TaskRepo) ListByPlan(ctx context.Context, planID pm.PlanID) ([]*pm.Task, error) {
	return r.list(ctx, taskSelect+` WHERE plan_id = ? ORDER BY created_at, id`, string(planID))
}

// ListUnplannedByProject returns the project's backlog (v2.9): tasks with an
// empty plan_id (not yet selected into any Plan), stable-ordered (created_at,
// id). The IS NULL guard tolerates pre-#283 rows that predate the NOT NULL
// DEFAULT ” column.
func (r *TaskRepo) ListUnplannedByProject(ctx context.Context, projectID pm.ProjectID) ([]*pm.Task, error) {
	// T339: also exclude archived rows from the backlog (archived tasks are read-only
	// and must not surface as live backlog work).
	return r.list(ctx, taskSelect+` WHERE project_id = ? AND (plan_id IS NULL OR plan_id = '') AND archived_at = '' ORDER BY created_at, id`, string(projectID))
}

func (r *TaskRepo) list(ctx context.Context, q string, arg string) ([]*pm.Task, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, q, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pm.Task
	for rows.Next() {
		t, err := scanTask(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CountByStatus returns a grouped count of pm_tasks per status across ALL
// projects (global), mirroring the old taskruntime FindByStatus full scan that
// stats used. since, if non-nil, restricts to tasks created at/after it.
// v2.7 #107 Phase-2 stats repoint.
func (r *TaskRepo) CountByStatus(ctx context.Context, since *time.Time) (map[pm.TaskStatus]int, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	var (
		rows *sql.Rows
		err  error
	)
	if since != nil {
		rows, err = exec.QueryContext(ctx,
			`SELECT status, COUNT(*) FROM pm_tasks WHERE created_at >= ? GROUP BY status`, ts(since.UTC()))
	} else {
		rows, err = exec.QueryContext(ctx, `SELECT status, COUNT(*) FROM pm_tasks GROUP BY status`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[pm.TaskStatus]int)
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		out[pm.TaskStatus(status)] = n
	}
	return out, rows.Err()
}

// CountActiveByAssignee returns, per assignee, the active-task split (Running
// "doing" + Pending "open") across ALL projects in ONE grouped scan — the
// agent-load metric source (T342). Terminal tasks and unassigned rows are
// excluded. A blocked task is still status=running (blocked_reason set), so it
// counts as Running (the agent is on it).
//
// T342d: tasks belonging to a TERMINAL plan (archived/done) are excluded — those
// are dead work that will never run, so counting them inflated backlog while the
// agent's Tasks panel (runnable-only) correctly showed nothing (@oopslink). Tasks
// with no plan (backlog) or in a draft/running plan still count.
func (r *TaskRepo) CountActiveByAssignee(ctx context.Context) (map[pm.IdentityRef]pm.AgentTaskLoad, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		`SELECT t.assignee, t.status, COUNT(*) FROM pm_tasks t
		   WHERE t.assignee IS NOT NULL AND t.assignee != '' AND t.status IN (?, ?)
		     AND (t.plan_id IS NULL OR t.plan_id = ''
		          OR t.plan_id NOT IN (SELECT id FROM pm_plans WHERE status IN (?, ?)))
		   GROUP BY t.assignee, t.status`,
		string(pm.TaskRunning), string(pm.TaskOpen),
		string(pm.PlanArchived), string(pm.PlanDone))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[pm.IdentityRef]pm.AgentTaskLoad)
	for rows.Next() {
		var assignee, status string
		var n int
		if err := rows.Scan(&assignee, &status, &n); err != nil {
			return nil, err
		}
		l := out[pm.IdentityRef(assignee)]
		switch pm.TaskStatus(status) {
		case pm.TaskRunning:
			l.Running += n
		case pm.TaskOpen:
			l.Pending += n
		}
		out[pm.IdentityRef(assignee)] = l
	}
	return out, rows.Err()
}

// ListActiveByAssignee returns the actual task rows that CountActiveByAssignee
// counts for one assignee: non-terminal (open/running) tasks that are NOT in a
// terminal (archived/done) plan, stable-ordered (created_at, id). It is the
// list-shaped twin of the backlog metric, so the Agent-detail Tasks panel can
// show EXACTLY the set the "backlog: N" badge counts — including tasks whose
// plan dependencies are not yet satisfied (these are pending/queued work, just
// not pullable yet). Mirrors the CountActiveByAssignee predicate verbatim.
func (r *TaskRepo) ListActiveByAssignee(ctx context.Context, assignee pm.IdentityRef) ([]*pm.Task, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		taskSelect+` WHERE assignee = ? AND status IN (?, ?)
		     AND (plan_id IS NULL OR plan_id = ''
		          OR plan_id NOT IN (SELECT id FROM pm_plans WHERE status IN (?, ?)))
		   ORDER BY created_at, id`,
		string(assignee),
		string(pm.TaskRunning), string(pm.TaskOpen),
		string(pm.PlanArchived), string(pm.PlanDone))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pm.Task
	for rows.Next() {
		t, err := scanTask(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListByStatuses returns tasks whose status is in any of the given statuses,
// across ALL projects (global), stable-ordered (created_at, id). Empty input →
// empty result. v2.7 #107 Phase-2 (proj-B) observability task-query repoint.
func (r *TaskRepo) ListByStatuses(ctx context.Context, statuses []pm.TaskStatus) ([]*pm.Task, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	ph := make([]string, len(statuses))
	args := make([]any, len(statuses))
	for i, s := range statuses {
		ph[i] = "?"
		args[i] = string(s)
	}
	q := taskSelect + ` WHERE status IN (` + strings.Join(ph, ",") + `) ORDER BY created_at, id`
	rows, err := exec.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pm.Task
	for rows.Next() {
		t, err := scanTask(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

const taskSelect = `SELECT id, project_id, title, description, status, assignee, derived_from_issue,
	completed_by, blocked_reason, created_by, created_at, updated_at, version, org_number, tags, status_changed_at, plan_id, archived_at, archived_by, branch, base, skip_merge_check, role, blocked_reason_type, blocked_comment, execution_lease_expires_at, model, required_capabilities FROM pm_tasks`

func scanTask(scan func(...any) error) (*pm.Task, error) {
	var (
		id, projectID, title, status, createdBy, createdAt, updatedAt string
		desc, assignee, derived, completedBy, blockedReason           sql.NullString
		version                                                       int
		orgNumber                                                     sql.NullInt64
		tags                                                          sql.NullString
		statusChangedAt                                               sql.NullString
		planID                                                        sql.NullString
		archivedAt                                                    sql.NullString
		archivedBy                                                    sql.NullString
		branch, base                                                  sql.NullString
		skipMergeCheck                                                bool
		role                                                          sql.NullString
		blockedReasonType, blockedComment                             sql.NullString
		execLeaseExpiresAt                                            sql.NullString
		model                                                         sql.NullString
		requiredCapabilities                                          sql.NullString
	)
	if err := scan(&id, &projectID, &title, &desc, &status, &assignee, &derived,
		&completedBy, &blockedReason, &createdBy, &createdAt, &updatedAt, &version, &orgNumber, &tags, &statusChangedAt, &planID, &archivedAt, &archivedBy, &branch, &base, &skipMergeCheck, &role, &blockedReasonType, &blockedComment, &execLeaseExpiresAt, &model, &requiredCapabilities); err != nil {
		return nil, err
	}
	return pm.RehydrateTask(pm.RehydrateTaskInput{
		ID: pm.TaskID(id), ProjectID: pm.ProjectID(projectID), Title: title, Description: desc.String,
		Status: pm.TaskStatus(status), Assignee: pm.IdentityRef(assignee.String),
		DerivedFromIssue: pm.IssueID(derived.String), CompletedBy: pm.IdentityRef(completedBy.String),
		BlockedReason: blockedReason.String, CreatedBy: pm.IdentityRef(createdBy),
		CreatedAt: parseTime(createdAt), UpdatedAt: parseTime(updatedAt), Version: version,
		OrgNumber:       int(orgNumber.Int64),
		Tags:            unmarshalTags(tags.String),
		StatusChangedAt: parseTime(statusChangedAt.String),
		PlanID:          pm.PlanID(planID.String),
		ArchivedAt:      parseTimePtr(archivedAt.String),
		ArchivedBy:      pm.IdentityRef(archivedBy.String),
		Branch:          branch.String,
		Base:            base.String,
		SkipMergeCheck:  skipMergeCheck,
		Role:            pm.CycleNodeRole(role.String),
		// v2.14.0 I14/F2 — block annotation + lease round-trip.
		BlockedReasonType:       pm.BlockReasonType(blockedReasonType.String),
		BlockedComment:          blockedComment.String,
		ExecutionLeaseExpiresAt: parseTimePtr(execLeaseExpiresAt.String),
		Model:                   model.String,
		RequiredCapabilities:    unmarshalCaps(requiredCapabilities.String),
	})
}

// marshalCaps serializes a canonical capability set as a JSON string array,
// ALWAYS a valid array ('[]' for nil/empty, never "" or "null"), matching the
// required_capabilities column's NOT NULL DEFAULT '[]' (v2.18.3 BE-1).
func marshalCaps(caps []string) string {
	if len(caps) == 0 {
		return "[]"
	}
	b, err := json.Marshal(caps)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// unmarshalCaps parses a stored required_capabilities JSON array. "" (old rows) /
// invalid → nil slice (unrestricted).
func unmarshalCaps(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

var (
	_ pm.IssueRepository = (*IssueRepo)(nil)
	_ pm.TaskRepository  = (*TaskRepo)(nil)
)
