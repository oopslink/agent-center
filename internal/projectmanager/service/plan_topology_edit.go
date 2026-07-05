package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// =============================================================================
// edit_plan_topology — running-plan LIVE topology edit (2026-07-05 design,
// docs/design/features/2026-07-05-plan-live-topology-edit.md).
//
// ONE batch, atomic, version-checked (CAS): apply a whole ops list to an
// in-memory copy of the plan's node/edge set, validate ONLY the TERMINAL state
// (never the intermediate steps), then persist + version++ + audit + (running)
// rebuild the orchestration graph + dispatch the newly-ready nodes — all inside a
// SINGLE RunInTx write tx. SQLite's single-writer serialization + the CAS gives
// the concurrency isolation the design's §4 relies on:
//
//   - CAS (plan.version == base_version) governs edit-vs-edit: a concurrent edit
//     that already advanced the version makes this commit ErrPlanVersionConflict.
//   - The in-tx re-read of dispatch records + the mutability predicate govern
//     edit-vs-advance: an auto-advance that dispatched a node just before this
//     commit is seen (the node now has a dispatch record) → the node is immutable
//     → ErrPlanNodeInFlight. An edit that commits first restructures the graph, and
//     the following advance dispatches off the new topology.
//
// "只校验终态" (validate the terminal state only) is the batch's core value: a
// reorder is several remove+add ops whose INTERMEDIATE shape may be cyclic/orphaned,
// but only the RESULT must be legal — impossible with per-op tools (each step must be
// self-consistent). Edges are persisted in a cycle-free order (removals first, then
// forward additions, then loopback additions) so the repo's per-insert acyclic guard
// never trips on a transient cycle while the terminal set is acyclic.
// =============================================================================

// TopologyOpKind is the discriminator for a single edit_plan_topology op.
type TopologyOpKind string

const (
	// OpAddNode selects a project task into the plan as a new (edgeless, un-dispatched)
	// node. Always allowed structurally (a fresh node is inherently mutable).
	OpAddNode TopologyOpKind = "add_node"
	// OpRemoveNode removes a node from the plan (back to the backlog) plus every edge
	// touching it. Allowed iff the node AND every node depending on it are mutable.
	OpRemoveNode TopologyOpKind = "remove_node"
	// OpAddEdge adds a dependency edge From depends_on To (seq/conditional/loopback).
	// Allowed iff From is mutable (its in-edge set changes); To may be any state.
	OpAddEdge TopologyOpKind = "add_edge"
	// OpRemoveEdge removes the edge(s) From depends_on To. Allowed iff From is mutable.
	OpRemoveEdge TopologyOpKind = "remove_edge"
)

// TopologyOp is one op in an edit_plan_topology batch. TaskID names the node for
// add/remove_node; From/To (+ Kind/When/MaxRounds) name the edge for add/remove_edge.
type TopologyOp struct {
	Kind       TopologyOpKind
	TaskID     pm.TaskID
	FromTaskID pm.TaskID
	ToTaskID   pm.TaskID
	EdgeKind   pm.EdgeKind
	When       string
	MaxRounds  int
}

// EditPlanTopologyCommand is the whole batch: the plan, the base_version the caller
// read from get_plan (CAS), the ops, and the actor (must be a project member).
type EditPlanTopologyCommand struct {
	PlanID      pm.PlanID
	BaseVersion int
	Ops         []TopologyOp
	Actor       pm.IdentityRef
}

