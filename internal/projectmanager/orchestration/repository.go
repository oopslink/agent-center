package orchestration

import "context"

// GraphRepository persists Graph aggregates.
type GraphRepository interface {
	Save(ctx context.Context, g *Graph) error
	Update(ctx context.Context, g *Graph) error
	FindByID(ctx context.Context, id GraphID) (*Graph, error)
	FindByPlanID(ctx context.Context, planID string) (*Graph, error)
	Delete(ctx context.Context, id GraphID) error
}

// NodeRepository persists Node entities within a Graph.
type NodeRepository interface {
	Save(ctx context.Context, n *Node) error
	Update(ctx context.Context, n *Node) error
	FindByID(ctx context.Context, id NodeID) (*Node, error)
	ListByGraph(ctx context.Context, graphID GraphID) ([]*Node, error)
	Delete(ctx context.Context, id NodeID) error
}

// EdgeRepository persists edges within a Graph.
type EdgeRepository interface {
	Save(ctx context.Context, graphID GraphID, e Edge) error
	Delete(ctx context.Context, graphID GraphID, from, to NodeID) error
	ListByGraph(ctx context.Context, graphID GraphID) ([]Edge, error)
}
