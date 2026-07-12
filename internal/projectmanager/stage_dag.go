package projectmanager

// Plan Stage structural value logic (2026-07-03 plan-stage-model design §4.2/§5).
// PURE domain — no I/O. These functions compute the stage grouping / entries and
// enforce the build-time structural invariants that buildPlanGraph relies on to lay
// the stages onto the orchestration graph correctly.

// StageOf builds the task→stage membership map from a task set: a task with a
// non-empty stage_id maps to that StageID; a stageless task is omitted (so a lookup
// on it returns the zero StageID ""). It is the single place the "节点 stage_id" is
// read into the structural functions below.
func StageOf(tasks []*Task) map[TaskID]StageID {
	out := make(map[TaskID]StageID, len(tasks))
	for _, t := range tasks {
		if sid := t.StageID(); sid != "" {
			out[t.ID()] = sid
		}
	}
	return out
}

// StageMembers returns the task ids that belong to stage `sid`, preserving the input
// order (deterministic落图). Empty when the stage has no members.
func StageMembers(tasks []*Task, sid StageID) []TaskID {
	var out []TaskID
	for _, t := range tasks {
		if t.StageID() == sid {
			out = append(out, t.ID())
		}
	}
	return out
}

// StageEntries returns a stage's ENTRY nodes (§5 "stage 入口 = stage 内无前驱者"): the
// member tasks that have no in-stage predecessor via a FORWARD dependency edge. A
// downstream stage's barrier attaches to these (they depend_on the upstream stages'
// gates). Loopback back-edges are excluded (they never define a forward predecessor).
//
// `members` is the stage's task-id set; `stageOf` is the plan-wide membership map;
// `edges` is the plan's full edge set. An entry has no incoming forward edge from
// ANOTHER member of the SAME stage.
func StageEntries(members []TaskID, stageOf map[TaskID]StageID, sid StageID, edges []Dependency) []TaskID {
	memberSet := make(map[TaskID]struct{}, len(members))
	for _, m := range members {
		memberSet[m] = struct{}{}
	}
	// A forward dependency edge {From depends_on To} means To runs before From, i.e.
	// From has an in-stage predecessor To. Collect the members that are such a `From`.
	hasInStagePred := make(map[TaskID]bool, len(members))
	for _, e := range forwardEdges(edges) {
		if _, isMember := memberSet[e.FromTaskID]; !isMember {
			continue
		}
		if stageOf[e.ToTaskID] == sid { // predecessor is in the SAME stage
			hasInStagePred[e.FromTaskID] = true
		}
	}
	var out []TaskID
	for _, m := range members {
		if !hasInStagePred[m] {
			out = append(out, m)
		}
	}
	return out
}

// ValidateStageEdges enforces the build-time cross-stage invariant (§5): a manual plan
// edge between two tasks in DIFFERENT stages BYPASSES the stage gate/barrier and is
// rejected. Cross-stage flow must go through the auto-generated gate barrier (a
// downstream stage's entry depends_on the upstream stage's gate node), never a
// hand-drawn business→business edge. An edge touching a STAGELESS task (one endpoint
// with no stage_id) is allowed — a stageless node is not governed by a stage barrier.
// Only an edge whose BOTH endpoints are in real but DIFFERENT stages is rejected.
func ValidateStageEdges(stageOf map[TaskID]StageID, edges []Dependency) error {
	for _, e := range edges {
		from, to := stageOf[e.FromTaskID], stageOf[e.ToTaskID]
		if from == "" || to == "" {
			continue // at least one endpoint is stageless — not a cross-stage edge.
		}
		if from != to {
			return ErrStageCrossEdge
		}
	}
	return nil
}

// ValidateStageDAG validates the OUTER stage DAG (§4.2): every depends_on target must
// be an existing stage of the SAME plan, and the depends_on relation must be acyclic
// (stages form a DAG, not a graph with back-edges). Returns ErrStageCrossPlanDependency
// for a dangling / cross-plan target and ErrStageCycle for a cycle.
func ValidateStageDAG(stages []*Stage) error {
	byID := make(map[StageID]*Stage, len(stages))
	for _, s := range stages {
		byID[s.ID()] = s
	}
	// Every depends_on target must exist within this stage set (same plan).
	for _, s := range stages {
		for _, dep := range s.DependsOnStages() {
			if _, ok := byID[dep]; !ok {
				return ErrStageCrossPlanDependency
			}
		}
	}
	// Iterative three-color DFS over the depends_on graph (s → its upstreams).
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[StageID]int, len(stages))
	for _, root := range stages {
		if color[root.ID()] != white {
			continue
		}
		type frame struct {
			id   StageID
			next int
		}
		stack := []frame{{id: root.ID()}}
		color[root.ID()] = gray
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			deps := byID[top.id].DependsOnStages()
			if top.next < len(deps) {
				child := deps[top.next]
				top.next++
				switch color[child] {
				case gray:
					return ErrStageCycle
				case white:
					color[child] = gray
					stack = append(stack, frame{id: child})
				}
				continue
			}
			color[top.id] = black
			stack = stack[:len(stack)-1]
		}
	}
	return nil
}
