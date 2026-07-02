package orchestration

import (
	"strings"
	"time"
)

// Graph is the aggregate root: a DAG of Nodes connected by Edges.
type Graph struct {
	id        GraphID
	planID    string
	status    GraphStatus
	nodes     map[NodeID]*Node
	edges     []Edge
	startNode NodeID
	endNode   NodeID
	createdAt time.Time
	updatedAt time.Time
	version   int
}

// NewGraphInput captures constructor args.
type NewGraphInput struct {
	ID          GraphID
	PlanID      string
	StartNodeID NodeID // optional; auto-generated if empty
	EndNodeID   NodeID // optional; auto-generated if empty
	CreatedAt   time.Time
}

func NewGraph(in NewGraphInput) (*Graph, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, ErrMissingRequiredField
	}
	at := in.CreatedAt
	if at.IsZero() {
		at = time.Now()
	}

	startID := in.StartNodeID
	if startID == "" {
		startID = NodeID(string(in.ID) + ":start")
	}
	endID := in.EndNodeID
	if endID == "" {
		endID = NodeID(string(in.ID) + ":end")
	}

	g := &Graph{
		id:        in.ID,
		planID:    in.PlanID,
		status:    GraphDraft,
		nodes:     map[NodeID]*Node{},
		edges:     nil,
		startNode: startID,
		endNode:   endID,
		createdAt: at.UTC(),
		updatedAt: at.UTC(),
		version:   1,
	}

	// Auto-create start + end control nodes.
	start, _ := NewNode(NewNodeInput{
		ID: startID, GraphID: in.ID, Category: NodeCategoryControl,
		ControlKind: ControlKindStart, Title: "Start", CreatedAt: at,
	})
	end, _ := NewNode(NewNodeInput{
		ID: endID, GraphID: in.ID, Category: NodeCategoryControl,
		ControlKind: ControlKindEnd, Title: "End", CreatedAt: at,
	})
	g.nodes[startID] = start
	g.nodes[endID] = end
	return g, nil
}

// RehydrateGraphInput for persistence round-trip.
type RehydrateGraphInput struct {
	ID        GraphID
	PlanID    string
	Status    GraphStatus
	StartNode NodeID
	EndNode   NodeID
	Nodes     []*Node
	Edges     []Edge
	CreatedAt time.Time
	UpdatedAt time.Time
	Version   int
}

func RehydrateGraph(in RehydrateGraphInput) (*Graph, error) {
	if !in.Status.IsValid() {
		return nil, ErrIllegalTransition
	}
	if in.Version < 1 {
		return nil, ErrMissingRequiredField
	}
	nodes := map[NodeID]*Node{}
	for _, n := range in.Nodes {
		nodes[n.ID()] = n
	}
	return &Graph{
		id:        in.ID,
		planID:    in.PlanID,
		status:    in.Status,
		nodes:     nodes,
		edges:     in.Edges,
		startNode: in.StartNode,
		endNode:   in.EndNode,
		createdAt: in.CreatedAt.UTC(),
		updatedAt: in.UpdatedAt.UTC(),
		version:   in.Version,
	}, nil
}

// Getters.
func (g *Graph) ID() GraphID         { return g.id }
func (g *Graph) PlanID() string      { return g.planID }
func (g *Graph) Status() GraphStatus { return g.status }
func (g *Graph) StartNodeID() NodeID { return g.startNode }
func (g *Graph) EndNodeID() NodeID   { return g.endNode }
func (g *Graph) CreatedAt() time.Time { return g.createdAt }
func (g *Graph) UpdatedAt() time.Time { return g.updatedAt }
func (g *Graph) Version() int        { return g.version }

func (g *Graph) FindNode(id NodeID) *Node { return g.nodes[id] }

func (g *Graph) Nodes() []*Node {
	out := make([]*Node, 0, len(g.nodes))
	for _, n := range g.nodes {
		out = append(out, n)
	}
	return out
}

func (g *Graph) Edges() []Edge {
	cp := make([]Edge, len(g.edges))
	copy(cp, g.edges)
	return cp
}

// Status transitions.

