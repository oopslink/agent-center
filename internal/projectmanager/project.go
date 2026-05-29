package projectmanager

import (
	"errors"
	"strings"
	"time"
)

// ProjectStatus is the Project lifecycle enum.
type ProjectStatus string

const (
	ProjectActive   ProjectStatus = "active"
	ProjectArchived ProjectStatus = "archived"
)

// IsValid reports enum membership.
func (s ProjectStatus) IsValid() bool {
	return s == ProjectActive || s == ProjectArchived
}

// Project is the project identity/lifecycle aggregate. It is NOT a giant
// aggregate: Issues and Tasks are separate ARs referencing this Project by
// id, never stored as child arrays (ADR-0046 §2).
type Project struct {
	id             ProjectID
	organizationID string
	name           string
	description    string
	status         ProjectStatus
	createdBy      IdentityRef
	createdAt      time.Time
	updatedAt      time.Time
	version        int
}

// NewProjectInput captures constructor args.
type NewProjectInput struct {
	ID             ProjectID
	OrganizationID string
	Name           string
	Description    string
	CreatedBy      IdentityRef
	CreatedAt      time.Time
}

// NewProject constructs a fresh active Project.
func NewProject(in NewProjectInput) (*Project, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("projectmanager: project id required")
	}
	if strings.TrimSpace(in.OrganizationID) == "" {
		return nil, errors.New("projectmanager: organization_id required")
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, errors.New("projectmanager: project name required")
	}
	if err := in.CreatedBy.Validate(); err != nil {
		return nil, err
	}
	if in.CreatedAt.IsZero() {
		return nil, errors.New("projectmanager: created_at required")
	}
	at := in.CreatedAt.UTC()
	return &Project{
		id:             in.ID,
		organizationID: in.OrganizationID,
		name:           in.Name,
		description:    in.Description,
		status:         ProjectActive,
		createdBy:      in.CreatedBy,
		createdAt:      at,
		updatedAt:      at,
		version:        1,
	}, nil
}

// RehydrateProjectInput is for repository round-trip.
type RehydrateProjectInput struct {
	ID             ProjectID
	OrganizationID string
	Name           string
	Description    string
	Status         ProjectStatus
	CreatedBy      IdentityRef
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Version        int
}

// RehydrateProject reconstructs without invariant checks.
func RehydrateProject(in RehydrateProjectInput) (*Project, error) {
	if !in.Status.IsValid() {
		return nil, ErrInvalidStatus
	}
	if in.Version < 1 {
		return nil, errors.New("projectmanager: version must be >= 1")
	}
	return &Project{
		id:             in.ID,
		organizationID: in.OrganizationID,
		name:           in.Name,
		description:    in.Description,
		status:         in.Status,
		createdBy:      in.CreatedBy,
		createdAt:      in.CreatedAt.UTC(),
		updatedAt:      in.UpdatedAt.UTC(),
		version:        in.Version,
	}, nil
}

// Getters.
func (p *Project) ID() ProjectID          { return p.id }
func (p *Project) OrganizationID() string { return p.organizationID }
func (p *Project) Name() string           { return p.name }
func (p *Project) Description() string    { return p.description }
func (p *Project) Status() ProjectStatus  { return p.status }
func (p *Project) CreatedBy() IdentityRef { return p.createdBy }
func (p *Project) CreatedAt() time.Time   { return p.createdAt }
func (p *Project) UpdatedAt() time.Time   { return p.updatedAt }
func (p *Project) Version() int           { return p.version }

// Rename updates the display name.
func (p *Project) Rename(name string, at time.Time) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("projectmanager: project name required")
	}
	p.name = name
	p.touch(at)
	return nil
}

// SetDescription updates the description.
func (p *Project) SetDescription(desc string, at time.Time) {
	p.description = desc
	p.touch(at)
}

// Archive marks the project archived (terminal).
func (p *Project) Archive(at time.Time) {
	p.status = ProjectArchived
	p.touch(at)
}

func (p *Project) touch(at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	p.updatedAt = at.UTC()
	p.version++
}
