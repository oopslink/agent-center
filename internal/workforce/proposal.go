package workforce

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// CandidateMetadata is the JSON-marshaled metadata attached to a Proposal
// (workforce/03 § 2).
type CandidateMetadata struct {
	GitRemoteURL     string `json:"git_remote_url,omitempty"`
	CommitCount      int    `json:"commit_count,omitempty"`
	RecentActivityAt string `json:"recent_activity_at,omitempty"`
	DetectedLanguage string `json:"detected_language,omitempty"`
}

// Marshal returns the JSON encoding (always a valid JSON object, "{}" if
// empty).
func (m CandidateMetadata) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

// WorkerProjectProposal is the Workforce BC AR (workforce/03).
//
// v2.5.5 dropped suggested_kind along with the ProjectKind type. The
// proposal still names a suggested target project (existing or to be
// created during Accept); kind was unused once Project lost the
// concept.
type WorkerProjectProposal struct {
	id                   ProposalID
	workerID             WorkerID
	candidatePath        string
	suggestedProjectID   ProjectID
	candidateMetadata    CandidateMetadata
	status               ProposalStatus
	proposedAt           time.Time
	reviewedAt           *time.Time
	reviewedByIdentityID string
	resultingMappingID   MappingID
	createdAt            time.Time
	updatedAt            time.Time
	version              int
}

// NewProposalInput captures constructor args.
type NewProposalInput struct {
	ID                 ProposalID
	WorkerID           WorkerID
	CandidatePath      string
	SuggestedProjectID ProjectID
	CandidateMetadata  CandidateMetadata
	ProposedAt         time.Time
}

// NewWorkerProjectProposal constructs a Proposal in `pending` state.
func NewWorkerProjectProposal(in NewProposalInput) (*WorkerProjectProposal, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("proposal: id required")
	}
	if strings.TrimSpace(string(in.WorkerID)) == "" {
		return nil, errors.New("proposal: worker_id required")
	}
	if strings.TrimSpace(in.CandidatePath) == "" {
		return nil, errors.New("proposal: candidate_path required")
	}
	if strings.TrimSpace(string(in.SuggestedProjectID)) == "" {
		return nil, errors.New("proposal: suggested_project_id required")
	}
	if in.ProposedAt.IsZero() {
		return nil, errors.New("proposal: proposed_at required")
	}
	at := in.ProposedAt.UTC()
	return &WorkerProjectProposal{
		id:                 in.ID,
		workerID:           in.WorkerID,
		candidatePath:      in.CandidatePath,
		suggestedProjectID: in.SuggestedProjectID,
		candidateMetadata:  in.CandidateMetadata,
		status:             ProposalPending,
		proposedAt:         at,
		createdAt:          at,
		updatedAt:          at,
		version:            1,
	}, nil
}

// RehydrateProposalInput is for repository round-tripping.
type RehydrateProposalInput struct {
	ID                   ProposalID
	WorkerID             WorkerID
	CandidatePath        string
	SuggestedProjectID   ProjectID
	CandidateMetadata    CandidateMetadata
	Status               ProposalStatus
	ProposedAt           time.Time
	ReviewedAt           *time.Time
	ReviewedByIdentityID string
	ResultingMappingID   MappingID
	CreatedAt            time.Time
	UpdatedAt            time.Time
	Version              int
}

// RehydrateWorkerProjectProposal reconstructs without re-validating.
func RehydrateWorkerProjectProposal(in RehydrateProposalInput) (*WorkerProjectProposal, error) {
	if !in.Status.IsValid() {
		return nil, errors.New("proposal: invalid status")
	}
	if in.Version < 1 {
		return nil, errors.New("proposal: version must be >= 1")
	}
	return &WorkerProjectProposal{
		id:                   in.ID,
		workerID:             in.WorkerID,
		candidatePath:        in.CandidatePath,
		suggestedProjectID:   in.SuggestedProjectID,
		candidateMetadata:    in.CandidateMetadata,
		status:               in.Status,
		proposedAt:           in.ProposedAt.UTC(),
		reviewedAt:           copyTimePtr(in.ReviewedAt),
		reviewedByIdentityID: in.ReviewedByIdentityID,
		resultingMappingID:   in.ResultingMappingID,
		createdAt:            in.CreatedAt.UTC(),
		updatedAt:            in.UpdatedAt.UTC(),
		version:              in.Version,
	}, nil
}

