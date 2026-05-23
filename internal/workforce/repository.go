package workforce

import (
	"context"
	"time"
)

// WorkerConfigFields names the worker config fields targeted by
// WorkerRepository.UpdateConfig. Either pointer may be nil to leave the
// corresponding field unchanged (ADR-0023 § 3 config update path).
type WorkerConfigFields struct {
	Concurrency *WorkerConcurrency
	Discovery   *WorkerDiscovery
}

// WorkerRepository defines persistence for the Worker AR (workforce/00 §
// 5.1). Implementations live in internal/workforce/sqlite.
type WorkerRepository interface {
	FindByID(ctx context.Context, id WorkerID) (*Worker, error)
	FindByStatus(ctx context.Context, status WorkerStatus) ([]*Worker, error)
	FindAll(ctx context.Context) ([]*Worker, error)
	Save(ctx context.Context, w *Worker) error
	UpdateStatus(ctx context.Context, id WorkerID, from, to WorkerStatus, version int) error
	UpdateLastHeartbeatAt(ctx context.Context, id WorkerID, at time.Time, workingSeconds int64) error
	// UpdateConfig writes the v2 behavior config (per ADR-0023 § 3).
	// Optimistic lock on version; returns ErrWorkerVersionConflict on mismatch.
	UpdateConfig(ctx context.Context, id WorkerID, fields WorkerConfigFields, version int) error
	// UpdateCapabilities replaces the capability list (per ADR-0023 § 4
	// worker auto-probe upload). Preserves user-controlled Enabled flag
	// where the agent_cli already existed. Optimistic lock on version.
	UpdateCapabilities(ctx context.Context, id WorkerID, detected []Capability, version int) error
	// ReplaceCapabilities stores the supplied list verbatim (no merge with
	// prior Enabled flags). Use this for user-toggle paths
	// (WorkerConfigService.SetCapabilityEnabled) where the caller has
	// already constructed the final desired list. Optimistic lock on version.
	ReplaceCapabilities(ctx context.Context, id WorkerID, caps []Capability, version int) error
}

// BootstrapTokenRepository defines persistence for BootstrapToken Entity
// (ADR-0023 § 2). Repository implementations sit in internal/workforce/sqlite.
type BootstrapTokenRepository interface {
	FindByID(ctx context.Context, id BootstrapTokenID) (*BootstrapToken, error)
	// FindByValueHash is the exchange path lookup (worker → center on enroll).
	FindByValueHash(ctx context.Context, hash string) (*BootstrapToken, error)
	// FindByWorkerID returns tokens by worker; optional status filter (empty
	// = all statuses).
	FindByWorkerID(ctx context.Context, workerID WorkerID, statuses ...BootstrapTokenStatus) ([]*BootstrapToken, error)
	// FindActiveByWorkerForUpdate is used inside a transaction by the
	// reissue path; concrete impls take a SELECT...FOR UPDATE lock so a
	// concurrent reissue cannot leave the worker with two active tokens
	// (DB unique index is the ultimate guard).
	FindActiveByWorkerForUpdate(ctx context.Context, workerID WorkerID) (*BootstrapToken, error)
	Save(ctx context.Context, t *BootstrapToken) error
	// UpdateStatus persists a state transition (active → used / expired /
	// revoked). Uses pre-image `from` so concurrent transitions cannot
	// silently clobber. Returns ErrBootstrapTokenStatusConflict on mismatch.
	UpdateStatus(ctx context.Context, t *BootstrapToken, from BootstrapTokenStatus) error
	// FindExpired returns active tokens with expires_at <= before; used by
	// ScanExpired job.
	FindExpired(ctx context.Context, before time.Time) ([]*BootstrapToken, error)
}

// WorkerProjectMappingRepository (workforce/00 § 5.2).
type WorkerProjectMappingRepository interface {
	FindByID(ctx context.Context, id MappingID) (*WorkerProjectMapping, error)
	FindByWorkerID(ctx context.Context, workerID WorkerID) ([]*WorkerProjectMapping, error)
	FindByProjectID(ctx context.Context, projectID ProjectID) ([]*WorkerProjectMapping, error)
	FindByWorkerAndProject(ctx context.Context, workerID WorkerID, projectID ProjectID) (*WorkerProjectMapping, error)
	Save(ctx context.Context, m *WorkerProjectMapping) error
	Invalidate(ctx context.Context, id MappingID, reason InvalidateReason, message string, at time.Time) error
	CountActiveByProjectID(ctx context.Context, projectID ProjectID) (int, error)
}

// WorkerProjectProposalRepository (workforce/00 § 5.3).
type WorkerProjectProposalRepository interface {
	FindByID(ctx context.Context, id ProposalID) (*WorkerProjectProposal, error)
	FindByWorkerID(ctx context.Context, workerID WorkerID, statuses ...ProposalStatus) ([]*WorkerProjectProposal, error)
	FindPending(ctx context.Context) ([]*WorkerProjectProposal, error)
	FindByCandidatePath(ctx context.Context, workerID WorkerID, candidatePath string) (*WorkerProjectProposal, error)
	Save(ctx context.Context, p *WorkerProjectProposal) error
	Update(ctx context.Context, p *WorkerProjectProposal) error
}

// ProjectFilter narrows ProjectRepository.FindAll.
type ProjectFilter struct {
	Kind *ProjectKind
}

// ProjectRepository (workforce/00 § 5.4).
type ProjectRepository interface {
	FindByID(ctx context.Context, id ProjectID) (*Project, error)
	FindAll(ctx context.Context, filter ProjectFilter) ([]*Project, error)
	Save(ctx context.Context, p *Project) error
	Update(ctx context.Context, id ProjectID, fields ProjectUpdateFields, version int, at time.Time) (*Project, error)
	Delete(ctx context.Context, id ProjectID) error
}