// EditPlanTopology applies an ops batch to a draft OR running plan atomically
// (2026-07-05 live-topology design §3/§4). Returns the newly-dispatched task ids
// (empty for a draft edit, or a running edit that readied nothing).
func (s *Service) EditPlanTopology(ctx context.Context, cmd EditPlanTopologyCommand) ([]pm.TaskID, error) {
	if s.plans == nil {
		return nil, ErrPlansUnavailable
	}
	now := s.clock.Now()
	var dispatched []pm.TaskID
	err := s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.plans.FindByID(txCtx, cmd.PlanID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, p.ProjectID(), cmd.Actor); err != nil {
			return err
		}
		if err := s.requireProjectMutable(txCtx, p.ProjectID()); err != nil {
			return err
		}
		// edit_plan_topology is the STRUCTURED-plan DAG editor. The built-in flat pool
		// has no DAG (ADR-0047) — its task set is managed via SelectTaskIntoPlan.
		if p.IsBuiltin() {
			return pm.ErrBuiltinPlanNoEdges
		}
		switch p.Status() {
		case pm.PlanDraft, pm.PlanRunning:
			// editable
		case pm.PlanArchived:
			return pm.ErrPlanArchived
		default: // done
			return pm.ErrPlanNotRunning
		}
		// §4.1 CAS — the optimistic-concurrency gate for edit-vs-edit. Under SQLite's
		// single-writer serialization the loaded version is the committed one, so a
		// mismatch means a concurrent edit already advanced the plan.
		if p.Version() != cmd.BaseVersion {
			return fmt.Errorf("%w: base_version=%d current=%d", pm.ErrPlanVersionConflict, cmd.BaseVersion, p.Version())
		}
		running := p.Status() == pm.PlanRunning

		// Load the CURRENT (pre-edit) snapshot: nodes, edges, dispatch records.
		curTasks, err := s.tasks.ListByPlan(txCtx, cmd.PlanID)
		if err != nil {
			return err
		}
		curEdges, err := s.plans.ListDependencies(txCtx, cmd.PlanID)
		if err != nil {
			return err
		}
		records, err := s.plans.ListDispatchRecords(txCtx, cmd.PlanID)
		if err != nil {
			return err
		}
		dispatchedSet := make(map[pm.TaskID]bool, len(records))
		for _, r := range records {
			dispatchedSet[r.TaskID] = true
		}
		taskByID := make(map[pm.TaskID]*pm.Task, len(curTasks))
		curNodes := make(map[pm.TaskID]bool, len(curTasks))
		for _, t := range curTasks {
			taskByID[t.ID()] = t
			curNodes[t.ID()] = true
		}

		// Apply the WHOLE batch to an in-memory copy (§4 step 2). Intermediate shapes
		// (transient cycles/orphans) are NOT validated — only the terminal result is.
		wm := newTopoWorkModel(curNodes, curEdges)
		if err := wm.apply(cmd.Ops); err != nil {
			return err
		}

		// Resolve every task the terminal graph references (added nodes may not be in
		// taskByID yet). All must exist, be same-project, and not belong to another plan.
		for id := range wm.nodes {
			if _, ok := taskByID[id]; ok {
				continue
			}
			t, terr := s.tasks.FindByID(txCtx, id)
			if terr != nil {
				return terr
			}
			if t.ProjectID() != p.ProjectID() {
				return pm.ErrPlanProjectMismatch
			}
			if cur := t.PlanID(); cur != "" && cur != cmd.PlanID {
				return pm.ErrTaskInOtherPlan
			}
			taskByID[id] = t
		}

		finalEdges := wm.edgeList()

		// ---- §4 step 3: validate the TERMINAL state ONLY. ----
		// (a) every edge endpoint is a node in the terminal graph (§9.8 scoping).
		for _, e := range finalEdges {
			if !wm.nodes[e.FromTaskID] || !wm.nodes[e.ToTaskID] {
				return fmt.Errorf("%w: edge %s→%s references a task not in the plan", pm.ErrPlanProjectMismatch, e.FromTaskID, e.ToTaskID)
			}
			if err := pm.ValidateControlEdgeShape(e); err != nil {
				return err
			}
		}
		// (b) acyclic forward graph + loopback ancestry over the TERMINAL edge set.
		if err := pm.ValidateNoCycle(finalEdges); err != nil {
			return err
		}
		for _, e := range finalEdges {
			if e.IsLoopback() {
				if err := pm.ValidateLoopback(finalEdges, e); err != nil {
					return err
				}
			}
			if e.FromTaskID == e.ToTaskID {
				return pm.ErrSelfDependency
			}
		}
		// (c) RUNNING only — every terminal node has a resolvable assignee (a live node
		// must be dispatchable). Draft defers this to start_plan (draft ≡ old per-op).
		if running {
			for id := range wm.nodes {
				if err := s.validateResolvableAssignee(txCtx, p, taskByID[id]); err != nil {
					return err
				}
			}
		}
		// (d) RUNNING only — every STRUCTURALLY-AFFECTED node must be mutable (§4/§6):
		// a node whose in-edge set changed, or a removed node. An immutable
		// (dispatched/running/terminal) affected node → ErrPlanNodeInFlight (named).
		if running {
			for _, id := range structurallyAffected(curNodes, curEdges, wm) {
				t := taskByID[id]
				if t == nil {
					continue // an added node — inherently mutable (new, un-dispatched).
				}
				if !pm.NodeMutable(t.Status(), dispatchedSet[id]) {
					return fmt.Errorf("%w: task %s", pm.ErrPlanNodeInFlight, id)
				}
			}
		}

		// ---- §4 step 4: persist the diff. ----
		addedNodes, removedNodes := wm.nodeDiff(curNodes)
		// Added nodes: select into the plan (+ ADD-ONLY participant delta, mirroring
		// SelectTaskIntoPlan so the assignee reaches the plan conversation).
		for _, id := range addedNodes {
			t := taskByID[id]
			if t.PlanID() == cmd.PlanID {
				continue // already in this plan (idempotent add).
			}
			if err := t.SetPlan(cmd.PlanID, now); err != nil {
				return err
			}
			if err := s.tasks.Update(txCtx, t); err != nil {
				return err
			}
			var participants []string
			if a := string(t.Assignee()); a != "" {
				participants = []string{a}
			}
			if err := s.emit(txCtx, EvtPlanParticipantsChanged,
				refsJSON(map[string]string{"plan_id": string(p.ID()), "project_id": string(p.ProjectID())}),
				planEventPayload{
					PlanID: string(p.ID()), ProjectID: string(p.ProjectID()),
					OwnerRef: "pm://plans/" + string(p.ID()), Participants: participants,
				}); err != nil {
				return err
			}
		}
		// Edge diff — remove FIRST, then add (forward before loopback) so no persist
		// step ever forms a transient cycle the repo's per-insert guard would reject.
		toRemove, toAdd := edgeDiff(curEdges, finalEdges)
		for _, e := range toRemove {
			if err := s.plans.RemoveDependency(txCtx, pm.Dependency{PlanID: cmd.PlanID, FromTaskID: e.FromTaskID, ToTaskID: e.ToTaskID}); err != nil {
				return err
			}
		}
		// Removed nodes: clear plan membership (their edges are already gone above).
		for _, id := range removedNodes {
			t := taskByID[id]
			if t == nil {
				continue
			}
			if err := t.ClearPlan(now); err != nil {
				return err
			}
			if err := s.tasks.Update(txCtx, t); err != nil {
				return err
			}
		}
		sort.SliceStable(toAdd, func(i, j int) bool {
			// forward edges (seq/conditional) before loopback back-edges.
			return !toAdd[i].IsLoopback() && toAdd[j].IsLoopback()
		})
		for _, e := range toAdd {
			e.PlanID = cmd.PlanID
			if err := s.plans.AddDependency(txCtx, e); err != nil {
				return err
			}
		}

		// ---- §5 layer 2: the ONE topology_commit audit entry. ----
		s.auditPlan(txCtx, p, pm.AuditPlanTopologyCommit, cmd.Actor, map[string]any{
			"from_version":  cmd.BaseVersion,
			"to_version":    cmd.BaseVersion + 1,
			"ops":           opsAuditDetail(cmd.Ops),
			"added_nodes":   taskIDStrings(addedNodes),
			"removed_nodes": taskIDStrings(removedNodes),
		})

		// ---- §4 step 4 (cont.): running → rebuild graph + dispatch new ready nodes. ----
		if running {
			finalTasks, err := s.tasks.ListByPlan(txCtx, cmd.PlanID)
			if err != nil {
				return err
			}
			reloadedEdges, err := s.plans.ListDependencies(txCtx, cmd.PlanID)
			if err != nil {
				return err
			}
			// Rebuild the orchestration graph to mirror the new topology. buildPlanGraph
			// self-heals node status from live task state (syncGraphToTasks), so done/
			// running nodes keep their state; dispatch records persist across the rebuild,
			// so already-dispatched nodes are NOT re-dispatched (graphReadySet skips them).
			if err := s.buildPlanGraph(txCtx, p, finalTasks, reloadedEdges, now); err != nil {
				return err
			}
			dispatched, err = s.dispatchReadyNodes(txCtx, p)
			if err != nil {
				return err
			}
		}

		// ---- §4 step 4: version++ (exactly one commit increment, deterministic). ----
		p.SetVersion(cmd.BaseVersion+1, now)
		return s.plans.Update(txCtx, p)
	})
	if err != nil {
		return nil, err
	}
	return dispatched, nil
}

