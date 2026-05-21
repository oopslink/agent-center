package discussion

// Status is the 6-state Issue status enum (discussion/00-overview § 1.1).
type Status string

const (
	// StatusOpen is the initial state right after issue.open.
	StatusOpen Status = "open"
	// StatusUnderDiscussion fires once a non-opener Message lands in the
	// related Conversation.
	StatusUnderDiscussion Status = "under_discussion"
	// StatusConcluded is the intermediate post-conclude-flow state; awaits
	// user confirmation (closed_no_action / closed_with_tasks / withdrawn-
	// via-conclude). In v1 conclude flow is single-step so we move directly
	// to a closed_* terminal via Conclude(), keeping `concluded` as an
	// addressable historical state for amended flows.
	StatusConcluded Status = "concluded"
	// StatusClosedNoAction is a terminal: "我们决定不做".
	StatusClosedNoAction Status = "closed_no_action"
	// StatusClosedWithTasks is a terminal: spawned N (>=1) tasks.
	StatusClosedWithTasks Status = "closed_with_tasks"
	// StatusWithdrawn is a terminal: opener withdrew the issue.
	StatusWithdrawn Status = "withdrawn"
)

// IsValid checks enum membership.
func (s Status) IsValid() bool {
	switch s {
	case StatusOpen, StatusUnderDiscussion, StatusConcluded,
		StatusClosedNoAction, StatusClosedWithTasks, StatusWithdrawn:
		return true
	}
	return false
}

// String returns the enum value.
func (s Status) String() string { return string(s) }

// IsTerminal reports whether the status is a terminal state (no outgoing
// transitions in the state machine).
func (s Status) IsTerminal() bool {
	switch s {
	case StatusClosedNoAction, StatusClosedWithTasks, StatusWithdrawn:
		return true
	}
	return false
}

// allowedTransitions is the closed transition table per
// discussion/00-overview § 1.1. Empty target slice ⇒ terminal.
var allowedTransitions = map[Status][]Status{
	StatusOpen: {
		StatusUnderDiscussion,
		StatusConcluded,
		StatusClosedNoAction,
		StatusClosedWithTasks,
		StatusWithdrawn,
	},
	StatusUnderDiscussion: {
		StatusConcluded,
		StatusClosedNoAction,
		StatusClosedWithTasks,
		StatusWithdrawn,
	},
	StatusConcluded: {
		StatusClosedNoAction,
		StatusClosedWithTasks,
		StatusWithdrawn,
	},
	StatusClosedNoAction:  {}, // terminal
	StatusClosedWithTasks: {}, // terminal
	StatusWithdrawn:       {}, // terminal
}

// CanTransitionTo reports whether from→to is in the allowed transition
// table. Unknown sources or targets ⇒ false.
func CanTransitionTo(from, to Status) bool {
	if !from.IsValid() || !to.IsValid() {
		return false
	}
	for _, candidate := range allowedTransitions[from] {
		if candidate == to {
			return true
		}
	}
	return false
}
