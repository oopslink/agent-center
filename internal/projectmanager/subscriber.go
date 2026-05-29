package projectmanager

import (
	"errors"
	"strings"
	"time"
)

// Subscriber truth lives in ProjectManager (ADR-0052). TaskSubscriber and
// IssueSubscriber are the MANUAL subscriber records. Creator and current
// assignee are always EFFECTIVE subscribers and are computed by the AppService
// (B2) — they are not stored as manual subscriber rows. Conversation
// participants mirror the effective set via the outbox projection.

// TaskSubscriber is a manual subscriber record for a Task.
type TaskSubscriber struct {
	taskID     TaskID
	identityID IdentityRef
	addedBy    IdentityRef
	createdAt  time.Time
}

// NewTaskSubscriber constructs a manual task subscriber record.
func NewTaskSubscriber(taskID TaskID, identityID, addedBy IdentityRef, at time.Time) (*TaskSubscriber, error) {
	if strings.TrimSpace(string(taskID)) == "" {
		return nil, errors.New("projectmanager: task id required")
	}
	if err := identityID.Validate(); err != nil {
		return nil, err
	}
	if at.IsZero() {
		return nil, errors.New("projectmanager: created_at required")
	}
	return &TaskSubscriber{taskID: taskID, identityID: identityID, addedBy: addedBy, createdAt: at.UTC()}, nil
}

func (s *TaskSubscriber) TaskID() TaskID          { return s.taskID }
func (s *TaskSubscriber) IdentityID() IdentityRef { return s.identityID }
func (s *TaskSubscriber) AddedBy() IdentityRef    { return s.addedBy }
func (s *TaskSubscriber) CreatedAt() time.Time    { return s.createdAt }

// IssueSubscriber is a manual subscriber record for an Issue.
type IssueSubscriber struct {
	issueID    IssueID
	identityID IdentityRef
	addedBy    IdentityRef
	createdAt  time.Time
}

// NewIssueSubscriber constructs a manual issue subscriber record.
func NewIssueSubscriber(issueID IssueID, identityID, addedBy IdentityRef, at time.Time) (*IssueSubscriber, error) {
	if strings.TrimSpace(string(issueID)) == "" {
		return nil, errors.New("projectmanager: issue id required")
	}
	if err := identityID.Validate(); err != nil {
		return nil, err
	}
	if at.IsZero() {
		return nil, errors.New("projectmanager: created_at required")
	}
	return &IssueSubscriber{issueID: issueID, identityID: identityID, addedBy: addedBy, createdAt: at.UTC()}, nil
}

func (s *IssueSubscriber) IssueID() IssueID        { return s.issueID }
func (s *IssueSubscriber) IdentityID() IdentityRef { return s.identityID }
func (s *IssueSubscriber) AddedBy() IdentityRef    { return s.addedBy }
func (s *IssueSubscriber) CreatedAt() time.Time    { return s.createdAt }
