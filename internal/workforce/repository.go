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
