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
	// ListByGraphs is the BATCHED counterpart of ListByGraph (issue-77cda494): it
	// returns the nodes of ALL the given graphs in ONE query (IN(...)), so a
	// multi-plan read (list_plans stage-aware view) pays a CONSTANT number of graph
	// reads instead of one-per-plan (N+1). Empty input ⇒ empty result, no query.
	ListByGraphs(ctx context.Context, graphIDs []GraphID) ([]*Node, error)
	Delete(ctx context.Context, id NodeID) error
}

// EdgeRepository persists edges within a Graph.
type EdgeRepository interface {
	Save(ctx context.Context, graphID GraphID, e Edge) error
	Delete(ctx context.Context, graphID GraphID, from, to NodeID) error
	ListByGraph(ctx context.Context, graphID GraphID) ([]Edge, error)
	// ListByGraphs is the BATCHED counterpart of ListByGraph (issue-77cda494):
	// the edges of ALL the given graphs in ONE query. Node ids are globally unique,
	// so callers key results off the node ids directly (no per-edge graph tag).
	ListByGraphs(ctx context.Context, graphIDs []GraphID) ([]Edge, error)
}
