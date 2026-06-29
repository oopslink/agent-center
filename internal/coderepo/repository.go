package coderepo

import "context"

// RepoRepository persists workspace CodeRepo aggregates (v2.18.4 BE-1).
type RepoRepository interface {
	Save(ctx context.Context, r *Repo) error
	Update(ctx context.Context, r *Repo) error
	FindByID(ctx context.Context, id string) (*Repo, error)
	// ListByOrg returns the workspace's repos, stable-ordered (created_at, id).
	ListByOrg(ctx context.Context, orgID string) ([]*Repo, error)
	Delete(ctx context.Context, id string) error
}
