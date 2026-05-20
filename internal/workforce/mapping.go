package workforce

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// WorkerProjectMapping is the Workforce BC entity sub-attached to Worker
// (workforce/01 § 4). Tracks "this worker holds this project at this
// base_path".
//
// Per workforce/01 § 4.5: worker_id / project_id / base_path immutable;
// invalidated is a terminal state with mandatory reason + message.
type WorkerProjectMapping struct {
	id                MappingID
	workerID          WorkerID
	projectID         ProjectID
	basePath          string
	sourceProposalID  ProposalID
	status            MappingStatus
	invalidateReason  InvalidateReason
	invalidateMessage string
	addedAt           time.Time
	invalidatedAt     *time.Time
	createdAt         time.Time
	updatedAt         time.Time
	version           int
}

// NewMappingInput captures the constructor arguments.
type NewMappingInput struct {
	ID               MappingID
	WorkerID         WorkerID
	ProjectID        ProjectID
	BasePath         string
	SourceProposalID ProposalID
	AddedAt          time.Time
}

// NewWorkerProjectMapping constructs a fresh active mapping.
func NewWorkerProjectMapping(in NewMappingInput) (*WorkerProjectMapping, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("mapping: id required")
	}
	if strings.TrimSpace(string(in.WorkerID)) == "" {
		return nil, errors.New("mapping: worker_id required")
	}
	if strings.TrimSpace(string(in.ProjectID)) == "" {
		return nil, errors.New("mapping: project_id required")
	}
	if strings.TrimSpace(in.BasePath) == "" {
		return nil, errors.New("mapping: base_path required")
	}
	if in.AddedAt.IsZero() {
		return nil, errors.New("mapping: added_at required")
	}
	addedAt := in.AddedAt.UTC()
	return &WorkerProjectMapping{
		id:               in.ID,
		workerID:         in.WorkerID,
		projectID:        in.ProjectID,
		basePath:         in.BasePath,
		sourceProposalID: in.SourceProposalID,
		status:           MappingActive,
		addedAt:          addedAt,
		createdAt:        addedAt,
		updatedAt:        addedAt,
		version:          1,
	}, nil
}

// RehydrateMappingInput is for repository round-tripping.
type RehydrateMappingInput struct {
	ID                MappingID
	WorkerID          WorkerID
	ProjectID         ProjectID
	BasePath          string
	SourceProposalID  ProposalID
	Status            MappingStatus
	InvalidateReason  InvalidateReason
	InvalidateMessage string
	AddedAt           time.Time
	InvalidatedAt     *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
	Version           int
}

// RehydrateWorkerProjectMapping reconstructs a mapping without invariant
// transition checks.
func RehydrateWorkerProjectMapping(in RehydrateMappingInput) (*WorkerProjectMapping, error) {
	if !in.Status.IsValid() {
		return nil, errors.New("mapping: invalid status")
	}
	if in.Version < 1 {
		return nil, errors.New("mapping: version must be >= 1")
	}
	return &WorkerProjectMapping{
		id:                in.ID,
		workerID:          in.WorkerID,
		projectID:         in.ProjectID,
		basePath:          in.BasePath,
		sourceProposalID:  in.SourceProposalID,
		status:            in.Status,
		invalidateReason:  in.InvalidateReason,
		invalidateMessage: in.InvalidateMessage,
		addedAt:           in.AddedAt.UTC(),
		invalidatedAt:     copyTimePtr(in.InvalidatedAt),
		createdAt:         in.CreatedAt.UTC(),
		updatedAt:         in.UpdatedAt.UTC(),
		version:           in.Version,
	}, nil
}

// Getters.

func (m *WorkerProjectMapping) ID() MappingID                  { return m.id }
func (m *WorkerProjectMapping) WorkerID() WorkerID             { return m.workerID }
func (m *WorkerProjectMapping) ProjectID() ProjectID           { return m.projectID }
func (m *WorkerProjectMapping) BasePath() string               { return m.basePath }
func (m *WorkerProjectMapping) SourceProposalID() ProposalID   { return m.sourceProposalID }
func (m *WorkerProjectMapping) Status() MappingStatus          { return m.status }
func (m *WorkerProjectMapping) InvalidateReason() InvalidateReason {
	return m.invalidateReason
}
func (m *WorkerProjectMapping) InvalidateMessage() string { return m.invalidateMessage }
func (m *WorkerProjectMapping) AddedAt() time.Time        { return m.addedAt }
func (m *WorkerProjectMapping) InvalidatedAt() *time.Time { return copyTimePtr(m.invalidatedAt) }
func (m *WorkerProjectMapping) CreatedAt() time.Time      { return m.createdAt }
func (m *WorkerProjectMapping) UpdatedAt() time.Time      { return m.updatedAt }
func (m *WorkerProjectMapping) Version() int              { return m.version }

// Invalidate flips status to `invalidated` with reason+message (workforce/01
// § 4.3). Errors if mapping already invalidated.
func (m *WorkerProjectMapping) Invalidate(at time.Time, reason InvalidateReason, message string) error {
	if m.status == MappingInvalidated {
		return ErrMappingNotActive
	}
	if !reason.IsValid() {
		return fmt.Errorf("mapping: invalid invalidate reason %q", reason)
	}
	if strings.TrimSpace(message) == "" {
		return errors.New("mapping: invalidate message required (conventions § 16)")
	}
	at = at.UTC()
	m.status = MappingInvalidated
	m.invalidateReason = reason
	m.invalidateMessage = message
	m.invalidatedAt = &at
	m.updatedAt = at
	m.version++
	return nil
}
