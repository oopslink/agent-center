// Package inputrequest hosts the InputRequest aggregate root (03-input-
// request.md).
package inputrequest

import (
	"errors"
	"fmt"
)

// Status is the 4-value enum (03-input-request § 3).
type Status string

const (
	StatusPending   Status = "pending"
	StatusResponded Status = "responded"
	StatusTimedOut  Status = "timed_out"
	StatusCanceled  Status = "canceled"
)

// IsValid checks enum membership.
func (s Status) IsValid() bool {
	switch s {
	case StatusPending, StatusResponded, StatusTimedOut, StatusCanceled:
		return true
	}
	return false
}

// String returns the enum value.
func (s Status) String() string { return string(s) }

// IsTerminal returns true for responded / timed_out / canceled.
func (s Status) IsTerminal() bool {
	return s == StatusResponded || s == StatusTimedOut || s == StatusCanceled
}

// Urgency is the 2-value enum.
type Urgency string

const (
	UrgencyNormal Urgency = "normal"
	UrgencyUrgent Urgency = "urgent"
)

// IsValid checks enum membership.
func (u Urgency) IsValid() bool {
	switch u {
	case UrgencyNormal, UrgencyUrgent:
		return true
	}
	return false
}

// ParseUrgency returns the typed enum or ErrInvalidUrgency.
func ParseUrgency(s string) (Urgency, error) {
	if s == "" {
		return UrgencyNormal, nil
	}
	u := Urgency(s)
	if !u.IsValid() {
		return "", fmt.Errorf("%w: %q", ErrInvalidUrgency, s)
	}
	return u, nil
}

// Sentinel errors.
var (
	ErrInputRequestNotFound        = errors.New("taskruntime: input request not found")
	ErrInputRequestAlreadyResolved = errors.New("taskruntime: input request already resolved")
	ErrInputRequestVersionConflict = errors.New("taskruntime: input request version conflict")
	ErrInvalidStatus               = errors.New("taskruntime: invalid input_request status")
	ErrInvalidTransition           = errors.New("taskruntime: invalid input_request transition")
	ErrInvalidUrgency              = errors.New("taskruntime: invalid urgency")
)
