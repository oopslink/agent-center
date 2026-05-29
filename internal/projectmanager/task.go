package projectmanager

import (
	"errors"
	"strings"
	"time"
)

// TaskStatus enum + state machine (plan §2.2):
//
//	open → assigned → running → blocked → running
//	running → completed → verified
//	open/assigned/running/blocked → canceled
//	completed/verified → reopened → open
//	assigned → open  (unassign)
type TaskStatus string

const (
	TaskOpen      TaskStatus = "open"
	TaskAssigned  TaskStatus = "assigned"
	TaskRunning   TaskStatus = "running"
	TaskBlocked   TaskStatus = "blocked"
	TaskCompleted TaskStatus = "completed"
	TaskVerified  TaskStatus = "verified"
	TaskCanceled  TaskStatus = "canceled"
	TaskReopened  TaskStatus = "reopened"
)

// IsValid reports enum membership.
func (s TaskStatus) IsValid() bool {
	switch s {
	case TaskOpen, TaskAssigned, TaskRunning, TaskBlocked,
		TaskCompleted, TaskVerified, TaskCanceled, TaskReopened:
		return true
	}
	return false
}

// taskTransitions is the allowed-transition adjacency (plan §2.2).
var taskTransitions = map[TaskStatus][]TaskStatus{
	TaskOpen:      {TaskAssigned, TaskCanceled},
	TaskAssigned:  {TaskRunning, TaskOpen, TaskCanceled}, // TaskOpen = unassign
	TaskRunning:   {TaskBlocked, TaskCompleted, TaskCanceled},
	TaskBlocked:   {TaskRunning, TaskCanceled},
	TaskCompleted: {TaskVerified, TaskReopened},
	TaskVerified:  {TaskReopened},
	TaskCanceled:  {}, // terminal
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

// Assign sets the assignee and moves open→assigned (or re-targets an already
// assigned task — the AppService orchestrates the AgentWorkItem supersede in
// B2; here we update assignment truth).
func (t *Task) Assign(assignee IdentityRef, at time.Time) error {
	if err := assignee.Validate(); err != nil {
		return err
	}
	switch t.status {
	case TaskOpen:
		if !t.status.CanTransitionTo(TaskAssigned) {
			return ErrIllegalTransition
		}
		t.status = TaskAssigned
	case TaskAssigned:
		// re-assignment keeps status assigned, changes assignee
	default:
		return ErrIllegalTransition
	}
	t.assignee = assignee
	t.touch(at)
	return nil
}

// Unassign clears the assignee, moving assigned→open.
func (t *Task) Unassign(at time.Time) error {
	if t.status != TaskAssigned {
		return ErrIllegalTransition
	}
	t.assignee = ""
	t.status = TaskOpen
	t.touch(at)
	return nil
}

// Start moves assigned→running.
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

// Cancel moves open/assigned/running/blocked→canceled (terminal).
func (t *Task) Cancel(at time.Time) error { return t.simpleTransition(TaskCanceled, at) }

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
