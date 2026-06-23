package agent

// Availability is the DERIVED agent signal (plan §10 OQ2). It is NOT stored
// and NOT a domain invariant — it exists for the UI and the dispatcher. The
// dispatcher only pushes new WorkItems to `available` agents; `busy` does not
// block queuing.
type Availability string

const (
	Available   Availability = "available"
	Busy        Availability = "busy"
	Unavailable Availability = "unavailable"
)

// DeriveAvailability computes availability from the three inputs, evaluated
// top-down with first-match-wins (OQ2):
//
//	Worker offline              → unavailable (highest priority)
//	lifecycle not running        → unavailable
//	running + has active work     → busy
//	running + no active work      → available
//
// workerOnline comes from the Environment BC (Worker.status == online);
// hasActiveTask comes from the Agent BC's AgentWorkItem stream (C2): true
// when the agent has an active or waiting_input WorkItem.
func DeriveAvailability(workerOnline bool, lifecycle AgentLifecycle, hasActiveTask bool) Availability {
	if !workerOnline {
		return Unavailable
	}
	if lifecycle != LifecycleRunning {
		return Unavailable
	}
	if hasActiveTask {
		return Busy
	}
	return Available
}

// Availability is the convenience method on the Agent AR; the WorkItem and
// Worker inputs are supplied by the caller (Agent owns neither).
func (a *Agent) Availability(workerOnline, hasActiveTask bool) Availability {
	return DeriveAvailability(workerOnline, a.lifecycle, hasActiveTask)
}
