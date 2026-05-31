package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// --- IssueRepo --------------------------------------------------------------

// IssueRepo implements pm.IssueRepository.
type IssueRepo struct{ db *sql.DB }

// NewIssueRepo constructs the repo.
func NewIssueRepo(db *sql.DB) *IssueRepo { return &IssueRepo{db: db} }

func (r *IssueRepo) Save(ctx context.Context, i *pm.Issue) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO pm_issues (id, project_id, title, description, status, created_by, created_at, updated_at, version)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		string(i.ID()), string(i.ProjectID()), i.Title(), nullString(i.Description()),
		string(i.Status()), string(i.CreatedBy()), ts(i.CreatedAt()), ts(i.UpdatedAt()), i.Version())
	if isUnique(err) {
		return pm.ErrIssueExists
	}
	return err
}

func (r *IssueRepo) Update(ctx context.Context, i *pm.Issue) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE pm_issues SET title=?, description=?, status=?, updated_at=?, version=? WHERE id=?`,
		i.Title(), nullString(i.Description()), string(i.Status()), ts(i.UpdatedAt()), i.Version(), string(i.ID()))
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

const issueSelect = `SELECT id, project_id, title, description, status, created_by, created_at, updated_at, version FROM pm_issues`

func scanIssue(scan func(...any) error) (*pm.Issue, error) {
	var (
		id, projectID, title, status, createdBy, createdAt, updatedAt string
		desc                                                          sql.NullString
		version                                                       int
	)
	if err := scan(&id, &projectID, &title, &desc, &status, &createdBy, &createdAt, &updatedAt, &version); err != nil {
		return nil, err
	}
	return pm.RehydrateIssue(pm.RehydrateIssueInput{
		ID: pm.IssueID(id), ProjectID: pm.ProjectID(projectID), Title: title, Description: desc.String,
		Status: pm.IssueStatus(status), CreatedBy: pm.IdentityRef(createdBy),
		CreatedAt: parseTime(createdAt), UpdatedAt: parseTime(updatedAt), Version: version,
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
			completed_by, blocked_reason, created_by, created_at, updated_at, version)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		string(t.ID()), string(t.ProjectID()), t.Title(), nullString(t.Description()), string(t.Status()),
		nullString(string(t.Assignee())), nullString(string(t.DerivedFromIssue())),
		nullString(string(t.CompletedBy())), nullString(t.BlockedReason()),
		string(t.CreatedBy()), ts(t.CreatedAt()), ts(t.UpdatedAt()), t.Version())
	if isUnique(err) {
		return pm.ErrTaskExists
	}
	return err
}

func (r *TaskRepo) Update(ctx context.Context, t *pm.Task) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE pm_tasks SET title=?, description=?, status=?, assignee=?, derived_from_issue=?,
			completed_by=?, blocked_reason=?, updated_at=?, version=? WHERE id=?`,
		t.Title(), nullString(t.Description()), string(t.Status()),
		nullString(string(t.Assignee())), nullString(string(t.DerivedFromIssue())),
		nullString(string(t.CompletedBy())), nullString(t.BlockedReason()),
		ts(t.UpdatedAt()), t.Version(), string(t.ID()))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return pm.ErrTaskNotFound
	}
	return nil
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

const taskSelect = `SELECT id, project_id, title, description, status, assignee, derived_from_issue,
	completed_by, blocked_reason, created_by, created_at, updated_at, version FROM pm_tasks`

func scanTask(scan func(...any) error) (*pm.Task, error) {
	var (
		id, projectID, title, status, createdBy, createdAt, updatedAt string
		desc, assignee, derived, completedBy, blockedReason           sql.NullString
		version                                                       int
	)
	if err := scan(&id, &projectID, &title, &desc, &status, &assignee, &derived,
		&completedBy, &blockedReason, &createdBy, &createdAt, &updatedAt, &version); err != nil {
		return nil, err
	}
	return pm.RehydrateTask(pm.RehydrateTaskInput{
		ID: pm.TaskID(id), ProjectID: pm.ProjectID(projectID), Title: title, Description: desc.String,
		Status: pm.TaskStatus(status), Assignee: pm.IdentityRef(assignee.String),
		DerivedFromIssue: pm.IssueID(derived.String), CompletedBy: pm.IdentityRef(completedBy.String),
		BlockedReason: blockedReason.String, CreatedBy: pm.IdentityRef(createdBy),
		CreatedAt: parseTime(createdAt), UpdatedAt: parseTime(updatedAt), Version: version,
	})
}

var (
	_ pm.IssueRepository = (*IssueRepo)(nil)
	_ pm.TaskRepository  = (*TaskRepo)(nil)
)
