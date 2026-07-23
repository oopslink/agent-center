package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// StageRepo implements pm.StageRepository (2026-07-03 plan-stage-model design §4.1):
// the lightweight first-class Stage aggregate + its outer-DAG depends_on edges (stored
// as a JSON array in one column — the outer stage DAG is small and always read/written
// whole). Stage status is derived, never stored (§4.1), so no status column exists.
type StageRepo struct{ db *sql.DB }

// NewStageRepo constructs the repo.
func NewStageRepo(db *sql.DB) *StageRepo { return &StageRepo{db: db} }

const stageSelect = `SELECT id, plan_id, name, depends_on_stages, gate_node_id, max_rounds, gate_task_id, gate_spec, created_at, updated_at, version FROM pm_stages`

func (r *StageRepo) Save(ctx context.Context, s *pm.Stage) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO pm_stages (id, plan_id, name, depends_on_stages, gate_node_id, max_rounds, gate_task_id, gate_spec, created_at, updated_at, version)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		string(s.ID()), string(s.PlanID()), s.Name(), marshalStageDeps(s.DependsOnStages()),
		s.GateNodeID(), s.MaxRounds(), string(s.GateTaskID()), marshalGateSpec(s.GateSpec()),
		ts(s.CreatedAt()), ts(s.UpdatedAt()), s.Version())
	if isUnique(err) {
		return pm.ErrStageExists
	}
	return err
}

func (r *StageRepo) Update(ctx context.Context, s *pm.Stage) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE pm_stages SET name=?, depends_on_stages=?, gate_node_id=?, max_rounds=?, gate_task_id=?, gate_spec=?, updated_at=?, version=? WHERE id=?`,
		s.Name(), marshalStageDeps(s.DependsOnStages()), s.GateNodeID(), s.MaxRounds(),
		string(s.GateTaskID()), marshalGateSpec(s.GateSpec()), ts(s.UpdatedAt()), s.Version(), string(s.ID()))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return pm.ErrStageNotFound
	}
	return nil
}

func (r *StageRepo) FindByID(ctx context.Context, id pm.StageID) (*pm.Stage, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, stageSelect+` WHERE id = ?`, string(id))
	s, err := scanStage(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pm.ErrStageNotFound
	}
	return s, err
}

func (r *StageRepo) ListByPlan(ctx context.Context, planID pm.PlanID) ([]*pm.Stage, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, stageSelect+` WHERE plan_id = ? ORDER BY created_at, id`, string(planID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pm.Stage
	for rows.Next() {
		s, err := scanStage(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *StageRepo) Delete(ctx context.Context, id pm.StageID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `DELETE FROM pm_stages WHERE id = ?`, string(id))
	return err
}

func (r *StageRepo) DeleteByPlan(ctx context.Context, planID pm.PlanID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `DELETE FROM pm_stages WHERE plan_id = ?`, string(planID))
	return err
}

func scanStage(scan func(...any) error) (*pm.Stage, error) {
	var id, planID, name, dependsJSON, gateNodeID, gateTaskID, gateSpecJSON, createdAt, updatedAt string
	var maxRounds, version int
	if err := scan(&id, &planID, &name, &dependsJSON, &gateNodeID, &maxRounds, &gateTaskID, &gateSpecJSON, &createdAt, &updatedAt, &version); err != nil {
		return nil, err
	}
	return pm.RehydrateStage(pm.RehydrateStageInput{
		ID:              pm.StageID(id),
		PlanID:          pm.PlanID(planID),
		Name:            name,
		DependsOnStages: unmarshalStageDeps(dependsJSON),
		GateNodeID:      gateNodeID,
		MaxRounds:       maxRounds,
		GateTaskID:      pm.TaskID(gateTaskID),
		GateSpec:        unmarshalGateSpec(gateSpecJSON),
		CreatedAt:       parseTime(createdAt),
		UpdatedAt:       parseTime(updatedAt),
		Version:         version,
	})
}

func marshalGateSpec(spec pm.GateSpec) string {
	b, err := json.Marshal(spec)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func unmarshalGateSpec(raw string) pm.GateSpec {
	var spec pm.GateSpec
	_ = json.Unmarshal([]byte(raw), &spec)
	return spec
}

// marshalStageDeps serializes the depends_on set as a JSON string array — ALWAYS a
// valid array ('[]' for nil/empty), matching the column's NOT NULL DEFAULT '[]'.
func marshalStageDeps(deps []pm.StageID) string {
	if len(deps) == 0 {
		return "[]"
	}
	ss := make([]string, len(deps))
	for i, d := range deps {
		ss[i] = string(d)
	}
	b, err := json.Marshal(ss)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// unmarshalStageDeps parses a JSON string-array of stage ids (” / bad JSON → nil).
func unmarshalStageDeps(s string) []pm.StageID {
	if s == "" {
		return nil
	}
	var ss []string
	if err := json.Unmarshal([]byte(s), &ss); err != nil {
		return nil
	}
	out := make([]pm.StageID, 0, len(ss))
	for _, x := range ss {
		out = append(out, pm.StageID(x))
	}
	return out
}

var _ pm.StageRepository = (*StageRepo)(nil)
