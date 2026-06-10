package projectmanager

// Plan execution-DAG value logic (v2.9 #283, design §2/§9.8). PURE domain — no
// I/O. The DAG is a set of depends_on edges scoped to exactly one Plan (§9.8):
// every Dependency carries its PlanID and two plans' edges are never mixed in a
// single edge set.

// Dependency is one depends_on edge: FromTaskID depends_on ToTaskID (i.e.
// FromTaskID may only start once ToTaskID is done). Scoped to one Plan (§9.8).
type Dependency struct {
	PlanID     PlanID
	FromTaskID TaskID
	ToTaskID   TaskID
}

// ValidateNoCycle reports ErrPlanCycle if the edge set contains any cycle. It
// runs an iterative DFS with three-color marking over the directed graph
// induced by the edges (from → to). The edge set is assumed to belong to a
// single Plan; callers pass one plan's edges.
func ValidateNoCycle(edges []Dependency) error {
	// Build adjacency: from → []to.
	adj := make(map[TaskID][]TaskID, len(edges))
	nodes := make(map[TaskID]struct{}, len(edges)*2)
	for _, e := range edges {
		adj[e.FromTaskID] = append(adj[e.FromTaskID], e.ToTaskID)
		nodes[e.FromTaskID] = struct{}{}
		nodes[e.ToTaskID] = struct{}{}
	}
	const (
		white = 0 // unvisited
		gray  = 1 // on the current DFS stack
		black = 2 // fully explored
	)
	color := make(map[TaskID]int, len(nodes))
	// Iterative DFS so deep chains can't blow the stack.
	for n := range nodes {
		if color[n] != white {
			continue
		}
		type frame struct {
			node TaskID
			next int
		}
		stack := []frame{{node: n}}
		color[n] = gray
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			children := adj[top.node]
			if top.next < len(children) {
				child := children[top.next]
				top.next++
				switch color[child] {
				case gray:
					return ErrPlanCycle // back-edge → cycle
				case white:
					color[child] = gray
					stack = append(stack, frame{node: child})
				}
				continue
			}
			color[top.node] = black
			stack = stack[:len(stack)-1]
		}
	}
	return nil
}

// WouldCreateCycle is the pre-persist guard the repo/service calls before
// inserting an edge. It returns ErrSelfDependency if add is a self-edge
// (from==to), else ErrPlanCycle if existing+add would contain a cycle, else nil.
// existing should be the target Plan's current edge set.
func WouldCreateCycle(existing []Dependency, add Dependency) error {
	if add.FromTaskID == add.ToTaskID {
		return ErrSelfDependency
	}
	combined := make([]Dependency, 0, len(existing)+1)
	combined = append(combined, existing...)
	combined = append(combined, add)
	return ValidateNoCycle(combined)
}
