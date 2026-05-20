// Package task hosts the Task aggregate root (01-task.md). 4-state state
// machine with reason+message on the abandoned terminal state.
package task

import (
	"errors"
)

// Status is the 4-value Task status enum (01-task § 3).
type Status string

const (
	StatusOpen      Status = "open"
	StatusSuspended Status = "suspended"
	StatusDone      Status = "done"
	StatusAbandoned Status = "abandoned"
)

// IsValid checks enum membership.
func (s Status) IsValid() bool {
	switch s {
	case StatusOpen, StatusSuspended, StatusDone, StatusAbandoned:
		return true
	}
	return false
}

// String returns the enum value.
func (s Status) String() string { return string(s) }

// IsTerminal reports whether the status is a terminal state.
func (s Status) IsTerminal() bool {
	return s == StatusDone || s == StatusAbandoned
}

// Priority is the 3-value priority enum (01-task § 5).
type Priority string

const (
	PriorityHigh   Priority = "high"
	PriorityMedium Priority = "medium"
	PriorityLow    Priority = "low"
)

// IsValid checks enum membership.
func (p Priority) IsValid() bool {
	switch p {
	case PriorityHigh, PriorityMedium, PriorityLow:
		return true
	}
	return false
}

// String returns the enum value.
func (p Priority) String() string { return string(p) }

// ParsePriority returns the enum value or ErrInvalidPriority.
func ParsePriority(s string) (Priority, error) {
	p := Priority(s)
	if !p.IsValid() {
		return "", ErrInvalidPriority
	}
	return p, nil
}

// AbandonedReason is the closed-enum reason for `task.abandoned` event.
// v1 free-form per 01-task § 4; we accept any non-empty string but always
// require an accompanying message (conventions § 16). Callers should choose
// stable values such as "user_request" / "supervisor_request" /
// "dispatch_limit_reached" / "no_input_channel_blocked_progress".
type AbandonedReason string

// Sentinel domain errors (01-task / 00-overview § 5.1).
var (
	ErrTaskNotFound            = errors.New("taskruntime: task not found")
	ErrTaskAlreadyExists       = errors.New("taskruntime: task already exists")
	ErrTaskInvalidTransition   = errors.New("taskruntime: invalid task status transition")
	ErrTaskVersionConflict     = errors.New("taskruntime: task version conflict (optimistic lock)")
	ErrInvalidPriority         = errors.New("taskruntime: invalid priority")
	ErrInvalidStatus           = errors.New("taskruntime: invalid task status")
	ErrTaskInvariantViolation  = errors.New("taskruntime: task invariant violation")
	ErrCannotUnbindConversation = errors.New("taskruntime: conversation unbind not supported in v1")
)
