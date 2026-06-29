// Package sqlite implements the coderepo RepoRepository over SQLite (v2.18.4 BE-1).
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/oopslink/agent-center/internal/coderepo"
	"github.com/oopslink/agent-center/internal/persistence"
)

// RepoRepo implements coderepo.RepoRepository.
type RepoRepo struct{ db *sql.DB }

// NewRepoRepo constructs the repo.
func NewRepoRepo(db *sql.DB) *RepoRepo { return &RepoRepo{db: db} }

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (r *RepoRepo) Save(ctx context.Context, repo *coderepo.Repo) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO code_repos (id, organization_id, label, description, url, provider, default_branch,
			credential_ciphertext, credential_nonce, created_by, created_at, updated_at, version)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		repo.ID(), repo.OrgID(), repo.Label(), nullStr(repo.Description()), repo.URL(), string(repo.Provider()),
		nullStr(repo.DefaultBranch()), nullBlob(repo.CredentialCiphertext()), nullBlob(repo.CredentialNonce()),
		string(repo.CreatedBy()), ts(repo.CreatedAt()), ts(repo.UpdatedAt()), repo.Version())
	return err
}

func (r *RepoRepo) Update(ctx context.Context, repo *coderepo.Repo) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE code_repos SET label=?, description=?, url=?, provider=?, default_branch=?,
			credential_ciphertext=?, credential_nonce=?, updated_at=?, version=? WHERE id=?`,
		repo.Label(), nullStr(repo.Description()), repo.URL(), string(repo.Provider()), nullStr(repo.DefaultBranch()),
		nullBlob(repo.CredentialCiphertext()), nullBlob(repo.CredentialNonce()), ts(repo.UpdatedAt()), repo.Version(), repo.ID())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return coderepo.ErrRepoNotFound
	}
	return nil
}

func (r *RepoRepo) FindByID(ctx context.Context, id string) (*coderepo.Repo, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, repoSelect+` WHERE id = ?`, id)
	repo, err := scanRepo(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, coderepo.ErrRepoNotFound
	}
	return repo, err
}

func (r *RepoRepo) ListByOrg(ctx context.Context, orgID string) ([]*coderepo.Repo, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, repoSelect+` WHERE organization_id = ? ORDER BY created_at, id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*coderepo.Repo
	for rows.Next() {
		repo, err := scanRepo(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, repo)
	}
	return out, rows.Err()
}

func (r *RepoRepo) Delete(ctx context.Context, id string) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx, `DELETE FROM code_repos WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return coderepo.ErrRepoNotFound
	}
	return nil
}

const repoSelect = `SELECT id, organization_id, label, description, url, provider, default_branch,
	credential_ciphertext, credential_nonce, created_by, created_at, updated_at, version FROM code_repos`

func scanRepo(scan func(...any) error) (*coderepo.Repo, error) {
	var (
		id, org, label, url, provider, createdBy, createdAt, updatedAt string
		desc, defaultBranch                                            sql.NullString
		ciphertext, nonce                                              []byte
		version                                                        int
	)
	if err := scan(&id, &org, &label, &desc, &url, &provider, &defaultBranch,
		&ciphertext, &nonce, &createdBy, &createdAt, &updatedAt, &version); err != nil {
		return nil, err
	}
	return coderepo.RehydrateRepo(coderepo.RehydrateRepoInput{
		ID: id, OrgID: org, Label: label, Description: desc.String, URL: url,
		Provider: coderepo.Provider(provider), DefaultBranch: defaultBranch.String,
		CredentialCiphertext: ciphertext, CredentialNonce: nonce,
		CreatedBy: coderepo.IdentityRef(createdBy),
		CreatedAt: parseTime(createdAt), UpdatedAt: parseTime(updatedAt), Version: version,
	}), nil
}

// nullBlob stores nil/empty as SQL NULL (no credential).
func nullBlob(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

var _ coderepo.RepoRepository = (*RepoRepo)(nil)
