package projectmanager

import "time"

// Plan node-status DERIVATION (v2.9 #285, design §9.2/§9.7). PURE logic — no I/O.
//
// §9.2 RED-LINE: node status is DERIVED, never stored as a competing field:
//
//	node_status = f(task.status, upstream-all-done?, dispatch-record)
//
// The six distinct states (and how they derive):
//   - done       : task terminal-done (completed / verified).
//   - failed     : task terminal-fail (discarded).
//   - running    : task is running.
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
	NodeDone       NodeStatus = "done"
	NodeFailed     NodeStatus = "failed"
)

// taskIsDone reports the §9.2 terminal-DONE mapping. ADR-0046: "verified" removed,
// so DONE == completed (the only success-terminal state).
func taskIsDone(s TaskStatus) bool { return s == TaskCompleted }

// taskIsFailed reports the §9.2 terminal-FAIL mapping (discarded).
func taskIsFailed(s TaskStatus) bool { return s == TaskDiscarded }

// TaskIsFailed is the exported §9.2 terminal-FAIL predicate (discarded), reused
// by the orchestrator's P2-2 failure handler so the "is this node failed?" rule
// lives in ONE place (§9.7 — a failed node leaves its downstream blocked).
func TaskIsFailed(s TaskStatus) bool { return taskIsFailed(s) }

// DeriveNodeStatus computes one node's DERIVED status (§9.2) from the three
// inputs. Precedence: terminal task state (done/failed) and running mirror the
// task directly; otherwise upstream gating decides blocked vs ready/dispatched.
func DeriveNodeStatus(taskStatus TaskStatus, upstreamAllDone bool, dispatched bool) NodeStatus {
	switch {
	case taskIsDone(taskStatus):
		return NodeDone
	case taskIsFailed(taskStatus):
		return NodeFailed
	case taskStatus == TaskRunning:
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

// ComputePlanView derives the whole-Plan read model from the selected tasks, the
// DAG edges, and the dispatch records (§9.2/§9.7/§9.1). It is PURE: callers load
// the three inputs and pass them in. Nodes are returned in the input `tasks`
// order (callers pass a stable order); the ready-set follows that same order.
func ComputePlanView(tasks []*Task, edges []Dependency, dispatch []DispatchRecord) PlanView {
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
	// upstream[N] = tasks N depends_on (edge.ToTaskID where edge.FromTaskID == N).
	upstream := make(map[TaskID][]TaskID, len(tasks))
	for _, e := range edges {
		// Only consider edges whose endpoints are both in the plan's task set.
		if _, ok := inPlan[e.FromTaskID]; !ok {
			continue
		}
		if _, ok := inPlan[e.ToTaskID]; !ok {
			continue
		}
		upstream[e.FromTaskID] = append(upstream[e.FromTaskID], e.ToTaskID)
	}

	view := PlanView{Progress: PlanProgress{Total: len(tasks)}}
	allDone := len(tasks) > 0
	for _, t := range tasks {
		// upstreamAllDone: every upstream node is terminal-done. A missing upstream
		// status (shouldn't happen — edges are pruned to in-plan tasks) is treated
		// as not-done (conservative: keeps the node blocked rather than dispatching).
		upstreamAllDone := true
		for _, up := range upstream[t.ID()] {
			us, ok := statusOf[up]
			if !ok || !taskIsDone(us) {
				upstreamAllDone = false
				break
			}
		}
		_, dispatched := dispatchedSet[t.ID()]
		ns := DeriveNodeStatus(t.Status(), upstreamAllDone, dispatched)
		view.Nodes = append(view.Nodes, PlanNodeView{
			TaskID:            t.ID(),
			TaskStatus:        t.Status(),
			NodeStatus:        ns,
			DependsOn:         upstream[t.ID()],
			Dispatched:        dispatched,
			DispatchedAt:      dispatchedAt[t.ID()],
			DispatchMessageID: dispatchedMsg[t.ID()],
		})
		switch ns {
		case NodeReady:
			view.ReadySet = append(view.ReadySet, t.ID())
		case NodeDone:
			view.Progress.Done++
		case NodeFailed:
			view.HasFailed = true
		}
		if ns != NodeDone {
			allDone = false
		}
	}
	view.AllDone = allDone
	return view
}
