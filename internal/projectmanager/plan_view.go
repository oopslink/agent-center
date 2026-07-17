package projectmanager

import "time"

// Plan node-status DERIVATION (v2.9 #285, design §9.2/§9.7). PURE logic — no I/O.
//
// §9.2 RED-LINE: node status is DERIVED, never stored as a competing field:
//
//	node_status = f(task.status, upstream-all-done?, dispatch-record, work-item-paused?)
//
// The seven distinct states (and how they derive):
//   - done       : task terminal-done (completed / verified).
//   - failed     : task terminal-fail (discarded).
//   - paused     : task is running BUT the agent paused its work item (T53). The
//                  underlying task stays `running` (pause is an execution-state
//                  overlay, not a task-lifecycle state), so without this the node
//                  would mis-display as `running` while nobody is working it — the
//                  "卡死/看着 running 实则停了" bug. Still derived, never stored: the
//                  source of truth is the live AgentWorkItem status.
//   - running    : task is running and its work item is not paused.
//   - blocked    : task not terminal/running AND some upstream is NOT done
//                  (a node downstream of a failed/unfinished node, §9.7).
//   - ready      : task not terminal/running, ALL upstream done, NOT yet dispatched.
//   - dispatched : task not terminal/running, ALL upstream done, dispatch record
//                  exists (mention posted) — the agent hasn't started yet
//                  (dispatched ≠ running).
//
// "upstream" of a node N is the set of tasks N depends_on (every edge.ToTaskID
// where edge.FromTaskID == N). N is satisfied iff EVERY upstream node is `done`.
// A failed/unfinished upstream therefore leaves N `blocked` (§9.7 subtree
// isolation); independent branches with no unfinished upstream still advance.

// NodeStatus is the DERIVED per-node status (§9.2). It is never persisted.
type NodeStatus string

const (
	NodeBlocked    NodeStatus = "blocked"
	NodeReady      NodeStatus = "ready"
	NodeDispatched NodeStatus = "dispatched"
	NodeRunning    NodeStatus = "running"
	NodePaused     NodeStatus = "paused" // T53: running task whose agent paused its work item
	NodeDone       NodeStatus = "done"
	NodeFailed     NodeStatus = "failed"
	// NodeSkipped (v2.13.0 I18/B1, control-flow §3.1) is a DERIVED TERMINAL status: a
	// node on a conditional branch that was NOT taken (its decision chose another
	// outcome), or any node only reachable through such pruned nodes. It counts as
	// "settled" alongside done for AllDone, so a plan with conditional branches can
	// still complete (the not-taken branch never runs). Like every NodeStatus it is
	// derived, never persisted — the task itself stays `open`, just never dispatched.
	NodeSkipped NodeStatus = "skipped"
)

// DecisionOutcome is one decision node's recorded outcome (v2.13.0 I18/B1, §2.3):
// when a decision node completes it records an outcome label (e.g. "pass"/"reject")
// that routes its conditional/loopback out-edges. Latest-wins per (PlanID, TaskID)
// — a re-decided (reopened) decision overwrites its prior outcome. Orchestrator-owned
// stored state, like DispatchRecord.
type DecisionOutcome struct {
	PlanID  PlanID
	TaskID  TaskID
	Outcome string
}

// taskIsDone reports the §9.2 terminal-DONE mapping. ADR-0046: "verified" removed,
// so DONE == completed (the only success-terminal state).
func taskIsDone(s TaskStatus) bool { return s == TaskCompleted }

// taskIsFailed reports the §9.2 terminal-FAIL mapping (discarded).
func taskIsFailed(s TaskStatus) bool { return s == TaskDiscarded }

// TaskIsDone is the exported §9.2 terminal-DONE predicate (completed), reused by the
// B1 control-flow driver so "is this node done?" lives in ONE place.
func TaskIsDone(s TaskStatus) bool { return taskIsDone(s) }

// TaskIsFailed is the exported §9.2 terminal-FAIL predicate (discarded), reused
// by the orchestrator's P2-2 failure handler so the "is this node failed?" rule
// lives in ONE place (§9.7 — a failed node leaves its downstream blocked).
func TaskIsFailed(s TaskStatus) bool { return taskIsFailed(s) }