// Getters.

func (p *WorkerProjectProposal) ID() ProposalID                { return p.id }
func (p *WorkerProjectProposal) WorkerID() WorkerID            { return p.workerID }
func (p *WorkerProjectProposal) CandidatePath() string         { return p.candidatePath }
func (p *WorkerProjectProposal) SuggestedProjectID() ProjectID { return p.suggestedProjectID }
func (p *WorkerProjectProposal) CandidateMetadata() CandidateMetadata {
	return p.candidateMetadata
}
func (p *WorkerProjectProposal) Status() ProposalStatus            { return p.status }
func (p *WorkerProjectProposal) ProposedAt() time.Time             { return p.proposedAt }
func (p *WorkerProjectProposal) ReviewedAt() *time.Time            { return copyTimePtr(p.reviewedAt) }
func (p *WorkerProjectProposal) ReviewedByIdentityID() string      { return p.reviewedByIdentityID }
func (p *WorkerProjectProposal) ResultingMappingID() MappingID     { return p.resultingMappingID }
func (p *WorkerProjectProposal) CreatedAt() time.Time              { return p.createdAt }
func (p *WorkerProjectProposal) UpdatedAt() time.Time              { return p.updatedAt }
func (p *WorkerProjectProposal) Version() int                      { return p.version }

// Accept transitions pending→accepted with resulting mapping id
// (workforce/03 § 4.1 + § 6.3).
func (p *WorkerProjectProposal) Accept(at time.Time, reviewedBy string, mappingID MappingID) error {
	if p.status != ProposalPending {
		return ErrProposalAlreadyTerminated
	}
	if strings.TrimSpace(reviewedBy) == "" {
		return errors.New("proposal: reviewed_by required")
	}
	if strings.TrimSpace(string(mappingID)) == "" {
		return errors.New("proposal: resulting_mapping_id required")
	}
	at = at.UTC()
	p.status = ProposalAccepted
	p.reviewedAt = &at
	p.reviewedByIdentityID = reviewedBy
	p.resultingMappingID = mappingID
	p.updatedAt = at
	p.version++
	return nil
}

// Ignore transitions pending→ignored (workforce/03 § 4.3).
func (p *WorkerProjectProposal) Ignore(at time.Time, reviewedBy string) error {
	if p.status != ProposalPending {
		return ErrProposalAlreadyTerminated
	}
	if strings.TrimSpace(reviewedBy) == "" {
		return errors.New("proposal: reviewed_by required")
	}
	at = at.UTC()
	p.status = ProposalIgnored
	p.reviewedAt = &at
	p.reviewedByIdentityID = reviewedBy
	p.updatedAt = at
	p.version++
	return nil
}

// Unignore flips ignored→pending (workforce/03 § 4.4).
func (p *WorkerProjectProposal) Unignore(at time.Time) error {
	if p.status != ProposalIgnored {
		return ErrProposalInvalidTransition
	}
	at = at.UTC()
	p.status = ProposalPending
	p.reviewedAt = nil
	p.reviewedByIdentityID = ""
	p.updatedAt = at
	p.version++
	return nil
}

// Supersede transitions pending→superseded; rarely used (workforce/03 § 1).
func (p *WorkerProjectProposal) Supersede(at time.Time, reviewedBy string) error {
	if p.status != ProposalPending {
		return ErrProposalInvalidTransition
	}
	at = at.UTC()
	p.status = ProposalSuperseded
	p.reviewedAt = &at
	p.reviewedByIdentityID = reviewedBy
	p.updatedAt = at
	p.version++
	return nil
}
