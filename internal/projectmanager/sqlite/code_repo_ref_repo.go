package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// CodeRepoRefRepo implements pm.CodeRepoRefRepository.
type CodeRepoRefRepo struct{ db *sql.DB }

// NewCodeRepoRefRepo constructs the repo.
func NewCodeRepoRefRepo(db *sql.DB) *CodeRepoRefRepo { return &CodeRepoRefRepo{db: db} }

func (r *CodeRepoRefRepo) Save(ctx context.Context, c *pm.CodeRepoRef) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO pm_code_repo_refs (id, project_id, url, label, added_by, created_at) VALUES (?,?,?,?,?,?)`,
		c.ID(), string(c.ProjectID()), c.URL(), nullString(c.Label()), string(c.AddedBy()), ts(c.CreatedAt()))
	return err
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

const codeRepoSelect = `SELECT id, project_id, url, label, added_by, created_at FROM pm_code_repo_refs`

func scanCodeRepoRef(scan func(...any) error) (*pm.CodeRepoRef, error) {
	var (
		id, projectID, url, addedBy, createdAt string
		label                                  sql.NullString
	)
	if err := scan(&id, &projectID, &url, &label, &addedBy, &createdAt); err != nil {
		return nil, err
	}
	return pm.RehydrateCodeRepoRef(pm.NewCodeRepoRefInput{
		ID: id, ProjectID: pm.ProjectID(projectID), URL: url, Label: label.String,
		AddedBy: pm.IdentityRef(addedBy), CreatedAt: parseTime(createdAt),
	})
}

var _ pm.CodeRepoRefRepository = (*CodeRepoRefRepo)(nil)
