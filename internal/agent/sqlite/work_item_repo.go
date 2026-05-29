package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/persistence"
)

// WorkItemRepo implements agent.WorkItemRepository.
type WorkItemRepo struct{ db *sql.DB }

// NewWorkItemRepo constructs the repo.
func NewWorkItemRepo(db *sql.DB) *WorkItemRepo { return &WorkItemRepo{db: db} }

func (r *WorkItemRepo) Save(ctx context.Context, w *agent.AgentWorkItem) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO agent_work_items (id, agent_id, task_ref, status, interactions, created_at, updated_at, version)
		 VALUES (?,?,?,?,?,?,?,?)`,
		w.ID(), string(w.AgentID()), w.TaskRef(), string(w.Status()), w.Interactions(),
		ts(w.CreatedAt()), ts(w.UpdatedAt()), w.Version())
	if err != nil && isUnique(err) {
		return agent.ErrWorkItemExists
	}
	return err
}

func (r *WorkItemRepo) Update(ctx context.Context, w *agent.AgentWorkItem) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE agent_work_items SET status=?, interactions=?, updated_at=?, version=? WHERE id=?`,
		string(w.Status()), w.Interactions(), ts(w.UpdatedAt()), w.Version(), w.ID())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return agent.ErrWorkItemNotFound
	}
	return nil
}

func (r *WorkItemRepo) FindByID(ctx context.Context, id string) (*agent.AgentWorkItem, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, workItemSelect+` WHERE id = ?`, id)
	w, err := scanWorkItem(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, agent.ErrWorkItemNotFound
	}
	return w, err
}

func (r *WorkItemRepo) ListByAgent(ctx context.Context, agentID agent.AgentID) ([]*agent.AgentWorkItem, error) {
	return r.list(ctx, workItemSelect+` WHERE agent_id = ? ORDER BY created_at, id`, string(agentID))
}

func (r *WorkItemRepo) ListByTask(ctx context.Context, taskRef string) ([]*agent.AgentWorkItem, error) {
	return r.list(ctx, workItemSelect+` WHERE task_ref = ? ORDER BY created_at, id`, taskRef)
}

func (r *WorkItemRepo) HasActiveWorkItem(ctx context.Context, agentID agent.AgentID) (bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_work_items WHERE agent_id = ? AND status IN ('active','waiting_input')`,
		string(agentID))
	var n int
	if err := row.Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *WorkItemRepo) list(ctx context.Context, q, arg string) ([]*agent.AgentWorkItem, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, q, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*agent.AgentWorkItem
	for rows.Next() {
		w, err := scanWorkItem(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

const workItemSelect = `SELECT id, agent_id, task_ref, status, interactions, created_at, updated_at, version FROM agent_work_items`

func scanWorkItem(scan func(...any) error) (*agent.AgentWorkItem, error) {
	var (
		id, agentID, taskRef, status, createdAt, updatedAt string
		interactions, version                              int
	)
	if err := scan(&id, &agentID, &taskRef, &status, &interactions, &createdAt, &updatedAt, &version); err != nil {
		return nil, err
	}
	return agent.RehydrateWorkItem(agent.RehydrateWorkItemInput{
		ID: id, AgentID: agent.AgentID(agentID), TaskRef: taskRef,
		Status: agent.WorkItemStatus(status), Interactions: interactions,
		CreatedAt: parseTime(createdAt), UpdatedAt: parseTime(updatedAt), Version: version,
	})
}

var _ agent.WorkItemRepository = (*WorkItemRepo)(nil)