// topoWorkModel is the in-memory copy the batch is applied to (§4 step 2): the node
// set + the edge set keyed by from|to (one edge per ordered pair, matching the repo's
// (plan,from,to) delete grain). Intermediate states are never validated.
type topoWorkModel struct {
	nodes map[pm.TaskID]bool
	edges map[string]pm.Dependency
}

func newTopoWorkModel(nodes map[pm.TaskID]bool, edges []pm.Dependency) *topoWorkModel {
	wm := &topoWorkModel{nodes: make(map[pm.TaskID]bool, len(nodes)), edges: make(map[string]pm.Dependency, len(edges))}
	for id := range nodes {
		wm.nodes[id] = true
	}
	for _, e := range edges {
		wm.edges[edgePairKey(e.FromTaskID, e.ToTaskID)] = e
	}
	return wm
}

func (wm *topoWorkModel) apply(ops []TopologyOp) error {
	for _, op := range ops {
		switch op.Kind {
		case OpAddNode:
			if strings.TrimSpace(string(op.TaskID)) == "" {
				return fmt.Errorf("%w: add_node requires a task_id", pm.ErrInvalidStatus)
			}
			wm.nodes[op.TaskID] = true
		case OpRemoveNode:
			if strings.TrimSpace(string(op.TaskID)) == "" {
				return fmt.Errorf("%w: remove_node requires a task_id", pm.ErrInvalidStatus)
			}
			delete(wm.nodes, op.TaskID)
			// Drop every edge touching the removed node.
			for k, e := range wm.edges {
				if e.FromTaskID == op.TaskID || e.ToTaskID == op.TaskID {
					delete(wm.edges, k)
				}
			}
		case OpAddEdge:
			if strings.TrimSpace(string(op.FromTaskID)) == "" || strings.TrimSpace(string(op.ToTaskID)) == "" {
				return fmt.Errorf("%w: add_edge requires from and to", pm.ErrInvalidStatus)
			}
			wm.edges[edgePairKey(op.FromTaskID, op.ToTaskID)] = pm.Dependency{
				FromTaskID: op.FromTaskID, ToTaskID: op.ToTaskID,
				Kind: pm.NormalizeEdgeKind(op.EdgeKind), When: op.When, MaxRounds: op.MaxRounds,
			}
		case OpRemoveEdge:
			if strings.TrimSpace(string(op.FromTaskID)) == "" || strings.TrimSpace(string(op.ToTaskID)) == "" {
				return fmt.Errorf("%w: remove_edge requires from and to", pm.ErrInvalidStatus)
			}
			delete(wm.edges, edgePairKey(op.FromTaskID, op.ToTaskID))
		default:
			return fmt.Errorf("%w: unknown topology op %q", pm.ErrInvalidStatus, op.Kind)
		}
	}
	return nil
}