// DeriveNodeStatus computes one node's DERIVED status (§9.2) from the four
// inputs. Precedence: terminal task state (done/failed) mirrors the task
// directly; a running task derives `paused` when its work item is paused else
// `running`; otherwise upstream gating decides blocked vs ready/dispatched.
func DeriveNodeStatus(taskStatus TaskStatus, upstreamAllDone bool, dispatched bool, paused bool) NodeStatus {
	switch {
	case taskIsDone(taskStatus):
		return NodeDone
	case taskIsFailed(taskStatus):
		return NodeFailed
	// ADR-0054: the two PARKED states must be caught here, ABOVE the upstream-gating
	// arms. Without this they fall through to the !upstreamAllDone / dispatched / default
	// tail and a delivered node renders `ready` or `dispatched` — i.e. the DAG would
	// advertise finished-and-handed-over work as startable, which is the re-dispatch this
	// issue exists to stop, and the board would read a parked node as merely un-started.
	//
	// `blocked` → NodePaused, NOT NodeBlocked: the node-status vocabulary already spends
	// `blocked` on "upstream deps unsatisfied" (§9.7), and NodePaused is the exact
	// existing meaning — set aside by its agent, an operator must resume it. Collapsing
	// the two would re-create the ADR-0046 name clash in the DERIVED layer.
	//
	// `delivered` → NodeRunning: from the DAG's point of view the node is un-settled work
	// in flight (it is not done until the acceptance says so), so downstream stays gated
	// and the node stays immutable. It is not `paused` — nobody set it aside, it is
	// progressing through an external verdict.
	case taskStatus == TaskBlocked:
		return NodePaused
	case taskStatus == TaskDelivered:
		return NodeRunning
	case taskStatus == TaskRunning:
		// T53: a running task whose agent paused its work item shows `paused`, not
		// `running` — so the DAG/card tells the truth (the agent set it aside) and an
		// operator knows to resume it rather than wait on a phantom-running node.
		if paused {
			return NodePaused
		}
		return NodeRunning
	case !upstreamAllDone:
		// open / blocked / reopened with an unsatisfied upstream → blocked (§9.7).
		return NodeBlocked
	case dispatched:
		// All upstream done + mention already posted, task not yet running.
		return NodeDispatched
	default:
		// All upstream done, not yet dispatched.
		return NodeReady
	}
}

// NodeMutable reports the live-topology-edit MUTABILITY predicate (2026-07-05
// plan-live-topology-edit design §2.1):
//
//	node mutable ⟺ NO dispatch record AND task non-terminal / non-running
//	             ⟺ node_status ∈ {blocked, ready}
//
// A mutable node has not started executing (no @mention/dispatch posted, the task is
// not running, not done, not failed), so its DAG structure — its in-edges, or its very
// presence — can be live-edited on a RUNNING plan without disturbing in-flight work. A
// dispatched / running / done / failed node is IMMUTABLE (undo it via reopen/loopback,
// not a topology edit). It reuses the SAME terminal/running predicates as
// DeriveNodeStatus (taskIsDone/taskIsFailed/TaskRunning) so the "is this node settled?"
// rule lives in one place — the mutability judgement never drifts from the derivation.
func NodeMutable(status TaskStatus, dispatched bool) bool {
	if dispatched {
		return false
	}
	// ADR-0054: a PARKED node (delivered / blocked) is settled-enough to be IMMUTABLE. It
	// has already executed — a delivered node's work is done and under review, a blocked
	// node's is half-done and paused — so live-editing its in-edges or deleting it would
	// disturb exactly the in-flight work this predicate exists to protect. Without this
	// arm both fall to `return true` (they are not done/failed/running and, once parked,
	// often carry no dispatch record), and the DAG would let them be edited away.
	if taskIsDone(status) || taskIsFailed(status) || status == TaskRunning || status.IsParked() {
		return false
	}
	return true
}

// Claimable reports whether a task can be CLAIMED (open→running) right now — a
// DERIVED predicate, never stored (ADR-0047 §1,守 §9.2 derive-not-store). A task
// is claimable iff it is not archived, still `open` (un-started; claim = open→
// running), has an assignee, is IN a plan, and that plan node is `dispatched`.
//
// Deliberately NOT claimable: a backlog task (planID=="") — captured but not yet
// ready; a `running` task — already claimed; a terminal/reopened task. The
// built-in pool makes its tasks claimable by recording a dispatch (no wake), so
// they reach nodeStatus==dispatched here just like a structured plan's ready node.
func Claimable(archived bool, status TaskStatus, assignee IdentityRef, planID PlanID, nodeStatus NodeStatus) bool {
	return !archived &&
		status == TaskOpen &&
		assignee != "" &&
		planID != "" &&
		nodeStatus == NodeDispatched
}

