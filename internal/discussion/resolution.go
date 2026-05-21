package discussion

import (
	"errors"
	"fmt"
	"strings"

	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
)

// ResolutionKind is the closed enum for Issue conclude resolutions per
// discussion/00-overview § 1.3.
type ResolutionKind string

const (
	// ResolutionClosedNoAction means "we decided not to do anything".
	ResolutionClosedNoAction ResolutionKind = "closed_no_action"
	// ResolutionClosedWithTasks means "we agreed; spawn these N tasks".
	ResolutionClosedWithTasks ResolutionKind = "closed_with_tasks"
	// ResolutionWithdrawn collapses conclude+withdraw (opener pulls the
	// issue back as part of the conclude flow).
	ResolutionWithdrawn ResolutionKind = "withdrawn"
)

// IsValid checks enum membership.
func (k ResolutionKind) IsValid() bool {
	switch k {
	case ResolutionClosedNoAction, ResolutionClosedWithTasks, ResolutionWithdrawn:
		return true
	}
	return false
}

// String returns the enum value.
func (k ResolutionKind) String() string { return string(k) }

// TargetStatus maps a ResolutionKind to the terminal Issue Status produced
// by a successful conclude.
func (k ResolutionKind) TargetStatus() Status {
	switch k {
	case ResolutionClosedNoAction:
		return StatusClosedNoAction
	case ResolutionClosedWithTasks:
		return StatusClosedWithTasks
	case ResolutionWithdrawn:
		return StatusWithdrawn
	}
	return ""
}

// Resolution wraps the user-supplied conclude payload. tasks must be
// non-empty when kind = closed_with_tasks; per ADR-0021 § 1 / § 7.
type Resolution struct {
	Kind    ResolutionKind
	Summary string
	Tasks   []dispatch.IssueConcludeTaskSpec
}

// ErrResolutionInvalid is returned by Resolution.Validate when the value
// would violate AR invariants.
var ErrResolutionInvalid = errors.New("discussion: invalid issue resolution")

// Validate enforces the kind / summary / tasks invariants. The dep-graph
// + topology checks happen inside IssueConcludeSpawn (already shared via
// IssueConcludeSpec.Validate).
func (r Resolution) Validate() error {
	if !r.Kind.IsValid() {
		return fmt.Errorf("%w: kind %q unknown", ErrResolutionInvalid, r.Kind)
	}
	if strings.TrimSpace(r.Summary) == "" {
		return fmt.Errorf("%w: summary required", ErrResolutionInvalid)
	}
	switch r.Kind {
	case ResolutionClosedWithTasks:
		if len(r.Tasks) == 0 {
			return fmt.Errorf("%w: closed_with_tasks requires at least 1 task spec", ErrResolutionInvalid)
		}
	case ResolutionClosedNoAction, ResolutionWithdrawn:
		if len(r.Tasks) > 0 {
			return fmt.Errorf("%w: kind %s must not carry tasks", ErrResolutionInvalid, r.Kind)
		}
	}
	return nil
}
