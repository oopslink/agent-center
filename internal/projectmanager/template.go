package projectmanager

import (
	"errors"
	"strings"
	"time"
)

type TemplateID string

type Template struct {
	id          TemplateID
	orgID       string
	name        string
	description string
	content     string     // markdown content
	builtin     bool       // system-preinstalled
	createdBy   IdentityRef
	createdAt   time.Time
	updatedAt   time.Time
	version     int
}

type NewTemplateInput struct {
	ID          TemplateID
	OrgID       string
	Name        string
	Description string
	Content     string
	Builtin     bool
	CreatedBy   IdentityRef
	CreatedAt   time.Time
}

func NewTemplate(in NewTemplateInput) (*Template, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("projectmanager: template id required")
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, errors.New("projectmanager: template name required")
	}
	if strings.TrimSpace(in.Content) == "" {
		return nil, errors.New("projectmanager: template content required")
	}
	at := in.CreatedAt
	if at.IsZero() {
		at = time.Now()
	}
	return &Template{
		id:          in.ID,
		orgID:       in.OrgID,
		name:        in.Name,
		description: in.Description,
		content:     in.Content,
		builtin:     in.Builtin,
		createdBy:   in.CreatedBy,
		createdAt:   at.UTC(),
		updatedAt:   at.UTC(),
		version:     1,
	}, nil
}

type RehydrateTemplateInput struct {
	ID          TemplateID
	OrgID       string
	Name        string
	Description string
	Content     string
	Builtin     bool
	CreatedBy   IdentityRef
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Version     int
}

func RehydrateTemplate(in RehydrateTemplateInput) (*Template, error) {
	if in.Version < 1 {
		return nil, errors.New("projectmanager: template version must be >= 1")
	}
	return &Template{
		id:          in.ID,
		orgID:       in.OrgID,
		name:        in.Name,
		description: in.Description,
		content:     in.Content,
		builtin:     in.Builtin,
		createdBy:   in.CreatedBy,
		createdAt:   in.CreatedAt.UTC(),
		updatedAt:   in.UpdatedAt.UTC(),
		version:     in.Version,
	}, nil
}

// Getters
func (t *Template) ID() TemplateID        { return t.id }
func (t *Template) OrgID() string         { return t.orgID }
func (t *Template) Name() string          { return t.name }
func (t *Template) Description() string   { return t.description }
func (t *Template) Content() string       { return t.content }
func (t *Template) IsBuiltin() bool       { return t.builtin }
func (t *Template) CreatedBy() IdentityRef { return t.createdBy }
func (t *Template) CreatedAt() time.Time  { return t.createdAt }
func (t *Template) UpdatedAt() time.Time  { return t.updatedAt }
func (t *Template) Version() int          { return t.version }

// Mutations
func (t *Template) Update(name, description, content string, at time.Time) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("projectmanager: template name required")
	}
	if strings.TrimSpace(content) == "" {
		return errors.New("projectmanager: template content required")
	}
	t.name = name
	t.description = description
	t.content = content
	t.touch(at)
	return nil
}

func (t *Template) touch(at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	t.updatedAt = at.UTC()
	t.version++
}
