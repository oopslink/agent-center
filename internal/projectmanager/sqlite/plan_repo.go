package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// --- PlanRepo ---------------------------------------------------------------

// PlanRepo implements pm.PlanRepository (v2.9 #283): the Plan aggregate plus its
// per-Plan depends_on execution-DAG edges. AddDependency enforces the acyclic +
// no-self-edge invariant before persisting; the DAG is 1:1-scoped to one Plan
// (§9.8). No node_status is read or written — node status is derived (§9.2).
type PlanRepo struct{ db *sql.DB }

// NewPlanRepo constructs the repo.
func NewPlanRepo(db *sql.DB) *PlanRepo { return &PlanRepo{db: db} }

// tsPtr formats an optional timestamp: nil → "" (the schema default for "no
// target date"), else RFC3339Nano (mirrors the task status_changed_at ” convention).
func tsPtr(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return ts(*t)
}

// parseTimePtr parses an optional stored timestamp: "" → nil, else a *time.Time.
func parseTimePtr(s string) *time.Time {
	if s == "" {
		return nil
	}
	t := parseTime(s)
	if t.IsZero() {
		return nil
	}
	return &t
}

func (r *PlanRepo) Save(ctx context.Context, p *pm.Plan) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO pm_plans (id, project_id, name, description, status, creator_ref, conversation_id, target_date, created_at, updated_at, version)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		string(p.ID()), string(p.ProjectID()), p.Name(), p.Description(),
		string(p.Status()), string(p.CreatorRef()), p.ConversationID(), tsPtr(p.TargetDate()),
		ts(p.CreatedAt()), ts(p.UpdatedAt()), p.Version())
	if isUnique(err) {
		return pm.ErrPlanExists
	}
	return err
}

func (r *PlanRepo) Update(ctx context.Context, p *pm.Plan) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE pm_plans SET name=?, description=?, status=?, conversation_id=?, target_date=?, updated_at=?, version=? WHERE id=?`,
		p.Name(), p.Description(), string(p.Status()), p.ConversationID(), tsPtr(p.TargetDate()),
		ts(p.UpdatedAt()), p.Version(), string(p.ID()))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return pm.ErrPlanNotFound
	}
	return nil
}

func (r *PlanRepo) FindByID(ctx context.Context, id pm.PlanID) (*pm.Plan, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, planSelect+` WHERE id = ?`, string(id))
	p, err := scanPlan(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pm.ErrPlanNotFound
	}
	return p, err
}

func (r *PlanRepo) ListByProject(ctx context.Context, projectID pm.ProjectID) ([]*pm.Plan, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, planSelect+` WHERE project_id = ? ORDER BY created_at, id`, string(projectID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pm.Plan
	for rows.Next() {
		p, err := scanPlan(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *PlanRepo) Delete(ctx context.Context, id pm.PlanID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx, `DELETE FROM pm_plans WHERE id = ?`, string(id))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return pm.ErrPlanNotFound
	}
	return nil
}

// AddDependency loads the plan's existing edges, runs WouldCreateCycle (which
// rejects self-edges and cycles), then inserts. The acyclic + no-self-edge
// invariant is enforced HERE before any write (§283 acyclic red-line).
func (r *PlanRepo) AddDependency(ctx context.Context, dep pm.Dependency) error {
	existing, err := r.ListDependencies(ctx, dep.PlanID)
	if err != nil {
		return err
	}
	if err := pm.WouldCreateCycle(existing, dep); err != nil {
		return err
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err = exec.ExecContext(ctx,
		`INSERT INTO pm_task_dependencies (plan_id, from_task_id, to_task_id) VALUES (?,?,?)`,
		string(dep.PlanID), string(dep.FromTaskID), string(dep.ToTaskID))
	return err
}

func (r *PlanRepo) RemoveDependency(ctx context.Context, dep pm.Dependency) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`DELETE FROM pm_task_dependencies WHERE plan_id = ? AND from_task_id = ? AND to_task_id = ?`,
		string(dep.PlanID), string(dep.FromTaskID), string(dep.ToTaskID))
	return err
}

// ListDependencies returns all depends_on edges scoped to one Plan (§9.8):
// the WHERE plan_id = ? isolates one plan's DAG from every other plan's.
func (r *PlanRepo) ListDependencies(ctx context.Context, planID pm.PlanID) ([]pm.Dependency, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		`SELECT plan_id, from_task_id, to_task_id FROM pm_task_dependencies WHERE plan_id = ? ORDER BY from_task_id, to_task_id`,
		string(planID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pm.Dependency
	for rows.Next() {
		var pid, from, to string
		if err := rows.Scan(&pid, &from, &to); err != nil {
			return nil, err
		}
		out = append(out, pm.Dependency{PlanID: pm.PlanID(pid), FromTaskID: pm.TaskID(from), ToTaskID: pm.TaskID(to)})
	}
	return out, rows.Err()
}

// --- Dispatch records (v2.9 #285, §9.3) -------------------------------------

// RecordDispatch writes the once-only {plan_id, task_id} dispatch record. It is
// idempotent on the PK: an INSERT OR IGNORE means re-running advance / event
// replay / a second upstream completing for an already-dispatched node is a
// no-op, never an error nor a second @mention (§9.3).
func (r *PlanRepo) RecordDispatch(ctx context.Context, planID pm.PlanID, taskID pm.TaskID, at time.Time, messageID string) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT OR IGNORE INTO pm_plan_dispatch_records (plan_id, task_id, dispatched_at, dispatch_message_id) VALUES (?,?,?,?)`,
		string(planID), string(taskID), ts(at), messageID)
	return err
}

