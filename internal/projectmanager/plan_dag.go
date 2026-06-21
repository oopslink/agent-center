package projectmanager

// Plan execution-DAG value logic (v2.9 #283, design §2/§9.8). PURE domain — no
// I/O. The DAG is a set of edges scoped to exactly one Plan (§9.8): every
// Dependency carries its PlanID and two plans' edges are never mixed in a single
// edge set.
//
// v2.13.0 I18/B1 (control-flow engine, docs/design/v2.13.0/control-flow-engine-spec.md):
// edges gain a Kind (seq/conditional/loopback) + When (outcome label) + MaxRounds
// (loopback bound). The zero value (Kind=="" → seq, When=="", MaxRounds==0) is the
// pre-B1 hard AND dependency, so existing plans are unchanged.

// EdgeKind is the control-flow kind of a plan edge (B1 §2.2). The ZERO value (and
// the stored default) normalizes to EdgeSeq, so an edge with no kind == today's hard
// AND dependency — the backward-compat anchor.
type EdgeKind string

const (
	// EdgeSeq is the default hard AND dependency: From may start only once To is
	// done. Identical to the pre-B1 Dependency semantics.
	EdgeSeq EdgeKind = "seq"
	// EdgeConditional routes by a decision's outcome: the edge is ACTIVE only when
	// its To (a decision node) completed with outcome == When. Otherwise the edge is
	// "dead" and prunes its From on that path (→ NodeSkipped).
	EdgeConditional EdgeKind = "conditional"
	// EdgeLoopback is a BOUNDED back-edge: From is a decision, To is a forward
	// ancestor (e.g. Dev). When From's outcome == When (e.g. "reject") the To-subgraph
	// is re-activated for another round, capped by MaxRounds. Excluded from forward
	// readiness + acyclic validation (it intentionally forms a cycle).
	EdgeLoopback EdgeKind = "loopback"
)

// NormalizeEdgeKind maps "" (old rows / unset) → EdgeSeq so the zero value is the
// backward-compatible default everywhere it is read.
func NormalizeEdgeKind(k EdgeKind) EdgeKind {
	if k == "" {
		return EdgeSeq
	}
	return k
}

// Dependency is one plan edge. For EdgeSeq (default) it is a depends_on edge:
// FromTaskID depends_on ToTaskID (FromTaskID may only start once ToTaskID is done).
// For EdgeConditional/EdgeLoopback it carries control-flow routing (Kind/When) and,
// for loopback, the round bound (MaxRounds). Scoped to one Plan (§9.8). B1:
// Kind/When/MaxRounds are additive — a zero-valued ("seq"/""/0) edge is the pre-B1
// hard dependency, so existing plans are unchanged.
type Dependency struct {
	PlanID     PlanID
	FromTaskID TaskID
	ToTaskID   TaskID
	Kind       EdgeKind // "" == EdgeSeq (back-compat)
	When       string   // outcome label for conditional/loopback; "" for seq
	MaxRounds  int      // loopback round cap (≥1); 0 for non-loopback
}

// IsLoopback reports whether this edge is a (back-edge) loopback. Loopback edges
// are excluded from forward readiness + acyclic validation.
func (d Dependency) IsLoopback() bool { return NormalizeEdgeKind(d.Kind) == EdgeLoopback }

// forwardEdges returns the edges that participate in the FORWARD graph (seq +
// conditional) — i.e. everything except loopback back-edges. Used for both acyclic
// validation and node-status derivation (loopback never blocks/forms a cycle there).
func forwardEdges(edges []Dependency) []Dependency {
	out := make([]Dependency, 0, len(edges))
	for _, e := range edges {
		if !e.IsLoopback() {
			out = append(out, e)
		}
	}
	return out
}

