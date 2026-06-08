package projectmanager

import (
	"errors"
	"strings"
	"time"
)

// TaskStatus enum + state machine (v2.8.1 model fix — @oopslink: "assigned 和
// open 不是一个层级的状态，assigned 还没开始做，还是 open 状态"):
//
//	open → running → blocked → running
//	running → completed → verified
//	open/running/blocked → discarded (terminal)
//	completed/verified → reopened → open
//
// The former "assigned" STATE is removed: assignee is PURE METADATA (set/cleared
// in any non-terminal state via Assign/Unassign), not a workflow state. A task
// with an assignee is still "open" until the agent starts it (open→running). The
// former "canceled" state is renamed "discarded" (uniform 废弃 semantic with
// Issue's discarded).
type TaskStatus string

const (
	TaskOpen      TaskStatus = "open"
	TaskRunning   TaskStatus = "running"
	TaskBlocked   TaskStatus = "blocked"
	TaskCompleted TaskStatus = "completed"
	TaskVerified  TaskStatus = "verified"
	TaskDiscarded TaskStatus = "discarded" // was "canceled" (v2.8.1 rename)
	TaskReopened  TaskStatus = "reopened"
)

// IsValid reports enum membership.
func (s TaskStatus) IsValid() bool {
	switch s {
	case TaskOpen, TaskRunning, TaskBlocked,
		TaskCompleted, TaskVerified, TaskDiscarded, TaskReopened:
		return true
	}
	return false
}

// taskTransitions is the allowed-transition adjacency. Start moves open→running
// directly (assignment is metadata, not a precondition state).
var taskTransitions = map[TaskStatus][]TaskStatus{
	TaskOpen:      {TaskRunning, TaskDiscarded},
	TaskRunning:   {TaskBlocked, TaskCompleted, TaskDiscarded},
	TaskBlocked:   {TaskRunning, TaskDiscarded},
	TaskCompleted: {TaskVerified, TaskReopened},
	TaskVerified:  {TaskReopened},
	TaskDiscarded: {}, // terminal
	TaskReopened:  {TaskOpen},
}

// CanTransitionTo reports whether from→to is a legal Task transition.
func (s TaskStatus) CanTransitionTo(to TaskStatus) bool {
	for _, n := range taskTransitions[s] {
		if n == to {
			return true
		}
	}
	return false
}

// IsTerminal reports whether the task has reached a concluded state: work is
// done (completed/verified) or abandoned (discarded). A Reopen can re-activate a
// completed/verified task, but in any concluded state the task is not "active
// work in flight". The complement (the active / non-terminal set) is exactly
// {open, running, blocked, reopened}. v2.7 #107 Phase-2 (proj-B): the
// observability default task-query set is the non-terminal set.
func (s TaskStatus) IsTerminal() bool {
	switch s {
	case TaskCompleted, TaskVerified, TaskDiscarded:
		return true
	}
	return false
}

// Task is a project-scoped unit of work and its assignment state. It binds a
// stable Conversation via owner_ref pm://tasks/{id} (held by Conversation,
// ADR-0047) across reassignments. A Task may be independent or derived from an
// Issue (DerivedFromIssue).
type Task struct {
	id               TaskID
	projectID        ProjectID
	title            string
	description      string
	status           TaskStatus
	assignee         IdentityRef // empty when unassigned
	derivedFromIssue IssueID     // empty when independent
	completedBy      IdentityRef // who set completed (enforces no self-verify)
	blockedReason    string
	createdBy        IdentityRef
	createdAt        time.Time
	updatedAt        time.Time
	version          int
	// orgNumber is the per-org, per-type monotonic display/reference number
	// (v2.7.1 #245, rendered "T<n>"). Allocated at create by the org sequence; 0
	// for rows predating the allocator / not yet backfilled (DTO omits org_ref then).
	orgNumber int
}

