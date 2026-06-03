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
	// v2.7 #131 (PR-6): CountActiveExecutions removed (read retired task_executions; dead path).
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
