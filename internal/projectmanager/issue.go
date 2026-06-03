package projectmanager

import (
	"errors"
	"strings"
	"time"
)

// IssueStatus enum + state machine (plan §2.2):
//
//	open → in_progress → resolved → closed
//	open/in_progress → withdrawn
//	resolved/closed → reopened → open
//
// `triaged` is final-shape only, not v2.7.
type IssueStatus string

const (
	IssueOpen       IssueStatus = "open"
	IssueInProgress IssueStatus = "in_progress"
	IssueResolved   IssueStatus = "resolved"
	IssueClosed     IssueStatus = "closed"
	IssueWithdrawn  IssueStatus = "withdrawn"
	IssueReopened   IssueStatus = "reopened"
)

// IsValid reports enum membership.
func (s IssueStatus) IsValid() bool {
	switch s {
	case IssueOpen, IssueInProgress, IssueResolved, IssueClosed, IssueWithdrawn, IssueReopened:
		return true
	}
	return false
}

// issueTransitions is the allowed-transition adjacency (plan §2.2).
var issueTransitions = map[IssueStatus][]IssueStatus{
	IssueOpen:       {IssueInProgress, IssueWithdrawn},
	IssueInProgress: {IssueResolved, IssueWithdrawn},
	IssueResolved:   {IssueClosed, IssueReopened},
	IssueClosed:     {IssueReopened},
	IssueReopened:   {IssueOpen},
	IssueWithdrawn:  {}, // terminal
}

// CanTransitionTo reports whether from→to is a legal Issue transition.
func (s IssueStatus) CanTransitionTo(to IssueStatus) bool {
	for _, n := range issueTransitions[s] {
		if n == to {
			return true
		}
	}
	return false
}

// Issue is a project-scoped problem/discussion item and its state. It binds a
// stable Conversation via owner_ref pm://issues/{id} (held by Conversation,
// ADR-0047) — the binding is not stored here.
type Issue struct {
	id          IssueID
	projectID   ProjectID
	title       string
	description string
	status      IssueStatus
	createdBy   IdentityRef
	createdAt   time.Time
	updatedAt   time.Time
	version     int
}

// NewIssueInput captures constructor args.
type NewIssueInput struct {
	ID          IssueID
	ProjectID   ProjectID
	Title       string
	Description string
	CreatedBy   IdentityRef
	CreatedAt   time.Time
}

// NewIssue constructs a fresh open Issue. An Issue must belong to a Project
// (no global issues — ADR-0046 §3).
func NewIssue(in NewIssueInput) (*Issue, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("projectmanager: issue id required")
	}
	if strings.TrimSpace(string(in.ProjectID)) == "" {
		return nil, ErrEmptyProjectScope
	}
	if strings.TrimSpace(in.Title) == "" {
		return nil, errors.New("projectmanager: issue title required")
	}
	if err := in.CreatedBy.Validate(); err != nil {
		return nil, err
	}
	if in.CreatedAt.IsZero() {
		return nil, errors.New("projectmanager: created_at required")
	}
	at := in.CreatedAt.UTC()
	return &Issue{
		id:          in.ID,
		projectID:   in.ProjectID,
		title:       in.Title,
		description: in.Description,
		status:      IssueOpen,
		createdBy:   in.CreatedBy,
		createdAt:   at,
		updatedAt:   at,
		version:     1,
	}, nil
}

// RehydrateIssueInput is for repository round-trip.
type RehydrateIssueInput struct {
	ID          IssueID
	ProjectID   ProjectID
	Title       string
	Description string
	Status      IssueStatus
	CreatedBy   IdentityRef
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Version     int
}

// RehydrateIssue reconstructs without invariant checks.
func RehydrateIssue(in RehydrateIssueInput) (*Issue, error) {
	if !in.Status.IsValid() {
		return nil, ErrInvalidStatus
	}
	if in.Version < 1 {
		return nil, errors.New("projectmanager: version must be >= 1")
	}
	return &Issue{
		id:          in.ID,
		projectID:   in.ProjectID,
		title:       in.Title,
		description: in.Description,
		status:      in.Status,
		createdBy:   in.CreatedBy,
		createdAt:   in.CreatedAt.UTC(),
		updatedAt:   in.UpdatedAt.UTC(),
		version:     in.Version,
	}, nil
}

// Getters.
func (i *Issue) ID() IssueID            { return i.id }
func (i *Issue) ProjectID() ProjectID   { return i.projectID }
func (i *Issue) Title() string          { return i.title }
func (i *Issue) Description() string    { return i.description }
func (i *Issue) Status() IssueStatus    { return i.status }
func (i *Issue) CreatedBy() IdentityRef { return i.createdBy }
func (i *Issue) CreatedAt() time.Time   { return i.createdAt }
func (i *Issue) UpdatedAt() time.Time   { return i.updatedAt }
func (i *Issue) Version() int           { return i.version }

// Rename updates the display title (metadata edit, not a state transition).
func (i *Issue) Rename(title string, at time.Time) error {
	if strings.TrimSpace(title) == "" {
		return errors.New("projectmanager: issue title required")
	}
	i.title = title
	i.touch(at)
	return nil
}

// SetDescription updates the description (metadata edit).
func (i *Issue) SetDescription(desc string, at time.Time) {
	i.description = desc
	i.touch(at)
}

// Transition moves the Issue to a new status, enforcing the state machine.
func (i *Issue) Transition(to IssueStatus, at time.Time) error {
	if !to.IsValid() {
		return ErrInvalidStatus
	}
	if i.status == to {
		return nil
	}
	if !i.status.CanTransitionTo(to) {
		return ErrIllegalTransition
	}
	i.status = to
	i.touch(at)
	return nil
}

func (i *Issue) touch(at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	i.updatedAt = at.UTC()
	i.version++
}
