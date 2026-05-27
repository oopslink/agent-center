package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

// TaskRepo implements task.Repository over SQLite.
type TaskRepo struct {
	db *sql.DB
}

// NewTaskRepo constructs the repo.
func NewTaskRepo(db *sql.DB) *TaskRepo { return &TaskRepo{db: db} }

const taskSelect = `SELECT
	id, project_id, parent_task_id, from_issue_id, title, description, description_blob_ref,
	status, priority, eta_at, requires_worktree, depends_on_task_ids,
	abandoned_reason, abandoned_message, conversation_id, current_execution_id,
	created_by, created_at, updated_at, version
FROM tasks`

// Save inserts a new Task row (fresh insert, version=1).
func (r *TaskRepo) Save(ctx context.Context, t *task.Task) error {
	if t == nil {
		return errors.New("task repo: nil task")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	depsJSON, err := marshalTaskIDs(t.DependsOnTaskIDs())
	if err != nil {
		return fmt.Errorf("marshal deps: %w", err)
	}
	const stmt = `INSERT INTO tasks (
		id, project_id, parent_task_id, from_issue_id, title, description, description_blob_ref,
		status, priority, eta_at, requires_worktree, depends_on_task_ids,
		abandoned_reason, abandoned_message, conversation_id, current_execution_id,
		created_by, created_at, updated_at, version
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	_, err = exec.ExecContext(ctx, stmt,
		string(t.ID()),
		t.ProjectID(),
		nullString(string(t.ParentTaskID())),
		nullString(t.FromIssueID()),
		t.Title(),
		t.Description(),
		nullString(t.DescriptionBlobRef()),
		string(t.Status()),
		string(t.Priority()),
		nullTimePtrStr(t.EtaAt()),
		boolToInt(t.RequiresWorktree()),
		depsJSON,
		nullString(t.AbandonedReason()),
		nullString(t.AbandonedMessage()),
		nullString(t.ConversationID()),
		nullString(string(t.CurrentExecutionID())),
		t.CreatedBy(),
		t.CreatedAt().Format(timeFormat),
		t.UpdatedAt().Format(timeFormat),
		t.Version(),
	)
	if err != nil {
		if IsUniqueConstraint(err) {
			return task.ErrTaskAlreadyExists
		}
		return err
	}
	return nil
}

// Update is the CAS UPDATE path (uses version for optimistic lock).
func (r *TaskRepo) Update(ctx context.Context, t *task.Task) error {
	if t == nil {
		return errors.New("task repo: nil task")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	depsJSON, err := marshalTaskIDs(t.DependsOnTaskIDs())
	if err != nil {
		return fmt.Errorf("marshal deps: %w", err)
	}
	// The Task in-memory state has already been advanced via state-machine
	// methods (which bump version); we CAS on (version - 1) to match the
	// pre-mutation row.
	const stmt = `UPDATE tasks SET
		title = ?, description = ?, description_blob_ref = ?,
		status = ?, priority = ?, eta_at = ?, requires_worktree = ?,
		depends_on_task_ids = ?, abandoned_reason = ?, abandoned_message = ?,
		conversation_id = ?, current_execution_id = ?,
		updated_at = ?, version = ?
	WHERE id = ? AND version = ?`
	res, err := exec.ExecContext(ctx, stmt,
		t.Title(),
		t.Description(),
		nullString(t.DescriptionBlobRef()),
		string(t.Status()),
		string(t.Priority()),
		nullTimePtrStr(t.EtaAt()),
		boolToInt(t.RequiresWorktree()),
		depsJSON,
		nullString(t.AbandonedReason()),
		nullString(t.AbandonedMessage()),
		nullString(t.ConversationID()),
		nullString(string(t.CurrentExecutionID())),
		t.UpdatedAt().Format(timeFormat),
		t.Version(),
		string(t.ID()),
		t.Version()-1,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// Disambiguate not-found vs CAS conflict.
		var existing int
		row := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE id = ?`, string(t.ID()))
		if scanErr := row.Scan(&existing); scanErr == nil {
			if existing == 0 {
				return task.ErrTaskNotFound
			}
		}
		return task.ErrTaskVersionConflict
	}
	return nil
}

// FindByID returns a Task by id.
func (r *TaskRepo) FindByID(ctx context.Context, id taskruntime.TaskID) (*task.Task, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	row := exec.QueryRowContext(ctx, taskSelect+` WHERE id = ?`, string(id))
	t, err := scanTask(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, task.ErrTaskNotFound
	}
	return t, err
}

