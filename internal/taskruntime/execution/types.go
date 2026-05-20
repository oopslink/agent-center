// Package execution hosts the TaskExecution aggregate root + Artifact
// child entity + Workspace / reason / state-machine value objects (02-task-
// execution.md).
package execution

import (
	"errors"
	"fmt"
)

// Status is the 6-value TaskExecution status enum (02-task-execution § 3).
type Status string

const (
	StatusSubmitted     Status = "submitted"
	StatusWorking       Status = "working"
	StatusInputRequired Status = "input_required"
	StatusCompleted     Status = "completed"
	StatusFailed        Status = "failed"
	StatusKilled        Status = "killed"
)

// IsValid checks enum membership.
func (s Status) IsValid() bool {
	switch s {
	case StatusSubmitted, StatusWorking, StatusInputRequired,
		StatusCompleted, StatusFailed, StatusKilled:
		return true
	}
	return false
}

// String returns the enum value.
func (s Status) String() string { return string(s) }

// IsTerminal returns true for completed / failed / killed.
func (s Status) IsTerminal() bool {
	return s == StatusCompleted || s == StatusFailed || s == StatusKilled
}

// IsActive returns true for the 3 non-terminal states.
func (s Status) IsActive() bool {
	return s == StatusSubmitted || s == StatusWorking || s == StatusInputRequired
}

// DispatchState models the orthogonal dispatch protocol state (ADR-0011).
type DispatchState string

const (
	DispatchPendingAck DispatchState = "pending_ack"
	DispatchAcked      DispatchState = "acked"
	DispatchNone       DispatchState = "" // terminal / nack
)

// IsValid checks enum membership.
func (d DispatchState) IsValid() bool {
	switch d {
	case DispatchPendingAck, DispatchAcked, DispatchNone:
		return true
	}
	return false
}

// WorkspaceMode is the worktree/direct VO (02-task-execution § 8).
type WorkspaceMode string

const (
	WorkspaceWorktree WorkspaceMode = "worktree"
	WorkspaceDirect   WorkspaceMode = "direct"
)

// IsValid checks enum membership.
func (m WorkspaceMode) IsValid() bool {
	switch m {
	case WorkspaceWorktree, WorkspaceDirect:
		return true
	}
	return false
}

// String returns the enum value.
func (m WorkspaceMode) String() string { return string(m) }

// ParseWorkspaceMode returns the typed enum or ErrUnknownWorkspaceMode.
func ParseWorkspaceMode(s string) (WorkspaceMode, error) {
	m := WorkspaceMode(s)
	if !m.IsValid() {
		return "", fmt.Errorf("%w: %q", ErrUnknownWorkspaceMode, s)
	}
	return m, nil
}

// Sentinel domain errors (00-overview § 5.2).
var (
	ErrTaskExecutionNotFound          = errors.New("taskruntime: task execution not found")
	ErrTaskExecutionAlreadyTerminated = errors.New("taskruntime: task execution already in terminal state")
	ErrTaskExecutionVersionConflict   = errors.New("taskruntime: task execution version conflict")
	ErrSingleActiveViolation          = errors.New("taskruntime: task already has active execution (single-active invariant)")
	ErrInvalidStatus                  = errors.New("taskruntime: invalid task execution status")
	ErrInvalidTransition              = errors.New("taskruntime: invalid task execution state transition")
	ErrUnknownWorkspaceMode           = errors.New("taskruntime: unknown workspace mode")
	ErrUnknownReason                  = errors.New("taskruntime: unknown reason")
	ErrArtifactNotFound               = errors.New("taskruntime: artifact not found")
	ErrArtifactImmutable              = errors.New("taskruntime: artifact is append-only, cannot modify")
	ErrInvariantViolation             = errors.New("taskruntime: task execution invariant violation")
)
