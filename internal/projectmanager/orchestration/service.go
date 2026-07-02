package orchestration

import (
	"context"
	"database/sql"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
)

// ServiceDeps groups all dependencies for NewService.
type ServiceDeps struct {
	DB     *sql.DB
	Graphs GraphRepository
	Nodes  NodeRepository
	Edges  EdgeRepository
	IDGen  idgen.Generator
	Clock  clock.Clock
}

// Service is a thin application-service layer that wraps the orchestration
// domain model. It owns transaction boundaries via persistence.RunInTx,
// delegates ID generation to idgen.Generator, and clocks mutations via
// clock.Clock.
type Service struct {
	db     *sql.DB
	graphs GraphRepository
	nodes  NodeRepository
	edges  EdgeRepository
	idgen  idgen.Generator
	clock  clock.Clock
}

// NewService constructs a Service from the given dependencies.
func NewService(deps ServiceDeps) *Service {
	return &Service{
		db:     deps.DB,
		graphs: deps.Graphs,
		nodes:  deps.Nodes,
		edges:  deps.Edges,
		idgen:  deps.IDGen,
		clock:  deps.Clock,
	}
}

// ---------------------------------------------------------------------------
// Graph lifecycle
// ---------------------------------------------------------------------------

// CreateGraph creates a new graph for the given planID. It auto-creates start
// and end control nodes and persists everything in a single transaction.
func (s *Service) CreateGraph(ctx context.Context, planID string) (GraphID, error) {
	id := GraphID(s.idgen.NewULID())
	now := s.clock.Now()
	g, err := NewGraph(NewGraphInput{ID: id, PlanID: planID, CreatedAt: now})
	if err != nil {
		return "", err
	}
	return id, persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.graphs.Save(txCtx, g); err != nil {
			return err
		}
		for _, n := range g.Nodes() {
			if err := s.nodes.Save(txCtx, n); err != nil {
				return err
			}
		}
		return nil
	})
}

// GetGraph returns the full graph aggregate (graph + nodes + edges) by ID.
func (s *Service) GetGraph(ctx context.Context, id GraphID) (*Graph, error) {
	return s.loadGraph(ctx, id)
}

// StartGraph transitions the graph from draft → running.
func (s *Service) StartGraph(ctx context.Context, id GraphID) error {
	g, err := s.graphs.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if err := g.Start(s.clock.Now()); err != nil {
		return err
	}
	return s.graphs.Update(ctx, g)
}

// FinishGraph transitions the graph from running → done.
func (s *Service) FinishGraph(ctx context.Context, id GraphID) error {
	g, err := s.graphs.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if err := g.Finish(s.clock.Now()); err != nil {
		return err
	}
	return s.graphs.Update(ctx, g)
}

// ---------------------------------------------------------------------------
// Node operations
// ---------------------------------------------------------------------------

// AddNode adds a new business or control node to the graph and persists it.
func (s *Service) AddNode(ctx context.Context, graphID GraphID, category, controlKind, title string, metadata map[string]any) (NodeID, error) {
	g, err := s.loadGraph(ctx, graphID)
	if err != nil {
		return "", err
	}
	id := NodeID(s.idgen.NewULID())
	now := s.clock.Now()
	n, err := g.AddNode(NewNodeInput{
		ID:          id,
		GraphID:     graphID,
		Category:    NodeCategory(category),
		ControlKind: ControlKind(controlKind),
		Title:       title,
		Metadata:    metadata,
		CreatedAt:   now,
	})
	if err != nil {
		return "", err
	}
	return id, persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.nodes.Save(txCtx, n); err != nil {
			return err
		}
		return s.graphs.Update(txCtx, g)
	})
}

// RemoveNode removes a node (and its edges) from the graph and deletes it from
// the repository. The domain enforces that running/completed nodes cannot be
// removed.
func (s *Service) RemoveNode(ctx context.Context, id NodeID) error {
	n, err := s.nodes.FindByID(ctx, id)
	if err != nil {
		return err
	}
	graphID := n.GraphID()
	g, err := s.loadGraph(ctx, graphID)
	if err != nil {
		return err
	}
	// Capture edges that touch this node before the domain removes them.
	var affectedEdges []Edge
	for _, e := range g.Edges() {
		if e.FromNodeID == id || e.ToNodeID == id {
			affectedEdges = append(affectedEdges, e)
		}
	}
	if err := g.RemoveNode(id); err != nil {
		return err
	}
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		for _, e := range affectedEdges {
			if err := s.edges.Delete(txCtx, graphID, e.FromNodeID, e.ToNodeID); err != nil {
				return err
			}
		}
		return s.nodes.Delete(txCtx, id)
	})
}

// UpdateNode updates the title and metadata of a node.
func (s *Service) UpdateNode(ctx context.Context, id NodeID, title string, metadata map[string]any) error {
	n, err := s.nodes.FindByID(ctx, id)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	if err := n.SetTitle(title, now); err != nil {
		return err
	}
	n.SetMetadata(metadata, now)
	return s.nodes.Update(ctx, n)
}

// StartNode transitions a node to running status.
func (s *Service) StartNode(ctx context.Context, id NodeID) error {
	n, err := s.nodes.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if err := n.Start(s.clock.Now()); err != nil {
		return err
	}
	return s.nodes.Update(ctx, n)
}

// CompleteNode transitions a node to completed status with the given outcome.
func (s *Service) CompleteNode(ctx context.Context, id NodeID, outcome string) error {
	n, err := s.nodes.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if err := n.Complete(outcome, s.clock.Now()); err != nil {
		return err
	}
	return s.nodes.Update(ctx, n)
}

