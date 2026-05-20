package workforce

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Project is the Workforce BC AR (workforce/02-project).
// Pure metadata container — no business state machine in v1.
type Project struct {
	id                   ProjectID
	name                 string
	kind                 ProjectKind
	defaultAgentCLI      string
	description          string
	createdByIdentityID  string
	createdAt            time.Time
	updatedAt            time.Time
	version              int
}

// NewProjectInput captures the constructor arguments.
type NewProjectInput struct {
	ID                  ProjectID
	Name                string
	Kind                ProjectKind
	DefaultAgentCLI     string
	Description         string
	CreatedByIdentityID string
	CreatedAt           time.Time
}

// NewProject constructs a fresh Project with version=1.
func NewProject(in NewProjectInput) (*Project, error) {
	if err := ValidateProjectSlug(in.ID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, errors.New("project: name required")
	}
	if !in.Kind.IsValid() {
		return nil, ErrProjectInvalidKind
	}
	if strings.TrimSpace(in.CreatedByIdentityID) == "" {
		return nil, errors.New("project: created_by_identity_id required")
	}
	if in.CreatedAt.IsZero() {
		return nil, errors.New("project: created_at required")
	}
	return &Project{
		id:                  in.ID,
		name:                in.Name,
		kind:                in.Kind,
		defaultAgentCLI:     in.DefaultAgentCLI,
		description:         in.Description,
		createdByIdentityID: in.CreatedByIdentityID,
		createdAt:           in.CreatedAt.UTC(),
		updatedAt:           in.CreatedAt.UTC(),
		version:             1,
	}, nil
}

// RehydrateProjectInput is for repository round-tripping.
type RehydrateProjectInput struct {
	ID                  ProjectID
	Name                string
	Kind                ProjectKind
	DefaultAgentCLI     string
	Description         string
	CreatedByIdentityID string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	Version             int
}

// RehydrateProject reconstructs without re-validating slugs.
func RehydrateProject(in RehydrateProjectInput) (*Project, error) {
	if string(in.ID) == "" {
		return nil, errors.New("project: id required")
	}
	if in.Version < 1 {
		return nil, errors.New("project: version must be >= 1")
	}
	return &Project{
		id:                  in.ID,
		name:                in.Name,
		kind:                in.Kind,
		defaultAgentCLI:     in.DefaultAgentCLI,
		description:         in.Description,
		createdByIdentityID: in.CreatedByIdentityID,
		createdAt:           in.CreatedAt.UTC(),
		updatedAt:           in.UpdatedAt.UTC(),
		version:             in.Version,
	}, nil
}

// Getters.

func (p *Project) ID() ProjectID                 { return p.id }
func (p *Project) Name() string                  { return p.name }
func (p *Project) Kind() ProjectKind             { return p.kind }
func (p *Project) DefaultAgentCLI() string       { return p.defaultAgentCLI }
func (p *Project) Description() string           { return p.description }
func (p *Project) CreatedByIdentityID() string   { return p.createdByIdentityID }
func (p *Project) CreatedAt() time.Time          { return p.createdAt }
func (p *Project) UpdatedAt() time.Time          { return p.updatedAt }
func (p *Project) Version() int                  { return p.version }

// ProjectUpdateFields aggregates the legal Update changes
// (workforce/00 § 5.4).
type ProjectUpdateFields struct {
	Name            *string
	Kind            *ProjectKind
	DefaultAgentCLI *string
	Description     *string
}

// IsEmpty reports whether no fields are set.
func (u ProjectUpdateFields) IsEmpty() bool {
	return u.Name == nil && u.Kind == nil && u.DefaultAgentCLI == nil && u.Description == nil
}

// ApplyAndBumpVersion mutates the project according to fields, bumping
// version and updated_at. Project.id is never touched.
func (p *Project) ApplyAndBumpVersion(fields ProjectUpdateFields, at time.Time) error {
	if fields.IsEmpty() {
		return errors.New("project: update has no field changes")
	}
	if fields.Name != nil {
		if strings.TrimSpace(*fields.Name) == "" {
			return errors.New("project: name cannot be empty")
		}
		p.name = *fields.Name
	}
	if fields.Kind != nil {
		if !fields.Kind.IsValid() {
			return ErrProjectInvalidKind
		}
		p.kind = *fields.Kind
	}
	if fields.DefaultAgentCLI != nil {
		p.defaultAgentCLI = *fields.DefaultAgentCLI
	}
	if fields.Description != nil {
		p.description = *fields.Description
	}
	p.updatedAt = at.UTC()
	p.version++
	return nil
}

// ValidateProjectSlug enforces lowercase / hyphenated identifier rules
// (workforce/02 § 5.1).
func ValidateProjectSlug(id ProjectID) error {
	s := string(id)
	if s == "" {
		return ErrProjectInvalidSlug
	}
	if len(s) > 128 {
		return fmt.Errorf("%w: slug too long (max 128)", ErrProjectInvalidSlug)
	}
	if s[0] == '-' || s[len(s)-1] == '-' {
		return fmt.Errorf("%w: must not start or end with '-'", ErrProjectInvalidSlug)
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-':
		default:
			return fmt.Errorf("%w: contains invalid character %q (only lowercase / digit / hyphen allowed)", ErrProjectInvalidSlug, c)
		}
	}
	return nil
}