// NewTaskInput captures constructor args.
type NewTaskInput struct {
	ID               TaskID
	ProjectID        ProjectID
	Title            string
	Description      string
	DerivedFromIssue IssueID
	CreatedBy        IdentityRef
	CreatedAt        time.Time
	// OrgNumber is the allocated per-org task number (v2.7.1 #245), supplied by
	// the service from the org sequence within the create tx.
	OrgNumber int
}

// NewTask constructs a fresh open Task. A Task must belong to a Project (no
// global/cross-project tasks — ADR-0046 §3).
func NewTask(in NewTaskInput) (*Task, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("projectmanager: task id required")
	}
	if strings.TrimSpace(string(in.ProjectID)) == "" {
		return nil, ErrEmptyProjectScope
	}
	if strings.TrimSpace(in.Title) == "" {
		return nil, errors.New("projectmanager: task title required")
	}
	if err := in.CreatedBy.Validate(); err != nil {
		return nil, err
	}
	if in.CreatedAt.IsZero() {
		return nil, errors.New("projectmanager: created_at required")
	}
	at := in.CreatedAt.UTC()
	return &Task{
		id:               in.ID,
		projectID:        in.ProjectID,
		title:            in.Title,
		description:      in.Description,
		status:           TaskOpen,
		derivedFromIssue: in.DerivedFromIssue,
		createdBy:        in.CreatedBy,
		createdAt:        at,
		updatedAt:        at,
		version:          1,
		orgNumber:        in.OrgNumber,
	}, nil
}

// RehydrateTaskInput is for repository round-trip.
type RehydrateTaskInput struct {
	ID               TaskID
	ProjectID        ProjectID
	Title            string
	Description      string
	Status           TaskStatus
	Assignee         IdentityRef
	DerivedFromIssue IssueID
	CompletedBy      IdentityRef
	BlockedReason    string
	CreatedBy        IdentityRef
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Version          int
	OrgNumber        int
}

// RehydrateTask reconstructs without invariant checks.
func RehydrateTask(in RehydrateTaskInput) (*Task, error) {
	if !in.Status.IsValid() {
		return nil, ErrInvalidStatus
	}
	if in.Version < 1 {
		return nil, errors.New("projectmanager: version must be >= 1")
	}
	return &Task{
		id:               in.ID,
		projectID:        in.ProjectID,
		title:            in.Title,
		description:      in.Description,
		status:           in.Status,
		assignee:         in.Assignee,
		derivedFromIssue: in.DerivedFromIssue,
		completedBy:      in.CompletedBy,
		blockedReason:    in.BlockedReason,
		createdBy:        in.CreatedBy,
		createdAt:        in.CreatedAt.UTC(),
		updatedAt:        in.UpdatedAt.UTC(),
		version:          in.Version,
		orgNumber:        in.OrgNumber,
	}, nil
}

// Getters.
func (t *Task) ID() TaskID                { return t.id }
func (t *Task) ProjectID() ProjectID      { return t.projectID }
func (t *Task) Title() string             { return t.title }
func (t *Task) Description() string       { return t.description }
func (t *Task) Status() TaskStatus        { return t.status }
func (t *Task) Assignee() IdentityRef     { return t.assignee }
func (t *Task) DerivedFromIssue() IssueID { return t.derivedFromIssue }
func (t *Task) CompletedBy() IdentityRef  { return t.completedBy }
func (t *Task) BlockedReason() string     { return t.blockedReason }
func (t *Task) CreatedBy() IdentityRef    { return t.createdBy }
func (t *Task) OrgNumber() int            { return t.orgNumber }
func (t *Task) CreatedAt() time.Time      { return t.createdAt }
func (t *Task) UpdatedAt() time.Time      { return t.updatedAt }
func (t *Task) Version() int              { return t.version }

// Rename updates the display title (metadata edit, not a state transition).
func (t *Task) Rename(title string, at time.Time) error {
	if strings.TrimSpace(title) == "" {
		return errors.New("projectmanager: task title required")
	}
	t.title = title
	t.touch(at)
	return nil
}

// SetDescription updates the description (metadata edit).
func (t *Task) SetDescription(desc string, at time.Time) {
	t.description = desc
	t.touch(at)
}

