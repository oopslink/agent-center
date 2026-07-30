package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

type AssignmentPoolRepo struct{ db *sql.DB }

func NewAssignmentPoolRepo(db *sql.DB) *AssignmentPoolRepo { return &AssignmentPoolRepo{db: db} }

func poolTS(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return ts(t)
}

func (r *AssignmentPoolRepo) Save(ctx context.Context, p *pm.AssignmentPool) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `INSERT INTO pm_assignment_pools
		(id, project_id, scheduling_class, auto_assign_enabled, holding_cap, created_at, updated_at, version)
		VALUES (?,?,?,?,?,?,?,?)`, string(p.ID()), string(p.ProjectID()), p.SchedulingClass(),
		boolToInt(p.AutoAssignEnabled()), p.HoldingCap(), ts(p.CreatedAt()), ts(p.UpdatedAt()), p.Version())
	if isUnique(err) {
		return pm.ErrAssignmentPoolExists
	}
	return err
}

func (r *AssignmentPoolRepo) FindByProject(ctx context.Context, projectID pm.ProjectID) (*pm.AssignmentPool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	var id, pid, class, createdAt, updatedAt string
	var enabled, cap, version int
	err := exec.QueryRowContext(ctx, `SELECT id, project_id, scheduling_class, auto_assign_enabled,
		holding_cap, created_at, updated_at, version FROM pm_assignment_pools WHERE project_id=?`,
		string(projectID)).Scan(&id, &pid, &class, &enabled, &cap, &createdAt, &updatedAt, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pm.ErrAssignmentPoolNotFound
	}
	if err != nil {
		return nil, err
	}
	return pm.RehydrateAssignmentPool(pm.RehydrateAssignmentPoolInput{ID: pm.AssignmentPoolID(id),
		ProjectID: pm.ProjectID(pid), SchedulingClass: class, AutoAssignEnabled: enabled != 0,
		HoldingCap: cap, CreatedAt: parseTime(createdAt), UpdatedAt: parseTime(updatedAt), Version: version})
}

func (r *AssignmentPoolRepo) AddTask(ctx context.Context, m pm.AssignmentPoolTask) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `INSERT INTO pm_assignment_pool_tasks
		(pool_id, task_id, priority, added_by, added_at, claimed_by, claimed_at, claim_expires_at, version)
		VALUES (?,?,?,?,?,?,?,?,?)`, string(m.PoolID), string(m.TaskID), m.Priority, string(m.AddedBy),
		ts(m.AddedAt), string(m.ClaimedBy), poolTS(m.ClaimedAt), poolTS(m.ClaimExpiresAt), m.Version)
	if isUnique(err) {
		return pm.ErrTaskInOtherPlan
	}
	return err
}

func (r *AssignmentPoolRepo) RemoveTask(ctx context.Context, poolID pm.AssignmentPoolID, taskID pm.TaskID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `DELETE FROM pm_assignment_pool_tasks WHERE pool_id=? AND task_id=?`, string(poolID), string(taskID))
	return err
}

const assignmentPoolTaskSelect = `SELECT pool_id, task_id, priority, added_by, added_at,
	claimed_by, claimed_at, claim_expires_at, version FROM pm_assignment_pool_tasks`

func scanAssignmentPoolTask(scan func(...any) error) (pm.AssignmentPoolTask, error) {
	var poolID, taskID, addedBy, addedAt, claimedBy, claimedAt, expiresAt string
	var priority, version int
	err := scan(&poolID, &taskID, &priority, &addedBy, &addedAt, &claimedBy, &claimedAt, &expiresAt, &version)
	return pm.AssignmentPoolTask{PoolID: pm.AssignmentPoolID(poolID), TaskID: pm.TaskID(taskID), Priority: priority,
		AddedBy: pm.IdentityRef(addedBy), AddedAt: parseTime(addedAt), ClaimedBy: pm.IdentityRef(claimedBy),
		ClaimedAt: parseTime(claimedAt), ClaimExpiresAt: parseTime(expiresAt), Version: version}, err
}

func (r *AssignmentPoolRepo) FindTask(ctx context.Context, taskID pm.TaskID) (pm.AssignmentPoolTask, bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	m, err := scanAssignmentPoolTask(exec.QueryRowContext(ctx, assignmentPoolTaskSelect+` WHERE task_id=?`, string(taskID)).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return pm.AssignmentPoolTask{}, false, nil
	}
	return m, err == nil, err
}

func (r *AssignmentPoolRepo) ListTasks(ctx context.Context, poolID pm.AssignmentPoolID) ([]pm.AssignmentPoolTask, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, assignmentPoolTaskSelect+` WHERE pool_id=? ORDER BY priority DESC, added_at, task_id`, string(poolID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pm.AssignmentPoolTask
	for rows.Next() {
		m, err := scanAssignmentPoolTask(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *AssignmentPoolRepo) ListHeldByActor(ctx context.Context, actor pm.IdentityRef) ([]pm.AssignmentPoolTask, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, assignmentPoolTaskSelect+` WHERE claimed_by=? ORDER BY claimed_at, task_id`, string(actor))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pm.AssignmentPoolTask
	for rows.Next() {
		m, err := scanAssignmentPoolTask(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *AssignmentPoolRepo) Claim(ctx context.Context, poolID pm.AssignmentPoolID, taskID pm.TaskID,
	expectedVersion int, actor pm.IdentityRef, at, expiresAt time.Time) (bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx, `UPDATE pm_assignment_pool_tasks
		SET claimed_by=?, claimed_at=?, claim_expires_at=?, version=version+1
		WHERE pool_id=? AND task_id=? AND version=? AND claimed_by=''`, string(actor), ts(at),
		poolTS(expiresAt), string(poolID), string(taskID), expectedVersion)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (r *AssignmentPoolRepo) Release(ctx context.Context, poolID pm.AssignmentPoolID, taskID pm.TaskID,
	expectedVersion int, actor pm.IdentityRef) (bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx, `UPDATE pm_assignment_pool_tasks
		SET claimed_by='', claimed_at='', claim_expires_at='', version=version+1
		WHERE pool_id=? AND task_id=? AND version=? AND claimed_by=?`, string(poolID), string(taskID), expectedVersion, string(actor))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

var _ pm.AssignmentPoolRepository = (*AssignmentPoolRepo)(nil)
