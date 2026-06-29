package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// CodeRepoRefRepo implements pm.CodeRepoRefRepository (and the coderepo service's
// RefUnlinker port, via CountReferencingProjects + UnlinkRepoEverywhere).
type CodeRepoRefRepo struct{ db *sql.DB }

// NewCodeRepoRefRepo constructs the repo.
func NewCodeRepoRefRepo(db *sql.DB) *CodeRepoRefRepo { return &CodeRepoRefRepo{db: db} }

func (r *CodeRepoRefRepo) Save(ctx context.Context, c *pm.CodeRepoRef) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO pm_code_repo_refs (id, project_id, url, label, added_by, created_at, repo_id, is_primary)
		 VALUES (?,?,?,?,?,?,?,?)`,
		c.ID(), string(c.ProjectID()), c.URL(), nullString(c.Label()), string(c.AddedBy()), ts(c.CreatedAt()),
		nullString(c.RepoID()), boolToInt(c.IsPrimary()))
	return err
}

// Update persists mutable ref fields (label, repo_id, is_primary). url/project are
// immutable for a ref.
func (r *CodeRepoRefRepo) Update(ctx context.Context, c *pm.CodeRepoRef) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE pm_code_repo_refs SET label=?, repo_id=?, is_primary=? WHERE id=?`,
		nullString(c.Label()), nullString(c.RepoID()), boolToInt(c.IsPrimary()), c.ID())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return pm.ErrCodeRepoRefNotFound
	}
	return nil
}

func (r *CodeRepoRefRepo) FindByID(ctx context.Context, id string) (*pm.CodeRepoRef, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, codeRepoSelect+` WHERE id = ?`, id)
	c, err := scanCodeRepoRef(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pm.ErrCodeRepoRefNotFound
	}
	return c, err
}

func (r *CodeRepoRefRepo) ListByProject(ctx context.Context, projectID pm.ProjectID) ([]*pm.CodeRepoRef, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, codeRepoSelect+` WHERE project_id = ? ORDER BY created_at, id`, string(projectID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pm.CodeRepoRef
	for rows.Next() {
		c, err := scanCodeRepoRef(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *CodeRepoRefRepo) Delete(ctx context.Context, id string) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx, `DELETE FROM pm_code_repo_refs WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return pm.ErrCodeRepoRefNotFound
	}
	return nil
}

// ClearPrimaryForProject unsets is_primary on every ref of a project EXCEPT exceptID
// — used to enforce the at-most-one-primary invariant when a new primary is set.
func (r *CodeRepoRefRepo) ClearPrimaryForProject(ctx context.Context, projectID pm.ProjectID, exceptID string) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`UPDATE pm_code_repo_refs SET is_primary=0 WHERE project_id=? AND id<>? AND is_primary=1`,
		string(projectID), exceptID)
	return err
}

// CountReferencingProjects returns the number of DISTINCT projects whose refs point
// at repoID (the coderepo RefUnlinker port — delete-confirm count).
func (r *CodeRepoRefRepo) CountReferencingProjects(ctx context.Context, repoID string) (int, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	var n int
	err := exec.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT project_id) FROM pm_code_repo_refs WHERE repo_id = ?`, repoID).Scan(&n)
	return n, err
}

// UnlinkRepoEverywhere clears repo_id + is_primary on every ref pointing at repoID
// (the coderepo RefUnlinker port — called when the Repo is deleted). Returns the
// number of DISTINCT projects affected. Runs in the caller's tx (Repo delete).
func (r *CodeRepoRefRepo) UnlinkRepoEverywhere(ctx context.Context, repoID string) (int, error) {
	n, err := r.CountReferencingProjects(ctx, repoID)
	if err != nil {
		return 0, err
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	if _, err := exec.ExecContext(ctx,
		`UPDATE pm_code_repo_refs SET repo_id=NULL, is_primary=0 WHERE repo_id=?`, repoID); err != nil {
		return 0, err
	}
	return n, nil
}

const codeRepoSelect = `SELECT id, project_id, url, label, added_by, created_at, repo_id, is_primary FROM pm_code_repo_refs`

func scanCodeRepoRef(scan func(...any) error) (*pm.CodeRepoRef, error) {
	var (
		id, projectID, url, addedBy, createdAt string
		label, repoID                          sql.NullString
		isPrimary                              int
	)
	if err := scan(&id, &projectID, &url, &label, &addedBy, &createdAt, &repoID, &isPrimary); err != nil {
		return nil, err
	}
	return pm.RehydrateCodeRepoRef(pm.NewCodeRepoRefInput{
		ID: id, ProjectID: pm.ProjectID(projectID), URL: url, Label: label.String,
		AddedBy: pm.IdentityRef(addedBy), CreatedAt: parseTime(createdAt),
		RepoID: repoID.String, IsPrimary: isPrimary != 0,
	})
}

var _ pm.CodeRepoRefRepository = (*CodeRepoRefRepo)(nil)