// ReopenNode transitions a completed node back to reopen status (Completed→Reopen)
// with the given reason, so it re-enters the ready-set for another round. Used by
// the T768 graph dispatch to propagate a task that was reopened (e.g. a decision
// loopback re-activating an upstream node) onto its bound graph node.
func (s *Service) ReopenNode(ctx context.Context, id NodeID, reason string) error {
	n, err := s.nodes.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if err := n.Reopen(reason, s.clock.Now()); err != nil {
		return err
	}
	return s.nodes.Update(ctx, n)
}

// DiscardNode transitions a node to discarded status.
func (s *Service) DiscardNode(ctx context.Context, id NodeID) error {
	n, err := s.nodes.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if err := n.Discard(s.clock.Now()); err != nil {
		return err
	}
	return s.nodes.Update(ctx, n)
}

// ResolveCondition evaluates a condition node's result ("success" or any other
// value treated as failure). On failure it reopens the relevant upstream chain.
// All modified nodes are persisted in a single transaction.
func (s *Service) ResolveCondition(ctx context.Context, id NodeID, result string) error {
	n, err := s.nodes.FindByID(ctx, id)
	if err != nil {
		return err
	}
	g, err := s.loadGraph(ctx, n.GraphID())
	if err != nil {
		return err
	}
	cfg, err := ParseConditionConfig(n.Metadata())
	if err != nil {
		return err
	}
	// Snapshot node versions before mutation to detect which ones changed.
	versionsBefore := make(map[NodeID]int, len(g.Nodes()))
	for _, node := range g.Nodes() {
		versionsBefore[node.ID()] = node.Version()
	}
	success := result == "success"
	if err := ApplyConditionResult(g, id, cfg, success, s.clock.Now()); err != nil {
		return err
	}
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		for _, node := range g.Nodes() {
			if node.Version() != versionsBefore[node.ID()] {
				if err := s.nodes.Update(txCtx, node); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// Edge operations
// ---------------------------------------------------------------------------

// AddEdge adds a directed edge from→to within a graph. The full graph is
// loaded so the domain can validate acyclicity.
func (s *Service) AddEdge(ctx context.Context, graphID GraphID, from, to NodeID) error {
	g, err := s.loadGraph(ctx, graphID)
	if err != nil {
		return err
	}
	if err := g.AddEdge(from, to); err != nil {
		return err
	}
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.edges.Save(txCtx, graphID, Edge{FromNodeID: from, ToNodeID: to}); err != nil {
			return err
		}
		return s.graphs.Update(txCtx, g)
	})
}

// RemoveEdge removes a directed edge from→to within a graph. Idempotent.
func (s *Service) RemoveEdge(ctx context.Context, graphID GraphID, from, to NodeID) error {
	return s.edges.Delete(ctx, graphID, from, to)
}

// ---------------------------------------------------------------------------
// Queries
// ---------------------------------------------------------------------------

// GetNode returns a single node by ID.
func (s *Service) GetNode(ctx context.Context, id NodeID) (*Node, error) {
	return s.nodes.FindByID(ctx, id)
}

// ListNodes returns all nodes belonging to a graph.
func (s *Service) ListNodes(ctx context.Context, graphID GraphID) ([]*Node, error) {
	return s.nodes.ListByGraph(ctx, graphID)
}

// GetReadyNodes loads the full graph and returns nodes whose upstream
// dependencies are all completed or discarded.
func (s *Service) GetReadyNodes(ctx context.Context, graphID GraphID) ([]*Node, error) {
	g, err := s.loadGraph(ctx, graphID)
	if err != nil {
		return nil, err
	}
	return g.ReadyNodes(), nil
}

// ---------------------------------------------------------------------------
// Task binding
// ---------------------------------------------------------------------------

// BindTask stores a taskID in the node's metadata under the key "task_id".
func (s *Service) BindTask(ctx context.Context, nodeID NodeID, taskID string) error {
	n, err := s.nodes.FindByID(ctx, nodeID)
	if err != nil {
		return err
	}
	meta := n.Metadata()
	meta["task_id"] = taskID
	n.SetMetadata(meta, s.clock.Now())
	return s.nodes.Update(ctx, n)
}

// UnbindTask removes the "task_id" key from the node's metadata.
func (s *Service) UnbindTask(ctx context.Context, nodeID NodeID) error {
	n, err := s.nodes.FindByID(ctx, nodeID)
	if err != nil {
		return err
	}
	meta := n.Metadata()
	delete(meta, "task_id")
	n.SetMetadata(meta, s.clock.Now())
	return s.nodes.Update(ctx, n)
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// loadGraph fetches the graph header, all its nodes, and all its edges from
// their respective repositories and rehydrates the domain aggregate.
func (s *Service) loadGraph(ctx context.Context, id GraphID) (*Graph, error) {
	g, err := s.graphs.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	nodes, err := s.nodes.ListByGraph(ctx, id)
	if err != nil {
		return nil, err
	}
	edges, err := s.edges.ListByGraph(ctx, id)
	if err != nil {
		return nil, err
	}
	return RehydrateGraph(RehydrateGraphInput{
		ID:        g.ID(),
		PlanID:    g.PlanID(),
		Status:    g.Status(),
		StartNode: g.StartNodeID(),
		EndNode:   g.EndNodeID(),
		Nodes:     nodes,
		Edges:     edges,
		CreatedAt: g.CreatedAt(),
		UpdatedAt: g.UpdatedAt(),
		Version:   g.Version(),
	})
}
