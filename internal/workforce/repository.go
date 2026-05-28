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
	// Delete hard-removes the Worker row (v2.5-B4 #52). Returns
	// ErrWorkerNotFound if id is unknown. Repository layer doesn't
	// cascade to child tables — the service layer owns that policy.
	Delete(ctx context.Context, id WorkerID) error
	UpdateStatus(ctx context.Context, id WorkerID, from, to WorkerStatus, version int) error
	UpdateLastHeartbeatAt(ctx context.Context, id WorkerID, at time.Time, workingSeconds int64) error
	// UpdateName mutates the friendly label (v2.4-D-X1 @oopslink ask).
	// CAS on version; returns ErrWorkerVersionConflict on mismatch,
	// ErrWorkerNotFound if id is unknown.
	UpdateName(ctx context.Context, id WorkerID, name string, version int) error
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

// AgentInstanceFilter narrows AgentInstanceRepository.FindAll.
type AgentInstanceFilter struct {
	WorkerID  *WorkerID
	State     *AgentInstanceState
	IsBuiltin *bool
	// OrganizationID scopes to a specific organization (v2.6).
	OrganizationID string
}

// AgentInstanceRepository defines persistence for AgentInstance AR
// (ADR-0024 § 1 + ADR-0029 § 1).
type AgentInstanceRepository interface {
	FindByID(ctx context.Context, id AgentInstanceID) (*AgentInstance, error)
	FindByName(ctx context.Context, name string) (*AgentInstance, error)
	FindAll(ctx context.Context, filter AgentInstanceFilter) ([]*AgentInstance, error)
	Save(ctx context.Context, a *AgentInstance) error
	// UpdateState transitions state with CAS on version.
	UpdateState(ctx context.Context, id AgentInstanceID, from, to AgentInstanceState, version int) error
	// UpdateConfig writes the config JSON + max_concurrent (nil = unchanged)
	// with CAS on version.
	UpdateConfig(ctx context.Context, id AgentInstanceID, config string, maxConcurrent *int, version int) error
	// Archive transitions idle → archived in one DB write atomically (records
	// archived_at / archived_reason / archived_message). CAS on version.
	Archive(ctx context.Context, id AgentInstanceID, at time.Time, reason AgentInstanceArchivedReason, message string, version int) error
	// CountActiveExecutions queries task_executions for this agent_instance_id
	// in non-terminal states (ADR-0024 § 2 computed field). Returns 0 if no
	// task_executions row exists yet (e.g. agent freshly created).
	CountActiveExecutions(ctx context.Context, id AgentInstanceID) (int, error)
	// BulkUpdateStateByWorker transitions every agent on `workerID` from
	// `from` → `to`. Used for worker.offline → all agents sleeping (and
	// worker.online → awakened) bulk path.
	BulkUpdateStateByWorker(ctx context.Context, workerID WorkerID, from, to AgentInstanceState) (int, error)
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
//
// v2.5.5 dropped the Kind filter along with the ProjectKind type;
// projects are now organised by free-text tags handled in the read
// path / Web Console, not at the DB level. Left as a struct for forward
// compat — future filters (e.g. by tag, by recency) will add fields
// here.
type ProjectFilter struct {
	// OrganizationID scopes results to a specific organization (v2.6).
	// Empty string means "no org filter" — legacy callers only.
	OrganizationID string
}

// ProjectRepository (workforce/00 § 5.4).
type ProjectRepository interface {
	FindByID(ctx context.Context, id ProjectID) (*Project, error)
	FindAll(ctx context.Context, filter ProjectFilter) ([]*Project, error)
	Save(ctx context.Context, p *Project) error
	Update(ctx context.Context, id ProjectID, fields ProjectUpdateFields, version int, at time.Time) (*Project, error)
	Delete(ctx context.Context, id ProjectID) error
}
