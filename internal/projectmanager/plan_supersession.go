package projectmanager

import "time"

// PlanSupersession is an explicit, appendable execution-history fact: a failed
// historical task node remains in the plan/audit trail, but completion math may
// treat it as covered by a later successor node in the same plan.
type PlanSupersession struct {
	PlanID           PlanID
	SupersededTaskID TaskID
	SuccessorTaskID  TaskID
	Reason           string
	ActorRef         IdentityRef
	CreatedAt        time.Time
}
