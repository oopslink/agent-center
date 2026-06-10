package projectmanager

import "time"

// DispatchRecord is the ONLY orchestrator-owned stored state in the Plan model
// (v2.9 #285, design §9.2/§9.3). Node status itself is DERIVED — never stored —
// from f(task.status, upstream-all-done?, dispatch-record). A DispatchRecord is
// written ONCE when a ready node's @mention is posted into the Plan conversation;
// advance dispatches a ready node only if it has no record, so re-running advance
// / event replay / a second upstream completing NEVER double-@mentions (§9.3).
// It is scoped to one Plan (§9.8): {plan_id, task_id} is the identity.
type DispatchRecord struct {
	PlanID            PlanID
	TaskID            TaskID
	DispatchedAt      time.Time
	DispatchMessageID string
}
