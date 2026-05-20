// Package workforce hosts the Workforce BC tactical types:
//   - Aggregate Roots: Worker (+ WorkerProjectMapping sub-entity), Project,
//     WorkerProjectProposal
//   - Value Objects: typed IDs, enum statuses, InvalidateReason, etc.
//   - Repository interfaces + sentinel errors (architecture layer)
//
// Per workforce/00-overview § 1 + § 5.
package workforce

import "errors"

// Typed identifiers (conventions § 0.3: typed alias, never bare string).
type (
	// WorkerID is the user-chosen unique identifier configured in worker.yaml.
	WorkerID string
	// ProjectID is a slug; per workforce/02 § 2 it is user-input not ULID.
	ProjectID string
	// MappingID is the ULID of a WorkerProjectMapping row.
	MappingID string
	// ProposalID is the ULID of a WorkerProjectProposal row.
	ProposalID string
)

// String returns the typed value as a plain string.
func (id WorkerID) String() string   { return string(id) }
func (id ProjectID) String() string  { return string(id) }
func (id MappingID) String() string  { return string(id) }
func (id ProposalID) String() string { return string(id) }

// WorkerStatus is the 2-state Worker status machine
// (workforce/01-worker § 1).
type WorkerStatus string

const (
	WorkerOnline  WorkerStatus = "online"
	WorkerOffline WorkerStatus = "offline"
)

// IsValid reports whether s matches a known status.
func (s WorkerStatus) IsValid() bool {
	switch s {
	case WorkerOnline, WorkerOffline:
		return true
	}
	return false
}

// String returns the underlying status string.
func (s WorkerStatus) String() string { return string(s) }

// ProposalStatus is the 4-state Proposal status machine
// (workforce/03 § 1).
type ProposalStatus string

const (
	ProposalPending    ProposalStatus = "pending"
	ProposalAccepted   ProposalStatus = "accepted"
	ProposalIgnored    ProposalStatus = "ignored"
	ProposalSuperseded ProposalStatus = "superseded"
)

// IsValid checks the enum membership.
func (s ProposalStatus) IsValid() bool {
	switch s {
	case ProposalPending, ProposalAccepted, ProposalIgnored, ProposalSuperseded:
		return true
	}
	return false
}

// IsTerminal reports whether the proposal is in a non-pending state.
func (s ProposalStatus) IsTerminal() bool {
	return s == ProposalAccepted || s == ProposalSuperseded
}

// String returns the enum value.
func (s ProposalStatus) String() string { return string(s) }

// MappingStatus is the 2-state Mapping status machine
// (workforce/01-worker § 4).
type MappingStatus string

const (
	MappingActive      MappingStatus = "active"
	MappingInvalidated MappingStatus = "invalidated"
)

// IsValid checks the enum membership.
func (s MappingStatus) IsValid() bool {
	switch s {
	case MappingActive, MappingInvalidated:
		return true
	}
	return false
}

// String returns the enum value.
func (s MappingStatus) String() string { return string(s) }

// ProjectKind is the open-set enum of project kinds (workforce/02 § 2).
type ProjectKind string

const (
	ProjectKindCoding    ProjectKind = "coding"
	ProjectKindWriting   ProjectKind = "writing"
	ProjectKindInvesting ProjectKind = "investing"
)

// IsValid reports whether k is one of the known kinds or empty (kind=null
// is allowed per workforce/02 § 5.5).
func (k ProjectKind) IsValid() bool {
	if k == "" {
		return true
	}
	switch k {
	case ProjectKindCoding, ProjectKindWriting, ProjectKindInvesting:
		return true
	}
	return false
}

// String returns the underlying value.
func (k ProjectKind) String() string { return string(k) }

// InvalidateReason is the enum used by WorkerProjectMapping.Invalidate
// (workforce/01 § 4.1).
type InvalidateReason string

const (
	InvalidateReasonPathMissing  InvalidateReason = "path_missing"
	InvalidateReasonNotGitRepo   InvalidateReason = "not_git_repo"
	InvalidateReasonManualRemove InvalidateReason = "manual_remove"
)

// IsValid reports whether r is one of the known reasons.
func (r InvalidateReason) IsValid() bool {
	switch r {
	case InvalidateReasonPathMissing, InvalidateReasonNotGitRepo, InvalidateReasonManualRemove:
		return true
	}
	return false
}

// String returns the reason value.
func (r InvalidateReason) String() string { return string(r) }

// OfflineReason categorises why a worker went offline.
type OfflineReason string

const (
	OfflineReasonHeartbeatTimeout OfflineReason = "heartbeat_timeout"
	OfflineReasonDisconnect       OfflineReason = "disconnect"
	OfflineReasonShutdown         OfflineReason = "shutdown"
)

// IsValid reports the enum membership.
func (r OfflineReason) IsValid() bool {
	switch r {
	case OfflineReasonHeartbeatTimeout, OfflineReasonDisconnect, OfflineReasonShutdown:
		return true
	}
	return false
}

// Workforce BC sentinel errors (architecture layer; impl returns these).
var (
	// Worker
	ErrWorkerNotFound        = errors.New("workforce: worker not found")
	ErrWorkerAlreadyExists   = errors.New("workforce: worker already enrolled")
	ErrWorkerVersionConflict = errors.New("workforce: worker version conflict (optimistic lock)")
	ErrWorkerInvalidStatus   = errors.New("workforce: invalid worker status")

	// Mapping
	ErrMappingNotFound      = errors.New("workforce: mapping not found")
	ErrMappingAlreadyActive = errors.New("workforce: (worker_id, project_id) already has active mapping")
	ErrMappingNotActive     = errors.New("workforce: mapping not in active state")

	// Proposal
	ErrProposalNotFound          = errors.New("workforce: proposal not found")
	ErrProposalAlreadyTerminated = errors.New("workforce: proposal already in terminal state")
	ErrProposalInvalidTransition = errors.New("workforce: invalid proposal status transition")
	ErrProposalAlreadyExists     = errors.New("workforce: proposal id already exists")
	ErrProposalVersionConflict   = errors.New("workforce: proposal version conflict (optimistic lock)")

	// Project
	ErrProjectNotFound        = errors.New("workforce: project not found")
	ErrProjectAlreadyExists   = errors.New("workforce: project_id already taken")
	ErrProjectVersionConflict = errors.New("workforce: project version conflict (optimistic lock)")
	ErrProjectHasActiveDeps   = errors.New("workforce: project has active task or mapping, cannot delete")
	ErrProjectInvalidSlug     = errors.New("workforce: project_id must be lowercase hyphenated slug")
	ErrProjectInvalidKind     = errors.New("workforce: project kind invalid")
)