// TaskClaimable is the convenience form over a *Task + its derived node status.
func TaskClaimable(t *Task, nodeStatus NodeStatus) bool {
	return Claimable(t.IsArchived(), t.Status(), t.Assignee(), t.PlanID(), nodeStatus)
}

// IsBacklogInert reports the T190 backlog-INERT invariant — the NAMED, shared form
// of the rule the Claimable / EnsureTaskRunnable gates already encode per-surface:
//
//	A task with NO plan (planID == "") is BACKLOG — captured, but not yet placed for
//	work — and is therefore INERT: it cannot be claimed, started, or have its status
//	changed (complete / block). It becomes actionable only once `add_task_to_plan`
//	places it in a real plan OR it is dispatched into the built-in Assignment Pool.
//
// The ONE exemption is discard / delete (cleanup of a mis-created backlog task),
// which act on the task directly without a plan / pool / work item — see
// docs/rules/backlog-task-inert.md and ErrTaskBacklogNotActionable. The predicate is
// deliberately the PURE planID=="" test (the non-dispatched built-in case is a
// runnability concern owned by EnsureTaskRunnable, not the inert invariant).
func IsBacklogInert(planID PlanID) bool { return planID == "" }

// ClaimableInPool is the OPEN-CLAIM predicate for the built-in assignment pool
// (T83 §3.2): a pool task is claimable WITHOUT pre-assignment — any eligible
// (project-member) agent may claim it. It drops the `assignee!=""` requirement of
// Claimable (which still governs structured-plan nodes, where only the assigned
// agent claims). Backlog (planID=="") stays not-claimable. The caller MUST have
// already established that planID belongs to a built-in pool plan (IsBuiltin).
func ClaimableInPool(archived bool, status TaskStatus, planID PlanID, nodeStatus NodeStatus) bool {
	return !archived &&
		status == TaskOpen &&
		planID != "" &&
		nodeStatus == NodeDispatched
}

// PlanNodeView is one node's DERIVED projection for the read model / DTO.
type PlanNodeView struct {
	TaskID            TaskID
	TaskStatus        TaskStatus
	NodeStatus        NodeStatus
	DependsOn         []TaskID // upstream task ids (edge.ToTaskID where From == this)
	Dispatched        bool
	DispatchedAt      time.Time
	DispatchMessageID string
}

// PlanProgress is the derived {done,total} progress indicator (§9.1).
type PlanProgress struct {
	Done  int
	Total int
}

// PlanView is the whole-Plan DERIVED read model (§9.2): per-node status, the
// ready-set (nodes that are `ready` — advance dispatches exactly these), a
// derived has_failed indicator (§9.1), and {done,total} progress. AllDone reports
// the §9.1 Plan-done condition (every node `done`).
type PlanView struct {
	Nodes     []PlanNodeView
	ReadySet  []TaskID
	HasFailed bool
	Progress  PlanProgress
	AllDone   bool
}

