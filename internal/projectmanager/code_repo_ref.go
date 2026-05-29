package projectmanager

import (
	"errors"
	"strings"
	"time"
)

// CodeRepoRef is a repository reference attached to a Project (ADR-0046 §2).
// It is a lightweight reference (URL + optional label), not a VCS integration.
type CodeRepoRef struct {
	id        string
	projectID ProjectID
	url       string
	label     string
	addedBy   IdentityRef
	createdAt time.Time
}

// NewCodeRepoRefInput captures constructor args.
type NewCodeRepoRefInput struct {
	ID        string
	ProjectID ProjectID
	URL       string
	Label     string
	AddedBy   IdentityRef
	CreatedAt time.Time
}

// NewCodeRepoRef constructs a repo reference.
func NewCodeRepoRef(in NewCodeRepoRefInput) (*CodeRepoRef, error) {
	if strings.TrimSpace(in.ID) == "" {
		return nil, errors.New("projectmanager: code repo ref id required")
	}
	if strings.TrimSpace(string(in.ProjectID)) == "" {
		return nil, ErrEmptyProjectScope
	}
	if strings.TrimSpace(in.URL) == "" {
		return nil, errors.New("projectmanager: code repo ref url required")
	}
	if in.CreatedAt.IsZero() {
		return nil, errors.New("projectmanager: created_at required")
	}
	return &CodeRepoRef{
		id:        in.ID,
		projectID: in.ProjectID,
		url:       in.URL,
		label:     in.Label,
		addedBy:   in.AddedBy,
		createdAt: in.CreatedAt.UTC(),
	}, nil
}

// RehydrateCodeRepoRef reconstructs from storage.
func RehydrateCodeRepoRef(in NewCodeRepoRefInput) (*CodeRepoRef, error) {
	return NewCodeRepoRef(in)
}

// Getters.
func (c *CodeRepoRef) ID() string           { return c.id }
func (c *CodeRepoRef) ProjectID() ProjectID { return c.projectID }
func (c *CodeRepoRef) URL() string          { return c.url }
func (c *CodeRepoRef) Label() string        { return c.label }
func (c *CodeRepoRef) AddedBy() IdentityRef { return c.addedBy }
func (c *CodeRepoRef) CreatedAt() time.Time { return c.createdAt }
