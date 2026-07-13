package sqlite

import (
	"context"
	"database/sql"

	"github.com/oopslink/agent-center/internal/persistence"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

type EdgeRepo struct{ db *sql.DB }

func NewEdgeRepo(db *sql.DB) *EdgeRepo { return &EdgeRepo{db: db} }

func (r *EdgeRepo) Save(ctx context.Context, graphID orch.GraphID, e orch.Edge) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO pm_graph_edges (graph_id, from_node_id, to_node_id) VALUES (?,?,?)`,
		string(graphID), string(e.FromNodeID), string(e.ToNodeID))
	if isUnique(err) {
		return orch.ErrEdgeExists
	}
	return err
}

func (r *EdgeRepo) Delete(ctx context.Context, graphID orch.GraphID, from, to orch.NodeID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`DELETE FROM pm_graph_edges WHERE graph_id = ? AND from_node_id = ? AND to_node_id = ?`,
		string(graphID), string(from), string(to))
	return err
}

func (r *EdgeRepo) ListByGraph(ctx context.Context, graphID orch.GraphID) ([]orch.Edge, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		`SELECT from_node_id, to_node_id FROM pm_graph_edges WHERE graph_id = ? ORDER BY from_node_id, to_node_id`,
		string(graphID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []orch.Edge
	for rows.Next() {
		var from, to string
		if err := rows.Scan(&from, &to); err != nil {
			return nil, err
		}
		out = append(out, orch.Edge{FromNodeID: orch.NodeID(from), ToNodeID: orch.NodeID(to)})
	}
	return out, rows.Err()
}

// ListByGraphs returns the edges of ALL the given graphs in ONE IN(...) query
// (issue-77cda494). Node ids are globally unique, so the caller keys results off the
// node ids without a per-edge graph tag. Empty input ⇒ nil.
func (r *EdgeRepo) ListByGraphs(ctx context.Context, graphIDs []orch.GraphID) ([]orch.Edge, error) {
	if len(graphIDs) == 0 {
		return nil, nil
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	in, args := graphIDPlaceholders(graphIDs)
	rows, err := exec.QueryContext(ctx,
		`SELECT from_node_id, to_node_id FROM pm_graph_edges WHERE graph_id IN (`+in+`) ORDER BY graph_id, from_node_id, to_node_id`,
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []orch.Edge
	for rows.Next() {
		var from, to string
		if err := rows.Scan(&from, &to); err != nil {
			return nil, err
		}
		out = append(out, orch.Edge{FromNodeID: orch.NodeID(from), ToNodeID: orch.NodeID(to)})
	}
	return out, rows.Err()
}

var _ orch.EdgeRepository = (*EdgeRepo)(nil)