// FindByProject returns tasks for a project with optional status filter.
func (r *TaskRepo) FindByProject(ctx context.Context, projectID string, filter task.Filter) ([]*task.Task, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	q := taskSelect + ` WHERE project_id = ?`
	args := []any{projectID}
	if filter.Status != nil {
		q += ` AND status = ?`
		args = append(args, string(*filter.Status))
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = task.DefaultLimit
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := exec.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTasks(rows)
}

// FindAll returns every task with the optional status / limit from
// Filter applied. Used by the Web Console "All projects" filter
// (v2.5.15 #70); FindByStatus already covered a cross-project read
// but required a concrete status, so it couldn't service the "All
// status × All projects" combination.
func (r *TaskRepo) FindAll(ctx context.Context, filter task.Filter) ([]*task.Task, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	q := taskSelect
	args := []any{}
	if filter.Status != nil {
		q += ` WHERE status = ?`
		args = append(args, string(*filter.Status))
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = task.DefaultLimit
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := exec.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTasks(rows)
}

// FindByStatus returns tasks across all projects matching status.
func (r *TaskRepo) FindByStatus(ctx context.Context, status task.Status, filter task.Filter) ([]*task.Task, error) {
	if !status.IsValid() {
		return nil, task.ErrInvalidStatus
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = task.DefaultLimit
	}
	q := taskSelect + ` WHERE status = ? ORDER BY created_at DESC LIMIT ?`
	rows, err := exec.QueryContext(ctx, q, string(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTasks(rows)
}

// FindBlockedBy returns tasks whose depends_on_task_ids JSON array contains
// blockerTaskID. SQLite has no JSON path index in v1 — we LIKE-match the
// JSON string (acceptable for v1 small data; later phases can introduce
// json_extract / generated columns).
func (r *TaskRepo) FindBlockedBy(ctx context.Context, blockerTaskID taskruntime.TaskID) ([]*task.Task, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	pattern := `%"` + string(blockerTaskID) + `"%`
	q := taskSelect + ` WHERE depends_on_task_ids LIKE ? ORDER BY created_at DESC`
	rows, err := exec.QueryContext(ctx, q, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks, err := scanTasks(rows)
	if err != nil {
		return nil, err
	}
	// LIKE may match unrelated substrings; precision filter in memory.
	out := tasks[:0]
	for _, t := range tasks {
		for _, dep := range t.DependsOnTaskIDs() {
			if dep == blockerTaskID {
				out = append(out, t)
				break
			}
		}
	}
	return out, nil
}

func scanTasks(rows *sql.Rows) ([]*task.Task, error) {
	var out []*task.Task
	for rows.Next() {
		t, err := scanTask(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanTask(scan func(...any) error) (*task.Task, error) {
	var (
		id            string
		projectID     string
		parentID      sql.NullString
		fromIssue     sql.NullString
		title         string
		description   string
		descBlobRef   sql.NullString
		status        string
		priority      string
		etaAtRaw      sql.NullString
		reqWorktree   int
		depsJSON      string
		abandonedR    sql.NullString
		abandonedM    sql.NullString
		convID        sql.NullString
		curExecID     sql.NullString
		createdBy     string
		createdAtRaw  string
		updatedAtRaw  string
		version       int
	)
	if err := scan(&id, &projectID, &parentID, &fromIssue, &title, &description, &descBlobRef,
		&status, &priority, &etaAtRaw, &reqWorktree, &depsJSON,
		&abandonedR, &abandonedM, &convID, &curExecID,
		&createdBy, &createdAtRaw, &updatedAtRaw, &version); err != nil {
		return nil, err
	}
	deps, err := unmarshalTaskIDs(depsJSON)
	if err != nil {
		return nil, fmt.Errorf("unmarshal deps: %w", err)
	}
	eta, err := parseTimePtrStr(etaAtRaw)
	if err != nil {
		return nil, fmt.Errorf("parse eta_at: %w", err)
	}
	createdAt, err := parseTimeStr(sql.NullString{String: createdAtRaw, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	updatedAt, err := parseTimeStr(sql.NullString{String: updatedAtRaw, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}
	return task.Rehydrate(task.RehydrateInput{
		ID:                 taskruntime.TaskID(id),
		ProjectID:          projectID,
		ParentTaskID:       taskruntime.TaskID(strings.TrimSpace(parentID.String)),
		FromIssueID:        fromIssue.String,
		Title:              title,
		Description:        description,
		DescriptionBlobRef: descBlobRef.String,
		Status:             task.Status(status),
		Priority:           task.Priority(priority),
		EtaAt:              eta,
		RequiresWorktree:   reqWorktree != 0,
		DependsOnTaskIDs:   deps,
		AbandonedReason:    abandonedR.String,
		AbandonedMessage:   abandonedM.String,
		ConversationID:     convID.String,
		CurrentExecutionID: taskruntime.TaskExecutionID(curExecID.String),
		CreatedBy:          createdBy,
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
		Version:            version,
	})
}

func marshalTaskIDs(ids []taskruntime.TaskID) (string, error) {
	if len(ids) == 0 {
		return "[]", nil
	}
	asStr := make([]string, len(ids))
	for i, id := range ids {
		asStr[i] = string(id)
	}
	return marshalStringList(asStr)
}

func unmarshalTaskIDs(s string) ([]taskruntime.TaskID, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	raw, err := unmarshalStringList(s)
	if err != nil {
		return nil, err
	}
	out := make([]taskruntime.TaskID, len(raw))
	for i, s := range raw {
		out[i] = taskruntime.TaskID(s)
	}
	return out, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