// edgeList returns the terminal edges in a stable order (by from,to) for deterministic
// persistence + validation.
func (wm *topoWorkModel) edgeList() []pm.Dependency {
	out := make([]pm.Dependency, 0, len(wm.edges))
	for _, e := range wm.edges {
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].FromTaskID != out[j].FromTaskID {
			return out[i].FromTaskID < out[j].FromTaskID
		}
		return out[i].ToTaskID < out[j].ToTaskID
	})
	return out
}

// nodeDiff returns the added (in terminal, not in current) and removed (in current, not
// in terminal) node ids, each stable-sorted.
func (wm *topoWorkModel) nodeDiff(cur map[pm.TaskID]bool) (added, removed []pm.TaskID) {
	for id := range wm.nodes {
		if !cur[id] {
			added = append(added, id)
		}
	}
	for id := range cur {
		if !wm.nodes[id] {
			removed = append(removed, id)
		}
	}
	sortTaskIDs(added)
	sortTaskIDs(removed)
	return added, removed
}

// structurallyAffected returns the terminal-state set of nodes whose STRUCTURE the
// batch changed (§4/§6 mutability domain): a node whose in-edge signature differs
// between the current and terminal graph (edge added/removed on it — this covers
// add_edge/remove_edge's `From` and a removed node's downstream that lost a
// prerequisite), plus every removed node itself. Added nodes are excluded (a fresh
// node is inherently mutable). Because it compares CURRENT vs TERMINAL (not per-op),
// a net-zero batch (add then remove the same edge) affects nothing — matching
// "只校验终态".
func structurallyAffected(curNodes map[pm.TaskID]bool, curEdges []pm.Dependency, wm *topoWorkModel) []pm.TaskID {
	curSig := inEdgeSignatures(curEdges)
	finalSig := inEdgeSignatures(wm.edgeList())
	affected := map[pm.TaskID]bool{}
	// nodes present in current OR terminal whose in-edge signature changed.
	seen := map[pm.TaskID]bool{}
	consider := func(id pm.TaskID) {
		if seen[id] {
			return
		}
		seen[id] = true
		if curSig[id] != finalSig[id] {
			affected[id] = true
		}
	}
	for id := range curNodes {
		consider(id)
	}
	for id := range wm.nodes {
		consider(id)
	}
	// removed nodes must themselves be mutable.
	for id := range curNodes {
		if !wm.nodes[id] {
			affected[id] = true
		}
	}
	out := make([]pm.TaskID, 0, len(affected))
	for id := range affected {
		out = append(out, id)
	}
	sortTaskIDs(out)
	return out
}

