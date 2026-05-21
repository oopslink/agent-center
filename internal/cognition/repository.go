package cognition

import (
	"context"
	"errors"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
)

// SupervisorInvocationRepository is the persistence port for the
// SupervisorInvocation AR (cognition/00 § 5.1). Append-only at the row
// level; UPDATE goes through CAS on the version column.
type SupervisorInvocationRepository interface {
	// Save inserts a new invocation row (status=running). Returns
	// ErrScopeKeyRunningExists if another running row for the same
	// (scope_kind, scope_key) already exists (partial unique index).
	Save(ctx context.Context, inv *SupervisorInvocation) error

	// UpdateStatusToTerminal transitions a running invocation to a terminal
	// status with the InvocationOutcome carried by the AR. Uses CAS on
	// version; returns ErrInvocationVersionConflict on stale write.
	UpdateStatusToTerminal(ctx context.Context, inv *SupervisorInvocation) error

	// FindByID returns the invocation row by id. ErrInvocationNotFound
	// when absent.
	FindByID(ctx context.Context, id InvocationID) (*SupervisorInvocation, error)

	// FindRunningByScope returns the single running invocation for a scope
	// (uniqueness enforced by partial unique index). Returns
	// ErrInvocationNotFound when no row matches.
	FindRunningByScope(ctx context.Context, scope InvocationScope) (*SupervisorInvocation, error)

	// FindRunning returns all rows with status=running (crash recovery).
	FindRunning(ctx context.Context) ([]*SupervisorInvocation, error)

	// Find returns invocations matching filter ordered by id ASC.
	Find(ctx context.Context, filter InvocationFilter) ([]*SupervisorInvocation, error)
}

// InvocationFilter is the parameter set for SupervisorInvocationRepository.Find.
type InvocationFilter struct {
	Status     *InvocationStatus
	ScopeKind  *ScopeKind
	ScopeKey   *string
	Since      *time.Time
	Until      *time.Time
	Cursor     *InvocationID
	Limit      int
}

// DefaultInvocationLimit is the default page size when Limit <= 0.
const DefaultInvocationLimit = 100

// MaxInvocationLimit caps Find.Limit.
const MaxInvocationLimit = 1000

// DecisionRecordRepository is the persistence port for DecisionRecord
// (cognition/00 § 5.2). Append-only: only Append + read methods exist;
// the absence of Update/Delete is a compile-time invariant.
type DecisionRecordRepository interface {
	// Append inserts a new decision row. Returns ErrDecisionImmutable on
	// duplicate id (extreme defensive guard) and ErrRationaleRequired
	// when rationale is empty (mirrors AR-side validation).
	Append(ctx context.Context, d *DecisionRecord) error

	// FindByID returns a decision record by id. ErrDecisionNotFound when
	// absent.
	FindByID(ctx context.Context, id DecisionID) (*DecisionRecord, error)

	// FindByInvocationID lists all decisions written by a single
	// invocation, ordered by created_at ASC.
	FindByInvocationID(ctx context.Context, id InvocationID) ([]*DecisionRecord, error)

	// Find returns rows matching the filter (currently invocation_id +
	// cursor + limit only; richer filtering is roadmap).
	Find(ctx context.Context, filter DecisionFilter) ([]*DecisionRecord, error)
}

// DecisionFilter is the parameter set for DecisionRecordRepository.Find.
type DecisionFilter struct {
	InvocationID *InvocationID
	Kind         *DecisionKind
	Cursor       *DecisionID
	Limit        int
}

// DefaultDecisionLimit is the default page size.
const DefaultDecisionLimit = 100

// MaxDecisionLimit caps Find.Limit.
const MaxDecisionLimit = 1000

// Sentinel errors. Use errors.Is to test.
var (
	ErrInvocationNotFound        = errors.New("cognition: invocation not found")
	ErrInvocationAlreadyTerminal = errors.New("cognition: invocation already in terminal state")
	ErrInvocationVersionConflict = errors.New("cognition: invocation version conflict (CAS)")
	ErrScopeKeyRunningExists     = errors.New("cognition: another running invocation already exists for this scope")
	ErrInvalidStatusTransition   = errors.New("cognition: invalid status transition")

	ErrDecisionNotFound  = errors.New("cognition: decision record not found")
	ErrDecisionImmutable = errors.New("cognition: decision record id already exists (immutable)")
	ErrRationaleRequired = errors.New("cognition: decision record rationale is required")
	ErrInvocationIDRequired = errors.New("cognition: decision record invocation_id is required")

	ErrInvocationLimitTooLarge = errors.New("cognition: invocation query limit exceeds MaxInvocationLimit")
	ErrDecisionLimitTooLarge   = errors.New("cognition: decision query limit exceeds MaxDecisionLimit")
)

// EventIDsAsStrings is a helper that converts a TriggerEventSet (or any
// []observability.EventID slice) into a []string — convenient for refs /
// payload construction.
func EventIDsAsStrings(ids []observability.EventID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = string(id)
	}
	return out
}