// DerivePlanView derives the whole-Plan read model from the selected tasks, the DAG
// edges, the dispatch records, the decision outcomes, and the set of tasks whose work
// item is paused (§9.2/§9.7/§9.1). It is PURE: callers load the inputs and pass them
// in. `paused` maps a TaskID→true when that task's live AgentWorkItem is paused (T53);
// a nil/empty map means "no paused overlay" — dispatch callers pass nil since pausing
// a running node never changes the ready-set or AllDone (a paused node is neither
// ready nor done). Nodes are returned in the input `tasks` order (callers pass a
// stable order); the ready-set follows that same order.
//
// T807 ④: DerivePlanView is the graph-era READ-VIEW derivation — the pure body the
// readers (get_plan detail / list enrich / builtin pool) call directly. "The graph is
// authoritative" is a DISPATCH property (T805 ③ drives readiness/decisions/loopback off
// the engine graph); a read VIEW is a projection of LIVE task state, so it derives here
// over live tasks+deps+outcomes+dispatch rather than physically re-reading the graph's
// node statuses (which are only synced at dispatch, so possibly stale at read time —
// and a READ must not sync/write). Deriving over live truth keeps the read byte-for-byte
// correct. (T810 ⑤: the old ComputePlanView shell was deleted — DerivePlanView is the
// single read-view derivation; the graph is the DISPATCH authority.)
func DerivePlanView(tasks []*Task, edges []Dependency, dispatch []DispatchRecord, outcomes []DecisionOutcome, paused map[TaskID]bool) PlanView {
	// Index task status by id, and whether each node is dispatched.
	statusOf := make(map[TaskID]TaskStatus, len(tasks))
	inPlan := make(map[TaskID]struct{}, len(tasks))
	for _, t := range tasks {
		statusOf[t.ID()] = t.Status()
		inPlan[t.ID()] = struct{}{}
	}
	dispatchedMsg := make(map[TaskID]string, len(dispatch))
	dispatchedAt := make(map[TaskID]time.Time, len(dispatch))
	dispatchedSet := make(map[TaskID]struct{}, len(dispatch))
	for _, d := range dispatch {
		dispatchedSet[d.TaskID] = struct{}{}
		dispatchedMsg[d.TaskID] = d.DispatchMessageID
		dispatchedAt[d.TaskID] = d.DispatchedAt
	}
	// outcomeOf[D] = the recorded outcome of decision node D (latest-wins, §2.3).
	outcomeOf := make(map[TaskID]string, len(outcomes))
	for _, o := range outcomes {
		if _, ok := inPlan[o.TaskID]; ok {
			outcomeOf[o.TaskID] = o.Outcome
		}
	}

	// FORWARD edges only (loopback excluded — §5: back-edges never gate forward
	// readiness). Partition each node's forward in-edges into seq upstream and
	// conditional groups (keyed by the decision To → its accepted When labels), and
	// collect ALL forward upstream for transitive pruning.
	//   seqUp[N]   = To of N's seq in-edges (hard AND deps).
	//   condUp[N]  = decisionTo → []When for N's conditional in-edges.
	//   allUp[N]   = every forward upstream of N (seq + conditional To).
	// upstream[N] (the DependsOn view field) keeps ALL forward upstream for display.
	seqUp := make(map[TaskID][]TaskID, len(tasks))
	condUp := make(map[TaskID]map[TaskID][]string, len(tasks))
	allUp := make(map[TaskID][]TaskID, len(tasks))
	for _, e := range edges {
		if _, ok := inPlan[e.FromTaskID]; !ok {
			continue
		}
		if _, ok := inPlan[e.ToTaskID]; !ok {
			continue
		}
		if e.IsLoopback() {
			continue // §5: loopback is a back-edge, not a forward prerequisite.
		}
		allUp[e.FromTaskID] = append(allUp[e.FromTaskID], e.ToTaskID)
		if NormalizeEdgeKind(e.Kind) == EdgeConditional {
			if condUp[e.FromTaskID] == nil {
				condUp[e.FromTaskID] = make(map[TaskID][]string)
			}
			condUp[e.FromTaskID][e.ToTaskID] = append(condUp[e.FromTaskID][e.ToTaskID], e.When)
		} else {
			seqUp[e.FromTaskID] = append(seqUp[e.FromTaskID], e.ToTaskID)
		}
	}

	// Pruning (§3.1). A node is SKIPPED if (a) a conditional decision it depends on is
	// done and routed to a DIFFERENT branch (dead branch), or (b) it has forward
	// upstream(s) and EVERY one is skipped (only reachable through pruned nodes).
	// Computed as a fixpoint (transitive closure).
	//
	// T467 (issue-f5067ad2): pruning is immune ONLY to a DONE (completed) node — a node
	// that genuinely concluded its own work is never reclassified. A DISCARDED node is
	// NOT immune: an untaken conditional-branch node (a node gated on an outcome the
	// Decision did not take) is often discarded as cleanup, and such a discard on a DEAD
	// branch must converge to NodeSkipped, NOT count as a failure. A discarded node on a
	// LIVE (taken / non-conditional) branch is not on a dead path, so pruning never
	// reaches it and it stays NodeFailed (a real failure — e.g. dev truly failed, or a
	// taken branch was discarded). A FAILED/real upstream still BLOCKS (not skips)
	// downstream (§9.7): it is not `skipped`, so the all-upstream-skipped test (b) fails.
	pruneImmune := func(id TaskID) bool { return taskIsDone(statusOf[id]) }
	skipped := make(map[TaskID]bool, len(tasks))
	// (a) direct dead-branch prune.
	for _, t := range tasks {
		id := t.ID()
		if pruneImmune(id) {
			continue
		}
		for decisionTo, whens := range condUp[id] {
			if !taskIsDone(statusOf[decisionTo]) {
				continue // decision not resolved yet → pending, not pruned.
			}
			oc := outcomeOf[decisionTo]
			matched := false
			for _, w := range whens {
				if w == oc {
					matched = true
					break
				}
			}
			if !matched {
				skipped[id] = true // decision chose another branch → this path is dead.
				break
			}
		}
	}
	// (b) transitive prune: fixpoint over "all forward upstream skipped".
	for changed := true; changed; {
		changed = false
		for _, t := range tasks {
			id := t.ID()
			if skipped[id] || pruneImmune(id) {
				continue
			}
			ups := allUp[id]
			if len(ups) == 0 {
				continue
			}
			allSkipped := true
			for _, up := range ups {
				if !skipped[up] {
					allSkipped = false
					break
				}
			}
			if allSkipped {
				skipped[id] = true
				changed = true
			}
		}
	}

	view := PlanView{Progress: PlanProgress{Total: len(tasks)}}
	allDone := len(tasks) > 0
	for _, t := range tasks {
		id := t.ID()
		var ns NodeStatus
		switch {
		case taskIsDone(t.Status()):
			ns = NodeDone
		case t.Status() == TaskRunning:
			// A running node tells the truth even on a (transiently) dead branch — never
			// hide a live task behind `skipped` (a decision can still be re-decided by a
			// loopback, re-activating this node).
			if paused[id] {
				ns = NodePaused
			} else {
				ns = NodeRunning
			}
		case skipped[id]:
			// §3.1 + T467 — a not-taken conditional branch (settled). Takes precedence over
			// taskIsFailed below so a DISCARDED untaken conditional-branch node converges to
			// NodeSkipped rather than polluting has_failed. (This is also how a v2.23.0
			// feature whose Decision exhausted to reject_exhausted settles: its Integrate's
			// When=pass in-edge no longer matches → dead branch → NodeSkipped.) A discarded
			// node on a LIVE branch is not `skipped` (pruning never reached it) → it falls
			// through to NodeFailed.
			ns = NodeSkipped
		case taskIsFailed(t.Status()):
			ns = NodeFailed
		default:
			// Forward readiness (§5): all seq upstream done AND every conditional
			// decision resolved to a matching branch. A pending (not-done) decision or
			// an unfinished seq upstream → blocked.
			forwardReady := true
			for _, up := range seqUp[id] {
				if !taskIsDone(statusOf[up]) {
					forwardReady = false
					break
				}
			}
			if forwardReady {
				for decisionTo, whens := range condUp[id] {
					if !taskIsDone(statusOf[decisionTo]) {
						forwardReady = false // decision pending → blocked.
						break
					}
					oc := outcomeOf[decisionTo]
					matched := false
					for _, w := range whens {
						if w == oc {
							matched = true
							break
						}
					}
					if !matched {
						forwardReady = false // (should already be skipped, defensive)
						break
					}
				}
			}
			_, dispatched := dispatchedSet[id]
			ns = DeriveNodeStatus(t.Status(), forwardReady, dispatched, paused[id])
		}

		view.Nodes = append(view.Nodes, PlanNodeView{
			TaskID:            id,
			TaskStatus:        t.Status(),
			NodeStatus:        ns,
			DependsOn:         allUp[id],
			Dispatched:        func() bool { _, d := dispatchedSet[id]; return d }(),
			DispatchedAt:      dispatchedAt[id],
			DispatchMessageID: dispatchedMsg[id],
		})
		switch ns {
		case NodeReady:
			view.ReadySet = append(view.ReadySet, id)
		case NodeDone:
			view.Progress.Done++
		case NodeFailed:
			view.HasFailed = true
		}
		// §3.1: a Plan completes when every node is DONE or SKIPPED (a not-taken branch
		// is "settled"). Without conditional branches there are no skipped nodes, so this
		// reduces to the pre-B1 "every node done".
		if ns != NodeDone && ns != NodeSkipped {
			allDone = false
		}
	}
	view.AllDone = allDone
	return view
}
