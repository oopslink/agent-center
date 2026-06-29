package projectmanager

import (
	"errors"
	"strings"
	"time"
)

// CodeRepoRef is a repository reference attached to a Project (ADR-0046 §2).
// It is a lightweight reference (URL or a workspace-Repo pointer), not a VCS
// integration.
type CodeRepoRef struct {
	id        string
	projectID ProjectID
	url       string
	label     string
	addedBy   IdentityRef
	createdAt time.Time
	// repoID points to a workspace coderepo.Repo (v2.18.4 BE-1, issue-f980c8de).
	// "" = a legacy url-only ref (pre-0087); non-"" = a reference to a workspace Repo
	// (the project side stores no url/credential — the resolver reads them off the
	// Repo). A url-only ref keeps url set; a workspace ref MAY leave url empty.
	repoID string
	// isPrimary marks the project's primary repo (the merge-check resolver reads the
	// primary ref's Repo url). At most one primary per project (service invariant).
	isPrimary bool
}

// NewCodeRepoRefInput captures constructor args.
type NewCodeRepoRefInput struct {
	ID        string
	ProjectID ProjectID
	URL       string
	Label     string
	AddedBy   IdentityRef
	CreatedAt time.Time
	// RepoID (v2.18.4 BE-1) references a workspace coderepo.Repo; "" = url-only ref.
	RepoID    string
	IsPrimary bool
}

// NewCodeRepoRef constructs a repo reference. A ref must carry EITHER a url (legacy
// url-only ref) OR a repo_id (workspace-Repo reference) — both empty is rejected.
func NewCodeRepoRef(in NewCodeRepoRefInput) (*CodeRepoRef, error) {
	if strings.TrimSpace(in.ID) == "" {
		return nil, errors.New("projectmanager: code repo ref id required")
	}
	if strings.TrimSpace(string(in.ProjectID)) == "" {
		return nil, ErrEmptyProjectScope
	}
	if strings.TrimSpace(in.URL) == "" && strings.TrimSpace(in.RepoID) == "" {
		return nil, errors.New("projectmanager: code repo ref requires a url or a repo_id")
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
		repoID:    strings.TrimSpace(in.RepoID),
		isPrimary: in.IsPrimary,
	}, nil
}

// RehydrateCodeRepoRef reconstructs from storage WITHOUT the construct-time
// invariants. A ref may legitimately be in a state NewCodeRepoRef would reject —
// e.g. a workspace ref whose repo was deleted is unlinked to url="" + repo_id=""
// (an orphaned empty ref the FE surfaces / the owner removes). Rehydrate must
// reconstruct whatever was persisted.
func RehydrateCodeRepoRef(in NewCodeRepoRefInput) (*CodeRepoRef, error) {
	return &CodeRepoRef{
		id:        in.ID,
		projectID: in.ProjectID,
		url:       in.URL,
		label:     in.Label,
		addedBy:   in.AddedBy,
		createdAt: in.CreatedAt.UTC(),
		repoID:    strings.TrimSpace(in.RepoID),
		isPrimary: in.IsPrimary,
	}, nil
}

// SetPrimary marks/unmarks this ref as the project's primary (the service enforces
// the at-most-one-primary invariant across the project's refs).
func (c *CodeRepoRef) SetPrimary(primary bool) { c.isPrimary = primary }

// Getters.
func (c *CodeRepoRef) ID() string           { return c.id }
func (c *CodeRepoRef) ProjectID() ProjectID { return c.projectID }
func (c *CodeRepoRef) URL() string          { return c.url }
func (c *CodeRepoRef) Label() string        { return c.label }
func (c *CodeRepoRef) AddedBy() IdentityRef { return c.addedBy }
func (c *CodeRepoRef) CreatedAt() time.Time { return c.createdAt }
func (c *CodeRepoRef) RepoID() string       { return c.repoID }
func (c *CodeRepoRef) IsPrimary() bool      { return c.isPrimary }
