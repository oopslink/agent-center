package sqlite

import (
	"context"
	"database/sql"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// --- TaskSubscriberRepo -----------------------------------------------------

// TaskSubscriberRepo implements pm.TaskSubscriberRepository.
type TaskSubscriberRepo struct{ db *sql.DB }

// NewTaskSubscriberRepo constructs the repo.
func NewTaskSubscriberRepo(db *sql.DB) *TaskSubscriberRepo { return &TaskSubscriberRepo{db: db} }

// Add inserts a manual subscriber (idempotent: re-adding the same pair is a
// no-op via INSERT OR IGNORE on the composite PK).
func (r *TaskSubscriberRepo) Add(ctx context.Context, s *pm.TaskSubscriber) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT OR IGNORE INTO pm_task_subscribers (task_id, identity_id, added_by, created_at) VALUES (?,?,?,?)`,
		string(s.TaskID()), string(s.IdentityID()), string(s.AddedBy()), ts(s.CreatedAt()))
	return err
}

func (r *TaskSubscriberRepo) Remove(ctx context.Context, taskID pm.TaskID, identityID pm.IdentityRef) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`DELETE FROM pm_task_subscribers WHERE task_id = ? AND identity_id = ?`, string(taskID), string(identityID))
	return err
}

func (r *TaskSubscriberRepo) ListByTask(ctx context.Context, taskID pm.TaskID) ([]*pm.TaskSubscriber, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		`SELECT task_id, identity_id, added_by, created_at FROM pm_task_subscribers WHERE task_id = ? ORDER BY created_at, identity_id`,
		string(taskID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pm.TaskSubscriber
	for rows.Next() {
		var taskIDc, identityID, addedBy, createdAt string
		if err := rows.Scan(&taskIDc, &identityID, &addedBy, &createdAt); err != nil {
			return nil, err
		}
		s, err := pm.NewTaskSubscriber(pm.TaskID(taskIDc), pm.IdentityRef(identityID), pm.IdentityRef(addedBy), parseTime(createdAt))
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// --- IssueSubscriberRepo ----------------------------------------------------

// IssueSubscriberRepo implements pm.IssueSubscriberRepository.
type IssueSubscriberRepo struct{ db *sql.DB }

// NewIssueSubscriberRepo constructs the repo.
func NewIssueSubscriberRepo(db *sql.DB) *IssueSubscriberRepo { return &IssueSubscriberRepo{db: db} }

func (r *IssueSubscriberRepo) Add(ctx context.Context, s *pm.IssueSubscriber) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT OR IGNORE INTO pm_issue_subscribers (issue_id, identity_id, added_by, created_at) VALUES (?,?,?,?)`,
		string(s.IssueID()), string(s.IdentityID()), string(s.AddedBy()), ts(s.CreatedAt()))
	return err
}

func (r *IssueSubscriberRepo) Remove(ctx context.Context, issueID pm.IssueID, identityID pm.IdentityRef) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`DELETE FROM pm_issue_subscribers WHERE issue_id = ? AND identity_id = ?`, string(issueID), string(identityID))
	return err
}

func (r *IssueSubscriberRepo) ListByIssue(ctx context.Context, issueID pm.IssueID) ([]*pm.IssueSubscriber, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		`SELECT issue_id, identity_id, added_by, created_at FROM pm_issue_subscribers WHERE issue_id = ? ORDER BY created_at, identity_id`,
		string(issueID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pm.IssueSubscriber
	for rows.Next() {
		var issueIDc, identityID, addedBy, createdAt string
		if err := rows.Scan(&issueIDc, &identityID, &addedBy, &createdAt); err != nil {
			return nil, err
		}
		s, err := pm.NewIssueSubscriber(pm.IssueID(issueIDc), pm.IdentityRef(identityID), pm.IdentityRef(addedBy), parseTime(createdAt))
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

var (
	_ pm.TaskSubscriberRepository  = (*TaskSubscriberRepo)(nil)
	_ pm.IssueSubscriberRepository = (*IssueSubscriberRepo)(nil)
)
