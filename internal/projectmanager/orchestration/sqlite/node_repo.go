package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/oopslink/agent-center/internal/persistence"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

type NodeRepo struct{ db *sql.DB }

func NewNodeRepo(db *sql.DB) *NodeRepo { return &NodeRepo{db: db} }

const nodeSelect = `SELECT id, graph_id, category, control_kind, title, status, outcome, metadata, action_logs, created_at, updated_at, version FROM pm_graph_nodes`

func (r *NodeRepo) Save(ctx context.Context, n *orch.Node) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO pm_graph_nodes (id, graph_id, category, control_kind, title, status, outcome, metadata, action_logs, created_at, updated_at, version)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		string(n.ID()), string(n.GraphID()), string(n.Category()), string(n.ControlKind()),
		n.Title(), string(n.Status()), n.Outcome(),
		n.MetadataJSON(), n.ActionLogsJSON(),
		ts(n.CreatedAt()), ts(n.UpdatedAt()), n.Version())
	if isUnique(err) {
		return orch.ErrNodeExists
	}
	return err
}

func (r *NodeRepo) Update(ctx context.Context, n *orch.Node) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE pm_graph_nodes SET title=?, status=?, outcome=?, metadata=?, action_logs=?, updated_at=?, version=? WHERE id=?`,
		n.Title(), string(n.Status()), n.Outcome(),
		n.MetadataJSON(), n.ActionLogsJSON(),
		ts(n.UpdatedAt()), n.Version(), string(n.ID()))
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return orch.ErrNodeNotFound
	}
	return nil
}

func (r *NodeRepo) FindByID(ctx context.Context, id orch.NodeID) (*orch.Node, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, nodeSelect+` WHERE id = ?`, string(id))
	n, err := scanNode(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, orch.ErrNodeNotFound
	}
	return n, err
}

func (r *NodeRepo) ListByGraph(ctx context.Context, graphID orch.GraphID) ([]*orch.Node, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, nodeSelect+` WHERE graph_id = ? ORDER BY created_at, id`, string(graphID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*orch.Node
	for rows.Next() {
		n, err := scanNode(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *NodeRepo) Delete(ctx context.Context, id orch.NodeID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `DELETE FROM pm_graph_nodes WHERE id = ?`, string(id))
	return err
}

func scanNode(scan func(...any) error) (*orch.Node, error) {
	var id, graphID, category, controlKind, title, status, outcome string
	var metadataJSON, actionLogsJSON, createdAt, updatedAt string
	var version int
	if err := scan(&id, &graphID, &category, &controlKind, &title, &status, &outcome,
		&metadataJSON, &actionLogsJSON, &createdAt, &updatedAt, &version); err != nil {
		return nil, err
	}
	return orch.RehydrateNode(orch.RehydrateNodeInput{
		ID:          orch.NodeID(id),
		GraphID:     orch.GraphID(graphID),
		Category:    orch.NodeCategory(category),
		ControlKind: orch.ControlKind(controlKind),
		Title:       title,
		Status:      orch.NodeStatus(status),
		Outcome:     outcome,
		Metadata:    unmarshalMetadata(metadataJSON),
		ActionLogs:  unmarshalActionLogs(actionLogsJSON),
		CreatedAt:   parseTime(createdAt),
		UpdatedAt:   parseTime(updatedAt),
		Version:     version,
	})
}

var _ orch.NodeRepository = (*NodeRepo)(nil)