// inEdgeSignatures maps each node → a canonical signature of its in-edges (the edges
// whose From == the node, i.e. the prerequisites it depends on). Two graphs give a
// node the same signature iff its dependency structure is identical.
func inEdgeSignatures(edges []pm.Dependency) map[pm.TaskID]string {
	byFrom := map[pm.TaskID][]string{}
	for _, e := range edges {
		byFrom[e.FromTaskID] = append(byFrom[e.FromTaskID], fmt.Sprintf("%s|%s|%s|%d",
			e.ToTaskID, pm.NormalizeEdgeKind(e.Kind), e.When, e.MaxRounds))
	}
	out := make(map[pm.TaskID]string, len(byFrom))
	for id, sigs := range byFrom {
		sort.Strings(sigs)
		out[id] = strings.Join(sigs, ";")
	}
	return out
}

// edgeDiff computes the edges to remove (in current, not terminal) and to add (in
// terminal, not current) by full signature (from,to,kind,when,max_rounds), so a
// changed edge kind on the same pair is a remove+add.
func edgeDiff(cur, final []pm.Dependency) (toRemove, toAdd []pm.Dependency) {
	curSig := map[string]pm.Dependency{}
	for _, e := range cur {
		curSig[edgeFullSig(e)] = e
	}
	finalSig := map[string]pm.Dependency{}
	for _, e := range final {
		finalSig[edgeFullSig(e)] = e
	}
	for sig, e := range curSig {
		if _, ok := finalSig[sig]; !ok {
			toRemove = append(toRemove, e)
		}
	}
	for sig, e := range finalSig {
		if _, ok := curSig[sig]; !ok {
			toAdd = append(toAdd, e)
		}
	}
	return toRemove, toAdd
}

func edgePairKey(from, to pm.TaskID) string { return string(from) + "\x00" + string(to) }

func edgeFullSig(e pm.Dependency) string {
	return fmt.Sprintf("%s|%s|%s|%s|%d", e.FromTaskID, e.ToTaskID, pm.NormalizeEdgeKind(e.Kind), e.When, e.MaxRounds)
}

func sortTaskIDs(ids []pm.TaskID) {
	sort.SliceStable(ids, func(i, j int) bool { return ids[i] < ids[j] })
}

func taskIDStrings(ids []pm.TaskID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, string(id))
	}
	return out
}

// opsAuditDetail renders the ops batch into a plain []map for the audit Detail JSON.
func opsAuditDetail(ops []TopologyOp) []map[string]any {
	out := make([]map[string]any, 0, len(ops))
	for _, op := range ops {
		m := map[string]any{"op": string(op.Kind)}
		switch op.Kind {
		case OpAddNode, OpRemoveNode:
			m["task_id"] = string(op.TaskID)
		case OpAddEdge:
			m["from"] = string(op.FromTaskID)
			m["to"] = string(op.ToTaskID)
			m["kind"] = string(pm.NormalizeEdgeKind(op.EdgeKind))
			if op.When != "" {
				m["when"] = op.When
			}
			if op.MaxRounds != 0 {
				m["max_rounds"] = op.MaxRounds
			}
		case OpRemoveEdge:
			m["from"] = string(op.FromTaskID)
			m["to"] = string(op.ToTaskID)
		}
		out = append(out, m)
	}
	return out
}
