package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// --- PlanFindingRepo --------------------------------------------------------

// PlanFindingRepo implements pm.PlanFindingRepository (v2.10 #ADR-0053): the
// plan-scoped shared-findings store. Findings are IMMUTABLE — Save (once) + reads
// + Delete/DeleteByPlan (retract / cascade), no Update. All methods honor
// persistence.ExecutorFromCtx so a Save composes into the producer's tx + outbox
// event (OQ1). §9.w: no FK; referential integrity is the AppService's job.
type PlanFindingRepo struct{ db *sql.DB }

// NewPlanFindingRepo constructs the repo.
func NewPlanFindingRepo(db *sql.DB) *PlanFindingRepo { return &PlanFindingRepo{db: db} }

const planFindingSelect = `SELECT id, plan_id, task_id, project_id, author_ref, kind, content, created_at, version FROM pm_plan_findings`

func (r *PlanFindingRepo) Save(ctx context.Context, f *pm.PlanFinding) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO pm_plan_findings (id, plan_id, task_id, project_id, author_ref, kind, content, created_at, version)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		string(f.ID()), string(f.PlanID()), string(f.TaskID()), string(f.ProjectID()),
		string(f.AuthorRef()), string(f.Kind()), f.Content(), ts(f.CreatedAt()), f.Version())
	if isUnique(err) {
		// a duplicate id should never happen (server-generated ULID); surface it as a
		// CONFLICT, not a misleading not-found (review #5).
		return pm.ErrPlanFindingExists
	}
	return err
}

func (r *PlanFindingRepo) FindByID(ctx context.Context, id pm.PlanFindingID) (*pm.PlanFinding, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, planFindingSelect+` WHERE id = ?`, string(id))
	f, err := scanPlanFinding(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pm.ErrPlanFindingNotFound
	}
	return f, err
}

// ListByPlan returns one Plan's findings, stable-ordered (created_at, id) so the
// dispatch injection and list_findings tool see a deterministic, oldest-first set.
func (r *PlanFindingRepo) ListByPlan(ctx context.Context, planID pm.PlanID) ([]*pm.PlanFinding, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, planFindingSelect+` WHERE plan_id = ? ORDER BY created_at, id`, string(planID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pm.PlanFinding
	for rows.Next() {
		f, err := scanPlanFinding(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// CountByPlan returns how many findings a Plan has (review #4: bounded dispatch
// read — the dispatcher needs the total to render the "latest N of M" notice
// without loading every row).
func (r *PlanFindingRepo) CountByPlan(ctx context.Context, planID pm.PlanID) (int, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	var n int
	err := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM pm_plan_findings WHERE plan_id = ?`, string(planID)).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// ListLatestByPlan returns at most `limit` of a Plan's MOST RECENT findings,
// re-ordered oldest-first for display (review #4: the dispatcher injects a bounded
// window, not the full history). It fetches the newest `limit` rows
// (created_at DESC, id DESC LIMIT ?) then reverses in memory — avoiding a FROM
// subquery so the query stays on the SQLite/Postgres common subset. limit <= 0
// returns an empty slice.
func (r *PlanFindingRepo) ListLatestByPlan(ctx context.Context, planID pm.PlanID, limit int) ([]*pm.PlanFinding, error) {
	if limit <= 0 {
		return nil, nil
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		planFindingSelect+` WHERE plan_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`,
		string(planID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var desc []*pm.PlanFinding
	for rows.Next() {
		f, err := scanPlanFinding(rows.Scan)
		if err != nil {
			return nil, err
		}
		desc = append(desc, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// reverse newest-first → oldest-first for display.
	for i, j := 0, len(desc)-1; i < j; i, j = i+1, j-1 {
		desc[i], desc[j] = desc[j], desc[i]
	}
	return desc, nil
}

func (r *PlanFindingRepo) Delete(ctx context.Context, id pm.PlanFindingID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx, `DELETE FROM pm_plan_findings WHERE id = ?`, string(id))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return pm.ErrPlanFindingNotFound
	}
	return nil
}

// DeleteByPlan removes every finding of a Plan (the Plan-delete cascade). Deleting
// zero rows is not an error (a plan may simply have no findings).
func (r *PlanFindingRepo) DeleteByPlan(ctx context.Context, planID pm.PlanID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `DELETE FROM pm_plan_findings WHERE plan_id = ?`, string(planID))
	return err
}

func scanPlanFinding(scan func(...any) error) (*pm.PlanFinding, error) {
	var (
		id, planID, taskID, projectID, authorRef, kind, content, createdAt string
		version                                                            int
	)
	if err := scan(&id, &planID, &taskID, &projectID, &authorRef, &kind, &content, &createdAt, &version); err != nil {
		return nil, err
	}
	return pm.RehydratePlanFinding(pm.RehydratePlanFindingInput{
		ID: pm.PlanFindingID(id), PlanID: pm.PlanID(planID), TaskID: pm.TaskID(taskID),
		ProjectID: pm.ProjectID(projectID), AuthorRef: pm.IdentityRef(authorRef),
		Kind: pm.PlanFindingKind(kind), Content: content,
		CreatedAt: parseTime(createdAt), Version: version,
	})
}

var _ pm.PlanFindingRepository = (*PlanFindingRepo)(nil)
