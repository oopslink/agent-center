package orchestration

// Edge is a directed dependency: ToNodeID depends on FromNodeID completing first.
type Edge struct {
	FromNodeID NodeID
	ToNodeID   NodeID
}

// ValidateNoCycle checks that adding `add` to `existing` edges does not create
// a cycle. Also rejects self-edges. Uses iterative DFS with three-color marking.
func ValidateNoCycle(existing []Edge, add Edge) error {
	if add.FromNodeID == add.ToNodeID {
		return ErrSelfEdge
	}

	// Build adjacency list including the new edge.
	adj := map[NodeID][]NodeID{}
	for _, e := range existing {
		adj[e.FromNodeID] = append(adj[e.FromNodeID], e.ToNodeID)
	}
	adj[add.FromNodeID] = append(adj[add.FromNodeID], add.ToNodeID)

	// Collect all nodes.
	nodes := map[NodeID]bool{}
	for _, e := range existing {
		nodes[e.FromNodeID] = true
		nodes[e.ToNodeID] = true
	}
	nodes[add.FromNodeID] = true
	nodes[add.ToNodeID] = true

	// Three-color DFS: 0=white, 1=gray(in-stack), 2=black(done).
	color := map[NodeID]int{}
	for n := range nodes {
		color[n] = 0
	}

	for n := range nodes {
		if color[n] != 0 {
			continue
		}
		// Iterative DFS using an explicit stack.
		type frame struct {
			node NodeID
			idx  int // next child index to visit
		}
		stack := []frame{{node: n, idx: 0}}
		color[n] = 1

		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			children := adj[top.node]
			if top.idx < len(children) {
				child := children[top.idx]
				top.idx++
				if color[child] == 1 {
					return ErrCycleDetected
				}
				if color[child] == 0 {
					color[child] = 1
					stack = append(stack, frame{node: child, idx: 0})
				}
			} else {
				color[top.node] = 2
				stack = stack[:len(stack)-1]
			}
		}
	}
	return nil
}

// ReopenChain computes all nodes on any path from `target` to `from` (exclusive
// of `from` itself) by reverse-traversing the edge graph. Used by condition
// failure to determine which nodes to reopen. Returns nodes in reverse
// topological order (closest to `from` first, `target` last).
func ReopenChain(edges []Edge, from, target NodeID) []NodeID {
	// Build reverse adjacency: for each edge a->b, reverse[b] = append(reverse[b], a)
	reverse := map[NodeID][]NodeID{}
	for _, e := range edges {
		reverse[e.ToNodeID] = append(reverse[e.ToNodeID], e.FromNodeID)
	}

	// BFS backwards from `from` to find all nodes that can reach `target`.
	visited := map[NodeID]bool{}
	queue := []NodeID{from}
	visited[from] = true
	reachable := map[NodeID]bool{}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, pred := range reverse[cur] {
			if visited[pred] {
				continue
			}
			visited[pred] = true
			reachable[pred] = true
			queue = append(queue, pred)
		}
	}

	if !reachable[target] {
		return nil // no path from target to from
	}

	// Now forward BFS from target, collecting only nodes that are also reachable.
	// This gives us exactly the nodes on paths between target and from.
	forward := map[NodeID][]NodeID{}
	for _, e := range edges {
		forward[e.FromNodeID] = append(forward[e.FromNodeID], e.ToNodeID)
	}

	onPath := map[NodeID]bool{}
	fwdQueue := []NodeID{target}
	fwdVisited := map[NodeID]bool{target: true}

	for len(fwdQueue) > 0 {
		cur := fwdQueue[0]
		fwdQueue = fwdQueue[1:]
		if cur == from {
			continue // don't go past `from`
		}
		onPath[cur] = true
		for _, succ := range forward[cur] {
			if fwdVisited[succ] {
				continue
			}
			if reachable[succ] || succ == from {
				fwdVisited[succ] = true
				fwdQueue = append(fwdQueue, succ)
			}
		}
	}

	// Build result: reverse topological order (closest to from first).
	// Simple approach: BFS backward from `from`, only include onPath nodes.
	var result []NodeID
	backQueue := []NodeID{from}
	backVisited := map[NodeID]bool{from: true}
	for len(backQueue) > 0 {
		cur := backQueue[0]
		backQueue = backQueue[1:]
		for _, pred := range reverse[cur] {
			if backVisited[pred] {
				continue
			}
			if onPath[pred] {
				backVisited[pred] = true
				result = append(result, pred)
				backQueue = append(backQueue, pred)
			}
		}
	}
	return result
}