// Assign sets the assignee as METADATA — it does NOT change the task's workflow
// state (v2.8.1 model fix: there is no "assigned" state; an assigned task is
// still "open" until started). Allowed in any non-terminal state; re-targets an
// already-assigned task. The AppService still emits pm.task.assigned so the
// WorkItemProjector dispatches the agent WorkItem.
func (t *Task) Assign(assignee IdentityRef, at time.Time) error {
	if err := assignee.Validate(); err != nil {
		return err
	}
	if t.status.IsTerminal() {
		return ErrIllegalTransition
	}
	t.assignee = assignee
	t.touch(at)
	return nil
}

// Unassign clears the assignee (metadata edit; no state change). Allowed in any
// non-terminal state.
func (t *Task) Unassign(at time.Time) error {
	if t.status.IsTerminal() {
		return ErrIllegalTransition
	}
	t.assignee = ""
	t.touch(at)
	return nil
}

// Start moves open→running (the agent picked up the work; assignment is metadata,
// not a precondition state).
func (t *Task) Start(at time.Time) error { return t.simpleTransition(TaskRunning, at) }

// Block moves running→blocked with a required reason (plan §2.2).
func (t *Task) Block(reason string, at time.Time) error {
	if strings.TrimSpace(reason) == "" {
		return ErrBlockReasonRequired
	}
	if !t.status.CanTransitionTo(TaskBlocked) {
		return ErrIllegalTransition
	}
	t.status = TaskBlocked
	t.blockedReason = reason
	t.touch(at)
	return nil
}

// Unblock moves blocked→running.
func (t *Task) Unblock(at time.Time) error {
	if err := t.simpleTransition(TaskRunning, at); err != nil {
		return err
	}
	t.blockedReason = ""
	return nil
}

// Complete moves running→completed and records who completed it (so the same
// identity cannot later verify it).
func (t *Task) Complete(by IdentityRef, at time.Time) error {
	if err := by.Validate(); err != nil {
		return err
	}
	if !t.status.CanTransitionTo(TaskCompleted) {
		return ErrIllegalTransition
	}
	t.status = TaskCompleted
	t.completedBy = by
	t.touch(at)
	return nil
}

// Verify moves completed→verified. The verifier must NOT be the identity that
// completed the task (no self-verification — enables Agent peer-review;
// plan §2.2 / §10 OQ4).
func (t *Task) Verify(by IdentityRef, at time.Time) error {
	if err := by.Validate(); err != nil {
		return err
	}
	if !t.status.CanTransitionTo(TaskVerified) {
		return ErrIllegalTransition
	}
	if by == t.completedBy {
		return ErrSelfVerify
	}
	t.status = TaskVerified
	t.touch(at)
	return nil
}

// Discard moves open/running/blocked→discarded (terminal; was "Cancel" pre-v2.8.1).
func (t *Task) Discard(at time.Time) error { return t.simpleTransition(TaskDiscarded, at) }

// Reopen moves completed/verified→reopened.
func (t *Task) Reopen(at time.Time) error { return t.simpleTransition(TaskReopened, at) }

// ToOpenFromReopened moves reopened→open (completing the reopen chain).
func (t *Task) ToOpenFromReopened(at time.Time) error {
	if err := t.simpleTransition(TaskOpen, at); err != nil {
		return err
	}
	// A reopened task starts fresh: clear assignment + completion truth.
	t.assignee = ""
	t.completedBy = ""
	t.blockedReason = ""
	return nil
}

// simpleTransition applies a status-only move guarded by the state machine.
func (t *Task) simpleTransition(to TaskStatus, at time.Time) error {
	if !to.IsValid() {
		return ErrInvalidStatus
	}
	if !t.status.CanTransitionTo(to) {
		return ErrIllegalTransition
	}
	t.status = to
	t.touch(at)
	return nil
}

func (t *Task) touch(at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	t.updatedAt = at.UTC()
	t.version++
}