// ListDispatchRecords returns one Plan's dispatch records (§9.8 per-plan scoping).
func (r *PlanRepo) ListDispatchRecords(ctx context.Context, planID pm.PlanID) ([]pm.DispatchRecord, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		`SELECT plan_id, task_id, dispatched_at, dispatch_message_id FROM pm_plan_dispatch_records WHERE plan_id = ? ORDER BY task_id`,
		string(planID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pm.DispatchRecord
	for rows.Next() {
		var pid, tid, at, mid string
		if err := rows.Scan(&pid, &tid, &at, &mid); err != nil {
			return nil, err
		}
		out = append(out, pm.DispatchRecord{
			PlanID: pm.PlanID(pid), TaskID: pm.TaskID(tid),
			DispatchedAt: parseTime(at), DispatchMessageID: mid,
		})
	}
	return out, rows.Err()
}

// ClearDispatch deletes one node's dispatch record (creator re-run path, §9.3).
// Deleting a non-existent record is a no-op (not an error).
func (r *PlanRepo) ClearDispatch(ctx context.Context, planID pm.PlanID, taskID pm.TaskID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`DELETE FROM pm_plan_dispatch_records WHERE plan_id = ? AND task_id = ?`,
		string(planID), string(taskID))
	return err
}

const planSelect = `SELECT id, project_id, name, description, status, creator_ref, conversation_id, target_date, created_at, updated_at, version FROM pm_plans`

func scanPlan(scan func(...any) error) (*pm.Plan, error) {
	var (
		id, projectID, name, description, status, creatorRef, conversationID, targetDate, createdAt, updatedAt string
		version                                                                                                int
	)
	if err := scan(&id, &projectID, &name, &description, &status, &creatorRef, &conversationID, &targetDate, &createdAt, &updatedAt, &version); err != nil {
		return nil, err
	}
	return pm.RehydratePlan(pm.RehydratePlanInput{
		ID: pm.PlanID(id), ProjectID: pm.ProjectID(projectID), Name: name, Description: description,
		Status: pm.PlanStatus(status), CreatorRef: pm.IdentityRef(creatorRef), ConversationID: conversationID,
		TargetDate: parseTimePtr(targetDate),
		CreatedAt:  parseTime(createdAt), UpdatedAt: parseTime(updatedAt), Version: version,
	})
}

var _ pm.PlanRepository = (*PlanRepo)(nil)