func (g *Graph) Start(at time.Time) error   { return g.transition(GraphRunning, at) }
func (g *Graph) Finish(at time.Time) error  { return g.transition(GraphDone, at) }
func (g *Graph) Archive(at time.Time) error { return g.transition(GraphArchived, at) }
func (g *Graph) Revert(at time.Time) error  { return g.transition(GraphDraft, at) }

func (g *Graph) transition(to GraphStatus, at time.Time) error {
	if !g.status.CanTransitionTo(to) {
		return ErrIllegalTransition
	}
	g.status = to
	g.touch(at)
	return nil
}

// Node operations.

func (g *Graph) AddNode(in NewNodeInput) (*Node, error) {
	in.GraphID = g.id
	if in.CreatedAt.IsZero() {
		in.CreatedAt = time.Now()
	}
	n, err := NewNode(in)
	if err != nil {
		return nil, err
	}
	if g.nodes[n.ID()] != nil {
		return nil, ErrNodeExists
	}
	g.nodes[n.ID()] = n
	g.touch(in.CreatedAt)
	return n, nil
}

func (g *Graph) RemoveNode(nodeID NodeID) error {
	n := g.nodes[nodeID]
	if n == nil {
		return ErrNodeNotFound
	}
	if n.Status() == NodeRunning || n.Status() == NodeCompleted {
		return ErrNodeNotRemovable
	}
	delete(g.nodes, nodeID)
	// Remove all edges referencing this node.
	filtered := make([]Edge, 0, len(g.edges))
	for _, e := range g.edges {
		if e.FromNodeID != nodeID && e.ToNodeID != nodeID {
			filtered = append(filtered, e)
		}
	}
	g.edges = filtered
	g.touch(time.Now())
	return nil
}

// Edge operations.

func (g *Graph) AddEdge(from, to NodeID) error {
	if g.nodes[from] == nil || g.nodes[to] == nil {
		return ErrNodeNotFound
	}
	// Check for duplicate.
	for _, e := range g.edges {
		if e.FromNodeID == from && e.ToNodeID == to {
			return ErrEdgeExists
		}
	}
	newEdge := Edge{FromNodeID: from, ToNodeID: to}
	if err := ValidateNoCycle(g.edges, newEdge); err != nil {
		return err
	}
	g.edges = append(g.edges, newEdge)
	g.touch(time.Now())
	return nil
}

func (g *Graph) RemoveEdge(from, to NodeID) error {
	for i, e := range g.edges {
		if e.FromNodeID == from && e.ToNodeID == to {
			g.edges = append(g.edges[:i], g.edges[i+1:]...)
			g.touch(time.Now())
			return nil
		}
	}
	return nil // idempotent: removing non-existent edge is a no-op
}

// ReadyNodes returns nodes in open/reopen status whose upstream
// dependencies are all completed or discarded.
func (g *Graph) ReadyNodes() []*Node {
	// Build set of upstream deps per node.
	upstream := map[NodeID][]NodeID{}
	for _, e := range g.edges {
		upstream[e.ToNodeID] = append(upstream[e.ToNodeID], e.FromNodeID)
	}

	var ready []*Node
	for _, n := range g.nodes {
		if n.Category() == NodeCategoryControl {
			continue
		}
		if n.Status() != NodeOpen && n.Status() != NodeReopen {
			continue
		}
		allDone := true
		for _, depID := range upstream[n.ID()] {
			dep := g.nodes[depID]
			if dep == nil {
				allDone = false
				break
			}
			depDone := dep.Status() == NodeCompleted || dep.Status() == NodeDiscarded
			// Control nodes (e.g. start) are treated as satisfied even when open:
			// they act as structural anchors and do not block downstream business nodes.
			if depDone || dep.Category() == NodeCategoryControl {
				continue
			}
			allDone = false
			break
		}
		if allDone {
			ready = append(ready, n)
		}
	}
	return ready
}

// IsAutoDone returns true when all business nodes are completed or discarded.
func (g *Graph) IsAutoDone() bool {
	for _, n := range g.nodes {
		if n.Category() == NodeCategoryControl {
			continue
		}
		if n.Status() != NodeCompleted && n.Status() != NodeDiscarded {
			return false
		}
	}
	return true
}

func (g *Graph) touch(at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	g.updatedAt = at.UTC()
	g.version++
}
