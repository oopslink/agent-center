package sqlite

import (
	"context"
	"database/sql"
	"errors"

	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
	"github.com/oopslink/agent-center/internal/persistence"
)

type GraphRepo struct{ db *sql.DB }

func NewGraphRepo(db *sql.DB) *GraphRepo { return &GraphRepo{db: db} }

const graphSelect = `SELECT id, plan_id, status, start_node, end_node, created_at, updated_at, version FROM pm_graphs`

func (r *GraphRepo) Save(ctx context.Context, g *orch.Graph) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO pm_graphs (id, plan_id, status, start_node, end_node, created_at, updated_at, version)
		 VALUES (?,?,?,?,?,?,?,?)`,
		string(g.ID()), g.PlanID(), string(g.Status()),
		string(g.StartNodeID()), string(g.EndNodeID()),
		ts(g.CreatedAt()), ts(g.UpdatedAt()), g.Version())
	if isUnique(err) {
		return orch.ErrGraphExists
	}
	return err
}

func (r *GraphRepo) Update(ctx context.Context, g *orch.Graph) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE pm_graphs SET plan_id=?, status=?, updated_at=?, version=? WHERE id=?`,
		g.PlanID(), string(g.Status()), ts(g.UpdatedAt()), g.Version(), string(g.ID()))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return orch.ErrGraphNotFound
	}
	return nil
}

func (r *GraphRepo) FindByID(ctx context.Context, id orch.GraphID) (*orch.Graph, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, graphSelect+` WHERE id = ?`, string(id))
	g, err := scanGraph(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, orch.ErrGraphNotFound
	}
	return g, err
}

func (r *GraphRepo) FindByPlanID(ctx context.Context, planID string) (*orch.Graph, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, graphSelect+` WHERE plan_id = ?`, planID)
	g, err := scanGraph(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, orch.ErrGraphNotFound
	}
	return g, err
}

func (r *GraphRepo) Delete(ctx context.Context, id orch.GraphID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `DELETE FROM pm_graphs WHERE id = ?`, string(id))
	return err
}

func scanGraph(scan func(...any) error) (*orch.Graph, error) {
	var id, planID, status, startNode, endNode, createdAt, updatedAt string
	var version int
	if err := scan(&id, &planID, &status, &startNode, &endNode, &createdAt, &updatedAt, &version); err != nil {
		return nil, err
	}
	return orch.RehydrateGraph(orch.RehydrateGraphInput{
		ID:        orch.GraphID(id),
		PlanID:    planID,
		Status:    orch.GraphStatus(status),
		StartNode: orch.NodeID(startNode),
		EndNode:   orch.NodeID(endNode),
		CreatedAt: parseTime(createdAt),
		UpdatedAt: parseTime(updatedAt),
		Version:   version,
	})
}

var _ orch.GraphRepository = (*GraphRepo)(nil)