// ValidateNoCycle reports ErrPlanCycle if the FORWARD edge set (seq + conditional)
// contains any cycle. Loopback edges are intentional back-edges and are EXCLUDED (B1
// §6.6) — they are validated separately by ValidateLoopback. It runs an iterative
// DFS with three-color marking over the directed graph induced by the forward edges
// (from → to). The edge set is assumed to belong to a single Plan.
func ValidateNoCycle(edges []Dependency) error {
	fwd := forwardEdges(edges)
	// Build adjacency: from → []to.
	adj := make(map[TaskID][]TaskID, len(fwd))
	nodes := make(map[TaskID]struct{}, len(fwd)*2)
	for _, e := range fwd {
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

// ValidateLoopback validates a single loopback edge against the plan's FORWARD graph
// (B1 §6.6): the edge must carry a When label and MaxRounds≥1, and its To must be a
// forward ANCESTOR of its From (so the back-edge re-activates an already-upstream
// node — a real loop, not a leap). `existing` is the plan's current edge set
// (forward edges define ancestry). Returns ErrInvalidLoopback on violation.
func ValidateLoopback(existing []Dependency, add Dependency) error {
	if !add.IsLoopback() {
		return nil
	}
	if add.When == "" || add.MaxRounds < 1 {
		return ErrInvalidLoopback
	}
	if add.FromTaskID == add.ToTaskID {
		return ErrInvalidLoopback
	}
	// To must be a forward (EXECUTION) ancestor of From: there is an execution path
	// To → … → From. Execution flows along dep.To → dep.From (the dependency To runs
	// before From), so we walk that direction.
	if !execReachable(forwardEdges(existing), add.ToTaskID, add.FromTaskID) {
		return ErrInvalidLoopback
	}
	return nil
}

// execReachable reports whether `target` is reachable from `start` following
// EXECUTION edges (dep.To → dep.From — the dependency To runs before From). Iterative
// DFS, cycle-safe via a visited set.
func execReachable(fwd []Dependency, start, target TaskID) bool {
	adj := execAdjacency(fwd)
	visited := map[TaskID]bool{start: true}
	stack := []TaskID{start}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, c := range adj[n] {
			if c == target {
				return true
			}
			if !visited[c] {
				visited[c] = true
				stack = append(stack, c)
			}
		}
	}
	return false
}

// execAdjacency builds the EXECUTION-direction adjacency (dep.To → []dep.From) over
// forward edges — i.e. "what runs AFTER this node".
func execAdjacency(fwd []Dependency) map[TaskID][]TaskID {
	adj := make(map[TaskID][]TaskID, len(fwd))
	for _, e := range fwd {
		adj[e.ToTaskID] = append(adj[e.ToTaskID], e.FromTaskID)
	}
	return adj
}

// LoopbackResetSet returns the nodes to re-activate when a loopback edge fires
// (B1 §4.2): every node on a FORWARD path from `to` (the loop target, e.g. Dev) to
// `from` (the decision), inclusive of both. These are the nodes a reject re-runs.
// Computed as forward-reachable-from-`to` ∩ can-forward-reach-`from`, so a linear
// Dev→Review→Decision chain yields exactly {Dev, Review, Decision}.
func LoopbackResetSet(edges []Dependency, to, from TaskID) []TaskID {
	fwd := forwardEdges(edges)
	execAdj := execAdjacency(fwd) // To → From ("what runs AFTER")
	depAdj := make(map[TaskID][]TaskID, len(fwd))
	for _, e := range fwd { // From → To ("what runs BEFORE") = reverse of execution
		depAdj[e.FromTaskID] = append(depAdj[e.FromTaskID], e.ToTaskID)
	}
	afterTo := closure(execAdj, to)     // nodes at/after `to` in execution (incl. to)
	beforeFrom := closure(depAdj, from) // nodes at/before `from` in execution (incl. from)
	var out []TaskID
	seen := map[TaskID]bool{}
	// Deterministic order: walk execution-forward from `to`.
	stack := []TaskID{to}
	visited := map[TaskID]bool{}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[n] {
			continue
		}
		visited[n] = true
		if afterTo[n] && beforeFrom[n] && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
		for _, c := range execAdj[n] {
			stack = append(stack, c)
		}
	}
	return out
}

// closure returns the set of nodes reachable from `start` (inclusive) over the
// given adjacency.
func closure(adj map[TaskID][]TaskID, start TaskID) map[TaskID]bool {
	seen := map[TaskID]bool{start: true}
	stack := []TaskID{start}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, c := range adj[n] {
			if !seen[c] {
				seen[c] = true
				stack = append(stack, c)
			}
		}
	}
	return seen
}

// WouldCreateCycle is the pre-persist guard the repo/service calls before inserting
// an edge. It returns ErrSelfDependency if add is a self-edge (from==to). For a
// loopback edge it instead enforces ValidateLoopback (the loopback IS a back-edge by
// design, so the acyclic check does not apply to it). For seq/conditional edges it
// returns ErrPlanCycle if existing+add's FORWARD graph would contain a cycle.
// existing should be the target Plan's current edge set.
func WouldCreateCycle(existing []Dependency, add Dependency) error {
	if add.IsLoopback() {
		return ValidateLoopback(existing, add)
	}
	if add.FromTaskID == add.ToTaskID {
		return ErrSelfDependency
	}
	combined := make([]Dependency, 0, len(existing)+1)
	combined = append(combined, existing...)
	combined = append(combined, add)
	return ValidateNoCycle(combined)
}
